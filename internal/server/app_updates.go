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

		stageResult, err := stageApp.Approve()
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
			var promoted bool
			prodAppEntry := prodAppEntries[index]
			if promoted, err = s.promoteApp(ctx, tx, stageAppEntry, prodAppEntry); err != nil {
				return nil, err
			}

			if promoted {
				promoteResults = append(promoteResults, prodAppEntry.AppPathDomain())
			}
		}
	}

	prodApps := make([]*app.App, 0, len(filteredApps))
	devApps := make([]*app.App, 0, len(filteredApps))

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
			prodApps = append(prodApps, prodApp)
			if _, err := prodApp.Reload(true, true); err != nil {
				return nil, fmt.Errorf("error reloading prod app %s: %w", prodApp.AppEntry, err)
			}
		}
	}

	for _, devAppEntry := range devAppEntries {
		devApp, err := s.setupApp(devAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up app %s: %w", devAppEntry, err)
		}
		reloadResults = append(reloadResults, devAppEntry.AppPathDomain())
		devApps = append(devApps, devApp)

		devResult, err := devApp.Approve()
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

	updatedApps := make([]*app.App, 0, len(stageApps)+len(prodApps)+len(devApps))
	updatedApps = append(updatedApps, devApps...)
	updatedApps = append(updatedApps, stageApps...)
	if promote {
		updatedApps = append(updatedApps, prodApps...)
	}

	// Commit the transaction if not dry run and update the in memory app store
	if err := s.CompleteTransaction(ctx, tx, updatedApps, dryRun); err != nil {
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

func (s *Server) ApproveApps(ctx context.Context, pathSpec string, dryRun bool) (*utils.AppApproveResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result, apps, err := s.ApproveAppsTx(ctx, tx, pathSpec)
	if err != nil {
		return nil, err
	}

	ret := &utils.AppApproveResponse{
		DryRun:         dryRun,
		ApproveResults: result,
	}

	if err := s.CompleteTransaction(ctx, tx, apps, dryRun); err != nil {
		return nil, err
	}

	return ret, nil
}

func (s *Server) ApproveAppsTx(ctx context.Context, tx metadata.Transaction, pathSpec string) ([]utils.ApproveResult, []*app.App, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	results := make([]utils.ApproveResult, 0, len(filteredApps))
	apps := make([]*app.App, 0, len(filteredApps))
	for _, appInfo := range filteredApps {
		appEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		if !appEntry.IsDev {
			// For prod apps, approve the staging app
			appEntry, err = s.getStageApp(ctx, tx, appEntry)
			if err != nil {
				return nil, nil, err
			}
		}

		app, err := s.setupApp(appEntry, tx)
		if err != nil {
			return nil, nil, err
		}
		apps = append(apps, app)
		result, err := s.auditApp(ctx, tx, app, true)
		if err != nil {
			return nil, nil, err
		}

		results = append(results, *result)
	}

	return results, apps, nil
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
	newApps := make([]*app.App, 0, len(filteredApps))
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

		var promoted bool
		if promoted, err = s.promoteApp(ctx, tx, stagingApp, prodAppEntry); err != nil {
			return nil, err
		}
		if !promoted {
			continue
		}

		prodApp, err := s.setupApp(prodAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up prod app %s: %w", prodAppEntry, err)
		}
		if _, err := prodApp.Reload(true, true); err != nil {
			return nil, fmt.Errorf("error reloading prod app %s: %w", prodApp.AppEntry, err)
		}
		newApps = append(newApps, prodApp)
		result = append(result, appInfo.AppPathDomain)
	}

	if err = s.CompleteTransaction(ctx, tx, newApps, dryRun); err != nil {
		return nil, err
	}

	return &utils.AppPromoteResponse{
		DryRun:         dryRun,
		PromoteResults: result}, nil
}

func (s *Server) promoteApp(ctx context.Context, tx metadata.Transaction, stagingApp *utils.AppEntry, prodApp *utils.AppEntry) (bool, error) {
	stagingFileStore := metadata.NewFileStore(stagingApp.Id, stagingApp.Metadata.VersionMetadata.Version, s.db, tx)

	if prodApp.Metadata.VersionMetadata.Version == stagingApp.Metadata.VersionMetadata.Version {
		s.Info().Msgf("App %s:%s already in sync, no promotion required", prodApp.Domain, prodApp.Path)
		return false, nil
	}
	prevVersion := prodApp.Metadata.VersionMetadata.Version
	newVersion := stagingApp.Metadata.VersionMetadata.Version

	prodApp.Metadata = stagingApp.Metadata
	prodApp.Metadata.VersionMetadata.PreviousVersion = prevVersion
	prodApp.Metadata.VersionMetadata.Version = newVersion // the prod app version after promote is the same as the staging app version
	// there might be some gaps in the prod app version numbers, but that is ok, the attempt is to have the version number in
	// sync with the staging app version number when a promote is done

	if err := stagingFileStore.PromoteApp(ctx, tx, prodApp.Id, prodApp.Metadata.VersionMetadata); err != nil {
		return false, err
	}

	if err := s.db.UpdateAppMetadata(ctx, tx, prodApp); err != nil {
		return false, err
	}
	return true, nil
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
			authnType := utils.AppAuthnType(updateAppRequest.AuthnType)
			if authnType != utils.AppAuthnDefault && authnType != utils.AppAuthnNone {
				return nil, fmt.Errorf("invalid authentication type %s", updateAppRequest.AuthnType)
			}
			linkedApp.Settings.AuthnType = utils.AppAuthnType(updateAppRequest.AuthnType)
		}

		if updateAppRequest.GitAuthName != utils.StringValueUndefined {
			linkedApp.Settings.GitAuthName = string(updateAppRequest.GitAuthName)
		}

		if err := s.db.UpdateAppSettings(ctx, tx, linkedApp); err != nil {
			return nil, err
		}

		ret = append(ret, linkedApp.AppPathDomain())
	}

	return ret, nil
}
