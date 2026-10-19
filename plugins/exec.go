// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"errors"

	"github.com/openrundev/openrun/internal/app"
	"github.com/openrundev/openrun/internal/system"
	"github.com/openrundev/openrun/internal/types"
	sdk "github.com/openrundev/openrun/pkg/plugin"
)

const MAX_BYTES_STDOUT = 100 * 1024 * 1024 // 100MB

func init() {
	// exec runs commands on the host: registered as a system plugin
	// (anonymous callers are blocked unless security.unsafe_allow_system_plugins_anon
	// is set), and every command must be in the server's
	// security.allowed_exec_commands allowlist (empty by default) as well as
	// approved for the app
	app.RegisterLocalProvider("exec", &sdk.ServeConfig{
		ProviderVersion: "builtin",
		Modules: map[string]sdk.ModuleDef{
			"exec": {
				Builder: NewExecModule,
				Functions: []sdk.FuncDef{
					{Name: "run", Type: sdk.WRITE, Method: "Run"},
				},
			},
		},
	}, app.LocalProviderOptions{SystemModules: []string{"exec"}})
}

type execModule struct{}

func NewExecModule() sdk.Module {
	return &execModule{}
}

func (e *execModule) InitModule(ctx context.Context, init sdk.ModuleInit) error {
	return nil
}

func (e *execModule) Close(ctx context.Context) error {
	return nil
}

// Run executes a host command. The allowlist comes from the server config,
// which the host exposes to in-process plugins; the module is host-bound
func (e *execModule) Run(ctx context.Context, call *sdk.Call) (any, error) {
	allowlist, err := execAllowlist(call)
	if err != nil {
		return nil, err
	}
	return execCommand(ctx, call, nil, allowlist)
}

// execAllowlist builds the host command allowlist from the server config
// exposed by the call's host. The entries are validated at config load, so
// a parse failure here is a programming error surfaced as a call error
func execAllowlist(call *sdk.Call) (*system.ExecAllowlist, error) {
	if call.Host == nil {
		return nil, errors.New("exec.run requires the compiled-in exec plugin")
	}
	config, _ := call.Host.Value(types.TL_SERVER_CONFIG).(*types.ServerConfig)
	if config == nil {
		return nil, errors.New("exec.run: server config is not available")
	}
	return system.NewExecAllowlist(config.Security.AllowedExecCommands)
}
