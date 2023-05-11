// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package utils

import "time"

// Config entries shared between client and server
type GlobalConfig struct {
	ConfigFile          string `toml:"config_file"`
	AdminUser           string `toml:"admin_user"`
	AdminPassword       string `toml:"admin_password"`
	AdminPasswordBcrypt string `toml:"admin_password_bcrypt"`
}

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	GlobalConfig
	Http     HttpConfig     `toml:"http"`
	Metadata MetadataConfig `toml:"metadata"`
	Log      LogConfig      `toml:"logging"`
}

// HttpConfig is the configuration for the HTTP server
type HttpConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

// MetadataConfig is the configuration for the Metadata persistence layer
type MetadataConfig struct {
	DBConnection string `toml:"db_connection"`
	AutoUpgrade  bool   `toml:"auto_upgrade"`
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	Level      string `toml:"level"`
	MaxBackups int    `toml:"max_backups"`
	MaxSizeMB  int    `toml:"max_size_mb"`
	Console    bool   `toml:"console"`
	File       bool   `toml:"file"`
}

// ClientConfig is the configuration for the Clace Client
type ClientConfig struct {
	GlobalConfig
	ServerUrl string `toml:"server_url"`
}

// AppId is the identifier uuid for an App
type AppId string

// AppPathDomain is a unique identifier for an app, consisting of the path and domain
type AppPathDomain struct {
	Path   string
	Domain string
}

func CreateAppPathDomain(path, domain string) AppPathDomain {
	return AppPathDomain{
		Path:   path,
		Domain: domain,
	}
}

// App is the application configuration in the DB
type AppEntry struct {
	Id         AppId      `json:"id"`
	Path       string     `json:"path"`
	Domain     string     `json:"domain"`
	SourceUrl  string     `json:"source_url"`
	FsPath     string     `json:"fs_path"`
	FsRefresh  bool       `json:"fs_refresh"`
	UserID     string     `json:"user_id"`
	CreateTime *time.Time `json:"create_time"`
	UpdateTime *time.Time `json:"update_time"`
	Rules      string     `json:"rules"`
	Metadata   string     `json:"metadata"`
}
