// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
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

	router.Use(middleware.Logger)
	router.Use(middleware.CleanPath)

	router.Mount(utils.INTERNAL_URL_PREFIX, handler.serveInternal(false))

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

	router.Use(middleware.Logger)
	router.Use(AddVaryHeader)
	router.Use(middleware.CleanPath)
	router.Use(middleware.Compress(5, COMPRESSION_ENABLED_MIME_TYPES...))

	if config.Security.AdminOverTCP {
		// Mount the internal API's only if admin over TCP is enabled
		logger.Warn().Msg("Admin API access over TCP is enabled, enable 2FA for admin user account")
		router.Mount(utils.INTERNAL_URL_PREFIX, handler.serveInternal(true))
	} else {
		router.Mount(utils.INTERNAL_URL_PREFIX, http.NotFoundHandler()) // reserve the path
	}

	router.HandleFunc("/*", handler.callApp)
	return handler
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

func (h *Handler) serveInternal(enableBasicAuth bool) http.Handler {

	// These API's are mounted at /_clace
	r := chi.NewRouter()

	// Get apps
	r.Get("/apps", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.getApps)
	}))

	// Create app
	r.Post("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.createApp)
	}))
	r.Post("/app/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.createApp)
	}))

	// Delete app
	r.Delete("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.deleteApp)
	}))
	r.Delete("/app/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.deleteApp)
	}))

	// API to audit the plugin usage and permissions for the app
	r.Post("/audit", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.auditApp)
	}))
	r.Post("/audit/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.auditApp)
	}))

	return r
}

func normalizePath(inp string) string {
	if len(inp) == 0 || inp[0] != '/' {
		inp = "/" + inp
	}
	if len(inp) > 1 {
		inp = strings.TrimRight(inp, "/")
	}
	return inp
}

func validatePath(inp string) error {
	if strings.Contains(inp, "/..") {
		return fmt.Errorf("path cannot contain '/..'")
	}
	if strings.Contains(inp, "../") {
		return fmt.Errorf("path cannot contain '../'")
	}
	if strings.Contains(inp, "/./") {
		return fmt.Errorf("path cannot contain '/./'")
	}
	if strings.HasSuffix(inp, "/.") {
		return fmt.Errorf("path cannot end with '/.'")
	}
	parts := strings.Split(inp, "/")
	lastPart := parts[len(parts)-1]
	if strings.Contains(lastPart, "_cl_") {
		return fmt.Errorf("last section of path cannot contain _cl_, clace reserved path")
	}
	return nil
}

func (h *Handler) apiHandler(w http.ResponseWriter, r *http.Request, enableBasicAuth bool, apiFunc func(r *http.Request) (any, error)) {
	if enableBasicAuth {
		authStatus := h.server.authHandler.authenticate(r.Header.Get("Authorization"))
		if !authStatus {
			w.Header().Add("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, REALM))
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	resp, err := apiFunc(r)
	h.Trace().Str("method", r.Method).Str("url", r.URL.String()).Err(err).Msg("API Received request")
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
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		h.Error().Err(err).Msg("error encoding response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) getApps(r *http.Request) (any, error) {
	appPath := r.URL.Query().Get("appPath")
	internalStr := r.URL.Query().Get("internal")
	internal := false
	if internalStr != "" {
		var err error
		if internal, err = strconv.ParseBool(internalStr); err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
		}
	}

	apps, err := h.server.GetAllApps(internal)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusNotFound)
	}

	filteredApps, err := parseAppPathSpec(appPath, apps)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	ret := utils.AppListResponse{Apps: make([]utils.AppResponse, 0, len(filteredApps))}
	for _, app := range filteredApps {
		retApp, err := h.server.GetApp(app, false)
		if err != nil {
			return nil, utils.CreateRequestError(err.Error(), http.StatusInternalServerError)
		}
		ret.Apps = append(ret.Apps, utils.AppResponse{AppEntry: *retApp.AppEntry})
	}

	return ret, nil
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
	if err := validatePath(appPath); err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

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
	if appRequest.AppAuthn != "" {
		authType := utils.AppAuthnType(strings.ToLower(string(appRequest.AppAuthn)))
		if authType != utils.AppAuthnDefault && authType != utils.AppAuthnNone {
			return nil, utils.CreateRequestError("Invalid auth type: "+string(authType), http.StatusBadRequest)
		}
		appEntry.Rules.AuthnType = utils.AppAuthnType(strings.ToLower(string(appRequest.AppAuthn)))
	} else {
		appEntry.Rules.AuthnType = utils.AppAuthnDefault
	}

	appEntry.Metadata = utils.Metadata{
		Version:     1,
		GitBranch:   appRequest.GitBranch,
		GitCommit:   appRequest.GitCommit,
		GitAuthName: appRequest.GitAuthName,
	}

	auditResult, err := h.server.CreateApp(r.Context(), &appEntry, approve)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	return auditResult, nil
}

func (h *Handler) deleteApp(r *http.Request) (any, error) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	appPath = normalizePath(appPath)
	err := h.server.DeleteApp(r.Context(), utils.CreateAppPathDomain(appPath, domain))
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
	auditResult, err := h.server.AuditApp(r.Context(), utils.CreateAppPathDomain(appPath, domain), approve)
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
