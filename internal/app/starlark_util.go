// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

func getStringAttr(s *starlarkstruct.Struct, key string) (string, error) {
	v, err := s.Attr(key)
	if err != nil {
		return "", fmt.Errorf("error getting %s: %s", key, err)
	}
	var vs starlark.String
	var ok bool
	if vs, ok = v.(starlark.String); !ok {
		return "", fmt.Errorf("%s is not a string", key)
	}
	return vs.GoString(), nil
}

func getBoolAttr(s *starlarkstruct.Struct, key string) (bool, error) {
	v, err := s.Attr(key)
	if err != nil {
		return false, fmt.Errorf("error getting %s: %s", key, err)
	}
	var vb starlark.Bool
	var ok bool
	if vb, ok = v.(starlark.Bool); !ok {
		return false, fmt.Errorf("%s is not a bool", key)
	}
	return bool(vb), nil
}

func getListStringAttr(s *starlarkstruct.Struct, key string, optional bool) ([]string, error) {
	v, err := s.Attr(key)
	if err != nil {
		if optional {
			return []string{}, nil
		} else {
			return nil, fmt.Errorf("error getting %s: %s", key, err)
		}
	}
	var list *starlark.List
	var ok bool
	if list, ok = v.(*starlark.List); !ok {
		return nil, fmt.Errorf("%s is not a list", key)
	}

	ret := []string{}
	iter := list.Iterate()
	var val starlark.Value
	var vs starlark.String
	count := -1
	for iter.Next(&val) {
		count++
		if vs, ok = val.(starlark.String); !ok {
			return nil, fmt.Errorf("iter %d in %s is not a string", count, key)
		}
		ret = append(ret, string(vs))
	}

	return ret, nil
}
