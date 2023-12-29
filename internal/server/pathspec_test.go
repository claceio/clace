// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"slices"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/utils"
)

func TestParsePathSpec(t *testing.T) {
	tests := map[string]struct {
		spec      string
		apps      []utils.AppPathDomain
		want      []utils.AppPathDomain
		wantError error
	}{
		"Match *": {
			spec:      "*", // defaults to no domain :*
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app"}, {Domain: "mydomain", Path: "/app"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app"}},
			wantError: nil,
		},
		"Match :*": {
			spec:      ":*", // same as *
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app"}, {Domain: "mydomain", Path: "/app"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app"}},
			wantError: nil,
		},
		"Match * none": {
			spec:      "*",
			apps:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/app"}},
			want:      []utils.AppPathDomain{},
			wantError: nil,
		},
		"Match /abc": {
			spec:      "/abc",
			apps:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/abc"}},
			wantError: nil,
		},
		"Match /abc*": {
			spec:      "/abc*",
			apps:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}},
			wantError: nil,
		},
		"Match /abc/*": {
			spec:      "/abc/*",
			apps:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/abc/def"}},
			wantError: nil,
		},
		"Match /abc/**": {
			spec:      "/abc/**",
			apps:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			wantError: nil,
		},
		"Match *:*": {
			spec:      "*:*",
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}},
			wantError: nil,
		},
		"Match *:**": {
			spec:      "*:**",
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match all": {
			spec:      "all",
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match empty": {
			spec:      "",
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match **:**": {
			spec:      "**:**",
			apps:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match mydomain*:**": {
			spec:      "mydomain*:**",
			apps:      []utils.AppPathDomain{{Domain: "testdomain", Path: "/app1"}, {Domain: "mydomain", Path: "/app/def"}, {Domain: "mydomain.test", Path: "/app2/def"}},
			want:      []utils.AppPathDomain{{Domain: "mydomain", Path: "/app/def"}, {Domain: "mydomain.test", Path: "/app2/def"}},
			wantError: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotApps, gotError := ParseSpec(tc.spec, tc.apps)
			testutil.AssertEqualsError(t, "error", gotError, tc.wantError)
			result := slices.Equal(gotApps, tc.want)
			if !result {
				t.Errorf("response got: %v, want: %v", gotApps, tc.want)
			}
		})
	}
}

func TestParsePathSpecErrors(t *testing.T) {
	tests := map[string]struct {
		spec      string
		wantError error
	}{
		"Match *:": {
			spec:      "*:",
			wantError: fmt.Errorf("app path spec cannot be empty"),
		},
		"Match :": {
			spec:      ":",
			wantError: fmt.Errorf("app path spec cannot be empty"),
		},
		"Match a:b:c": {
			spec:      "a:b:c",
			wantError: fmt.Errorf("path spec has to be in the format of domain:path"),
		},
		"Match invalid path": {
			spec:      ":[]",
			wantError: fmt.Errorf("syntax error in pattern"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, gotError := ParseSpec(tc.spec, []utils.AppPathDomain{{Domain: "", Path: "/app1"}})
			testutil.AssertErrorContains(t, gotError, tc.wantError.Error())
		})
	}
}
