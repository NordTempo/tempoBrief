package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type answer struct {
	Items []struct {
		ID    int64  `json:"id"`
		Label string `json:"label" desc:"short label"`
	} `json:"items"`
	Done bool `json:"done"`
}

func TestSchemaForIsStrict(t *testing.T) {
	b, _ := json.Marshal(SchemaFor[answer]())
	got := string(b)
	for _, want := range []string{
		`"additionalProperties":false`,
		`"required":["items","done"]`,
		`"required":["id","label"]`,
		`"description":"short label"`,
		`"id":{"type":"integer"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("schema lacks %s: %s", want, got)
		}
	}
}

type staticModel string

func (m staticModel) Name() string { return "static" }
func (m staticModel) JSON(context.Context, Request) ([]byte, error) {
	return []byte(m), nil
}

func TestGenerateRejectsAnswersOffSchema(t *testing.T) {
	if _, err := Generate[answer](context.Background(), staticModel(`{"items":[],"done":true,"extra":1}`), "", "", ""); err == nil {
		t.Fatal("unknown fields should be an error")
	}
	got, err := Generate[answer](context.Background(), staticModel(`{"items":[{"id":3,"label":"x"}],"done":true}`), "", "", "")
	if err != nil || len(got.Items) != 1 || got.Items[0].ID != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}
}
