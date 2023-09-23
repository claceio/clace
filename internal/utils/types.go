// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"time"
)

const (
	INTERNAL_URL_PREFIX     = "/_clace"
	APP_INTERNAL_URL_PREFIX = "/_clace_app"
)

// Config entries shared between client and server
type GlobalConfig struct {
	ConfigFile    string `toml:"config_file"`
	AdminUser     string `toml:"admin_user"`
	AdminPassword string `toml:"admin_password"`
	ServerUri     string `toml:"server_uri"`
}

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	GlobalConfig
	Http     HttpConfig     `toml:"http"`
	Https    HttpsConfig    `toml:"https"`
	Security SecurityConfig `toml:"security"`
	Metadata MetadataConfig `toml:"metadata"`
	Log      LogConfig      `toml:"logging"`
	System   SystemConfig   `toml:"system"`
}

// HttpConfig is the configuration for the HTTP server
type HttpConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

// HttpsConfig is the configuration for the HTTPs server
type HttpsConfig struct {
	Host             string `toml:"host"`
	Port             int    `toml:"port"`
	EnableCertLookup bool   `toml:"enable_cert_lookup"`
	ServiceEmail     string `toml:"service_email"`
	UseStaging       bool   `toml:"use_staging"`
	StorageLocation  string `toml:"storage_location"`
	CertLocation     string `toml:"cert_location"`
}

// SecurityConfig is the configuration for Inter process communication
type SecurityConfig struct {
	AdminOverTCP        bool   `toml:"admin_over_tcp"`
	AdminPasswordBcrypt string `toml:"admin_password_bcrypt"`
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

// SystemConfig is the system level configuration
type SystemConfig struct {
	TailwindCSSCommand        string `toml:"tailwindcss_command"`
	DisableFileHashDevMode    bool   `toml:"disable_file_hash_dev_mode"`
	FileWatcherDebounceMillis int    `toml:"file_watcher_debounce_millis"`
}

// ClientConfig is the configuration for the Clace Client
type ClientConfig struct {
	GlobalConfig
	SkipCertCheck bool `toml:"skip_cert_check"`
}

// AppId is the identifier for an App
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

// Permission represents a permission granted to an app to run
// a plugin method with the given arguments
type Permission struct {
	Plugin    string
	Method    string
	Arguments []string
}

// AuditResult represents the result of an app audit
type AuditResult struct {
	Id                  AppId        `json:"id"`
	NewLoads            []string     `json:"new_loads"`
	NewPermissions      []Permission `json:"new_permissions"`
	ApprovedLoads       []string     `json:"approved_loads"`
	ApprovedPermissions []Permission `json:"approved_permissions"`
	NeedsApproval       bool         `json:"needs_approval"`
}

// AppEntry is the application configuration in the DB
type AppEntry struct {
	Id          AppId        `json:"id"`
	Path        string       `json:"path"`
	Domain      string       `json:"domain"`
	SourceUrl   string       `json:"source_url"`
	FsPath      string       `json:"fs_path"`
	IsDev       bool         `json:"is_dev"`
	AutoSync    bool         `json:"auto_sync"`
	AutoReload  bool         `json:"auto_reload"`
	UserID      string       `json:"user_id"`
	CreateTime  *time.Time   `json:"create_time"`
	UpdateTime  *time.Time   `json:"update_time"`
	Rules       string       `json:"rules"`
	Metadata    string       `json:"metadata"`
	Loads       []string     `json:"loads"`
	Permissions []Permission `json:"permissions"`
}
