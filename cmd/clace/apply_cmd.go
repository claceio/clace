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

func initApplyCommand(commonFlags []cli.Flag, clientConfig *types.ClientConfig) *cli.Command {
	flags := make([]cli.Flag, 0, len(commonFlags)+2)
	flags = append(flags, commonFlags...)
	flags = append(flags, newStringFlag("branch", "b", "The branch to checkout if using git source", "main"))
	flags = append(flags, newStringFlag("commit", "c", "The commit SHA to checkout if using git source. This takes precedence over branch", ""))
	flags = append(flags, newStringFlag("git-auth", "g", "The name of the git_auth entry in server config to use", ""))
	flags = append(flags, newBoolFlag("approve", "a", "Approve the app permissions", false))
	flags = append(flags, dryRunFlag())

	return &cli.Command{
		Name:      "apply",
		Usage:     "Apply app configuration declaratively",
		Flags:     flags,
		Before:    altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		ArgsUsage: "<filePath> [<appPathGlob>]",
		UsageText: `args: <filePath> [<appPathGlob>]

<filePath> is the path to the file containing the app configuration.
<appPathGlob> defaults to "*:**" (same as "all") for the apply command.
` + PATH_SPEC_HELP +
			`
Examples:
  Apply app config for all apps: clace apply ./app.clace
  Apply app config for all apps: clace apply ./app.clace all
  Apply app config for example.com domain apps: clace apply ./app.clace example.com:**
  Apply app config from git for all apps: clace apply github.com/claceio/apps/app.clace 
`,

		Action: func(cCtx *cli.Context) error {
			if cCtx.NArg() == 0 || cCtx.NArg() > 2 {
				return fmt.Errorf("expected one or two arguments: <filePath> [<appPathGlob>]")
			}

			client := system.NewHttpClient(clientConfig.ServerUri, clientConfig.AdminUser, clientConfig.Client.AdminPassword, clientConfig.Client.SkipCertCheck)
			values := url.Values{}
			values.Add("applyPath", cCtx.Args().Get(0))
			if cCtx.NArg() == 2 {
				values.Add("appPathGlob", cCtx.Args().Get(1))
			}
			values.Add("commitId", cCtx.Args().Get(0))
			values.Add("approve", strconv.FormatBool(cCtx.Bool("approve")))
			values.Add(DRY_RUN_ARG, strconv.FormatBool(cCtx.Bool(DRY_RUN_FLAG)))
			values.Add("branch", cCtx.String("branch"))
			values.Add("commit", cCtx.String("commit"))
			values.Add("gitAuth", cCtx.String("git-auth"))

			var applyResponse types.AppApplyResponse
			err := client.Post("/_clace/apply", values, nil, &applyResponse)
			if err != nil {
				return err
			}

			fmt.Printf("Applied %#+v\n", applyResponse)
			return nil
		},
	}
}
