// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/segmentio/ksuid"
)

func parseAppPath(inp string) (utils.AppPathDomain, error) {
	domain := ""
	path := ""
	if strings.Contains(inp, ":") {
		split := strings.Split(inp, ":")
		if len(split) != 2 {
			return utils.AppPathDomain{}, fmt.Errorf("invalid app path %s, expected one \":\"", inp)
		}
		domain = split[0]
		path = split[1]
	} else {
		path = inp
	}

	path = normalizePath(path)
	return utils.AppPathDomain{Domain: domain, Path: path}, nil
}

func normalizePath(inp string) string {
	if len(inp) == 0 || inp[0] != '/' {
		inp = "/" + inp
	}
	if len(inp) > 1 {
		inp = strings.TrimRight(inp, "/")
	}
	return inp
}

func (s *Server) CreateApp(ctx context.Context, appPath string, approve bool, appRequest utils.CreateAppRequest) (*utils.AuditResult, error) {
	appPathDomain, err := parseAppPath(appPath)
	if err != nil {
		return nil, err
	}
	if err := validatePathForCreate(appPathDomain.Path); err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	matchedApp, err := s.CheckAppValid(appPathDomain.Domain, appPathDomain.Path)
	if err != nil {
		return nil, utils.CreateRequestError(
			fmt.Sprintf("error matching app: %s", err), http.StatusInternalServerError)
	}
	if matchedApp != "" {
		return nil, utils.CreateRequestError(
			fmt.Sprintf("App already exists at %s", matchedApp), http.StatusBadRequest)
	}

	var appEntry utils.AppEntry
	appEntry.Path = appPathDomain.Path
	appEntry.Domain = appPathDomain.Domain
	appEntry.SourceUrl = appRequest.SourceUrl
	appEntry.IsDev = appRequest.IsDev
	if appRequest.AppAuthn != "" {
		authType := utils.AppAuthnType(strings.ToLower(string(appRequest.AppAuthn)))
		if authType != utils.AppAuthnDefault && authType != utils.AppAuthnNone {
			return nil, utils.CreateRequestError("Invalid auth type: "+string(authType), http.StatusBadRequest)
		}
		appEntry.Settings.AuthnType = utils.AppAuthnType(strings.ToLower(string(appRequest.AppAuthn)))
	} else {
		appEntry.Settings.AuthnType = utils.AppAuthnDefault
	}

	appEntry.Metadata.VersionMetadata = utils.VersionMetadata{
		Version:            1,
		GitBranch:          appRequest.GitBranch,
		GitCommitRequested: appRequest.GitCommit,
		GitAuthName:        appRequest.GitAuthName,
	}

	auditResult, err := s.createApp(ctx, &appEntry, approve)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	return auditResult, nil
}

