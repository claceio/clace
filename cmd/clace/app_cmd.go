// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func createAppCommand(commonFlags []cli.Flag, globalConfig *utils.GlobalConfig, clientConfig *utils.ClientConfig) *cli.Command {
	return &cli.Command{
		Name: "app",
		Subcommands: []*cli.Command{
			{
				Name:   "create",
				Usage:  "Create a new app",
				Flags:  commonFlags,
				Before: altsrc.InitInputSourceWithContext(commonFlags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
				Action: func(cCtx *cli.Context) error {
					clientConfig.GlobalConfig = *globalConfig
					fmt.Printf("Config url %s user %s passwd %s\n", clientConfig.ServerUrl, clientConfig.AdminUser, clientConfig.AdminPassword)
					return nil
				},
			},
		},
	}
}
