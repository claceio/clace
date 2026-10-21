// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestSecretsAccess covers which plugin arguments are template evaluated:
// ace.eval values and app level param values containing {{ are, with the
// secrets the call is approved for; plain strings never are, so request data
// forwarded to a plugin cannot resolve the app's secrets
func TestSecretsAccess(t *testing.T) {
	t.Setenv("abc", "secret-value")
	t.Setenv("abc2", "other-value")
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := r.URL.Path + "|" + r.Header.Get("X-Test")
		if user, pass, ok := r.BasicAuth(); ok {
			out += "basic " + user + ":" + pass
		}
		if r.Method == "POST" {
			body, _ := io.ReadAll(r.Body)
			out += string(body)
		}
		io.WriteString(w, out) //nolint:errcheck
	}))
	defer testServer.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"params.star": `
param("key", default='{{ secret_from "env" "abc"}}')
param("other")
param("braces", default="not {{ a template")
param("plain", default="plain value")
param("creds", type=DICT, default={"token": '{{ secret_from "env" "abc"}}', "user": "u1"})
param("hosts", type=LIST, default=["h1", '{{ secret_from "env" "abc"}}'])
`,
		"app.star": `

load ("http.in", "http")

def header(req, value):
	resp = http.get("` + testServer.URL + `/path", headers={"X-Test": value})
	return resp.value.body()

def eval_ok(req):
	return header(req, ace.eval('{{ secret_from "env" "abc"}}'))

def eval_denied(req):
	return header(req, ace.eval('{{ secret_from "env" "abc2"}}'))

def plain_string(req):
	return header(req, '{{ secret_from "env" "abc"}}')

def request_data(req):
	return header(req, req.Query["q"][0])

def positional(req):
	resp = http.get(ace.eval("` + testServer.URL + `/{{ secret_from \"env\" \"abc\"}}"))
	return resp.value.body()

def in_list(req):
	resp = http.get("` + testServer.URL + `/path", headers={"X-Test": "x"}, params={"q": "y"})
	return resp.value.body()

def eval_param(req):
	return header(req, ace.eval(param.key))

def eval_twice(req):
	return header(req, ace.eval(ace.eval(ace.eval('{{ secret_from "env" "abc"}}'))))

def eval_concat(req):
	return header(req, ace.eval("Bearer " + ace.eval(param.key)))

def eval_param_value(req):
	return header(req, ace.eval(param.other))

def auto_param(req):
	return header(req, param.key)

def auto_param_value(req):
	return header(req, param.other)

def braces_param(req):
	return header(req, param.braces)

def plain_param(req):
	return header(req, param.plain)

def concat(req):
	return header(req, "Bearer " + param.key + "/" + param.other)

def concat_request(req):
	return header(req, req.Query["q"][0] + ":" + param.key)

def str_param(req):
	return header(req, str(param.key))

def format_param(req):
	return header(req, "%s" % param.key)

def dict_param(req):
	return header(req, param.creds["token"] + ":" + param.creds["user"])

def list_param(req):
	return header(req, param.hosts[0] + ":" + param.hosts[1])

def cyclic(req):
	items = []
	items.append(items)
	resp = http.get("` + testServer.URL + `/path", headers={"X-Test": "x"}, params={"q": items})
	return resp.value.body()

def tuple_arg(req):
	resp = http.get("` + testServer.URL + `/path", auth_basic=("user", param.key))
	return resp.value.body()

def nested(req):
	resp = http.post("` + testServer.URL + `/path", headers={"X-Test": "x"}, json_body={"outer": [{"inner": param.key}]})
	return resp.value.body()

def eval_not_string(req):
	return header(req, ace.eval(1))

def eval_bad_template(req):
	return header(req, ace.eval("{{ unclosed"))

def eval_no_secret(req):
	return header(req, ace.eval('{{ upper "abc" }}'))

HEADERS = {"X-Test": ace.eval('{{ secret_from "env" "abc"}}')}
URLS = ["` + testServer.URL + `/path", ace.eval("` + testServer.URL + `/{{ secret_from \"env\" \"abc\"}}")]

def frozen_dict(req):
	resp = http.get("` + testServer.URL + `/path", headers=HEADERS)
	return resp.value.body() + "|" + str(HEADERS)

def frozen_list(req):
	resp = http.get(URLS[1])
	return resp.value.body() + "|" + str(URLS[1])

app = ace.app("testApp", custom_layout=True, routes = [
	ace.api("/eval_ok", handler=eval_ok, type=ace.TEXT),
	ace.api("/eval_denied", handler=eval_denied, type=ace.TEXT),
	ace.api("/plain_string", handler=plain_string, type=ace.TEXT),
	ace.api("/request_data", handler=request_data, type=ace.TEXT),
	ace.api("/positional", handler=positional, type=ace.TEXT),
	ace.api("/eval_param", handler=eval_param, type=ace.TEXT),
	ace.api("/eval_twice", handler=eval_twice, type=ace.TEXT),
	ace.api("/eval_concat", handler=eval_concat, type=ace.TEXT),
	ace.api("/eval_param_value", handler=eval_param_value, type=ace.TEXT),
	ace.api("/auto_param", handler=auto_param, type=ace.TEXT),
	ace.api("/auto_param_value", handler=auto_param_value, type=ace.TEXT),
	ace.api("/braces_param", handler=braces_param, type=ace.TEXT),
	ace.api("/plain_param", handler=plain_param, type=ace.TEXT),
	ace.api("/concat", handler=concat, type=ace.TEXT),
	ace.api("/concat_request", handler=concat_request, type=ace.TEXT),
	ace.api("/str_param", handler=str_param, type=ace.TEXT),
	ace.api("/format_param", handler=format_param, type=ace.TEXT),
	ace.api("/dict_param", handler=dict_param, type=ace.TEXT),
	ace.api("/list_param", handler=list_param, type=ace.TEXT),
	ace.api("/cyclic", handler=cyclic, type=ace.TEXT),
	ace.api("/tuple_arg", handler=tuple_arg, type=ace.TEXT),
	ace.api("/nested", handler=nested, type=ace.TEXT),
	ace.api("/eval_not_string", handler=eval_not_string, type=ace.TEXT),
	ace.api("/eval_bad_template", handler=eval_bad_template, type=ace.TEXT),
	ace.api("/eval_no_secret", handler=eval_no_secret, type=ace.TEXT),
	ace.api("/frozen_dict", handler=frozen_dict, type=ace.TEXT),
	ace.api("/frozen_list", handler=frozen_list, type=ace.TEXT),
	],
    permissions=[
	ace.permission("http.in", "get", secrets=[["abc"]]),
	ace.permission("http.in", "post", secrets=[["abc"]]),
	])
`,
	}

	a, _, err := CreateTestAppInt(logger, "/test", "", fileData, false,
		[]string{"http.in"},
		[]types.Permission{
			{Plugin: "http.in", Method: "get", Secrets: [][]string{{"abc"}}},
			{Plugin: "http.in", Method: "post", Secrets: [][]string{{"abc"}}},
		},
		nil, "app_prd_testapp", types.AppSettings{},
		map[string]string{"other": `{{ secret_from "env" "abc"}}`}, nil, nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	_, err = a.Audit()
	testutil.AssertNoError(t, err)

	cases := []struct {
		path string
		code int
		body string
	}{
		{"/eval_ok", 200, "/path|secret-value"},
		{"/eval_denied", 500, "error calling secret_from: the plugin call is not allowed to access the secret: secret abc2 is not in the allowed patterns"},
		// a plain string is passed through literally, even when it is a template referencing an allowed secret
		{"/plain_string", 200, `/path|{{ secret_from "env" "abc"}}`},
		{"/request_data?q=" + url.QueryEscape(`{{ secret_from "env" "abc"}}`), 200, `/path|{{ secret_from "env" "abc"}}`},
		{"/positional", 200, "/secret-value|"},
		// app level param values containing {{ are templates, ace.eval on them is a no-op
		{"/eval_param", 200, "/path|secret-value"}, // params.star default
		{"/eval_twice", 200, "/path|secret-value"}, // wrapping a template again is a no-op
		{"/eval_concat", 200, "/path|Bearer secret-value"},
		{"/eval_param_value", 200, "/path|secret-value"}, // app param value
		{"/auto_param", 200, "/path|secret-value"},
		{"/auto_param_value", 200, "/path|secret-value"},
		// a param value with {{ which is not a secret template passes through (lenient)
		{"/braces_param", 200, "/path|not {{ a template"},
		{"/plain_param", 200, "/path|plain value"},
		// string concatenation keeps the strings literal and resolves the templates
		{"/concat", 200, "/path|Bearer secret-value/secret-value"},
		{"/concat_request?q=" + url.QueryEscape(`{{ secret_from "env" "abc2"}}`), 200, `/path|{{ secret_from "env" "abc2"}}:secret-value`},
		// str() and % formatting give the reference text as a plain string, not evaluated
		{"/str_param", 200, `/path|{{ secret_from "env" "abc"}}`},
		{"/format_param", 200, `/path|{{ secret_from "env" "abc"}}`},
		// strings inside list and dict params are wrapped too
		{"/dict_param", 200, "/path|secret-value:u1"},
		{"/list_param", 200, "/path|h1:secret-value"},
		{"/cyclic", 500, "nesting exceeds max depth"},
		{"/tuple_arg", 200, "/path|basic user:secret-value"},
		{"/nested", 200, `/path|x{"outer":[{"inner":"secret-value"}]}`},
		{"/eval_not_string", 500, "eval: for parameter template: got int, want string or template"},
		{"/eval_bad_template", 500, `function "unclosed" not defined`},
		{"/eval_no_secret", 200, "/path|ABC"},
		// module level (frozen) containers resolve without being modified
		{"/frozen_dict", 200, `/path|secret-value|{"X-Test": {{ secret_from "env" "abc"}}}`},
		{"/frozen_list", 200, `/secret-value||` + testServer.URL + `/{{ secret_from "env" "abc"}}`},
	}
	for _, c := range cases {
		request := httptest.NewRequest("GET", "/test"+c.path, nil)
		response := httptest.NewRecorder()
		a.ServeHTTP(response, request)
		testutil.AssertEqualsInt(t, c.path+" code", c.code, response.Code)
		testutil.AssertStringContains(t, response.Body.String(), c.body)
	}
}

