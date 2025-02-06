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

	"github.com/claceio/clace/internal/app/appfs"
	"github.com/claceio/clace/internal/app/apptype"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const (
	APP = "app"
)

func (s *Server) loadApplyInfo(fileName string, data []byte) ([]*AppApplyConfig, error) {
	appDefs := make([]*starlarkstruct.Struct, 0)

	createAppBuiltin := func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var path, source starlark.String
		var dev starlark.Bool
		var params *starlark.Dict = starlark.NewDict(0)

		if err := starlark.UnpackArgs(APP, args, kwargs, "path", &path, "source", &source, "dev?", &dev, "params?", &params); err != nil {
			return nil, err
		}

		fields := starlark.StringDict{
			"path":   path,
			"source": source,
			"dev":    dev,
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

	ret := make([]*AppApplyConfig, 0, len(appDefs))
	for _, appDef := range appDefs {
		applyConfig, err := appDefToApplyConfig(appDef)
		if err != nil {
			return nil, err
		}
		ret = append(ret, applyConfig)
	}

	return ret, nil
}

func appDefToApplyConfig(appDef *starlarkstruct.Struct) (*AppApplyConfig, error) {
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

	appPathDomain, err := parseAppPath(path)
	if err != nil {
		return nil, err
	}

	params, err := apptype.GetDictAttr(appDef, "params", true)
	if err != nil {
		return nil, err
	}

	return &AppApplyConfig{
		Path:      appPathDomain,
		SourceUrl: source,
		IsDev:     dev,
		Params:    params,
	}, nil
}

type AppApplyConfig struct {
	Path      types.AppPathDomain
	SourceUrl string
	IsDev     bool
	Params    map[string]any
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

func (s *Server) Apply(ctx context.Context, applyPath string, appPathGlob string, approve, dryRun, promote bool, branch, commit, gitAuth string) (*types.AppApplyResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	dir, file, err := s.setupSource(applyPath, branch, commit, gitAuth)
	if err != nil {
		return nil, err
	}
	sourceFS, err := appfs.NewSourceFs(dir, appfs.NewDiskReadFS(s.Logger, dir, nil), false)
	if err != nil {
		return nil, err
	}

	applyConfig := map[types.AppPathDomain]*AppApplyConfig{}
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
			if _, ok := applyConfig[config.Path]; ok {
				return nil, fmt.Errorf("duplicate app %s defined in file %s", config.Path, f)
			}
			applyConfig[config.Path] = config
		}
	}

	filteredApps := make([]types.AppPathDomain, 0, len(applyConfig))
	for appPath, _ := range applyConfig {
		match, err := MatchGlob(appPathGlob, appPath)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		filteredApps = append(filteredApps, appPath)
	}

	updateResults := make([]types.AppPathDomain, 0, len(filteredApps))
	approveResults := make([]types.ApproveResult, 0, len(filteredApps))
	promoteResults := make([]types.AppPathDomain, 0, len(filteredApps))

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
		res, err := s.applyAppCreate(ctx, tx, newApp, applyInfo, approve, dryRun)
		if err != nil {
			return nil, err
		}

		createResults = append(createResults, *res)
	}

	for _, updateApp := range updatedApps {
		applyInfo := applyConfig[updateApp]
		updated, approveRes, promoteRes, err := s.applyAppUpdate(ctx, tx, applyInfo, approve, dryRun, promote)
		if err != nil {
			return nil, err
		}
		if updated {
			updateResults = append(updateResults, updateApp)
			approveResults = append(approveResults, approveRes)
			promoteResults = append(promoteResults, promoteRes)
		}
	}

	// Commit the transaction if not dry run and update the in memory app store
	if err := s.CompleteTransaction(ctx, tx, updateResults, dryRun, "reload"); err != nil {
		return nil, err
	}

	ret := &types.AppApplyResponse{
		DryRun:         dryRun,
		CreateResults:  createResults,
		UpdateResults:  updateResults,
		ApproveResults: approveResults,
		PromoteResults: promoteResults,
	}

	return ret, nil
}

func convertToMapString(input map[string]any) (map[string]string, error) {
	ret := make(map[string]string)
	for k, v := range input {
		if value, ok := v.(string); ok {
			ret[k] = value
		} else {
			val, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			ret[k] = string(val)
		}
	}
	return ret, nil
}

func (s *Server) applyAppCreate(ctx context.Context, tx types.Transaction, appPath types.AppPathDomain, applyInfo *AppApplyConfig, approve, dryRun bool) (*types.AppCreateResponse, error) {
	params, err := convertToMapString(applyInfo.Params)
	if err != nil {
		return nil, err
	}
	req := types.CreateAppRequest{
		SourceUrl:   applyInfo.SourceUrl,
		IsDev:       applyInfo.IsDev,
		ParamValues: params,
	}

	return s.CreateApp(ctx, tx, appPath.String(), approve, dryRun, req)
}

func (s *Server) applyAppUpdate(ctx context.Context, tx types.Transaction, applyInfo *AppApplyConfig, approve, dryRun, promote bool) (bool, types.ApproveResult, types.AppPathDomain, error) {
	return false, types.ApproveResult{}, types.AppPathDomain{}, nil
}
