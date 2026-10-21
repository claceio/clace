// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
)

func TestHttpPluginBasics(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "test contents") //nolint:errcheck
	}))

	testServerJson := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"key": "value"}`) //nolint:errcheck
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `

load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/")],
    permissions=[
	ace.permission("http.in", "get"),
	ace.permission("http.in", "post"),
	ace.permission("http.in", "delete"),
	ace.permission("http.in", "put"),
	ace.permission("http.in", "patch"),
	ace.permission("http.in", "head"),
	ace.permission("http.in", "options"),
	])

def handler(req):
	resp1 = http.get("` + testServer.URL + `?a=b", headers={"X-Test": "test"}, params={"c": "d"}, auth_basic=("user", "pass"))
	resp2 = http.post("` + testServer.URL + `")
	resp3 = http.delete("` + testServer.URL + `")
	resp4 = http.put("` + testServer.URL + `")
	resp5 = http.patch("` + testServer.URL + `")
	resp6 = http.head("` + testServer.URL + `")
	resp7 = http.options("` + testServer.URL + `")
	resp8 = http.get("` + testServerJson.URL + `")

	return {
		"key1": resp1.value.body(),
		"key2": resp2.value.body(),
		"key3": resp3.value.body(),
		"key4": resp4.value.body(),
		"key5": resp5.value.body(),
		"key6": resp6.value.body(),
		"key7": resp7.value.body(),
		"key8": resp8.value.json(),
		}
`,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData,
		[]string{"http.in"},
		[]types.Permission{
			{Plugin: "http.in", Method: "get"},
			{Plugin: "http.in", Method: "post"},
			{Plugin: "http.in", Method: "delete"},
			{Plugin: "http.in", Method: "put"},
			{Plugin: "http.in", Method: "patch"},
			{Plugin: "http.in", Method: "head"},
			{Plugin: "http.in", Method: "options"},
		},
		nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	ret := make(map[string]any)
	json.UnmarshalRead(response.Body, &ret) //nolint:errcheck
	testutil.AssertEqualsString(t, "body", "test contents", ret["key1"].(string))
	testutil.AssertEqualsString(t, "body", "test contents", ret["key2"].(string))
	testutil.AssertEqualsString(t, "body", "test contents", ret["key3"].(string))
	testutil.AssertEqualsString(t, "body", "test contents", ret["key4"].(string))
	testutil.AssertEqualsString(t, "body", "test contents", ret["key5"].(string))
	testutil.AssertEqualsString(t, "body", "", ret["key6"].(string))
	testutil.AssertEqualsString(t, "body", "test contents", ret["key7"].(string))
	testutil.AssertEqualsString(t, "body", `value`, ret["key8"].(map[string]any)["key"].(string))
}

func TestHttpPluginPermit(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "test contents") //nolint:errcheck
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", routes = [ace.api("/", type=ace.TEXT)],
    permissions=[
	ace.permission("http.in", "get", permit=["net:read"]),
	])

def handler(req):
	return http.get("` + testServer.URL + `").value.body()
`,
	}

	perms := []types.Permission{
		{Plugin: "http.in", Method: "get", Permit: []string{"net:read"}},
	}

	a, _, err := CreateTestAppAuthorizer(logger, fileData, []string{"http.in"}, perms, nil, &testRBAC{perms: []string{"net:read"}})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	// RBAC_ENABLED is set in the request context as the server middleware does
	// for every app request; the permit gate reads that per-request value
	request := httptest.NewRequest("GET", "/test", nil)
	ctx := context.WithValue(request.Context(), types.USER_ID, "reader@example.com")
	ctx = context.WithValue(ctx, types.RBAC_ENABLED, true)
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())

	a, _, err = CreateTestAppAuthorizer(logger, fileData, []string{"http.in"}, perms, nil, &testRBAC{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request = httptest.NewRequest("GET", "/test", nil)
	ctx = context.WithValue(request.Context(), types.USER_ID, "blocked@example.com")
	ctx = context.WithValue(ctx, types.RBAC_ENABLED, true)
	request = request.WithContext(ctx)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "user blocked@example.com does not have any required permission [net:read]")

	// RBAC disabled: no RBAC_ENABLED in the context, permit gate is inactive
	a, _, err = CreateTestAppAuthorizer(logger, fileData, []string{"http.in"}, perms, nil, &testRBAC{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request = httptest.NewRequest("GET", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())
}

func TestSecretsConfig(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "test contents") //nolint:errcheck
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `

load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/")],
    permissions=[
	ace.permission("http.in", "get", secrets=["abc"]),
	])

def handler(req):
	resp1 = http.get("` + testServer.URL + `?a=b", headers={"X-Test": "test"}, params={"c": "d"}, auth_basic=("user", "pass"))

	return {
		"key1": resp1.value.body(),
		}
`,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData,
		[]string{"http.in"},
		[]types.Permission{
			{Plugin: "http.in", Method: "get"},
		},
		nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	_, err = a.Audit()
	testutil.AssertErrorContains(t, err, "entry 0 in secrets list is not a list")
}

func TestSecretsAccess(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "test contents") //nolint:errcheck
	}))

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `

load ("http.in", "http")


def test1(req):
	resp = http.get("` + testServer.URL + `?a=b", headers={"X-Test": '{{ secret_from "env" "abc"}}'}, params={"c": "d"}, auth_basic=("user", "pass"))
	return resp.value.body()

def test2(req):
	resp = http.get("` + testServer.URL + `?a=b", headers={"X-Test": '{{ secret_from "env" "abc2"}}'}, params={"c": "d"}, auth_basic=("user", "pass"))
	return resp.value.body()

app = ace.app("testApp", custom_layout=True, routes = [ace.api("/api1",  handler=test1, type=ace.TEXT), ace.api("/api2", handler=test2, type=ace.TEXT)],
    permissions=[
	ace.permission("http.in", "get", secrets=[["abc"]]),
	])
`,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData,
		[]string{"http.in"},
		[]types.Permission{
			{Plugin: "http.in", Method: "get", Secrets: [][]string{{"abc"}}},
		},
		nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	_, err = a.Audit()
	testutil.AssertNoError(t, err)

	request := httptest.NewRequest("GET", "/test/api1", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertEqualsString(t, "body", "test contents", response.Body.String())

	request = httptest.NewRequest("GET", "/test/api2", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "error calling secret_from: the plugin call is not allowed to access the secret: secret abc2 is not in the allowed patterns")
}

