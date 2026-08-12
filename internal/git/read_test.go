package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

func TestBranchesAndTags(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "branches and tags"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newReadFixture(t)

			branches, err := fixture.service.Branches(context.Background(), fixture.id, "main")
			if err != nil {
				t.Fatalf("list branches: %v", err)
			}
			if len(branches) != 2 {
				t.Fatalf("branches = %+v, want two", branches)
			}
			byName := make(map[string]Branch, len(branches))
			for _, branch := range branches {
				byName[branch.Name] = branch
			}
			if branch := byName["main"]; branch.SHA != fixture.commitSHA || !branch.Default {
				t.Errorf("main branch = %+v", branch)
			}
			if branch := byName["feature/slash"]; branch.SHA != fixture.commitSHA || branch.Default {
				t.Errorf("feature/slash branch = %+v", branch)
			}

			tags, err := fixture.service.Tags(context.Background(), fixture.id)
			if err != nil {
				t.Fatalf("list tags: %v", err)
			}
			if len(tags) != 2 {
				t.Fatalf("tags = %+v, want two", tags)
			}
			tagsByName := make(map[string]Tag, len(tags))
			for _, tag := range tags {
				tagsByName[tag.Name] = tag
			}
			lightweight := tagsByName["lightweight"]
			if lightweight.SHA != fixture.commitSHA || lightweight.ObjectType != "commit" || lightweight.PeeledSHA != fixture.commitSHA || lightweight.PeeledType != "commit" {
				t.Errorf("lightweight tag = %+v", lightweight)
			}
			annotated := tagsByName["annotated"]
			if annotated.SHA == fixture.commitSHA || annotated.ObjectType != "tag" || annotated.PeeledSHA != fixture.commitSHA || annotated.PeeledType != "commit" {
				t.Errorf("annotated tag = %+v", annotated)
			}
		})
	}
}

func TestTreeRootAndNested(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "root and nested tree"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newReadFixture(t)

			root, err := fixture.service.Tree(context.Background(), fixture.id, "main", "")
			if err != nil {
				t.Fatalf("list root tree: %v", err)
			}
			if root.CommitSHA != fixture.commitSHA || root.Path != "" {
				t.Fatalf("root identity = %+v", root)
			}
			wantNames := []string{"docs", "README.md", "binary.dat", "dependency", "empty", "link"}
			if got := entryNames(root.Entries); fmt.Sprint(got) != fmt.Sprint(wantNames) {
				t.Fatalf("root names = %v, want %v", got, wantNames)
			}
			entries := make(map[string]TreeEntry, len(root.Entries))
			for _, entry := range root.Entries {
				entries[entry.Name] = entry
			}
			if entry := entries["docs"]; entry.Type != "tree" || entry.Size != -1 || entry.Path != "docs" {
				t.Errorf("docs entry = %+v", entry)
			}
			if entry := entries["dependency"]; entry.Type != "commit" || entry.Mode != "160000" || entry.Size != -1 {
				t.Errorf("gitlink entry = %+v", entry)
			}
			if entry := entries["README.md"]; entry.Type != "blob" || entry.Size != int64(len(fixture.text)) {
				t.Errorf("README entry = %+v", entry)
			}
			if entry := entries["link"]; entry.Type != "blob" || entry.Mode != "120000" {
				t.Errorf("symlink entry = %+v", entry)
			}

			nested, err := fixture.service.Tree(context.Background(), fixture.id, fixture.commitSHA, "docs")
			if err != nil {
				t.Fatalf("list nested tree: %v", err)
			}
			if nested.CommitSHA != fixture.commitSHA || nested.Path != "docs" || len(nested.Entries) != 1 {
				t.Fatalf("nested tree = %+v", nested)
			}
			if entry := nested.Entries[0]; entry.Name != "guide.txt" || entry.Path != "docs/guide.txt" || entry.Size != int64(len(fixture.nested)) {
				t.Errorf("nested entry = %+v", entry)
			}
		})
	}
}

