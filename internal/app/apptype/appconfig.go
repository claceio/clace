// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptype

type AppConfig struct {
	Routing RouteConfig `json:"routing"`
	Htmx    HtmxConfig  `json:"htmx"`
}

type RouteConfig struct {
	TemplateLocations []string `json:"template_locations"`
	BaseTemplates     string   `json:"base_templates"`
	PushEvents        bool     `json:"push_events"`
	EarlyHints        bool     `json:"early_hints"`
}

type HtmxConfig struct {
	Version string `json:"version"`
}

// NewAppConfig creates an AppConfig with default values. This config is used when lock
// file is not present. The config file load order is
//
//	DefaultAppConfig -> StarlarkAppConfig
func NewAppConfig() *AppConfig {
	return &AppConfig{
		Routing: RouteConfig{
			TemplateLocations: []string{"*.go.html"},
			BaseTemplates:     "base_templates",
			PushEvents:        false,
			EarlyHints:        true,
		},
		Htmx: HtmxConfig{
			Version: "1.9.2",
		},
	}
}

// NewCompatibleAppConfig creates an AppConfig focused on maintaining backward compatibility.
// This is used when the app is created from a source url where the source has the config lock file
// present. The configs are read in the order
//
// CompatibleAppConfig -> LockFile -> StarlarkAppConfig
//
// The goal is that if the application has a lock file, then all settings will attempt to be locked
// such that there should not be any change in behavior when the Clace version is updated.
// Removing the lock file will result in new config defaults getting applied, which can be
// done when the app developer wants to do an application refresh. Refresh will require additional
// testing to ensure that UI functionality is not changed..
func NewCompatibleAppConfig() *AppConfig {
	config := NewAppConfig()
	config.Htmx.Version = "1.9.1"
	return config
}
