package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const configFileFlagName = "config_file"

type GlobalConfig struct {
	ConfigFile string
}

type ServerConfig struct {
	Host     string
	Port     int
	LogLevel string
}

type ClientConfig struct {
}

func allCommands(serverConfig *ServerConfig, clientConfig *ClientConfig) []*cli.Command {
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

func serverCommands(serverConfig *ServerConfig) []*cli.Command {
	flags := []cli.Flag{
		newStringFlag("listen_host", "i", "The interface to listen on", "127.0.01", &serverConfig.Host),
		newIntFlag("listen_port", "p", "The port to listen on", 25223, &serverConfig.Port),
		newStringFlag("log_level", "l", "The logging level to use", "INFO", &serverConfig.LogLevel),
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
						fmt.Printf("Starting server, addr %s port %d log_level  %s\n",
							serverConfig.Host, serverConfig.Port, serverConfig.LogLevel)
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
	serverConfig := ServerConfig{}
	clientConfig := ClientConfig{}
	globalFlags := globalFlags(&globalConfig)

	app := &cli.App{
		Name:                 "clace",
		Usage:                "Clace client and server https://clace.io/",
		EnableBashCompletion: true,
		Suggest:              true,
		Flags:                globalFlags,
		Before:               altsrc.InitInputSourceWithContext(globalFlags, altsrc.NewTomlSourceFromFlagFunc(configFileFlagName)),
		Commands:             allCommands(&serverConfig, &clientConfig),
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Println("error: ", err)
	}
}
