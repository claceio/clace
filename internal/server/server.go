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
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi/middleware"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
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
	udsServer   *http.Server
	handler     *Handler
	apps        *AppStore
	authHandler *AdminBasicAuth
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

	accessLogger := utils.RollingFileLogger(&config.Log, "access.log")
	customLogger := log.New(accessLogger, "", log.LstdFlags)
	middleware.DefaultLogger = middleware.RequestLogger(
		&middleware.DefaultLogFormatter{Logger: customLogger, NoColor: true})

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

func (s *Server) CreateApp(appEntry *utils.AppEntry, approve bool) (*utils.AuditResult, error) {
	if isGit(appEntry.SourceUrl) && appEntry.IsDev {
		return nil, fmt.Errorf("cannot create dev mode app from git source. For dev mode, manually checkout the git repo and create app from the local path")
	}

	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}
	appEntry.Id = utils.AppId("app" + id.String())
	err = s.db.CreateApp(appEntry)
	if err != nil {
		return nil, err
	}
	cleanupApp := true
	defer func() {
		if cleanupApp {
			s.Info().Msgf("Deleting app %s %s", appEntry.Path, appEntry.Id)
			s.db.DeleteApp(utils.AppPathDomain{Path: appEntry.Path, Domain: appEntry.Domain})
		}
	}()

	if isGit(appEntry.SourceUrl) {
		// Checkout the git repo locally and load into database
		if err := s.loadSourceFromGit(appEntry); err != nil {
			return nil, err
		}
	} else if !appEntry.IsDev {
		// App is loaded from disk (not git) and not in dev mode, load files into DB
		if err := s.loadSourceFromDisk(appEntry); err != nil {
			return nil, err
		}
	}

	application, err := s.setupApp(appEntry)
	if err != nil {
		return nil, err
	}

	s.apps.AddApp(application)
	s.Debug().Msgf("Created app %s %s", appEntry.Path, appEntry.Id)

	auditResult, err := application.Audit()
	if err != nil {
		return nil, fmt.Errorf("App %s audit failed: %s", appEntry.Id, err)
	}

	if approve {
		appEntry.Loads = auditResult.NewLoads
		appEntry.Permissions = auditResult.NewPermissions
		s.db.UpdateAppPermissions(appEntry)
		s.Info().Msgf("Approved app %s %s: %+v %+v", appEntry.Domain, appEntry.Path, auditResult.NewLoads, auditResult.NewPermissions)
	}

	// Persist the metadata so that any git info is saved
	if err := s.db.UpdateAppMetadata(appEntry); err != nil {
		return nil, err
	}
	cleanupApp = false
	return auditResult, nil
}

func (s *Server) setupApp(appEntry *utils.AppEntry) (*app.App, error) {
	subLogger := s.With().Str("id", string(appEntry.Id)).Str("path", appEntry.Path).Logger()
	appLogger := utils.Logger{Logger: &subLogger}
	var sourceFS *util.SourceFs
	if !appEntry.IsDev {
		// Prod mode, use DB as source
		fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.Version, s.db)
		dbFs, err := metadata.NewDbFs(s.Logger, fileStore)
		if err != nil {
			return nil, err
		}
		sourceFS = util.NewSourceFs("", dbFs, false)
	} else {
		// Dev mode, use local disk as source
		sourceFS = util.NewSourceFs(appEntry.SourceUrl,
			&util.DiskWriteFS{DiskReadFS: util.NewDiskReadFS(&appLogger, appEntry.SourceUrl)},
			appEntry.IsDev)
	}

	appPath := fmt.Sprintf(os.ExpandEnv("$CL_HOME/run/app/%s"), appEntry.Id)
	workFS := util.NewWorkFs(appPath, &util.DiskWriteFS{DiskReadFS: util.NewDiskReadFS(&appLogger, appPath)})
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

		application, err = s.setupApp(appEntry)
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

	if app.Rules.AuthnType == utils.AppAuthnDefault || app.Rules.AuthnType == "" {
		// The default authn type is to use the admin user account
		authStatus := s.authHandler.authenticate(r.Header.Get("Authorization"))
		if !authStatus {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
	} else if app.Rules.AuthnType == utils.AppAuthnNone {
		// No authentication required
	} else {
		http.Error(w, "Unsupported authn type: "+string(app.Rules.AuthnType), http.StatusInternalServerError)
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
			// Request uses known domain, but app is not for this domain
			continue
		}

		if !checkDomain && entry.Domain != "" {
			// Request does not use known domain, but app is for a domain
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
		if err := s.db.UpdateAppPermissions(app.AppEntry); err != nil {
			return nil, err
		}
		s.Info().Msgf("Approved app %s %s: %+v %+v", pathDomain.Path, pathDomain.Domain, auditResult.NewLoads, auditResult.NewPermissions)
	}

	return auditResult, nil
}

