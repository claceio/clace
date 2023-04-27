// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"

	"github.com/claceio/clace/internal/utils"
)

// The url prefix for internal API calls
const URL_PREFIX = "/_clace"

type Handler struct {
	*utils.Logger
	config *utils.ServerConfig
}

// NewHandler creates a new handler
func NewHandler(logger *utils.Logger, config *utils.ServerConfig) *Handler {
	return &Handler{
		Logger: logger,
		config: config,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Debug().Str("method", r.Method).Str("url", r.URL.String()).Msg("Received request")

	if strings.HasPrefix(r.URL.Path, URL_PREFIX) {
		h.serveInternal(w, r)
	} else {
		h.serveApp(w, r)
	}
}

func (h *Handler) serveInternal(w http.ResponseWriter, r *http.Request) {

}

func (h *Handler) serveApp(w http.ResponseWriter, r *http.Request) {
}
