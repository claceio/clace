// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/segmentio/ksuid"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/claceio/clace/plugins" // Register builtin plugins
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
	return application, nil
}

func (s *Server) createApp(appEntry *utils.AppEntry) (*app.App, error) {
	subLogger := s.With().Str("id", string(appEntry.Id)).Str("path", appEntry.Path).Logger()
	appLogger := utils.Logger{Logger: &subLogger}

	path := appEntry.FsPath
	if path == "" {
		path = appEntry.SourceUrl
	}

	sourceFS := app.NewAppFS(path, os.DirFS(path))
	appPath := fmt.Sprintf(os.ExpandEnv("$CL_HOME/run/app/%s"), appEntry.Id)
	workFS := app.NewAppFS(appPath, os.DirFS(appPath))
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
