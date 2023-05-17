// Copyright (c) ClaceIO, LLC
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

func newAltStringFlag(name, alias, usage, value string, destionation *string) *altsrc.StringFlag {
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

func newAltIntFlag(name, alias, usage string, value int, destionation *int) *altsrc.IntFlag {
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

func newAltBoolFlag(name, alias, usage string, value bool, destination *bool) *altsrc.BoolFlag {
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

func newStringFlag(name, alias, usage, value string) *cli.StringFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return &cli.StringFlag{
		Name:    name,
		Aliases: aliases,
		Usage:   usage,
		Value:   value,
	}
}

func newIntFlag(name, alias, usage string, value int) *cli.IntFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return &cli.IntFlag{
		Name:    name,
		Aliases: aliases,
		Usage:   usage,
		Value:   value,
	}
}

func newBoolFlag(name, alias, usage string, value bool) *cli.BoolFlag {
	var aliases []string
	if alias != "" {
		aliases = []string{alias}
	}
	return &cli.BoolFlag{
		Name:    name,
		Aliases: aliases,
		Usage:   usage,
		Value:   value,
	}
}
