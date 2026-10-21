// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/app"
	"github.com/openrundev/openrun/internal/app/action"
	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
)

// The files of DOWNLOAD and IMAGE results, for callers without an app
// session: fetched in-process through the app router (the CLI through the
// management API), returned inline by the MCP tools

func uploadOutcome(t *testing.T, a *app.App, content string) *action.Outcome {
	t.Helper()
	outcome, invErr := findAction(t, a, "upload").Invoke(userCtx(), action.Invocation{Op: action.OpRun,
		Files: map[string]action.UploadedFile{"upload": {Filename: "orders.txt",
			Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(content)), nil }}}})
	if invErr != nil {
		t.Fatalf("invoke upload: %s", invErr)
	}
	return outcome
}

func TestActionFetchResultFiles(t *testing.T) {
	a := createActionsTestApp(t, nil)

	// An image result: a static file of the app, relative to the action page
	imageAction := findAction(t, a, "image")
	outcome, invErr := imageAction.Invoke(userCtx(), action.Invocation{Op: action.OpRun})
	if invErr != nil {
		t.Fatalf("invoke image: %s", invErr)
	}
	files := action.ResultFiles(outcome.ValuesMap)
	outcome.Close()
	testutil.AssertEqualsInt(t, "image files", 1, len(files))
	local, external, err := imageAction.ResolveResultURL(files[0].URL)
	testutil.AssertNoError(t, err)
	testutil.AssertEqualsBool(t, "external", false, external)
	testutil.AssertEqualsString(t, "local url", "/test/static/openrun-logo.png", local)

	fetched, err := a.FetchLocal(userCtx(), local, 1<<20)
	testutil.AssertNoError(t, err)
	testutil.AssertEqualsString(t, "image mime", "image/png", fetched.MimeType)
	if !strings.HasPrefix(string(fetched.Data), "\x89PNG") {
		t.Fatalf("not a png: %q", fetched.Data[:8])
	}
	_, err = a.FetchLocal(userCtx(), local, 100)
	if !errors.Is(err, action.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
	_, err = a.FetchLocal(userCtx(), "/test/static/nosuch.png", 1<<20)
	testutil.AssertErrorContains(t, err, "status 404")

	// A download result: a single access file registered with fs.serve_tmp_file
	outcome = uploadOutcome(t, a, "first\nsecond\n")
	files = action.ResultFiles(outcome.ValuesMap)
	outcome.Close()
	testutil.AssertEqualsInt(t, "download files", 1, len(files))
	testutil.AssertEqualsString(t, "download name", "numbered_orders.txt", files[0].Name)
	local, external, err = findAction(t, a, "upload").ResolveResultURL(files[0].URL)
	testutil.AssertNoError(t, err)
	testutil.AssertEqualsBool(t, "external", false, external)
	testutil.AssertStringContains(t, local, "/test/_openrun_app/file/usr_file_")

	// A fetch refused for its size does not consume the single access file
	_, err = a.FetchLocal(userCtx(), local, 5)
	if !errors.Is(err, action.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
	fetched, err = a.FetchLocal(userCtx(), local, 1<<20)
	testutil.AssertNoError(t, err)
	testutil.AssertStringContains(t, string(fetched.Data), "1\tfirst")
	testutil.AssertStringContains(t, string(fetched.Data), "2\tsecond")
	testutil.AssertEqualsString(t, "detected mime", "text/plain", fetched.MimeType)
	// ... the completed fetch does
	_, err = a.FetchLocal(userCtx(), local, 1<<20)
	testutil.AssertErrorContains(t, err, "status 404")
}

// A file registered with fs.serve_tmp_file at the default (user level)
// visibility is served to the user who registered it only; the url is
// served by the app that registered the file, not through another app
func TestServeTmpFileUserVisibility(t *testing.T) {
	a := createActionsTestApp(t, nil)
	outcome := uploadOutcome(t, a, "first\nsecond\n")
	files := action.ResultFiles(outcome.ValuesMap)
	outcome.Close()
	local, _, err := findAction(t, a, "upload").ResolveResultURL(files[0].URL)
	testutil.AssertNoError(t, err)

	bobCtx := context.WithValue(context.Background(), types.USER_ID, "builtin:bob")
	_, err = a.FetchLocal(bobCtx, local, 1<<20)
	testutil.AssertErrorContains(t, err, "status 403")
	_, err = a.FetchLocal(context.Background(), local, 1<<20)
	testutil.AssertErrorContains(t, err, "status 401")

	// Another app on the server (another id) does not serve the file
	other := createActionsTestAppId(t, nil, "/other", "app_prd_otherapp")
	_, err = other.FetchLocal(userCtx(), strings.Replace(local, "/test/", "/other/", 1), 1<<20)
	testutil.AssertErrorContains(t, err, "status 404")

	// The registering user gets it, once (single access)
	fetched, err := a.FetchLocal(userCtx(), local, 1<<20)
	testutil.AssertNoError(t, err)
	testutil.AssertStringContains(t, string(fetched.Data), "first")
	_, err = a.FetchLocal(userCtx(), local, 1<<20)
	testutil.AssertErrorContains(t, err, "status 404")
}
