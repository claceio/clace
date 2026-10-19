// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
)

// writeDiskApp writes a minimal app source dir and an apply file declaring
// it at appPath, returning both paths
func writeDiskApp(t *testing.T, appPath string) (sourceDir string, applyFile string) {
	t.Helper()
	dir := t.TempDir()
	sourceDir = filepath.Join(dir, "app")
	if err := os.Mkdir(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "app.star"), []byte("app = ace.app(\"diskApp\")\n"), 0600); err != nil {
		t.Fatal(err)
	}
	applyFile = filepath.Join(dir, "apps.ace")
	if err := os.WriteFile(applyFile, []byte("app(\""+appPath+"\", \""+sourceDir+"\")\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return sourceDir, applyFile
}

func assertDiskRefused(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "unsafe_enable_remote_disk_read") {
		t.Fatalf("%s: expected the disk path to be refused for a remote caller, got %v", what, err)
	}
}

// The remote REST surface refuses server disk paths everywhere one is
// accepted: app source, apply file (apply and apply delete), sync path and
// pretty-print input. The unsafe flag lifts the refusal
func TestRemoteApiDiskSourcesRefused(t *testing.T) {
	server, ts, mintKey := newRemoteApiTestServer(t)
	// admin: pretty_print needs config:read, the others are granted to alice too
	alice := remoteClient(ts, mintKey(t, &types.ApiKeyCreateRequest{User: "admin"}))
	sourceDir, applyFile := writeDiskApp(t, "/apps/disk")

	var createResp types.AppCreateResponse
	err := alice.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/disk", SourceUrl: sourceDir}, &createResp)
	assertDiskRefused(t, "create_app", err)

	var applyResp types.AppApplyResponse
	err = alice.Post("/_openrun/apply", url.Values{"applyPath": {applyFile}, "appPathGlob": {"all"}}, nil, &applyResp)
	assertDiskRefused(t, "apply", err)

	var deleteResp types.ApplyDeleteResponse
	err = alice.Delete("/_openrun/apply", url.Values{"applyPath": {applyFile}, "appPathGlob": {"all"}}, &deleteResp)
	assertDiskRefused(t, "apply_delete", err)

	var syncResp types.SyncCreateResponse
	err = alice.Post("/_openrun/sync", url.Values{"path": {applyFile}, "scheduled": {"true"}},
		&types.SyncMetadata{ScheduleFrequency: 10}, &syncResp)
	assertDiskRefused(t, "sync_create", err)

	var prettyResp types.AppExportResponse
	err = alice.Get("/_openrun/pretty_print", url.Values{"applyPath": {applyFile}}, &prettyResp)
	assertDiskRefused(t, "pretty_print", err)

	// An empty source is not a loophole: it would resolve to the server's
	// working directory, so it is rejected outright
	err = alice.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/disk"}, &createResp)
	if err == nil || !strings.Contains(err.Error(), "source url is required") {
		t.Fatalf("empty source: expected a rejection, got %v", err)
	}

	// Nothing was created along the way
	if _, exists, err := server.findAppInfo(types.AppPathDomain{Domain: "127.0.0.1", Path: "/apps/disk"}); err != nil || exists {
		t.Fatalf("app must not exist after refused creates: exists=%v err=%v", exists, err)
	}

	// Directories under security.remote_disk_read_paths are exempt: the app
	// source and the apply file inside the root are accepted, a sibling
	// outside it and a symlink inside it pointing outside are not
	root := t.TempDir()
	insideDir := filepath.Join(root, "app")
	if err := os.Mkdir(insideDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(insideDir, "app.star"), []byte("app = ace.app(\"insideApp\")\n"), 0600); err != nil {
		t.Fatal(err)
	}
	insideApply := filepath.Join(root, "apps.ace")
	if err := os.WriteFile(insideApply, []byte("app(\"/apps/inside\", \""+insideDir+"\")\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(sourceDir), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	server.staticConfig.Security.RemoteDiskReadPaths = []string{root}
	t.Cleanup(func() { server.staticConfig.Security.RemoteDiskReadPaths = nil })
	if err := alice.Get("/_openrun/pretty_print", url.Values{"applyPath": {insideApply}}, &prettyResp); err != nil {
		t.Fatalf("pretty_print under remote_disk_read_paths: %v", err)
	}
	if err := alice.Post("/_openrun/apply", url.Values{"applyPath": {insideApply}, "appPathGlob": {"all"}}, nil, &applyResp); err != nil {
		t.Fatalf("apply under remote_disk_read_paths: %v", err)
	}
	if len(applyResp.CreateResults) != 1 || applyResp.CreateResults[0].AppPathDomain.Path != "/apps/inside" {
		t.Fatalf("apply under the allowed root must create the declared app, got %+v", applyResp.CreateResults)
	}
	err = alice.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/disk", SourceUrl: sourceDir}, &createResp)
	assertDiskRefused(t, "create_app outside remote_disk_read_paths", err)
	err = alice.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/disk", SourceUrl: filepath.Join(root, "escape", "app")}, &createResp)
	assertDiskRefused(t, "create_app through a symlink out of remote_disk_read_paths", err)
	server.staticConfig.Security.RemoteDiskReadPaths = nil

	// The unsafe flag restores disk access for remote callers
	server.staticConfig.Security.UnsafeEnableRemoteDiskRead = true
	t.Cleanup(func() { server.staticConfig.Security.UnsafeEnableRemoteDiskRead = false })
	if err := alice.Get("/_openrun/pretty_print", url.Values{"applyPath": {applyFile}}, &prettyResp); err != nil {
		t.Fatalf("pretty_print with the flag set: %v", err)
	}
	if err := alice.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/disk", SourceUrl: sourceDir}, &createResp); err != nil {
		t.Fatalf("create_app with the flag set: %v", err)
	}
}

// The unix domain socket surface accepts disk paths with the flag off
func TestUDSDiskSourcesAllowed(t *testing.T) {
	server, _, _ := newRemoteApiTestServer(t)
	udsHandler := NewUDSHandler(server.Logger, server.staticConfig, server)
	uds := httptest.NewServer(udsHandler.router)
	t.Cleanup(uds.Close)
	client := system.NewHttpClient(uds.URL, "", false)
	sourceDir, applyFile := writeDiskApp(t, "/apps/udsdisk")

	var prettyResp types.AppExportResponse
	if err := client.Get("/_openrun/pretty_print", url.Values{"applyPath": {applyFile}}, &prettyResp); err != nil {
		t.Fatalf("pretty_print over uds: %v", err)
	}
	var createResp types.AppCreateResponse
	if err := client.Post("/_openrun/app", nil, &types.CreateAppRequest{Path: "/apps/udsdisk", SourceUrl: sourceDir}, &createResp); err != nil {
		t.Fatalf("create_app over uds: %v", err)
	}
}

// In-process plugin calls (the console) carry an RBAC enforced context with
// no transport marker: they are remote callers for disk paths. Git urls and
// the no-source marker are never gated
func TestPluginContextDiskSourceRefused(t *testing.T) {
	server, db, ctx := newApplyTestServer(t)
	t.Cleanup(func() { db.Close() })
	sourceDir, _ := writeDiskApp(t, "/plugindisk")

	pluginCtx := &managementAPIContext{Context: context.Background(), userId: types.ADMIN_USER, rbacEnabled: true}
	_, err := server.CreateApp(pluginCtx, "/plugindisk", DeployOptions{}, &types.CreateAppRequest{Path: "/plugindisk", SourceUrl: sourceDir})
	assertDiskRefused(t, "plugin create_app", err)
	testutil.AssertNoError(t, server.checkDiskSource(pluginCtx, "github.com/openrundev/apps/misc/disk_usage", "app source"))
	testutil.AssertNoError(t, server.checkDiskSource(pluginCtx, "https://github.com/openrundev/apps", "app source"))
	testutil.AssertNoError(t, server.checkDiskSource(pluginCtx, types.NO_SOURCE, "app source"))

	// An explicit decision on the context (server-owned flows) allows the path
	if _, err := server.CreateApp(system.WithDiskSourceAccess(pluginCtx, true), "/plugindisk", DeployOptions{},
		&types.CreateAppRequest{Path: "/plugindisk", SourceUrl: sourceDir}); err != nil {
		t.Fatalf("create with disk access: %v", err)
	}
	// The trusted context of the test server (UDS semantics) is allowed
	testutil.AssertNoError(t, server.checkDiskSource(ctx, sourceDir, "app source"))
}

// A sync entry's runs read disk paths with the creator's standing: a locally
// created entry runs from anywhere, a remotely created one is refused even
// under the scheduler's trusted background context, unless the caller of a
// manual run has disk access of its own
func TestSyncRunContextDiskAccess(t *testing.T) {
	server, db, ctx := newApplyTestServer(t)
	t.Cleanup(func() { db.Close() })
	_, applyFile := writeDiskApp(t, "/syncdisk")

	local := &types.SyncEntry{Path: applyFile}
	remote := &types.SyncEntry{Path: applyFile, Metadata: types.SyncMetadata{RemoteCreated: true}}
	scheduler := backgroundOperationContext(context.Background(), "scheduler")
	// admin over the remote API: RBAC passes, the transport decides disk access
	restCaller := &managementAPIContext{Context: context.Background(), userId: types.ADMIN_USER, rbacEnabled: true}
	udsCaller := system.WithDiskSourceAccess(ctx, true)

	testutil.AssertNoError(t, server.checkDiskSource(server.syncRunContext(scheduler, local), applyFile, "apply file"))
	testutil.AssertNoError(t, server.checkDiskSource(server.syncRunContext(restCaller, local), applyFile, "apply file"))
	assertDiskRefused(t, "scheduled run of a remote entry", server.checkDiskSource(server.syncRunContext(scheduler, remote), applyFile, "apply file"))
	assertDiskRefused(t, "remote manual run of a remote entry", server.checkDiskSource(server.syncRunContext(restCaller, remote), applyFile, "apply file"))
	testutil.AssertNoError(t, server.checkDiskSource(server.syncRunContext(udsCaller, remote), applyFile, "apply file"))

	// The creator's standing is recorded on create: the trusted test
	// context (UDS semantics) is local, an enforced context without the
	// marker is remote
	if callerHasDiskAccess(restCaller) {
		t.Fatal("an enforced context without a transport marker must not have disk access")
	}
	if !callerHasDiskAccess(ctx) || !callerHasDiskAccess(udsCaller) {
		t.Fatal("trusted and uds contexts must have disk access")
	}
	resp, err := server.CreateSyncEntry(ctx, applyFile, SyncCreateOptions{Scheduled: true}, &types.SyncMetadata{ScheduleFrequency: 10})
	testutil.AssertNoError(t, err)
	entries, err := server.ListSyncEntries(ctx)
	testutil.AssertNoError(t, err)
	found := false
	for _, entry := range entries.Entries {
		if entry.Id == resp.Id {
			found = true
			if entry.Metadata.RemoteCreated {
				t.Fatal("entry created over a trusted context must not be marked remote created")
			}
		}
	}
	if !found {
		t.Fatalf("sync entry %s not listed", resp.Id)
	}
	// A remote creator cannot schedule a disk path at all
	_, err = server.CreateSyncEntry(restCaller, applyFile, SyncCreateOptions{Scheduled: true}, &types.SyncMetadata{ScheduleFrequency: 10})
	assertDiskRefused(t, "remote sync create", err)
}
