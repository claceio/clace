// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/types"
)

func (s *Server) ReloadApps(ctx context.Context, appPathGlob string, approve, dryRun, promote bool, branch, commit, gitAuth string) (*types.AppReloadResponse, error) {
	filteredApps, err := s.FilterApps(appPathGlob, false)
	if err != nil {
		return nil, types.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reloadResults := make([]types.AppPathDomain, 0, len(filteredApps))
	approveResults := make([]types.ApproveResult, 0, len(filteredApps))
	promoteResults := make([]types.AppPathDomain, 0, len(filteredApps))

	prodAppEntries := make([]*types.AppEntry, 0, len(filteredApps))
	stageAppEntries := make([]*types.AppEntry, 0, len(filteredApps))
	devAppEntries := make([]*types.AppEntry, 0, len(filteredApps))

	// Track the staging and prod apps
	for _, appInfo := range filteredApps {
		if appInfo.IsDev {
			var devAppEntry *types.AppEntry
			if devAppEntry, err = s.GetAppEntry(ctx, tx, appInfo.AppPathDomain); err != nil {
				return nil, err
			}

			devAppEntries = append(devAppEntries, devAppEntry)
			continue
		}
		prodAppEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, err
		}
		prodAppEntries = append(prodAppEntries, prodAppEntry)

		stageAppEntry, err := s.getStageApp(ctx, tx, prodAppEntry)
		if err != nil {
			return nil, err
		}
		stageAppEntries = append(stageAppEntries, stageAppEntry)
	}

	stageApps := make([]*app.App, 0, len(stageAppEntries))
	// Load code for all staging apps into the transaction context
	for index, stageAppEntry := range stageAppEntries {
		if err := s.loadAppCode(ctx, tx, stageAppEntry, branch, commit, gitAuth); err != nil {
			return nil, err
		}
		reloadResults = append(reloadResults, stageAppEntry.AppPathDomain())

		// Persist the metadata so that any git info is saved
		if err := s.db.UpdateAppMetadata(ctx, tx, stageAppEntry); err != nil {
			return nil, err
		}
		if err := s.db.UpdateAppSettings(ctx, tx, stageAppEntry); err != nil {
			return nil, err
		}

		stageApp, err := s.setupApp(stageAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up stage app %s: %w", stageAppEntry, err)
		}
		stageApps = append(stageApps, stageApp)

		stageResult, err := stageApp.Audit()
		if err != nil {
			return nil, fmt.Errorf("error approving app %s: %w", stageAppEntry, err)
		}

		if stageResult.NeedsApproval {
			if !approve {
				return nil, fmt.Errorf("app %s needs approval", stageAppEntry)
			} else {
				stageApp.AppEntry.Metadata.Loads = stageResult.NewLoads
				stageApp.AppEntry.Metadata.Permissions = stageResult.NewPermissions
				if err := s.db.UpdateAppMetadata(ctx, tx, stageApp.AppEntry); err != nil {
					return nil, err
				}
			}
			approveResults = append(approveResults, *stageResult)
		}

		if promote {
			prodAppEntry := prodAppEntries[index]
			if err = s.promoteApp(ctx, tx, stageAppEntry, prodAppEntry); err != nil {
				return nil, err
			}

			promoteResults = append(promoteResults, prodAppEntry.AppPathDomain())
		}
	}

	for _, stageApp := range stageApps {
		if _, err := stageApp.Reload(true, true, app.DryRun(dryRun)); err != nil {
			return nil, fmt.Errorf("error reloading stage app %s: %w", stageApp.AppEntry, err)
		}
	}

	if promote {
		for _, prodAppEntry := range prodAppEntries {
			prodApp, err := s.setupApp(prodAppEntry, tx)
			if err != nil {
				return nil, fmt.Errorf("error setting up prod app %s: %w", prodAppEntry, err)
			}
			if _, err := prodApp.Reload(true, true, app.DryRun(dryRun)); err != nil {
				return nil, fmt.Errorf("error reloading prod app %s: %w", prodApp.AppEntry, err)
			}

			reloadResults = append(reloadResults, prodAppEntry.AppPathDomain())
		}
	}

	for _, devAppEntry := range devAppEntries {
		devApp, err := s.setupApp(devAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up app %s: %w", devAppEntry, err)
		}
		reloadResults = append(reloadResults, devAppEntry.AppPathDomain())

		devResult, err := devApp.Audit()
		if err != nil {
			return nil, fmt.Errorf("error auditing dev app %s: %w", devAppEntry, err)
		}

		if devResult.NeedsApproval {
			if !approve {
				return nil, fmt.Errorf("app %s needs approval", devAppEntry)
			} else {
				devApp.AppEntry.Metadata.Loads = devResult.NewLoads
				devApp.AppEntry.Metadata.Permissions = devResult.NewPermissions
				if err := s.db.UpdateAppMetadata(ctx, tx, devApp.AppEntry); err != nil {
					return nil, err
				}
			}
			approveResults = append(approveResults, *devResult)
		}

		if _, err := devApp.Reload(true, true, app.DryRun(dryRun)); err != nil {
			return nil, fmt.Errorf("error reloading dev app %s: %w", devApp.AppEntry, err)
		}
	}

	// Commit the transaction if not dry run and update the in memory app store
	if err := s.CompleteTransaction(ctx, tx, reloadResults, dryRun); err != nil {
		return nil, err
	}

	ret := &types.AppReloadResponse{
		DryRun:         dryRun,
		ReloadResults:  reloadResults,
		ApproveResults: approveResults,
		PromoteResults: promoteResults,
	}

	return ret, nil
}

