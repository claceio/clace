// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package starlark_type

import (
	"fmt"
	"hash/fnv"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// TemplateChunk is one piece of a Template: template text to evaluate, or
// literal text (a plain string concatenated to a template) passed through
// as is
type TemplateChunk struct {
	Text string
	Eval bool // evaluate as a template; false for literal text
	// Strict applies to evaluated chunks: the text must parse and is always
	// executed (ace.eval). Lenient chunks (app level param values wrapped
	// automatically) pass through unless they are templates referencing
	// secrets, as arbitrary operator strings may contain {{
	Strict bool
}

// Template is an opaque Starlark value holding template text that is
// evaluated (secret references, template functions) when the value is passed
// as a plugin argument. Values come from ace.eval and from app level param
// values containing {{. Plain Starlark strings are never template evaluated,
// so request data an app forwards to a plugin cannot resolve secrets. A
// template concatenated with a string (+) keeps the string as a literal
// chunk, never evaluated, so "Bearer " + param.key works and user text joined
// to a param cannot introduce a template. Other string operations are type
// errors, which keeps the resolved value out of app code
type Template struct {
	Chunks []TemplateChunk
}

// NewTemplate returns a template for text the app explicitly asked to
// evaluate (strict) or an app level value wrapped automatically (lenient)
func NewTemplate(text string, strict bool) Template {
	return Template{Chunks: []TemplateChunk{{Text: text, Eval: true, Strict: strict}}}
}

// Text returns the unevaluated text of the template
func (t Template) Text() string {
	var b strings.Builder
	for _, c := range t.Chunks {
		b.WriteString(c.Text)
	}
	return b.String()
}

// Resolve evaluates the template with the given evaluator, which is called
// for each evaluated chunk with its strictness
func (t Template) Resolve(eval func(text string, strict bool) (string, error)) (string, error) {
	var b strings.Builder
	for _, c := range t.Chunks {
		if !c.Eval {
			b.WriteString(c.Text)
			continue
		}
		s, err := eval(c.Text, c.Strict)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

func (t Template) String() string {
	return t.Text()
}

func (t Template) Type() string {
	return "template"
}

func (t Template) Freeze() {
}

func (t Template) Truth() starlark.Bool {
	return t.Text() != ""
}

func (t Template) Hash() (uint32, error) {
	h := fnv.New32a()
	h.Write([]byte(t.Text())) //nolint:errcheck
	return h.Sum32(), nil
}

// Binary implements + with a string on either side, and with another
// template. String operands become literal chunks
func (t Template) Binary(op syntax.Token, y starlark.Value, side starlark.Side) (starlark.Value, error) {
	if op != syntax.PLUS {
		return nil, nil // unsupported, starlark reports the type error
	}
	var other []TemplateChunk
	switch v := y.(type) {
	case starlark.String:
		other = []TemplateChunk{{Text: string(v)}}
	case Template:
		other = v.Chunks
	case *Template:
		other = v.Chunks
	default:
		return nil, nil
	}
	chunks := make([]TemplateChunk, 0, len(t.Chunks)+len(other))
	if side == starlark.Left {
		chunks = append(append(chunks, t.Chunks...), other...)
	} else {
		chunks = append(append(chunks, other...), t.Chunks...)
	}
	return Template{Chunks: chunks}, nil
}

func (t Template) CompareSameType(op syntax.Token, y starlark.Value, depth int) (bool, error) {
	other, ok := y.(Template)
	if !ok {
		if p, isPtr := y.(*Template); isPtr {
			other = *p
		} else {
			return false, fmt.Errorf("cannot compare template with %s", y.Type())
		}
	}
	switch op {
	case syntax.EQL:
		return t.Text() == other.Text(), nil
	case syntax.NEQ:
		return t.Text() != other.Text(), nil
	}
	return false, fmt.Errorf("template %s template not implemented", op)
}

// TemplateValue returns the template held by v, if it is one
func TemplateValue(v starlark.Value) (Template, bool) {
	switch t := v.(type) {
	case Template:
		return t, true
	case *Template:
		return *t, true
	}
	return Template{}, false
}

var _ starlark.Value = (*Template)(nil)
var _ starlark.HasBinary = (*Template)(nil)
var _ starlark.Comparable = (*Template)(nil)
