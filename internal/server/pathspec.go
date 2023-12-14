// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"

	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/claceio/clace/internal/utils"
)

// createPathDomain creates a slice of AppPathDomain from a slice of AppInfo
func createPathDomain(apps []utils.AppInfo) []utils.AppPathDomain {
	ret := make([]utils.AppPathDomain, 0, len(apps))
	for _, app := range apps {
		ret = append(ret, app.AppPathDomain)
	}

	return ret
}

// ParseSpecFromInfo parses a path spec in the format of domain:path.  If domain is not specified, it will match empty domain.
// glob patters are supported, *:** matches all apps.
func ParseSpecFromInfo(pathSpec string, apps []utils.AppInfo) ([]utils.AppInfo, error) {
	appPathDomain := createPathDomain(apps)
	pathDomains, error := ParseSpec(pathSpec, appPathDomain)
	if error != nil {
		return nil, error
	}
	found := map[string]bool{}
	for _, pathDomain := range pathDomains {
		found[pathDomain.String()] = true
	}

	ret := make([]utils.AppInfo, 0, len(found))
	for _, app := range apps {
		if found[app.AppPathDomain.String()] {
			ret = append(ret, app)
		}
	}
	return ret, nil
}

// ParseSpec parses a path spec in the format of domain:path. If domain is not specified, it will match empty domain.
// glob patters are supported, *:** matches all apps.
func ParseSpec(pathSpec string, apps []utils.AppPathDomain) ([]utils.AppPathDomain, error) {
	if pathSpec == "" {
		pathSpec = "*:**"
	}
	split := strings.Split(pathSpec, ":")
	if len(split) > 2 {
		return nil, fmt.Errorf("path spec has to be in the format of domain:path")
	}
	var app, domain string
	if len(split) == 2 {
		domain = split[0]
		app = split[1]
	} else {
		app = split[0]
	}

	if app == "" {
		return nil, fmt.Errorf("app path spec cannot be empty")
	}

	if app == "*" {
		app = "/*"
	}

	ret := make([]utils.AppPathDomain, 0)
	for _, entry := range apps {
		appMatch, err := doublestar.Match(app, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid path spec app value %s: %s", app, err)
		}
		if !appMatch {
			continue
		}
		if domain == "" && entry.Domain == "" {
			ret = append(ret, entry)
		} else {
			domainMatch, err := doublestar.Match("/"+domain, "/"+entry.Domain)
			if err != nil {
				return nil, fmt.Errorf("invalid path spec domain value %s: %s", domain, err)
			}
			if domainMatch {
				ret = append(ret, entry)
			}
		}
	}
	return ret, nil
}
