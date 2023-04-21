// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package api

import clserver "github.com/claceio/clace/internal/server"

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	*clserver.ServerConfig
}

// Server is the instance of the Clace Server
type Server struct {
	config *ServerConfig
	server *clserver.Server
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *ServerConfig) *Server {
	return &Server{
		config: config,
		server: clserver.NewServer(config.ServerConfig),
	}
}

// Start starts the Clace Server
func (s *Server) Start() error {
	return s.server.Start()
}

// Stop stops the Clace Server
func (s *Server) Stop() error {
	return s.server.Stop()
}
