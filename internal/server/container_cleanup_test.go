// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/container"
	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
)

type fakeStaleContainerManager struct {
	containers []container.Container
	stopped    []container.ContainerName
	stopErr    error
}

func (f *fakeStaleContainerManager) ListOpenRunContainers(context.Context) ([]container.Container, error) {
	return f.containers, nil
}

func (f *fakeStaleContainerManager) StopContainer(_ context.Context, name container.ContainerName) error {
	f.stopped = append(f.stopped, name)
	return f.stopErr
}

func TestCleanupStaleContainersStopsOnlyUnreferencedContainers(t *testing.T) {
	manager := &fakeStaleContainerManager{
		containers: []container.Container{
			{ID: "1", Names: "clc-active", State: "running"},
			{ID: "2", Names: "clc-stale", State: "running"},
			{ID: "3", State: "running"},
		},
	}

	err := cleanupStaleContainers(context.Background(), testutil.TestLogger(), manager, map[container.ContainerName]bool{
		"clc-active": true,
	}, nil)
	if err != nil {
		t.Fatalf("cleanupStaleContainers returned error: %v", err)
	}

	want := []container.ContainerName{"clc-stale"}
	if !slices.Equal(manager.stopped, want) {
		t.Fatalf("stopped containers = %#v, want %#v", manager.stopped, want)
	}
}

func TestCleanupStaleContainersKeepsAlwaysOnSidecars(t *testing.T) {
	// An idle-stopped app is closed (not in the active set); its always_on
	// worker sidecar stays up while the app exists, a deleted app's does not
	manager := &fakeStaleContainerManager{
		containers: []container.Container{
			{ID: "1", Names: "clc-app1-abcd-worker", State: "running",
				LabelString: container.SidecarAlwaysOnLabelKey + "=true," + container.LABEL_PREFIX + "app.id=app1"},
			{ID: "2", Names: "clc-app1-abcd-cache", State: "running",
				LabelString: container.LABEL_PREFIX + "app.id=app1"},
			{ID: "3", Names: "clc-gone-abcd-worker", State: "running",
				LabelString: container.SidecarAlwaysOnLabelKey + "=true," + container.LABEL_PREFIX + "app.id=gone"},
		},
	}
	existing := map[string]bool{"app1": true}
	keep := func(cont container.Container) bool {
		return cont.HasLabel(container.SidecarAlwaysOnLabelKey, "true") &&
			existing[cont.LabelValue(container.LABEL_PREFIX+"app.id")]
	}
	if err := cleanupStaleContainers(context.Background(), testutil.TestLogger(), manager, nil, keep); err != nil {
		t.Fatalf("cleanupStaleContainers returned error: %v", err)
	}
	want := []container.ContainerName{"clc-app1-abcd-cache", "clc-gone-abcd-worker"}
	if !slices.Equal(manager.stopped, want) {
		t.Fatalf("stopped containers = %#v, want %#v", manager.stopped, want)
	}
}

func TestCleanupStaleContainersReturnsStopErrors(t *testing.T) {
	stopErr := errors.New("stop failed")
	manager := &fakeStaleContainerManager{
		containers: []container.Container{{ID: "1", Names: "clc-stale", State: "running"}},
		stopErr:    stopErr,
	}

	err := cleanupStaleContainers(context.Background(), testutil.TestLogger(), manager, nil, nil)
	if !errors.Is(err, stopErr) {
		t.Fatalf("cleanupStaleContainers error = %v, want %v", err, stopErr)
	}
}

func TestSupersededImageKeepSetRetainsWindowPerCopyAndPoolsSharedRepo(t *testing.T) {
	apps := []types.AppInfo{
		{Id: "app_prd_shared", IsDev: false, Version: 5, RetainPreviousImages: 1, ImageName: "cli-shared:v5"},
		{Id: "app_stg_shared", IsDev: false, Version: 6, RetainPreviousImages: 1, ImageName: "cli-shared:v6"},
		{Id: "app_dev_x", IsDev: true, Version: 1, RetainPreviousImages: 1, ImageName: "cli-app_dev_x:dev-1"},
		{Id: "app_prd_legacy", Version: 3, RetainPreviousImages: 1, ImageName: ""},
		{Id: "app_prd_keepall", Version: 2, RetainPreviousImages: -1, ImageName: "cli-keepall:v2"},
		{Id: "app_stg_keepall", Version: 2, RetainPreviousImages: 1, ImageName: "cli-keepall:v2"},
		{Id: "app_prd_zero", Version: 4, RetainPreviousImages: 0, ImageName: "cli-zero:v4"},
	}
	versions := map[types.AppId]map[int]string{
		// 7 is newer than current (revert); 4 is the most recent older
		// version and has no recorded image, consuming the one slot
		"app_prd_shared":  {1: "cli-shared:v1", 3: "cli-shared:v3", 4: "", 5: "cli-shared:v5", 7: "cli-shared:v7"},
		"app_stg_shared":  {5: "cli-shared:v5", 6: "cli-shared:v6"},
		"app_stg_keepall": {1: "cli-keepall:v1", 2: "cli-keepall:v2"},
		"app_prd_zero":    {3: "cli-zero:v3", 4: "cli-zero:v4"},
	}
	var listed []types.AppId
	keep, repos, err := supersededImageKeepSet(apps,
		func(appId types.AppId) (map[int]string, error) {
			listed = append(listed, appId)
			v, ok := versions[appId]
			if !ok {
				return nil, errors.New("unexpected app " + string(appId))
			}
			return v, nil
		})
	if err != nil {
		t.Fatalf("supersededImageKeepSet returned error: %v", err)
	}

	wantKeep := map[container.ImageName]bool{
		"cli-shared:v5":  true,
		"cli-shared:v6":  true,
		"cli-shared:v7":  true,
		"cli-keepall:v2": true,
		"cli-keepall:v1": true,
		"cli-zero:v4":    true,
	}
	if len(keep) != len(wantKeep) {
		t.Fatalf("keep = %v, want %v", keep, wantKeep)
	}
	for name := range wantKeep {
		if !keep[name] {
			t.Fatalf("keep = %v, want %v", keep, wantKeep)
		}
	}
	wantRepos := map[types.AppId]bool{"shared": true, "zero": true}
	if len(repos) != len(wantRepos) || !repos["shared"] || !repos["zero"] {
		t.Fatalf("repos = %v, want %v", repos, wantRepos)
	}
	// Dev apps, apps without a recorded image and -1 copies are not listed
	for _, id := range listed {
		if id == "app_dev_x" || id == "app_prd_legacy" || id == "app_prd_keepall" {
			t.Fatalf("versions listed for %s", id)
		}
	}
}

func TestSupersededImageKeepSetReturnsVersionListErrors(t *testing.T) {
	apps := []types.AppInfo{{Id: "app_prd_x", Version: 1, ImageName: "cli-x:v1"}}
	_, _, err := supersededImageKeepSet(apps, func(types.AppId) (map[int]string, error) {
		return nil, errors.New("db down")
	})
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want version list error", err)
	}
}