func (s *Server) loadAppCode(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, branch, commit, gitAuth string) error {
	s.Info().Msgf("Reloading app code %v", appEntry)

	if isGit(appEntry.SourceUrl) {
		// Checkout the git repo locally and load into database
		if err := s.loadSourceFromGit(ctx, tx, appEntry, branch, commit, gitAuth); err != nil {
			return err
		}
	} else {
		// App is loaded from disk (not git), load files into DB
		if err := s.loadSourceFromDisk(ctx, tx, appEntry); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) StagedUpdate(ctx context.Context, appPathGlob string, dryRun, promote bool, handler stagedUpdateHandler, args map[string]any) (*types.AppStagedUpdateResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, entries, promoteResults, err := s.StagedUpdateAppsTx(ctx, tx, appPathGlob, promote, handler, args)
	if err != nil {
		return nil, err
	}

	ret := &types.AppStagedUpdateResponse{
		DryRun:              dryRun,
		StagedUpdateResults: result,
		PromoteResults:      promoteResults,
	}

	if err := s.CompleteTransaction(ctx, tx, entries, dryRun); err != nil {
		return nil, err
	}

	return ret, nil
}

type stagedUpdateHandler func(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, args map[string]any) (any, types.AppPathDomain, error)

func (s *Server) StagedUpdateAppsTx(ctx context.Context, tx types.Transaction, appPathGlob string, promote bool, handler stagedUpdateHandler, args map[string]any) ([]any, []types.AppPathDomain, []types.AppPathDomain, error) {
	filteredApps, err := s.FilterApps(appPathGlob, false)
	if err != nil {
		return nil, nil, nil, err
	}

	results := make([]any, 0, len(filteredApps))
	entries := make([]types.AppPathDomain, 0, len(filteredApps))
	promoteResults := make([]types.AppPathDomain, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		appEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		var prodAppEntry *types.AppEntry
		if !appEntry.IsDev {
			// For prod apps, update the staging app
			prodAppEntry = appEntry
			appEntry, err = s.getStageApp(ctx, tx, appEntry)
			if err != nil {
				return nil, nil, nil, err
			}

			stagingFileStore := metadata.NewFileStore(appEntry.Id, appEntry.Metadata.VersionMetadata.Version, s.db, tx)
			err := stagingFileStore.IncrementAppVersion(ctx, tx, &appEntry.Metadata)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error incrementing app version: %w", err)
			}
		}

		result, app, err := handler(ctx, tx, appEntry, args)
		if err != nil {
			return nil, nil, nil, err
		}

		if err := s.db.UpdateAppMetadata(ctx, tx, appEntry); err != nil {
			return nil, nil, nil, err
		}

		if promote && prodAppEntry != nil {
			if err = s.promoteApp(ctx, tx, appEntry, prodAppEntry); err != nil {
				return nil, nil, nil, err
			}

			// prod app audit result is not added to results, since it will be same as the staging app
			prodApp, err := s.setupApp(prodAppEntry, tx)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("error setting up prod app %s: %w", prodAppEntry, err)
			}
			entries = append(entries, prodApp.AppPathDomain())
			promoteResults = append(promoteResults, prodAppEntry.AppPathDomain())
		}

		entries = append(entries, app)
		results = append(results, result)
	}

	return results, entries, promoteResults, nil
}

