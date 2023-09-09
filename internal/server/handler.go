// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"golang.org/x/crypto/bcrypt"
)

const (
	REALM               = "clace"
	INTERNAL_URL_PREFIX = "/_clace"
)

type Handler struct {
	*utils.Logger
	config *utils.ServerConfig
	server *Server
	router *chi.Mux
}

// NewHandler creates a new handler
func NewHandler(logger *utils.Logger, config *utils.ServerConfig, server *Server) *Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil && rvr != http.ErrAbortHandler {
					msg := fmt.Sprint(rvr)
					logger.Error().Str("recover", msg).Str("trace", string(debug.Stack())).Msg("Error during request processing")
					http.Error(w, msg, http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	})

	handler := &Handler{
		Logger: logger,
		config: config,
		server: server,
		router: router,
	}

	router.Use(handler.createAuthMiddleware)
	router.Mount(INTERNAL_URL_PREFIX, handler.serveInternal())
	router.HandleFunc("/*", handler.callApp)
	return handler
}

func (h *Handler) createAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			h.basicAuthFailed(w)
			return
		}

		if h.config.AdminUser == "" {
			h.Warn().Msg("No admin username specified, basic auth not available")
			h.basicAuthFailed(w)
			return
		}

		if subtle.ConstantTimeCompare([]byte(h.config.AdminUser), []byte(user)) != 1 {
			h.Warn().Msg("Admin username does not match")
			h.basicAuthFailed(w)
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(h.config.AdminPasswordBcrypt), []byte(pass))
		if err != nil {
			h.Warn().Err(err).Msg("Password match failed")
			h.basicAuthFailed(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) basicAuthFailed(w http.ResponseWriter) {
	w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
	w.WriteHeader(http.StatusUnauthorized)
}

func (h *Handler) callApp(w http.ResponseWriter, r *http.Request) {
	h.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")

	domain := r.Host
	if strings.Contains(domain, ":") {
		domain = strings.Split(domain, ":")[0]
	}
	matchedApp, err := h.server.MatchApp(domain, r.URL.Path)
	if err != nil {
		h.Error().Err(err).Str("path", r.URL.Path).Msg("No app matched request")
		http.Error(w, "No matching app found", http.StatusNotFound)
		return
	}

	h.server.serveApp(w, r, matchedApp)
}

func (h *Handler) serveInternal() http.Handler {
	// These API's are mounted at /_clace
	r := chi.NewRouter()

	// Get app
	r.Get("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.getApp)
	}))
	r.Get("/app/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.getApp)
	}))

	// Create app
	r.Post("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.createApp)
	}))
	r.Post("/app/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.createApp)
	}))

	// Delete app
	r.Delete("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.deleteApp)
	}))
	r.Delete("/app/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.deleteApp)
	}))

	// API to audit the plugin usage and permissions for the app
	r.Post("/audit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.auditApp)
	}))
	r.Post("/audit/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, h.auditApp)
	}))

	return r
}

func normalizePath(path string) string {
	if len(path) == 0 || path[0] != '/' {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func (h *Handler) apiHandler(w http.ResponseWriter, r *http.Request, apiFunc func(r *http.Request) (any, error)) {
	resp, err := apiFunc(r)
	h.Trace().Str("method", r.Method).Str("url", r.URL.String()).Interface("resp", resp).Err(err).Msg("API Received request")
	if err != nil {
		if reqError, ok := err.(utils.RequestError); ok {
			w.Header().Add("Content-Type", "application/json")
			errStr, _ := json.Marshal(reqError)
			http.Error(w, string(errStr), reqError.Code)
			return
		}
		h.Error().Err(err).Msg("error in api func call")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if resp == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	h.Info().Msgf("response: %+v", resp)
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		h.Error().Err(err).Msg("error encoding response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) getApp(r *http.Request) (any, error) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	app, err := h.server.GetApp(utils.CreateAppPathDomain(appPath, domain), false)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusNotFound)
	}

	return app.AppEntry, nil
}

func (h *Handler) createApp(r *http.Request) (any, error) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	matchedApp, err := h.server.MatchAppForDomain(domain, appPath)
	if err != nil {
		return nil, utils.CreateRequestError(
			fmt.Sprintf("error matching app: %s", err), http.StatusInternalServerError)
	}
	if matchedApp != "" {
		return nil, utils.CreateRequestError(
			fmt.Sprintf("App already exists at %s", matchedApp), http.StatusBadRequest)
	}

	var appRequest utils.CreateAppRequest
	err = json.NewDecoder(r.Body).Decode(&appRequest)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	var appEntry utils.AppEntry
	appEntry.Path = appPath
	appEntry.Domain = domain
	appEntry.SourceUrl = appRequest.SourceUrl
	appEntry.IsDev = appRequest.IsDev
	appEntry.AutoSync = appRequest.AutoSync
	appEntry.AutoReload = appRequest.AutoReload

	_, err = h.server.AddApp(&appEntry)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	return appEntry, nil
}

func (h *Handler) deleteApp(r *http.Request) (any, error) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	err := h.server.DeleteApp(utils.CreateAppPathDomain(appPath, domain))
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	h.Trace().Str("appPath", appPath).Msg("Deleted app successfully")
	return nil, nil
}

func (h *Handler) auditApp(r *http.Request) (any, error) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")
	approve := r.URL.Query().Get("approve")
	approveBool := false
	if approve != "" {
		var err error
		if approveBool, err = strconv.ParseBool(r.URL.Query().Get("approve")); err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
		}
	}

	appPath = normalizePath(appPath)
	auditResult, err := h.server.AuditApp(utils.CreateAppPathDomain(appPath, domain), approveBool)
	return auditResult, err
}
