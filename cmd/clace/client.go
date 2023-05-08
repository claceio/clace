// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/claceio/clace/internal/utils"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func clientCommands(globalConfig *GlobalConfig, clientConfig *utils.ClientConfig) []*cli.Command {
	flags := []cli.Flag{
		newStringFlag("client.server_url", "s", "The server connection url", clientConfig.Conn.ServerUrl, &clientConfig.Conn.ServerUrl),
	}

	return []*cli.Command{
		{
			Name: "app",
			Subcommands: []*cli.Command{
				{
					Name:   "create",
					Usage:  "Create a new app",
					Flags:  flags,
					Before: altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
					Action: func(cCtx *cli.Context) error {
						fmt.Printf("Config url %s\n", clientConfig.Conn.ServerUrl)
						return nil
					},
				},
			},
		},
	}
}
