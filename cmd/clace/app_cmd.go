// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func initAppCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "app",
		Usage: "Manage apps on the server",
		Subcommands: []*cli.Command{
			appCreateCommand(commonFlags, clientConfig),
			appListCommand(commonFlags, clientConfig),
			appDeleteCommand(commonFlags, clientConfig),
			appAuditCommand(commonFlags, clientConfig),
		},
	}
}

func appCreateCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("domain", "", "The domain to add the app to", ""))
	flags = append(flags, newBoolFlag("is_dev", "", "Is the application in development mode", false))
	flags = append(flags, newBoolFlag("approve", "", "Approve the app permissions", false))
	//flags = append(flags, newBoolFlag("auto_sync", "", "Whether to automatically sync the application code", false))
	flags = append(flags, newBoolFlag("auto_reload", "", "Whether to automatically reload the UI on app updates", false))

	return &cli.Command{
		Name:      "create",
		Usage:     "Create a new app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<app_path> <app_source_url>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("require two arguments: <app_path> <app_source_url>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			if cCtx.IsSet("domain") {
				values.Add("domain", cCtx.String("domain"))
			}
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))

			body := utils.CreateAppRequest{
				SourceUrl:  cCtx.Args().Get(1),
				IsDev:      cCtx.Bool("is_dev"),
				AutoSync:   cCtx.Bool("auto_sync"),
				AutoReload: cCtx.Bool("auto_reload"),
			}
			var auditResult utils.AuditResult
			err := client.Post("/_clace/app"+cCtx.Args().Get(0), values, body, &auditResult)
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
	flags = append(flags, newStringFlag("domain", "", "The domain to list apps from", ""))

	return &cli.Command{
		Name:      "list",
		Usage:     "List an app or apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<app_path>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("require one argument: <app_path>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			if cCtx.IsSet("domain") {
				values.Add("domain", cCtx.String("domain"))
			}

			resp := make(map[string]any)
			err := client.Get("/_clace/app"+cCtx.Args().Get(0), values, &resp)
			if err != nil {
				return err
			}
			fmt.Fprintf(cCtx.App.ErrWriter, "%s", resp)
			return nil
		},
	}
}

func appDeleteCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("domain", "", "The domain to delete the app from", ""))

	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete an app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<app_path>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("require one argument: <app_path>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			if cCtx.IsSet("domain") {
				values.Add("domain", cCtx.String("domain"))
			}

			err := client.Delete("/_clace/app"+cCtx.Args().Get(0), values)
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
	flags = append(flags, newStringFlag("domain", "", "The domain for the app", ""))
	flags = append(flags, newBoolFlag("approve", "", "Approve the app permissions", false))

	return &cli.Command{
		Name:      "audit",
		Usage:     "Audit app permissions",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<app_path>",
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("require one argument: <app_path>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			if cCtx.IsSet("domain") {
				values.Add("domain", cCtx.String("domain"))
			}
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))

			var auditResult utils.AuditResult
			err := client.Post("/_clace/audit"+cCtx.Args().First(), values, nil, &auditResult)
			if err != nil {
				return err
			}
			fmt.Printf("App audit: %s\n", cCtx.Args().First())
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

			return nil
		},
	}
}
