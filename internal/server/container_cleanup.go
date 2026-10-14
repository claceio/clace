// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/openrundev/openrun/internal/container"
	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
)

type staleContainerManager interface {
	ListOpenRunContainers(ctx context.Context) ([]container.Container, error)
	StopContainer(ctx context.Context, name container.ContainerName) error
}

func (s *Server) startStaleContainerCleanup() {
	if s.Config().System.ContainerCommand == "" || s.Config().System.ContainerCommand == types.CONTAINER_KUBERNETES {
		return
	}
	if s.Config().System.StaleContainerCleanupIntervalMins <= 0 {
		return
	}

	interval := time.Duration(s.Config().System.StaleContainerCleanupIntervalMins) * time.Minute
	s.staleContainerCleanup = system.StartPeriodicTask(context.Background(), interval, false, s.staleContainerCleanupPass)
}

func (s *Server) staleContainerCleanupPass(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := s.cleanupStaleContainers(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.Error().Err(err).Msg("Error cleaning up stale containers")
	}
}

func (s *Server) cleanupStaleContainers(ctx context.Context) error {
	manager := container.NewCommandCM(s.Logger, s.Config(), "", "")
	active := s.apps.ActiveContainerNames()
	// Containers started by operations still in flight (reload/apply/sync
	// before their DB transaction commits) are not yet referenced by the app
	// store; treat them as active so they are not stopped mid-deploy.
	for name := range s.inFlightContainerNames() {
		active[name] = true
	}
	// always_on sidecars (workers) keep running while their app is
	// idle-stopped, and an idle-stopped app is closed (no longer in the app
	// store): keep them as long as their app still exists. Superseded
	// versions are stopped at deploy commit, not by the sweeper
	appIds := map[string]bool{}
	if s.db != nil { // nil only in tests exercising the sweep loop itself
		apps, err := s.FilterApps("all", true)
		if err != nil {
			// Without the app list the keep filter cannot tell an always_on
			// sidecar of a live app from a stale one; skip this sweep
			// instead of stopping them
			return fmt.Errorf("listing apps for stale container sweep: %w", err)
		}
		for _, appInfo := range apps {
			appIds[string(appInfo.Id)] = true
		}
	}
	// Job containers run in the foreground for an active run record; one
	// whose run is no longer active belongs to a lost run and is stopped
	activeRuns := s.activeJobRunIds(ctx)
	keep := func(cont container.Container) bool {
		if runId := cont.LabelValue(container.LABEL_PREFIX + container.JobRunLabel); runId != "" {
			return activeRuns == nil || activeRuns[runId]
		}
		return cont.HasLabel(container.SidecarAlwaysOnLabelKey, "true") &&
			appIds[cont.LabelValue(container.LABEL_PREFIX+"app.id")]
	}
	return cleanupStaleContainers(ctx, s.Logger, manager, active, keep)
}

// cleanupStaleContainers stops the managed containers not in active; keep,
// when set, exempts containers by their labels
func cleanupStaleContainers(ctx context.Context, logger *types.Logger, manager staleContainerManager,
	active map[container.ContainerName]bool, keep func(container.Container) bool) error {
	containers, err := manager.ListOpenRunContainers(ctx)
	if err != nil {
		return err
	}

	var retErr error
	for _, cont := range containers {
		containerName := container.ContainerName(cont.Names)
		if containerName == "" {
			logger.Warn().Str("container_id", cont.ID).Msg("Skipping OpenRun managed container with no name")
			continue
		}
		if active[containerName] || (keep != nil && keep(cont)) {
			continue
		}

		logger.Info().Str("container", string(containerName)).Msg("Stopping stale OpenRun managed container")
		if err := manager.StopContainer(ctx, containerName); err != nil {
			retErr = errors.Join(retErr, err)
			logger.Error().Err(err).Str("container", string(containerName)).Msg("Error stopping stale OpenRun managed container")
		}
	}
	return retErr
}

