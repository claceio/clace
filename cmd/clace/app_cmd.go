// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func initAppCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "app",
		Usage: "Manage Clace apps",
		Subcommands: []*cli.Command{
			appCreateCommand(commonFlags, clientConfig),
			appListCommand(commonFlags, clientConfig),
			appDeleteCommand(commonFlags, clientConfig),
			appAuditCommand(commonFlags, clientConfig),
			appReloadCommand(commonFlags, clientConfig),
			appPromoteCommand(commonFlags, clientConfig),
		},
	}
}

func appCreateCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newBoolFlag("dev", "d", "Is the application in development mode", false))
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))
	flags = append(flags, newStringFlag("auth-type", "", "The authentication type to use: can be default or none", "default"))
	flags = append(flags, newStringFlag("branch", "b", "The branch to checkout if using git source", "main"))
	flags = append(flags, newStringFlag("commit", "c", "The commit SHA to checkout if using git source. This takes precedence over branch", ""))
	flags = append(flags, newStringFlag("git-auth", "g", "The name of the git_auth entry to use", ""))

	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<app_path> <app_source_url>",
		UsageText: `args: <app_path> <app_source_url>

Create app from github source: clace app create --approve /disk_usage github.com/claceio/clace/examples/memory_usage/
Create app from local disk: clace app create --approve /disk_usage $HOME/clace_source/clace/examples/memory_usage/
Create app for development (source has to be disk): clace app create --approve --dev /disk_usage $HOME/clace_source/clace/examples/memory_usage/
Create app from a git commit: clace app create --approve --commit 1234567890 /disk_usage github.com/claceio/clace/examples/memory_usage/
Create app from a git branch: clace app create --approve --branch main /disk_usage github.com/claceio/clace/examples/memory_usage/
Create app using git url: clace app create --approve /disk_usage git@github.com:claceio/clace.git/examples/disk_usage
Create app using git url, with git private key auth: clace app create --approve --git-auth mykey /disk_usage git@github.com:claceio/privaterepo.git/examples/disk_usage
Create app for specified domain, no auth : clace app create --approve --auth-type=none clace.example.com:/ github.com/claceio/clace/examples/memory_usage/`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("require two arguments: <app_path> <app_source_url>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().Get(0))
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))

			body := utils.CreateAppRequest{
				SourceUrl:   cCtx.Args().Get(1),
				IsDev:       cCtx.Bool("dev"),
				AppAuthn:    utils.AppAuthnType(cCtx.String("auth-type")),
				GitBranch:   cCtx.String("branch"),
				GitCommit:   cCtx.String("commit"),
				GitAuthName: cCtx.String("git-auth"),
			}
			var auditResult utils.AuditResult
			err := client.Post("/_clace/app", values, body, &auditResult)
			if err != nil {
				return err
			}
			fmt.Printf("App audit results %s : %s\n", cCtx.Args().First(), auditResult.Id)
			fmt.Printf("  Plugins :\n")
			for _, load := range auditResult.NewLoads {
				fmt.Printf("    %s\n", load)
			}
			fmt.Printf("  Permissions:\n")
			for _, perm := range auditResult.NewPermissions {
				fmt.Printf("    %s.%s %s\n", perm.Plugin, perm.Method, perm.Arguments)
			}

			if auditResult.NeedsApproval {
				if cCtx.Bool("approve") {
					fmt.Print("App created. Permissions have been approved\n")
				} else {
					fmt.Print("App created. Permissions need to be approved\n")
				}
			} else {
				fmt.Print("App created. No approval required\n")
			}

			return nil
		},
	}
}

func appListCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newBoolFlag("internal", "i", "Include internal apps", false))
	flags = append(flags, newStringFlag("format", "f", "The display format. Valid options are table, csv, json, jsonl and jsonl_pretty", FORMAT_TABLE))

	return &cli.Command{
		Name:      "list",
		Usage:     "List apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec>",
		UsageText: `args: <pathSpec>

The <pathSpec> defaults to "*:**", which matches all apps across all domains, including no domain.
The domain and path are separated by a ":". pathSpec supports a glob pattern.
In the glob, * matches any number of characters, ** matches any number of characters including /.
To prevent shell expansion for *, placing the path in quotes is recommended.

List all apps, across domains: clace app list
List apps at the lop level with no domain specified: clace app list "*"
List all apps in the domain clace.example.com: clace app list "clace.example.com:**"
List all apps with no domain specified: clace app list "**"
List all apps with no domain, under the /utils folder: clace app list "/utils/**"
List all apps with no domain, including staging apps, under the /utils folder: clace app list --internal "/utils/**"
List apps at the lop level with no domain specified, with jsonl format: clace app list --format jsonl "*"`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() > 1 {
				return fmt.Errorf("only one argument expected: <pathSpec>")
			}
			values := url.Values{}
			values.Add("internal", fmt.Sprintf("%t", cCtx.Bool("internal")))
			if cCtx.NArg() == 1 {
				values.Add("pathSpec", cCtx.Args().Get(0))
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			var appListResponse utils.AppListResponse
			err := client.Get("/_clace/apps", values, &appListResponse)
			if err != nil {
				return err
			}
			printAppList(cCtx, appListResponse.Apps, cCtx.String("format"))
			return nil
		},
	}
}

