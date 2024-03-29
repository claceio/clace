// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi/middleware"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/claceio/clace/internal/app/store" // Register db plugin
	_ "github.com/claceio/clace/plugins"            // Register builtin plugins
)

const (
	DEFAULT_CERT_FILE = "default.crt"
	DEFAULT_KEY_FILE  = "default.key"
)

// CL_HOME is the root directory for Clace logs and temp files
var CL_HOME = os.ExpandEnv("$CL_HOME")

func init() {
	if len(CL_HOME) == 0 {
		// Default to current directory if CL_HOME is not set
		CL_HOME = "."
		os.Setenv("CL_HOME", CL_HOME)
	}
}

// Server is the instance of the Clace Server
type Server struct {
	*utils.Logger
	config      *utils.ServerConfig
	db          *metadata.Metadata
	httpServer  *http.Server
	httpsServer *http.Server
	udsServer   *http.Server
	handler     *Handler
	apps        *AppStore
	authHandler *AdminBasicAuth
	ssoAuth     *SSOAuth
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *utils.ServerConfig) (*Server, error) {
	logger := utils.NewLogger(&config.Log)
	db, err := metadata.NewMetadata(logger, config)
	if err != nil {
		return nil, err
	}

	server := &Server{
		Logger: logger,
		config: config,
		db:     db,
	}
	server.apps = NewAppStore(logger, server)
	server.authHandler = NewAdminBasicAuth(logger, config)

	// Setup SSO auth
	server.ssoAuth = NewSSOAuth(logger, config)
	if err = server.ssoAuth.Setup(); err != nil {
		return nil, err
	}

	if config.Log.AccessLogging {
		accessLogger := utils.RollingFileLogger(&config.Log, "access.log")
		customLogger := log.New(accessLogger, "", log.LstdFlags)
		middleware.DefaultLogger = middleware.RequestLogger(
			&middleware.DefaultLogFormatter{Logger: customLogger, NoColor: true})
	} else {
		middleware.DefaultLogger = func(next http.Handler) http.Handler {
			return next // no-op, logging is disabled
		}
	}

	return server, nil
}

// setupAdminAccount sets up the basic auth password for admin account. If admin user is unset,
// that means admin account is not enabled. If AdminPasswordBcrypt is set, it will be used as
// the password hash for the admin account. If AdminPasswordBcrypt is not set, a random password
// will be generated for that server startup. The generated password will be printed to stdout.
func (s *Server) setupAdminAccount() (string, error) {
	if s.config.AdminUser == "" {
		s.Warn().Msg("No admin username specified, skipping admin account setup")
		return "", nil
	}

	if s.config.Security.AdminPasswordBcrypt != "" {
		s.Info().Msgf("Using admin password bcrypt hash from configuration")
		return "", nil
	}

	s.Debug().Msg("Generating admin password")
	var err error
	password, err := utils.GenerateRandomPassword()
	if err != nil {
		return "", err
	}

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(password), utils.BCRYPT_COST)
	if err != nil {
		return "", err
	}

	s.config.Security.AdminPasswordBcrypt = string(bcryptHash)
	return password, nil
}

// Start starts the Clace Server
func (s *Server) Start() error {
	s.handler = NewTCPHandler(s.Logger, s.config, s)
	serverUri := strings.TrimSpace(os.ExpandEnv(s.config.GlobalConfig.ServerUri))
	if serverUri == "" {
		return errors.New("server_uri is not set")
	}

	// Start unix domain socket server
	if !strings.HasPrefix(serverUri, "http://") && !strings.HasPrefix(serverUri, "https://") {
		// Unix domain sockets is enabled
		socketDir := path.Dir(serverUri)
		if err := os.MkdirAll(socketDir, 0700); err != nil {
			return fmt.Errorf("error creating directory %s : %s", socketDir, err)
		}

		udsHandler := NewUDSHandler(s.Logger, s.config, s)
		socket, err := net.Listen("unix", serverUri)
		if err != nil {
			_, errDial := net.Dial("unix", serverUri)
			if errDial != nil {
				// Cannot dial also, so it's safe to delete the socket file
				if removeErr := os.Remove(serverUri); removeErr != nil {
					return fmt.Errorf("error removing socket file %s : %s", serverUri, removeErr)
				}
				socket, err = net.Listen("unix", serverUri)
				if err != nil {
					return fmt.Errorf("error creating socket after deleting old file  %s : %s", serverUri, err)
				}

			} else {
				return fmt.Errorf("error creating socket, another server already running %s : %s", serverUri, err)
			}
		}

		s.udsServer = &http.Server{
			WriteTimeout: 180 * time.Second,
			ReadTimeout:  180 * time.Second,
			IdleTimeout:  30 * time.Second,
			Handler:      udsHandler.router,
		}

		s.Info().Str("address", serverUri).Msg("Starting unix domain socket server")
		go func() {
			if err := s.udsServer.Serve(socket); err != nil {
				s.Error().Err(err).Msg("UDS server error")
				if s.httpServer != nil {
					s.httpServer.Shutdown(context.Background())
				}
				if s.httpsServer != nil {
					s.httpsServer.Shutdown(context.Background())
				}
				os.Exit(1)
			}
		}()
	} else {
		s.Info().Msg("Unix domain sockets are disabled")
	}

	// Start HTTP and HTTPS servers
	if s.config.Http.Port >= 0 {
		s.httpServer = &http.Server{
			WriteTimeout: 180 * time.Second,
			ReadTimeout:  180 * time.Second,
			IdleTimeout:  30 * time.Second,
			Handler:      s.handler.router,
		}
	}

	if s.config.Https.Port >= 0 {
		s.httpsServer = s.setupHTTPSServer()
	}

	generatedPass, err := s.setupAdminAccount()
	if err != nil {
		return err
	}
	if generatedPass != "" {
		fmt.Printf("Admin user    : %s\n", s.config.AdminUser)
		fmt.Printf("Admin password: %s\n", generatedPass)
	}

	if s.httpServer != nil {
		addr := fmt.Sprintf("%s:%d", s.config.Http.Host, s.config.Http.Port)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		s.config.Http.Port = listener.Addr().(*net.TCPAddr).Port
		addr = fmt.Sprintf("%s:%d", s.config.Http.Host, s.config.Http.Port)
		s.Info().Str("address", addr).Msg("Starting HTTP server")

		go func() {
			if err := s.httpServer.Serve(listener); err != nil {
				s.Error().Err(err).Msg("HTTP server error")
				if s.httpsServer != nil {
					s.httpsServer.Shutdown(context.Background())
				}
				if s.udsServer != nil {
					s.udsServer.Shutdown(context.Background())
				}
				os.Exit(1)
			}
		}()
	}

	if s.httpsServer != nil {
		addr := fmt.Sprintf("%s:%d", s.config.Https.Host, s.config.Https.Port)
		listener, err := tls.Listen("tcp", addr, s.httpsServer.TLSConfig)
		if err != nil {
			return err
		}
		s.config.Https.Port = listener.Addr().(*net.TCPAddr).Port
		addr = fmt.Sprintf("%s:%d", s.config.Https.Host, s.config.Https.Port)
		s.Info().Str("address", addr).Msg("Starting HTTPS server")
		go func() {
			if err := s.httpsServer.Serve(listener); err != nil {
				s.Error().Err(err).Msg("HTTPS server error")
				if s.httpServer != nil {
					s.httpServer.Shutdown(context.Background())
				}
				if s.udsServer != nil {
					s.udsServer.Shutdown(context.Background())
				}
				os.Exit(1)
			}
		}()
	}
	return nil
}