func (s *Server) createApp(ctx context.Context, appEntry *utils.AppEntry, approve bool) (*utils.AuditResult, error) {
	if isGit(appEntry.SourceUrl) {
		if appEntry.IsDev {
			return nil, fmt.Errorf("cannot create dev mode app from git source. For dev mode, manually checkout the git repo and create app from the local path")
		}
	} else {
		// Make sure the source path is absolute
		var err error
		appEntry.SourceUrl, err = filepath.Abs(appEntry.SourceUrl)
		if err != nil {
			return nil, err
		}
	}

	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if appEntry.IsDev {
		appEntry.Id = utils.AppId(utils.ID_PREFIX_APP_DEV + id.String())
	} else {
		appEntry.Id = utils.AppId(utils.ID_PREFIX_APP_PRD + id.String())
	}
	if err := s.db.CreateApp(ctx, tx, appEntry); err != nil {
		return nil, err
	}

	// Create the stage app entry if not dev
	stageAppEntry := *appEntry
	workEntry := appEntry
	if !appEntry.IsDev {
		stageAppEntry.Path = appEntry.Path + utils.STAGE_SUFFIX
		stageAppEntry.Id = utils.AppId(utils.ID_PREFIX_APP_STG + string(appEntry.Id)[len(utils.ID_PREFIX_APP_PRD):])
		stageAppEntry.MainApp = appEntry.Id
		if err := s.db.CreateApp(ctx, tx, &stageAppEntry); err != nil {
			return nil, err
		}
		workEntry = &stageAppEntry // Work on the stage app for prod apps, it will be promoted later
	}

	if isGit(appEntry.SourceUrl) {
		// Checkout the git repo locally and load into database
		if err := s.loadSourceFromGit(ctx, tx, workEntry); err != nil {
			return nil, err
		}
	} else if !appEntry.IsDev {
		// App is loaded from disk (not git) and not in dev mode, load files into DB
		if err := s.loadSourceFromDisk(ctx, tx, workEntry); err != nil {
			return nil, err
		}
	}

	application, err := s.setupApp(workEntry, tx)
	if err != nil {
		return nil, err
	}

	s.Debug().Msgf("Created app %s %s", workEntry.Path, workEntry.Id)
	auditResult, err := s.auditApp(ctx, tx, application, approve)
	if err != nil {
		return nil, fmt.Errorf("App %s audit failed: %s", workEntry.Id, err)
	}

	// Persist the metadata so that any git info is saved
	if err := s.db.UpdateAppMetadata(ctx, tx, workEntry); err != nil {
		return nil, err
	}

	if !appEntry.IsDev {
		// Update the prod app metadata, promote from stage
		_, err = s.promoteApp(ctx, tx, &stageAppEntry, appEntry)
		if err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return auditResult, nil
}

func (s *Server) setupApp(appEntry *utils.AppEntry, tx metadata.Transaction) (*app.App, error) {
	subLogger := s.With().Str("id", string(appEntry.Id)).Str("path", appEntry.Path).Logger()
	appLogger := utils.Logger{Logger: &subLogger}
	var sourceFS *util.SourceFs
	if !appEntry.IsDev {
		// Prod mode, use DB as source
		fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.VersionMetadata.Version, s.db, tx)
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

func (s *Server) GetAppEntry(ctx context.Context, tx metadata.Transaction, pathDomain utils.AppPathDomain) (*utils.AppEntry, error) {
	return s.db.GetAppTx(ctx, tx, pathDomain)
}

func (s *Server) GetApp(pathDomain utils.AppPathDomain, init bool) (*app.App, error) {
	application, err := s.apps.GetApp(pathDomain)
	if err != nil {
		// App not found in cache, get from DB
		appEntry, err := s.db.GetApp(pathDomain)
		if err != nil {
			return nil, err
		}

		application, err = s.setupApp(appEntry, metadata.Transaction{})
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

func (s *Server) DeleteApps(ctx context.Context, pathSpec string) error {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, appInfo := range filteredApps {
		if err := s.db.DeleteApp(ctx, tx, appInfo.Id); err != nil {
			return err
		}
		if err := s.apps.DeleteApp(appInfo.AppPathDomain); err != nil {
			return fmt.Errorf("error deleting app: %s", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Server) serveApp(w http.ResponseWriter, r *http.Request, appInfo utils.AppPathDomain) {
	app, err := s.GetApp(appInfo, true)
	if err != nil {
		s.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if app.Settings.AuthnType == utils.AppAuthnDefault || app.Settings.AuthnType == "" {
		// The default authn type is to use the admin user account
		authStatus := s.authHandler.authenticate(r.Header.Get("Authorization"))
		if !authStatus {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
	} else if app.Settings.AuthnType == utils.AppAuthnNone {
		// No authentication required
	} else {
		http.Error(w, "Unsupported authn type: "+string(app.Settings.AuthnType), http.StatusInternalServerError)
		return
	}

	app.ServeHTTP(w, r)
}

func (s *Server) MatchApp(hostHeader, matchPath string) (utils.AppInfo, error) {
	s.Trace().Msgf("MatchApp %s %s", hostHeader, matchPath)
	apps, err := s.db.GetAllApps(true)
	if err != nil {
		return utils.AppInfo{}, err
	}
	matchPath = normalizePath(matchPath)

	// Find unique domains
	domainMap := map[string]bool{}
	for _, appInfo := range apps {
		if !domainMap[appInfo.Domain] {
			domainMap[appInfo.Domain] = true
			// TODO : cache domain list
		}
	}

	// Check if host header matches a known domain
	checkDomain := false
	if hostHeader != "" && domainMap[hostHeader] {
		s.Trace().Msgf("Matched domain %s", hostHeader)
		checkDomain = true
	}

	for _, appInfo := range apps {
		if checkDomain && appInfo.Domain != hostHeader {
			// Request uses known domain, but app is not for this domain
			continue
		}

		if !checkDomain && appInfo.Domain != "" {
			// Request does not use known domain, but app is for a domain
			continue
		}

		if strings.HasPrefix(matchPath, appInfo.Path) {
			if len(appInfo.Path) == 1 || len(appInfo.Path) == len(matchPath) || matchPath[len(appInfo.Path)] == '/' {
				s.Debug().Msgf("Matched app %s for path %s", appInfo, matchPath)
				return appInfo, nil
			}
		}
	}

	return utils.AppInfo{}, errors.New("no matching app found")
}

func (s *Server) CheckAppValid(domain, matchPath string) (string, error) {
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
				matchedApp = utils.AppPathDomain{Domain: domain, Path: path}.String()
				s.Debug().Msgf("Matched app %s for path %s", matchedApp, matchPath)
				break
			}
		}

		// If /test/other is in use, do not allow /test
		if strings.HasPrefix(path, matchPath) {
			if len(matchPath) == 1 || len(path) == len(matchPath) || path[len(matchPath)] == '/' {
				matchedApp = utils.AppPathDomain{Domain: domain, Path: path}.String()
				s.Debug().Msgf("Matched app %s for path %s", matchedApp, matchPath)
				break
			}
		}
	}

	return matchedApp, nil
}

func (s *Server) auditApp(ctx context.Context, tx metadata.Transaction, app *app.App, approve bool) (*utils.AuditResult, error) {
	auditResult, err := app.Audit()
	if err != nil {
		return nil, err
	}

	if approve {
		if err != nil {
			return nil, err
		}

		app.AppEntry.Metadata.Loads = auditResult.NewLoads
		app.AppEntry.Metadata.Permissions = auditResult.NewPermissions
		if err := s.db.UpdateAppMetadata(ctx, tx, app.AppEntry); err != nil {
			return nil, err
		}
		s.Info().Msgf("Approved app %s %s: %+v %+v", app.Path, app.Domain, auditResult.NewLoads, auditResult.NewPermissions)
	}

	return auditResult, nil
}

func (s *Server) AuditApps(ctx context.Context, pathSpec string, approve bool) ([]utils.AuditResult, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ret, err := s.AuditAppsTx(ctx, tx, pathSpec, approve)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return ret, nil
}

func (s *Server) AuditAppsTx(ctx context.Context, tx metadata.Transaction, pathSpec string, approve bool) ([]utils.AuditResult, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	results := make([]utils.AuditResult, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		app, err := s.GetApp(appInfo.AppPathDomain, false)
		if err != nil {
			return nil, err
		}
		result, err := s.auditApp(ctx, tx, app, approve)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}

	return results, nil
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

func (s *Server) loadSourceFromGit(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry) error {
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

	if appEntry.Metadata.VersionMetadata.GitCommitRequested == "" {
		// No commit id specified, checkout specified branch
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(appEntry.Metadata.VersionMetadata.GitBranch)
		cloneOptions.SingleBranch = true
		cloneOptions.Depth = 1
	}

	if appEntry.Metadata.VersionMetadata.GitAuthName != "" {
		// Auth is specified, load the key
		authEntry, err := s.loadGitKey(appEntry.Metadata.VersionMetadata.GitAuthName)
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
	if appEntry.Metadata.VersionMetadata.GitCommitRequested != "" {
		s.Info().Msgf("Checking out commit %s", appEntry.Metadata.VersionMetadata.GitCommitRequested)
		options.Hash = plumbing.NewHash(appEntry.Metadata.VersionMetadata.GitCommitRequested)
	} else {
		options.Branch = plumbing.NewBranchReferenceName(appEntry.Metadata.VersionMetadata.GitBranch)
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
	appEntry.Metadata.VersionMetadata.GitCommit = commit.Hash.String()
	appEntry.Metadata.VersionMetadata.GitMessage = commit.Message
	s.Info().Msgf("Cloned git repo %s %s:%s folder %s to %s, commit %s: %s", repo, appEntry.Metadata.VersionMetadata.GitBranch, appEntry.Metadata.VersionMetadata.GitCommit, folder, tmpDir, commit.Hash.String(), commit.Message)
	checkoutFolder := tmpDir
	if folder != "" {
		checkoutFolder = path.Join(tmpDir, folder)
	}

	s.Info().Msgf("Loading app sources from %s", checkoutFolder)
	// Walk the local directory and add all files to the database
	fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.VersionMetadata.Version, s.db, tx)
	highestVersion, err := fileStore.GetHighestVersion(ctx, tx, appEntry.Id)
	if err != nil {
		return err
	}
	prevVersion := appEntry.Metadata.VersionMetadata.Version
	if highestVersion == 0 {
		prevVersion = 0 // No previous version, start at 0
	}
	appEntry.Metadata.VersionMetadata.PreviousVersion = prevVersion
	appEntry.Metadata.VersionMetadata.Version = highestVersion + 1
	if err := fileStore.AddAppVersion(ctx, tx, appEntry.Metadata.VersionMetadata, checkoutFolder); err != nil {
		return err
	}

	return nil
}

func (s *Server) loadSourceFromDisk(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry) error {
	s.Info().Msgf("Loading app sources from %s", appEntry.SourceUrl)
	appEntry.Metadata.VersionMetadata.GitBranch = ""
	appEntry.Metadata.VersionMetadata.GitCommit = ""
	appEntry.Metadata.VersionMetadata.GitCommitRequested = ""
	appEntry.Metadata.VersionMetadata.GitAuthName = ""
	appEntry.Metadata.VersionMetadata.GitSha = ""
	appEntry.Metadata.VersionMetadata.GitMessage = ""

	fileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.VersionMetadata.Version, s.db, tx)
	highestVersion, err := fileStore.GetHighestVersion(ctx, tx, appEntry.Id)
	if err != nil {
		return fmt.Errorf("error getting highest version: %w", err)
	}
	prevVersion := appEntry.Metadata.VersionMetadata.Version
	if highestVersion == 0 {
		prevVersion = 0 // No previous version, set to 0
	}

	appEntry.Metadata.VersionMetadata.PreviousVersion = prevVersion
	appEntry.Metadata.VersionMetadata.Version = highestVersion + 1
	// Walk the local directory and add all files to the database
	if err := fileStore.AddAppVersion(ctx, tx, appEntry.Metadata.VersionMetadata, appEntry.SourceUrl); err != nil {
		return err
	}
	return nil
}

func (s *Server) FilterApps(appPathSpec string, includeInternal bool) ([]utils.AppInfo, error) {
	apps, err := s.db.GetAllApps(includeInternal)
	if err != nil {
		return nil, err
	}

	linkedApps := make(map[string]utils.AppInfo)
	var mainApps []utils.AppInfo
	if includeInternal {
		mainApps = make([]utils.AppInfo, 0, len(apps))

		for _, appInfo := range apps {
			if strings.HasPrefix(string(appInfo.Id), utils.ID_PREFIX_APP_PRD) || strings.HasPrefix(string(appInfo.Id), utils.ID_PREFIX_APP_DEV) {
				mainApps = append(mainApps, appInfo)
			} else {
				linkedApps[appInfo.String()] = appInfo
			}
		}
	} else {
		mainApps = apps
	}
	// Filter based on path spec. This is done on the main apps path only.
	filteredApps, err := ParseSpecFromInfo(appPathSpec, mainApps)
	if err != nil {
		return nil, err
	}

	if !includeInternal {
		return filteredApps, nil
	}

	// Include staging apps for prod apps
	result := make([]utils.AppInfo, 0, 2*len(filteredApps))
	for _, appInfo := range filteredApps {
		result = append(result, appInfo)
		if strings.HasPrefix(string(appInfo.Id), utils.ID_PREFIX_APP_PRD) {
			stageAppPath := utils.AppPathDomain{Domain: appInfo.Domain, Path: appInfo.Path + utils.STAGE_SUFFIX}

			if linkedApp, ok := linkedApps[stageAppPath.String()]; ok {
				result = append(result, linkedApp)
			}
		}
	}

	return result, nil
}

func (s *Server) GetApps(ctx context.Context, pathSpec string, internal bool) ([]utils.AppResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, internal)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	ret := make([]utils.AppResponse, 0, len(filteredApps))
	for _, app := range filteredApps {
		retApp, err := s.GetApp(app.AppPathDomain, false)
		if err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusInternalServerError)
		}
		ret = append(ret, utils.AppResponse{AppEntry: *retApp.AppEntry})
	}
	return ret, nil
}
