// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"go.starlark.net/starlark"
)

// Request is a starlark.Value that represents an HTTP request. A Request is created from the Go http.Request
// and passed to the starlark handler function as it only argument. The Data field is updated with the handler's
// response and then the template evaluation is done with the same Request
type Request struct {
	AppName     string
	AppPath     string
	AppUrl      string
	PagePath    string
	PageUrl     string
	Method      string
	IsDev       bool
	AutoReload  bool
	IsPartial   bool
	PushEvents  bool
	HtmxVersion string
	Headers     http.Header
	RemoteIP    string
	UrlParams   map[string]string
	Form        url.Values
	Query       url.Values
	PostForm    url.Values
	Data        any
}

func (r Request) Attr(name string) (starlark.Value, error) {
	switch name {
	case "AppName":
		return starlark.String(r.AppName), nil
	case "AppPath":
		return starlark.String(r.AppPath), nil
	case "AppUrl":
		return starlark.String(r.AppUrl), nil
	case "PagePath":
		return starlark.String(r.PagePath), nil
	case "PageUrl":
		return starlark.String(r.PageUrl), nil
	case "Method":
		return starlark.String(r.Method), nil
	case "IsDev":
		return starlark.Bool(r.IsDev), nil
	case "AutoReload":
		return starlark.Bool(r.AutoReload), nil
	case "IsPartial":
		return starlark.Bool(r.IsPartial), nil
	case "PushEvents":
		return starlark.Bool(r.PushEvents), nil
	case "HtmxVersion":
		return starlark.String(r.HtmxVersion), nil
	case "Headers":
		return utils.MarshalStarlark(r.Headers)
	case "RemoteIP":
		return starlark.String(r.RemoteIP), nil
	case "UrlParams":
		return utils.MarshalStarlark(r.UrlParams)
	case "Form":
		return utils.MarshalStarlark(r.Form)
	case "Query":
		return utils.MarshalStarlark(r.Query)
	case "PostForm":
		return utils.MarshalStarlark(r.PostForm)
	case "Data":
		return utils.MarshalStarlark(r.Data)
	default:
		return starlark.None, fmt.Errorf("request has no attribute '%s'", name)
	}
}

func (r Request) AttrNames() []string {
	return []string{"AppName", "AppPath", "AppUrl", "PagePath", "PageUrl", "Method", "IsDev", "AutoReload", "IsPartial", "PushEvents", "HtmxVersion", "Headers", "RemoteIP", "UrlParams", "Form", "Query", "PostForm", "Data"}
}

func (r Request) String() string {
	return strings.ToLower(fmt.Sprintf("%s:%s:%s", r.AppName, r.PagePath, r.Method))
}

func (r Request) Type() string {
	return "Request"
}

func (r Request) Freeze() {
}

func (r Request) Truth() starlark.Bool {
	return r.AppName != ""
}

func (r Request) Hash() (uint32, error) {
	return starlark.Tuple{starlark.String(r.AppName), starlark.String(r.PagePath), starlark.String(r.Method), starlark.String(r.RemoteIP)}.Hash()
}

var _ starlark.Value = (*Request)(nil)