func (s *Server) setupHTTPSServer() *http.Server {

	var tlsConfig *tls.Config
	if s.config.Https.ServiceEmail != "" {
		// Certmagic is enabled
		if s.config.Https.UseStaging {
			// Use Let's Encrypt staging server
			certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
		}
		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.Email = s.config.Https.ServiceEmail
		certmagic.DefaultACME.DisableHTTPChallenge = true
		// Customize the storage directory
		customStorageDir := os.ExpandEnv(s.config.Https.StorageLocation)
		certmagic.Default.Storage = &certmagic.FileStorage{Path: customStorageDir}

		magicConfig := certmagic.NewDefault()
		magicConfig.OnDemand = &certmagic.OnDemandConfig{
			DecisionFunc: func(name string) error {
				// Always issue a certificate
				return nil
			},
		}
		tlsConfig = magicConfig.TLSConfig()
		tlsConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsConfig.NextProtos...)
		tlsConfig.GetCertificate = magicConfig.GetCertificate
		tlsConfig.MinVersion = tls.VersionTLS12
	} else {
		// Certmagic is disabled, use certs from disk or create self signed ones
		tlsConfig = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				domain := hello.ServerName

				if s.config.Https.EnableCertLookup {
					certFilePath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), domain+".crt")
					certKeyPath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), domain+".key")
					// Check if certificate and key files exist on disk for the domain
					_, certErr := os.Stat(certFilePath)
					_, keyErr := os.Stat(certKeyPath)

					// If certificate and key files exist, load them
					if certErr == nil && keyErr == nil {
						cert, err := tls.LoadX509KeyPair(certFilePath, certKeyPath)
						return &cert, err
					}
				}

				certFilePath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), DEFAULT_CERT_FILE)
				certKeyPath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), DEFAULT_KEY_FILE)

				_, certErr := os.Stat(certFilePath)
				_, keyErr := os.Stat(certKeyPath)
				if certErr != nil || keyErr != nil {
					s.Info().Msgf("Generating default self signed certificate")

					if err := os.MkdirAll(os.ExpandEnv(s.config.Https.CertLocation), 0700); err != nil {
						return nil, fmt.Errorf("error creating cert directory %s : %s",
							os.ExpandEnv(s.config.Https.CertLocation), err)
					}

					err := GenerateSelfSignedCertificate(certFilePath, certKeyPath, 365*24*time.Hour)
					if err != nil {
						return nil, fmt.Errorf("error generating self signed certificate: %w", err)
					}
				}

				cert, err := tls.LoadX509KeyPair(certFilePath, certKeyPath)
				return &cert, err
			},
		}
	}

	server := &http.Server{
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      s.handler.router,
		TLSConfig:    tlsConfig,
	}
	return server
}

// Stop stops the Clace Server
func (s *Server) Stop(ctx context.Context) error {
	s.Info().Msg("Stopping service")
	var err1, err2, err3 error
	if s.httpServer != nil {
		err1 = s.httpServer.Shutdown(ctx)
	}
	if s.httpsServer != nil {
		err2 = s.httpsServer.Shutdown(ctx)
	}
	if s.udsServer != nil {
		err3 = s.udsServer.Shutdown(ctx)
	}

	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

// isGit returns true if the sourceURL is a git URL
func isGit(url string) bool {
	return strings.HasPrefix(url, "github.com") || strings.HasPrefix(url, "git@github.com") ||
		strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}
