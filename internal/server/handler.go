// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

const (
	VARY_HEADER = "Vary"
	DRY_RUN_ARG = "dryRun"
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
				fmt.Fprintf(os.Stderr, "Panic %s: %s\n", msg, string(debug.Stack()))
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

	h.server.serveApp(w, r, matchedApp.AppPathDomain)
}

func validatePathForCreate(inp string) error {
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

func parseBoolArg(arg string, defaultValue bool) (bool, error) {
	if arg != "" {
		ret, err := strconv.ParseBool(arg)
		if err != nil {
			return defaultValue, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
		}
		return ret, nil
	}
	return defaultValue, nil
}

func (h *Handler) getApps(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	internal, err := parseBoolArg(r.URL.Query().Get("internal"), false)
	if err != nil {
		return nil, err
	}

	filteredApps, err := h.server.GetApps(r.Context(), pathSpec, internal)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return &utils.AppListResponse{Apps: filteredApps}, nil
}

func (h *Handler) createApp(r *http.Request) (any, error) {
	appPath := r.URL.Query().Get("appPath")
	approve, err := parseBoolArg(r.URL.Query().Get("approve"), false)
	if err != nil {
		return nil, err
	}
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	var appRequest utils.CreateAppRequest
	err = json.NewDecoder(r.Body).Decode(&appRequest)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	results, err := h.server.CreateApp(r.Context(), appPath, approve, dryRun, appRequest)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return results, nil
}

func (h *Handler) deleteApps(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	if pathSpec == "" {
		return nil, utils.CreateRequestError("pathSpec is required", http.StatusBadRequest)
	}

	results, err := h.server.DeleteApps(r.Context(), pathSpec, dryRun)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}
	return results, nil
}

func (h *Handler) approveApps(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	if pathSpec == "" {
		return nil, utils.CreateRequestError("pathSpec is required", http.StatusBadRequest)
	}

	approveResult, err := h.server.ApproveApps(r.Context(), pathSpec, dryRun)
	return approveResult, err
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

func (h *Handler) reloadApps(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	approve, err := parseBoolArg(r.URL.Query().Get("approve"), false)
	if err != nil {
		return nil, err
	}

	if pathSpec == "" {
		return nil, utils.CreateRequestError("pathSpec is required", http.StatusBadRequest)
	}
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	promote, err := parseBoolArg(r.URL.Query().Get("promote"), false)
	if err != nil {
		return nil, err
	}

	ret, err := h.server.ReloadApps(r.Context(), pathSpec, approve, dryRun, promote, r.URL.Query().Get("branch"), r.URL.Query().Get("commit"), r.URL.Query().Get("gitAuth"))
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return ret, nil
}

func (h *Handler) promoteApps(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	if pathSpec == "" {
		return nil, utils.CreateRequestError("pathSpec is required", http.StatusBadRequest)
	}

	ret, err := h.server.PromoteApps(r.Context(), pathSpec, dryRun)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return ret, nil
}

func (h *Handler) previewApp(r *http.Request) (any, error) {
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}
	appPath := r.URL.Query().Get("appPath")
	if appPath == "" {
		return nil, utils.CreateRequestError("appPath is required", http.StatusBadRequest)
	}
	commitId := r.URL.Query().Get("commitId")
	if commitId == "" {
		return nil, utils.CreateRequestError("commitId is required", http.StatusBadRequest)
	}
	approve, err := parseBoolArg(r.URL.Query().Get("approve"), false)
	if err != nil {
		return nil, err
	}

	ret, err := h.server.PreviewApp(r.Context(), appPath, commitId, approve, dryRun)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return ret, nil
}

func (h *Handler) getApp(r *http.Request) (any, error) {
	appPath := r.URL.Query().Get("appPath")
	if appPath == "" {
		return nil, utils.CreateRequestError("appPath is required", http.StatusBadRequest)
	}

	ret, err := h.server.GetAppApi(r.Context(), appPath)
	if err != nil {
		fmt.Println("aaa2", err)
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	fmt.Println("aaa", ret)

	return ret, nil
}

func (h *Handler) updateAppSettings(r *http.Request) (any, error) {
	pathSpec := r.URL.Query().Get("pathSpec")
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	if pathSpec == "" {
		return nil, utils.CreateRequestError("pathSpec is required", http.StatusBadRequest)
	}

	var updateAppRequest utils.UpdateAppRequest
	err = json.NewDecoder(r.Body).Decode(&updateAppRequest)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	ret, err := h.server.UpdateAppSettings(r.Context(), pathSpec, dryRun, updateAppRequest)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return ret, nil
}

func (h *Handler) linkAccount(r *http.Request) (any, error) {
	appPath := r.URL.Query().Get("appPath")
	dryRun, err := parseBoolArg(r.URL.Query().Get(DRY_RUN_ARG), false)
	if err != nil {
		return nil, err
	}

	if appPath == "" {
		return nil, utils.CreateRequestError("appPath is required", http.StatusBadRequest)
	}

	plugin := r.URL.Query().Get("plugin")
	account := r.URL.Query().Get("account")
	if plugin == "" || account == "" {
		return nil, utils.CreateRequestError("plugin and account are required", http.StatusBadRequest)
	}

	ret, err := h.server.LinkAccount(r.Context(), appPath, plugin, account, dryRun)
	if err != nil {
		return nil, utils.CreateRequestError(err.Error(), http.StatusBadRequest)
	}

	return ret, nil
}

func (h *Handler) serveInternal(enableBasicAuth bool) http.Handler {
	// These API's are mounted at /_clace
	r := chi.NewRouter()

	// Get apps
	r.Get("/apps", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.getApps)
	}))

	// Get app
	r.Get("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.getApp)
	}))

	// Create app
	r.Post("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.createApp)
	}))

	// Delete app
	r.Delete("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.deleteApps)
	}))

	// API to approve the plugin usage and permissions for the app
	r.Post("/approve", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.approveApps)
	}))

	// API to reload apps
	r.Post("/reload", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.reloadApps)
	}))

	// API to promote apps
	r.Post("/promote", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.promoteApps)
	}))

	// API to create a preview version of an app
	r.Post("/preview", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.previewApp)
	}))

	// API to update app settings
	r.Post("/app_settings", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.updateAppSettings)
	}))

	// API to change account links
	r.Post("/link_account", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.apiHandler(w, r, enableBasicAuth, h.linkAccount)
	}))

	return r
}
