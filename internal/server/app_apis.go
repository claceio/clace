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

func (s *Server) CreateApp(ctx context.Context, appPath string, approve, dryRun bool, appRequest utils.CreateAppRequest) (*utils.AppCreateResponse, error) {
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
		if !s.ssoAuth.ValidateAuthType(string(appRequest.AppAuthn)) {
			return nil, fmt.Errorf("invalid authentication type %s", appRequest.AppAuthn)
		}
		appEntry.Settings.AuthnType = appRequest.AppAuthn
	} else {
		appEntry.Settings.AuthnType = utils.AppAuthnDefault
	}

	appEntry.Metadata.VersionMetadata = utils.VersionMetadata{
		Version: 0,
	}

	auditResult, err := s.createApp(ctx, &appEntry, approve, dryRun, appRequest.GitBranch, appRequest.GitCommit, appRequest.GitAuthName)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	s.apps.ClearAllAppCache() // Clear the cache so that the new app is loaded next time
	return auditResult, nil
}

func (s *Server) createApp(ctx context.Context, appEntry *utils.AppEntry, approve, dryRun bool, branch, commit, gitAuth string) (*utils.AppCreateResponse, error) {
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
		appEntry.Id = utils.AppId(utils.ID_PREFIX_APP_PROD + id.String())
	}
	if err := s.db.CreateApp(ctx, tx, appEntry); err != nil {
		return nil, err
	}

	// Create the stage app entry if not dev
	stageAppEntry := *appEntry
	workEntry := appEntry
	if !appEntry.IsDev {
		stageAppEntry.Path = appEntry.Path + utils.STAGE_SUFFIX
		stageAppEntry.Id = utils.AppId(utils.ID_PREFIX_APP_STAGE + string(appEntry.Id)[len(utils.ID_PREFIX_APP_PROD):])
		stageAppEntry.MainApp = appEntry.Id
		stageAppEntry.Metadata.VersionMetadata.Version = 1
		if err := s.db.CreateApp(ctx, tx, &stageAppEntry); err != nil {
			return nil, err
		}
		workEntry = &stageAppEntry // Work on the stage app for prod apps, it will be promoted later
	}

	if isGit(workEntry.SourceUrl) {
		// Checkout the git repo locally and load into database
		if err := s.loadSourceFromGit(ctx, tx, workEntry, branch, commit, gitAuth); err != nil {
			return nil, err
		}
	} else if !workEntry.IsDev {
		// App is loaded from disk (not git) and not in dev mode, load files into DB
		if err := s.loadSourceFromDisk(ctx, tx, workEntry); err != nil {
			return nil, err
		}
	}

	// Create the in memory app object
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

	// Persist the settings
	if err := s.db.UpdateAppSettings(ctx, tx, workEntry); err != nil {
		return nil, err
	}

	results := []utils.ApproveResult{*auditResult}
	if !workEntry.IsDev {
		// Update the prod app metadata, promote from stage
		if err = s.promoteApp(ctx, tx, &stageAppEntry, appEntry); err != nil {
			return nil, err
		}

		prodApp, err := s.setupApp(appEntry, tx)
		if err != nil {
			return nil, err
		}

		prodAuditResult, err := s.auditApp(ctx, tx, prodApp, approve)
		if err != nil {
			return nil, fmt.Errorf("App %s audit failed: %s", appEntry.Id, err)
		}
		results = append(results, *prodAuditResult)
	}

	ret := &utils.AppCreateResponse{
		DryRun:         dryRun,
		ApproveResults: results,
	}

	if dryRun {
		return ret, nil
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// In memory app cache is not updated. The next GetApp call will load the new app
	return ret, nil
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
		sourceFS, err = util.NewSourceFs("", dbFs, false)
		if err != nil {
			return nil, err
		}
	} else {
		// Dev mode, use local disk as source
		var err error
		sourceFS, err = util.NewSourceFs(appEntry.SourceUrl,
			&util.DiskWriteFS{DiskReadFS: util.NewDiskReadFS(&appLogger, appEntry.SourceUrl)},
			appEntry.IsDev)
		if err != nil {
			return nil, err
		}
	}

	appPath := fmt.Sprintf(os.ExpandEnv("$CL_HOME/run/app/%s"), appEntry.Id)
	workFS := util.NewWorkFs(appPath, &util.DiskWriteFS{DiskReadFS: util.NewDiskReadFS(&appLogger, appPath)})
	application := app.NewApp(sourceFS, workFS, &appLogger, appEntry, &s.config.System, s.config.Plugins)

	return application, nil
}

