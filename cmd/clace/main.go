// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/claceio/clace/internal/utils"
	"github.com/claceio/clace/pkg/api"
)

const configFileFlagName = "config_file"

type GlobalConfig struct {
	ConfigFile string
}

func allCommands(globalConfig *GlobalConfig, serverConfig *api.ServerConfig, clientConfig *utils.ClientConfig) []*cli.Command {
	var commands []*cli.Command
	for _, v := range [][]*cli.Command{
		serverCommands(serverConfig),
		clientCommands(globalConfig, clientConfig),
	} {
		commands = append(commands, v...)
	}
	return commands
}

func globalFlags(globalConfig *GlobalConfig, clientConfig *utils.ClientConfig) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    configFileFlagName,
			Aliases: []string{"c"},
			Usage:   "TOML configuration file",
			//Destination: &globalConfig.ConfigFile,
			EnvVars: []string{"CL_CONFIG_FILE"},
		},
	}
}

func main() {
	globalConfig := GlobalConfig{}
	serverConfig := api.NewServerConfig()
	clientConfig := utils.NewClientConfig()
	globalFlags := globalFlags(&globalConfig, clientConfig)

	app := &cli.App{
		Name:                 "clace",
		Usage:                "Clace client and server https://clace.io/",
		EnableBashCompletion: true,
		Suggest:              true,
		Flags:                globalFlags,
		Before:               altsrc.InitInputSourceWithContext(globalFlags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		Commands:             allCommands(&globalConfig, serverConfig, clientConfig),
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Printf("error: %s", err)
		os.Exit(1)
	}
}
