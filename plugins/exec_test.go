// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"strings"
	"testing"

	"github.com/openrundev/openrun/internal/types"
	sdk "github.com/openrundev/openrun/pkg/plugin"
)

// configHost is the host services of an in-process plugin call in tests:
// it serves the server config and nothing else
type configHost struct {
	config *types.ServerConfig
}

func (h configHost) Value(key string) any {
	if key == types.TL_SERVER_CONFIG && h.config != nil {
		return h.config
	}
	return nil
}

func execRunHostCall(allowed []string, kwargs ...sdk.Kwarg) *sdk.Call {
	call := execRunCall(kwargs...)
	config := &types.ServerConfig{}
	config.Security.AllowedExecCommands = allowed
	call.Host = configHost{config: config}
	return call
}

// exec.run enforces security.allowed_exec_commands from the host's server
// config, on the resolved command
func TestExecRunAllowlist(t *testing.T) {
	module := NewExecModule()
	echoArgs := []sdk.Kwarg{
		{Name: "path", Value: "echo"},
		{Name: "args", Value: []any{"hello"}},
	}

	result, err := module.(*execModule).Run(context.Background(), execRunHostCall([]string{"echo"}, echoArgs...))
	if err != nil {
		t.Fatalf("allowed command: %v", err)
	}
	if lines, ok := result.([]any); !ok || len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("output = %#v", result)
	}

	_, err = module.(*execModule).Run(context.Background(), execRunHostCall([]string{"ls"}, echoArgs...))
	if err == nil || !strings.Contains(err.Error(), "not in security.allowed_exec_commands") {
		t.Fatalf("unlisted command: %v", err)
	}

	_, err = module.(*execModule).Run(context.Background(), execRunHostCall(nil, echoArgs...))
	if err == nil || !strings.Contains(err.Error(), "not in security.allowed_exec_commands") {
		t.Fatalf("empty allowlist: %v", err)
	}

	// Without a host (an external provider build) the module cannot see the
	// server config and refuses rather than running unchecked
	_, err = module.(*execModule).Run(context.Background(), execRunCall(echoArgs...))
	if err == nil || !strings.Contains(err.Error(), "compiled-in") {
		t.Fatalf("no host: %v", err)
	}
	noConfig := execRunCall(echoArgs...)
	noConfig.Host = configHost{}
	_, err = module.(*execModule).Run(context.Background(), noConfig)
	if err == nil || !strings.Contains(err.Error(), "server config is not available") {
		t.Fatalf("host without config: %v", err)
	}
}

// The command runs by its resolved path but keeps the requested name as
// argv[0], for programs that dispatch on it
func TestExecRunKeepsArgv0(t *testing.T) {
	module := NewExecModule()
	result, err := module.(*execModule).Run(context.Background(), execRunHostCall([]string{"sh"},
		sdk.Kwarg{Name: "path", Value: "sh"},
		sdk.Kwarg{Name: "args", Value: []any{"-c", "echo $0"}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if lines, ok := result.([]any); !ok || len(lines) != 1 || lines[0] != "sh" {
		t.Fatalf("argv[0] output = %#v", result)
	}
}