func printAppList(cCtx *cli.Context, apps []utils.AppResponse, format string) {
	switch format {
	case FORMAT_JSON:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		enc.Encode(apps)
	case FORMAT_JSONL:
		enc := json.NewEncoder(cCtx.App.Writer)
		for _, app := range apps {
			enc.Encode(app)
		}
	case FORMAT_JSONL_PRETTY:
		enc := json.NewEncoder(cCtx.App.Writer)
		enc.SetIndent("", "  ")
		for _, app := range apps {
			enc.Encode(app)
			fmt.Fprintf(cCtx.App.Writer, "\n")
		}
	case FORMAT_TABLE:
		formatStrHead := "%-35s %-4s %-7s %-40s %-15s %-30s %-30s\n"
		formatStrData := "%-35s %-4s %-7d %-40s %-15s %-30s %-30s\n"
		fmt.Fprintf(cCtx.App.Writer, formatStrHead, "Id", "Type", "Version",
			"GitCommit", "GitBranch", "Domain:Path", "SourceUrl")
		for _, app := range apps {
			fmt.Fprintf(cCtx.App.Writer, formatStrData, app.Id, appType(app),
				app.Metadata.VersionMetadata.Version, app.Metadata.VersionMetadata.GitCommit,
				app.Metadata.VersionMetadata.GitBranch, &app.AppEntry, app.SourceUrl)
		}
	case FORMAT_CSV:
		for _, app := range apps {
			fmt.Fprintf(cCtx.App.Writer, "%s,%s,%d,%s,%s,%s,%s\n", app.Id, appType(app),
				app.Metadata.VersionMetadata.Version, app.Metadata.VersionMetadata.GitCommit, app.Metadata.VersionMetadata.GitBranch,
				app.AppEntry.AppPathDomain(), app.SourceUrl)
		}
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}
}

func appType(app utils.AppResponse) string {
	if app.IsDev {
		return "DEV"
	} else {
		if strings.HasPrefix(string(app.Id), utils.ID_PREFIX_APP_PRD) {
			return "PROD"
		} else {
			return "STG"
		}
	}
}

func appDeleteCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)

	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete an app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <pathSpec>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))

			err := client.Delete("/_clace/app", values)
			if err != nil {
				return err
			}
			fmt.Fprintf(cCtx.App.ErrWriter, "App deleted %s\n", cCtx.Args().Get(0))
			return nil
		},
	}
}

func appAuditCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))

	return &cli.Command{
		Name:      "audit",
		Usage:     "Audit app permissions",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <pathSpec>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))

			var auditResponse utils.AppAuditResponse
			err := client.Post("/_clace/audit", values, nil, &auditResponse)
			if err != nil {
				return err
			}
			for _, auditResult := range auditResponse.AuditResults {
				fmt.Printf("App audit: %s\n", auditResult.AppPathDomain)
				fmt.Printf("  Plugins :\n")
				for _, load := range auditResult.NewLoads {
					fmt.Printf("    %s\n", load)
				}
				fmt.Printf("  Permissions:\n")
				for _, perm := range auditResult.NewPermissions {
					fmt.Printf("    %s.%s %s\n", perm.Plugin, perm.Method, perm.Arguments)
				}

				if auditResult.NeedsApproval {
					if cCtx.Bool("approve") {
						fmt.Printf("App permissions have been approved.\n")
					} else {
						fmt.Printf("App permissions need to be approved...\n")
					}
				} else {
					fmt.Printf("App permissions are current, no approval required.\n")
				}
			}

			return nil
		},
	}
}

func appReloadCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))
	flags = append(flags, newBoolFlag("promote", "p", "Promote the change from stage to prod", false))
	flags = append(flags, newStringFlag("branch", "b", "The branch to checkout if using git source", ""))
	flags = append(flags, newStringFlag("commit", "c", "The commit SHA to checkout if using git source. This takes precedence over branch", ""))
	flags = append(flags, newStringFlag("git-auth", "g", "The name of the git_auth entry to use", ""))
	flags = append(flags, newBoolFlag("dry-run", "n", "Whether to run in dry run (check only) mode", false))

	return &cli.Command{
		Name:      "reload",
		Usage:     "Reload the app source code",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <pathSpec>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().First())
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))
			values.Add("promote", strconv.FormatBool(cCtx.Bool("promote")))
			values.Add("branch", cCtx.String("branch"))
			values.Add("commit", cCtx.String("commit"))
			values.Add("gitAuth", cCtx.String("git-auth"))

			var response map[string]any
			err := client.Post("/_clace/reload", values, nil, &response)
			if err != nil {
				return err
			}
			fmt.Fprintf(cCtx.App.ErrWriter, "App reloaded %s\n", response)
			return nil
		},
	}
}

func appPromoteCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)

	return &cli.Command{
		Name:      "promote",
		Usage:     "Promote the app from staging to production",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <pathSpec>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().First())

			var response map[string]any
			err := client.Post("/_clace/promote", values, nil, &response)
			if err != nil {
				return err
			}
			fmt.Fprintf(cCtx.App.ErrWriter, "App(s) promoted %s\n", response)
			return nil
		},
	}
}
