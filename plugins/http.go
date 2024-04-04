// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

// Based on code from https://github.com/qri-io/starlib/blob/master/http/http.go

package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/app/starlark_type"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// AsString unquotes a starlark string value
func AsString(x starlark.Value) (string, error) {
	return strconv.Unquote(x.String())
}

// Encodings for form data.
//
// See: https://developer.mozilla.org/en-US/docs/Web/HTTP/Methods/POST
const (
	formEncodingMultipart = "multipart/form-data"
	formEncodingURL       = "application/x-www-form-urlencoded"
)

func init() {
	h := &httpPlugin{}
	pluginFuncs := []plugin.PluginFunc{
		app.CreatePluginApi(h.Get, app.READ),
		app.CreatePluginApi(h.Head, app.READ),
		app.CreatePluginApi(h.Options, app.READ),
		app.CreatePluginApi(h.Post, app.WRITE),
		app.CreatePluginApi(h.Put, app.WRITE),
		app.CreatePluginApi(h.Delete, app.WRITE),
		app.CreatePluginApi(h.Patch, app.WRITE),
	}
	app.RegisterPlugin("http", NewHttpPlugin, pluginFuncs)
}

type httpPlugin struct {
	client *http.Client
}

func NewHttpPlugin(pluginContext *types.PluginContext) (any, error) {
	return &httpPlugin{client: http.DefaultClient}, nil
}

