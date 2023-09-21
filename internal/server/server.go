// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/segmentio/ksuid"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/claceio/clace/plugins" // Register builtin plugins
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
	handler     *Handler
	apps        *AppStore
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
	return server, nil
}

// setupAdminAccount sets up the basic auth password for admin account if not specified
// in the configuration. If admin user is unset, that means admin account is not enabled.
// If admin password is set, it will be used as the password for the admin account.
// If admin password hash is set, it will be used as the password hash for the admin account.
// If neither is set, a random password will be generated for the server session and used as
// the password for the admin account. The generated password will be printed to stdout.
func (s *Server) setupAdminAccount() (string, error) {
	if s.config.AdminUser == "" {
		s.Warn().Msg("No admin username specified, skipping admin account setup")
		return "", nil
	}

	password := ""
	if s.config.AdminPassword != "" {
		s.Info().Msg("Using admin password from configuration")
		password = s.config.AdminPassword
	} else {
		if s.config.AdminPasswordBcrypt != "" {
			s.Info().Msg("Using admin password bcrypt hash from configuration")
			return "", nil
		}
	}

	if password == "" {
		s.Debug().Msg("Generating admin password")
		var err error
		password, err = utils.GenerateRandomPassword()
		if err != nil {
			return "", err
		}
	}

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(password), utils.BCRYPT_COST)
	if err != nil {
		return "", err
	}

	s.config.AdminPasswordBcrypt = string(bcryptHash)

	if s.config.AdminPassword != "" {
		return "", nil
	} else {
		return password, nil
	}
}

// Start starts the Clace Server
func (s *Server) Start() error {
	s.handler = NewHandler(s.Logger, s.config, s)

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
				os.Exit(1)
			}
		}()
	}
	return nil
}

func (s *Server) setupHTTPSServer() *http.Server {
	// Use Let's Encrypt staging server
	if s.config.Https.UseStaging {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	}

	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.Email = s.config.Https.ServiceEmail

	// Customize the storage directory
	customStorageDir := os.ExpandEnv(s.config.Https.StorageLocation)
	certmagic.Default.Storage = &certmagic.FileStorage{Path: customStorageDir}

	server := &http.Server{
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      s.handler.router,
		TLSConfig: &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				domain := hello.ServerName
				s.Info().Msgf("GetCertificate called for %s", domain)

				if s.config.Https.EnableCertLookup {
					// Cert lookup is enabled
					// Check if certificate and key files exist on disk for the domain
					_, certErr := os.Stat(domain + ".crt")
					_, keyErr := os.Stat(domain + ".key")

					// If certificate and key files exist, load them
					if certErr == nil && keyErr == nil {
						cert, err := tls.LoadX509KeyPair(domain+".crt", domain+".key")
						return &cert, err
					}
				}

				if strings.TrimSpace(certmagic.DefaultACME.Email) == "" {
					// If certmagic is disabled, look for default certificate and key files
					s.Info().Msgf("Looking up default certificate for %s", domain)
					certFilePath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), DEFAULT_CERT_FILE)
					certKeyPath := path.Join(os.ExpandEnv(s.config.Https.CertLocation), DEFAULT_KEY_FILE)

					_, certErr := os.Stat(certFilePath)
					_, keyErr := os.Stat(certKeyPath)
					if certErr != nil || keyErr != nil {
						// If default certificate and key files don't exist, use certmagic to obtain a certificate
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
				}
				// If certificate or key file doesn't exist, use certmagic to obtain a certificate
				s.Info().Msgf("Auto generating certificate for %s", domain)

				certmagicConfig := certmagic.NewDefault()
				err := certmagicConfig.ManageSync(hello.Context(), []string{domain})
				if err != nil {
					return nil, err
				}
				return certmagicConfig.GetCertificate(hello)

			},
		},
	}
	return server
}

// Stop stops the Clace Server
func (s *Server) Stop(ctx context.Context) error {
	s.Info().Msg("Stopping service")
	var err1, err2 error
	if s.httpServer != nil {
		err1 = s.httpServer.Shutdown(ctx)
	}
	if s.httpsServer != nil {
		err2 = s.httpsServer.Shutdown(ctx)
	}
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *Server) AddApp(appEntry *utils.AppEntry, approve bool) (*utils.AuditResult, error) {
	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}
	appEntry.Id = utils.AppId("app" + id.String())
	err = s.db.AddApp(appEntry)
	if err != nil {
		return nil, err
	}

	application, err := s.createApp(appEntry)
	if err != nil {
		return nil, err
	}

	s.apps.AddApp(application)
	s.Debug().Msgf("Created app %s %s", appEntry.Path, appEntry.Id)

	auditResult, err := application.Audit()
	if err != nil {
		return nil, fmt.Errorf("App %s created, audit failed: %s", appEntry.Id, err)
	}

	if approve {
		appEntry.Loads = auditResult.NewLoads
		appEntry.Permissions = auditResult.NewPermissions
		s.db.UpdateAppPermissions(appEntry)
		s.Info().Msgf("Approved app %s %s: %+v %+v", appEntry.Domain, appEntry.Path, auditResult.NewLoads, auditResult.NewPermissions)
	}
	return auditResult, nil
}

