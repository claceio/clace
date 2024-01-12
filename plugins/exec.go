// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"

	"github.com/claceio/clace/internal/app"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const MAX_BYTES_STDOUT = 100 * 1024 * 1024 // 100MB

func init() {
	e := &ExecPlugin{}
	app.RegisterPlugin("exec", NewExecPlugin, []app.PluginFunc{
		app.CreatePluginApi(e.Run, false),
	})
}

// execResponse is the response from an exec command
type execResponse struct {
	exitCode  int
	error     string
	stderr    string
	lines     *starlark.List
	truncated bool
}

// Struct turns a response into a *starlark.Struct
func (r *execResponse) Struct() *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"lines":     r.lines,
		"exit_code": starlark.MakeInt(r.exitCode),
		"error":     starlark.String(r.error),
		"stderr":    starlark.String(r.stderr),
		"truncated": starlark.Bool(r.truncated),
	})
}

func createResponse(err error, stdout io.ReadCloser, stderr bytes.Buffer) *execResponse {
	lines := starlark.NewList([]starlark.Value{})
	if exitErr, ok := err.(*exec.ExitError); ok {

		return &execResponse{
			exitCode: exitErr.ExitCode(),
			error:    exitErr.Error(),
			stderr:   stderr.String(),
			lines:    lines,
		}
	}

	return &execResponse{
		exitCode: 127,
		error:    err.Error(),
		stderr:   stderr.String(),
		lines:    lines,
	}
}

type ExecPlugin struct {
}

func NewExecPlugin(_ *app.PluginContext) (any, error) {
	return &ExecPlugin{}, nil
}

func (e *ExecPlugin) Run(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		path           starlark.String
		cmdArgs        *starlark.List
		env            *starlark.List
		processPartial starlark.Bool
	)
	if err := starlark.UnpackArgs("run", args, kwargs, "path", &path, "args?", &cmdArgs, "env?", &env, "process_partial?", &processPartial); err != nil {
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
		ret := createResponse(err, stdout, stderr)
		return ret.Struct(), nil
	}

	var buf bytes.Buffer
	nCopied, err := io.CopyN(&buf, stdout, MAX_BYTES_STDOUT)
	if err != nil && err != io.EOF {
		return nil, err
	}

	runErr := cmd.Wait()
	if !processPartialBool && runErr != nil {
		ret := createResponse(runErr, stdout, stderr)
		return ret.Struct(), nil
	}

	lines := starlark.NewList([]starlark.Value{})
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		lines.Append(starlark.String(scanner.Text()))
	}

	if lines.Len() == 0 && runErr != nil {
		// if no lines in stdout and there was an error (processPartial case), return the error
		ret := createResponse(runErr, stdout, stderr)
		return ret.Struct(), nil
	}

	if scanner.Err() != nil {
		return nil, scanner.Err()
	}

	ret := execResponse{
		lines:     lines,
		truncated: nCopied == MAX_BYTES_STDOUT,
	}
	return ret.Struct(), nil
}
