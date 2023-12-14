// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"io/fs"
	"time"
)

const (
	ID_PREFIX_APP_PRD       = "app_prd_"
	ID_PREFIX_APP_DEV       = "app_dev_"
	ID_PREFIX_APP_STG       = "app_stg_"
	INTERNAL_URL_PREFIX     = "/_clace"
	APP_INTERNAL_URL_PREFIX = "/_clace_app"
	INTERNAL_APP_DELIM      = "_cl_"
	STAGE_SUFFIX            = INTERNAL_APP_DELIM + "stage"
)

// Config entries shared between client and server
type GlobalConfig struct {
	ConfigFile string `toml:"config_file"`
	AdminUser  string `toml:"admin_user"`
	ServerUri  string `toml:"server_uri"`
}

// ServerConfig is the configuration for the Clace Server
type ServerConfig struct {
	GlobalConfig
	Http     HttpConfig              `toml:"http"`
	Https    HttpsConfig             `toml:"https"`
	Security SecurityConfig          `toml:"security"`
	Metadata MetadataConfig          `toml:"metadata"`
	Log      LogConfig               `toml:"logging"`
	System   SystemConfig            `toml:"system"`
	GitAuth  map[string]GitAuthEntry `toml:"git_auth"`
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
	FileWatcherDebounceMillis int    `toml:"file_watcher_debounce_millis"`
	NodePath                  string `toml:"node_path"`
}

// GitAuth is a github auth config entry
type GitAuthEntry struct {
	UserID      string `toml:"user_id"`       // the user id of the user, defaults to "git" https://github.com/src-d/go-git/issues/637
	KeyFilePath string `toml:"key_file_path"` // the path to the private key file
	Password    string `toml:"password"`      // the password for the private key file
}

// ClientConfig is the configuration for the Clace Client
type ClientConfig struct {
	GlobalConfig
	SkipCertCheck bool   `toml:"skip_cert_check"`
	AdminPassword string `toml:"admin_password"`
}

// AppId is the identifier for an App
type AppId string

// AppPathDomain is a unique identifier for an app, consisting of the path and domain
type AppPathDomain struct {
	Path   string
	Domain string
}

func (a AppPathDomain) String() string {
	if a.Domain == "" {
		return a.Path
	} else {
		return a.Domain + ":" + a.Path
	}
}

// AppInfo is the basic info for an app
type AppInfo struct {
	AppPathDomain
	Id    AppId
	IsDev bool
}

func CreateAppPathDomain(path, domain string) AppPathDomain {
	return AppPathDomain{
		Path:   path,
		Domain: domain,
	}
}

func CreateAppInfo(id AppId, path, domain string, isDev bool) AppInfo {
	return AppInfo{
		AppPathDomain: AppPathDomain{
			Path:   path,
			Domain: domain,
		},
		Id:    id,
		IsDev: isDev,
	}
}

// Permission represents a permission granted to an app to run
// a plugin method with the given arguments
type Permission struct {
	Plugin    string
	Method    string
	Arguments []string
}

// AppAuthnType is the app level authentication type
type AppAuthnType string

const (
	AppAuthnDefault AppAuthnType = "default" // Use whatever auth is the default for the system
	AppAuthnNone    AppAuthnType = "none"    // No auth
)

// VersionMetadata contains the metadata for an app
type VersionMetadata struct {
	Version            int    `json:"version"`
	PreviousVersion    int    `json:"previous_version"`
	GitSha             string `json:"git_sha"`
	GitBranch          string `json:"git_branch"`
	GitCommit          string `json:"git_commit"`
	GitCommitRequested string `json:"git_commit_requested"`
	GitMessage         string `json:"git_message"`
	GitAuthName        string `json:"git_auth_name"`
}

// AppEntry is the application configuration in the DB
type AppEntry struct {
	Id         AppId       `json:"id"`
	Path       string      `json:"path"`
	MainApp    AppId       `json:"linked_app"` // the id of the app that this app is linked to
	Domain     string      `json:"domain"`
	SourceUrl  string      `json:"source_url"`
	IsDev      bool        `json:"is_dev"`
	UserID     string      `json:"user_id"`
	CreateTime *time.Time  `json:"create_time"`
	UpdateTime *time.Time  `json:"update_time"`
	Settings   AppSettings `json:"settings"` // settings are not version controlled
	Metadata   AppMetadata `json:"metadata"` // metadata is version controlled
}

func (ae *AppEntry) String() string {
	if ae.Domain == "" {
		return ae.Path
	} else {
		return ae.Domain + ":" + ae.Path
	}
}

func (ae *AppEntry) AppPathDomain() AppPathDomain {
	return AppPathDomain{
		Path:   ae.Path,
		Domain: ae.Domain,
	}
}

// AppMetadata contains the configuration for an app. App configurations are version controlled.
type AppMetadata struct {
	VersionMetadata VersionMetadata `json:"version_metadata"`
	Loads           []string        `json:"loads"`
	Permissions     []Permission    `json:"permissions"`
}

// AppSettings contains the settings for an app. Settings are not version controlled.
type AppSettings struct {
	AuthnType AppAuthnType `json:"authn_type"`
}

// WritableFS is the interface for the writable underlying file system used by AppFS
type ReadableFS interface {
	fs.FS
	fs.ReadFileFS
	fs.GlobFS
	// Stat returns the stats for the named file.
	Stat(name string) (fs.FileInfo, error)
}

type CompressedReader interface {
	ReadCompressed() (data []byte, compressionType string, err error)
}

// WritableFS is the interface for the writable underlying file system used by AppFS
type WritableFS interface {
	ReadableFS
	Write(name string, bytes []byte) error
	Remove(name string) error
}
