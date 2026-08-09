package lexicons

import (
	"encoding/json"
	"testing"
)

func TestDeveloperProfileLexicon(t *testing.T) {
	t.Parallel()
	contents, err := Files.ReadFile("dev.adenosine.profile.json")
	if err != nil {
		t.Fatalf("read profile Lexicon: %v", err)
	}
	var schema struct {
		Lexicon int    `json:"lexicon"`
		ID      string `json:"id"`
		Defs    map[string]struct {
			Type   string `json:"type"`
			Key    string `json:"key"`
			Record struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"record"`
		} `json:"defs"`
	}
	if err := json.Unmarshal(contents, &schema); err != nil {
		t.Fatalf("decode profile Lexicon: %v", err)
	}
	main, ok := schema.Defs["main"]
	if schema.Lexicon != 1 || schema.ID != "dev.adenosine.profile" || !ok || main.Type != "record" || main.Key != "literal:self" {
		t.Fatalf("unexpected profile Lexicon identity: %#v", schema)
	}
	if len(main.Record.Required) != 1 || main.Record.Required[0] != "createdAt" {
		t.Fatalf("required fields = %v", main.Record.Required)
	}
	for _, field := range []string{"displayName", "bio", "website", "location", "createdAt"} {
		if _, ok := main.Record.Properties[field]; !ok {
			t.Fatalf("profile Lexicon is missing %q", field)
		}
	}
}
