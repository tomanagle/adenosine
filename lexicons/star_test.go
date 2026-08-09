package lexicons

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStarLexicon(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "strong reference record"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := Files.ReadFile("dev.adenosine.star.json")
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Lexicon int    `json:"lexicon"`
				ID      string `json:"id"`
				Defs    map[string]struct {
					Type   string `json:"type"`
					Key    string `json:"key"`
					Record struct {
						Required   []string `json:"required"`
						Properties map[string]struct {
							Type   string `json:"type"`
							Ref    string `json:"ref"`
							Format string `json:"format"`
						} `json:"properties"`
					} `json:"record"`
				} `json:"defs"`
			}
			if err := json.Unmarshal(contents, &schema); err != nil {
				t.Fatal(err)
			}
			main := schema.Defs["main"]
			if schema.Lexicon != 1 || schema.ID != "dev.adenosine.star" || main.Type != "record" || main.Key != "any" {
				t.Fatalf("schema identity = %#v", schema)
			}
			if !reflect.DeepEqual(main.Record.Required, []string{"subject", "createdAt"}) {
				t.Fatalf("required fields = %v", main.Record.Required)
			}
			if subject := main.Record.Properties["subject"]; subject.Type != "ref" || subject.Ref != "com.atproto.repo.strongRef" {
				t.Fatalf("subject = %#v", subject)
			}
			if createdAt := main.Record.Properties["createdAt"]; createdAt.Type != "string" || createdAt.Format != "datetime" {
				t.Fatalf("createdAt = %#v", createdAt)
			}
		})
	}
}
