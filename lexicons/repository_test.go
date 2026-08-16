package lexicons

import (
	"encoding/json"
	"testing"
)

func TestRepositoryLexicon(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		file   string
		id     string
		fields []string
	}{
		{name: "portable repository record", file: "dev.adenosine.repo.json", id: "dev.adenosine.repo", fields: []string{"slug", "name", "description", "defaultBranch", "git", "web", "forkedFrom", "transferredFrom", "transferredTo", "createdAt", "updatedAt"}},
		{name: "portable repository transfer proposal", file: "dev.adenosine.repositoryTransfer.json", id: "dev.adenosine.repositoryTransfer", fields: []string{"repository", "destinationDID", "destinationOrganization", "destinationOwner", "createdAt", "expiresAt"}},
		{name: "portable repository transfer acceptance", file: "dev.adenosine.repositoryTransferAcceptance.json", id: "dev.adenosine.repositoryTransferAcceptance", fields: []string{"proposal", "repository", "createdAt"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := Files.ReadFile(testCase.file)
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
						Required   []string                   `json:"required"`
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"record"`
				} `json:"defs"`
			}
			if err := json.Unmarshal(contents, &schema); err != nil {
				t.Fatal(err)
			}
			main := schema.Defs["main"]
			if schema.Lexicon != 1 || schema.ID != testCase.id || main.Type != "record" || main.Key != "any" {
				t.Fatalf("schema identity = %#v", schema)
			}
			if len(main.Record.Required) == 0 {
				t.Fatalf("record has no required fields: %#v", main.Record)
			}
			for _, field := range testCase.fields {
				if _, ok := main.Record.Properties[field]; !ok {
					t.Fatalf("missing field %q", field)
				}
			}
		})
	}
}