// TestSecretsAccessAction checks a secret param in an action handler: the
// app level value resolves through param.X, and through args.X when the
// caller did not supply the param; a caller supplied args.X is a plain
// string, passed through literally
func TestSecretsAccessAction(t *testing.T) {
	t.Setenv("abc", "secret-value")
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Header.Get("X-Test")) //nolint:errcheck
	}))
	defer testServer.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"params.star": `
param("key", default='{{ secret_from "env" "abc"}}')
param("name", default="n1")
`,
		"app.star": `
load ("http.in", "http")

def handler(dry_run, args):
	resp = http.get("` + testServer.URL + `", headers={"X-Test": param.key})
	resp2 = http.get("` + testServer.URL + `", headers={"X-Test": args.key})
	return ace.result(status="got " + resp.value.body() + " and " + resp2.value.body(), values=[{"key": args.key}], report=ace.TABLE)

app = ace.app("testApp", actions=[ace.action("testAction", "/", handler)],
    permissions=[ace.permission("http.in", "get", secrets=[["abc"]])])
`,
	}

	a, _, err := CreateTestAppPlugin(logger, fileData,
		[]string{"http.in"},
		[]types.Permission{{Plugin: "http.in", Method: "get", Secrets: [][]string{{"abc"}}}},
		nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	post := func(values url.Values) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/test", strings.NewReader(values.Encode()))
		request.Header.Set("HX-Request", "true")
		request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		a.ServeHTTP(response, request)
		testutil.AssertEqualsInt(t, "code", 200, response.Code)
		return response
	}

	// Caller supplied value, even the same template text, is a literal
	response := post(url.Values{"name": {"n2"}, "key": {`{{ secret_from "env" "abc"}}`}})
	testutil.AssertStringContains(t, response.Body.String(), `got secret-value and {{ secret_from &#34;env&#34; &#34;abc&#34;}}`)

	// No value for key in the invocation: args.key is the app level
	// template; it resolves in the plugin call and the result shows the
	// reference text, never the secret
	response = post(url.Values{"name": {"n2"}})
	testutil.AssertStringContains(t, response.Body.String(), "got secret-value and secret-value")
	testutil.AssertStringContains(t, response.Body.String(), `{{ secret_from &#34;env&#34; &#34;abc&#34;}}</td>`)
	if strings.Contains(response.Body.String(), "secret-value</td>") {
		t.Fatalf("resolved secret leaked into the action result: %s", response.Body.String())
	}
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
	return http.get(ace.eval('https://api.example.com/{{secret "HOSTSECRET"}}')).value.body()
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
	return http.get("` + redirector.URL + `/start", {"k": ace.eval('{{secret "HOSTSECRET"}}')}).value.body()
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
	return http.get(ace.eval("` + server.URL + `/{{secret \"TOKEN\"}}/start")).value.body()
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

