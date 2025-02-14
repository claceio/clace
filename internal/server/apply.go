// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/BurntSushi/toml"
	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const (
	APP = "app"
)

func (s *Server) loadApplyInfo(fileName string, data []byte) ([]*types.CreateAppRequest, error) {
	appDefs := make([]*starlarkstruct.Struct, 0)

	createAppBuiltin := func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path, source starlark.String
		var dev starlark.Bool
		var params *starlark.Dict = starlark.NewDict(0)
		var auth, gitAuth, gitBranch, gitCommit, appSpec starlark.String
		var appConfig = starlark.NewDict(0)
		var containerOpts = starlark.NewDict(0)
		var containerArgs = starlark.NewDict(0)
		var containerVols = &starlark.List{}

		if err := starlark.UnpackArgs(APP, args, kwargs, "path", &path, "source", &source, "dev?", &dev,
			"auth?", &auth, "git_auth?", &gitAuth, "git_branch?", &gitBranch, "git_commit?", &gitCommit,
			"params?", &params, "spec?", &appSpec, "app_config", &appConfig,
			"container_opts?", &containerOpts, "container_args?", &containerArgs, "container_vols?", &containerVols,
		); err != nil {
			return nil, err
		}

		fields := starlark.StringDict{
			"path":           path,
			"source":         source,
			"dev":            dev,
			"auth":           auth,
			"git_auth":       gitAuth,
			"git_branch":     gitBranch,
			"git_commit":     gitCommit,
			"params":         params,
			"spec":           appSpec,
			"app_config":     appConfig,
			"container_opts": containerOpts,
			"container_args": containerArgs,
			"container_vols": containerVols,
		}

		appStruct := starlarkstruct.FromStringDict(starlark.String(APP), fields)
		appDefs = append(appDefs, appStruct)
		return appStruct, nil
	}

	builtins := starlark.StringDict{
		APP: starlark.NewBuiltin(APP, createAppBuiltin),
	}

	thread := &starlark.Thread{
		Name:  fileName,
		Print: func(_ *starlark.Thread, msg string) { s.Info().Msg(msg) },
	}

	options := syntax.FileOptions{}
	_, err := starlark.ExecFileOptions(&options, thread, fileName, data, builtins)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			s.Error().Err(evalErr).Msgf("Error loading app definitions: %s", evalErr.Backtrace())
		}
		return nil, fmt.Errorf("error loading app definitions: %w", err)
	}

	ret := make([]*types.CreateAppRequest, 0, len(appDefs))
	for _, appDef := range appDefs {
		applyConfig, err := appDefToApplyInfo(appDef)
		if err != nil {
			return nil, err
		}
		ret = append(ret, applyConfig)
	}

	return ret, nil
}

func appDefToApplyInfo(appDef *starlarkstruct.Struct) (*types.CreateAppRequest, error) {
	path, err := apptype.GetStringAttr(appDef, "path")
	if err != nil {
		return nil, err
	}

	source, err := apptype.GetStringAttr(appDef, "source")
	if err != nil {
		return nil, err
	}

	dev, err := apptype.GetOptionalBoolAttr(appDef, "dev")
	if err != nil {
		return nil, err
	}

	auth, err := apptype.GetStringAttr(appDef, "auth")
	if err != nil {
		return nil, err
	}

	gitAuth, err := apptype.GetStringAttr(appDef, "git_auth")
	if err != nil {
		return nil, err
	}
	gitBranch, err := apptype.GetStringAttr(appDef, "git_branch")
	if err != nil {
		return nil, err
	}
	gitCommit, err := apptype.GetStringAttr(appDef, "git_commit")
	if err != nil {
		return nil, err
	}
	params, err := apptype.GetDictAttr(appDef, "params", true)
	if err != nil {
		return nil, err
	}
	spec, err := apptype.GetStringAttr(appDef, "spec")
	if err != nil {
		return nil, err
	}

	appConfig, err := apptype.GetDictAttr(appDef, "app_config", true)
	if err != nil {
		return nil, err
	}
	containerArgs, err := apptype.GetDictAttr(appDef, "container_args", true)
	if err != nil {
		return nil, err
	}
	containerOpts, err := apptype.GetDictAttr(appDef, "container_opts", true)
	if err != nil {
		return nil, err
	}
	containerVols, err := apptype.GetListStringAttr(appDef, "container_vols", true)
	if err != nil {
		return nil, err
	}

	paramStr, err := convertToMapString(params, false)
	if err != nil {
		return nil, err
	}
	appConfigStr, err := convertToMapString(appConfig, true)
	if err != nil {
		return nil, err
	}
	containerArgsStr, err := convertToMapString(containerArgs, false)
	if err != nil {
		return nil, err
	}
	containerOptsStr, err := convertToMapString(containerOpts, false)
	if err != nil {
		return nil, err
	}

	return &types.CreateAppRequest{
		Path:             path,
		SourceUrl:        source,
		IsDev:            dev,
		ParamValues:      paramStr,
		AppAuthn:         types.AppAuthnType(auth),
		GitAuthName:      gitAuth,
		GitBranch:        gitBranch,
		GitCommit:        gitCommit,
		Spec:             types.AppSpec(spec),
		AppConfig:        appConfigStr,
		ContainerOptions: containerOptsStr,
		ContainerArgs:    containerArgsStr,
		ContainerVolumes: containerVols,
	}, nil
}

