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
	plugin := &execPlugin{}
	app.RegisterPlugin("exec", plugin.Struct())
}

// execPlugin is a plugin that provides OS command execution functionality
type execPlugin struct {
}

// Struct returns this plugins's methods as a starlark Struct
func (e *execPlugin) Struct() *starlarkstruct.Struct {
	return starlarkstruct.FromStringDict(starlarkstruct.Default, e.StringDict())
}

// StringDict returns all plugin methods in a starlark.StringDict
func (e *execPlugin) StringDict() starlark.StringDict {
	return starlark.StringDict{
		"run": starlark.NewBuiltin("run", e.run),
	}
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

func createResponse(err error) *execResponse {
	lines := starlark.NewList([]starlark.Value{})
	if exitErr, ok := err.(*exec.ExitError); ok {
		return &execResponse{
			exitCode: exitErr.ExitCode(),
			error:    exitErr.Error(),
			stderr:   string(exitErr.Stderr),
			lines:    lines,
		}
	}

	return &execResponse{
		exitCode: 127,
		error:    err.Error(),
		stderr:   err.Error(),
		lines:    lines,
	}
}

func (e *execPlugin) run(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		path    starlark.String
		cmdArgs *starlark.List
		env     *starlark.List
	)
	if err := starlark.UnpackArgs("run", args, kwargs, "path", &path, "args?", &cmdArgs, "env?", &env); err != nil {
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
	cmd.Stderr = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		ret := createResponse(err)
		return ret.Struct(), nil
	}

	var buf bytes.Buffer
	nCopied, err := io.CopyN(&buf, stdout, MAX_BYTES_STDOUT)
	if err != nil && err != io.EOF {
		return nil, err
	}

	err = cmd.Wait()
	if err != nil {
		ret := createResponse(err)
		return ret.Struct(), nil
	}

	lines := starlark.NewList([]starlark.Value{})
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	for scanner.Scan() {
		lines.Append(starlark.String(scanner.Text()))
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
