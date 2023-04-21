// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
)

// CL_ROOT is the root directory for Clace logs and temp files
var CL_ROOT = os.ExpandEnv("$CL_ROOT")

func init() {
	if len(CL_ROOT) == 0 {
		// Default to current directory if CL_ROOT is not set
		CL_ROOT = fmt.Sprintf(".%s", string(os.PathSeparator))
		os.Setenv("CL_ROOT", CL_ROOT)
	}
}

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
	LogConfig
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	LogLevel       string `toml:"log_level"`
	MaxBackups     int    `toml:"max_backups"`
	MaxSizeMB      int    `toml:"max_size_mb"`
	ConsoleLogging bool   `toml:"console_logging"`
	FileLogging    bool   `toml:"file_logging"`
}

type Server struct {
	*Logger
	config *ServerConfig
}

// NewServer creates a new instance of the Clace Server
func NewServer(config *ServerConfig) *Server {
	logger := NewLogger(&config.LogConfig)
	return &Server{
		Logger: logger,
		config: config,
	}
}

// Start starts the Clace Server
func (s *Server) Start() error {
	log.Info().Msg("Starting server")
	return nil
}

// Stop stops the Clace Server
func (s *Server) Stop() error {
	return nil
}
