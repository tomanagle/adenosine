package lexicons

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPullRequestLexicons(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		file       string
		id         string
		required   []string
		properties []string
	}{
		{name: "pull request", file: "dev.adenosine.pullRequest.json", id: "dev.adenosine.pullRequest", required: []string{"sourceRepository", "targetRepository", "sourceBranch", "targetBranch", "headSHA", "title", "body", "createdAt", "updatedAt"}, properties: []string{"body", "createdAt", "headSHA", "sourceBranch", "sourceRepository", "targetBranch", "targetRepository", "title", "updatedAt"}},
		{name: "pull request status", file: "dev.adenosine.pullRequestStatus.json", id: "dev.adenosine.pullRequestStatus", required: []string{"subject", "targetRepository", "state", "createdAt", "updatedAt"}, properties: []string{"createdAt", "mergeCommitSHA", "state", "subject", "targetRepository", "updatedAt"}},
		{name: "pull request review", file: "dev.adenosine.pullRequestReview.json", id: "dev.adenosine.pullRequestReview", required: []string{"subject", "verdict", "body", "createdAt", "updatedAt"}, properties: []string{"body", "createdAt", "subject", "updatedAt", "verdict"}},
		{name: "pull request review request", file: "dev.adenosine.pullRequestReviewRequest.json", id: "dev.adenosine.pullRequestReviewRequest", required: []string{"subject", "targetRepository", "reviewer", "requestedBy", "createdAt", "updatedAt"}, properties: []string{"createdAt", "requestedBy", "reviewer", "subject", "targetRepository", "updatedAt"}},
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
			gotProperties := make([]string, 0, len(main.Record.Properties))
			for _, property := range testCase.properties {
				if _, ok := main.Record.Properties[property]; ok {
					gotProperties = append(gotProperties, property)
				}
			}
			if !reflect.DeepEqual(gotProperties, testCase.properties) || len(main.Record.Properties) != len(testCase.properties) {
				t.Fatalf("properties = %v, want exactly %v", gotProperties, testCase.properties)
			}
		})
	}
}

func TestPullRequestLexiconConstraints(t *testing.T) {
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
		{name: "source repository strong ref", file: "dev.adenosine.pullRequest.json", field: "sourceRepository", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "target repository strong ref", file: "dev.adenosine.pullRequest.json", field: "targetRepository", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "source branch bounds", file: "dev.adenosine.pullRequest.json", field: "sourceBranch", want: property{Type: "string", MinLength: 1, MaxLength: 255}},
		{name: "target branch bounds", file: "dev.adenosine.pullRequest.json", field: "targetBranch", want: property{Type: "string", MinLength: 1, MaxLength: 255}},
		{name: "head SHA bounds", file: "dev.adenosine.pullRequest.json", field: "headSHA", want: property{Type: "string", MinLength: 40, MaxLength: 64}},
		{name: "title bounds", file: "dev.adenosine.pullRequest.json", field: "title", want: property{Type: "string", MinLength: 1, MaxLength: 255}},
		{name: "pull request body bound", file: "dev.adenosine.pullRequest.json", field: "body", want: property{Type: "string", MaxLength: 65535}},
		{name: "status subject strong ref", file: "dev.adenosine.pullRequestStatus.json", field: "subject", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "status target strong ref", file: "dev.adenosine.pullRequestStatus.json", field: "targetRepository", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "status states", file: "dev.adenosine.pullRequestStatus.json", field: "state", want: property{Type: "string", KnownValues: []string{"open", "closed", "merged"}}},
		{name: "merge SHA bounds", file: "dev.adenosine.pullRequestStatus.json", field: "mergeCommitSHA", want: property{Type: "string", MinLength: 40, MaxLength: 64}},
		{name: "review subject strong ref", file: "dev.adenosine.pullRequestReview.json", field: "subject", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "review verdicts", file: "dev.adenosine.pullRequestReview.json", field: "verdict", want: property{Type: "string", KnownValues: []string{"comment", "approve", "request_changes"}}},
		{name: "review body bound", file: "dev.adenosine.pullRequestReview.json", field: "body", want: property{Type: "string", MaxLength: 65535}},
		{name: "created timestamp", file: "dev.adenosine.pullRequestReview.json", field: "createdAt", want: property{Type: "string", Format: "datetime"}},
		{name: "updated timestamp", file: "dev.adenosine.pullRequestReview.json", field: "updatedAt", want: property{Type: "string", Format: "datetime"}},
		{name: "review request subject", file: "dev.adenosine.pullRequestReviewRequest.json", field: "subject", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "review request target", file: "dev.adenosine.pullRequestReviewRequest.json", field: "targetRepository", want: property{Type: "ref", Ref: "com.atproto.repo.strongRef"}},
		{name: "reviewer DID", file: "dev.adenosine.pullRequestReviewRequest.json", field: "reviewer", want: property{Type: "string", Format: "did"}},
		{name: "requester DID", file: "dev.adenosine.pullRequestReviewRequest.json", field: "requestedBy", want: property{Type: "string", Format: "did"}},
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
