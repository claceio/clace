// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/plugin"
	"github.com/claceio/clace/internal/types"
	"go.starlark.net/starlark"
)

const MAX_BYTES_STDOUT = 100 * 1024 * 1024 // 100MB

func init() {
	e := &ExecPlugin{}
	app.RegisterPlugin("exec", NewExecPlugin, []plugin.PluginFunc{
		app.CreatePluginApi(e.Run, app.READ_WRITE),
	})
}

type ExecPlugin struct {
}

func NewExecPlugin(_ *types.PluginContext) (any, error) {
	return &ExecPlugin{}, nil
}

func (e *ExecPlugin) Run(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		path                         starlark.String
		cmdArgs                      *starlark.List
		env                          *starlark.List
		processPartial, stdoutToFile starlark.Bool
	)
	if err := starlark.UnpackArgs("run", args, kwargs, "path", &path, "args?", &cmdArgs, "env?", &env,
		"process_partial?", &processPartial, "stdout_file", &stdoutToFile); err != nil {
		return nil, err
	}
	if cmdArgs == nil {
		cmdArgs = starlark.NewList([]starlark.Value{})
	}
	if env == nil {
		env = starlark.NewList([]starlark.Value{})
	}

	pathStr := string(path)
	argsList := make([]string, 0, cmdArgs.Len())
	envList := make([]string, 0, env.Len())
	processPartialBool := bool(processPartial)
	stdoutToFileBool := bool(stdoutToFile)

	for i := 0; i < cmdArgs.Len(); i++ {
		value, ok := cmdArgs.Index(i).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("args must be a list of strings")
		}
		argsList = append(argsList, string(value))
	}

	for i := 0; i < env.Len(); i++ {
		value, ok := env.Index(i).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("env must be a list of strings")
		}
		envList = append(envList, string(value))
	}

	cmd := exec.Command(pathStr, argsList...)
	cmd.Env = envList
	stdout, err := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	var tempFile *os.File

	if stdoutToFileBool {
		tempFile, err = os.CreateTemp("", "clace-exec-stdout-*")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary file: %w", err)
		}
		defer tempFile.Close()
		_, err = io.Copy(tempFile, stdout)
	} else {
		_, err = io.CopyN(&buf, stdout, MAX_BYTES_STDOUT)
	}

	if err != nil && err != io.EOF {
		return nil, err
	}

	runErr := cmd.Wait()
	if !processPartialBool && runErr != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s: %s", runErr, stderr.String())
		}
		return nil, runErr
	}

	if stdoutToFileBool {
		return app.NewResponse(starlark.String(tempFile.Name())), nil
	}

	lines := starlark.NewList([]starlark.Value{})
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		lines.Append(starlark.String(scanner.Text()))
	}

	if lines.Len() == 0 && runErr != nil {
		// if no lines in stdout and there was an error (processPartial case), return the error
		return nil, runErr
	}

	if scanner.Err() != nil {
		return nil, scanner.Err()
	}

	return app.NewResponse(lines), nil
}
