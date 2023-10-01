// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/sha512"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"golang.org/x/crypto/bcrypt"
)

const (
	VARY_HEADER = "Vary"
)

var (
	COMPRESSION_ENABLED_MIME_TYPES = []string{
		"text/html",
		"text/css",
		"text/plain",
		"text/xml",
		"text/x-component",
		"text/javascript",
		"application/x-javascript",
		"application/javascript",
		"application/json",
		"application/manifest+json",
		"application/vnd.api+json",
		"application/xml",
		"application/xhtml+xml",
		"application/rss+xml",
		"application/atom+xml",
		"application/vnd.ms-fontobject",
		"application/x-font-ttf",
		"application/x-font-opentype",
		"application/x-font-truetype",
		"image/svg+xml",
		"image/x-icon",
		"image/vnd.microsoft.icon",
		"font/ttf",
		"font/eot",
		"font/otf",
		"font/opentype",
	}
)

const (
	REALM = "clace"
)

type Handler struct {
	*utils.Logger
	config *utils.ServerConfig
	server *Server
	router *chi.Mux
}

func panicRecovery(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil && rvr != http.ErrAbortHandler {
				msg := fmt.Sprint(rvr)
				//logger.Error().Str("recover", msg).Str("trace", string(debug.Stack())).Msg("Error during request processing")
				// TODO log
				http.Error(w, msg, http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// NewUDSHandler creates a new handler for admin APIs over the unix domain socket
func NewUDSHandler(logger *utils.Logger, config *utils.ServerConfig, server *Server) *Handler {
	router := chi.NewRouter()
	router.Use(panicRecovery)

	handler := &Handler{
		Logger: logger,
		config: config,
		server: server,
		router: router,
	}

	router.Use(middleware.CleanPath)

	router.Mount(utils.INTERNAL_URL_PREFIX, handler.serveInternal())

	// App APIs are not mounted over UDS
	// No authentication middleware is added for UDS, the unix file permissions are used
	return handler
}

// NewTCPHandler creates a new handler for HTTP/HTTPS requests. App API's are mounted amd
// authentication is enabled. It also mounts the internal APIs if admin over TCP is enabled
func NewTCPHandler(logger *utils.Logger, config *utils.ServerConfig, server *Server) *Handler {
	router := chi.NewRouter()
	router.Use(panicRecovery)

	handler := &Handler{
		Logger: logger,
		config: config,
		server: server,
		router: router,
	}

	router.Use(AddVaryHeader)
	router.Use(middleware.CleanPath)
	router.Use(middleware.Compress(5, COMPRESSION_ENABLED_MIME_TYPES...))
	router.Use(handler.createAuthMiddleware)

	if config.Security.AdminOverTCP {
		// Mount the internal API's only if admin over TCP is enabled
		logger.Warn().Msg("Admin API access over TCP is enabled, enable 2FA for admin user account")
		router.Mount(utils.INTERNAL_URL_PREFIX, handler.serveInternal())
	} else {
		router.Mount(utils.INTERNAL_URL_PREFIX, http.NotFoundHandler()) // reserve the path
	}

	router.HandleFunc("/*", handler.callApp)
	return handler
}

func (h *Handler) createAuthMiddleware(next http.Handler) http.Handler {
	// Cache the success auth header to avoid the bcrypt hash check penalty
	// Basic auth is supported for admin user only, and changing it requires service restart.
	// Caching the sha of the successful auth header allows us to skip the bcrypt check
	// which significantly improves performance.
	authHeaderLock := sync.RWMutex{}
	authShaCache := ""
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaderLock.RLock()
		authShaCopy := authShaCache
		authHeaderLock.RUnlock()

		inputHeader := r.Header.Get("Authorization")
		if authShaCopy != "" {
			inputSha := sha512.Sum512([]byte(inputHeader))
			inputShaSlice := inputSha[:]

			if subtle.ConstantTimeCompare(inputShaSlice, []byte(authShaCopy)) != 1 {
				h.Warn().Msg("Auth header cache check failed")
				h.basicAuthFailed(w)
				return
			}

			// Cached header matches, so we can skip the rest of the auth checks
			next.ServeHTTP(w, r)
			return
		}

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

		err := bcrypt.CompareHashAndPassword([]byte(h.config.Security.AdminPasswordBcrypt), []byte(pass))
		if err != nil {
			h.Warn().Err(err).Msg("Password match failed")
			h.basicAuthFailed(w)
			return
		}

		authHeaderLock.RLock()
		authShaCopy = authShaCache
		authHeaderLock.RUnlock()
		if authShaCopy == "" {
			// Successful request, so we can cache the auth header
			authHeaderLock.Lock()
			inputSha := sha512.Sum512([]byte(inputHeader))
			authShaCache = string(inputSha[:])
			authHeaderLock.Unlock()
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
	approveStr := r.URL.Query().Get("approve")
	approve := false
	if approveStr != "" {
		var err error
		if approve, err = strconv.ParseBool(approveStr); err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
		}
	}

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

	auditResult, err := h.server.AddApp(&appEntry, approve)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	return auditResult, nil
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
	approveStr := r.URL.Query().Get("approve")
	approve := false
	if approveStr != "" {
		var err error
		if approve, err = strconv.ParseBool(approveStr); err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
		}
	}

	appPath = normalizePath(appPath)
	auditResult, err := h.server.AuditApp(utils.CreateAppPathDomain(appPath, domain), approve)
	return auditResult, err
}

func AddVaryHeader(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		value := w.Header().Get(VARY_HEADER)
		if value != "" {
			value += ", HX-Request"
		} else {
			value = "HX-Request"
		}

		w.Header().Add(VARY_HEADER, value)
		next.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}
