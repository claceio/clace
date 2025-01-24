// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package types

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
	SourceUrl        string            `json:"source_url"`
	IsDev            bool              `json:"is_dev"`
	AppAuthn         AppAuthnType      `json:"app_authn"`
	GitBranch        string            `json:"git_branch"`
	GitCommit        string            `json:"git_commit"`
	GitAuthName      string            `json:"git_auth_name"`
	Spec             AppSpec           `json:"spec"`
	ParamValues      map[string]string `json:"param_values"`
	ContainerOptions map[string]string `json:"container_options"`
	ContainerArgs    map[string]string `json:"container_args"`
	ContainerVolumes []string          `json:"container_volumes"`
	AppConfig        map[string]string `json:"appconfig"`
}

// UpdateAppRequest is the request body for updating an app settings
type UpdateAppRequest struct {
	AuthnType          StringValue `json:"authn_type"`
	GitAuthName        StringValue `json:"git_auth_name"`
	StageWriteAccess   BoolValue   `json:"stage_write_access"`
	PreviewWriteAccess BoolValue   `json:"preview_write_access"`
	Spec               StringValue `json:"spec"`
}

func CreateUpdateAppRequest() UpdateAppRequest {
	return UpdateAppRequest{
		AuthnType:          StringValueUndefined,
		GitAuthName:        StringValueUndefined,
		StageWriteAccess:   BoolValueUndefined,
		PreviewWriteAccess: BoolValueUndefined,
		Spec:               StringValueUndefined,
	}
}

// UpdateAppMetadataRequest is the request body for updating an app metadata
type UpdateAppMetadataRequest struct {
	Spec          StringValue           `json:"spec"`
	ConfigType    AppMetadataConfigType `json:"config_type"`
	ConfigEntries []string              `json:"config_entries"`
}

func CreateUpdateAppMetadataRequest() UpdateAppMetadataRequest {
	return UpdateAppMetadataRequest{
		Spec:          StringValueUndefined,
		ConfigType:    AppMetadataConfigType(StringValueUndefined),
		ConfigEntries: []string{},
	}
}

// ApproveResult represents the result of an app approval audit
type ApproveResult struct {
	Id                  AppId         `json:"id"`
	AppPathDomain       AppPathDomain `json:"app_path_domain"`
	NewLoads            []string      `json:"new_loads"`
	NewPermissions      []Permission  `json:"new_permissions"`
	ApprovedLoads       []string      `json:"approved_loads"`
	ApprovedPermissions []Permission  `json:"approved_permissions"`
	NeedsApproval       bool          `json:"needs_approval"`
}

type AppResponse struct {
	AppEntry
	StagedChanges bool `json:"staged_changes"`
}

type AppListResponse struct {
	Apps []AppResponse `json:"apps"`
}

type AppCreateResponse struct {
	DryRun         bool            `json:"dry_run"`
	HttpUrl        string          `json:"http_url"`
	HttpsUrl       string          `json:"https_url"`
	ApproveResults []ApproveResult `json:"approve_results"`
}

type AppDeleteResponse struct {
	DryRun  bool      `json:"dry_run"`
	AppInfo []AppInfo `json:"app_info"`
}

type AppStagedUpdateResponse struct {
	DryRun              bool            `json:"dry_run"`
	StagedUpdateResults any             `json:"staged_update_results"`
	PromoteResults      []AppPathDomain `json:"promote_results"`
}

type AppApproveResponse struct {
	DryRun              bool            `json:"dry_run"`
	StagedUpdateResults []ApproveResult `json:"staged_update_results"`
	PromoteResults      []AppPathDomain `json:"promote_results"`
}

type AppReloadResponse struct {
	DryRun         bool            `json:"dry_run"`
	ReloadResults  []AppPathDomain `json:"reload_results"`
	ApproveResults []ApproveResult `json:"approve_results"`
	PromoteResults []AppPathDomain `json:"promote_results"`
}

type AppApplyResponse struct {
	DryRun         bool                `json:"dry_run"`
	CreateResults  []AppCreateResponse `json:"create_results"`
	UpdateResults  []AppPathDomain     `json:"update_results"`
	ApproveResults []ApproveResult     `json:"approve_results"`
	PromoteResults []AppPathDomain     `json:"promote_results"`
}

type AppPromoteResponse struct {
	DryRun         bool            `json:"dry_run"`
	PromoteResults []AppPathDomain `json:"promote_results"`
}

type AppUpdateSettingsResponse struct {
	DryRun        bool            `json:"dry_run"`
	UpdateResults []AppPathDomain `json:"update_results"`
}

type AppPreviewResponse struct {
	DryRun        bool          `json:"dry_run"`
	HttpUrl       string        `json:"http_url"`
	HttpsUrl      string        `json:"https_url"`
	Success       bool          `json:"success"`
	ApproveResult ApproveResult `json:"approve_result"`
}

type AppLinkAccountResponse struct {
	DryRun              bool            `json:"dry_run"`
	StagedUpdateResults []AppPathDomain `json:"staged_update_results"`
	PromoteResults      []AppPathDomain `json:"promote_results"`
}

type AppUpdateMetadataResponse struct {
	DryRun              bool            `json:"dry_run"`
	StagedUpdateResults []AppPathDomain `json:"staged_update_results"`
	PromoteResults      []AppPathDomain `json:"promote_results"`
}

type AppGetResponse struct {
	AppEntry AppEntry `json:"app_entry"`
}

type AppVersionListResponse struct {
	Versions []AppVersion `json:"versions"`
}

type AppVersionFilesResponse struct {
	Files []AppFile `json:"files"`
}

type AppVersionSwitchResponse struct {
	DryRun      bool `json:"dry_run"`
	FromVersion int  `json:"from_version"`
	ToVersion   int  `json:"to_version"`
}

type AppToken struct {
	Type  WebhookType `json:"type"`
	Url   string      `json:"url"`
	Token string      `json:"token"`
}

type TokenListResponse struct {
	Tokens []AppToken `json:"tokens"`
}

type TokenCreateResponse struct {
	DryRun bool     `json:"dry_run"`
	Token  AppToken `json:"token"`
}

type TokenDeleteResponse struct {
	DryRun bool `json:"dry_run"`
}
