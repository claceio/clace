// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

func init() {
	h := &storePlugin{}
	pluginFuncs := []app.PluginFunc{
		app.CreatePluginApi(h.Insert, false),
		app.CreatePluginApiName(h.SelectById, false, "select_by_id"),
	}
	app.RegisterPlugin("store", NewStorePlugin, pluginFuncs)
}

type storePlugin struct {
	sqlStore *SqlStore
}

func NewStorePlugin(pluginContext *app.PluginContext) (any, error) {
	sqlStore, err := NewSqlStore(pluginContext)

	return &storePlugin{
		sqlStore: sqlStore,
	}, err
}

func (s *storePlugin) Insert(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var entry Entry

	if err := starlark.UnpackArgs("insert", args, kwargs, "table", &table, "entry", &entry); err != nil {
		return nil, err
	}

	id, err := s.sqlStore.Insert(table, &entry)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(id), nil
}

func (s *storePlugin) SelectById(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var id starlark.Int

	if err := starlark.UnpackArgs("select_by_id", args, kwargs, "table", &table, "id", &id); err != nil {
		return nil, err
	}

	var idVal int64
	var ok bool
	if idVal, ok = id.Int64(); !ok || idVal < 0 {
		return utils.NewErrorResponse(fmt.Errorf("invalid id value")), nil
	}

	entry, err := s.sqlStore.SelectById(table, EntryId(idVal))
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}

	returnType, err := CreateType(table, entry)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(returnType), nil
}
