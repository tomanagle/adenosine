package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

const (
	maxRevisionLength = 1024
	maxTreePathLength = 4096
	maxRefOutput      = 8 * 1024 * 1024
	maxTreeOutput     = 32 * 1024 * 1024
	maxMetadataOutput = 4 * 1024
	maxTreeEntries    = 100000
)

var (
	// ErrInvalidInput indicates that a revision, path, or object ID is unsafe or malformed.
	ErrInvalidInput = errors.New("invalid Git read input")
	// ErrObjectNotFound indicates that an object or revision does not exist in the repository.
	ErrObjectNotFound = errors.New("Git object or revision not found")
	// ErrUnsupportedObject indicates that an object has the wrong type for the requested operation.
	ErrUnsupportedObject = errors.New("unsupported Git object")
	// ErrOutputLimit indicates that bounded Git command output exceeded its limit.
	ErrOutputLimit = errors.New("Git command output limit exceeded")
)

// Branch is a branch head from the repository's refs/heads namespace.
type Branch struct {
	Name    string
	SHA     string
	Default bool
}

// Tag describes a lightweight or annotated tag and its peeled target.
type Tag struct {
	Name       string
	SHA        string
	ObjectType string
	PeeledSHA  string
	PeeledType string
}

// Tree is one directory listing resolved from a commit revision.
type Tree struct {
	CommitSHA string
	Path      string
	Entries   []TreeEntry
}

// TreeEntry is one immediate child of a tree. Size is -1 for trees and commits.
type TreeEntry struct {
	Mode string
	Type string
	SHA  string
	Size int64
	Name string
	Path string
}

// BlobMetadata describes a blob before its bytes are streamed.
type BlobMetadata struct {
	SHA  string
	Type string
	Size int64
}

// Branches lists branch heads and marks the configured default branch.
func (service *Service) Branches(ctx context.Context, id repository.ID, defaultBranch string) ([]Branch, error) {
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return nil, err
	}
	output, err := service.runBounded(ctx, maxRefOutput, []string{
		"--git-dir=" + repositoryPath,
		"for-each-ref",
		"--format=%(objectname)%00%(refname:strip=2)",
		"refs/heads/",
	})
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	records, err := parseLineRecords(output, 2)
	if err != nil {
		return nil, fmt.Errorf("parse branches: %w", err)
	}
	branches := make([]Branch, 0, len(records))
	for _, fields := range records {
		if !utf8.Valid(fields[1]) {
			return nil, fmt.Errorf("parse branches: invalid UTF-8 name")
		}
		name := string(fields[1])
		branches = append(branches, Branch{Name: name, SHA: string(fields[0]), Default: name == defaultBranch})
	}
	return branches, nil
}