func (s *Server) setupSource(applyPath, branch, commit, gitAuth string) (string, string, error) {
	if !isGit(applyPath) {
		return filepath.Dir(applyPath), filepath.Base(applyPath), nil
	}

	// Create temp directory on disk
	tmpDir, err := os.MkdirTemp("", "clace_git_apply_")
	if err != nil {
		return "", "", err
	}

	branch = cmp.Or(branch, "main")
	_, _, file, err := s.checkoutRepo(applyPath, branch, commit, gitAuth, tmpDir)
	if err != nil {
		return "", "", err
	}

	return tmpDir, file, nil
}

func (s *Server) Apply(ctx context.Context, applyPath string, appPathGlob string, approve, dryRun, promote bool,
	reload types.AppReloadOption, branch, commit, gitAuth string, force bool) (*types.AppApplyResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if reload == "" {
		reload = types.AppReloadOptionUpdated
	}

	dir, file, err := s.setupSource(applyPath, branch, commit, gitAuth)
	if err != nil {
		return nil, err
	}
	sourceFS, err := appfs.NewSourceFs(dir, appfs.NewDiskReadFS(s.Logger, dir, nil), false)
	if err != nil {
		return nil, err
	}

	applyConfig := map[types.AppPathDomain]*types.CreateAppRequest{}
	globFiles, err := sourceFS.Glob(file)
	if err != nil {
		return nil, err
	}
	for _, f := range globFiles {
		fileBytes, err := sourceFS.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("error reading file %s: %w", f, err)
		}

		fileConfig, err := s.loadApplyInfo(f, fileBytes)
		if err != nil {
			return nil, err
		}

		for _, config := range fileConfig {

			appPathDomain, err := parseAppPath(config.Path)
			if err != nil {
				return nil, err
			}
			if _, ok := applyConfig[appPathDomain]; ok {
				return nil, fmt.Errorf("duplicate app %s defined in file %s", config.Path, f)
			}
			applyConfig[appPathDomain] = config
		}
	}

	filteredApps := make([]types.AppPathDomain, 0, len(applyConfig))
	for appPathDomain := range applyConfig {
		match, err := MatchGlob(appPathGlob, appPathDomain)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		filteredApps = append(filteredApps, appPathDomain)
	}

	updateResults := make([]types.AppPathDomain, 0, len(filteredApps))
	approveResults := make([]types.ApproveResult, 0, len(filteredApps))
	promoteResults := make([]types.AppPathDomain, 0, len(filteredApps))
	reloadResults := make([]types.AppPathDomain, 0, len(filteredApps))

	allApps, err := s.apps.GetAllAppsInfo()
	if err != nil {
		return nil, err
	}
	allAppsMap := make(map[types.AppPathDomain]types.AppInfo)
	for _, appInfo := range allApps {
		allAppsMap[appInfo.AppPathDomain] = appInfo
	}

	newApps := make([]types.AppPathDomain, 0, len(filteredApps))
	updatedApps := make([]types.AppPathDomain, 0, len(filteredApps))

	for _, appPath := range filteredApps {
		appInfo, ok := allAppsMap[appPath]
		if !ok {
			// New app being created
			newApps = append(newApps, appPath)
		} else {
			applyInfo := applyConfig[appPath]
			if appInfo.SourceUrl != applyInfo.SourceUrl {
				return nil, fmt.Errorf("app %s already exists with different source url: %s", appPath, appInfo.SourceUrl)
			}
			if appInfo.IsDev != applyInfo.IsDev {
				return nil, fmt.Errorf("app %s already exists with different dev status: %t", appPath, appInfo.IsDev)
			}

			updatedApps = append(updatedApps, appPath)
		}
	}

	createResults := make([]types.AppCreateResponse, 0, len(newApps))
	for _, newApp := range newApps {
		applyInfo := applyConfig[newApp]
		res, err := s.CreateApp(ctx, tx, newApp.String(), approve, dryRun, applyInfo)
		if err != nil {
			return nil, err
		}

		createResults = append(createResults, *res)
	}

	for _, updateApp := range updatedApps {
		applyInfo := applyConfig[updateApp]
		applyResult, err := s.applyAppUpdate(ctx, tx, updateApp, applyInfo, approve, dryRun, promote, reload, force)
		if err != nil {
			return nil, err
		}

		updateResults = append(updateResults, applyResult.Updated...)
		if applyResult.Promoted {
			promoteResults = append(promoteResults, updateApp)
		}
		reloadResults = append(reloadResults, applyResult.Reloaded...)
		if applyResult.ApproveResult != nil {
			approveResults = append(approveResults, *applyResult.ApproveResult)
		}
	}

	// Commit the transaction if not dry run and update the in memory app store
	if err := s.CompleteTransaction(ctx, tx, updateResults, dryRun, "apply"); err != nil {
		return nil, err
	}

	ret := &types.AppApplyResponse{
		DryRun:         dryRun,
		CreateResults:  createResults,
		UpdateResults:  updateResults,
		ApproveResults: approveResults,
		PromoteResults: promoteResults,
		ReloadResults:  reloadResults,
	}

	return ret, nil
}

