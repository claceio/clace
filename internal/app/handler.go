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
		requestData := Request{
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

		var handlerResponse any = map[string]any{} // no handler means empty Data map is passed into template
		if handler != nil {
			ret, err := starlark.Call(thread, handler, starlark.Tuple{requestData}, nil)
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
				if firstFrame != "" {
					msg = msg + " : " + firstFrame
				}
				http.Error(w, msg, http.StatusInternalServerError)
				return
			}

			retStruct, ok := ret.(*starlarkstruct.Struct)
			if ok {
				// Handle Redirect response
				url, err := util.GetStringAttr(retStruct, "url")
				// starlark Type() is not implemented for structs, so we can't check the type
				// Looked at the mandatory properties to decide on type for now
				if err == nil {
					// Redirect type struct returned by handler
					code, _ := util.GetIntAttr(retStruct, "code")
					a.Trace().Msgf("Redirecting to %s with code %d", url, code)
					http.Redirect(w, r, url, int(code))
					return
				}

				// response type struct returned by handler Instead of template defined in
				// the route, use the template specified in the response
				err, done := a.handleResponse(retStruct, w, requestData, rtype)
				if done {
					return
				}

				http.Error(w, fmt.Sprintf("Error handling response: %s", err), http.StatusInternalServerError)
				return
			}

			handlerResponse, err = utils.UnmarshalStarlark(ret)
			if err != nil {
				a.Error().Err(err).Msg("error converting response")
				http.Error(w, err.Error(), http.StatusInternalServerError)
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

func (a *App) handleResponse(retStruct *starlarkstruct.Struct, w http.ResponseWriter, requestData Request, rtype string) (error, bool) {
	templateBlock, err := util.GetStringAttr(retStruct, "block")
	if err != nil {
		return err, false
	}

	data, err := retStruct.Attr("data")
	if err != nil {
		a.Error().Err(err).Msg("error getting data from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	responseRtype, err := util.GetStringAttr(retStruct, "type")
	if err != nil {
		a.Error().Err(err).Msg("error getting type from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}
	if responseRtype == "" {
		// Default to the type set at the route level
		responseRtype = rtype
	}

	if templateBlock == "" && responseRtype != "json" {
		return fmt.Errorf("block not defined in response and type is not html"), false
	}

	code, err := util.GetIntAttr(retStruct, "code")
	if err != nil {
		a.Error().Err(err).Msg("error getting code from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	retarget, err := util.GetStringAttr(retStruct, "retarget")
	if err != nil {
		a.Error().Err(err).Msg("error getting retarget from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	reswap, err := util.GetStringAttr(retStruct, "reswap")
	if err != nil {
		a.Error().Err(err).Msg("error getting reswap from response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	templateValue, err := utils.UnmarshalStarlark(data)
	if err != nil {
		a.Error().Err(err).Msg("error converting response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}

	if strings.ToLower(responseRtype) == "json" {
		// If the route type is JSON, then return the handler response as JSON
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(templateValue)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return err, true
		}
		return nil, true
	}

	requestData.Data = templateValue
	if retarget != "" {
		w.Header().Add("HX-Retarget", retarget)
	}
	if reswap != "" {
		w.Header().Add("HX-Reswap", reswap)
	}

	w.WriteHeader(int(code))
	err = a.template.ExecuteTemplate(w, templateBlock, requestData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, true
	}
	return nil, true
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