func (s *Server) GetAppApi(ctx context.Context, appPath string) (*utils.AppGetResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	pathDomain, err := parseAppPath(appPath)
	if err != nil {
		return nil, err
	}

	appEntry, error := s.db.GetAppTx(ctx, tx, pathDomain)
	if error != nil {
		return nil, error
	}

	return &utils.AppGetResponse{
		AppEntry: *appEntry,
	}, nil
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

func (s *Server) DeleteApps(ctx context.Context, pathSpec string, dryRun bool) (*utils.AppDeleteResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, appInfo := range filteredApps {
		if err := s.db.DeleteApp(ctx, tx, appInfo.Id); err != nil {
			return nil, err
		}
	}

	ret := &utils.AppDeleteResponse{
		DryRun:  dryRun,
		AppInfo: filteredApps,
	}

	if dryRun {
		return ret, nil
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// Remove from in memory app cache
	for _, appInfo := range filteredApps {
		if err := s.apps.DeleteLinkedApps(appInfo.AppPathDomain); err != nil {
			return nil, fmt.Errorf("error deleting app: %s", err)
		}
	}

	return ret, nil
}

func (s *Server) authenticateAndServeApp(w http.ResponseWriter, r *http.Request, appInfo utils.AppPathDomain) {
	app, err := s.GetApp(appInfo, true)
	if err != nil {
		s.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	appAuth := app.Settings.AuthnType
	if app.Settings.AuthnType == "" || appAuth == utils.AppAuthnDefault {
		appAuth = utils.AppAuthnType(s.config.Security.AppDefaultAuthType)
	}

	if appAuth == "" { // no default auth type set, default to system admin user auth
		appAuth = utils.AppAuthnSystem
	}

	if app.Settings.AuthnType == utils.AppAuthnNone {
		// No authentication required
	} else if appAuth == utils.AppAuthnSystem {
		// Use system admin user for authentication
		authStatus := s.authHandler.authenticate(r.Header.Get("Authorization"))
		if !authStatus {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
	} else {
		authString := string(appAuth)
		if !s.ssoAuth.ValidateProviderName(authString) {
			http.Error(w, "Unsupported authentication provider: "+authString, http.StatusInternalServerError)
			return
		}

		// Redirect to the auth provider if not logged in
		loggedIn, err := s.ssoAuth.CheckAuth(w, r, authString, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		if !loggedIn {
			return // Already redirected to auth provider
		}
	}

	// Authentication successful, serve the app
	app.ServeHTTP(w, r)
}

func (s *Server) MatchApp(hostHeader, matchPath string) (utils.AppInfo, error) {
	s.Trace().Msgf("MatchApp %s %s", hostHeader, matchPath)
	apps, err := s.apps.GetAllApps()
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
				if appInfo.Path == "/" && strings.HasPrefix(matchPath, "/"+utils.STAGE_SUFFIX) {
					// Do not match /_cl_stage to /
					continue
				}
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

func (s *Server) auditApp(ctx context.Context, tx metadata.Transaction, app *app.App, approve bool) (*utils.ApproveResult, error) {
	auditResult, err := app.Audit()
	if err != nil {
		return nil, err
	}

	if approve {
		app.AppEntry.Metadata.Loads = auditResult.NewLoads
		app.AppEntry.Metadata.Permissions = auditResult.NewPermissions
		if err := s.db.UpdateAppMetadata(ctx, tx, app.AppEntry); err != nil {
			return nil, err
		}
		s.Info().Msgf("Approved app %s %s: %+v %+v", app.Path, app.Domain, auditResult.NewLoads, auditResult.NewPermissions)
	}

	return auditResult, nil
}

func (s *Server) CompleteTransaction(ctx context.Context, tx metadata.Transaction, entries []utils.AppPathDomain, dryRun bool) error {
	if dryRun {
		return nil
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Update the in memory cache
	if entries != nil {
		s.apps.DeleteApps(entries)
	}
	return nil
}

func (s *Server) getStageApp(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry) (*utils.AppEntry, error) {
	if appEntry.IsDev {
		return nil, fmt.Errorf("cannot get stage for dev app %s", appEntry.AppPathDomain())
	}
	if strings.HasSuffix(appEntry.Path, utils.STAGE_SUFFIX) {
		return nil, fmt.Errorf("app is already a stage app %s", appEntry.AppPathDomain())
	}

	stageAppPath := utils.AppPathDomain{Domain: appEntry.Domain, Path: appEntry.Path + utils.STAGE_SUFFIX}
	stageAppEntry, err := s.db.GetAppTx(ctx, tx, stageAppPath)
	if err != nil {
		return nil, err
	}

	return stageAppEntry, nil
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

func (s *Server) loadSourceFromGit(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry, branch, commit, gitAuth string) error {
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

	if branch == "" {
		// No branch specified, use the one from the previous version, otherwise default to main
		if appEntry.Metadata.VersionMetadata.GitBranch != "" {
			branch = appEntry.Metadata.VersionMetadata.GitBranch
		} else {
			branch = "main"
		}
	}

	if gitAuth == "" {
		// If not auth is specified, use the previous one used
		gitAuth = appEntry.Settings.GitAuthName
	}

	if commit == "" {
		// No commit id specified, checkout specified branch
		cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOptions.SingleBranch = true
		cloneOptions.Depth = 1
	}

	if gitAuth != "" {
		// Auth is specified, load the key
		authEntry, err := s.loadGitKey(gitAuth)
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
	if commit != "" {
		s.Info().Msgf("Checking out commit %s", commit)
		options.Hash = plumbing.NewHash(commit)
	} else {
		options.Branch = plumbing.NewBranchReferenceName(branch)
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
	newCommit, err := gitRepo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	// Update the git info into the appEntry, the caller needs to persist it into the app metadata
	// This function will persist it into the app_version metadata
	appEntry.Metadata.VersionMetadata.GitCommit = newCommit.Hash.String()
	appEntry.Metadata.VersionMetadata.GitMessage = newCommit.Message
	if commit != "" {
		appEntry.Metadata.VersionMetadata.GitBranch = ""
	} else {
		appEntry.Metadata.VersionMetadata.GitBranch = branch
	}
	appEntry.Settings.GitAuthName = gitAuth

	s.Info().Msgf("Cloned git repo %s %s:%s folder %s to %s, commit %s: %s", repo, appEntry.Metadata.VersionMetadata.GitBranch, appEntry.Metadata.VersionMetadata.GitCommit, folder, tmpDir, newCommit.Hash.String(), newCommit.Message)
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
	if err := fileStore.AddAppVersionDisk(ctx, tx, appEntry.Metadata, checkoutFolder); err != nil {
		return err
	}

	return nil
}

func (s *Server) loadSourceFromDisk(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry) error {
	s.Info().Msgf("Loading app sources from %s", appEntry.SourceUrl)
	appEntry.Metadata.VersionMetadata.GitBranch = ""
	appEntry.Metadata.VersionMetadata.GitCommit = ""
	appEntry.Settings.GitAuthName = ""
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
	if err := fileStore.AddAppVersionDisk(ctx, tx, appEntry.Metadata, appEntry.SourceUrl); err != nil {
		return err
	}
	return nil
}

func (s *Server) FilterApps(appPathSpec string, includeInternal bool) ([]utils.AppInfo, error) {
	apps, err := s.db.GetAllApps(includeInternal)
	if err != nil {
		return nil, err
	}

	linkedApps := make(map[string][]utils.AppInfo)
	var mainApps []utils.AppInfo
	if includeInternal {
		mainApps = make([]utils.AppInfo, 0, len(apps))

		for _, appInfo := range apps {
			if appInfo.MainApp != "" {
				linkedApps[string(appInfo.MainApp)] = append(linkedApps[string(appInfo.MainApp)], appInfo)
			} else {
				mainApps = append(mainApps, appInfo)
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

	// Include staging and preview apps for prod apps
	result := make([]utils.AppInfo, 0, 2*len(filteredApps))
	for _, appInfo := range filteredApps {
		result = append(result, appInfo)
		result = append(result, linkedApps[string(appInfo.Id)]...)
	}

	return result, nil
}

func (s *Server) GetApps(ctx context.Context, pathSpec string, internal bool) ([]utils.AppResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

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

		stagedChanges := false
		if strings.HasPrefix(string(app.Id), utils.ID_PREFIX_APP_PROD) {
			stageApp, err := s.getStageApp(ctx, tx, retApp.AppEntry)
			if err != nil {
				return nil, err
			}
			if stageApp.Metadata.VersionMetadata.Version != retApp.AppEntry.Metadata.VersionMetadata.Version {
				// staging app is at different version than prod app
				stagedChanges = true
			}
		}
		ret = append(ret, utils.AppResponse{AppEntry: *retApp.AppEntry, StagedChanges: stagedChanges})
	}
	return ret, nil
}

func (s *Server) PreviewApp(ctx context.Context, mainAppPath, commitId string, approve, dryRun bool) (*utils.AppPreviewResponse, error) {
	mainAppPathDomain, err := parseAppPath(mainAppPath)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	mainAppEntry, err := s.db.GetAppTx(ctx, tx, mainAppPathDomain)
	if err != nil {
		return nil, err
	}

	if !isGit(mainAppEntry.SourceUrl) {
		return nil, fmt.Errorf("cannot preview app %s, source is not git", mainAppPath)
	}

	previewAppEntry := *mainAppEntry
	previewAppEntry.Path = mainAppEntry.Path + utils.PREVIEW_SUFFIX + "_" + commitId
	previewAppEntry.MainApp = mainAppEntry.Id
	previewAppEntry.Id = utils.AppId(utils.ID_PREFIX_APP_PREVIEW + string(mainAppEntry.Id)[len(utils.ID_PREFIX_APP_PROD):])

	// Check if it already exists
	if _, err = s.db.GetAppTx(ctx, tx, previewAppEntry.AppPathDomain()); err == nil {
		return nil, fmt.Errorf("preview app %s already exists", previewAppEntry.AppPathDomain())
	}

	previewAppEntry.Metadata.VersionMetadata = utils.VersionMetadata{
		Version: 0,
	}

	if err := s.db.CreateApp(ctx, tx, &previewAppEntry); err != nil {
		return nil, err
	}

	// Checkout the git repo locally and load into database
	if err := s.loadSourceFromGit(ctx, tx, &previewAppEntry, "", commitId, previewAppEntry.Settings.GitAuthName); err != nil {
		return nil, err
	}

	// Create the in memory app object
	application, err := s.setupApp(&previewAppEntry, tx)
	if err != nil {
		return nil, err
	}

	s.Debug().Msgf("Created preview app %s %s", previewAppEntry.Path, previewAppEntry.Id)
	auditResult, err := s.auditApp(ctx, tx, application, approve)
	if err != nil {
		return nil, fmt.Errorf("app %s audit failed: %s", previewAppEntry.Id, err)
	}

	// Persist the metadata so that any git info is saved
	if err := s.db.UpdateAppMetadata(ctx, tx, &previewAppEntry); err != nil {
		return nil, err
	}

	// Persist the settings
	if err := s.db.UpdateAppSettings(ctx, tx, &previewAppEntry); err != nil {
		return nil, err
	}

	ret := &utils.AppPreviewResponse{
		DryRun:        dryRun,
		ApproveResult: *auditResult,
		Success:       true,
	}

	if auditResult.NeedsApproval && !approve {
		ret.Success = false // Needs approval but not approved, do not create the preview app
		return ret, nil
	}

	if dryRun {
		return ret, nil
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	s.apps.ClearAllAppCache() // Clear the cache so that the new app is loaded next time
	return ret, nil
}
