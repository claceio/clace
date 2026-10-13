// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	apppkg "github.com/openrundev/openrun/internal/app"
	"github.com/openrundev/openrun/internal/types"
)

// deployWarnings collects the advisories a staged update raises without
// failing: they are returned with the response. The collector travels in the
// context from StagedUpdate down to the handlers
type deployWarnings struct {
	list []string
}

type deployWarningsKey struct{}

func withDeployWarnings(ctx context.Context) (context.Context, *deployWarnings) {
	w := &deployWarnings{}
	return context.WithValue(ctx, deployWarningsKey{}, w), w
}

// addDeployWarnings records the warnings on the context's collector, if any
func addDeployWarnings(ctx context.Context, warnings []string) {
	if w, ok := ctx.Value(deployWarningsKey{}).(*deployWarnings); ok {
		w.list = append(w.list, warnings...)
	}
}

// containerConfigPlugin is the plugin method evaluating the param and build
// arg values of a containerized app, the one whose secret allowlist those
// values resolve against
const (
	containerPlugin       = "container.in"
	containerConfigMethod = "config"
)

// secretAccessWarnings returns the advisories for values set on the app
// (params, container build args) which reference secrets the app cannot
// resolve: the app declares secret access which is not approved yet, or (for
// a containerized app) declares none. nil when no value references secrets
// or the app can resolve them. The values are evaluated by the container.in
// config call with the secrets allowed for that call, so that is the
// allowlist checked. An app which does not load is not reported on: the
// problem surfaces on the first request either way
//
// before is the app's ungranted secret access from before the update (see
// ungrantedSecretsOf): an update which changes it, such as setting the
// secrets param of a spec based app, is warned about even when the value
// itself references no secret, so the user learns the app needs approval
// at the point they declared the access. An unrelated update to an app
// whose ungranted access predates it is not warned about
func (s *Server) secretAccessWarnings(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry, values []string, before []types.Permission) []string {
	mgr := s.secretsMgr()
	if mgr == nil {
		return nil
	}
	refs := slices.ContainsFunc(values, mgr.ReferencesSecrets)
	warnings, err := withTemporaryApp(s, ctx, appEntry, tx, func(app *apppkg.App) ([]string, error) {
		audit, err := app.Audit()
		if err != nil {
			return nil, err
		}
		if len(audit.UngrantedSecrets) > 0 {
			if refs {
				return []string{ungrantedSecretsWarning(mainAppPath(appEntry), "the new value references secrets", audit.UngrantedSecrets)}, nil
			}
			if !reflect.DeepEqual(audit.UngrantedSecrets, before) {
				return []string{ungrantedSecretsWarning(mainAppPath(appEntry), "", audit.UngrantedSecrets)}, nil
			}
			return nil, nil
		}
		if refs && slices.Contains(audit.NewLoads, containerPlugin) && len(app.SecretsAllowed(containerPlugin, containerConfigMethod)) == 0 {
			return []string{noSecretAccessWarning(mainAppPath(appEntry))}, nil
		}
		return nil, nil
	})
	if err != nil {
		s.Warn().Err(err).Msgf("app %s: could not check secret access for the updated value", appEntry)
		return nil
	}
	return warnings
}

// ungrantedSecretsOf returns the app's current ungranted secret access, for
// a before/after comparison around an update. nil when the app does not
// audit (the update then only warns for values referencing secrets)
func (s *Server) ungrantedSecretsOf(ctx context.Context, tx types.Transaction, appEntry *types.AppEntry) []types.Permission {
	ungranted, err := withTemporaryApp(s, ctx, appEntry, tx, func(app *apppkg.App) ([]types.Permission, error) {
		audit, err := app.Audit()
		if err != nil {
			return nil, err
		}
		return audit.UngrantedSecrets, nil
	})
	if err != nil {
		return nil
	}
	return ungranted
}

// mainAppPath is the path a user runs approve on: the prod app's path for a
// stage entry, the entry's own path otherwise
func mainAppPath(appEntry *types.AppEntry) string {
	return mainAppPathDomain(appEntry.AppPathDomain(), appEntry.MainApp, appEntry.LinkedAppPath).String()
}

// ungrantedSecretsWarning is the advisory for an app declaring secret access
// which is neither approved nor allowed by the server config. The same text
// is produced by the CLI for create and reload from the audit result
func ungrantedSecretsWarning(appPath, context string, ungranted []types.Permission) string {
	return types.UngrantedSecretsWarning(appPath, context, ungranted)
}

// noSecretAccessWarning is the advisory for a containerized app which
// declares no secret access while a value set on it references secrets
func noSecretAccessWarning(appPath string) string {
	return fmt.Sprintf("app %s: the new value references secrets, but the app has no secret access for %s.%s "+
		"(none declared by the app and none allowed by the server config). The secrets will not resolve: declare them "+
		"on the app's %s.%s permission (the secrets param for spec based apps, e.g. --param secrets='[[\"regex:.*\"]]') "+
		"and approve the app with `openrun app approve %s`, or allow them in the server config [[permissions.allow]] entry for %s.%s",
		appPath, containerPlugin, containerConfigMethod, containerPlugin, containerConfigMethod, appPath, containerPlugin, containerConfigMethod)
}
