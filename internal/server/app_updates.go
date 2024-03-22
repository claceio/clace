// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
)

func (s *Server) ReloadApps(ctx context.Context, pathSpec string, approve, dryRun, promote bool, branch, commit, gitAuth string) (*utils.AppReloadResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reloadResults := make([]utils.AppPathDomain, 0, len(filteredApps))
	approveResults := make([]utils.ApproveResult, 0, len(filteredApps))
	promoteResults := make([]utils.AppPathDomain, 0, len(filteredApps))

	prodAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))
	stageAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))
	devAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))

	// Track the staging and prod apps
	for _, appInfo := range filteredApps {
		if appInfo.IsDev {
			var devAppEntry *utils.AppEntry
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
		if _, err := stageApp.Reload(true, true); err != nil {
			return nil, fmt.Errorf("error reloading stage app %s: %w", stageApp.AppEntry, err)
		}
	}

	if promote {
		for _, prodAppEntry := range prodAppEntries {
			prodApp, err := s.setupApp(prodAppEntry, tx)
			if err != nil {
				return nil, fmt.Errorf("error setting up prod app %s: %w", prodAppEntry, err)
			}
			if _, err := prodApp.Reload(true, true); err != nil {
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

		if _, err := devApp.Reload(true, true); err != nil {
			return nil, fmt.Errorf("error reloading dev app %s: %w", devApp.AppEntry, err)
		}
	}

	// Commit the transaction if not dry run and update the in memory app store
	if err := s.CompleteTransaction(ctx, tx, reloadResults, dryRun); err != nil {
		return nil, err
	}

	ret := &utils.AppReloadResponse{
		DryRun:         dryRun,
		ReloadResults:  reloadResults,
		ApproveResults: approveResults,
		PromoteResults: promoteResults,
	}

	return ret, nil
}

func (s *Server) loadAppCode(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry, branch, commit, gitAuth string) error {
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

func (s *Server) StagedUpdate(ctx context.Context, pathSpec string, dryRun, promote bool, handler stagedUpdateHandler, args map[string]any) (*utils.AppStagedUpdateResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, entries, promoteResults, err := s.StagedUpdateAppsTx(ctx, tx, pathSpec, promote, handler, args)
	if err != nil {
		return nil, err
	}

	ret := &utils.AppStagedUpdateResponse{
		DryRun:              dryRun,
		StagedUpdateResults: result,
		PromoteResults:      promoteResults,
	}

	if err := s.CompleteTransaction(ctx, tx, entries, dryRun); err != nil {
		return nil, err
	}

	return ret, nil
}

type stagedUpdateHandler func(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry, args map[string]any) (any, utils.AppPathDomain, error)

func (s *Server) StagedUpdateAppsTx(ctx context.Context, tx metadata.Transaction, pathSpec string, promote bool, handler stagedUpdateHandler, args map[string]any) ([]any, []utils.AppPathDomain, []utils.AppPathDomain, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, nil, nil, err
	}

	results := make([]any, 0, len(filteredApps))
	entries := make([]utils.AppPathDomain, 0, len(filteredApps))
	promoteResults := make([]utils.AppPathDomain, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		appEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		var prodAppEntry *utils.AppEntry
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

func (s *Server) auditHandler(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry, args map[string]any) (any, utils.AppPathDomain, error) {
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

func (s *Server) PromoteApps(ctx context.Context, pathSpec string, dryRun bool) (*utils.AppPromoteResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := make([]utils.AppPathDomain, 0, len(filteredApps))
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
		if _, err := prodApp.Reload(true, true); err != nil {
			return nil, fmt.Errorf("error reloading prod app %s: %w", prodApp.AppEntry, err)
		}
		result = append(result, appInfo.AppPathDomain)
	}

	if err = s.CompleteTransaction(ctx, tx, result, dryRun); err != nil {
		return nil, err
	}

	return &utils.AppPromoteResponse{
		DryRun:         dryRun,
		PromoteResults: result}, nil
}

func (s *Server) promoteApp(ctx context.Context, tx metadata.Transaction, stagingApp *utils.AppEntry, prodApp *utils.AppEntry) error {
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

func (s *Server) UpdateAppSettings(ctx context.Context, pathSpec string, dryRun bool, updateAppRequest utils.UpdateAppRequest) (*utils.AppUpdateSettingsResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	results := make([]utils.AppPathDomain, 0, len(filteredApps))
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

	ret := &utils.AppUpdateSettingsResponse{
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

func (s *Server) updateAppSettings(ctx context.Context, tx metadata.Transaction, appPathDomain utils.AppPathDomain, updateAppRequest utils.UpdateAppRequest) ([]utils.AppPathDomain, error) {
	mainAppEntry, err := s.db.GetAppTx(ctx, tx, appPathDomain)
	if err != nil {
		return nil, err
	}

	linkedApps, err := s.db.GetLinkedApps(ctx, tx, mainAppEntry.Id)
	if err != nil {
		return nil, err
	}

	linkedApps = append(linkedApps, mainAppEntry) // Include the main app

	ret := make([]utils.AppPathDomain, 0, len(linkedApps))
	for _, linkedApp := range linkedApps {
		if updateAppRequest.StageWriteAccess != utils.BoolValueUndefined {
			if updateAppRequest.StageWriteAccess == utils.BoolValueTrue {
				linkedApp.Settings.StageWriteAccess = true
			} else {
				linkedApp.Settings.StageWriteAccess = false
			}
		}

		if updateAppRequest.PreviewWriteAccess != utils.BoolValueUndefined {
			if updateAppRequest.PreviewWriteAccess == utils.BoolValueTrue {
				linkedApp.Settings.PreviewWriteAccess = true
			} else {
				linkedApp.Settings.PreviewWriteAccess = false
			}
		}

		if updateAppRequest.AuthnType != utils.StringValueUndefined {
			if !s.ssoAuth.ValidateAuthType(string(updateAppRequest.AuthnType)) {
				return nil, fmt.Errorf("invalid authentication type %s", updateAppRequest.AuthnType)
			}
			linkedApp.Settings.AuthnType = utils.AppAuthnType(updateAppRequest.AuthnType)
		}

		if updateAppRequest.GitAuthName != utils.StringValueUndefined {
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

func (s *Server) accountLinkHandler(ctx context.Context, tx metadata.Transaction, appEntry *utils.AppEntry, args map[string]any) (any, utils.AppPathDomain, error) {
	if appEntry.Metadata.Accounts == nil {
		appEntry.Metadata.Accounts = []utils.AccountLink{}
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
		appEntry.Metadata.Accounts = append(appEntry.Metadata.Accounts, utils.AccountLink{
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
