package gitssh

import "testing"

func TestParseCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		value     string
		operation string
		owner     string
		slug      string
		valid     bool
	}{
		{name: "upload SCP path", value: "git-upload-pack 'alice/project.git'", operation: "upload-pack", owner: "alice", slug: "project", valid: true},
		{name: "receive URI path", value: "git-receive-pack '/alice/project.git'", operation: "receive-pack", owner: "alice", slug: "project", valid: true},
		{name: "DID owner", value: "git-upload-pack 'did:plc:alice/project.git'", operation: "upload-pack", owner: "did:plc:alice", slug: "project", valid: true},
		{name: "shell", value: "sh -c 'git-upload-pack alice/project.git'"},
		{name: "extra argument", value: "git-upload-pack 'alice/project.git' --strict"},
		{name: "unquoted", value: "git-upload-pack alice/project.git"},
		{name: "nested path", value: "git-upload-pack 'groups/alice/project.git'"},
		{name: "traversal", value: "git-upload-pack '../project.git'"},
		{name: "missing suffix", value: "git-upload-pack 'alice/project'"},
		{name: "uppercase slug", value: "git-upload-pack 'alice/Project.git'"},
		{name: "metacharacters", value: "git-upload-pack 'alice/project.git;id'"},
		{name: "newline", value: "git-upload-pack 'alice/project.git\n'"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseCommand(test.value)
			if !test.valid {
				if err == nil {
					t.Fatalf("parseCommand(%q) unexpectedly succeeded", test.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommand(%q): %v", test.value, err)
			}
			if got.operation != test.operation || got.owner != test.owner || got.slug != test.slug {
				t.Fatalf("command = %#v", got)
			}
		})
	}
}
