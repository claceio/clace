// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package stardefs

import (
	"fmt"

	"go.starlark.net/starlark"
)

func Convert(value starlark.Value) (interface{}, error) {
	switch v := value.(type) {
	case starlark.String:
		return string(v), nil
	case starlark.Int:
		num, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("unable to convert starlark.Int to int64")
		}
		return num, nil
	case starlark.Float:
		return float64(v), nil
	case starlark.Bool:
		return bool(v), nil
	case *starlark.List:
		length := v.Len()
		goList := make([]interface{}, length)

		for i := 0; i < length; i++ {
			item := v.Index(i)
			goItem, err := Convert(item)
			if err != nil {
				return nil, fmt.Errorf("error converting list item: %w", err)
			}
			goList[i] = goItem
		}
		return goList, nil
	case starlark.Tuple:
		length := v.Len()
		goList := make([]interface{}, length)

		for i := 0; i < length; i++ {
			item := v.Index(i)
			goItem, err := Convert(item)
			if err != nil {
				return nil, fmt.Errorf("error converting tuple item: %w", err)
			}
			goList[i] = goItem
		}
		return goList, nil
	case *starlark.Dict:
		return dictToGoMap(v)
	// Add more cases for other types as needed
	default:
		return nil, fmt.Errorf("unsupported value type '%T'", value)
	}
}

func dictToGoMap(d *starlark.Dict) (map[string]interface{}, error) {
	goMap := make(map[string]interface{}, d.Len())

	for _, k := range d.Keys() {
		v, _, _ := d.Get(k)

		goKey, err := Convert(k)
		if err != nil {
			return nil, fmt.Errorf("error converting key: %w", err)
		}

		goValue, err := Convert(v)
		if err != nil {
			return nil, fmt.Errorf("error converting value for key '%v': %w", goKey, err)
		}

		if strKey, ok := goKey.(string); ok {
			goMap[strKey] = goValue
		} else {
			return nil, fmt.Errorf("non-string key found: '%v'", goKey)
		}
	}

	return goMap, nil
}
