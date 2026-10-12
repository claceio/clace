// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

type fakeSetter struct {
	calls []acp.SetSessionConfigOptionValueId
	err   error
}

func (f *fakeSetter) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	f.calls = append(f.calls, *params.ValueId)
	return acp.SetSessionConfigOptionResponse{}, f.err
}

func selectOption(id string, category *acp.SessionConfigOptionCategory, values ...string) acp.SessionConfigOption {
	opts := make(acp.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, v := range values {
		opts = append(opts, acp.SessionConfigSelectOption{Name: v, Value: acp.SessionConfigValueId(v)})
	}
	return acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:       acp.SessionConfigId(id),
		Category: category,
		Options:  acp.SessionConfigSelectOptions{Ungrouped: &opts},
	}}
}

func TestApplySessionConfigSuffixAndCategoryMatch(t *testing.T) {
	setter := &fakeSetter{}
	modelCat := acp.SessionConfigOptionCategoryModel
	thoughtCat := acp.SessionConfigOptionCategoryThoughtLevel
	options := []acp.SessionConfigOption{
		selectOption("llm", &modelCat, "anthropic/claude-fable-5", "openai/gpt-5"),
		selectOption("level", &thoughtCat, "low", "high"),
	}
	// bare model name matches via the provider/ suffix rule
	warnings := applySessionConfig(context.Background(), setter, "ses1", options, "claude-fable-5", "high")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(setter.calls) != 2 || setter.calls[0].Value != "anthropic/claude-fable-5" || setter.calls[1].ConfigId != "level" {
		t.Errorf("bad calls: %+v", setter.calls)
	}
}

func TestApplySessionConfigWarnings(t *testing.T) {
	// no options advertised at all
	warnings := applySessionConfig(context.Background(), &fakeSetter{}, "ses1", nil, "m1", "high")
	if len(warnings) != 2 || !strings.Contains(warnings[0], "does not advertise") {
		t.Errorf("expected 2 not-advertised warnings, got %v", warnings)
	}

	// model option advertised but the value is unknown
	options := []acp.SessionConfigOption{selectOption("model", nil, "a/m1", "a/m2")}
	warnings = applySessionConfig(context.Background(), &fakeSetter{}, "ses1", options, "nosuch", "")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "agent offers: a/m1, a/m2") {
		t.Errorf("expected unknown value warning, got %v", warnings)
	}

	// set call fails: warning, not error
	setter := &fakeSetter{err: fmt.Errorf("boom")}
	warnings = applySessionConfig(context.Background(), setter, "ses1", options, "a/m1", "")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "boom") {
		t.Errorf("expected set failure warning, got %v", warnings)
	}

	// nothing configured: no calls, no warnings
	warnings = applySessionConfig(context.Background(), setter, "ses1", options, "", "")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

// optionsSetter returns a new option set from each set call, like an agent
// whose effort levels depend on the chosen model
type optionsSetter struct {
	calls []acp.SetSessionConfigOptionValueId
	next  []acp.SessionConfigOption
}

func (f *optionsSetter) SetSessionConfigOption(ctx context.Context, params acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	f.calls = append(f.calls, *params.ValueId)
	return acp.SetSessionConfigOptionResponse{ConfigOptions: f.next}, nil
}

func TestApplySessionConfigUsesResponseOptions(t *testing.T) {
	// the effort option only offers "max" after the model change
	setter := &optionsSetter{next: []acp.SessionConfigOption{
		selectOption("model", nil, "big"),
		selectOption("effort", nil, "low", "max"),
	}}
	options := []acp.SessionConfigOption{
		selectOption("model", nil, "small", "big"),
		selectOption("effort", nil, "low"),
	}
	if warnings := applySessionConfig(context.Background(), setter, "ses1", options, "big", "max"); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(setter.calls) != 2 || setter.calls[1].Value != "max" {
		t.Fatalf("unexpected calls: %+v", setter.calls)
	}
}

func TestApplySessionConfigSkipsCurrentValue(t *testing.T) {
	setter := &fakeSetter{}
	option := selectOption("model", nil, "small", "big")
	option.Select.CurrentValue = "big"
	if warnings := applySessionConfig(context.Background(), setter, "ses1", []acp.SessionConfigOption{option}, "big", ""); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(setter.calls) != 0 {
		t.Fatalf("an option already at the configured value must not be set again: %+v", setter.calls)
	}
}
