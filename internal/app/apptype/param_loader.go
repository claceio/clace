// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package apptype

import (
	"fmt"
	"regexp"

	"github.com/claceio/clace/internal/app/starlark_type"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

const (
	PARAM = "param"
)

// AppParam represents a parameter in an app.
type AppParam struct {
	Name         string
	Description  string
	Type         starlark_type.TypeName
	DefaultValue starlark.Value
}

func ReadParamInfo(fileName string, inp []byte) (map[string]AppParam, error) {
	paramInfo, err := LoadParamInfo(fileName, inp)
	if err != nil {
		return nil, err
	}

	if err := validateParamInfo(paramInfo); err != nil {
		return nil, err
	}

	return paramInfo, nil
}

var spaceRegex = regexp.MustCompile(`\s`)

func validateParamInfo(paramInfo map[string]AppParam) error {
	for _, p := range paramInfo {
		if p.Name == "" {
			return fmt.Errorf("param name is required")
		}
		if spaceRegex.MatchString(p.Name) {
			return fmt.Errorf("param name \"%s\" has spaces", p.Name)
		}
	}
	return nil
}

func LoadParamInfo(fileName string, data []byte) (map[string]AppParam, error) {
	definedParams := make(map[string]AppParam)

	paramBuiltin := func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var name, description, dataType starlark.String
		var defaultValue starlark.Value = starlark.None

		if err := starlark.UnpackArgs(PARAM, args, kwargs, "name", &name, "type?", &dataType, "default?", &defaultValue, "description?", &description); err != nil {
			return nil, err
		}

		if _, ok := definedParams[string(name)]; ok {
			return nil, fmt.Errorf("param %s already defined", name)
		}

		typeVal := starlark_type.TypeName(dataType)
		if typeVal == "" {
			typeVal = starlark_type.STRING
		}
		if typeVal != starlark_type.INT && typeVal != starlark_type.STRING &&
			typeVal != starlark_type.BOOLEAN && typeVal != starlark_type.DICT && typeVal != starlark_type.LIST {
			return nil, fmt.Errorf("unknown type %s for %s", typeVal, name)
		}

		definedParams[string(name)] = AppParam{
			Name:         string(name),
			Type:         typeVal,
			DefaultValue: defaultValue,
			Description:  string(description),
		}

		paramDict := starlark.StringDict{
			"name":        name,
			"type":        dataType,
			"default":     defaultValue,
			"description": description,
		}
		newParam := starlarkstruct.FromStringDict(starlark.String(PARAM), paramDict)
		return newParam, nil
	}

	builtins := starlark.StringDict{
		PARAM:                         starlark.NewBuiltin(PARAM, paramBuiltin),
		string(starlark_type.INT):     starlark.String(starlark_type.INT),
		string(starlark_type.STRING):  starlark.String(starlark_type.STRING),
		string(starlark_type.BOOLEAN): starlark.String(starlark_type.BOOLEAN),
		string(starlark_type.DICT):    starlark.String(starlark_type.DICT),
		string(starlark_type.LIST):    starlark.String(starlark_type.LIST),
	}

	thread := &starlark.Thread{
		Name:  fileName,
		Print: func(_ *starlark.Thread, msg string) { fmt.Println(msg) },
	}

	_, err := starlark.ExecFile(thread, fileName, data, builtins)
	if err != nil {
		if evalErr, ok := err.(*starlark.EvalError); ok {
			fmt.Printf("Error loading app params: %s\n", evalErr.Backtrace()) // TODO: log
		}
		return nil, fmt.Errorf("error loading params: %w", err)
	}

	return definedParams, nil
}