func TestBlobMetadataAndStreaming(t *testing.T) {
	t.Parallel()
	fixture := newReadFixture(t)
	testCases := []struct {
		name string
		sha  string
		want []byte
	}{
		{name: "text", sha: fixture.textSHA, want: fixture.text},
		{name: "binary", sha: fixture.binarySHA, want: fixture.binaryData},
		{name: "empty", sha: fixture.emptySHA, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			metadata, err := fixture.service.BlobMetadata(context.Background(), fixture.id, testCase.sha)
			if err != nil {
				t.Fatalf("blob metadata: %v", err)
			}
			if metadata.SHA != testCase.sha || metadata.Type != "blob" || metadata.Size != int64(len(testCase.want)) {
				t.Errorf("metadata = %+v", metadata)
			}
			var output bytes.Buffer
			if err := fixture.service.StreamBlob(context.Background(), fixture.id, testCase.sha, &output); err != nil {
				t.Fatalf("stream blob: %v", err)
			}
			if !bytes.Equal(output.Bytes(), testCase.want) {
				t.Errorf("streamed bytes = %v, want %v", output.Bytes(), testCase.want)
			}
		})
	}
}

func TestReadInputAndObjectErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "input and object errors"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newReadFixture(t)
			ctx := context.Background()

			invalidRevisions := []string{"", "-main", "main\x00other", "main\nother", strings.Repeat("a", maxRevisionLength+1)}
			for _, revision := range invalidRevisions {
				if _, err := fixture.service.Tree(ctx, fixture.id, revision, ""); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Tree revision %q error = %v, want ErrInvalidInput", revision, err)
				}
			}
			invalidPaths := []string{"/docs", "docs/", "docs//file", ".", "..", "docs/../file", "docs\\file", "docs\x00file", string([]byte{0xff})}
			for _, treePath := range invalidPaths {
				if _, err := fixture.service.Tree(ctx, fixture.id, "main", treePath); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("Tree path %q error = %v, want ErrInvalidInput", treePath, err)
				}
			}
			invalidSHAs := []string{"", fixture.textSHA[:12], strings.ToUpper(fixture.textSHA), strings.Repeat("g", 40), strings.Repeat("a", 41)}
			for _, sha := range invalidSHAs {
				if _, err := fixture.service.BlobMetadata(ctx, fixture.id, sha); !errors.Is(err, ErrInvalidInput) {
					t.Errorf("BlobMetadata SHA %q error = %v, want ErrInvalidInput", sha, err)
				}
			}
			if err := validateBlobSHA(strings.Repeat("a", 64)); err != nil {
				t.Errorf("64-character SHA rejected: %v", err)
			}
			if _, err := fixture.service.Tree(ctx, fixture.id, "missing", ""); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing revision error = %v, want ErrObjectNotFound", err)
			}
			if _, err := fixture.service.Tree(ctx, fixture.id, "main", "missing"); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing path error = %v, want ErrObjectNotFound", err)
			}
			if _, err := fixture.service.Tree(ctx, fixture.id, "main", "README.md"); !errors.Is(err, ErrUnsupportedObject) {
				t.Errorf("blob tree path error = %v, want ErrUnsupportedObject", err)
			}
			if _, err := fixture.service.Tree(ctx, fixture.id, fixture.textSHA, ""); !errors.Is(err, ErrUnsupportedObject) {
				t.Errorf("blob revision error = %v, want ErrUnsupportedObject", err)
			}
			if _, err := fixture.service.BlobMetadata(ctx, fixture.id, strings.Repeat("0", 40)); !errors.Is(err, ErrObjectNotFound) {
				t.Errorf("missing blob error = %v, want ErrObjectNotFound", err)
			}
			if _, err := fixture.service.BlobMetadata(ctx, fixture.id, fixture.treeSHA); !errors.Is(err, ErrUnsupportedObject) {
				t.Errorf("tree metadata error = %v, want ErrUnsupportedObject", err)
			}
			if err := fixture.service.StreamBlob(ctx, fixture.id, fixture.textSHA, nil); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("nil stream output error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestTreeRejectsInvalidUTF8Name(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "invalid UTF-8 name"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newReadFixture(t)
			invalidTree := fixture.gitInput(t, []byte("100644 blob "+fixture.textSHA+"\tbad\xffname\x00"), "mktree", "-z")
			invalidCommit := fixture.gitInput(t, nil, "commit-tree", invalidTree, "-m", "invalid name")

			if _, err := fixture.service.Tree(context.Background(), fixture.id, invalidCommit, ""); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid UTF-8 tree error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestBoundedOutputAndEntryCount(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "bounded output and entry count"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			buffer := &limitedBuffer{limit: 3}
			if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 || !buffer.truncated || buffer.String() != "abc" {
				t.Fatalf("limited buffer = {written:%d err:%v truncated:%v value:%q}", written, err, buffer.truncated, buffer.String())
			}
			record := []byte("100644 blob " + strings.Repeat("a", 40) + " 1\tfile\x00")
			output := bytes.Repeat(record, maxTreeEntries+1)
			if _, err := parseTreeEntries(output, ""); !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("entry limit error = %v, want ErrOutputLimit", err)
			}
		})
	}
}

