// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/claceio/clace/internal/utils/chi"
)

// The url prefix for internal API calls
const INTERNAL_URL_PREFIX = "/_clace"

type Handler struct {
	*utils.Logger
	config *utils.ServerConfig
	server *Server
	router *chi.Mux
}

// NewHandler creates a new handler
func NewHandler(logger *utils.Logger, config *utils.ServerConfig, server *Server) *Handler {
	router := chi.NewRouter()
	handler := &Handler{
		Logger: logger,
		config: config,
		server: server,
		router: router,
	}
	router.Mount(INTERNAL_URL_PREFIX, handler.serveInternal())
	router.Get("/*", handler.matchApp)
	return handler

}

func (h *Handler) matchApp(w http.ResponseWriter, r *http.Request) {
	h.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("App Received request")

	// TODO : handle domain based routing
	domain := ""
	paths, err := h.server.db.GetAllApps(domain)
	if err != nil {
		h.Error().Err(err).Msg("Error getting apps")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	requestPath := strings.TrimRight(r.URL.Path, "/")
	matchedPath := ""
	for _, path := range paths {
		if strings.HasPrefix(requestPath, path) {
			if len(path) == len(requestPath) || requestPath[len(path)] == '/' {
				h.Info().Str("path", path).Msg("Matched app")
				matchedPath = path
				break
			}
		}
	}

	if matchedPath == "" {
		h.Error().Msg("No app matched request")
		http.Error(w, "No matching app found", http.StatusNotFound)
		return
	}

	h.server.serveApp(w, r, matchedPath, domain)

}

func (h *Handler) serveInternal() http.Handler {
	r := chi.NewRouter()
	r.Get("/app/*", h.getApp)
	r.Post("/app/*", h.createApp)
	r.Delete("/app/*", h.deleteApp)
	return r
}

func (h *Handler) getApp(w http.ResponseWriter, r *http.Request) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	if len(appPath) == 0 || appPath[0] != '/' {
		appPath = "/" + appPath
	}

	app, err := h.server.GetApp(utils.CreateAppPathDomain(appPath, domain))
	if err != nil {
		h.Error().Err(err).Msg("error getting App")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.NewEncoder(w).Encode(app.AppEntry)
	if err != nil {
		h.Error().Err(err).Msg("error enoding app")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	h.Trace().Msgf("Create app called %s", r.URL.String())
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	if len(appPath) == 0 || appPath[0] != '/' {
		appPath = "/" + appPath
	}
	var app utils.AppEntry
	err := json.NewDecoder(r.Body).Decode(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error parsing App body")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	app.Path = appPath
	app.Domain = domain
	_, err = h.server.AddApp(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error adding App")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (h *Handler) deleteApp(w http.ResponseWriter, r *http.Request) {
	appPath := chi.URLParam(r, "*")
	domain := r.URL.Query().Get("domain")

	if len(appPath) == 0 || appPath[0] != '/' {
		appPath = "/" + appPath
	}
	err := h.server.DeleteApp(utils.CreateAppPathDomain(appPath, domain))
	if err != nil {
		h.Error().Err(err).Msg("Error deleting App")
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
