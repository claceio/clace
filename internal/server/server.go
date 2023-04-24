// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/claceio/clace/internal/metadata"
	"github.com/claceio/clace/internal/utils"
)

// CL_ROOT is the root directory for Clace logs and temp files
var CL_ROOT = os.ExpandEnv("$CL_ROOT")

func init() {
	if len(CL_ROOT) == 0 {
		// Default to current directory if CL_ROOT is not set
		CL_ROOT = "."
		os.Setenv("CL_ROOT", CL_ROOT)
	}
}

type Server struct {
	*utils.Logger
	config     *utils.ServerConfig
	db         *metadata.Metadata
	httpServer *http.Server
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *utils.ServerConfig) (*Server, error) {
	logger := utils.NewLogger(&config.LogConfig)
	db, err := metadata.NewMetadata(logger, config)
	if err != nil {
		return nil, err
	}

	return &Server{
		Logger: logger,
		config: config,
		db:     db,
	}, nil
}

// Start starts the Clace Server
func (s *Server) Start() error {
	s.Info().Str("host", s.config.HttpHost).Int("port", s.config.HttpPort).Msg("Starting HTTP server")
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.HttpHost, s.config.HttpPort),
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
		IdleTimeout:  30 * time.Second,
		//Handler:      NewRouter(*staticDir),
	}
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil {
			s.Error().Err(err).Msg("Error starting server")
		}
	}()
	return nil
}

// Stop stops the Clace Server
func (s *Server) Stop() error {
	return nil
}
