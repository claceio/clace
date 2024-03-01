// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/claceio/clace/internal/app/util"
	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func (a *App) earlyHints(w http.ResponseWriter, r *http.Request) {
	sendHint := false
	for _, f := range a.sourceFS.StaticFiles() {
		if strings.HasSuffix(f, ".css") {
			sendHint = true
			w.Header().Add("Link", fmt.Sprintf("<%s>; rel=preload; as=style",
				path.Join(a.Path, a.sourceFS.HashName(f))))
		} else if strings.HasSuffix(f, ".js") {
			if !strings.HasSuffix(f, "sse.js") {
				sendHint = true
				w.Header().Add("Link", fmt.Sprintf("<%s>; rel=preload; as=script",
					path.Join(a.Path, a.sourceFS.HashName(f))))
			}
		}
	}

	if sendHint {
		a.Trace().Msg("Sending early hints for static files")
		w.WriteHeader(http.StatusEarlyHints)
	}
}

func getRequestUrl(r *http.Request) string {
	if r.TLS != nil {
		return fmt.Sprintf("https://%s", r.Host)
	} else {
		return fmt.Sprintf("http://%s", r.Host)
	}
}

func (a *App) createHandlerFunc(html, block string, handler starlark.Callable, rtype string) http.HandlerFunc {
	goHandler := func(w http.ResponseWriter, r *http.Request) {
		thread := &starlark.Thread{
			Name:  a.Path,
			Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
		}

		// Save the request context in the starlark thread local
		thread.SetLocal(utils.TL_CONTEXT, r.Context())

		isHtmxRequest := r.Header.Get("HX-Request") == "true" && !(r.Header.Get("HX-Boosted") == "true")

		if a.Config.Routing.EarlyHints && !a.IsDev && r.Method == http.MethodGet &&
			r.Header.Get("sec-fetch-mode") == "navigate" &&
			!(strings.ToLower(rtype) == "json") && !(isHtmxRequest && block != "") {
			// Prod mode, for a GET request from newer browsers on a top level HTML page, send http early hints
			a.earlyHints(w, r)
		}

		appPath := a.Path
		if appPath == "/" {
			appPath = ""
		}
		pagePath := r.URL.Path
		if pagePath == "/" {
			pagePath = ""
		}
		appUrl := getRequestUrl(r) + appPath
		requestData := utils.Request{
			AppName:     a.Name,
			AppPath:     appPath,
			AppUrl:      appUrl,
			PagePath:    pagePath,
			PageUrl:     appUrl + pagePath,
			Method:      r.Method,
			IsDev:       a.IsDev,
			IsPartial:   isHtmxRequest,
			PushEvents:  a.Config.Routing.PushEvents,
			HtmxVersion: a.Config.Htmx.Version,
			Headers:     r.Header,
			RemoteIP:    getRemoteIP(r),
		}

		chiContext := chi.RouteContext(r.Context())
		params := map[string]string{}
		if chiContext != nil && chiContext.URLParams.Keys != nil {
			for i, k := range chiContext.URLParams.Keys {
				params[k] = chiContext.URLParams.Values[i]
			}
		}
		requestData.UrlParams = params

		r.ParseForm()
		requestData.Form = r.Form
		requestData.Query = r.URL.Query()
		requestData.PostForm = r.PostForm

		var deferredCleanup func() error
		var handlerResponse any = map[string]any{} // no handler means empty Data map is passed into template
		if handler != nil {
			deferredCleanup = func() error {
				// Check for any deferred cleanups
				err := runDeferredCleanup(thread)
				if err != nil {
					a.Error().Err(err).Msg("error cleaning up plugins")
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return err
				}
				return nil
			}

			defer deferredCleanup()

			// Call the handler function
			ret, err := starlark.Call(thread, handler, starlark.Tuple{requestData}, nil)

			if err == nil && a.errorHandler != nil {
				pluginErrLocal := thread.Local(utils.TL_PLUGIN_API_FAILED_ERROR)
				if pluginErrLocal != nil {
					pluginErr := pluginErrLocal.(error)
					a.Error().Err(pluginErr).Msg("handler had plugin API failure")
					err = pluginErr // handle as if the handler had returned an error
				}
			}

			if err != nil {
				a.Error().Err(err).Msg("error calling handler")

				firstFrame := ""
				if evalErr, ok := err.(*starlark.EvalError); ok {
					// Iterate through the CallFrame stack for debugging information
					for i, frame := range evalErr.CallStack {
						fmt.Printf("Function: %s, Position: %s\n", frame.Name, frame.Pos)
						if i == 0 {
							firstFrame = fmt.Sprintf("Function %s, Position %s", frame.Name, frame.Pos)
						}
					}
				}

				msg := err.Error()
				if firstFrame != "" && a.IsDev {
					msg = msg + " : " + firstFrame
				}

				if a.errorHandler == nil {
					// No err handler defined, abort
					http.Error(w, msg, http.StatusInternalServerError)
					return
				}

				// error handler is defined, call it
				valueDict := starlark.Dict{}
				valueDict.SetKey(starlark.String("error"), starlark.String(msg))
				ret, err = starlark.Call(thread, a.errorHandler, starlark.Tuple{requestData, &valueDict}, nil)
				if err != nil {
					// error handler itself failed
					firstFrame := ""
					if evalErr, ok := err.(*starlark.EvalError); ok {
						// Iterate through the CallFrame stack for debugging information
						for i, frame := range evalErr.CallStack {
							fmt.Printf("Function: %s, Position: %s\n", frame.Name, frame.Pos)
							if i == 0 {
								firstFrame = fmt.Sprintf("Function %s, Position %s", frame.Name, frame.Pos)
							}
						}
					}

					msg := err.Error()
					if firstFrame != "" && a.IsDev {
						msg = msg + " : " + firstFrame
					}
					http.Error(w, msg, http.StatusInternalServerError)
					return
				}
			}

			retStruct, ok := ret.(*starlarkstruct.Struct)
			if ok {
				// response type struct returned by handler Instead of template defined in
				// the route, use the template specified in the response
				done, err := a.handleResponse(retStruct, r, w, requestData, rtype, deferredCleanup)
				if done {
					return
				}

				http.Error(w, fmt.Sprintf("Error handling response: %s", err), http.StatusInternalServerError)
				return
			}

			if ret != nil {
				// Response from handler, or if handler failed, response from error_handler if defined
				handlerResponse, err = utils.UnmarshalStarlark(ret)
				if err != nil {
					a.Error().Err(err).Msg("error converting response")
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}

		if deferredCleanup != nil {
			if deferredCleanup() != nil {
				return
			}
		}

		if strings.ToLower(rtype) == "json" {
			// If the route type is JSON, then return the handler response as JSON
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(handlerResponse)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		requestData.Data = handlerResponse
		var err error
		if isHtmxRequest && block != "" {
			a.Trace().Msgf("Rendering block %s", block)
			err = a.template.ExecuteTemplate(w, block, requestData)
		} else {
			referrer := r.Header.Get("Referer")
			isUpdateRequest := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
			if !isHtmxRequest && isUpdateRequest && block != "" && referrer != "" {
				// If block is defined, and this is a non-GET request, then redirect to the referrer page
				// This handles the Post/Redirect/Get pattern required if HTMX is disabled
				a.Trace().Msgf("Redirecting to %s with code %d", referrer, http.StatusSeeOther)
				http.Redirect(w, r, referrer, http.StatusSeeOther)
				return
			} else {
				a.Trace().Msgf("Rendering page %s", html)
				err = a.template.ExecuteTemplate(w, html, requestData)
			}
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	return goHandler
}

func (a *App) handleResponse(retStruct *starlarkstruct.Struct, r *http.Request, w http.ResponseWriter, requestData utils.Request, rtype string, deferredCleanup func() error) (bool, error) {
	// Handle ace.redirect type struct returned by handler
	url, err := util.GetStringAttr(retStruct, "url")
	// starlark Type() is not implemented for structs, so we can't check the type
	// Looked at the mandatory properties to decide on type for now
	if err == nil {
		// Redirect type struct returned by handler
		code, err1 := util.GetIntAttr(retStruct, "code")
		refresh, err2 := util.GetBoolAttr(retStruct, "refresh")
		if err1 != nil || err2 != nil {
			http.Error(w, "Invalid redirect response", http.StatusInternalServerError)
		}

		if refresh {
			w.Header().Add("HX-Refresh", "true")
		}
		a.Trace().Msgf("Redirecting to %s with code %d", url, code)
		if deferredCleanup != nil {
			if err := deferredCleanup(); err != nil {
				return false, err
			}
		}
		http.Redirect(w, r, url, int(code))
		return true, nil
	}

	// Handle ace.response type struct returned by handler
	templateBlock, err := util.GetStringAttr(retStruct, "block")
	if err != nil {
		return false, err
	}

	data, err := retStruct.Attr("data")
	if err != nil {
		a.Error().Err(err).Msg("error getting data from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	responseRtype, err := util.GetStringAttr(retStruct, "type")
	if err != nil {
		a.Error().Err(err).Msg("error getting type from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}
	if responseRtype == "" {
		// Default to the type set at the route level
		responseRtype = rtype
	}

	if templateBlock == "" && responseRtype != "json" {
		return false, fmt.Errorf("block not defined in response and type is not json")
	}

	code, err := util.GetIntAttr(retStruct, "code")
	if err != nil {
		a.Error().Err(err).Msg("error getting code from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	retarget, err := util.GetStringAttr(retStruct, "retarget")
	if err != nil {
		a.Error().Err(err).Msg("error getting retarget from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	reswap, err := util.GetStringAttr(retStruct, "reswap")
	if err != nil {
		a.Error().Err(err).Msg("error getting reswap from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	redirect, err := util.GetStringAttr(retStruct, "redirect")
	if err != nil {
		a.Error().Err(err).Msg("error getting redirect from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	templateValue, err := utils.UnmarshalStarlark(data)
	if err != nil {
		a.Error().Err(err).Msg("error converting response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}

	if strings.ToLower(responseRtype) == "json" {
		if deferredCleanup != nil && deferredCleanup() != nil {
			return true, nil
		}
		// If the route type is JSON, then return the handler response as JSON
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(templateValue)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return true, nil
		}
		return true, nil
	}

	requestData.Data = templateValue
	if retarget != "" {
		w.Header().Add("HX-Retarget", retarget)
	}
	if reswap != "" {
		w.Header().Add("HX-Reswap", reswap)
	}
	if redirect != "" {
		w.Header().Add("HX-Redirect", redirect)
	}

	if deferredCleanup != nil && deferredCleanup() != nil {
		return true, nil
	}
	w.WriteHeader(int(code))
	err = a.template.ExecuteTemplate(w, templateBlock, requestData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return true, nil
	}
	return true, nil
}

func getRemoteIP(r *http.Request) string {
	remoteIP := r.Header.Get("X-Real-IP")
	if remoteIP == "" {
		remoteIP = r.Header.Get("X-Forwarded-For")
	}
	if remoteIP == "" && r.RemoteAddr != "" {
		if r.RemoteAddr[0] == '[' {
			// IPv6
			remoteIP = strings.Split(r.RemoteAddr, "]")[0][1:]
		} else {
			remoteIP = strings.Split(r.RemoteAddr, ":")[0]
		}
	}
	return remoteIP
}
