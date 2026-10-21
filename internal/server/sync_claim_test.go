// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/types"
)

// TestSyncReplacementClaimRefreshesAppInfo: a sync deleted and created again
// claims the apps it finds up to date (#111). The claim is a metadata-only
// write, so the in-memory app info (what list_apps and the console read)
// must be refreshed on commit like any other app change: before #115 it kept
// the deleted entry's id until a server reload and the console listed the
// apps as unmanaged
func TestSyncReplacementClaimRefreshesAppInfo(t *testing.T) {
	server, db, ctx := newApplyTestServer(t)
	defer db.Close()
	// The cache clear on commit audits the cleared apps
	if err := server.initAuditDB("sqlite:" + filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatalf("init audit db: %v", err)
	}

	applyPath := filepath.Join(t.TempDir(), "sync.ace")
	appSourceDir := filepath.Join(t.TempDir(), "app")
	if err := os.Mkdir(appSourceDir, 0700); err != nil {
		t.Fatalf("create app source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appSourceDir, "app.star"), []byte("app = ace.app(\"syncApp\")\n"), 0600); err != nil {
		t.Fatalf("write app.star: %v", err)
	}
	if err := os.WriteFile(applyPath, []byte(fmt.Sprintf("app(\"/apps/sync-claim\", %q)\n", appSourceDir)), 0600); err != nil {
		t.Fatalf("write apply file: %v", err)
	}

	// The stage app carries the sync state (prod picks it up on promote)
	cachedStage := func() types.AppInfo {
		t.Helper()
		apps, err := server.apps.GetAllAppsInfo()
		if err != nil {
			t.Fatalf("get all apps info: %v", err)
		}
		for _, app := range apps {
			if strings.HasPrefix(string(app.Id), types.ID_PREFIX_APP_STAGE) && app.MainApp != "" {
				return app
			}
		}
		t.Fatal("stage app not found in app info")
		return types.AppInfo{}
	}
	cachedStageSyncId := func() string { return cachedStage().AppliedSyncId }

	first, err := server.CreateSyncEntry(ctx, applyPath, SyncCreateOptions{Scheduled: true}, &types.SyncMetadata{})
	if err != nil {
		t.Fatalf("create first sync entry: %v", err)
	}
	if got := cachedStageSyncId(); got != first.Id {
		t.Fatalf("stage app sync id after create = %q, want %q", got, first.Id)
	}

	// Replace the sync: delete it and schedule the same file again. The new
	// entry finds the app up to date and claims it without a new version
	if _, err := server.DeleteSyncEntry(ctx, first.Id, false); err != nil {
		t.Fatalf("delete first sync entry: %v", err)
	}
	second, err := server.CreateSyncEntry(ctx, applyPath, SyncCreateOptions{Scheduled: true}, &types.SyncMetadata{})
	if err != nil {
		t.Fatalf("create second sync entry: %v", err)
	}
	if second.Id == first.Id {
		t.Fatalf("second sync entry reused id %q", first.Id)
	}
	applyResp := second.SyncJobStatus.ApplyResponse
	if len(applyResp.CreateResults) != 0 || len(applyResp.UpdateResults) != 0 || len(applyResp.ReloadResults) != 0 {
		t.Fatalf("second sync changed the app: %+v", applyResp)
	}

	// The database has the new id (#111) and so does the cached app info (#115)
	entry, err := db.GetAppEntry(ctx, cachedStage().AppPathDomain)
	if err != nil {
		t.Fatalf("get stage app entry: %v", err)
	}
	if entry.Metadata.AppliedSyncId != second.Id {
		t.Fatalf("stored stage app sync id = %q, want %q", entry.Metadata.AppliedSyncId, second.Id)
	}
	if got := cachedStageSyncId(); got != second.Id {
		t.Fatalf("cached stage app sync id = %q, want %q: the claim did not refresh the app info cache", got, second.Id)
	}
}
