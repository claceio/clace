// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestParseGithub(t *testing.T) {
	tests := map[string]struct {
		url        string
		wantRepo   string
		wantFolder string
		wantError  error
	}{
		"blank":                    {url: "", wantRepo: "", wantFolder: "", wantError: fmt.Errorf("invalid github url: /, expected github.com/orgName/repoName or github.com/orgName/repoName/folder")},
		"org and repo only":        {url: "github.com/orgName/repoName", wantRepo: "https://github.com/orgName/repoName", wantFolder: "", wantError: nil},
		"org, repo and folder":     {url: "http://github.com/orgName/repoName/folderName", wantRepo: "http://github.com/orgName/repoName", wantFolder: "folderName/", wantError: nil},
		"org, repo and subfolders": {url: "https://github.com/orgName/repoName/folderName/sub", wantRepo: "https://github.com/orgName/repoName", wantFolder: "folderName/sub/", wantError: nil},
		"invalid url":              {url: "/orgName", wantRepo: "", wantFolder: "", wantError: fmt.Errorf("invalid github url: /orgName/, expected github.com/orgName/repoName or github.com/orgName/repoName/folder")},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotRepo, gotFolder, gotError := parseGithubUrl(tc.url)
			testutil.AssertEqualsString(t, "repo", gotRepo, tc.wantRepo)
			testutil.AssertEqualsString(t, "folder", gotFolder, tc.wantFolder)
			testutil.AssertEqualsError(t, "error", gotError, tc.wantError)
		})
	}
}
