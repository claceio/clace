// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"errors"
	"time"
)

var ErrUserNotFound = errors.New("user not found")

// BuiltinUser is one row of the builtin_users table: an account for the
// builtin app auth type. The principal RBAC grants match is
// builtin:<Username>; Groups are matched by group: references like SSO
// groups. CredentialVersion increments on every password set, disable,
// enable and unlock: form-login sessions record it at login and are rejected
// once it changes, so those operations revoke existing sessions immediately
// (a rehash of the same password on login does not change it). Email,
// EmailVerifiedAt, DisplayName and MustChangePassword are stored for the
// account features built on this table (self-service password change, reset
// links) and are not yet set by any API
type BuiltinUser struct {
	Id                 string     `json:"id"`
	Username           string     `json:"username"`
	PasswordHash       string     `json:"-"`
	CredentialVersion  int        `json:"credential_version"`
	Groups             []string   `json:"groups"`
	DisplayName        string     `json:"display_name,omitempty"`
	Email              string     `json:"email,omitempty"`
	EmailVerifiedAt    *time.Time `json:"email_verified_at,omitempty"`
	DisabledAt         *time.Time `json:"disabled_at,omitempty"`
	MustChangePassword bool       `json:"must_change_password"`
	FailedAttempts     int        `json:"failed_attempts"`
	LockedUntil        *time.Time `json:"locked_until,omitempty"`
	PasswordUpdatedAt  time.Time  `json:"password_updated_at"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	CreatedBy          string     `json:"created_by"`
	CreateTime         time.Time  `json:"create_time"`
	UpdateTime         time.Time  `json:"update_time"`
	// RowVersion is the optimistic concurrency token of the row, advanced
	// by every account update (not by login bookkeeping)
	RowVersion int `json:"row_version"`
}

// Disabled reports whether logins and API authority are refused for the user
func (u *BuiltinUser) Disabled() bool {
	return u.DisabledAt != nil
}

// Locked reports whether password logins are refused at the given time
// because of repeated failures
func (u *BuiltinUser) Locked(now time.Time) bool {
	return u.LockedUntil != nil && now.Before(*u.LockedUntil)
}

// GroupList returns the groups, never nil
func (u *BuiltinUser) GroupList() []string {
	if u.Groups == nil {
		return []string{}
	}
	return u.Groups
}