func convertToMapString(input map[string]any, convertToml bool) (map[string]string, error) {
	ret := make(map[string]string)
	for k, v := range input {
		if value, ok := v.(string); ok {
			if convertToml {
				ret[k] = "\"" + value + "\""
			} else {
				ret[k] = value
			}
		} else {
			var val []byte
			var err error
			if convertToml {
				val, err = toml.Marshal(v)
			} else {
				val, err = json.Marshal(v)
			}
			if err != nil {
				return nil, err
			}
			ret[k] = string(val)
		}
	}
	return ret, nil
}

func (s *Server) applyAppUpdate(ctx context.Context, tx types.Transaction, appPathDomain types.AppPathDomain, newInfo *types.CreateAppRequest,
	approve, dryRun, promote bool, reload types.AppReloadOption, force bool) (*types.AppApplyResult, error) {
	liveApp, err := s.GetAppEntry(ctx, tx, appPathDomain)
	if err != nil {
		return nil, fmt.Errorf("app missing during update %w", err)
	}

	prodApp := liveApp
	if !liveApp.IsDev {
		// For prod apps, update the staging app
		liveApp, err = s.getStageApp(ctx, tx, liveApp)
		if err != nil {
			return nil, err
		}

		stagingFileStore := metadata.NewFileStore(liveApp.Id, liveApp.Metadata.VersionMetadata.Version, s.db, tx)
		err := stagingFileStore.IncrementAppVersion(ctx, tx, &liveApp.Metadata)
		if err != nil {
			return nil, fmt.Errorf("error incrementing app version: %w", err)
		}
	}

	oldInfoStr := string(liveApp.Metadata.VersionMetadata.ApplyInfo)
	var oldInfo *types.CreateAppRequest
	if len(oldInfoStr) > 0 {
		if err := json.Unmarshal([]byte(oldInfoStr), &oldInfo); err != nil {
			return nil, fmt.Errorf("error unmarshalling stored app info: %w", err)
		}
		oldInfo.AppAuthn = cmp.Or(oldInfo.AppAuthn, types.AppAuthnDefault)
	}
	newInfo.AppAuthn = cmp.Or(newInfo.AppAuthn, types.AppAuthnDefault)

	authChanged := checkPropertyChanged(oldInfo, func(info *types.CreateAppRequest) any {
		return info.AppAuthn
	}, newInfo.AppAuthn, liveApp.Settings.AuthnType, force)
	if authChanged {
		return nil, fmt.Errorf("app %s authentication changed, cannot apply changes. Use \"app update-settings\"", appPathDomain)
	}

	gitAuthChanged := checkPropertyChanged(oldInfo, func(info *types.CreateAppRequest) any {
		return info.GitAuthName
	}, newInfo.GitAuthName, liveApp.Settings.GitAuthName, force)
	if gitAuthChanged {
		return nil, fmt.Errorf("app %s git auth changed, cannot apply changes. Use \"app update-settings\"", appPathDomain)
	}

	specChanged := checkPropertyChanged(oldInfo, func(info *types.CreateAppRequest) any {
		return info.Spec
	}, newInfo.Spec, liveApp.Metadata.Spec, force)
	if specChanged {
		if newInfo.Spec == "" {
			liveApp.Metadata.SpecFiles = nil
			liveApp.Metadata.Spec = ""
		} else {
			appFiles := s.GetAppSpec(newInfo.Spec)
			if len(appFiles) == 0 {
				return nil, fmt.Errorf("invalid app spec %s for app %s", newInfo.Spec, appPathDomain)
			}
			liveApp.Metadata.SpecFiles = &appFiles
			liveApp.Metadata.Spec = newInfo.Spec
		}
	}

	gitBranchChanged := checkPropertyChanged(oldInfo, func(info *types.CreateAppRequest) any {
		return info.GitBranch
	}, newInfo.GitBranch, liveApp.Metadata.VersionMetadata.GitBranch, force)
	if gitBranchChanged {
		liveApp.Metadata.VersionMetadata.GitBranch = newInfo.GitBranch
	}
	gitCommitChanged := false
	if newInfo.GitCommit != "" {
		gitCommitChanged = checkPropertyChanged(oldInfo, func(info *types.CreateAppRequest) any {
			return info.GitCommit
		}, newInfo.GitCommit, liveApp.Metadata.VersionMetadata.GitCommit, force)
		if gitCommitChanged {
			liveApp.Metadata.VersionMetadata.GitCommit = newInfo.GitCommit
		}
	}

	var oldParams map[string]string
	if oldInfo != nil {
		oldParams = oldInfo.ParamValues
	}
	paramsChanged := mergeMap(oldParams, newInfo.ParamValues, liveApp.Metadata.ParamValues, force)

	var oldContOptions map[string]string
	if oldInfo != nil {
		oldContOptions = oldInfo.ContainerOptions
	}
	contConfigChanged := mergeMap(oldContOptions, newInfo.ContainerOptions, liveApp.Metadata.ContainerOptions, force)

	var oldContArgs map[string]string
	if oldInfo != nil {
		oldContArgs = oldInfo.ContainerArgs
	}
	contArgsChanged := mergeMap(oldContArgs, newInfo.ContainerArgs, liveApp.Metadata.ContainerArgs, force)

	var oldContVolumes []string
	if oldInfo != nil {
		oldContVolumes = oldInfo.ContainerVolumes
	}
	contVolsChanged := mergeSlice(oldContVolumes, newInfo.ContainerVolumes, &liveApp.Metadata.ContainerVolumes, force)

	var oldAppConfig map[string]string
	if oldInfo != nil {
		oldAppConfig = oldInfo.AppConfig
	}
	appConfigChanged := mergeMap(oldAppConfig, newInfo.AppConfig, liveApp.Metadata.AppConfig, force)

	updated := specChanged || gitBranchChanged || gitCommitChanged || paramsChanged ||
		contConfigChanged || contArgsChanged || contVolsChanged || appConfigChanged
	updatedApps := make([]types.AppPathDomain, 0)
	if updated {
		liveApp.Metadata.VersionMetadata.ApplyInfo, err = json.Marshal(newInfo)
		if err != nil {
			return nil, err
		}
		if err := s.db.UpdateAppMetadata(ctx, tx, liveApp); err != nil {
			return nil, err
		}
		updatedApps = append(updatedApps, liveApp.AppPathDomain())
	}

	reloadApp := reload == types.AppReloadOptionAll || updated && reload == types.AppReloadOptionUpdated
	promoteApp := false
	ret := &types.AppApplyResult{
		DryRun: dryRun,
	}
	if reloadApp {
		reloadResult, err := s.ReloadApp(ctx, tx, prodApp, approve, dryRun, promote, newInfo.GitBranch, newInfo.GitCommit, newInfo.GitAuthName)
		if err != nil {
			return nil, err
		}
		ret.ApproveResult = reloadResult.ApproveResult
		ret.Reloaded = reloadResult.ReloadResults
		promoteApp = len(reloadResult.PromoteResults) > 0
	}

	if updated && promote && !liveApp.IsDev {
		if !reloadApp {
			// For prod apps, promote the staging app (unless already promoted as part of reload)
			if err = s.promoteApp(ctx, tx, liveApp, prodApp); err != nil {
				return nil, err
			}
			promoteApp = true
		}
		updatedApps = append(updatedApps, prodApp.AppPathDomain())
	}

	ret.Updated = updatedApps
	ret.Promoted = promoteApp
	return ret, nil
}

