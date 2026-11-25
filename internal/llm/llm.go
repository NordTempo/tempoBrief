// Package llm makes structured calls to whichever model the user connected:
// the Claude API, any OpenAI-compatible API, or a subscription CLI (Claude
// Code, Codex) that is already logged in. Every call returns JSON validated
// against a schema derived from a Go type. See docs/providers.md.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/NordTempo/tempoBrief/internal/settings"
)

// Model returns one JSON object that matches the schema.
type Model interface {
	JSON(ctx context.Context, req Request) ([]byte, error)
	// Name is shown in status and logs, such as "anthropic/claude-haiku-5-5".
	Name() string
}

type Request struct {
	System string
	Prompt string
	// Schema is a JSON Schema for the answer: objects with every property
	// required and no additional properties, which every provider's strict
	// mode accepts.
	Schema map[string]any
	// Effort is a hint for providers that support it: "low" for bulk work.
	Effort string
}

// Generate asks the model for a T. The schema comes from T's json tags, and
// `desc` tags become property descriptions.
func Generate[T any](ctx context.Context, m Model, system, prompt, effort string) (T, error) {
	var out T
	raw, err := m.JSON(ctx, Request{System: system, Prompt: prompt, Schema: SchemaFor[T](), Effort: effort})
	if err != nil {
		return out, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("%s: answer does not match the schema: %w (got %.300s)", m.Name(), err, raw)
	}
	return out, nil
}

// New builds the model for the configured provider.
func New(p settings.Provider) (Model, error) {
	switch p.Kind {
	case settings.ProviderAnthropic:
		return newAnthropic(p)
	case settings.ProviderOpenAI:
		return newOpenAI(p)
	case settings.ProviderClaudeCLI:
		return claudeCLI{model: p.Model}, nil
	case settings.ProviderCodexCLI:
		return codexCLI{model: p.Model}, nil
	}
	return nil, fmt.Errorf("unknown provider %q", p.Kind)
}

// SchemaFor derives a strict JSON Schema from a Go struct type. It supports
// what tempoBrief's answers need: structs, slices, strings, integers,
// floats and booleans.
func SchemaFor[T any]() map[string]any {
	return schemaOf(reflect.TypeFor[T]())
}

func schemaOf(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Pointer:
		return schemaOf(t.Elem())
	case reflect.Struct:
		props := map[string]any{}
		required := []string{}
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			p := schemaOf(f.Type)
			if d := f.Tag.Get("desc"); d != "" {
				p["description"] = d
			}
			props[name] = p
			required = append(required, name)
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	}
	panic(fmt.Sprintf("llm: no schema for %s", t))
}
