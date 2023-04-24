// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package utils

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	HttpConfig     `toml:"http"`
	MetadataConfig `toml:"metadata"`
	LogConfig      `toml:"logging"`
}

// HttpConfig is the configuration for the HTTP server
type HttpConfig struct {
	HttpHost string `toml:"host"`
	HttpPort int    `toml:"port"`
}

// MetadataConfig is the configuration for the Metadata persistence layer
type MetadataConfig struct {
	DBConnection string `toml:"db_connection"`
	AutoUpgrade  bool   `toml:"auto_upgrade"`
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	LogLevel       string `toml:"log_level"`
	MaxBackups     int    `toml:"max_backups"`
	MaxSizeMB      int    `toml:"max_size_mb"`
	ConsoleLogging bool   `toml:"console_logging"`
	FileLogging    bool   `toml:"file_logging"`
}

// App is the application configuration
type App struct {
	*Logger
	Path       string
	Domain     string
	CodeUrl    string
	UserID     string
	CreateTime string
	UpdateTime string
	Rules      string
	Metadata   string
}