func mergeMap(old, new, live map[string]string, force bool) bool {
	if force {
		// Force overwrite the live map
		if reflect.DeepEqual(live, new) {
			return false
		}
		// Force update all values
		clear(live)
		for k, v := range new {
			live[k] = v
		}
		return true
	}

	updated := false
	if old == nil {
		// First run of apply
		for k, v := range new {
			// Add values from new, retaining existing live values
			updated = true
			live[k] = v
		}
	} else {
		// Three way merge
		for k, v := range old {
			newV, ok := new[k]
			if ok && v != newV {
				// Changed from old to new
				if live[k] != newV {
					updated = true
					live[k] = newV
				}
			}
			if !ok {
				// Removed from new
				_, present := live[k]
				if present {
					updated = true
					delete(live, k)
				}
			}
		}

		for k, v := range new {
			_, ok := old[k]
			if !ok {
				// Added in new
				updated = true
				live[k] = v
			}
		}
	}
	return updated
}

func mergeSlice(old, new []string, live *[]string, force bool) bool {
	if force {
		if reflect.DeepEqual(*live, new) {
			return false
		}
		// Force update all values
		*live = append([]string{}, new...)
		return true
	}

	updated := false
	liveDict := make(map[string]int)
	for i, v := range *live {
		liveDict[v] = i
	}
	newDict := make(map[string]int)
	for i, v := range new {
		newDict[v] = i
	}
	oldDict := make(map[string]int)
	for i, v := range old {
		oldDict[v] = i
	}

	if old == nil {
		// First run of apply
		for _, v := range new {
			// Add values from new, retaining existing live values
			if !hasEntry(v, liveDict) {
				updated = true
				*live = append(*live, v)
			}
		}
	} else {
		// Three way merge
		for _, v := range old {
			if !hasEntry(v, newDict) && hasEntry(v, liveDict) {
				// Removed from new
				updated = true
				*live = slices.Delete(*live, liveDict[v], liveDict[v]+1)
			}
		}
		for _, v := range new {
			if !hasEntry(v, oldDict) && !hasEntry(v, liveDict) {
				// Added in new
				updated = true
				*live = append(*live, v)
			}
		}
	}

	return updated
}

func hasEntry(s string, dict map[string]int) bool {
	_, ok := dict[s]
	return ok
}

func checkPropertyChanged(oldInfo *types.CreateAppRequest, fetchVal func(*types.CreateAppRequest) any, newVal, liveVal any, force bool) bool {
	if force || oldInfo == nil {
		return !reflect.DeepEqual(liveVal, newVal)
	}
	var oldVal = fetchVal(oldInfo)
	return !reflect.DeepEqual(oldVal, newVal) && !reflect.DeepEqual(liveVal, newVal)
}