func (h *httpPlugin) Get(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("get")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Head(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("head")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Options(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("options")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Post(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("post")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Put(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("put")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Delete(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("delete")
	return httpFunc(thread, builtin, args, kwargs)
}

func (h *httpPlugin) Patch(thread *starlark.Thread, builtin *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	httpFunc := h.reqMethod("patch")
	return httpFunc(thread, builtin, args, kwargs)
}

// reqMethod is a factory function for generating starlark builtin functions for different http request methods
func (h *httpPlugin) reqMethod(method string) func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var (
			urlv         starlark.String
			params       = &starlark.Dict{}
			headers      = &starlark.Dict{}
			formBody     = &starlark.Dict{}
			formEncoding starlark.String
			auth         starlark.Tuple
			body         starlark.String
			jsonBody     starlark.Value
		)

		if err := starlark.UnpackArgs(method, args, kwargs, "url", &urlv, "params?", &params, "headers", &headers, "body", &body, "form_body", &formBody, "form_encoding", &formEncoding, "json_body", &jsonBody, "auth", &auth); err != nil {
			return nil, err
		}

		rawurl, err := AsString(urlv)
		if err != nil {
			return nil, err
		}
		if err = setQueryParams(&rawurl, params); err != nil {
			return nil, err
		}

		req, err := http.NewRequest(strings.ToUpper(method), rawurl, nil)
		if err != nil {
			return nil, err
		}

		if err = setHeaders(req, headers); err != nil {
			return nil, err
		}
		if err = setAuth(req, auth); err != nil {
			return nil, err
		}
		if err = setBody(req, body, formBody, formEncoding, jsonBody); err != nil {
			return nil, err
		}

		res, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}

		r := &Response{*res}
		return app.NewResponse(r.Struct()), nil
	}
}

func setQueryParams(rawurl *string, params *starlark.Dict) error {
	keys := params.Keys()
	if len(keys) == 0 {
		return nil
	}

	u, err := url.Parse(*rawurl)
	if err != nil {
		return err
	}

	q := u.Query()
	for _, key := range keys {
		keystr, err := AsString(key)
		if err != nil {
			return err
		}

		val, _, err := params.Get(key)
		if err != nil {
			return err
		}
		if val.Type() != "string" {
			return fmt.Errorf("expected param value for key '%s' to be a string. got: '%s'", key, val.Type())
		}
		valstr, err := AsString(val)
		if err != nil {
			return err
		}

		q.Set(keystr, valstr)
	}

	u.RawQuery = q.Encode()
	*rawurl = u.String()
	return nil
}

func setAuth(req *http.Request, auth starlark.Tuple) error {
	if len(auth) == 0 {
		return nil
	} else if len(auth) == 2 {
		username, err := AsString(auth[0])
		if err != nil {
			return fmt.Errorf("parsing auth username string: %s", err.Error())
		}
		password, err := AsString(auth[1])
		if err != nil {
			return fmt.Errorf("parsing auth password string: %s", err.Error())
		}
		req.SetBasicAuth(username, password)
		return nil
	}
	return fmt.Errorf("expected two values for auth params tuple")
}

func setHeaders(req *http.Request, headers *starlark.Dict) error {
	keys := headers.Keys()
	if len(keys) == 0 {
		return nil
	}

	for _, key := range keys {
		keystr, err := AsString(key)
		if err != nil {
			return err
		}

		val, _, err := headers.Get(key)
		if err != nil {
			return err
		}
		if val.Type() != "string" {
			return fmt.Errorf("expected param value for key '%s' to be a string. got: '%s'", key, val.Type())
		}
		valstr, err := AsString(val)
		if err != nil {
			return err
		}

		req.Header.Add(keystr, valstr)
	}

	return nil
}

func setBody(req *http.Request, body starlark.String, formData *starlark.Dict, formEncoding starlark.String, jsondata starlark.Value) error {
	if !starlark_type.IsEmptyStarlarkString(body) {
		uq, err := strconv.Unquote(body.String())
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(strings.NewReader(uq))
		// Specifying the Content-Length ensures that https://go.dev/src/net/http/transfer.go doesnt specify Transfer-Encoding: chunked which is not supported by some endpoints.
		// This is required when using ioutil.NopCloser method for the request body (see ShouldSendChunkedRequestBody() in the library mentioned above).
		req.ContentLength = int64(len(uq))

		return nil
	}

	if jsondata != nil && jsondata.String() != "" {
		req.Header.Set("Content-Type", "application/json")

		v, err := starlark_type.UnmarshalStarlark(jsondata)
		if err != nil {
			return err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewBuffer(data))
		req.ContentLength = int64(len(data))
	}

	if formData != nil && formData.Len() > 0 {
		form := url.Values{}
		for _, key := range formData.Keys() {
			keystr, err := AsString(key)
			if err != nil {
				return err
			}

			val, _, err := formData.Get(key)
			if err != nil {
				return err
			}
			if val.Type() != "string" {
				return fmt.Errorf("expected param value for key '%s' to be a string. got: '%s'", key, val.Type())
			}
			valstr, err := AsString(val)
			if err != nil {
				return err
			}

			form.Add(keystr, valstr)
		}

		var contentType string
		switch formEncoding {
		case formEncodingURL, "":
			contentType = formEncodingURL
			req.Body = io.NopCloser(strings.NewReader(form.Encode()))
			req.ContentLength = int64(len(form.Encode()))

		case formEncodingMultipart:
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			defer mw.Close()

			contentType = mw.FormDataContentType()

			for k, values := range form {
				for _, v := range values {
					w, err := mw.CreateFormField(k)
					if err != nil {
						return err
					}
					if _, err := w.Write([]byte(v)); err != nil {
						return err
					}
				}
			}

			req.Body = io.NopCloser(&b)

		default:
			return fmt.Errorf("unknown form encoding: %s", formEncoding)
		}

		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", contentType)
		}
	}

	return nil
}

// Response represents an HTTP response, wrapping a go http.Response with
// starlark methods
type Response struct {
	http.Response
}

// Struct turns a response into a *starlark.Struct
func (r *Response) Struct() *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"url":         starlark.String(r.Request.URL.String()),
		"status_code": starlark.MakeInt(r.StatusCode),
		"headers":     r.HeadersDict(),
		"encoding":    starlark.String(strings.Join(r.TransferEncoding, ",")),

		"body": starlark.NewBuiltin("body", r.Text),
		"json": starlark.NewBuiltin("json", r.JSON),
	})
}

// HeadersDict flops
func (r *Response) HeadersDict() *starlark.Dict {
	d := new(starlark.Dict)
	for key, vals := range r.Header {
		if err := d.SetKey(starlark.String(key), starlark.String(strings.Join(vals, ","))); err != nil {
			panic(err)
		}
	}
	return d
}

// Text returns the raw data as a string
func (r *Response) Text(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body.Close()
	// reset reader to allow multiple calls
	r.Body = io.NopCloser(bytes.NewReader(data))

	return starlark.String(string(data)), nil
}

// JSON attempts to parse the response body as JSON
func (r *Response) JSON(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var data interface{}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	r.Body.Close()
	// reset reader to allow multiple calls
	r.Body = io.NopCloser(bytes.NewReader(body))
	return starlark_type.MarshalStarlark(data)
}