func TestHttpPluginDeferredCleanupWithoutBodyRead(t *testing.T) {
	done := make(chan struct{}, 1)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, "test contents"); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		<-r.Context().Done()
		done <- struct{}{}
	}))
	defer testServer.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `

load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/")],
    permissions=[
	ace.permission("http.in", "get"),
	])

def handler(req):
	resp = http.get("` + testServer.URL + `")
	return {
		"status_code": resp.value.status_code,
	}
`,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData,
		[]string{"http.in"},
		[]types.Permission{
			{Plugin: "http.in", Method: "get"},
		},
		nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)

	ret := make(map[string]any)
	json.UnmarshalRead(response.Body, &ret) //nolint:errcheck
	testutil.AssertEqualsInt(t, "status_code", 200, int(ret["status_code"].(float64)))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected deferred cleanup to close the HTTP response body")
	}
}

// TestHttpPluginRedirectChecked verifies that a redirect target is checked
// against the disallow list and the approved http.in permissions before it is
// followed (GHSA-qgqh-4fgp-rf5c)
func TestHttpPluginRedirectChecked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "target contents") //nolint:errcheck
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/internal", http.StatusFound)
	}))
	defer redirector.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "get")])

def handler(req):
	return http.get("` + redirector.URL + `/start").value.body()
