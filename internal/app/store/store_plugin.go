// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"errors"
	"fmt"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

func init() {
	h := &storePlugin{}
	pluginFuncs := []utils.PluginFunc{
		app.CreatePluginApiName(h.SelectById, app.READ, "select_by_id"),
		app.CreatePluginApi(h.Select, app.READ),
		app.CreatePluginApi(h.Count, app.READ),
		app.CreatePluginApi(h.Insert, app.WRITE),
		app.CreatePluginApi(h.Update, app.WRITE),
		app.CreatePluginApiName(h.DeleteById, app.WRITE, "delete_by_id"),
		app.CreatePluginApi(h.Delete, app.WRITE),
	}
	app.RegisterPlugin("store", NewStorePlugin, pluginFuncs)
}

type storePlugin struct {
	sqlStore *SqlStore
}

func NewStorePlugin(pluginContext *utils.PluginContext) (any, error) {
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
	return utils.NewResponse(int64(id)), nil
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

func (s *storePlugin) Update(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var entry Entry

	if err := starlark.UnpackArgs("update", args, kwargs, "table", &table, "entry", &entry); err != nil {
		return nil, err
	}

	success, err := s.sqlStore.Update(table, &entry)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(success), nil
}

func (s *storePlugin) DeleteById(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var id starlark.Int

	if err := starlark.UnpackArgs("delete_by_id", args, kwargs, "table", &table, "id", &id); err != nil {
		return nil, err
	}

	var idVal int64
	var ok bool
	if idVal, ok = id.Int64(); !ok || idVal < 0 {
		return utils.NewErrorResponse(fmt.Errorf("invalid id value")), nil
	}

	rows, err := s.sqlStore.DeleteById(table, EntryId(idVal))
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(rows), nil
}

func (s *storePlugin) Select(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var limit, offset starlark.Int
	var filter *starlark.Dict
	var sort *starlark.List

	if err := starlark.UnpackArgs("select", args, kwargs, "table", &table, "filter", &filter, "sort?", &sort, "offset?", &offset, "limit?", &limit); err != nil {
		return nil, err
	}

	var limitVal, offsetVal int64
	var ok bool
	if limitVal, ok = limit.Int64(); !ok || limitVal < 0 {
		return utils.NewErrorResponse(fmt.Errorf("invalid limit value")), nil
	}
	if offsetVal, ok = offset.Int64(); !ok || offsetVal < 0 {
		return utils.NewErrorResponse(fmt.Errorf("invalid offset value")), nil
	}

	if filter == nil {
		filter = starlark.NewDict(0)
	}
	if sort == nil {
		sort = starlark.NewList([]starlark.Value{})
	}

	filterUnmarshalled, err := utils.UnmarshalStarlark(filter)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}

	filterMap, ok := filterUnmarshalled.(map[string]any)
	if !ok {
		return utils.NewErrorResponse(errors.New("invalid filter")), nil
	}

	sortList, err := util.GetStringList(sort)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}

	iterator, err := s.sqlStore.Select(table, filterMap, sortList, offsetVal, limitVal)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(iterator), nil
}

func (s *storePlugin) Count(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var filter *starlark.Dict

	if err := starlark.UnpackArgs("select", args, kwargs, "table", &table, "filter", &filter); err != nil {
		return nil, err
	}

	if filter == nil {
		filter = starlark.NewDict(0)
	}

	filterUnmarshalled, err := utils.UnmarshalStarlark(filter)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}

	filterMap, ok := filterUnmarshalled.(map[string]any)
	if !ok {
		return utils.NewErrorResponse(errors.New("invalid filter")), nil
	}

	count, err := s.sqlStore.Count(table, filterMap)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(count), nil
}

func (s *storePlugin) Delete(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var filter *starlark.Dict

	if err := starlark.UnpackArgs("delete", args, kwargs, "table", &table, "filter", &filter); err != nil {
		return nil, err
	}

	if filter == nil {
		filter = starlark.NewDict(0)
	}

	filterUnmarshalled, err := utils.UnmarshalStarlark(filter)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}

	filterMap, ok := filterUnmarshalled.(map[string]any)
	if !ok {
		return utils.NewErrorResponse(errors.New("invalid filter")), nil
	}

	rows, err := s.sqlStore.Delete(table, filterMap)
	if err != nil {
		return utils.NewErrorResponse(err), nil
	}
	return utils.NewResponse(rows), nil
}