func (s *Server) createApp(appEntry *utils.AppEntry) (*app.App, error) {
	subLogger := s.With().Str("id", string(appEntry.Id)).Str("path", appEntry.Path).Logger()
	appLogger := utils.Logger{Logger: &subLogger}

	path := appEntry.FsPath
	if path == "" {
		path = appEntry.SourceUrl
	}

	sourceFS := app.NewAppFS(path, os.DirFS(path), appEntry.IsDev, &s.config.System)
	appPath := fmt.Sprintf(os.ExpandEnv("$CL_HOME/run/app/%s"), appEntry.Id)
	workFS := app.NewAppFS(appPath, os.DirFS(appPath), appEntry.IsDev, &s.config.System)
	application := app.NewApp(sourceFS, workFS, &appLogger, appEntry, &s.config.System)

	return application, nil
}

func (s *Server) GetApp(pathDomain utils.AppPathDomain, init bool) (*app.App, error) {
	application, err := s.apps.GetApp(pathDomain)
	if err != nil {
		// App not found in cache, get from DB
		appEntry, err := s.db.GetApp(pathDomain)
		if err != nil {
			return nil, err
		}

		application, err = s.createApp(appEntry)
		if err != nil {
			return nil, err
		}
		s.apps.AddApp(application)
	}

	if !init {
		return application, nil
	}

	// Initialize the app
	if err := application.Initialize(); err != nil {
		return nil, fmt.Errorf("error initializing app: %w", err)
	}

	return application, nil
}

func (s *Server) DeleteApp(pathDomain utils.AppPathDomain) error {
	err := s.db.DeleteApp(pathDomain)
	if err != nil {
		return err
	}
	err = s.apps.DeleteApp(pathDomain)
	if err != nil {
		return fmt.Errorf("error deleting app: %s", err)
	}
	return nil
}

func (s *Server) serveApp(w http.ResponseWriter, r *http.Request, pathDomain utils.AppPathDomain) {
	app, err := s.GetApp(pathDomain, true)
	if err != nil {
		s.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	app.ServeHTTP(w, r)
}

func (s *Server) MatchApp(hostHeader, matchPath string) (utils.AppPathDomain, error) {
	s.Trace().Msgf("MatchApp %s %s", hostHeader, matchPath)
	var pathDomain utils.AppPathDomain
	pathDomains, err := s.db.GetAllApps()
	if err != nil {
		return pathDomain, err
	}
	matchPath = normalizePath(matchPath)

	// Find unique domains
	domainMap := map[string]bool{}
	for _, path := range pathDomains {
		if !domainMap[path.Domain] {
			domainMap[path.Domain] = true
			// TODO : cache domain list
		}
	}

	// Check if host header matches a known domain
	checkDomain := false
	if hostHeader != "" && domainMap[hostHeader] {
		s.Trace().Msgf("Matched domain %s", hostHeader)
		checkDomain = true
	}

	for _, entry := range pathDomains {
		if checkDomain && entry.Domain != hostHeader {
			// Request uses domain, but app is not for this domain
			continue
		}

		if !checkDomain && entry.Domain != "" {
			// Request does not use domain, but app is for a domain
			continue
		}

		if strings.HasPrefix(matchPath, entry.Path) {
			if len(entry.Path) == 1 || len(entry.Path) == len(matchPath) || matchPath[len(entry.Path)] == '/' {
				s.Debug().Msgf("Matched app %s for path %s", entry, matchPath)
				return entry, nil
			}
		}
	}

	return pathDomain, errors.New("no matching app found")
}

func (s *Server) MatchAppForDomain(domain, matchPath string) (string, error) {
	paths, err := s.db.GetAppsForDomain(domain)
	if err != nil {
		return "", err
	}
	matchPath = normalizePath(matchPath)
	matchedApp := ""
	for _, path := range paths {
		s.Trace().Msgf("MatchAppForDomain %s %s %t", path, matchPath, strings.HasPrefix(matchPath, path))
		// If /test is in use, do not allow /test/other
		if strings.HasPrefix(matchPath, path) {
			if len(path) == 1 || len(path) == len(matchPath) || matchPath[len(path)] == '/' {
				matchedApp = path
				s.Debug().Msgf("Matched app %s for path %s", matchedApp, matchPath)
				break
			}
		}

		// If /test/other is in use, do not allow /test
		if strings.HasPrefix(path, matchPath) {
			if len(matchPath) == 1 || len(path) == len(matchPath) || path[len(matchPath)] == '/' {
				matchedApp = path
				s.Debug().Msgf("Matched app %s for path %s", matchedApp, matchPath)
				break
			}
		}
	}

	return matchedApp, nil
}

func (s *Server) AuditApp(pathDomain utils.AppPathDomain, approve bool) (*utils.AuditResult, error) {
	app, err := s.GetApp(pathDomain, false)
	if err != nil {
		return nil, err
	}

	auditResult, err := app.Audit()
	if err != nil {
		return nil, err
	}

	if approve {
		app.AppEntry.Loads = auditResult.NewLoads
		app.AppEntry.Permissions = auditResult.NewPermissions
		s.db.UpdateAppPermissions(app.AppEntry)
		s.Info().Msgf("Approved app %s %s: %+v %+v", pathDomain.Path, pathDomain.Domain, auditResult.NewLoads, auditResult.NewPermissions)
	}
	return auditResult, nil
}
