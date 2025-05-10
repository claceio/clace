// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/claceio/clace/internal/passwd"
	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/segmentio/ksuid"
)

func (s *Server) CreateSyncEntry(ctx context.Context, path string, scheduled, dryRun bool, sync *types.SyncMetadata) (*types.SyncCreateResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	genId, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}
	id := "cl_syn_" + strings.ToLower(genId.String())

	if !scheduled {
		// Webhook sync entry
		secret, err := passwd.GeneratePassword()
		if err != nil {
			return nil, err
		}
		sync.WebhookSecret = fmt.Sprintf("cl_tkn_%s", base64.StdEncoding.EncodeToString([]byte(secret)))
	} else if sync.ScheduleFrequency <= 0 {
		sync.ScheduleFrequency = s.config.System.DefaultScheduleMins
	}

	syncEntry := types.SyncEntry{
		Id:          id,
		Path:        path,
		IsScheduled: scheduled,
		UserID:      system.GetContextUserId(ctx),
		Metadata:    *sync,
	}

	// Persist the settings
	if err := s.db.CreateSync(ctx, tx, &syncEntry); err != nil {
		return nil, err
	}

	syncStatus, updatedApps, err := s.runSyncJob(ctx, tx, &syncEntry, true, nil)
	if err != nil {
		return nil, err
	}

	ret := types.SyncCreateResponse{
		Id:                syncEntry.Id,
		DryRun:            dryRun,
		WebhookUrl:        "", // TODO
		WebhookSecret:     syncEntry.Metadata.WebhookSecret,
		ScheduleFrequency: syncEntry.Metadata.ScheduleFrequency,
		SyncJobStatus:     *syncStatus,
	}

	if dryRun {
		return &ret, nil
	}

	if err := s.CompleteTransaction(ctx, tx, updatedApps, false, "create_sync"); err != nil {
		return nil, err
	}

	return &ret, nil
}

func (s *Server) DeleteSyncEntry(ctx context.Context, id string, dryRun bool) (*types.SyncDeleteResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := s.db.DeleteSync(ctx, tx, id); err != nil {
		return nil, err
	}

	ret := types.SyncDeleteResponse{
		Id:     id,
		DryRun: dryRun,
	}

	if dryRun {
		return &ret, nil
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &ret, nil
}

func (s *Server) ListSyncEntries(ctx context.Context) (*types.SyncListResponse, error) {
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	entries, err := s.db.GetSyncEntries(ctx, tx)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		e.Metadata.WebhookUrl = "" // TODO: Set the actual webhook URL
	}

	ret := types.SyncListResponse{
		Entries: entries,
	}
	return &ret, nil
}

func (s *Server) syncRunner() {
	s.Info().Msg("Starting sync runner loop")
	for range s.syncTimer.C {
		err := s.runSyncJobs()
		if err != nil {
			s.Error().Err(err).Msg("Error running sync")
			break
		}
	}
	s.Warn().Msg("Sync runner stopped")
}

func (s *Server) runSyncJobs() error {
	ctx := context.Background()
	tx, err := s.db.BeginTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Create a new repo cache if not passed in
	repoCache, err := NewRepoCache(s)
	if err != nil {
		return err
	}
	defer repoCache.Cleanup()

	scheduleEntries, err := s.db.GetSyncEntries(ctx, tx)
	if err != nil {
		return err
	}
	for _, entry := range scheduleEntries {
		if !entry.IsScheduled || entry.Metadata.ScheduleFrequency <= 0 {
			continue
		}

		_, _, err = s.runSyncJob(ctx, types.Transaction{}, entry, true, repoCache) // each sync runs in its own transaction
		if err != nil {
			s.Error().Err(err).Msgf("Error running sync job %s", entry.Id)
			// One failure does not stop the rest
		}
	}

	return nil
}

