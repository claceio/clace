// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openrundev/openrun/internal/passwd"
	"github.com/openrundev/openrun/internal/types"
	"github.com/urfave/cli/v2"
)

const (
	USER_VALUE_FLAG   = "value"
	USER_RANDOM_FLAG  = "random"
	USER_PROMPT_FLAG  = "prompt"
	USER_GROUPS_FLAG  = "groups"
	USER_DISABLE_FLAG = "disable"
	USER_ENABLE_FLAG  = "enable"
	USER_UNLOCK_FLAG  = "unlock"
)

func initUserCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "user",
		Usage: "Manage builtin users, for apps using the \"builtin\" auth type",
		Subcommands: []*cli.Command{
			userUpdateCommand(commonFlags, clientConfig, false),
			userUpdateCommand(commonFlags, clientConfig, true),
			userDeleteCommand(commonFlags, clientConfig),
			userListCommand(commonFlags, clientConfig),
			userShowCommand(commonFlags, clientConfig),
		},
	}
}

func userPasswordFlags(commonFlags []cli.Flag, update bool) []cli.Flag {
	passwordUsage := "The password value. A random password is generated when no password flag is set"
	if update {
		passwordUsage = "The new password value. The current password is kept when no password flag is set"
	}
	flags := make([]cli.Flag, 0, len(commonFlags)+7)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag(USER_VALUE_FLAG, "v", passwordUsage, ""))
	flags = append(flags, newBoolFlag(USER_RANDOM_FLAG, "r", "Generate a random password (printed to stderr)", false))
	flags = append(flags, newBoolFlag(USER_PROMPT_FLAG, "p", "Prompt for the password", false))
	flags = append(flags, newStringFlag(USER_GROUPS_FLAG, "g",
		"Comma separated groups for the user, used for RBAC group: matching", ""))
	if update {
		flags = append(flags, newBoolFlag(USER_DISABLE_FLAG, "", "Disable the user: logins are refused and the user's API keys and sessions stop working", false))
		flags = append(flags, newBoolFlag(USER_ENABLE_FLAG, "", "Enable a disabled user", false))
		flags = append(flags, newBoolFlag(USER_UNLOCK_FLAG, "", "Clear the login lockout caused by repeated failed logins", false))
	}
	return flags
}

// userPassword resolves the password flags to the plaintext password. For an
// update, no password flag set means keep the current password (returns "")
func userPassword(cCtx *cli.Context, update bool) (string, error) {
	setFlags := 0
	for _, flag := range []string{USER_VALUE_FLAG, USER_RANDOM_FLAG, USER_PROMPT_FLAG} {
		if cCtx.IsSet(flag) {
			setFlags++
		}
	}
	if setFlags > 1 {
		return "", fmt.Errorf("only one of --%s, --%s and --%s can be set", USER_VALUE_FLAG, USER_RANDOM_FLAG, USER_PROMPT_FLAG)
	}

	switch {
	case cCtx.IsSet(USER_VALUE_FLAG):
		return cCtx.String(USER_VALUE_FLAG), nil
	case cCtx.Bool(USER_PROMPT_FLAG):
		return promptPassword("Enter password: ")
	case cCtx.Bool(USER_RANDOM_FLAG) || !update:
		// add generates a password by default; update keeps the current one
		password, err := passwd.GeneratePassword()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(os.Stderr, "Generated password is: %s\n\n", password)
		return password, nil
	}
	return "", nil
}

func userUpdateCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig, update bool) *cli.Command {
	name, usage := "add", "Add a builtin user (takes effect immediately)"
	usageText := `args: <username>

Examples:
  Add a user with a generated password:  openrun user add alice --groups dev,qa
  Add a user with a password value:      openrun user add alice --value mypassword
  Add a user, prompting for password:    openrun user add alice --prompt`
	if update {
		name, usage = "update", "Update a builtin user's password, groups or state"
		usageText = `args: <username>

Examples:
  Change the groups, keep the password:  openrun user update alice --groups dev,ops
  Change the password:                   openrun user update alice --value newpassword
  Generate a new random password:        openrun user update alice --random
  Disable / enable the user:             openrun user update alice --disable
  Clear a login lockout:                 openrun user update alice --unlock`
	}

	return &cli.Command{
		Name:      name,
		Usage:     usage,
		Flags:     userPasswordFlags(commonFlags, update),
		ArgsUsage: "<username>",
		UsageText: usageText,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("expected one argument: <username>")
			}
			username := cCtx.Args().Get(0)

			password, err := userPassword(cCtx, update)
			if err != nil {
				return err
			}

			// The password is sent as entered; the server hashes it (argon2id)
			// and enforces the password policy
			updateRequest := types.UserUpdateRequest{Password: password}
			if cCtx.IsSet(USER_GROUPS_FLAG) {
				updateRequest.Groups = parseGroups(cCtx.String(USER_GROUPS_FLAG))
			}
			if update {
				if cCtx.Bool(USER_DISABLE_FLAG) && cCtx.Bool(USER_ENABLE_FLAG) {
					return fmt.Errorf("only one of --%s and --%s can be set", USER_DISABLE_FLAG, USER_ENABLE_FLAG)
				}
				if cCtx.Bool(USER_DISABLE_FLAG) {
					disabled := true
					updateRequest.Disabled = &disabled
				} else if cCtx.Bool(USER_ENABLE_FLAG) {
					disabled := false
					updateRequest.Disabled = &disabled
				}
				updateRequest.Unlock = cCtx.Bool(USER_UNLOCK_FLAG)
			}

			values := url.Values{}
			values.Add("username", username)
			values.Add("update", strconv.FormatBool(update))

			client := newHttpClient(clientConfig)
			defer client.CloseIdleConnections()
			var response types.UserUpdateResponse
			if err := client.Post("/_openrun/user", values, &updateRequest, &response); err != nil {
				return err
			}

			operation := "added"
			if response.Updated {
				operation = "updated"
			}
			printStdout(cCtx, "User %s: %s\n", operation, response.Username)
			return nil
		},
	}
}

