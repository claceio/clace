// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/types"
)

// A containerized app, as the container spec produces it: the proxy and
// container config calls are allowed by the default server permissions, the
// secrets on the container config permission (when declared) are not
func writeSecretAccessTestApp(t *testing.T, secrets string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatalf("create app dir: %v", err)
	}
	containerPerm := ""
	if secrets != "" {
		containerPerm = `, ace.permission("container.in", "config", [container.AUTO], secrets=` + secrets + `)`
	}
	appStar := `load("proxy.in", "proxy")
load("container.in", "container")

app = ace.app("secret app",
    routes=[ace.proxy("/", proxy.config(container.URL))],
    container=container.config(container.AUTO, port=8080),
    permissions=[ace.permission("proxy.in", "config", [container.URL])` + containerPerm + `])
`
	if err := os.WriteFile(filepath.Join(dir, "app.star"), []byte(appStar), 0600); err != nil {
		t.Fatalf("write app.star: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Containerfile"), []byte("FROM busybox:1.36\nEXPOSE 8080\n"), 0600); err != nil {
		t.Fatalf("write Containerfile: %v", err)
	}
	return dir
}

// The default server permissions: the container config call is allowed
// with no secrets
func newSecretAccessTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	server, db, ctx := newApplyTestServer(t)
	_ = ctx
	if err := server.initAuditDB("sqlite:" + filepath.Join(t.TempDir(), "audit.db")); err != nil {
		t.Fatalf("init audit db: %v", err)
	}
	server.staticConfig.Permissions.Allow = []types.Permission{
		{Plugin: "proxy.in", Method: "config", Arguments: []string{"<CONTAINER_URL>"}},
		{Plugin: "container.in", Method: "config", Arguments: []string{"regex:.*"}, Secrets: [][]string{}},
	}
	return server, func() { db.Close() }
}

func approveResults(t *testing.T, resp *types.AppStagedUpdateResponse) []*types.ApproveResult {
	t.Helper()
	results, ok := resp.StagedUpdateResults.([]any)
	if !ok {
		t.Fatalf("staged update results = %T, want []any", resp.StagedUpdateResults)
	}
	ret := make([]*types.ApproveResult, 0, len(results))
	for _, r := range results {
		ar, ok := r.(*types.ApproveResult)
		if !ok {
			t.Fatalf("staged update result = %T, want *ApproveResult", r)
		}
		ret = append(ret, ar)
	}
	return ret
}

// An app declaring secret access the server config does not grant deploys
// without approval, as before, with the audit result saying the secrets are
// ungranted; approve records the access and reports it as a change
func TestDeployReportsUngrantedSecretsAndApproveRecordsThem(t *testing.T) {
	server, closeDB := newSecretAccessTestServer(t)
	defer closeDB()
	_, _, ctx := newApplyTestServer(t)

	created, err := server.CreateApp(ctx, "/secretapp", DeployOptions{}, &types.CreateAppRequest{SourceUrl: writeSecretAccessTestApp(t, `[["regex:.*"]]`)})
	if err != nil {
		t.Fatalf("create app without approve: %v", err)
	}
	ar := created.ApproveResults[0]
	if ar.NeedsApproval || !ar.Changed || ar.Approved {
		t.Fatalf("create audit: NeedsApproval=%v Changed=%v Approved=%v, want false/true/false", ar.NeedsApproval, ar.Changed, ar.Approved)
	}
	if len(ar.UngrantedSecrets) != 1 || ar.UngrantedSecrets[0].Plugin != "container.in" || ar.UngrantedSecrets[0].SecretsString() != "regex:.*" {
		t.Fatalf("create audit UngrantedSecrets = %+v, want the container.in config secrets", ar.UngrantedSecrets)
	}

	// (A reload without approve returns the same audit result for the CLI to
	// warn; reload needs container support, so that is covered by the
	// test_secret_access.yaml integration test)

	// A param referencing a secret warns, pointing at approve
	updated, err := server.UpdateAppParams(ctx, "/secretapp", DeployOptions{}, "MY_SECRET", `{{secret_from "db" "my_secret"}}`)
	if err != nil {
		t.Fatalf("update param: %v", err)
	}
	if len(updated.Warnings) != 1 || !strings.Contains(updated.Warnings[0], "the new value references secrets, but the app declares secret access which is not approved: container.in.config auto secrets=regex:.*") ||
		!strings.Contains(updated.Warnings[0], "openrun app approve /secretapp") {
		t.Fatalf("update param warnings = %q, want the ungranted secrets advisory", updated.Warnings)
	}
	// A param not referencing secrets on an app whose ungranted access
	// predates it does not warn
	updated, err = server.UpdateAppParams(ctx, "/secretapp", DeployOptions{}, "OTHER", "plain")
	if err != nil {
		t.Fatalf("update plain param: %v", err)
	}
	if len(updated.Warnings) != 0 {
		t.Fatalf("plain param warnings = %q, want none", updated.Warnings)
	}

	// Approve records the declared permissions and reports the change
	approved, err := server.ApproveApps(ctx, "/secretapp", DeployOptions{})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	results := approveResults(t, approved)
	if len(results) != 1 || !results[0].Approved || !results[0].Changed || results[0].NeedsApproval {
		t.Fatalf("approve results = %+v, want one approved changed result", results)
	}

	// Approved: the secret param no longer warns, a repeat approve is unchanged
	updated, err = server.UpdateAppParams(ctx, "/secretapp", DeployOptions{}, "MY_SECRET", `{{secret_from "db" "my_secret"}}`)
	if err != nil {
		t.Fatalf("update param after approve: %v", err)
	}
	if len(updated.Warnings) != 0 {
		t.Fatalf("warnings after approve = %q, want none", updated.Warnings)
	}
	approved, err = server.ApproveApps(ctx, "/secretapp", DeployOptions{})
	if err != nil {
		t.Fatalf("approve again: %v", err)
	}
	results = approveResults(t, approved)
	if len(results) != 1 || !results[0].Approved || results[0].Changed {
		t.Fatalf("repeat approve results = %+v, want approved and unchanged", results)
	}

	// Create with approve records the access, no secrets ungranted
	created, err = server.CreateApp(ctx, "/secretapp2", DeployOptions{Approve: true}, &types.CreateAppRequest{SourceUrl: writeSecretAccessTestApp(t, `[["regex:.*"]]`)})
	if err != nil {
		t.Fatalf("create app with approve: %v", err)
	}
	ar = created.ApproveResults[0]
	if ar.NeedsApproval || !ar.Changed || !ar.Approved {
		t.Fatalf("create --approve audit: NeedsApproval=%v Changed=%v Approved=%v, want false/true/true", ar.NeedsApproval, ar.Changed, ar.Approved)
	}
}

