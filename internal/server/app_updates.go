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
	"github.com/claceio/clace/internal/utils"
)

func (s *Server) ReloadApps(ctx context.Context, pathSpec string, approve, promote bool, branch, commit, gitAuth string) (*utils.AppReloadResponse, error) {
	filteredApps, err := s.FilterApps(pathSpec, false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	approveResults := make([]utils.ApproveResult, 0, len(filteredApps))
	promoteResults := make([]utils.AppPathDomain, 0, len(filteredApps))

	prodAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))
	stageAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))
	devAppEntries := make([]*utils.AppEntry, 0, len(filteredApps))

	// Track the staging and prod apps
	for _, appInfo := range filteredApps {
		if appInfo.IsDev {
			// Dev mode app, reload from disk
			var devAppEntry *utils.AppEntry
			if devAppEntry, err = s.GetAppEntry(ctx, tx, appInfo.AppPathDomain); err != nil {
				return nil, err
			}

			app, err := s.GetApp(appInfo.AppPathDomain, true)
			if err != nil {
				return nil, err
			}
			// TODO : notify other server instances to reload
			if _, err = app.Reload(true, true); err != nil {
				return nil, fmt.Errorf("error reloading app %s: %w", appInfo, err)
			}

			// Dev app code loaded
			devAppEntries = append(prodAppEntries, devAppEntry)
			continue
		}
		prodAppEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, err
		}
		prodAppEntries = append(prodAppEntries, prodAppEntry)

		stageAppPath := appInfo.AppPathDomain
		stageAppPath.Path = stageAppPath.Path + utils.STAGE_SUFFIX
		stageAppEntry, err := s.GetAppEntry(ctx, tx, stageAppPath)
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

		// Persist the metadata so that any git info is saved
		if err := s.db.UpdateAppMetadata(ctx, tx, stageAppEntry); err != nil {
			return nil, err
		}

		stageApp, err := s.setupApp(stageAppEntry, tx)
		if err != nil {
			return nil, fmt.Errorf("error setting up stage app %s: %w", stageAppEntry, err)
		}
		stageApps = append(stageApps, stageApp)

		approveResult, err := stageApp.Approve()
		if err != nil {
			return nil, fmt.Errorf("error approving app %s: %w", stageAppEntry, err)
		}
		approveResults = append(approveResults, *approveResult)

		if approveResult.NeedsApproval {
			if !approve {
				return nil, fmt.Errorf("app %s needs approval", stageAppEntry)
			} else {
				stageApp.AppEntry.Metadata.Loads = approveResult.NewLoads
				stageApp.AppEntry.Metadata.Permissions = approveResult.NewPermissions
				if err := s.db.UpdateAppMetadata(ctx, tx, stageApp.AppEntry); err != nil {
					return nil, err
				}
			}
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
		devApps = append(devApps, devApp)

		approveResult, err := devApp.Approve()
		if err != nil {
			return nil, fmt.Errorf("error auditing dev app %s: %w", devAppEntry, err)
		}

		if approveResult.NeedsApproval {
			if !approve {
				return nil, fmt.Errorf("app %s needs approval", devAppEntry)
			} else {
				devApp.AppEntry.Metadata.Loads = approveResult.NewLoads
				devApp.AppEntry.Metadata.Permissions = approveResult.NewPermissions
				if err := s.db.UpdateAppMetadata(ctx, tx, devApp.AppEntry); err != nil {
					return nil, err
				}
			}
		}

		if _, err := devApp.Reload(true, true); err != nil {
			return nil, fmt.Errorf("error reloading dev app %s: %w", devApp.AppEntry, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	// Update the in memory app cache. This is done after the changes are committed
	s.apps.UpdateApps(devApps)
	s.apps.UpdateApps(stageApps)
	if promote {
		s.apps.UpdateApps(prodApps)
	}

	ret := &utils.AppReloadResponse{
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

func (s *Server) PromoteApps(ctx context.Context, pathSpec string) (*utils.AppPromoteResponse, error) {
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
		if !strings.HasPrefix(string(appInfo.Id), utils.ID_PREFIX_APP_PRD) {
			// Not a prod app, skip
			continue
		}

		prodAppEntry, err := s.GetAppEntry(ctx, tx, appInfo.AppPathDomain)
		if err != nil {
			return nil, fmt.Errorf("error getting prod app %s: %w", appInfo, err)
		}

		stagingAppPath := appInfo.AppPathDomain
		stagingAppPath.Path = appInfo.Path + utils.STAGE_SUFFIX
		stagingApp, err := s.GetAppEntry(ctx, tx, stagingAppPath)
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

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	s.apps.UpdateApps(newApps)
	return &utils.AppPromoteResponse{PromoteResults: result}, nil
}

func (s *Server) promoteApp(ctx context.Context, tx metadata.Transaction, stagingApp *utils.AppEntry, prodApp *utils.AppEntry) (bool, error) {
	stagingFileStore := metadata.NewFileStore(stagingApp.Id, stagingApp.Metadata.VersionMetadata.Version, s.db, tx)

	if stagingApp.Metadata.VersionMetadata.Version != 1 &&
		prodApp.Metadata.VersionMetadata.Version == stagingApp.Metadata.VersionMetadata.Version {
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