`,
	}

	tests := []struct {
		name     string
		perms    []types.Permission
		disallow []types.Permission
		code     int
		contains string
	}{
		{"redirect followed when the target is permitted",
			[]types.Permission{{Plugin: "http.in", Method: "get"}}, nil, 200, "target contents"},
		{"redirect refused by the disallow list",
			[]types.Permission{{Plugin: "http.in", Method: "get"}},
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + target.URL}}},
			500, "not followed: app /test is not permitted to call http.in.get: the target is disallowed by the server config (permissions.disallow)"},
		{"redirect refused by the approved argument pattern",
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + redirector.URL}}},
			nil, 500, "not followed: app /test is not permitted to call http.in.get: argument 0 does not match the approved pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &types.ServerConfig{}
			sc.Permissions.Disallow = tt.disallow
			a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, tt.perms, sc)
			if err != nil {
				t.Fatalf("Error %s", err)
			}
			request := httptest.NewRequest("GET", "/test", nil)
			response := httptest.NewRecorder()
			a.ServeHTTP(response, request)
			testutil.AssertEqualsInt(t, "code", tt.code, response.Code)
			testutil.AssertStringContains(t, response.Body.String(), tt.contains)
		})
	}
}

// TestHttpDefaultContainerRuleCovers verifies that the default server config
// http.in entry (method "regex:.*", arguments ["regex:^<CONTAINER_URL>"])
// covers an app's own-container http call at audit time, so the app deploys
// without an approval, while a call to another host still needs one. Before
// the method accepted a regex: pattern the entry never matched
func TestHttpDefaultContainerRuleCovers(t *testing.T) {
	logger := testutil.TestLogger()
	appFile := func(perm string) map[string]string {
		return map[string]string{
			"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[` + perm + `])

def handler(req):
	return "ok"
`,
		}
	}
	sc := &types.ServerConfig{}
	sc.Permissions.Allow = []types.Permission{
		{Plugin: "http.in", Method: "regex:.*", Arguments: []string{"regex:^<CONTAINER_URL>"}, Secrets: [][]string{}},
	}

	tests := []struct {
		name          string
		perm          string
		needsApproval bool
	}{
		{"own container call covered", `ace.permission("http.in", "get", arguments=["<CONTAINER_URL>"])`, false},
		{"own container path covered", `ace.permission("http.in", "post", arguments=["<CONTAINER_URL>/api"])`, false},
		{"other host needs approval", `ace.permission("http.in", "get", arguments=["https://api.example.com"])`, true},
		{"unrestricted url needs approval", `ace.permission("http.in", "get")`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, err := CreateTestAppPluginServerConfig(logger, appFile(tt.perm), []string{"http.in"}, nil, sc)
			if err != nil {
				t.Fatalf("Error %s", err)
			}
			result, err := a.Audit()
			testutil.AssertNoError(t, err)
			testutil.AssertEqualsBool(t, "needs approval", tt.needsApproval, result.NeedsApproval)
		})
	}
}

// TestHttpPluginRedirectEffectiveMethod verifies that a redirect target is
// checked under the method of the redirected request: a 303 after a post is
// a get, so an app approved for post on one url and get on another can
// follow it, and an app approved for post only cannot
func TestHttpPluginRedirectEffectiveMethod(t *testing.T) {
	var result *httptest.Server
	result = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, result.URL+"/result", http.StatusSeeOther)
			return
		}
		io.WriteString(w, r.Method+" result") //nolint:errcheck
	}))
	defer result.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "post"), ace.permission("http.in", "get")])

def handler(req):
	return http.post("` + result.URL + `/start", body="x").value.body()
