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

func initPreviewCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	return &cli.Command{
		Name:  "preview",
		Usage: "Manage Clace preview apps",
		Subcommands: []*cli.Command{
			previewCreateCommand(commonFlags, clientConfig),
		},
	}
}

func previewCreateCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "create",
		Usage:     "Create a preview version of the app from specified git commit id",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<appPath> <gitCommitId>",
		UsageText: `args: <appPath> <gitCommitId>

    <app_path> is a required first argument. The optional domain and path are separated by a ":". This is the app for which the preview app is to be created.
    <gitCommitId> is a required second argument. This is the commit from which the preview app is to be created.

	Examples:
	  Preview and approve: clace preview create --approve /myapp 86c24c88ceda21589801895e9f871617a716ad47
	  Preview app in dryrun mode: clace preview create --dry-run example.com:/myapp 86c24c88ceda21589801895e9f871617a716ad47`,
		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() != 2 {
				return fmt.Errorf("requires two arguments: <appPath> <gitCommitId>")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.AdminPassword, clientConfig.SkipCertCheck)
			values := url.Values{}
			values.Add("appPath", cCtx.Args().First())
			values.Add("commitId", cCtx.Args().Get(1))
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))

			var previewResponse types.AppPreviewResponse
			err := client.Post("/_clace/preview", values, nil, &previewResponse)
			if err != nil {
				return err
			}

			approveResult := previewResponse.ApproveResult
			fmt.Printf("App audit results %s - %s\n", approveResult.AppPathDomain, approveResult.Id)
			printApproveResult(approveResult)

			status := "failed"
			if previewResponse.Success {
				status = "succeeded"
			}
			if approveResult.NeedsApproval {
				if cCtx.Bool("approve") {
					fmt.Printf("App creation %s. Permissions have been approved\n", status)
				} else {
					fmt.Printf("App creation %s, permissions need to be approved, add the --approve option\n", status)
				}
			} else {
				fmt.Printf("App creation %s. No approval required\n", status)
			}

			if previewResponse.DryRun {
				fmt.Print(DRY_RUN_MESSAGE)
			}

			if !previewResponse.Success {
				return fmt.Errorf("preview app creation failed")
			}
			return nil
		},
	}
}
