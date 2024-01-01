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

func appUpdateSettingsCommand(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update Clace apps settings",
		Subcommands: []*cli.Command{
			appUpdateStageWrite(commonFlags, clientConfig),
			appUpdatePreviewWrite(commonFlags, clientConfig),
			appUpdateAuthnType(commonFlags, clientConfig),
			appUpdateGitAuth(commonFlags, clientConfig),
		},
	}
}

func appUpdateStageWrite(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "stage-write-access",
		Usage:     "Update write access permission for staging app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec> <value:true|false>",

		UsageText: `args: <pathSpec> <value:true|false>

	First required argument is <pathSpec>. The domain and path are separated by a ":". pathSpec supports a glob pattern.
	In the glob, * matches any number of characters, ** matches any number of characters including /.
	all is a shortcut for "*:**", which matches all apps across all domains, including no domain.
	To prevent shell expansion for *, placing the path in quotes is recommended.

	The second required argument <value> is a boolean value, true or false.

	Examples:
	  Update all apps, across domains: clace app update stage-write-access all true
	  Update apps in the example.com domain: clace app stage-write-access "example.com:**" false`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <pathSpec> <value>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := utils.CreateUpdateAppRequest()
			boolValue, err := strconv.ParseBool(cCtx.Args().Get(1))
			if err != nil {
				return fmt.Errorf("invalid value %s for stage-write-access, expected true or false", cCtx.Args().Get(1))
			}
			if boolValue {
				body.StageWriteAccess = utils.BoolValueTrue
			} else {
				body.StageWriteAccess = utils.BoolValueFalse
			}

			var updateResponse utils.AppUpdateSettingsResponse
			err = client.Post("/_clace/app_settings", values, body, &updateResponse)
			if err != nil {
				return err
			}

			for _, updateResult := range updateResponse.UpdateResults {
				fmt.Printf("Updating %s\n", updateResult)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated.\n", len(updateResponse.UpdateResults))

			if updateResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func appUpdatePreviewWrite(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "preview-write-access",
		Usage:     "Update write access permission for preview apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec> <value:true|false>",

		UsageText: `args: <pathSpec> <value:true|false>

	First required argument is <pathSpec>. The domain and path are separated by a ":". pathSpec supports a glob pattern.
	In the glob, * matches any number of characters, ** matches any number of characters including /.
	all is a shortcut for "*:**", which matches all apps across all domains, including no domain.
	To prevent shell expansion for *, placing the path in quotes is recommended.

	The second required argument <value> is a boolean value, true or false.

	Examples:
	  Update all apps, across domains: clace app update preview-write-access all true
	  Update apps in the example.com domain: clace app preview-write-access "example.com:**" false`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <pathSpec> <value>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := utils.CreateUpdateAppRequest()
			boolValue, err := strconv.ParseBool(cCtx.Args().Get(1))
			if err != nil {
				return fmt.Errorf("invalid value %s for preview-write-access, expected true or false", cCtx.Args().Get(1))
			}
			if boolValue {
				body.PreviewWriteAccess = utils.BoolValueTrue
			} else {
				body.PreviewWriteAccess = utils.BoolValueFalse
			}

			var updateResponse utils.AppUpdateSettingsResponse
			err = client.Post("/_clace/app_settings", values, body, &updateResponse)
			if err != nil {
				return err
			}

			for _, updateResult := range updateResponse.UpdateResults {
				fmt.Printf("Updating %s\n", updateResult)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated.\n", len(updateResponse.UpdateResults))

			if updateResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func appUpdateAuthnType(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "auth-type",
		Usage:     "Update authentication type for apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec> <value:default|none>",

		UsageText: `args: <pathSpec> <value:default|none>

	First required argument is <pathSpec>. The domain and path are separated by a ":". pathSpec supports a glob pattern.
	In the glob, * matches any number of characters, ** matches any number of characters including /.
	all is a shortcut for "*:**", which matches all apps across all domains, including no domain.
	To prevent shell expansion for *, placing the path in quotes is recommended.

	The second required argument <value> is a string, default or none.

	Examples:
	  Update all apps, across domains: clace app update auth-type all default
	  Update apps in the example.com domain: clace app auth-type "example.com:**" none`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <pathSpec> <value>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := utils.CreateUpdateAppRequest()
			body.AuthnType = utils.StringValue(cCtx.Args().Get(1))

			var updateResponse utils.AppUpdateSettingsResponse
			if err := client.Post("/_clace/app_settings", values, body, &updateResponse); err != nil {
				return err
			}

			for _, updateResult := range updateResponse.UpdateResults {
				fmt.Printf("Updating %s\n", updateResult)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated.\n", len(updateResponse.UpdateResults))

			if updateResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}

func appUpdateGitAuth(commonFlags []cli.Flag, clientConfig *utils.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "git-auth",
		Usage:     "Update git-auth entry for apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<pathSpec> <value>",

		UsageText: `args: <pathSpec> <value:default|none>

	First required argument is <pathSpec>. The domain and path are separated by a ":". pathSpec supports a glob pattern.
	In the glob, * matches any number of characters, ** matches any number of characters including /.
	all is a shortcut for "*:**", which matches all apps across all domains, including no domain.
	To prevent shell expansion for *, placing the path in quotes is recommended.

	The second required argument <value> is a string. Specify the git_auth entry key name as configured in the clace.toml config.

	Examples:
	  Update all apps, across domains: clace app update git-auth all mygit
	  Update apps in the example.com domain: clace app git-auth "example.com:**" gitentrykey`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <pathSpec> <value>")
			}

			client := utils.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(0))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := utils.CreateUpdateAppRequest()
			body.GitAuthName = utils.StringValue(cCtx.Args().Get(1))

			var updateResponse utils.AppUpdateSettingsResponse
			if err := client.Post("/_clace/app_settings", values, body, &updateResponse); err != nil {
				return err
			}

			for _, updateResult := range updateResponse.UpdateResults {
				fmt.Printf("Updating %s\n", updateResult)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated.\n", len(updateResponse.UpdateResults))

			if updateResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}
