// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
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
	config     *utils.ServerConfig
	db         *metadata.Metadata
	httpServer *http.Server
	handler    *Handler
	apps       *AppStore
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
// in the configuration. If admin user is unset, tat means admin account is not enabled.
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
	addr := fmt.Sprintf("%s:%d", s.config.Http.Host, s.config.Http.Port)
	s.Info().Str("address", addr).Msg("Starting HTTP server")
	s.handler = NewHandler(s.Logger, s.config, s)
	s.httpServer = &http.Server{
		Addr:         addr,
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
		IdleTimeout:  30 * time.Second,
		Handler:      s.handler.router,
	}

	generatedPass, err := s.setupAdminAccount()
	if err != nil {
		return err
	}
	if generatedPass != "" {
		fmt.Printf("Admin user    : %s\n", s.config.AdminUser)
		fmt.Printf("Admin password: %s\n", generatedPass)
	}

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil {
			s.Error().Err(err).Msg("server error")
			os.Exit(1)
		}
	}()
	return nil
}

// Stop stops the Clace Server
func (s *Server) Stop(ctx context.Context) error {
	s.Info().Msg("Stopping service")
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) AddApp(appEntry *utils.AppEntry) (*app.App, error) {
	appEntry.Id = utils.AppId(uuid.New().String())
	err := s.db.AddApp(appEntry)
	if err != nil {
		return nil, fmt.Errorf("error adding app: %s", err)
	}

	application, err := s.createApp(appEntry)
	if err != nil {
		return nil, err
	}

	s.apps.AddApp(application)
	s.Debug().Msgf("Created app %s %s", appEntry.Path, appEntry.Id)
	return application, nil
}

func (s *Server) createApp(appEntry *utils.AppEntry) (*app.App, error) {
	subLogger := s.With().Str("id", string(appEntry.Id)).Str("path", appEntry.Path).Logger()
	appLogger := utils.Logger{Logger: &subLogger}

	fs := app.NewAppFSImpl(appEntry.FsPath)
	application := app.NewApp(fs, &appLogger, appEntry)

	// Initialize the app
	s.Trace().Msg("Initializing app")
	if err := application.Initialize(); err != nil {
		return nil, fmt.Errorf("error initializing app: %w", err)
	}
	s.Info().Msg("Initialized app")
	return application, nil
}

func (s *Server) GetApp(pathDomain utils.AppPathDomain) (*app.App, error) {
	application, err := s.apps.GetApp(pathDomain)
	if err != nil {
		// App not found in cache, get from DB
		appEntry, err := s.db.GetApp(pathDomain)
		if err != nil {
			return nil, fmt.Errorf("error getting app: %w", err)
		}

		application, err = s.createApp(appEntry)
		if err != nil {
			return nil, err
		}
		s.apps.AddApp(application)
	}

	return application, nil
}

func (s *Server) DeleteApp(pathDomain utils.AppPathDomain) error {
	err := s.db.DeleteApp(pathDomain)
	if err != nil {
		return fmt.Errorf("error removing app: %s", err)
	}
	s.apps.DeleteApp(pathDomain)
	if err != nil {
		return fmt.Errorf("error deleting app: %s", err)
	}
	return nil
}

func (s *Server) serveApp(w http.ResponseWriter, r *http.Request, path, domain string) {
	app, err := s.GetApp(utils.CreateAppPathDomain(path, domain))
	if err != nil {
		s.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	app.ServeHTTP(w, r)
}
