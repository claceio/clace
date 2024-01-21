// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"fmt"
	"slices"
	"strings"
)

const (
	AND_CONDITION = "$AND"
	OR_CONDITION  = "$OR"
)

var opToSql map[string]string

func init() {
	opToSql = map[string]string{
		"$gt":  ">",
		"$lt":  "<",
		"$gte": ">=",
		"$lte": "<=",
		"$eq":  "=",
		"$ne":  "!=",
	}
}

func parseQuery(query map[string]any) (string, []interface{}, error) {
	var conditions []string
	var params []interface{}

	var keys []string
	for key := range query {
		keys = append(keys, key)
	}
	slices.Sort(keys) // Sort the keys, mainly for easily testing the generated query

	for _, key := range keys {
		value := query[key]
		condition, subParams, err := parseCondition(key, value)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, condition)
		params = append(params, subParams...)
	}

	joinedConditions := strings.Join(conditions, " AND ")
	return joinedConditions, params, nil
}

func parseCondition(field string, value any) (string, []any, error) {
	switch v := value.(type) {
	case []map[string]any:
		// Check if the map represents a logical operator or multiple conditions
		if isLogicalOperator(field) {
			return parseLogicalOperator(field, v)
		}

		return "", nil, fmt.Errorf("invalid condition for %s, list supported for logical operators only, got: %#v", field, value)
	case map[string]any:
		if isLogicalOperator(field) {
			return "", nil, fmt.Errorf("invalid condition for %s, expected list, got: %#v", field, value)
		}
		op := getOperator(field)
		if op != "" {
			return "", nil, fmt.Errorf("operator %s supported for field conditions only: %#v", field, value)
		}
		return parseFieldCondition(field, v)
	case map[any]any:
		return "", nil, fmt.Errorf("invalid query condition for %s, only map of strings supported: %#v", field, value)
	case []any:
		return "", nil, fmt.Errorf("invalid query condition for %s, only list of maps supported: %#v", field, value)
	default:
		if isLogicalOperator(field) {
			return "", nil, fmt.Errorf("invalid condition for %s, expected list of maps, got: %#v", field, value)
		}
		if getOperator(field) != "" {
			return "", nil, fmt.Errorf("operator %s supported for field conditions only: %#v", field, value)
		}
		// Simple equality condition
		return fmt.Sprintf("%s = ?", field), []any{v}, nil
	}
}

func parseLogicalOperator(operator string, query []map[string]any) (string, []any, error) {
	var conditions []string
	var params []interface{}

	for _, cond := range query {
		condition, subParams, err := parseQuery(cond)
		if err != nil {
			return "", nil, err
		}

		conditions = append(conditions, condition)
		params = append(params, subParams...)
	}

	joiner := " AND "
	if strings.ToUpper(operator) == OR_CONDITION {
		joiner = " OR "
	}

	joinedConditions := strings.Join(conditions, joiner)
	return " ( " + joinedConditions + " ) ", params, nil
}

func parseFieldCondition(field string, query map[string]any) (string, []any, error) {
	var keys []string
	for key := range query {
		keys = append(keys, key)
	}
	slices.Sort(keys) // Sort the keys, mainly for easily testing the generated query

	var conditions []string
	var params []interface{}

	var err error
	for _, key := range keys {
		value := query[key]

		var subCondition string
		var subParams []any
		switch v := value.(type) {
		case []map[string]any:
			// Check if the map represents a logical operator or multiple conditions
			if isLogicalOperator(key) {
				subCondition, subParams, err = parseFieldLogicalOperator(field, key, v)
				if err != nil {
					return "", nil, err
				}
			} else {
				return "", nil, fmt.Errorf("invalid condition for %s %s, list supported for logical operators only, got: %#v", field, key, value)
			}
		case map[string]any:
			return "", nil, fmt.Errorf("invalid query condition for %s %s, map not supported: %#v", field, key, value)
		case map[any]any:
			return "", nil, fmt.Errorf("invalid query condition for %s %s, map not supported: %#v", field, key, value)
		case []any:
			return "", nil, fmt.Errorf("invalid query condition for %s %s, only list of maps supported: %#v", field, key, value)
		default:
			op := getOperator(key)
			if op == "" {
				return "", nil, fmt.Errorf("invalid query condition for %s %s, only operators supported: %#v", field, key, value)
			}
			subCondition = fmt.Sprintf("%s %s ?", field, op)
			subParams = []any{value}
		}

		conditions = append(conditions, subCondition)
		params = append(params, subParams...)
	}

	joinedConditions := strings.Join(conditions, " AND ")
	return joinedConditions, params, nil
}

func parseFieldLogicalOperator(field string, operator string, query []map[string]any) (string, []any, error) {
	var conditions []string
	var params []interface{}

	for _, cond := range query {
		if len(cond) != 1 {
			return "", nil, fmt.Errorf("invalid logical condition for %s %s, only one key supported: %#v", field, operator, cond)
		}

		for key, value := range cond {
			op := getOperator(key)
			if op == "" {
				return "", nil, fmt.Errorf("invalid logical condition for %s %s, only operators supported: %#v", field, key, value)
			}
			conditions = append(conditions, fmt.Sprintf("%s %s ?", field, op))
			params = append(params, value)
		}
	}

	joiner := " AND "
	if strings.ToUpper(operator) == OR_CONDITION {
		joiner = " OR "
	}

	joinedConditions := strings.Join(conditions, joiner)
	return " ( " + joinedConditions + " ) ", params, nil
}

func isLogicalOperator(operator string) bool {
	operator = strings.ToUpper(operator)
	return operator == AND_CONDITION || operator == OR_CONDITION
}

func getOperator(field string) string {
	return opToSql[field]
}