// TestHttpPluginParamsChecked verifies that the url with the params values
// merged into its query is checked against the approved argument patterns
// and the disallow list, as the url argument alone was: params can add to or
// replace the query the patterns matched
func TestHttpPluginParamsChecked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "mode="+r.URL.Query().Get("mode")) //nolint:errcheck
	}))
	defer server.Close()

	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load ("http.in", "http")
app = ace.app("testApp", custom_layout=True, routes = [ace.api("/", type=ace.TEXT)],
    permissions=[ace.permission("http.in", "get")])

def handler(req):
	return http.get("` + server.URL + `/items?mode=safe", params={"mode": req.Query["mode"][0]}).value.body()
`,
	}
	tests := []struct {
		name     string
		perms    []types.Permission
		disallow []types.Permission
		mode     string
		code     int
		contains string
	}{
		{"params within the pattern",
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + server.URL + "/items"}}},
			nil, "adm1n-value", 200, "mode=adm1n-value"},
		{"params replacing a query value the pattern matched",
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + server.URL + `/items\?mode=safe$`}}},
			nil, "adm1n-value", 500, "url with params not permitted: app /test is not permitted to call http.in.get: argument 0 does not match the approved pattern"},
		{"params keeping the query the pattern matched",
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + server.URL + `/items\?mode=safe$`}}},
			nil, "safe", 200, "mode=safe"},
		{"params hitting the disallow list",
			[]types.Permission{{Plugin: "http.in", Method: "get"}},
			[]types.Permission{{Plugin: "http.in", Method: "get", Arguments: []string{"regex:mode=adm1n"}}},
			"adm1n-value", 500, "url with params not permitted: app /test is not permitted to call http.in.get: the target is disallowed by the server config (permissions.disallow)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &types.ServerConfig{}
			sc.Permissions.Disallow = tt.disallow
			a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, tt.perms, sc)
			if err != nil {
				t.Fatalf("Error %s", err)
			}
			request := httptest.NewRequest("GET", "/test?mode="+tt.mode, nil)
			response := httptest.NewRecorder()
			a.ServeHTTP(response, request)
			testutil.AssertEqualsInt(t, "code", tt.code, response.Code)
			testutil.AssertStringContains(t, response.Body.String(), tt.contains)
			if tt.code == 500 && strings.Contains(response.Body.String(), tt.mode) {
				t.Fatalf("param value printed in error: %s", response.Body.String())
			}
		})
	}
}

// TestHttpPluginRedirectSecretsNotForwarded verifies that a redirect is
// followed only when the permission approving the target grants each secret
// the call resolved: custom headers and a 307/308 body go along to the
// target, so a host the app may call without a secret must not receive one
// through a redirect from a host it may call with it. The check is on the
// secrets actually resolved, not the source permission's grants, and a
// template without a secret carries nothing
func TestHttpPluginRedirectSecretsNotForwarded(t *testing.T) {
	t.Setenv("HOSTSECRET", "s3cr3t-value")
	t.Setenv("OTHERSECRET", "0ther-value")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "target got key="+r.Header.Get("X-Key")) //nolint:errcheck
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
    permissions=[ace.permission("http.in", "get", secrets=[["HOSTSECRET"], ["OTHERSECRET"]])])

def handler(req):
	headers = {"X-Key": "plain"}
	s = req.Query.get("secret")
	if s == ["tmpl"]:
		headers = {"X-Key": ace.eval('{{ "no-secret" }}')}
	elif s:
		headers = {"X-Key": ace.eval('{{secret "' + s[0] + '"}}')}
	return http.get("` + redirector.URL + `/start", headers=headers).value.body()
`,
	}
	source := func(secrets ...string) types.Permission {
		p := types.Permission{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + redirector.URL}}
		for _, s := range secrets {
			p.Secrets = append(p.Secrets, []string{s})
		}
		return p
	}
	dest := func(secrets ...string) types.Permission {
		p := types.Permission{Plugin: "http.in", Method: "get", Arguments: []string{"regex:^" + target.URL}}
		for _, s := range secrets {
			p.Secrets = append(p.Secrets, []string{s})
		}
		return p
	}
	refused := "not followed: app /test is not permitted to call http.in.get: the matching permission does not allow the secrets the call resolved ("
	tests := []struct {
		name     string
		perms    []types.Permission
		secret   string
		code     int
		contains string
	}{
		{"target permission without the secret refuses the redirect",
			[]types.Permission{source("HOSTSECRET"), dest()}, "HOSTSECRET", 500, refused + "HOSTSECRET)"},
		{"target permission with the secret follows the redirect",
			[]types.Permission{source("HOSTSECRET"), dest("HOSTSECRET")}, "HOSTSECRET", 200, "target got key=s3cr3t-value"},
		{"target permission with a wider secret pattern follows the redirect",
			[]types.Permission{source("HOSTSECRET"), dest("regex:HOST.*")}, "HOSTSECRET", 200, "target got key=s3cr3t-value"},
		{"a later permission with the secret approves the target",
			[]types.Permission{source("HOSTSECRET"), dest(), {Plugin: "http.in", Method: "get", Secrets: [][]string{{"HOSTSECRET"}}}},
			"HOSTSECRET", 200, "target got key=s3cr3t-value"},
		{"call without templates follows the redirect",
			[]types.Permission{source("HOSTSECRET"), dest()}, "", 200, "target got key=plain"},
		// only the secrets resolved count, not every grant of the source permission
		{"target granting the resolved secret but not the other source grants follows",
			[]types.Permission{source("HOSTSECRET", "OTHERSECRET"), dest("HOSTSECRET")}, "HOSTSECRET", 200, "target got key=s3cr3t-value"},
		{"template without a secret follows the redirect",
			[]types.Permission{source("HOSTSECRET"), dest()}, "tmpl", 200, "target got key=no-secret"},
		// the resolved key is matched against the target's patterns, never the source pattern text
		{"target pattern matching the source pattern text but not the resolved key refuses",
			[]types.Permission{source("regex:HOSTSECRET|OTHERSECRET"), dest("regex:HOSTSECRET")}, "OTHERSECRET", 500, refused + "OTHERSECRET)"},
		{"target pattern matching the resolved key follows",
			[]types.Permission{source("regex:HOSTSECRET|OTHERSECRET"), dest("regex:OTHER.*")}, "OTHERSECRET", 200, "target got key=0ther-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"http.in"}, tt.perms, &types.ServerConfig{})
			if err != nil {
				t.Fatalf("Error %s", err)
			}
			path := "/test"
			if tt.secret != "" {
				path += "?secret=" + tt.secret
			}
			request := httptest.NewRequest("GET", path, nil)
			response := httptest.NewRecorder()
			a.ServeHTTP(response, request)
			testutil.AssertEqualsInt(t, "code", tt.code, response.Code)
			body := response.Body.String()
			testutil.AssertStringContains(t, body, tt.contains)
			if tt.code == 500 && (strings.Contains(body, "s3cr3t-value") || strings.Contains(body, "0ther-value")) {
				t.Fatalf("resolved secret leaked in error: %s", body)
			}
		})
	}
}