// Tags lists tags and peels annotated tags to their target objects.
func (service *Service) Tags(ctx context.Context, id repository.ID) ([]Tag, error) {
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return nil, err
	}
	output, err := service.runBounded(ctx, maxRefOutput, []string{
		"--git-dir=" + repositoryPath,
		"for-each-ref",
		"--format=%(objectname)%00%(objecttype)%00%(*objectname)%00%(*objecttype)%00%(refname:strip=2)",
		"refs/tags/",
	})
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	records, err := parseLineRecords(output, 5)
	if err != nil {
		return nil, fmt.Errorf("parse tags: %w", err)
	}
	tags := make([]Tag, 0, len(records))
	for _, fields := range records {
		if !utf8.Valid(fields[4]) {
			return nil, fmt.Errorf("parse tags: invalid UTF-8 name")
		}
		tag := Tag{
			SHA:        string(fields[0]),
			ObjectType: string(fields[1]),
			PeeledSHA:  string(fields[2]),
			PeeledType: string(fields[3]),
			Name:       string(fields[4]),
		}
		if tag.PeeledSHA == "" {
			tag.PeeledSHA = tag.SHA
			tag.PeeledType = tag.ObjectType
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// Tree lists the immediate entries at a repository-relative path for a commit revision.
func (service *Service) Tree(ctx context.Context, id repository.ID, revision, treePath string) (Tree, error) {
	if err := validateRevision(revision); err != nil {
		return Tree{}, err
	}
	if err := validateTreePath(treePath); err != nil {
		return Tree{}, err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return Tree{}, err
	}
	commitSHA, err := service.resolve(ctx, repositoryPath, revision+"^{commit}")
	if err != nil {
		if errors.Is(err, ErrObjectNotFound) {
			objectSHA, objectErr := service.resolve(ctx, repositoryPath, revision+"^{object}")
			if objectErr == nil {
				objectType, typeErr := service.objectProperty(ctx, repositoryPath, "-t", objectSHA)
				if typeErr != nil {
					return Tree{}, fmt.Errorf("inspect revision: %w", typeErr)
				}
				return Tree{}, fmt.Errorf("revision resolves to %s: %w", objectType, ErrUnsupportedObject)
			}
		}
		return Tree{}, fmt.Errorf("resolve revision: %w", err)
	}
	treeish := commitSHA + "^{tree}"
	if treePath != "" {
		treeish = commitSHA + ":" + treePath
	}
	treeSHA, err := service.resolve(ctx, repositoryPath, treeish)
	if err != nil {
		return Tree{}, fmt.Errorf("resolve tree path: %w", err)
	}
	objectType, err := service.objectProperty(ctx, repositoryPath, "-t", treeSHA)
	if err != nil {
		return Tree{}, fmt.Errorf("inspect tree path: %w", err)
	}
	if objectType != "tree" {
		return Tree{}, fmt.Errorf("tree path resolves to %s: %w", objectType, ErrUnsupportedObject)
	}
	output, err := service.runBounded(ctx, maxTreeOutput, []string{
		"--git-dir=" + repositoryPath,
		"ls-tree",
		"-z",
		"-l",
		"--full-tree",
		treeSHA,
		"--",
	})
	if err != nil {
		return Tree{}, fmt.Errorf("list tree: %w", err)
	}
	entries, err := parseTreeEntries(output, treePath)
	if err != nil {
		return Tree{}, fmt.Errorf("parse tree: %w", err)
	}
	return Tree{CommitSHA: commitSHA, Path: treePath, Entries: entries}, nil
}

// BlobMetadata verifies that a full object ID identifies a blob and returns its size.
func (service *Service) BlobMetadata(ctx context.Context, id repository.ID, sha string) (BlobMetadata, error) {
	if err := validateBlobSHA(sha); err != nil {
		return BlobMetadata{}, err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return BlobMetadata{}, err
	}
	return service.blobMetadata(ctx, repositoryPath, sha)
}

// StreamBlob verifies a blob and streams its bytes directly to output.
func (service *Service) StreamBlob(ctx context.Context, id repository.ID, sha string, output io.Writer) error {
	if err := validateBlobSHA(sha); err != nil {
		return err
	}
	if output == nil {
		return fmt.Errorf("output writer must not be nil: %w", ErrInvalidInput)
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return err
	}
	metadata, err := service.blobMetadata(ctx, repositoryPath, sha)
	if err != nil {
		return err
	}
	counter := &countingWriter{writer: output}
	if err := service.runner.run(ctx, []string{
		"--git-dir=" + repositoryPath,
		"cat-file",
		"blob",
		sha,
	}, nil, counter); err != nil {
		return fmt.Errorf("stream blob: %w", err)
	}
	if counter.written != metadata.Size {
		return fmt.Errorf("stream blob: wrote %d bytes, expected %d", counter.written, metadata.Size)
	}
	return nil
}

func (service *Service) blobMetadata(ctx context.Context, repositoryPath, sha string) (BlobMetadata, error) {
	objectType, err := service.objectProperty(ctx, repositoryPath, "-t", sha)
	if err != nil {
		return BlobMetadata{}, fmt.Errorf("inspect blob type: %w", err)
	}
	if objectType != "blob" {
		return BlobMetadata{}, fmt.Errorf("object %s is %s: %w", sha, objectType, ErrUnsupportedObject)
	}
	sizeText, err := service.objectProperty(ctx, repositoryPath, "-s", sha)
	if err != nil {
		return BlobMetadata{}, fmt.Errorf("inspect blob size: %w", err)
	}
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size < 0 {
		return BlobMetadata{}, fmt.Errorf("parse blob size %q", sizeText)
	}
	return BlobMetadata{SHA: sha, Type: objectType, Size: size}, nil
}

func (service *Service) resolve(ctx context.Context, repositoryPath, revision string) (string, error) {
	output, err := service.runBounded(ctx, maxMetadataOutput, []string{
		"--git-dir=" + repositoryPath,
		"rev-parse",
		"--verify",
		"--end-of-options",
		revision,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, ErrOutputLimit) {
			return "", err
		}
		return "", ErrObjectNotFound
	}
	return singleLine(output)
}

func (service *Service) objectProperty(ctx context.Context, repositoryPath, option, sha string) (string, error) {
	output, err := service.runBounded(ctx, maxMetadataOutput, []string{
		"--git-dir=" + repositoryPath,
		"cat-file",
		option,
		sha,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(err, ErrOutputLimit) {
			return "", err
		}
		return "", ErrObjectNotFound
	}
	return singleLine(output)
}

func (service *Service) repositoryPath(ctx context.Context, id repository.ID) (string, error) {
	repositoryPath, err := service.paths.Path(ctx, id)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	return repositoryPath, nil
}

func (service *Service) runBounded(ctx context.Context, limit int, args []string) ([]byte, error) {
	output := &limitedBuffer{limit: limit}
	if err := service.runner.run(ctx, args, nil, output); err != nil {
		return nil, err
	}
	if output.truncated {
		return nil, ErrOutputLimit
	}
	return output.buffer.Bytes(), nil
}

func validateRevision(revision string) error {
	if revision == "" || len(revision) > maxRevisionLength || strings.HasPrefix(revision, "-") || !utf8.ValidString(revision) {
		return fmt.Errorf("invalid revision: %w", ErrInvalidInput)
	}
	for _, character := range revision {
		if character == 0 || character < 0x20 || character == 0x7f {
			return fmt.Errorf("invalid revision: %w", ErrInvalidInput)
		}
	}
	return nil
}

func validateTreePath(treePath string) error {
	if len(treePath) > maxTreePathLength || !utf8.ValidString(treePath) || strings.HasPrefix(treePath, "/") || strings.HasSuffix(treePath, "/") || strings.Contains(treePath, "\\") {
		return fmt.Errorf("invalid tree path: %w", ErrInvalidInput)
	}
	if treePath == "" {
		return nil
	}
	for _, component := range strings.Split(treePath, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("invalid tree path: %w", ErrInvalidInput)
		}
		for _, character := range component {
			if character == 0 || character < 0x20 || character == 0x7f {
				return fmt.Errorf("invalid tree path: %w", ErrInvalidInput)
			}
		}
	}
	return nil
}

func validateBlobSHA(sha string) error {
	if len(sha) != 40 && len(sha) != 64 {
		return fmt.Errorf("blob SHA must be a full 40- or 64-character object ID: %w", ErrInvalidInput)
	}
	for _, character := range sha {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return fmt.Errorf("blob SHA must be lowercase hexadecimal: %w", ErrInvalidInput)
			}
		}
	}
	return nil
}

