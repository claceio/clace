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

	_, err = s.AppEvalTemplate(nil, "", `{{secret "SA_TEST_SECRET"}}`)
	if !errors.Is(err, ErrNoSecretAccess) {
		t.Fatalf("no secret access: err = %v, want ErrNoSecretAccess", err)
	}

	_, err = s.AppEvalTemplate([][]string{{"OTHER"}}, "", `{{secret_from "env" "SA_TEST_SECRET"}}`)
	if !errors.Is(err, ErrSecretNotAllowed) {
		t.Fatalf("secret not in patterns: err = %v, want ErrSecretNotAllowed", err)
	}

	got, err := s.AppEvalTemplate([][]string{{"regex:^SA_.*"}}, "", `{{secret "SA_TEST_SECRET"}}`)
	if err != nil || got != "value1" {
		t.Fatalf("allowed secret: got %q, err %v", got, err)
	}
}
