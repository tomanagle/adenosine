package gitssh

import (
	"fmt"
	"strings"
)

type command struct {
	operation string
	owner     string
	slug      string
}

func parseCommand(value string) (command, error) {
	var operation string
	switch {
	case strings.HasPrefix(value, "git-upload-pack '"):
		operation = "upload-pack"
		value = strings.TrimPrefix(value, "git-upload-pack '")
	case strings.HasPrefix(value, "git-receive-pack '"):
		operation = "receive-pack"
		value = strings.TrimPrefix(value, "git-receive-pack '")
	default:
		return command{}, fmt.Errorf("unsupported SSH command")
	}
	if !strings.HasSuffix(value, "'") {
		return command{}, fmt.Errorf("repository path must be single quoted")
	}
	path := strings.TrimPrefix(strings.TrimSuffix(value, "'"), "/")
	if strings.ContainsAny(path, "'\\\x00\r\n\t") {
		return command{}, fmt.Errorf("repository path contains unsupported characters")
	}
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return command{}, fmt.Errorf("repository path must contain an owner and repository")
	}
	if !strings.HasSuffix(parts[1], ".git") {
		return command{}, fmt.Errorf("repository path must end in .git")
	}
	slug := strings.TrimSuffix(parts[1], ".git")
	if slug == "" || slug == "." || slug == ".." {
		return command{}, fmt.Errorf("repository slug is invalid")
	}
	for _, character := range slug {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("._-", character) {
			return command{}, fmt.Errorf("repository slug is invalid")
		}
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		if slug[0] < '0' || slug[0] > '9' {
			return command{}, fmt.Errorf("repository slug is invalid")
		}
	}
	return command{operation: operation, owner: parts[0], slug: slug}, nil
}