func parseGithubUrl(sourceUrl string) (repo string, folder string, err error) {
	if !strings.HasSuffix(sourceUrl, "/") {
		sourceUrl = sourceUrl + "/"
	}

	if strings.HasPrefix(sourceUrl, "git@github.com:") {
		// Using git url format
		split := strings.SplitN(sourceUrl, "/", 3)
		if len(split) != 3 {
			return "", "", fmt.Errorf("invalid github url: %s, expected git@github.com:orgName/repoName or git@github.com:orgName/repoName/folder", sourceUrl)
		}

		return fmt.Sprintf("%s/%s", split[0], split[1]), split[2], nil
	}

	if strings.HasPrefix(sourceUrl, "github.com") {
		sourceUrl = "https://" + sourceUrl
	}

	url, err := url.Parse(sourceUrl)
	if err != nil {
		return "", "", err
	}

	split := strings.SplitN(url.Path, "/", 4)
	if len(split) == 4 {
		return fmt.Sprintf("%s://%s/%s/%s", url.Scheme, url.Host, split[1], split[2]), split[3], nil
	}

	return "", "", fmt.Errorf("invalid github url: %s, expected github.com/orgName/repoName or github.com/orgName/repoName/folder", sourceUrl)
}

type gitAuthEntry struct {
	user     string
	key      []byte
	password string
}

// loadGitKey gets the git key from the config and loads the key from disk
func (s *Server) loadGitKey(gitAuth string) (*gitAuthEntry, error) {
	authEntry, ok := s.config.GitAuth[gitAuth]
	if !ok {
		return nil, fmt.Errorf("git auth entry %s not found in server config", gitAuth)
	}

	gitKey, err := os.ReadFile(authEntry.KeyFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading git key %s: %w", authEntry.KeyFilePath, err)
	}

	user := "git" // https://github.com/src-d/go-git/issues/637, default to "git"
	if authEntry.UserID != "" {
		user = authEntry.UserID
	}

	return &gitAuthEntry{
		user:     user,
		key:      gitKey,
		password: authEntry.Password,
	}, nil
}

func (s *Server) loadSourceFromGit(appEntry *utils.AppEntry) error {
	// Figure on which repo to clone
	repo, folder, err := parseGithubUrl(appEntry.SourceUrl)
	if err != nil {
		return err
	}

	// Create temp directory on disk
	tmpDir, err := os.MkdirTemp("", "clace_git_"+string(appEntry.Id)+"_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	s.Info().Msgf("Cloning git repo %s to %s", repo, tmpDir)

	cloneOptions := git.CloneOptions{
		URL: repo,
	}

	if appEntry.Metadata.GitCommit == "" {
		// No commit id specified, checkout specified branch
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(appEntry.Metadata.GitBranch)
		cloneOptions.SingleBranch = true
		cloneOptions.Depth = 1
	}

	if appEntry.Metadata.GitAuthName != "" {
		// Auth is specified, load the key
		authEntry, err := s.loadGitKey(appEntry.Metadata.GitAuthName)
		if err != nil {
			return err
		}
		s.Info().Msgf("Using git auth %s", authEntry.user)
		auth, err := ssh.NewPublicKeys(authEntry.user, authEntry.key, authEntry.password)
		if err != nil {
			return err
		}
		cloneOptions.Auth = auth
	}

	// Configure the repo to Clone
	gitRepo, err := git.PlainClone(tmpDir, false, &cloneOptions)
	if err != nil {
		return err
	}

	w, err := gitRepo.Worktree()
	if err != nil {
		return err
	}
	// Checkout specified hash
	options := git.CheckoutOptions{}
	if appEntry.Metadata.GitCommit != "" {
		s.Info().Msgf("Checking out commit %s", appEntry.Metadata.GitCommit)
		options.Hash = plumbing.NewHash(appEntry.Metadata.GitCommit)
	} else {
		options.Branch = plumbing.NewBranchReferenceName(appEntry.Metadata.GitBranch)
	}

	/* Sparse checkout seems to not be reliable with go-git
	if folder != "" {
		options.SparseCheckoutDirectories = []string{folder}
	}
	*/
	if err := w.Checkout(&options); err != nil {
		return err
	}

	ref, err := gitRepo.Head()
	if err != nil {
		return err
	}
	commit, err := gitRepo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	// Update the git info into the appEntry, the caller needs to persist it
	appEntry.Metadata.GitCommit = commit.Hash.String()
	s.Info().Msgf("Cloned git repo %s %s:%s folder %s to %s, commit %s: %s", repo, appEntry.Metadata.GitBranch, appEntry.Metadata.GitCommit, folder, tmpDir, commit.Hash.String(), commit.Message)
	checkoutFolder := tmpDir
	if folder != "" {
		checkoutFolder = path.Join(tmpDir, folder)
	}

	s.Info().Msgf("Loading app sources from %s", checkoutFolder)
	// Walk the local directory and add all files to the database
	fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.Version, s.db)
	if err := fileStore.AddAppVersion(appEntry.Metadata.Version, commit.Hash.String(), appEntry.Metadata.GitBranch, commit.Message, checkoutFolder); err != nil {
		return err
	}

	return nil
}

func (s *Server) loadSourceFromDisk(appEntry *utils.AppEntry) error {
	appEntry.Metadata.GitBranch = ""
	appEntry.Metadata.GitCommit = ""
	fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.Version, s.db)
	// Walk the local directory and add all files to the database
	if err := fileStore.AddAppVersion(appEntry.Metadata.Version, "", "", "", appEntry.SourceUrl); err != nil {
		return err
	}
	return nil
}