// parseGroups splits a comma separated groups value, dropping empty entries
// so --groups "" clears the groups list
func parseGroups(groupsArg string) []string {
	groups := []string{}
	for group := range strings.SplitSeq(groupsArg, ",") {
		if group = strings.TrimSpace(group); group != "" {
			groups = append(groups, group)
		}
	}
	return groups
}

func userDeleteCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete a builtin user (the user's API keys and sessions are revoked)",
		Flags:     commonFlags,
		ArgsUsage: "<username>",
		UsageText: `Examples:
  Delete a user: openrun user delete alice`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("expected one argument: <username>")
			}

			values := url.Values{}
			values.Add("username", cCtx.Args().Get(0))

			client := newHttpClient(clientConfig)
			defer client.CloseIdleConnections()
			var response types.UserDeleteResponse
			if err := client.Delete("/_openrun/user", values, &response); err != nil {
				return err
			}

			printStdout(cCtx, "User deleted: %s\n", response.Username)
			return nil
		},
	}
}

func userListCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+1)
	flags = append(flags, commonFlags...)
	flags = append(flags, newFormatFlag())

	return &cli.Command{
		Name:  "list",
		Usage: "List builtin users (passwords are not shown)",
		Flags: flags,
		UsageText: `Examples:
  List users: openrun user list`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 0 {
				return fmt.Errorf("expected no arguments")
			}

			client := newHttpClient(clientConfig)
			defer client.CloseIdleConnections()
			var response types.UserListResponse
			if err := client.Get("/_openrun/users", nil, &response); err != nil {
				return err
			}

			return printUserList(cCtx, response.Users, cmp.Or(cCtx.String("format"), clientConfig.Client.DefaultFormat))
		},
	}
}

func userShowCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+1)
	flags = append(flags, commonFlags...)
	flags = append(flags, newFormatFlag())

	return &cli.Command{
		Name:      "show",
		Usage:     "Show one builtin user (the password is not shown)",
		Flags:     flags,
		ArgsUsage: "<username>",
		UsageText: `Examples:
  Show a user: openrun user show alice`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("expected one argument: <username>")
			}

			values := url.Values{}
			values.Add("username", cCtx.Args().Get(0))

			client := newHttpClient(clientConfig)
			defer client.CloseIdleConnections()
			var user types.BuiltinUserInfo
			if err := client.Get("/_openrun/user", values, &user); err != nil {
				return err
			}

			return printUserList(cCtx, []types.BuiltinUserInfo{user}, cmp.Or(cCtx.String("format"), clientConfig.Client.DefaultFormat))
		},
	}
}

// userStatus is the one-word state of a user for the table formats
func userStatus(u types.BuiltinUserInfo) string {
	switch {
	case u.Disabled:
		return "disabled"
	case u.LockedUntil != nil:
		return "locked"
	}
	return "active"
}

func userTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatJobTime(*t)
}

func printUserList(cCtx *cli.Context, users []types.BuiltinUserInfo, format string) error {
	if isTemplateFormat(format) {
		return printTemplate(cCtx, format, users)
	}
	switch format {
	case FORMAT_JSON:
		enc := newJSONEncoder(cCtx.App.Writer, true)
		json.MarshalEncode(enc, users, deterministicJSON) //nolint:errcheck
	case FORMAT_JSONL:
		enc := newJSONEncoder(cCtx.App.Writer, false)
		for _, u := range users {
			json.MarshalEncode(enc, u, deterministicJSON) //nolint:errcheck
		}
	case FORMAT_JSONL_PRETTY:
		enc := newJSONEncoder(cCtx.App.Writer, true)
		for _, u := range users {
			json.MarshalEncode(enc, u, deterministicJSON) //nolint:errcheck
		}
	case FORMAT_BASIC:
		formatStr := "%-30s %-40s\n"
		printStdout(cCtx, formatStr, "Username", "Groups")
		for _, u := range users {
			printStdout(cCtx, formatStr, u.Username, strings.Join(u.Groups, ","))
		}
	case FORMAT_TABLE, "":
		formatStr := "%-30s %-10s %-22s %-22s %-40s\n"
		printStdout(cCtx, formatStr, "Username", "Status", "Created", "LastLogin", "Groups")
		for _, u := range users {
			printStdout(cCtx, formatStr, u.Username, userStatus(u), formatJobTime(u.CreateTime),
				userTime(u.LastLoginAt), strings.Join(u.Groups, ","))
		}
	case FORMAT_CSV:
		for _, u := range users {
			printStdout(cCtx, "%s,%s,%s,%s,%s\n", u.Username, userStatus(u), formatJobTime(u.CreateTime),
				userTime(u.LastLoginAt), strings.Join(u.Groups, " "))
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
	return nil
}
