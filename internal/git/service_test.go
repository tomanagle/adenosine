package git

import (
	"bytes"
	"context"
	"crypto/sha1" // Git SHA-1 pack fixture.
	encodingbinary "encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/google/uuid"
)

func TestServiceInitializesRealBareRepository(t *testing.T) {
	testCases := []struct{ name string }{{name: "initializes and serves normal refs while hiding controlled refs"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			binary, err := exec.LookPath("git")
			if err != nil {
				t.Skip("git executable is unavailable")
			}
			paths, err := storage.NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatalf("create filesystem storage: %v", err)
			}
			service := NewService(NewRunner(binary), paths)
			id := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))

			if err := service.Init(context.Background(), id, "main"); err != nil {
				t.Fatalf("initialize bare repository: %v", err)
			}
			exists, err := paths.Exists(context.Background(), id)
			if err != nil {
				t.Fatalf("check repository existence: %v", err)
			}
			if !exists {
				t.Fatal("bare repository was not created")
			}
			path, err := paths.Path(context.Background(), id)
			if err != nil {
				t.Fatalf("resolve repository path: %v", err)
			}
			hashCommand := exec.Command(binary, "--git-dir="+path, "hash-object", "-w", "--stdin")
			hashCommand.Stdin = bytes.NewBufferString("hello\n")
			hashOutput, err := hashCommand.Output()
			if err != nil {
				t.Fatalf("create Git object: %v", err)
			}
			sha := string(bytes.TrimSpace(hashOutput))
			if output, err := exec.Command(binary, "--git-dir="+path, "update-ref", "refs/tags/blob", sha).CombinedOutput(); err != nil {
				t.Fatalf("create ref: %v: %s", err, output)
			}
			if output, err := exec.Command(binary, "--git-dir="+path, "update-ref", "refs/adenosine/pull/1/head", sha).CombinedOutput(); err != nil {
				t.Fatalf("create controlled ref: %v: %s", err, output)
			}

			refs, err := service.Refs(context.Background(), id)
			if err != nil {
				t.Fatalf("list refs: %v", err)
			}
			if len(refs) != 2 {
				t.Fatalf("refs = %+v, want two refs", refs)
			}
			var advertisement bytes.Buffer
			if err := service.UploadPack(context.Background(), id, nil, &advertisement, PackOptions{AdvertiseRefs: true}); err != nil {
				t.Fatalf("advertise upload-pack refs: %v", err)
			}
			if !bytes.Contains(advertisement.Bytes(), []byte("refs/tags/blob")) {
				t.Fatalf("upload-pack advertisement does not contain blob tag: %q", advertisement.Bytes())
			}
			if bytes.Contains(advertisement.Bytes(), []byte("refs/adenosine/")) {
				t.Fatalf("upload-pack advertisement exposes controlled ref: %q", advertisement.Bytes())
			}
			var receiveAdvertisement bytes.Buffer
			if err := service.ReceivePack(context.Background(), id, nil, &receiveAdvertisement, PackOptions{AdvertiseRefs: true}); err != nil {
				t.Fatalf("advertise receive-pack refs: %v", err)
			}
			if bytes.Contains(receiveAdvertisement.Bytes(), []byte("refs/adenosine/")) {
				t.Fatalf("receive-pack advertisement exposes controlled ref: %q", receiveAdvertisement.Bytes())
			}
			if !bytes.Contains(receiveAdvertisement.Bytes(), []byte("refs/tags/blob")) {
				t.Fatalf("receive-pack advertisement hides normal ref: %q", receiveAdvertisement.Bytes())
			}
			payload := strings.Repeat("0", len(sha)) + " " + sha + " refs/adenosine/pull/2/head\x00report-status\n"
			pack := []byte("PACK")
			pack = encodingbinary.BigEndian.AppendUint32(pack, 2)
			pack = encodingbinary.BigEndian.AppendUint32(pack, 0)
			checksum := sha1.Sum(pack)
			pack = append(pack, checksum[:]...)
			input := bytes.NewBufferString(fmt.Sprintf("%04x%s0000", len(payload)+4, payload))
			_, _ = input.Write(pack)
			var receiveOutput bytes.Buffer
			if err := service.ReceivePackSession(context.Background(), id, input, &receiveOutput, ""); err != nil {
				t.Fatalf("attempt controlled ref update: %v", err)
			}
			if !bytes.Contains(receiveOutput.Bytes(), []byte("deny updating a hidden ref")) {
				t.Fatalf("receive-pack did not reject controlled ref update: %q", receiveOutput.Bytes())
			}
			if err := exec.Command(binary, "--git-dir="+path, "show-ref", "--verify", "--quiet", "refs/adenosine/pull/2/head").Run(); err == nil {
				t.Fatal("receive-pack created controlled ref")
			}

			command := exec.Command(binary, "--git-dir="+path, "rev-parse", "--is-bare-repository")
			output, err := command.Output()
			if err != nil {
				t.Fatalf("inspect bare repository: %v", err)
			}
			if string(output) != "true\n" {
				t.Fatalf("is-bare-repository = %q, want true", output)
			}
		})
	}
}
