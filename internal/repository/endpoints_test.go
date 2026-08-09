package repository

import (
	"testing"

	"github.com/google/uuid"
)

func TestEndpointsUseDIDRoutesAndConfiguredHosts(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		port    uint16
		wantSSH string
	}{
		{name: "default SSH port", port: 22, wantSSH: "ssh://git@ssh.example/did:plc:alice/project.git"},
		{name: "custom SSH port", port: 2222, wantSSH: "ssh://git@ssh.example:2222/did:plc:alice/project.git"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			endpoints, err := buildEndpoints("https://code.example/base", "ssh.example", testCase.port)
			if err != nil {
				t.Fatal(err)
			}
			repo := Repository{ID: ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1")), OwnerDID: "did:plc:alice", Slug: "project"}
			web, https, ssh := endpoints.For(repo)
			if web != "https://code.example/base/did:plc:alice/project" || https != web+".git" || ssh != testCase.wantSSH {
				t.Fatalf("endpoints = %q, %q, %q", web, https, ssh)
			}
		})
	}
}
