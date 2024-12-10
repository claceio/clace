// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"

	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// PluginResponse is a starlark.Value that represents the response to a plugin request
type PluginResponse struct {
	errorCode int
	err       error
	value     any
	thread    *starlark.Thread
}

func NewErrorResponse(err error, thread *starlark.Thread) *PluginResponse {
	return &PluginResponse{
		errorCode: 1,
		err:       err,
		value:     nil,
		thread:    thread,
	}
}

func NewErrorCodeResponse(errorCode int, err error, value any) *PluginResponse {
	return &PluginResponse{
		errorCode: errorCode,
		err:       err,
		value:     value,
	}
}

func NewResponse(value any) *PluginResponse {
	return &PluginResponse{
		value: value,
	}
}

func (r *PluginResponse) Attr(name string) (starlark.Value, error) {
	switch name {
	case "error_code":
		return starlark.MakeInt(r.errorCode), nil
	case "error":
		// Error value is being checked in the handler code, clear the thread local state
		if r.thread != nil {
			r.thread.SetLocal(types.TL_PLUGIN_API_FAILED_ERROR, nil)
		}

		if r.err == nil {
			return starlark.None, nil
		}
		return starlark.String(r.err.Error()), nil
	case "value":
		if r.err != nil {
			// Value is being accessed when there was an error, abort
			return nil, r.err
		}

		if r.value == nil {
			return starlark.None, nil
		}

		if v, ok := r.value.(starlark.Value); ok {
			return v, nil
		}
		if v, ok := r.value.(*starlarkstruct.Struct); ok {
			return v, nil
		}
		return starlark_type.MarshalStarlark(r.value)

	default:
		return starlark.None, fmt.Errorf("response has no attribute '%s'", name)
	}
}

func (r *PluginResponse) AttrNames() []string {
	return []string{"error_code", "error", "value"}
}

func (r *PluginResponse) String() string {
	return fmt.Sprintf("%d:%s:%s", r.errorCode, r.err, r.value)
}

func (r *PluginResponse) Type() string {
	return "Response"
}

func (r *PluginResponse) Freeze() {
}

func (r *PluginResponse) Truth() starlark.Bool {
	// Error value is being checked in the handler code, clear the thread local state
	if r.thread != nil {
		r.thread.SetLocal(types.TL_PLUGIN_API_FAILED_ERROR, nil)
	}
	return r.err == nil
}

func (r *PluginResponse) Hash() (uint32, error) {
	var err error
	var errValue starlark.Value
	errValue, err = r.Attr("error")
	if err != nil {
		return 0, err
	}

	var value starlark.Value
	value, err = r.Attr("value")
	if err != nil {
		return 0, err
	}
	return starlark.Tuple{starlark.MakeInt(r.errorCode), errValue, value}.Hash()
}

func (r *PluginResponse) UnmarshalStarlarkType() (any, error) {
	return map[string]any{
		"error_code": r.errorCode,
		"error":      r.err,
		"value":      r.value,
	}, nil
}

var _ starlark.Value = (*PluginResponse)(nil)
var _ starlark_type.TypeUnmarshaler = (*PluginResponse)(nil)
