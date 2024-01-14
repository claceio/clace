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

func initAccountCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "account",
		Usage: "Manage Clace accounts",
		Subcommands: []*cli.Command{
			accountLinkCommand(commonFlags, clientConfig),
			accountListCommand(commonFlags, clientConfig),
		},
	}
}

func accountLinkCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "link",
		Usage:     "Link an app to to use specific account for a plugin",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath> <pluginName> <accountName>",
		UsageText: `args: <appPath> <pluginName> <accountName>

    <app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which the account link is to be created.
    <pluginName> is the required second argument. This is the name of the plugin.
	<accountName> is the required third argument. This is the name of the account to link to the plugin.

	Examples:
	  Link db plugin: clace account link /myapp db.in temp
	  Link in dryrun mode: clace account link --dry-run example.com:/ rest.in testaccount`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 3 {
				return fmt.Errorf("requires three arguments: <appPath> <pluginName> <accountName>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())
			values.Add("plugin", cCtx.Args().Get(1))
			values.Add("account", cCtx.Args().Get(2))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			var linkResponse utils.AppLinkAccountResponse
			err := client.Post("/_clace/link_account", values, nil, &linkResponse)
			if err != nil {
				return err
			}

			for _, linkedApp := range linkResponse.LinkResults {
				fmt.Printf("Linked app %s\n", linkedApp)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) linked, 0 app(s) promoted.\n", len(linkResponse.LinkResults))

			if linkResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func accountListCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "list",
		Usage:     "List the accounts linked to an app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath>",
		UsageText: `args: <appPath>

    <app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which the account link is to be created.

	Examples:
	  List plugins for app: clace account list /myapp
	  List plugins for app: clace account list example.com:/`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 1 {
				return fmt.Errorf("requires one argument: <appPath>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())

			var response utils.AppGetResponse
			err := client.Get("/_clace/app", values, &response)
			if err != nil {
				return err
			}

			appInfo := response.AppEntry
			if len(appInfo.Metadata.Accounts) == 0 {
				fmt.Fprintf(cCtx.App.Writer, "No account links for app %s : %s\n", appInfo.AppPathDomain(), appInfo.Id)
				return nil
			}
			fmt.Fprintf(cCtx.App.Writer, "Account links for app %s : %s\n", appInfo.AppPathDomain(), appInfo.Id)
			for _, plugin := range appInfo.Metadata.Accounts {
				fmt.Fprintf(cCtx.App.Writer, "  %s: %s\n", plugin.Plugin, plugin.AccountName)
			}

			return nil
		},
	}
}