// cleanupSupersededImages removes the generated images of app versions
// outside each app's container.retain_previous_images window, after the
// version cleanup has run. Docker/Podman only: on Kubernetes the images live
// in a registry with its own retention. Per app copy (prod, stage, preview)
// the kept images are the current version's, those of versions newer than
// the current one (present after a revert) and those of the
// retain_previous_images most recent older versions; the copies sharing a
// repository (stage and prod by default) pool their kept images. Apps whose
// current version has no recorded image (deployed before the name was
// recorded, image spec apps) take no part, and a repository used by a copy
// configured with -1 is left alone. Images pinned by operations in flight
// (see container.PinImage) and images referenced by any container are never
// removed. Best effort: failures are logged, not returned
func (s *Server) cleanupSupersededImages(ctx context.Context) {
	command := s.Config().System.ContainerCommand
	if command == "" || command == types.CONTAINER_KUBERNETES {
		return
	}
	manager := container.NewCommandCM(s.Logger, s.Config(), "", "")
	// The images to keep come from the versions in the database. A deploy
	// committing after that read protects its image with a pin only until
	// the operation is over, so the sweep is tied to the image generation
	// of the read (see container.ImageGeneration) and recomputed when a pin
	// was released in between
	const maxAttempts = 3
	for attempt := 1; ; attempt++ {
		generation := container.ImageGeneration()
		apps, err := s.db.GetAllAppsContext(ctx, true)
		if err != nil {
			s.Error().Err(err).Msg("error listing apps for the superseded image cleanup")
			return
		}
		keep, repos, err := supersededImageKeepSet(apps, func(appId types.AppId) (map[int]string, error) {
			return s.db.GetAppVersionImageNames(ctx, appId)
		})
		if err != nil {
			s.Error().Err(err).Msg("error computing retained app images")
			return
		}
		changed := false
		for repoId := range repos {
			err := manager.RemoveRepoImagesExcept(ctx, repoId, keep, generation)
			if errors.Is(err, container.ErrImagesChanged) {
				changed = true
				break
			}
			if err != nil {
				s.Error().Err(err).Msgf("error removing superseded images of %s", container.GenImageName(repoId, ""))
			}
		}
		if !changed {
			return
		}
		if attempt >= maxAttempts {
			s.Info().Msg("superseded image cleanup deferred, deploys in progress; the next cleanup pass retries")
			return
		}
	}
}

// supersededImageKeepSet returns the generated images to keep across all
// apps and the image repositories (by app id, see GenImageName) to sweep.
// versionImages returns the image recorded on each stored version of an app
// (see Metadata.GetAppVersionImageNames). The window of each app copy is
// count based over its stored versions, like the fs.retain_versions cleanup:
// the current version, every newer version and the RetainPreviousImages most
// recent older versions. A copy with RetainPreviousImages < 0 excludes its
// repository from the sweep.
func supersededImageKeepSet(apps []types.AppInfo,
	versionImages func(types.AppId) (map[int]string, error)) (map[container.ImageName]bool, map[types.AppId]bool, error) {
	keep := map[container.ImageName]bool{}
	repos := map[types.AppId]bool{}
	skip := map[types.AppId]bool{}
	for _, app := range apps {
		if app.IsDev || app.ImageName == "" {
			continue
		}
		repoId, ok := container.ImageRepoAppId(container.ImageName(app.ImageName))
		if !ok {
			continue
		}
		keep[container.ImageName(app.ImageName)] = true
		if app.RetainPreviousImages < 0 {
			skip[repoId] = true
			continue
		}
		repos[repoId] = true

		images, err := versionImages(app.Id)
		if err != nil {
			return nil, nil, fmt.Errorf("listing versions of app %s: %w", app.AppPathDomain, err)
		}
		var older []int
		for version, image := range images {
			if version < app.Version {
				older = append(older, version)
				continue
			}
			// The current version and newer ones (after a revert) are kept
			if image != "" {
				keep[container.ImageName(image)] = true
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(older)))
		for i, version := range older {
			if i >= app.RetainPreviousImages {
				break
			}
			if image := images[version]; image != "" {
				keep[container.ImageName(image)] = true
			}
		}
	}
	for repoId := range skip {
		delete(repos, repoId)
	}
	return keep, repos, nil
}
