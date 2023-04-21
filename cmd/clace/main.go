// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/claceio/clace/pkg/api"
)

const configFileFlagName = "config_file"

type GlobalConfig struct {
	ConfigFile string
}

type ClientConfig struct {
}

func allCommands(serverConfig *api.ServerConfig, clientConfig *ClientConfig) []*cli.Command {
	var commands []*cli.Command
	for _, v := range [][]*cli.Command{
		serverCommands(serverConfig),
		clientCommands(clientConfig),
	} {
		commands = append(commands, v...)
	}
	return commands
}

func clientCommands(clientConfig *ClientConfig) []*cli.Command {
	return nil
}

func newStringFlag(name, alias, usage, value string, destionation *string) *altsrc.StringFlag {
	envString := fmt.Sprintf("CL_%s", strings.ToUpper(name))
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewStringFlag(&cli.StringFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString},
		Destination: destionation,
	})
}

func newIntFlag(name, alias, usage string, value int, destionation *int) *altsrc.IntFlag {
	envString := fmt.Sprintf("CL_%s", strings.ToUpper(name))
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewIntFlag(&cli.IntFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString},
		Destination: destionation,
	})
}

func newBoolFlag(name, alias, usage string, value bool, destination *bool) *altsrc.BoolFlag {
	envString := fmt.Sprintf("CL_%s", strings.ToUpper(name))
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString},
		Destination: destination,
	})
}

func serverCommands(serverConfig *api.ServerConfig) []*cli.Command {

	flags := []cli.Flag{
		newStringFlag("listen_host", "i", "The interface to listen on", serverConfig.Host, &serverConfig.Host),
		newIntFlag("listen_port", "p", "The port to listen on", serverConfig.Port, &serverConfig.Port),
		newStringFlag("log_level", "l", "The logging level to use", serverConfig.LogLevel, &serverConfig.LogLevel),
		newBoolFlag("console_logging", "", "Enable console logging", serverConfig.ConsoleLogging, &serverConfig.ConsoleLogging),
	}

	return []*cli.Command{
		{
			Name: "server",
			Subcommands: []*cli.Command{
				{
					Name:   "start",
					Usage:  "Start the clace server",
					Flags:  flags,
					Before: altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
					Action: func(cCtx *cli.Context) error {
						server := api.NewServer(serverConfig)
						err := server.Start()
						if err != nil {
							fmt.Printf("Error starting server: %s\n", err)
							os.Exit(1)
						}
						return nil
					},
				},
			},
		},
	}
}

func globalFlags(globalConfig *GlobalConfig) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        configFileFlagName,
			Aliases:     []string{"c"},
			Usage:       "TOML configuration file",
			Destination: &globalConfig.ConfigFile,
			EnvVars:     []string{"CL_CONFIG_FILE"},
		},
	}
}

func main() {
	globalConfig := GlobalConfig{}
	serverConfig := api.NewServerConfig()
	clientConfig := ClientConfig{}
	globalFlags := globalFlags(&globalConfig)

	app := &cli.App{
		Name:                 "clace",
		Usage:                "Clace client and server https://clace.io/",
		EnableBashCompletion: true,
		Suggest:              true,
		Flags:                globalFlags,
		Before:               altsrc.InitInputSourceWithContext(globalFlags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		Commands:             allCommands(serverConfig, &clientConfig),
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println("error: ", err)
	}
}
