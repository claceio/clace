// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package system

import (
	"context"
	"errors"
	"testing"

	"github.com/openrundev/openrun/internal/types"
)

// A template resolving a secret with no secret access fails with an error
// callers can identify, so the container handler can add the fix to it
func TestAppEvalTemplateSecretAccessErrors(t *testing.T) {
	t.Setenv("SA_TEST_SECRET", "value1")
	s, err := NewSecretManager(context.Background(), map[string]types.SecretConfig{"env": {}}, "env", &types.ServerConfig{})
	if err != nil {
		t.Fatalf("new secret manager: %v", err)
	}

	_, err = s.AppEvalTemplate(nil, "", `{{secret "SA_TEST_SECRET"}}`, true)
	if !errors.Is(err, ErrNoSecretAccess) {
		t.Fatalf("no secret access: err = %v, want ErrNoSecretAccess", err)
	}

	_, err = s.AppEvalTemplate([][]string{{"OTHER"}}, "", `{{secret_from "env" "SA_TEST_SECRET"}}`, true)
	if !errors.Is(err, ErrSecretNotAllowed) {
		t.Fatalf("secret not in patterns: err = %v, want ErrSecretNotAllowed", err)
	}

	got, err := s.AppEvalTemplate([][]string{{"regex:^SA_.*"}}, "", `{{secret "SA_TEST_SECRET"}}`, true)
	if err != nil || got != "value1" {
		t.Fatalf("allowed secret: got %q, err %v", got, err)
	}
}

// A multi entry secret pattern names the joined secret: it covers a secret
// call with at least as many keys, each matching, never a prefix of itself
func TestAppEvalTemplateMultiKeyPatterns(t *testing.T) {
	t.Setenv("PROD", "prod-alone")
	t.Setenv("PROD_PUBLIC_KEY", "prod-public")
	t.Setenv("PROD_PUBLIC_KEY_V2", "prod-public-v2")
	s, err := NewSecretManager(context.Background(), map[string]types.SecretConfig{"env": {}}, "env", &types.ServerConfig{})
	if err != nil {
		t.Fatalf("new secret manager: %v", err)
	}
	perms := [][]string{{"PROD", "PUBLIC_KEY"}}
	for _, tc := range []struct {
		tmpl string
		want string
	}{
		{`{{secret "PROD" "PUBLIC_KEY"}}`, "prod-public"},
		{`{{secret "PROD" "PUBLIC_KEY" "V2"}}`, "prod-public-v2"},
		{`{{secret "PROD"}}`, ""},
		{`{{secret}}`, ""},
		{`{{secret "OTHER" "PUBLIC_KEY"}}`, ""},
	} {
		got, err := s.AppEvalTemplate(perms, "", tc.tmpl, true)
		if tc.want == "" {
			if !errors.Is(err, ErrSecretNotAllowed) {
				t.Fatalf("%s: err = %v, want ErrSecretNotAllowed", tc.tmpl, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, err %v, want %q", tc.tmpl, got, err, tc.want)
		}
	}
}