`,
	}
	tests := []struct {
		name     string
		perms    []types.Permission
		code     int
		contains string
	}{
		{"post then get both approved",
			[]types.Permission{
				{Plugin: "http.in", Method: "post", Arguments: []string{"regex:^" + result.URL + "/start$"}},
				{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + result.URL + "/result$"}},
			}, 200, "GET result"},
		{"get not approved",
			[]types.Permission{
				{Plugin: "http.in", Method: "post", Arguments: []string{"regex:^" + result.URL}},
			}, 500, "not followed: app /test is not permitted to call http.in.get"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, tt.perms, &types.ServerConfig{})
			if err != nil {
				t.Fatalf("Error %s", err)
			}
			request := httptest.NewRequest("GET", "/test", nil)
			response := httptest.NewRecorder()
			a.ServeHTTP(response, request)
			testutil.AssertEqualsInt(t, "code", tt.code, response.Code)
			testutil.AssertStringContains(t, response.Body.String(), tt.contains)
		})
	}
}

// TestHttpPluginResolvedSecretNotInError verifies that when a resolved secret
// makes an argument fail the approved pattern, the error does not reveal the
// secret value
func TestHttpPluginResolvedSecretNotInError(t *testing.T) {
	t.Setenv("HOSTSECRET", "evil.example")
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "get", arguments=["regex:^https://api\\.example\\.com/[^.]*$"], secrets=[["HOSTSECRET"]])])

def handler(req):
	# The raw text matches the pattern (no "." after the host), the
	# resolved path does not
	return http.get('https://api.example.com/{{secret "HOSTSECRET"}}').value.body()
`,
	}
	perms := []types.Permission{{Plugin: "http.in", Method: "get",
		Arguments: []string{`regex:^https://api\.example\.com/[^.]*$`}, Secrets: [][]string{{"HOSTSECRET"}}}}
	a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, perms, &types.ServerConfig{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}
	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	body := response.Body.String()
	testutil.AssertStringContains(t, body, "template-resolved argument 0 does not match the approved pattern")
	if strings.Contains(body, "evil.example") {
		t.Fatalf("resolved secret leaked in error: %s", body)
	}
}

// TestHttpPluginRedirectErrorNotLeakingSecrets verifies that a redirect
// permission mismatch does not reveal resolved secrets held in the call's
// other positional arguments: one permission approves the original url, a
// second one matches the redirect target but not the resolved params
func TestHttpPluginRedirectErrorNotLeakingSecrets(t *testing.T) {
	t.Setenv("HOSTSECRET", "s3cr3t-value")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "target contents") //nolint:errcheck
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/internal?echo="+r.URL.Query().Get("k"), http.StatusFound)
	}))
	defer redirector.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "get", secrets=[["HOSTSECRET"]])])

def handler(req):
	return http.get("` + redirector.URL + `/start", {"k": '{{secret "HOSTSECRET"}}'}).value.body()
`,
	}
	perms := []types.Permission{
		{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + redirector.URL}, Secrets: [][]string{{"HOSTSECRET"}}},
		{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + target.URL, "regex:^nomatch"}},
	}
	a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, perms, &types.ServerConfig{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}
	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	body := response.Body.String()
	testutil.AssertStringContains(t, body, "not followed: app /test is not permitted to call http.in.get: argument 1 does not match the approved pattern")
	if strings.Contains(body, "s3cr3t-value") {
		t.Fatalf("resolved secret leaked in error: %s", body)
	}
}

// TestHttpPluginRedirectErrorNotLeakingPathSecret verifies that a refused
// redirect does not reveal a resolved secret held in the url path: a relative
// redirect keeps the original path, which the error must not print
func TestHttpPluginRedirectErrorNotLeakingPathSecret(t *testing.T) {
	t.Setenv("TOKEN", "t0ken-value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "result", http.StatusFound)
	}))
	defer server.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "get", secrets=[["TOKEN"]])])

def handler(req):
	return http.get("` + server.URL + `/{{secret \"TOKEN\"}}/start").value.body()
`,
	}
	perms := []types.Permission{
		{Plugin: "http.in", Method: "get", Arguments: []string{"regex:/start$"}, Secrets: [][]string{{"TOKEN"}}},
	}
	a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, perms, &types.ServerConfig{})
	if err != nil {
		t.Fatalf("Error %s", err)
	}
	request := httptest.NewRequest("GET", "/test", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 500, response.Code)
	body := response.Body.String()
	testutil.AssertStringContains(t, body, "redirect to "+server.URL+" not followed: app /test is not permitted to call http.in.get: argument 0 does not match the approved pattern")
	if strings.Contains(body, "t0ken-value") {
		t.Fatalf("resolved secret leaked in error: %s", body)
	}
}
