// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
		newStringFlag("http_host", "i", "The interface to bind on for HTTP", serverConfig.Http.Host, &serverConfig.Http.Host),
		newIntFlag("http_port", "p", "The port to listen on for HTTP", serverConfig.Http.Port, &serverConfig.Http.Port),
		newStringFlag("log_level", "l", "The logging level to use", serverConfig.Log.Level, &serverConfig.Log.Level),
		newBoolFlag("console_logging", "", "Enable console logging", serverConfig.Log.ConsoleLogging, &serverConfig.Log.ConsoleLogging),
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
						server, err := api.NewServer(serverConfig)
						if err != nil {
							fmt.Printf("Error initializing server: %s\n", err)
							os.Exit(1)
						}
						err = server.Start()
						if err != nil {
							fmt.Printf("Error starting server: %s\n", err)
							os.Exit(1)
						}

						c := make(chan os.Signal, 1)
						// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
						signal.Notify(c, os.Interrupt)

						// Block until we receive our signal.
						<-c

						// Create a deadline to wait for.
						ctxTimeout, cancel := context.WithTimeout(context.Background(), 30)
						defer cancel()
						server.Stop(ctxTimeout)

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
