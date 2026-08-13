package lexicons

import (
	"encoding/json"
	"testing"
)

func TestOrganizationLexicons(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		file     string
		id       string
		required []string
	}{
		{name: "organization root", file: "dev.adenosine.organization.json", id: "dev.adenosine.organization", required: []string{"slug", "name", "createdAt", "updatedAt"}},
		{name: "owner grant", file: "dev.adenosine.organizationGrant.json", id: "dev.adenosine.organizationGrant", required: []string{"organization", "subject", "role", "authority", "createdAt"}},
		{name: "member acceptance", file: "dev.adenosine.organizationMembership.json", id: "dev.adenosine.organizationMembership", required: []string{"organization", "grant", "visibility", "createdAt", "updatedAt"}},
		{name: "owner revocation", file: "dev.adenosine.organizationRevocation.json", id: "dev.adenosine.organizationRevocation", required: []string{"organization", "grant", "subject", "authority", "createdAt"}},
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
						Required []string `json:"required"`
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
			for _, required := range testCase.required {
				found := false
				for _, field := range main.Record.Required {
					found = found || field == required
				}
				if !found {
					t.Fatalf("required fields %q do not contain %q", main.Record.Required, required)
				}
			}
		})
	}
}
