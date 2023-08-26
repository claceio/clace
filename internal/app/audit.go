// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/claceio/clace/internal/stardefs"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (a *App) Audit() (*utils.AuditResult, error) {
	buf, err := a.fs.ReadFile(APP_FILE_NAME)
	if err != nil {
		return nil, err
	}

	auditLoader := func(t *starlark.Thread, module string) (starlark.StringDict, error) {
		// The loader in audit mode is used to track the modules that are loaded.
		// A copy of the real loader's response is returned, with builtins replaced with dummy methods,
		// so that the audit can be run without any side effects
		pluginDict, err := a.loaderImpl(t, module)
		if err != nil {
			return nil, err
		}

		// Replace all the builtins with dummy methods
		dummyDict := make(starlark.StringDict)
		for k, v := range pluginDict {
			val := make(starlark.StringDict)
			if s, ok := v.(*starlarkstruct.Struct); ok {
				for _, attr := range s.AttrNames() {
					val[attr] = starlark.NewBuiltin(k, func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
						a.Info().Msgf("Plugin called during audit: %s.%s.%s", module, k, attr)
						return starlarkstruct.FromStringDict(starlarkstruct.Default, make(starlark.StringDict)), nil
					})
				}
			}
			dummyDict[k] = starlarkstruct.FromStringDict(starlarkstruct.Default, val)
		}

		return dummyDict, nil
	}

	thread := &starlark.Thread{
		Name:  a.Path,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) }, // TODO use logger
		Load:  auditLoader,
	}

	builtin := stardefs.CreateBuiltin()
	if builtin == nil {
		return nil, errors.New("error creating builtin")
	}

	_, prog, err := starlark.SourceProgram(APP_FILE_NAME, buf, builtin.Has)
	if err != nil {
		return nil, fmt.Errorf("parsing source failed %v", err)
	}

	loads := []string{}
	for i := 0; i < prog.NumLoads(); i++ {
		p, _ := prog.Load(i)
		if !slices.Contains(loads, p) {
			loads = append(loads, p)
		}
	}

	// This runs the starlark script, with dummy plugin methods
	// The intent is to load the permissions from the app definition while trying
	// to avoid any potential side effects from script
	globals, err := prog.Init(thread, builtin)
	if err != nil {
		return nil, fmt.Errorf("source init failed %v", err)
	}

	return a.createAuditResponse(loads, globals)
}

func needsApproval(a *utils.AuditResult) bool {
	if !slices.Equal(a.NewLoads, a.ApprovedLoads) {
		return true
	}

	permEquals := func(a, b utils.Permission) bool {
		if a.Plugin != b.Plugin || a.Method != b.Method {
			return false
		}
		if !slices.Equal(a.Arguments, b.Arguments) {
			return false
		}
		return true
	}

	//TODO: sort slices before checking equality
	return !slices.EqualFunc(a.NewPermissions, a.ApprovedPermissions, permEquals)
}

func (a *App) createAuditResponse(loads []string, globals starlark.StringDict) (*utils.AuditResult, error) {
	// the App entry should not get updated during the audit call, since there
	// can be audit calls when the app is running.
	appDef, err := verifyConfig(globals)
	if err != nil {
		return nil, err
	}

	perms := []utils.Permission{}
	results := utils.AuditResult{
		NewLoads:            loads,
		NewPermissions:      perms,
		ApprovedLoads:       a.Loads,
		ApprovedPermissions: a.Permissions,
	}
	permissions, err := appDef.Attr("permissions")
	if err != nil {
		// permission order needs to match for now
		results.NeedsApproval = needsApproval(&results)
		return &results, nil
	}

	var ok bool
	var permList *starlark.List
	if permList, ok = permissions.(*starlark.List); !ok {
		return nil, fmt.Errorf("permissions is not a list")
	}
	iter := permList.Iterate()
	var val starlark.Value
	count := -1
	for iter.Next(&val) {
		count++
		var perm *starlarkstruct.Struct
		if perm, ok = val.(*starlarkstruct.Struct); !ok {
			return nil, fmt.Errorf("permissions entry %d is not a struct", count)
		}
		a.Info().Msgf("perm: %+v", perm)
		var pluginStr, methodStr string
		var args []string
		if pluginStr, err = getStringAttr(perm, "plugin"); err != nil {
			return nil, err
		}
		if methodStr, err = getStringAttr(perm, "method"); err != nil {
			return nil, err
		}
		if args, err = getListStringAttr(perm, "arguments", true); err != nil {
			return nil, err
		}
		perms = append(perms, utils.Permission{
			Plugin:    pluginStr,
			Method:    methodStr,
			Arguments: args,
		})

	}
	results.NewPermissions = perms
	results.NeedsApproval = needsApproval(&results)
	return &results, nil
}