type readFixture struct {
	t          *testing.T
	binary     string
	repository string
	service    *Service
	id         repository.ID
	commitSHA  string
	treeSHA    string
	textSHA    string
	binarySHA  string
	emptySHA   string
	text       []byte
	binaryData []byte
	nested     []byte
}

func newReadFixture(t *testing.T) *readFixture {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	paths, err := storage.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatalf("create filesystem storage: %v", err)
	}
	id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af2"))
	service := NewService(NewRunner(binary), paths)
	if err := service.Init(context.Background(), id, "main"); err != nil {
		t.Fatalf("initialize repository: %v", err)
	}
	repositoryPath, err := paths.Path(context.Background(), id)
	if err != nil {
		t.Fatalf("resolve repository path: %v", err)
	}
	fixture := &readFixture{
		t:          t,
		binary:     binary,
		repository: repositoryPath,
		service:    service,
		id:         id,
		text:       []byte("hello, tree\n"),
		binaryData: []byte{0, 1, 2, 0xff, '\n', 0},
		nested:     []byte("nested document\n"),
	}
	fixture.textSHA = fixture.gitInput(t, fixture.text, "hash-object", "-w", "--stdin")
	fixture.binarySHA = fixture.gitInput(t, fixture.binaryData, "hash-object", "-w", "--stdin")
	fixture.emptySHA = fixture.gitInput(t, nil, "hash-object", "-w", "--stdin")
	nestedSHA := fixture.gitInput(t, fixture.nested, "hash-object", "-w", "--stdin")
	linkSHA := fixture.gitInput(t, []byte("README.md"), "hash-object", "-w", "--stdin")
	nestedTree := fixture.gitInput(t, []byte("100644 blob "+nestedSHA+"\tguide.txt\x00"), "mktree", "-z")
	dependencyTree := fixture.gitInput(t, nil, "mktree", "-z")
	dependencyCommit := fixture.gitInput(t, nil, "commit-tree", dependencyTree, "-m", "dependency")
	rootInput := []byte(
		"040000 tree " + nestedTree + "\tdocs\x00" +
			"160000 commit " + dependencyCommit + "\tdependency\x00" +
			"100644 blob " + fixture.textSHA + "\tREADME.md\x00" +
			"100644 blob " + fixture.binarySHA + "\tbinary.dat\x00" +
			"100644 blob " + fixture.emptySHA + "\tempty\x00" +
			"120000 blob " + linkSHA + "\tlink\x00",
	)
	fixture.treeSHA = fixture.gitInput(t, rootInput, "mktree", "-z")
	fixture.commitSHA = fixture.gitInput(t, nil, "commit-tree", fixture.treeSHA, "-m", "initial")
	fixture.git(t, "update-ref", "refs/heads/main", fixture.commitSHA)
	fixture.git(t, "update-ref", "refs/heads/feature/slash", fixture.commitSHA)
	fixture.git(t, "update-ref", "refs/tags/lightweight", fixture.commitSHA)
	fixture.git(t, "tag", "-a", "annotated", "-m", "annotated tag", fixture.commitSHA)
	return fixture
}

func (fixture *readFixture) git(t *testing.T, arguments ...string) string {
	t.Helper()
	return fixture.gitInput(t, nil, arguments...)
}

func (fixture *readFixture) gitInput(t *testing.T, input []byte, arguments ...string) string {
	t.Helper()
	args := append([]string{"-c", "commit.gpgSign=false", "-c", "tag.gpgSign=false", "--git-dir=" + fixture.repository}, arguments...)
	command := exec.Command(fixture.binary, args...)
	command.Stdin = bytes.NewReader(input)
	command.Env = append(command.Environ(),
		"GIT_AUTHOR_NAME=Adenosine Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Adenosine Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func entryNames(entries []TreeEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name
	}
	return names
}
