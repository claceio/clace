// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
)

// RequestError is the error returned by the API
type RequestError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func CreateRequestError(message string, code int) RequestError {
	return RequestError{
		Message: message,
		Code:    code,
	}
}

func (r RequestError) Error() string {
	if r.Message == "" {
		return fmt.Sprintf("status code %d", r.Code)
	} else {
		return r.Message
	}
}

// CreateAppRequest is the request body for creating an app
type CreateAppRequest struct {
	SourceUrl   string       `json:"source_url"`
	IsDev       bool         `json:"is_dev"`
	AppAuthn    AppAuthnType `json:"app_authn"`
	GitBranch   string       `json:"git_branch"`
	GitCommit   string       `json:"git_commit"`
	GitAuthName string       `json:"git_auth_name"`
}

// AuditResult represents the result of an app audit
type AuditResult struct {
	Id                  AppId         `json:"id"`
	AppPathDomain       AppPathDomain `json:"app_path_domain"`
	NewLoads            []string      `json:"new_loads"`
	NewPermissions      []Permission  `json:"new_permissions"`
	ApprovedLoads       []string      `json:"approved_loads"`
	ApprovedPermissions []Permission  `json:"approved_permissions"`
	NeedsApproval       bool          `json:"needs_approval"`
}

type AppAuditResponse struct {
	AuditResults []AuditResult `json:"audit_results"`
}

type AppResponse struct {
	AppEntry
	// TODO add git info from version info
}

type AppListResponse struct {
	Apps []AppResponse `json:"apps"`
}

type AppReloadResponse struct {
	AuditResults   []AuditResult   `json:"audit_results"`
	PromoteResults []AppPathDomain `json:"promote_results"`
}

type AppPromoteResponse struct {
	PromoteResults []AppPathDomain `json:"promote_results"`
}
