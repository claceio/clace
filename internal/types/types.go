// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"time"

	"github.com/claceio/clace/internal/app/starlark_type"
)

const (
	ID_PREFIX_APP_PROD      = "app_prd_"
	ID_PREFIX_APP_DEV       = "app_dev_"
	ID_PREFIX_APP_STAGE     = "app_stg_"
	ID_PREFIX_APP_PREVIEW   = "app_pre_"
	INTERNAL_URL_PREFIX     = "/_clace"
	APP_INTERNAL_URL_PREFIX = "/_clace_app"
	INTERNAL_APP_DELIM      = "_cl_"
	STAGE_SUFFIX            = INTERNAL_APP_DELIM + "stage"
	PREVIEW_SUFFIX          = INTERNAL_APP_DELIM + "preview"
)

const (
	TL_CONTEXT                  = "TL_context"
	TL_DEFER_MAP                = "TL_defer_map"
	TL_CURRENT_MODULE_FULL_PATH = "TL_current_module_full_path"
	TL_PLUGIN_API_FAILED_ERROR  = "TL_plugin_api_failed_error"
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
	Http        HttpConfig                `toml:"http"`
	Https       HttpsConfig               `toml:"https"`
	Security    SecurityConfig            `toml:"security"`
	Metadata    MetadataConfig            `toml:"metadata"`
	Log         LogConfig                 `toml:"logging"`
	System      SystemConfig              `toml:"system"`
	GitAuth     map[string]GitAuthEntry   `toml:"git_auth"`
	Plugins     map[string]PluginSettings `toml:"plugin"`
	Auth        map[string]AuthConfig     `toml:"auth"`
	ProfileMode string                    `toml:"profile_mode"`
}

type PluginSettings map[string]any

type PluginContext struct {
	Logger    *Logger
	AppId     AppId
	StoreInfo *starlark_type.StoreInfo
	Config    PluginSettings
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
	AppDefaultAuthType  string `toml:"app_default_auth_type"`
	SessionSecret       string `toml:"session_secret"`
	SessionMaxAge       int    `toml:"session_max_age"`
	SessionHttpsOnly    bool   `toml:"session_https_only"`
	CallbackUrl         string `toml:"callback_url"`
}

// MetadataConfig is the configuration for the Metadata persistence layer
type MetadataConfig struct {
	DBConnection string `toml:"db_connection"`
	AutoUpgrade  bool   `toml:"auto_upgrade"`
}

// LogConfig is the configuration for the Logger
type LogConfig struct {
	Level         string `toml:"level"`
	MaxBackups    int    `toml:"max_backups"`
	MaxSizeMB     int    `toml:"max_size_mb"`
	Console       bool   `toml:"console"`
	File          bool   `toml:"file"`
	AccessLogging bool   `toml:"access_logging"`
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

// AuthConfig is the configuration for the Authentication provider
type AuthConfig struct {
	Key          string   `toml:"key"`           // the client id
	Secret       string   `toml:"secret"`        // the client secret
	OrgUrl       string   `toml:"org_url"`       // the org url, used for Okta
	Domain       string   `toml:"domain"`        // the domain, used for Auth0
	DiscoveryUrl string   `toml:"discovery_url"` // the discovery url, used for OIDC
	HostedDomain string   `toml:"hosted_domain"` // the hosted domain, used for Google
	Scopes       []string `toml:"scopes"`        // oauth scopes
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
	Id      AppId
	IsDev   bool
	MainApp AppId
}

func CreateAppPathDomain(path, domain string) AppPathDomain {
	return AppPathDomain{
		Path:   path,
		Domain: domain,
	}
}

func CreateAppInfo(id AppId, path, domain string, isDev bool, mainApp AppId) AppInfo {
	return AppInfo{
		AppPathDomain: AppPathDomain{
			Path:   path,
			Domain: domain,
		},
		Id:      id,
		IsDev:   isDev,
		MainApp: mainApp,
	}
}

// Permission represents a permission granted to an app to run
// a plugin method with the given arguments
type Permission struct {
	Plugin    string   `json:"plugin"`
	Method    string   `json:"method"`
	Arguments []string `json:"arguments"`
	IsRead    *bool    `json:"is_read,omitempty"` // Whether the call is a Read operation or Write operation.
	// nil value means go with the default as set in the plugin code
}

// AppAuthnType is the app level authentication type
type AppAuthnType string

const (
	AppAuthnNone    AppAuthnType = "none"    // No auth
	AppAuthnDefault AppAuthnType = "default" // Use whatever auth is the default for the system
	AppAuthnSystem  AppAuthnType = "system"  // Use the system admin user
)

// VersionMetadata contains the metadata for an app
type VersionMetadata struct {
	Version         int    `json:"version"`
	PreviousVersion int    `json:"previous_version"`
	GitBranch       string `json:"git_branch"`
	GitCommit       string `json:"git_commit"`
	GitMessage      string `json:"git_message"`
}

// AppEntry is the application configuration in the DB
type AppEntry struct {
	Id         AppId       `json:"id"`
	Path       string      `json:"path"`
	MainApp    AppId       `json:"main_app"` // the id of the app that this app is linked to
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
	Accounts        []AccountLink   `json:"accounts"`
}

// AppSettings contains the settings for an app. Settings are not version controlled.
type AppSettings struct {
	AuthnType          AppAuthnType `json:"authn_type"`
	GitAuthName        string       `json:"git_auth_name"`
	StageWriteAccess   bool         `json:"stage_write_access"`
	PreviewWriteAccess bool         `json:"preview_write_access"`
}

// AccountLink links the account to use for each plugin
type AccountLink struct {
	Plugin      string `json:"plugin"`
	AccountName string `json:"account_name"`
}

type BoolValue int

const (
	BoolValueUndefined BoolValue = iota
	BoolValueTrue
	BoolValueFalse
)

type StringValue string

const (
	StringValueUndefined StringValue = "<CL_UNDEFINED>"
)

type AppVersion struct {
	Active          bool
	AppId           AppId
	Version         int
	PreviousVersion int
	UserId          string
	Metadata        *AppMetadata
	CreateTime      time.Time
}

type AppFile struct {
	Name string
	Etag string
	Size int64
}