func parseLineRecords(output []byte, fieldCount int) ([][][]byte, error) {
	if len(output) == 0 {
		return nil, nil
	}
	lines := bytes.Split(output, []byte{'\n'})
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	records := make([][][]byte, 0, len(lines))
	for _, line := range lines {
		fields := bytes.Split(line, []byte{0})
		if len(fields) != fieldCount {
			return nil, fmt.Errorf("unexpected field count %d", len(fields))
		}
		records = append(records, fields)
	}
	return records, nil
}

func parseTreeEntries(output []byte, parent string) ([]TreeEntry, error) {
	if len(output) == 0 {
		return []TreeEntry{}, nil
	}
	records := bytes.Split(output, []byte{0})
	if len(records[len(records)-1]) == 0 {
		records = records[:len(records)-1]
	}
	if len(records) > maxTreeEntries {
		return nil, ErrOutputLimit
	}
	entries := make([]TreeEntry, 0, len(records))
	for _, record := range records {
		tab := bytes.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("missing entry name")
		}
		header := strings.Fields(string(record[:tab]))
		if len(header) != 4 {
			return nil, fmt.Errorf("unexpected header field count %d", len(header))
		}
		nameBytes := record[tab+1:]
		if !utf8.Valid(nameBytes) {
			return nil, fmt.Errorf("invalid UTF-8 entry name: %w", ErrInvalidInput)
		}
		name := string(nameBytes)
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
			return nil, fmt.Errorf("invalid entry name: %w", ErrInvalidInput)
		}
		if header[1] != "tree" && header[1] != "blob" && header[1] != "commit" {
			return nil, fmt.Errorf("entry %q has type %s: %w", name, header[1], ErrUnsupportedObject)
		}
		size := int64(-1)
		if header[3] != "-" {
			parsed, err := strconv.ParseInt(header[3], 10, 64)
			if err != nil || parsed < 0 {
				return nil, fmt.Errorf("invalid size %q", header[3])
			}
			size = parsed
		}
		entries = append(entries, TreeEntry{
			Mode: header[0],
			Type: header[1],
			SHA:  header[2],
			Size: size,
			Name: name,
			Path: path.Join(parent, name),
		})
	}
	sort.Slice(entries, func(left, right int) bool {
		leftDirectory := entries[left].Type == "tree"
		rightDirectory := entries[right].Type == "tree"
		if leftDirectory != rightDirectory {
			return leftDirectory
		}
		return bytes.Compare([]byte(entries[left].Name), []byte(entries[right].Name)) < 0
	})
	return entries, nil
}

func singleLine(output []byte) (string, error) {
	line := strings.TrimSuffix(string(output), "\n")
	if line == "" || strings.Contains(line, "\n") {
		return "", fmt.Errorf("unexpected Git output")
	}
	return line, nil
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (writer *countingWriter) Write(value []byte) (int, error) {
	written, err := writer.writer.Write(value)
	writer.written += int64(written)
	return written, err
}
