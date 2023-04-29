// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"

	"github.com/claceio/clace/internal/utils"
	"github.com/claceio/clace/internal/utils/chi"
	"github.com/google/uuid"
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
	router.Get("/test", handler.test)
	return handler

}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	h.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("Received request")

}

func (h *Handler) serveInternal() http.Handler {
	r := chi.NewRouter()
	r.Post("/app/*", h.createApp)
	return r
}

func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	h.Trace().Msgf("Creat app called %s", r.URL.String())
	appPath := chi.URLParam(r, "*")

	if len(appPath) == 0 || appPath[0] != '/' {
		appPath = "/" + appPath
	}
	var app utils.App
	err := json.NewDecoder(r.Body).Decode(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error parsing App body")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	app.Path = appPath
	app.Id = uuid.New().String()
	err = h.server.db.AddApp(&app)
	if err != nil {
		h.Error().Err(err).Msg("Error adding App")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.Info().Msgf("Created app %s %s", appPath, app)
}
