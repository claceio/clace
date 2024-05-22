// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func appUpdateSettingsCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "update-settings",
		Usage: "Update Clace apps settings. Settings changes are NOT staged, they apply immediately to matched stage, prod and preview apps.",
		Subcommands: []*cli.Command{
			appUpdateStageWrite(commonFlags, clientConfig),
			appUpdatePreviewWrite(commonFlags, clientConfig),
			appUpdateAuthnType(commonFlags, clientConfig),
			appUpdateGitAuth(commonFlags, clientConfig),
		},
	}
}

func appUpdateStageWrite(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "stage-write-access",
		Usage:     "Update write access permission for staging app",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<value:true|false> <pathSpec>",

		UsageText: `args: <value:true|false> <pathSpec>

The first required argument <value> is a boolean value, true or false.
The second required argument is <pathSpec>. ` + PATH_SPEC_HELP + `

	Examples:
	  Update all apps, across domains: clace app update-settings stage-write-access true all
	  Update apps in the example.com domain: clace app update-settings stage-write-access false "example.com:**"`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <value> <pathSpec>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := types.CreateUpdateAppRequest()
			boolValue, err := strconv.ParseBool(cCtx.Args().Get(0))
			if err != nil {
				return fmt.Errorf("invalid value %s for stage-write-access, expected true or false", cCtx.Args().Get(0))
			}
			if boolValue {
				body.StageWriteAccess = types.BoolValueTrue
			} else {
				body.StageWriteAccess = types.BoolValueFalse
			}

			var updateResponse types.AppUpdateSettingsResponse
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

func appUpdatePreviewWrite(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "preview-write-access",
		Usage:     "Update write access permission for preview apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<value:true|false> <pathSpec>",

		UsageText: `args: <value:true|false> <pathSpec>

The first required argument <value> is a boolean value, true or false.
The second required argument is <pathSpec>. ` + PATH_SPEC_HELP + `

	Examples:
	  Update all apps, across domains: clace app update-settings preview-write-access true all 
	  Update apps in the example.com domain: clace app update-settings preview-write-access false "example.com:**"`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <value> <pathSpec>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := types.CreateUpdateAppRequest()
			boolValue, err := strconv.ParseBool(cCtx.Args().Get(0))
			if err != nil {
				return fmt.Errorf("invalid value %s for preview-write-access, expected true or false", cCtx.Args().Get(0))
			}
			if boolValue {
				body.PreviewWriteAccess = types.BoolValueTrue
			} else {
				body.PreviewWriteAccess = types.BoolValueFalse
			}

			var updateResponse types.AppUpdateSettingsResponse
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

func appUpdateAuthnType(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "auth",
		Usage:     "Update authentication mode for apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<value:system|default|none|custom> <pathSpec>",

		UsageText: `args: <value:default|none> <pathSpec>

The first required argument <value> is a string, system, default, none or OAuth entry name.
The second required argument is <pathSpec>. ` + PATH_SPEC_HELP + `

	Examples:
	  Update all apps, across domains: clace app update-settings auth default all
	  Update apps in the example.com domain: clace app update-settings auth none "example.com:**"`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <value> <pathSpec>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := types.CreateUpdateAppRequest()
			body.AuthnType = types.StringValue(cCtx.Args().Get(0))

			var updateResponse types.AppUpdateSettingsResponse
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

func appUpdateGitAuth(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "git-auth",
		Usage:     "Update git-auth entry for apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<entryName> <pathSpec>",

		UsageText: `args: <entryName> <pathSpec> 

The first required argument <entryName> is a string. Specify the git_auth entry key name as configured in the clace.toml config.
Set to "-" to remove the git_auth entry.
The second required argument is <pathSpec>. ` + PATH_SPEC_HELP + `

	Examples:
	  Update all apps, across domains: clace app update-settings git-auth mygit all
	  Update apps in the example.com domain: clace app git-auth gitentrykey "example.com:**"`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <entryName> <pathSpec>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			body := types.CreateUpdateAppRequest()
			body.GitAuthName = types.StringValue(cCtx.Args().Get(0))

			var updateResponse types.AppUpdateSettingsResponse
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

func appUpdateMetadataCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "update-metadata",
		Usage: `Update Clace app metadata. Metadata updates are staged and have to be promoted to prod. Use "clace param" to update app parameter metadata.`,
		Subcommands: []*cli.Command{
			appUpdateAppSpec(commonFlags, clientConfig),
		},
	}
}

func appUpdateAppSpec(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, dryRunFlag())
	flags = append(flags, newBoolFlag(PROMOTE_FLAG, "p", "Promote the change from stage to prod", false))

	return &cli.Command{
		Name:      "spec",
		Usage:     "Update app spec for apps",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<value:spec_name|none> <pathSpec>",

		UsageText: `args: <value:spec_name|none> <pathSpec>

The first required argument <value> is a string, a valid app spec name or - (to unset spec).
The second required argument is <pathSpec>. ` + PATH_SPEC_HELP + `

	Examples:
	  Update all apps, across domains: clace app update-metadata spec - all
	  Update apps in the example.com domain: clace app update-metadata spec proxy "example.com:**"`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two argument: <value> <pathSpec>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("pathSpec", cCtx.Args().Get(1))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))
			values.Add(PROMOTE_ARG, strconv.FormatBool(cCtx.Bool(PROMOTE_FLAG)))

			body := types.CreateUpdateAppMetadataRequest()
			body.Spec = types.StringValue(cCtx.Args().Get(0))

			var updateResponse types.AppUpdateMetadataResponse
			if err := client.Post("/_clace/app_metadata", values, body, &updateResponse); err != nil {
				return err
			}

			for _, updateResult := range updateResponse.StagedUpdateResults {
				fmt.Printf("Updating %s\n", updateResult)
			}
			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated.\n", len(updateResponse.StagedUpdateResults))

			if len(updateResponse.PromoteResults) > 0 {
				fmt.Fprintf(cCtx.App.Writer, "Promoted apps: ")
				for i, promoteResult := range updateResponse.PromoteResults {
					if i > 0 {
						fmt.Fprintf(cCtx.App.Writer, ", ")
					}
					fmt.Fprintf(cCtx.App.Writer, "%s", promoteResult)
				}
				fmt.Fprintln(cCtx.App.Writer)
			}

			fmt.Fprintf(cCtx.App.Writer, "%d app(s) updated, %d app(s) promoted.\n", len(updateResponse.StagedUpdateResults), len(updateResponse.PromoteResults))

			if updateResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			return nil
		},
	}
}
