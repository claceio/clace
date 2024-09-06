// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/claceio/clace/internal/testutil"
	"github.com/claceio/clace/internal/types"
)

func evalFunc(input string) (string, error) {
	if len(input) < 4 {
		return input, nil
	}
	if input[0] != '{' || input[1] != '{' || input[len(input)-1] != '}' || input[len(input)-2] != '}' {
		return input, nil
	}

	return "XXXmysecretXXX", nil
}

func TestUpdateAuth(t *testing.T) {
	serverConfig := &types.ServerConfig{
		Auth: map[string]types.AuthConfig{
			"auth0": {
				Key:    "myclientID",
				Secret: `{{ secret("asm", "mysecret")}}`,
			},
			"auth2": {
				Key:    "myclientID2",
				Secret: `{{ secret "env" "TEST"} }}`,
			},
		},
	}

	err := updateConfigSecrets(serverConfig, evalFunc)
	testutil.AssertEqualsError(t, "error", err, nil)
	testutil.AssertEqualsString(t, "clientID", "myclientID", serverConfig.Auth["auth0"].Key)
	testutil.AssertEqualsString(t, "secret", "XXXmysecretXXX", serverConfig.Auth["auth0"].Secret)
	testutil.AssertEqualsString(t, "clientID", "myclientID2", serverConfig.Auth["auth2"].Key)
	testutil.AssertEqualsString(t, "secret", "XXXmysecretXXX", serverConfig.Auth["auth2"].Secret)
}
