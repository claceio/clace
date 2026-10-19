// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
)

// An approved exec.in call is still bounded by the server's
// security.allowed_exec_commands: the app's approval alone does not grant a
// command the server does not list
func TestExecAllowlistEnforced(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load("exec.in", "exec")

app = ace.app("testApp", routes = [ace.api("/", type=ace.TEXT)])

def handler(req):
	return "\n".join(exec.run("ls", ["/"]).value)
`}
	serve := func(sc *types.ServerConfig) *httptest.ResponseRecorder {
		a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"exec.in"},
			[]types.Permission{{Plugin: "exec.in", Method: "run", Arguments: []string{"ls"}}}, sc)
		if err != nil {
			t.Fatalf("Error %s", err)
		}
		request := httptest.NewRequest("GET", "/test", nil)
		// exec is a system plugin: an authenticated caller is required
		request = request.WithContext(context.WithValue(request.Context(), types.USER_ID, "testuser"))
		response := httptest.NewRecorder()
		a.ServeHTTP(response, request)
		return response
	}

	response := serve(&types.ServerConfig{})
	testutil.AssertEqualsInt(t, "default denies", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "not in security.allowed_exec_commands")

	response = serve(execAllowedServerConfig("du"))
	testutil.AssertEqualsInt(t, "unlisted command", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "not in security.allowed_exec_commands")

	response = serve(execAllowedServerConfig("ls"))
	testutil.AssertEqualsInt(t, "listed command", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "tmp")
}

// The allowlist is read from the live server config, so a dynamic update
// applies to the next call of an app that is already loaded
func TestExecAllowlistFollowsLiveConfig(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
load("exec.in", "exec")

app = ace.app("testApp", routes = [ace.api("/", type=ace.TEXT)])

def handler(req):
	return "\n".join(exec.run("echo", ["hi"]).value)
`}
	live := execAllowedServerConfig("echo")
	a, _, err := CreateTestAppPluginServerConfig(logger, fileData, []string{"exec.in"},
		[]types.Permission{{Plugin: "exec.in", Method: "run", Arguments: []string{"echo"}}}, live)
	if err != nil {
		t.Fatalf("Error %s", err)
	}
	serve := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/test", nil)
		request = request.WithContext(context.WithValue(request.Context(), types.USER_ID, "testuser"))
		response := httptest.NewRecorder()
		a.ServeHTTP(response, request)
		return response
	}
	testutil.AssertEqualsInt(t, "allowed", 200, serve().Code)

	// The server publishes a new snapshot on a dynamic update
	revoked := &types.ServerConfig{}
	a.SetConfigSource(func() *types.ServerConfig { return revoked })
	response := serve()
	testutil.AssertEqualsInt(t, "revoked", 500, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "not in security.allowed_exec_commands")
}
