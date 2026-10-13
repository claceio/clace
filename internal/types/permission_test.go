// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"
)

func TestPermissionDescribe(t *testing.T) {
	t.Parallel()

	p := Permission{Plugin: "container.in", Method: "config", Arguments: []string{"AUTO"}, Secrets: [][]string{{"regex:.*"}, {"db", "pass"}}}
	if got, want := p.Describe(), "container.in.config AUTO secrets=regex:.*;db,pass"; got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
	if got, want := (Permission{Plugin: "http.in", Method: "get"}).Describe(), "http.in.get"; got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
}

func TestAppPathDomainMainPath(t *testing.T) {
	t.Parallel()

	stage := AppPathDomain{Path: "/secrettest" + STAGE_SUFFIX, Domain: "example.com"}
	if got := stage.MainPath(); got.Path != "/secrettest" || got.Domain != "example.com" {
		t.Fatalf("MainPath() = %+v, want the prod path", got)
	}
	prod := AppPathDomain{Path: "/secrettest"}
	if got := prod.MainPath(); got != prod {
		t.Fatalf("MainPath() = %+v, want unchanged for a prod path", got)
	}
}

func TestUngrantedSecretsWarning(t *testing.T) {
	t.Parallel()

	ungranted := []Permission{{Plugin: "container.in", Method: "config", Arguments: []string{"AUTO"}, Secrets: [][]string{{"my_secret"}}}}
	msg := UngrantedSecretsWarning("/secrettest", "", ungranted)
	for _, want := range []string{"app /secrettest: the app declares secret access which is not approved: container.in.config AUTO secrets=my_secret",
		"openrun app approve /secrettest", "[[permissions.allow]]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("warning %q missing %q", msg, want)
		}
	}
	msg = UngrantedSecretsWarning("/secrettest", "the new value references secrets", ungranted)
	if !strings.HasPrefix(msg, "app /secrettest: the new value references secrets, but the app declares") {
		t.Fatalf("warning with context = %q", msg)
	}
}
