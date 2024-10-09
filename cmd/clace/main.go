// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"log"
	"os"
	"path"

	"github.com/urfave/cli/v2"

	"github.com/claceio/clace/internal/system"
	"github.com/claceio/clace/internal/types"
)

// Added by goreleaser as build information
var (
	gitCommit  string // gitCommit is the git commit that was compiled
	gitVersion string // gitVersion is the build tag
)

const configFileFlagName = "config-file"

func getAllCommands(globalConfig *types.GlobalConfig, clientConfig *types.ClientConfig, serverConfig *types.ServerConfig) ([]*cli.Command, error) {
	var allCommands []*cli.Command
	serverCommands, err := getServerCommands(serverConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	clientCommands, err := getClientCommands(clientConfig)
	if err != nil {
		return nil, err
	}

	passwordCommands, err := getPasswordCommands(clientConfig)
	if err != nil {
		return nil, err
	}

	for _, v := range [][]*cli.Command{
		serverCommands,
		clientCommands,
		passwordCommands,
	} {
		allCommands = append(allCommands, v...)
	}
	return allCommands, nil
}

func globalFlags(globalConfig *types.GlobalConfig, clientConfig *types.ClientConfig) ([]cli.Flag, error) {
	return []cli.Flag{
		&cli.StringFlag{
			Name:        configFileFlagName,
			Aliases:     []string{"c"},
			Usage:       "TOML configuration file",
			Destination: &globalConfig.ConfigFile,
			EnvVars:     []string{"CL_CONFIG_FILE"},
		},
		&cli.BoolFlag{
			Name:    "version",
			Aliases: []string{"v"},
			Usage:   "Print version info",
		},
	}, nil
}

func parseConfig(cCtx *cli.Context, globalConfig *types.GlobalConfig, clientConfig *types.ClientConfig, serverConfig *types.ServerConfig) error {
	filePath := path.Clean(os.ExpandEnv("$CL_HOME/clace.toml"))
	if cCtx.IsSet(configFileFlagName) {
		filePath = cCtx.String(configFileFlagName)
	}

	//fmt.Fprintf(os.Stderr, "Loading config file: %s\n", filePath)
	buf, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if err := system.LoadGlobalConfig(string(buf), globalConfig); err != nil {
		return err
	}

	if err := system.LoadClientConfig(string(buf), clientConfig); err != nil {
		return err
	}
	if err := system.LoadServerConfig(string(buf), serverConfig); err != nil {
		return err
	}

	return nil
}

func main() {
	globalConfig, clientConfig, serverConfig, err := system.GetDefaultConfigs()
	if err != nil {
		log.Fatal(err)
	}
	globalFlags, err := globalFlags(globalConfig, clientConfig)
	if err != nil {
		log.Fatal(err)
	}
	allCommands, err := getAllCommands(globalConfig, clientConfig, serverConfig)
	if err != nil {
		log.Fatal(err)
	}

	app := &cli.App{
		Name:                 "clace",
		Usage:                "Clace client and server https://clace.io/",
		EnableBashCompletion: true,
		Suggest:              true,
		Flags:                globalFlags,
		Before: func(ctx *cli.Context) error {
			err := parseConfig(ctx, globalConfig, clientConfig, serverConfig)
			if ctx.Command != nil && ctx.Command.Name == "password" {
				// For password command, ignore error parsing config
				return err
			}

			return nil
		},
		ExitErrHandler: func(c *cli.Context, err error) {
			if err != nil {
				fmt.Fprintf(cli.ErrWriter, "error: %s\n", err)
				os.Exit(1)
			}
		},
		Commands: allCommands,
		Action: func(ctx *cli.Context) error {
			// Default action when no subcommand is specified
			if ctx.Bool("version") {
				printVersion(ctx)
				os.Exit(0)
			}
			return cli.ShowAppHelp(ctx)
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s", err)
		os.Exit(1)
	}
}
