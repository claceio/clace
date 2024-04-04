// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"slices"
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/types"
)

func TestParsePathSpec(t *testing.T) {
	tests := map[string]struct {
		spec      string
		apps      []types.AppPathDomain
		want      []types.AppPathDomain
		wantError error
	}{
		"Match *": {
			spec:      "*", // defaults to no domain :*
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app"}, {Domain: "mydomain", Path: "/app"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app"}},
			wantError: nil,
		},
		"Match :*": {
			spec:      ":*", // same as *
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app"}, {Domain: "mydomain", Path: "/app"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app"}},
			wantError: nil,
		},
		"Match * none": {
			spec:      "*",
			apps:      []types.AppPathDomain{{Domain: "mydomain", Path: "/app"}},
			want:      []types.AppPathDomain{},
			wantError: nil,
		},
		"Match /abc": {
			spec:      "/abc",
			apps:      []types.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/abc"}},
			wantError: nil,
		},
		"Match /abc*": {
			spec:      "/abc*",
			apps:      []types.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}},
			wantError: nil,
		},
		"Match /abc/*": {
			spec:      "/abc/*",
			apps:      []types.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/abc/def"}},
			wantError: nil,
		},
		"Match /abc/**": {
			spec:      "/abc/**",
			apps:      []types.AppPathDomain{{Domain: "mydomain", Path: "/abc"}, {Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc2"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/abc"}, {Domain: "", Path: "/abc/def"}, {Domain: "", Path: "/abc/def/xyz"}},
			wantError: nil,
		},
		"Match *:*": {
			spec:      "*:*",
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app1"}},
			wantError: nil,
		},
		"Match *:**": {
			spec:      "*:**",
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match all": {
			spec:      "all",
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match empty": {
			spec:      "",
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match **:**": {
			spec:      "**:**",
			apps:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "", Path: "/app1"}, {Domain: "mydomain", Path: "/app2/def"}},
			wantError: nil,
		},
		"Match mydomain*:**": {
			spec:      "mydomain*:**",
			apps:      []types.AppPathDomain{{Domain: "testdomain", Path: "/app1"}, {Domain: "mydomain", Path: "/app/def"}, {Domain: "mydomain.test", Path: "/app2/def"}},
			want:      []types.AppPathDomain{{Domain: "mydomain", Path: "/app/def"}, {Domain: "mydomain.test", Path: "/app2/def"}},
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
			_, gotError := ParseSpec(tc.spec, []types.AppPathDomain{{Domain: "", Path: "/app1"}})
			testutil.AssertErrorContains(t, gotError, tc.wantError.Error())
		})
	}
}