// A containerized app declaring no secret access: a param referencing a
// secret warns that the app has no access, and declaring the access through
// the params (the secrets param of spec based apps) warns that it needs
// approval even though the value itself references no secret
func TestParamUpdateWarnsOnMissingAndDeclaredSecretAccess(t *testing.T) {
	server, closeDB := newSecretAccessTestServer(t)
	defer closeDB()
	_, _, ctx := newApplyTestServer(t)

	// The app declares the container permission with the secrets param, as the specs do
	dir := writeSecretAccessTestApp(t, "param.secrets")
	if err := os.WriteFile(filepath.Join(dir, "params.star"), []byte(`param("secrets", type=LIST, default=[])`+"\n"), 0600); err != nil {
		t.Fatalf("write params.star: %v", err)
	}
	created, err := server.CreateApp(ctx, "/nosecrets", DeployOptions{}, &types.CreateAppRequest{SourceUrl: dir})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if ar := created.ApproveResults[0]; ar.NeedsApproval || len(ar.UngrantedSecrets) != 0 {
		t.Fatalf("create audit = %+v, want no approval and no ungranted secrets", ar)
	}

	updated, err := server.UpdateAppParams(ctx, "/nosecrets", DeployOptions{}, "MY_SECRET", `{{secret "my_secret"}}`)
	if err != nil {
		t.Fatalf("update param: %v", err)
	}
	if len(updated.Warnings) != 1 || !strings.Contains(updated.Warnings[0], "app /nosecrets: the new value references secrets, but the app has no secret access for container.in.config") ||
		!strings.Contains(updated.Warnings[0], "--param secrets=") {
		t.Fatalf("update param warnings = %q, want the no secret access advisory", updated.Warnings)
	}

	// Declaring the access through the secrets param: ungranted until approved
	updated, err = server.UpdateAppParams(ctx, "/nosecrets", DeployOptions{}, "secrets", `[["my_secret"]]`)
	if err != nil {
		t.Fatalf("update secrets param: %v", err)
	}
	if len(updated.Warnings) != 1 || !strings.Contains(updated.Warnings[0], "app /nosecrets: the app declares secret access which is not approved: container.in.config auto secrets=my_secret") {
		t.Fatalf("secrets param warnings = %q, want the ungranted secrets advisory", updated.Warnings)
	}

	if _, err := server.ApproveApps(ctx, "/nosecrets", DeployOptions{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	updated, err = server.UpdateAppParams(ctx, "/nosecrets", DeployOptions{}, "MY_SECRET", `{{secret "my_secret"}}`)
	if err != nil {
		t.Fatalf("update param after approve: %v", err)
	}
	if len(updated.Warnings) != 0 {
		t.Fatalf("warnings after approve = %q, want none", updated.Warnings)
	}
}