func (s *Server) runSyncJob(ctx context.Context, inputTx types.Transaction, entry *types.SyncEntry, checkCommitHash bool, repoCache *RepoCache) (*types.SyncJobStatus, []types.AppPathDomain, error) {
	var tx types.Transaction
	var err error
	if inputTx.Tx == nil {
		tx, err = s.db.BeginTransaction(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer tx.Rollback()
	} else {
		tx = inputTx
		// No rollback here if transaction is passed in
	}

	if repoCache == nil {
		// Create a new repo cache if not passed in
		repoCache, err = NewRepoCache(s)
		if err != nil {
			return nil, nil, err
		}
		defer repoCache.Cleanup()
	}

	lastRunApps := entry.Status.ApplyResponse.FilteredApps
	lastRunCommitId := ""
	if checkCommitHash {
		lastRunCommitId = entry.Status.CommitId
	}

	applyInfo, updatedApps, applyErr := s.Apply(ctx, tx, entry.Path, "all", entry.Metadata.Approve, false, entry.Metadata.Promote, types.AppReloadOption(entry.Metadata.Reload),
		entry.Metadata.GitBranch, "", entry.Metadata.GitAuth, entry.Metadata.Clobber, entry.Metadata.ForceReload, lastRunCommitId, repoCache)

	status := types.SyncJobStatus{
		LastExecutionTime: time.Now(),
		IsApply:           true,
	}
	if applyErr != nil {
		s.Error().Err(applyErr).Msgf("Error applying sync job %s", entry.Id)
		status.Error = applyErr.Error()
		applyInfo = &types.AppApplyResponse{}
		applyInfo.FilteredApps = lastRunApps
	} else {
		status.CommitId = applyInfo.CommitId
	}

	status.ApplyResponse = *applyInfo
	reloadResults := make([]types.AppPathDomain, 0, len(lastRunApps))
	approveResults := make([]types.ApproveResult, 0, len(lastRunApps))
	promoteResults := make([]types.AppPathDomain, 0, len(lastRunApps))

	if applyErr == nil && applyInfo.SkippedApply && entry.Metadata.Reload == string(types.AppReloadOptionMatched) {
		if len(applyInfo.FilteredApps) == 0 {
			// This run was skipped, use the last run apps
			applyInfo.FilteredApps = lastRunApps
		}

		// The apply was skipped, check if the apps need to be reloaded
		// The attempt is to avoid doing a full github checkout on the apply file repo and on the
		// app source repo, a list API is used to get the last commit
		appMap := map[types.AppPathDomain]*types.AppEntry{}
		appMissing := false
		for _, appPath := range lastRunApps {
			app, err := s.db.GetAppTx(ctx, tx, appPath)
			if err != nil {
				appMissing = true
				s.Error().Err(err).Msgf("Error getting app %s", appPath)
				break
			}
			appMap[appPath] = app
		}

		if appMissing {
			// App has been deleted, run the full apply with the latest commit even if it was already applied
			if !checkCommitHash {
				return nil, nil, fmt.Errorf("Unexpected error, sync rerun with no commit hash")
			}
			return s.runSyncJob(ctx, inputTx, entry, false, repoCache)
		} else {
			for _, appPath := range lastRunApps {
				app := appMap[appPath]
				reloadResult, err := s.ReloadApp(ctx, tx, app, nil, entry.Metadata.Approve, false, entry.Metadata.Promote,
					app.Metadata.VersionMetadata.GitBranch, "", app.Settings.GitAuthName, repoCache, entry.Metadata.ForceReload)
				if err != nil {
					return nil, nil, err
				}

				reloadResults = append(reloadResults, reloadResult.ReloadResults...)
				if reloadResult.ApproveResult != nil {
					approveResults = append(approveResults, *reloadResult.ApproveResult)
				}
				promoteResults = append(promoteResults, reloadResult.PromoteResults...)
			}

			status.ApplyResponse.ReloadResults = reloadResults
			status.ApplyResponse.ApproveResults = approveResults
			status.ApplyResponse.PromoteResults = promoteResults
		}
	}
	status.ApplyResponse = *applyInfo

	err = s.db.UpdateSyncStatus(ctx, tx, entry.Id, &status)
	if err != nil {
		return nil, nil, err
	}

	if inputTx.Tx == nil {
		if err := s.CompleteTransaction(ctx, tx, updatedApps, false, "sync"); err != nil {
			return nil, nil, err
		}
	}

	return &status, updatedApps, nil
}
