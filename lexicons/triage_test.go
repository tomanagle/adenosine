package lexicons

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRepositoryTriageLexicons(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		file       string
		id         string
		required   []string
		properties []string
	}{
		{name: "label", file: "dev.adenosine.repositoryLabel.json", id: "dev.adenosine.repositoryLabel", required: []string{"repository", "name", "color", "description", "createdAt", "updatedAt"}, properties: []string{"color", "createdAt", "description", "name", "repository", "updatedAt"}},
		{name: "milestone", file: "dev.adenosine.repositoryMilestone.json", id: "dev.adenosine.repositoryMilestone", required: []string{"repository", "title", "description", "state", "createdAt", "updatedAt"}, properties: []string{"closedAt", "createdAt", "description", "dueAt", "repository", "state", "title", "updatedAt"}},
		{name: "subject snapshot", file: "dev.adenosine.subjectTriage.json", id: "dev.adenosine.subjectTriage", required: []string{"subject", "kind", "repository", "labels", "assignees", "createdAt", "updatedAt"}, properties: []string{"assignees", "createdAt", "kind", "labels", "milestone", "repository", "subject", "updatedAt"}},
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
						Type       string                     `json:"type"`
						Required   []string                   `json:"required"`
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"record"`
				} `json:"defs"`
			}
			if err := json.Unmarshal(contents, &schema); err != nil {
				t.Fatal(err)
			}
			main := schema.Defs["main"]
			if schema.Lexicon != 1 || schema.ID != testCase.id || len(schema.Defs) != 1 || main.Type != "record" || main.Key != "any" || main.Record.Type != "object" {
				t.Fatalf("schema identity = %#v", schema)
			}
			if !reflect.DeepEqual(main.Record.Required, testCase.required) {
				t.Fatalf("required fields = %v, want %v", main.Record.Required, testCase.required)
			}
			properties := make([]string, 0, len(main.Record.Properties))
			for _, property := range testCase.properties {
				if _, ok := main.Record.Properties[property]; ok {
					properties = append(properties, property)
				}
			}
			if !reflect.DeepEqual(properties, testCase.properties) || len(main.Record.Properties) != len(testCase.properties) {
				t.Fatalf("properties = %v, want exactly %v", properties, testCase.properties)
			}
		})
	}
}

func TestRepositoryTriageLexiconConstraints(t *testing.T) {
	t.Parallel()
	type property struct {
		Type        string   `json:"type"`
		Ref         string   `json:"ref"`
		Format      string   `json:"format"`
		MinLength   int      `json:"minLength"`
		MaxLength   int      `json:"maxLength"`
		KnownValues []string `json:"knownValues"`
	}
	testCases := []struct {
		name  string
		file  string
		field string
		want  property
	}{
		{name: "label repository strong ref", file: "dev.adenosine.repositoryLabel.json", field: "repository", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "label name bounds", file: "dev.adenosine.repositoryLabel.json", field: "name", want: property{Type: "string", MinLength: 1, MaxLength: 50}},
		{name: "label color is exactly six characters", file: "dev.adenosine.repositoryLabel.json", field: "color", want: property{Type: "string", MinLength: 6, MaxLength: 6}},
		{name: "milestone states", file: "dev.adenosine.repositoryMilestone.json", field: "state", want: property{Type: "string", KnownValues: []string{"open", "closed"}}},
		{name: "triage subject strong ref", file: "dev.adenosine.subjectTriage.json", field: "subject", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "triage kinds", file: "dev.adenosine.subjectTriage.json", field: "kind", want: property{Type: "string", KnownValues: []string{"issue", "pull_request"}}},
		{name: "triage milestone URI", file: "dev.adenosine.subjectTriage.json", field: "milestone", want: property{Type: "string", Format: "at-uri"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			contents, err := Files.ReadFile(testCase.file)
			if err != nil {
				t.Fatal(err)
			}
			var schema struct {
				Defs map[string]struct {
					Record struct {
						Properties map[string]property `json:"properties"`
					} `json:"record"`
				} `json:"defs"`
			}
			if err := json.Unmarshal(contents, &schema); err != nil {
				t.Fatal(err)
			}
			if got := schema.Defs["main"].Record.Properties[testCase.field]; !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("%s = %#v, want %#v", testCase.field, got, testCase.want)
			}
		})
	}
}
