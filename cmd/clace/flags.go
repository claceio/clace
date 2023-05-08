// Copyright (c) Clace Inc
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

func envString(name string) string {
	return fmt.Sprintf("CL_%s", strings.ToUpper(name))
}

func newStringFlag(name, alias, usage, value string, destionation *string) *altsrc.StringFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewStringFlag(&cli.StringFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString(name)},
		Destination: destionation,
	})
}

func newIntFlag(name, alias, usage string, value int, destionation *int) *altsrc.IntFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewIntFlag(&cli.IntFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString(name)},
		Destination: destionation,
	})
}

func newBoolFlag(name, alias, usage string, value bool, destination *bool) *altsrc.BoolFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return altsrc.NewBoolFlag(&cli.BoolFlag{
		Name:        name,
		Aliases:     aliases,
		Usage:       usage,
		Value:       value,
		EnvVars:     []string{envString(name)},
		Destination: destination,
	})
}
