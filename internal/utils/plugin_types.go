// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"

	"go.starlark.net/starlark"
)

type NewPluginFunc func(pluginContext *PluginContext) (any, error)

// PluginMap is the plugin function mapping to PluginFuncs
type PluginMap map[string]*PluginInfo

// PluginFunc is the Clace plugin function mapping to starlark function
type PluginFunc struct {
	Name         string
	IsRead       bool
	FunctionName string
}

// PluginFuncInfo is the Clace plugin function info for the starlark function
type PluginInfo struct {
	ModuleName  string // exec
	PluginPath  string // exec.in
	FuncName    string // run
	IsRead      bool
	HandlerName string
	Builder     NewPluginFunc
}

type PluginContext struct {
	Logger    *Logger
	AppId     AppId
	StoreInfo *StoreInfo
	Config    PluginSettings
}

// PluginResponse is a starlark.Value that represents the response to a plugin request
type PluginResponse struct {
	errorCode int
	err       error
	data      any
}

func NewErrorResponse(err error) *PluginResponse {
	return &PluginResponse{
		errorCode: 1,
		err:       err,
		data:      nil,
	}
}

func NewErrorCodeResponse(errorCode int, err error, data any) *PluginResponse {
	return &PluginResponse{
		errorCode: errorCode,
		err:       err,
		data:      data,
	}
}

func NewResponse(data any) *PluginResponse {
	return &PluginResponse{
		data: data,
	}
}

func (r *PluginResponse) Attr(name string) (starlark.Value, error) {
	switch name {
	case "error_code":
		return starlark.MakeInt(r.errorCode), nil
	case "error":
		if r.err == nil {
			return starlark.None, nil
		}
		return starlark.String(r.err.Error()), nil
	case "data":
		if r.data == nil {
			return starlark.None, nil
		}

		if _, ok := r.data.(starlark.Value); ok {
			return r.data.(starlark.Value), nil
		}
		return MarshalStarlark(r.data)

	default:
		return starlark.None, fmt.Errorf("response has no attribute '%s'", name)
	}
}

func (r *PluginResponse) AttrNames() []string {
	return []string{"error_code", "error", "data"}
}

func (r *PluginResponse) String() string {
	return fmt.Sprintf("%d:%s:%s", r.errorCode, r.err, r.data)
}

func (r *PluginResponse) Type() string {
	return "Response"
}

func (r *PluginResponse) Freeze() {
}

func (r *PluginResponse) Truth() starlark.Bool {
	return r.err == nil
}

func (r *PluginResponse) Hash() (uint32, error) {
	var err error
	var errValue starlark.Value
	errValue, err = r.Attr("error")
	if err != nil {
		return 0, err
	}

	var dataValue starlark.Value
	dataValue, err = r.Attr("data")
	if err != nil {
		return 0, err
	}
	return starlark.Tuple{starlark.MakeInt(r.errorCode), errValue, dataValue}.Hash()
}

func (r *PluginResponse) UnmarshalStarlarkType() (any, error) {
	return map[string]any{
		"error_code": r.errorCode,
		"error":      r.err,
		"data":       r.data,
	}, nil
}

var _ starlark.Value = (*PluginResponse)(nil)
var _ TypeUnmarshaler = (*PluginResponse)(nil)
