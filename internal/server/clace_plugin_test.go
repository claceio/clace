// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
)

func TestGetSourceUrl(t *testing.T) {
	tests := []struct {
		url    string
		branch string
		want   string
	}{
		{
			url:    "github.com/claceio/clace/myapp",
			branch: "main",
			want:   "https://github.com/claceio/clace/tree/main/myapp",
		},
		{
			url:    "https://github.com/claceio/clace/myapp",
			branch: "main",
			want:   "https://github.com/claceio/clace/tree/main/myapp",
		},
		{
			url:    "https://github.com/claceio/clace/myapp",
			branch: "main",
			want:   "https://github.com/claceio/clace/tree/main/myapp",
		},
		{
			url:    "/claceio/clace/myapp",
			branch: "main",
			want:   "",
		},
		{
			url:    "git@github.com/claceio/clace.git/myapp/t1/t2",
			branch: "develop",
			want:   "",
		},
		{
			url:    "git@github.com:claceio/clace.git/myapp/t1/t2",
			branch: "develop",
			want:   "https://github.com/claceio/clace/tree/develop/myapp/t1/t2",
		},
		{
			url:    "github.com/claceio",
			branch: "main",
			want:   "",
		},
		{
			url:    "https://github.com/claceio/clace/myapp",
			branch: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		testutil.AssertEqualsString(t, tt.url, tt.want, getSourceUrl(tt.url, tt.branch))
	}
}