func (s *Server) auditHandler(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, args map[string]any) (any, types.AppPathDomain, error) {
	appPathDomain := appEntry.AppPathDomain()
	app, err := s.setupApp(appEntry, tx)
	if err != nil {
		return nil, appPathDomain, err
	}
	result, err := s.auditApp(ctx, tx, app, true)
	if err != nil {
		return nil, appPathDomain, err
	}

	return result, appPathDomain, nil
}

func (s *Server) PromoteApps(ctx context.Context, appPathGlob string, dryRun bool) (*types.AppPromoteResponse, error) {
	filteredApps, err := s.FilterApps(appPathGlob, false)
	if err != nil {
		return nil, types.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := make([]types.AppPathDomain, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		if appInfo.IsDev {
			// Not a prod app, skip
			continue
		}

		prodAppEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		stagingApp, err := s.getStageApp(ctx, tx, prodAppEntry)
		if err != nil {
			return nil, err
		}

		if err = s.promoteApp(ctx, tx, stagingApp, prodAppEntry); err != nil {
			return nil, err
		}

		prodApp, err := s.setupApp(prodAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up prod app %s: %w", prodAppEntry, err)
		}
		if _, err := prodApp.Reload(true, true, app.DryRun(dryRun)); err != nil {
			return nil, fmt.Errorf("error reloading prod app %s: %w", prodApp.AppEntry, err)
		}
		result = append(result, appInfo.AppPathDomain)
	}

	if err = s.CompleteTransaction(ctx, tx, result, dryRun); err != nil {
		return nil, err
	}

	return &types.AppPromoteResponse{
		DryRun:         dryRun,
		PromoteResults: result}, nil
}

func (s *Server) promoteApp(ctx context.Context, tx types.Transaction, stagingApp *types.AppEntry, prodApp *types.AppEntry) error {
	stagingFileStore := metadata.NewFileStore(stagingApp.Id, stagingApp.Metadata.VersionMetadata.Version, s.db, tx)
	prodFileStore := metadata.NewFileStore(prodApp.Id, prodApp.Metadata.VersionMetadata.Version, s.db, tx)
	prevVersion := prodApp.Metadata.VersionMetadata.Version
	newVersion := stagingApp.Metadata.VersionMetadata.Version

	existingProdVersion, _ := prodFileStore.GetAppVersion(ctx, tx, newVersion)
	prodApp.Metadata = stagingApp.Metadata
	if prevVersion != newVersion {
		prodApp.Metadata.VersionMetadata.PreviousVersion = prevVersion
		prodApp.Metadata.VersionMetadata.Version = newVersion // the prod app version after promote is the same as the staging app version
		// there might be some gaps in the prod app version numbers, but that is ok, the attempt is to have the version number in
		// sync with the staging app version number when a promote is done

		if existingProdVersion == nil {
			if err := stagingFileStore.PromoteApp(ctx, tx, prodApp.Id, &prodApp.Metadata); err != nil {
				return err
			}
		}
	}

	// Even if there is no version change, promotion is done to update other metadata settings like account links
	if err := s.db.UpdateAppMetadata(ctx, tx, prodApp); err != nil {
		return err
	}
	return nil
}

func (s *Server) UpdateAppSettings(ctx context.Context, appPathGlob string, dryRun bool, updateAppRequest types.UpdateAppRequest) (*types.AppUpdateSettingsResponse, error) {
	filteredApps, err := s.FilterApps(appPathGlob, false)
	if err != nil {
		return nil, types.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	results := make([]types.AppPathDomain, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		_, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		appResults, err := s.updateAppSettings(ctx, tx, appInfo.AppPathDomain, updateAppRequest)
		if err != nil {
			return nil, err
		}

		results = append(results, appResults...)
	}

	ret := &types.AppUpdateSettingsResponse{
		DryRun:        dryRun,
		UpdateResults: results,
	}

	if dryRun {
		return ret, nil
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	s.apps.DeleteApps(results) // Delete instead of update to avoid having to initialize all the linked apps
	// Apps will get reloaded on the next request
	return ret, nil
}

func (s *Server) updateAppSettings(ctx context.Context, tx types.Transaction, appPathDomain types.AppPathDomain, updateAppRequest types.UpdateAppRequest) ([]types.AppPathDomain, error) {
	mainAppEntry, err := s.db.GetAppTx(ctx, tx, appPathDomain)
	if err != nil {
		return nil, err
	}

	linkedApps, err := s.db.GetLinkedApps(ctx, tx, mainAppEntry.Id)
	if err != nil {
		return nil, err
	}

	linkedApps = append(linkedApps, mainAppEntry) // Include the main app

	ret := make([]types.AppPathDomain, 0, len(linkedApps))
	for _, linkedApp := range linkedApps {
		if updateAppRequest.StageWriteAccess != types.BoolValueUndefined {
			if updateAppRequest.StageWriteAccess == types.BoolValueTrue {
				linkedApp.Settings.StageWriteAccess = true
			} else {
				linkedApp.Settings.StageWriteAccess = false
			}
		}

		if updateAppRequest.PreviewWriteAccess != types.BoolValueUndefined {
			if updateAppRequest.PreviewWriteAccess == types.BoolValueTrue {
				linkedApp.Settings.PreviewWriteAccess = true
			} else {
				linkedApp.Settings.PreviewWriteAccess = false
			}
		}

		if updateAppRequest.AuthnType != types.StringValueUndefined {
			if !s.ssoAuth.ValidateAuthType(string(updateAppRequest.AuthnType)) {
				return nil, fmt.Errorf("invalid authentication type %s", updateAppRequest.AuthnType)
			}
			linkedApp.Settings.AuthnType = types.AppAuthnType(updateAppRequest.AuthnType)
		}

		if updateAppRequest.GitAuthName != types.StringValueUndefined {
			if updateAppRequest.GitAuthName == "-" {
				linkedApp.Settings.GitAuthName = ""
			} else {
				linkedApp.Settings.GitAuthName = string(updateAppRequest.GitAuthName)
			}
		}

		if err := s.db.UpdateAppSettings(ctx, tx, linkedApp); err != nil {
			return nil, err
		}

		ret = append(ret, linkedApp.AppPathDomain())
	}

	return ret, nil
}

func (s *Server) accountLinkHandler(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, args map[string]any) (any, types.AppPathDomain, error) {
	if appEntry.Metadata.Accounts == nil {
		appEntry.Metadata.Accounts = []types.AccountLink{}
	}

	plugin := args["plugin"].(string)
	account := args["account"].(string)

	matchIndex := -1
	for i, accountLink := range appEntry.Metadata.Accounts {
		if accountLink.Plugin == plugin {
			// Update existing value
			accountLink.AccountName = account
			matchIndex = i
			break
		}
	}
	if matchIndex == -1 {
		// Add new value
		appEntry.Metadata.Accounts = append(appEntry.Metadata.Accounts, types.AccountLink{
			Plugin:      plugin,
			AccountName: account,
		})
	} else {
		if account == "-" {
			// Delete the entry
			appEntry.Metadata.Accounts = append(appEntry.Metadata.Accounts[:matchIndex], appEntry.Metadata.Accounts[matchIndex+1:]...)
		} else {
			// Update existing value
			appEntry.Metadata.Accounts[matchIndex].AccountName = account
		}
	}

	appPathDomain := appEntry.AppPathDomain()
	return appPathDomain, appPathDomain, nil
}

func (s *Server) updateParamHandler(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, args map[string]any) (any, types.AppPathDomain, error) {
	paramName := args["paramName"].(string)
	paramValue := args["paramValue"].(string)

	if appEntry.Metadata.ParamValues == nil {
		appEntry.Metadata.ParamValues = make(map[string]string)
	}

	paramValue = types.StripQuotes(strings.TrimSpace(paramValue))
	if paramValue == "-" {
		// Delete the entry
		delete(appEntry.Metadata.ParamValues, paramName)
	} else {
		// Update existing value
		appEntry.Metadata.ParamValues[paramName] = paramValue
	}

	appPathDomain := appEntry.AppPathDomain()
	return appPathDomain, appPathDomain, nil
}

func (s *Server) updateMetadataHandler(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, args map[string]any) (any, types.AppPathDomain, error) {
	updateMetadata := args["metadata"].(types.UpdateAppMetadataRequest)

	if updateMetadata.Spec != types.StringValueUndefined {
		// The type is being updated
		var appFiles types.SpecFiles
		if updateMetadata.Spec != "-" {
			appFiles = s.GetAppSpec(types.AppSpec(updateMetadata.Spec))
			if appFiles == nil {
				return nil, appEntry.AppPathDomain(), fmt.Errorf("invalid app spec %s", updateMetadata.Spec)
			}
			appEntry.Metadata.Spec = types.AppSpec(updateMetadata.Spec)
		} else {
			appFiles = make(types.SpecFiles)
			appEntry.Metadata.Spec = types.AppSpec("")
		}

		appEntry.Metadata.SpecFiles = &appFiles
	}

	if updateMetadata.ConfigType != "" && updateMetadata.ConfigType != types.AppMetadataConfigType(types.StringValueUndefined) {
		s.updateAppMetadataConfig(appEntry, updateMetadata.ConfigType, updateMetadata.ConfigEntries)
	}

	appPathDomain := appEntry.AppPathDomain()
	return appPathDomain, appPathDomain, nil
}

// updateAppMetadataConfig updates the app metadata config
func (s *Server) updateAppMetadataConfig(appEntry *types.AppEntry, configType types.AppMetadataConfigType, configEntries []string) error {
	if len(configEntries) == 0 {
		return nil
	}

	if configType == types.AppMetadataContainerVolumes {
		appEntry.Metadata.ContainerVolumes = configEntries
		return nil
	}

	for _, entry := range configEntries {
		key, value, ok := strings.Cut(entry, "=")

		if !ok && configType != types.AppMetadataContainerOptions {
			return fmt.Errorf("invalid %s %s, need key=value", configType, entry)
		}

		key = strings.TrimSpace(key)
		value = types.StripQuotes(strings.TrimSpace(value))

		switch configType {
		case types.AppMetadataContainerOptions:
			if appEntry.Metadata.ContainerOptions == nil {
				appEntry.Metadata.ContainerOptions = make(map[string]string)
			}
			if value != "-" {
				appEntry.Metadata.ContainerOptions[key] = value
			} else {
				delete(appEntry.Metadata.ContainerOptions, key)
			}
		case types.AppMetadataContainerArgs:
			if appEntry.Metadata.ContainerArgs == nil {
				appEntry.Metadata.ContainerArgs = make(map[string]string)
			}
			if value != "-" {
				appEntry.Metadata.ContainerArgs[key] = value
			} else {
				delete(appEntry.Metadata.ContainerArgs, key)
			}
		case types.AppMetadataAppConfig:
			if appEntry.Metadata.AppConfig == nil {
				appEntry.Metadata.AppConfig = make(map[string]string)
			}
			if value != "-" {
				appEntry.Metadata.AppConfig[key] = value
			} else {
				delete(appEntry.Metadata.AppConfig, key)
			}
		// case AppMetadataContainerVolumes not expected here, already handled
		default:
			return fmt.Errorf("invalid config type %s", configType)
		}
	}

	return nil
}
