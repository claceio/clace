// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
)

func initClacePlugin(server *Server) {
	c := &clacePlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApiName(c.ListApps, app.READ, "list_apps"),
	}

	newClacePlugin := func(pluginContext *types.PluginContext) (any, error) {
		return &clacePlugin{server: server}, nil
	}

	app.RegisterPlugin("clace", newClacePlugin, pluginFuncs)
}

type clacePlugin struct {
	server *Server
}

func getRequestUserId(thread *starlark.Thread) string {
	ctxVal := thread.Local(types.TL_CONTEXT)
	if ctxVal == nil {
		return ""
	}

	ctx, ok := ctxVal.(context.Context)
	if !ok {
		return ""
	}

	return getUserId(ctx)
}

func (c *clacePlugin) verifyHasAccess(userId string, appAuth types.AppAuthnType) bool {
	if appAuth == types.AppAuthnDefault {
		appAuth = types.AppAuthnType(c.server.config.Security.AppDefaultAuthType)
	}

	// Verify user_id as set in authenticateAndServeApp
	if appAuth == "" || appAuth == types.AppAuthnNone {
		// No auth required for this app, allow access
		return true
	} else if appAuth == types.AppAuthnSystem {
		return userId == "admin"
	} else if appAuth == "cert" || strings.HasPrefix(string(appAuth), "cert_") {
		return userId == string(appAuth)
	} else {
		provider, _, ok := strings.Cut(string(userId), ":")
		if !ok {
			c.server.Warn().Str("user_id", userId).Msg("Unknown user_id format")
			return false
		}
		// Check Oauth provider is the same as the app's provider
		return provider == string(appAuth)
	}
}

func getAppUrl(app types.AppInfo, server *Server) string {
	useHttps := server.config.Https.Port < 0
	domain := app.AppPathDomain.Domain
	if domain == "" {
		domain = server.config.System.DefaultDomain
	}
	if useHttps {
		return fmt.Sprintf("https://%s:%d%s", domain, server.config.Https.Port, app.Path)
	} else {
		return fmt.Sprintf("http://%s:%d%s", domain, server.config.Http.Port, app.Path)
	}
}

func (c *clacePlugin) ListApps(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	apps, err := c.server.apps.GetAllApps()
	if err != nil {
		return nil, err
	}

	userId := getRequestUserId(thread)

	ret := starlark.List{}
	for _, app := range apps {
		if !c.verifyHasAccess(userId, app.Auth) {
			continue
		}

		v := starlark.Dict{}
		v.SetKey(starlark.String("name"), starlark.String(app.Name))
		v.SetKey(starlark.String("url"), starlark.String(getAppUrl(app, c.server)))
		v.SetKey(starlark.String("path"), starlark.String(app.AppPathDomain.String()))
		v.SetKey(starlark.String("id"), starlark.String(app.Id))
		v.SetKey(starlark.String("is_dev"), starlark.Bool(app.IsDev))
		v.SetKey(starlark.String("main_app"), starlark.String(app.MainApp))
		if app.Auth == types.AppAuthnDefault {
			v.SetKey(starlark.String("auth"), starlark.String(c.server.config.Security.AppDefaultAuthType))
		} else {
			v.SetKey(starlark.String("auth"), starlark.String(app.Auth))
		}
		v.SetKey(starlark.String("source_url"), starlark.String(app.SourceUrl))
		v.SetKey(starlark.String("spec"), starlark.String(app.Spec))

		ret.Append(&v)
	}
	return &ret, nil
}
