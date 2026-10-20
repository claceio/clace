// Copyright (c) OpenRun Inc
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestBlueGreenDeployModeDecodesStringsAndBooleans(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]BlueGreenDeployMode{
		`blue_green_deploy = "auto"`:  BlueGreenDeployAuto,
		`blue_green_deploy = "True"`:  BlueGreenDeployTrue,
		`blue_green_deploy = "false"`: BlueGreenDeployFalse,
		`blue_green_deploy = true`:    BlueGreenDeployTrue,
		`blue_green_deploy = false`:   BlueGreenDeployFalse,
		``:                            "",
	} {
		var c Container
		if _, err := toml.Decode(input, &c); err != nil {
			t.Fatalf("decode %q: %v", input, err)
		}
		if c.BlueGreenDeploy != want {
			t.Fatalf("decode %q = %q, want %q", input, c.BlueGreenDeploy, want)
		}
	}

	for _, input := range []string{`blue_green_deploy = "sometimes"`, `blue_green_deploy = 1`} {
		var c Container
		if _, err := toml.Decode(input, &c); err == nil || !strings.Contains(err.Error(), "expected auto, true or false") {
			t.Fatalf("decode %q error = %v, want an invalid value error", input, err)
		}
	}
}

func TestBlueGreenDeployModeEncodesAsString(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(Container{BlueGreenDeploy: BlueGreenDeployFalse}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(buf.String(), `blue_green_deploy = "false"`) {
		t.Fatalf("encoded:\n%s\nwant blue_green_deploy = \"false\"", buf.String())
	}
	buf.Reset()
	if err := toml.NewEncoder(&buf).Encode(Container{}); err != nil {
		t.Fatalf("encode zero: %v", err)
	}
	if !strings.Contains(buf.String(), `blue_green_deploy = "auto"`) {
		t.Fatalf("encoded zero value:\n%s\nwant blue_green_deploy = \"auto\"", buf.String())
	}
}
