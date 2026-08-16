package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/event"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/restapi"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
)

const (
	bobDID          = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	hostedDID       = "did:plc:cccccccccccccccccccccccc"
	commonDID       = "did:plc:ewvi7nxzyoun6zhxrhs64oiz"
	repositoryCID   = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	sourceURI       = "at://" + bobDID + "/dev.adenosine.repo/b-only"
	targetURI       = "at://" + hostedDID + "/dev.adenosine.repo/0198a8512a897ae2a370dc68883e3af5"
	pullRequestRKey = "0198a8512a897ae2a370dc68883e3af6"
	reviewRKey      = "0198a8512a897ae2a370dc68883e3af7"
	statusRKey      = "rylp3ai6qs7it4e7mxvgntnq746seyavrk22slbvrt4swgvgpg3q"
	sourceBranch    = "feature"
	targetBranch    = "main"
	headSHA         = "6f072a30c8d42d61fc35099dd8cc01e6d86d2c05"
	baseSHA         = "5e8f4658bd4277bfe9033c4562efba862b1a8466"
	targetRepoID    = "0198a851-2a89-7ae2-a370-dc68883e3af5"
	sourceGitHost   = "adenosine-b-tls"
	featurePath     = "FEDERATED.md"
	featureLine     = "Fetched from the federated feature branch on B."
	title           = "Federated pull request from Bob"
	body            = "Proposed from B's isolated feature branch into A's main branch."
	reviewBody      = "Common reviewer approves the exact federated change."
)

var (
	pullRequestURI = "at://" + bobDID + "/" + pullrequest.Collection + "/" + pullRequestRKey
	reviewURI      = "at://" + commonDID + "/" + pullrequest.ReviewCollection + "/" + reviewRKey
	createdAt      = time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	updatedAt      = time.Date(2026, 8, 9, 16, 1, 0, 0, time.UTC)
	mergeAt        = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	httpClient     = &http.Client{Timeout: 10 * time.Second}
)

type fixture struct {
	name  string
	event []byte
}

type sourceResolver struct{}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return mergeAt }

func (sourceResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if host != sourceGitHost {
		return nil, fmt.Errorf("test resolver refused unexpected host %q", host)
	}
	return net.DefaultResolver.LookupIP(ctx, network, host)
}

type projection struct {
	prURI, prCID, authorDID                                    string
	sourceURI, sourceCID, sourceBranch                         string
	targetURI, targetCID, targetBranch                         string
	headSHA, title, body, state                                string
	statusURI, statusCID, mergeCommitSHA                       string
	createdAt, updatedAt                                       time.Time
	reviewCount, repositoryCount, openRepositoryCount          int64
	reviewURI, reviewCID, reviewAuthor, reviewVerdict          string
	reviewBody, reviewSubjectURI, reviewSubjectCID             string
	authoritativeStatusAuthor, authoritativeStatusTarget       string
	authoritativeStatusCID, authoritativeStatusSubject         string
	authoritativeStatusSubjectCID, authoritativeTargetCID      string
	authoritativeStatusState, authoritativeMergeSHA            string
	authoritativeStatusCreatedAt, authoritativeStatusUpdatedAt time.Time
	maliciousRawAuthor, maliciousRawCID                        string
	maliciousStatusState, maliciousStatusAuthor                string
	statusCount, liveReviewCount, sourceRepositoryCount        int64
}

type projectionExpectation struct {
	state, mergeSHA string
}

type deterministicPublisher struct {
	git    *gitservice.Service
	status pullrequest.Status
	record []byte
}

func (*deterministicPublisher) CreatePullRequest(context.Context, string, string, pullrequest.Record) (pullrequest.PullRequest, error) {
	return pullrequest.PullRequest{}, fmt.Errorf("unexpected pull request publication")
}

func (*deterministicPublisher) CreatePullRequestReview(context.Context, string, string, pullrequest.ReviewRecord) (pullrequest.Review, error) {
	return pullrequest.Review{}, fmt.Errorf("unexpected pull request review publication")
}

func (publisher *deterministicPublisher) PutPullRequestStatus(ctx context.Context, author string, record pullrequest.StatusRecord) (pullrequest.Status, error) {
	prRecord, err := pullRequestRecord()
	if err != nil {
		return pullrequest.Status{}, err
	}
	commit, err := publisher.git.Commit(ctx, repository.ID(uuid.MustParse(targetRepoID)), "refs/heads/"+targetBranch)
	if err != nil {
		return pullrequest.Status{}, fmt.Errorf("resolve merged target in publisher: %w", err)
	}
	wantSubject := pullrequest.StrongRef{URI: pullRequestURI, CID: deterministicCID(prRecord)}
	wantTarget := pullrequest.StrongRef{URI: targetURI, CID: repositoryCID}
	if author != hostedDID || record.Subject != wantSubject || record.TargetRepository != wantTarget ||
		record.State != pullrequest.StateMerged || record.MergeCommitSHA != commit.SHA || record.MergeCommitSHA == "" ||
		!record.CreatedAt.Equal(createdAt) || !record.UpdatedAt.Equal(mergeAt) {
		return pullrequest.Status{}, fmt.Errorf("unexpected merged status publication by %q: %+v (target SHA %s)", author, record, commit.SHA)
	}
	wire, err := statusRecordAt(string(record.State), record.MergeCommitSHA, record.UpdatedAt)
	if err != nil {
		return pullrequest.Status{}, err
	}
	status := pullrequest.Status{
		URI: "at://" + hostedDID + "/" + pullrequest.StatusCollection + "/" + statusRKey,
		CID: deterministicCID(wire), AuthorDID: hostedDID, StatusRecord: record,
	}
	if err := status.Validate(); err != nil {
		return pullrequest.Status{}, fmt.Errorf("validate deterministic merged status: %w", err)
	}
	publisher.status, publisher.record = status, wire
	return status, nil
}

func main() {
	phase := flag.String("phase", "create", "pull request acceptance phase: create, fetch, merge, or final")
	flag.Parse()

	ctx := context.Background()
	var err error
	switch *phase {
	case "create":
		err = create(ctx)
	case "fetch":
		err = fetch(ctx)
	case "merge":
		err = merge(ctx)
	case "final":
		err = verifyDatabases(ctx, []databaseCase{{name: "B after A stops", url: requiredEnv("DATABASE_URL_B"), wantSourceRepositoryCount: 1}}, projectionExpectation{state: "merged"})
	default:
		err = fmt.Errorf("unknown phase %q", *phase)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("federation pull request phase %s passed\n", *phase)
}

func create(ctx context.Context) error {
	fixtures, err := canonicalFixtures()
	if err != nil {
		return err
	}
	instances := []struct {
		name string
		url  string
	}{
		{name: "A", url: requiredEnv("ADENOSINE_A_URL")},
		{name: "B", url: requiredEnv("ADENOSINE_B_URL")},
	}

	// Status and review deliberately arrive before their pull request subject.
	for _, testCase := range fixtures {
		for _, instance := range instances {
			if err := deliver(ctx, instance.url, testCase.event); err != nil {
				return fmt.Errorf("deliver %s to %s: %w", testCase.name, instance.name, err)
			}
		}
	}
	for _, testCase := range fixtures {
		for _, instance := range instances {
			if err := deliver(ctx, instance.url, testCase.event); err != nil {
				return fmt.Errorf("replay %s to %s: %w", testCase.name, instance.name, err)
			}
		}
	}

	return verifyDatabases(ctx, []databaseCase{
		{name: "A", url: requiredEnv("DATABASE_URL_A"), wantSourceRepositoryCount: 1},
		{name: "B", url: requiredEnv("DATABASE_URL_B"), wantSourceRepositoryCount: 1},
	}, projectionExpectation{state: "open"})
}

func fetch(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL"), nil)
	if err != nil {
		return fmt.Errorf("open A database: %w", err)
	}
	defer db.Close()
	filesystem, err := storage.NewFilesystem(requiredEnv("ADENOSINE_REPO_ROOT"))
	if err != nil {
		return fmt.Errorf("open A repository storage: %w", err)
	}
	runner, err := gitservice.NewRunnerWithHTTPCAInfo(valueOrDefault("ADENOSINE_GIT_BINARY", "git"), requiredEnv("ADENOSINE_PR_FETCH_CAINFO"))
	if err != nil {
		return fmt.Errorf("create Git runner with B CA: %w", err)
	}
	nativeGit := gitservice.NewServiceWithResolver(runner, filesystem, sourceResolver{}, func(ip net.IP) bool {
		return ip.IsPrivate()
	})
	result, err := pullrequest.NewService(pullrequest.NewPostgresStore(db.Queries()), nativeGit).Refresh(ctx, pullRequestURI)
	if err != nil {
		return fmt.Errorf("refresh projected pull request: %w", err)
	}
	wantRepositoryID := repository.ID(uuid.MustParse(targetRepoID))
	commit, err := nativeGit.Commit(ctx, result.RepositoryID, result.HeadRef)
	if err != nil {
		return fmt.Errorf("resolve controlled head: %w", err)
	}
	refs, err := nativeGit.Refs(ctx, result.RepositoryID)
	if err != nil {
		return fmt.Errorf("list fetched refs: %w", err)
	}
	quarantineAbsent := true
	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, "refs/adenosine/quarantine/") {
			quarantineAbsent = false
		}
	}
	var advertisement bytes.Buffer
	if err := nativeGit.UploadPack(ctx, result.RepositoryID, nil, &advertisement, gitservice.PackOptions{AdvertiseRefs: true}); err != nil {
		return fmt.Errorf("advertise target refs: %w", err)
	}
	one := 1
	checks := []struct {
		name string
		ok   bool
	}{
		{name: "local A target repository", ok: result.RepositoryID == wantRepositoryID},
		{name: "controlled head resolves projected feature commit", ok: commit.SHA == headSHA && result.HeadRef == "refs/adenosine/pull/4bd4ead7d34a3ca13c0744db31ddaf92d9ff898ab4532c49a91b8374437adc51/head"},
		{name: "merge base is A main", ok: result.MergeBase == baseSHA},
		{name: "diff endpoints", ok: result.Diff.BaseSHA == baseSHA && result.Diff.HeadSHA == headSHA},
		{name: "deterministic feature file", ok: len(result.Diff.Files) == 1 && result.Diff.Files[0].Status == "A" && result.Diff.Files[0].NewPath == featurePath && result.Diff.Files[0].Additions != nil && *result.Diff.Files[0].Additions == one && result.Diff.Files[0].Deletions != nil && *result.Diff.Files[0].Deletions == 0},
		{name: "deterministic feature patch", ok: strings.Contains(result.Diff.Patch, "+++ b/"+featurePath) && strings.Contains(result.Diff.Patch, "+"+featureLine)},
		{name: "quarantine refs removed", ok: quarantineAbsent},
		{name: "controlled ref hidden from upload-pack", ok: !bytes.Contains(advertisement.Bytes(), []byte("refs/adenosine/"))},
	}
	for _, testCase := range checks {
		if !testCase.ok {
			return fmt.Errorf("%s failed: result=%+v commit=%+v refs=%+v", testCase.name, result, commit, refs)
		}
	}
	return nil
}

func merge(ctx context.Context) error {
	db, err := database.Open(ctx, requiredEnv("DATABASE_URL_A"), nil)
	if err != nil {
		return fmt.Errorf("open A database: %w", err)
	}
	defer db.Close()
	filesystem, err := storage.NewFilesystem(requiredEnv("ADENOSINE_REPO_ROOT"))
	if err != nil {
		return fmt.Errorf("open A repository storage: %w", err)
	}
	runner, err := gitservice.NewRunnerWithHTTPCAInfo(valueOrDefault("ADENOSINE_GIT_BINARY", "git"), requiredEnv("ADENOSINE_PR_FETCH_CAINFO"))
	if err != nil {
		return fmt.Errorf("create Git runner with B CA: %w", err)
	}
	nativeGit := gitservice.NewServiceWithResolver(runner, filesystem, sourceResolver{}, func(ip net.IP) bool { return ip.IsPrivate() })
	authStore := auth.NewPostgresStore(db.Queries())
	if _, err := authStore.UpsertLogin(ctx, hostedDID, "hosted.example", mergeAt); err != nil {
		return fmt.Errorf("ensure target owner account: %w", err)
	}
	sessions := auth.NewSessionService(authStore, fixedClock{}, auth.UUIDv7Generator{}, auth.RandomSessionSecretGenerator{}, time.Hour)
	_, plaintext, err := sessions.CreateSession(ctx, hostedDID)
	if err != nil {
		return fmt.Errorf("create target owner browser session: %w", err)
	}
	publisher := &deterministicPublisher{git: nativeGit}
	pullRequests := pullrequest.NewApplicationService(
		pullrequest.NewPostgresStore(db.Queries()), nativeGit, publisher, fixedClock{}, authStore, event.NewWriter(db.Queries()),
	)
	const origin = "http://adenosine-a:8080"
	server, err := restapi.NewServer("", origin, db, slog.New(slog.NewTextHandler(io.Discard, nil)), restapi.Observability{}, restapi.Dependencies{
		Sessions: auth.NewSessionAuthenticator(authStore, fixedClock{}), PullRequests: pullRequests,
	}, nil)
	if err != nil {
		return fmt.Errorf("construct public REST server: %w", err)
	}
	httpServer := httptest.NewServer(server.Handler)
	defer httpServer.Close()
	body, err := json.Marshal(map[string]string{"pull_request_uri": pullRequestURI, "strategy": string(gitservice.MergeCommit)})
	if err != nil {
		return fmt.Errorf("encode merge request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL+"/api/v1/pull-requests/merge", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("construct merge request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: plaintext})
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("invoke public merge route: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil {
		return fmt.Errorf("read merge response: %w", err)
	}
	if len(responseBody) > 64*1024 {
		return fmt.Errorf("merge response exceeds 64 KiB")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("merge status = %d, want 200: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result generated.PullRequestMerge
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return fmt.Errorf("decode merge response: %w", err)
	}
	if err := verifyMergeResponse(result, publisher.status); err != nil {
		return err
	}
	if err := verifyOutbox(ctx, db, result.MergeCommitSha); err != nil {
		return err
	}
	eventBody, err := statusEvent(publisher.record)
	if err != nil {
		return err
	}
	for _, testCase := range []struct{ name, url string }{
		{name: "A", url: requiredEnv("ADENOSINE_A_URL")},
		{name: "B", url: requiredEnv("ADENOSINE_B_URL")},
	} {
		if err := deliver(ctx, testCase.url, eventBody); err != nil {
			return fmt.Errorf("deliver merged status update to %s: %w", testCase.name, err)
		}
		if err := deliver(ctx, testCase.url, eventBody); err != nil {
			return fmt.Errorf("replay merged status update to %s: %w", testCase.name, err)
		}
	}
	if err := verifyDatabases(ctx, []databaseCase{
		{name: "A after merge", url: requiredEnv("DATABASE_URL_A"), wantSourceRepositoryCount: 1},
		{name: "B after merge", url: requiredEnv("DATABASE_URL_B"), wantSourceRepositoryCount: 1},
	}, projectionExpectation{state: "merged", mergeSHA: result.MergeCommitSha}); err != nil {
		return err
	}
	return verifyVanillaClone(ctx, result.MergeCommitSha)
}

func verifyMergeResponse(got generated.PullRequestMerge, status pullrequest.Status) error {
	mergeSHA := got.MergeCommitSha
	checks := []struct {
		name string
		ok   bool
	}{
		{name: "merge Git result", ok: got.Strategy == generated.PullRequestMergeStrategy(gitservice.MergeCommit) && got.OldSha == baseSHA && got.HeadSha == headSHA && got.TargetRef == "refs/heads/main" && validGitSHA(mergeSHA)},
		{name: "target owner status identity", ok: got.Status.Uri == status.URI && got.Status.Cid == status.CID && got.Status.AuthorDid == hostedDID},
		{name: "target owner status content", ok: got.Status.PullRequestUri == pullRequestURI && got.Status.PullRequestCid == status.Subject.CID && got.Status.TargetRepositoryUri == targetURI && got.Status.TargetRepositoryCid == repositoryCID && string(got.Status.State) == "merged" && got.Status.MergeCommitSha != nil && *got.Status.MergeCommitSha == mergeSHA && got.Status.CreatedAt.Equal(createdAt) && got.Status.UpdatedAt.Equal(mergeAt)},
	}
	for _, testCase := range checks {
		if !testCase.ok {
			return fmt.Errorf("%s failed: response=%+v status=%+v", testCase.name, got, status)
		}
	}
	return nil
}

func verifyOutbox(ctx context.Context, db *database.DB, mergeSHA string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin refs-updated outbox query: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT aggregate_type, aggregate_id, payload FROM ops.outbox_events WHERE type = 'git.refs_updated'`)
	if err != nil {
		return fmt.Errorf("query refs-updated outbox event: %w", err)
	}
	defer rows.Close()
	type payload struct {
		RepositoryID   string `json:"repository_id"`
		Ref            string `json:"ref"`
		OldSHA         string `json:"old_sha"`
		NewSHA         string `json:"new_sha"`
		HeadSHA        string `json:"head_sha"`
		ActorDID       string `json:"actor_did"`
		PullRequestURI string `json:"pull_request_uri"`
		Strategy       string `json:"strategy"`
	}
	var count int
	for rows.Next() {
		count++
		var aggregateType, aggregateID string
		var raw []byte
		if err := rows.Scan(&aggregateType, &aggregateID, &raw); err != nil {
			return fmt.Errorf("scan refs-updated outbox event: %w", err)
		}
		var got payload
		if err := json.Unmarshal(raw, &got); err != nil {
			return fmt.Errorf("decode refs-updated payload: %w", err)
		}
		want := payload{targetRepoID, "refs/heads/main", baseSHA, mergeSHA, headSHA, hostedDID, pullRequestURI, string(gitservice.MergeCommit)}
		if aggregateType != "repository" || aggregateID != targetRepoID || got != want {
			return fmt.Errorf("refs-updated outbox event mismatch: aggregate=%s/%s payload=%+v want=%+v", aggregateType, aggregateID, got, want)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate refs-updated outbox events: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("refs-updated outbox event count = %d, want 1", count)
	}
	return nil
}

func verifyVanillaClone(ctx context.Context, mergeSHA string) error {
	directory, err := os.MkdirTemp("", "adenosine-federation-pr-")
	if err != nil {
		return fmt.Errorf("create clone directory: %w", err)
	}
	defer os.RemoveAll(directory)
	repositoryPath := filepath.Join(directory, "hosted-repo")
	cloneURL := "http://adenosine-a:8080/" + hostedDID + "/hosted-repo.git"
	command := exec.CommandContext(ctx, valueOrDefault("ADENOSINE_GIT_BINARY", "git"), "clone", "--branch", targetBranch, "--single-branch", cloneURL, repositoryPath)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("clone merged target repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	runGit := func(args ...string) (string, error) {
		command := exec.CommandContext(ctx, valueOrDefault("ADENOSINE_GIT_BINARY", "git"), append([]string{"-C", repositoryPath}, args...)...)
		output, err := command.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
		return strings.TrimSpace(string(output)), nil
	}
	head, err := runGit("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	parents, err := runGit("show", "-s", "--format=%P", "HEAD")
	if err != nil {
		return err
	}
	feature, err := os.ReadFile(filepath.Join(repositoryPath, featurePath))
	if err != nil {
		return fmt.Errorf("read cloned feature file: %w", err)
	}
	if head != mergeSHA || parents != baseSHA+" "+headSHA || string(feature) != featureLine+"\n" {
		return fmt.Errorf("cloned merge mismatch: HEAD=%q parents=%q feature=%q", head, parents, string(feature))
	}
	return nil
}

type databaseCase struct {
	name                      string
	url                       string
	wantSourceRepositoryCount int64
}

func verifyDatabases(ctx context.Context, testCases []databaseCase, expectation projectionExpectation) error {
	var want *projection
	for _, testCase := range testCases {
		db, err := database.Open(ctx, testCase.url, nil)
		if err != nil {
			return fmt.Errorf("open %s database: %w", testCase.name, err)
		}
		got, snapshotErr := snapshot(ctx, db)
		db.Close()
		if snapshotErr != nil {
			return fmt.Errorf("query %s projection: %w", testCase.name, snapshotErr)
		}
		if err := verifyProjection(got, testCase.wantSourceRepositoryCount, expectation); err != nil {
			return fmt.Errorf("%s projection: %w", testCase.name, err)
		}
		if want == nil {
			copy := got
			copy.sourceRepositoryCount = 0
			want = &copy
			continue
		}
		got.sourceRepositoryCount = 0
		if got != *want {
			return fmt.Errorf("%s authoritative projection differs from first database: got %+v want %+v", testCase.name, got, *want)
		}
	}
	return nil
}

func snapshot(ctx context.Context, db *database.DB) (projection, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return projection{}, err
	}
	defer tx.Rollback(ctx)

	var got projection
	err = tx.QueryRow(ctx, `
		SELECT uri, cid, author_did, source_repository_uri, source_repository_cid,
		       source_branch, target_repository_uri, target_repository_cid, target_branch,
		       head_sha, title, body, state, COALESCE(status_uri, ''), COALESCE(status_cid, ''),
		       COALESCE(merged_commit_sha, ''), record_created_at, record_updated_at, review_count
		FROM network.pull_requests WHERE uri = $1 AND deleted_at IS NULL`, pullRequestURI).Scan(
		&got.prURI, &got.prCID, &got.authorDID, &got.sourceURI, &got.sourceCID,
		&got.sourceBranch, &got.targetURI, &got.targetCID, &got.targetBranch,
		&got.headSHA, &got.title, &got.body, &got.state, &got.statusURI, &got.statusCID,
		&got.mergeCommitSHA, &got.createdAt, &got.updatedAt, &got.reviewCount,
	)
	if err != nil {
		return projection{}, fmt.Errorf("pull request: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT uri, cid, author_did, verdict, body, pull_request_uri, pull_request_cid
		FROM network.pull_request_reviews
		WHERE pull_request_uri = $1 AND deleted_at IS NULL`, pullRequestURI).Scan(
		&got.reviewURI, &got.reviewCID, &got.reviewAuthor, &got.reviewVerdict, &got.reviewBody,
		&got.reviewSubjectURI, &got.reviewSubjectCID,
	)
	if err != nil {
		return projection{}, fmt.Errorf("review: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT cid, author_did, pull_request_uri, pull_request_cid,
		       target_repository_uri, target_repository_cid, state, COALESCE(merged_commit_sha, ''),
		       record_created_at, record_updated_at
		FROM network.pull_request_statuses WHERE uri = $1 AND deleted_at IS NULL`, got.statusURI).Scan(
		&got.authoritativeStatusCID, &got.authoritativeStatusAuthor, &got.authoritativeStatusSubject,
		&got.authoritativeStatusSubjectCID, &got.authoritativeStatusTarget, &got.authoritativeTargetCID,
		&got.authoritativeStatusState, &got.authoritativeMergeSHA, &got.authoritativeStatusCreatedAt, &got.authoritativeStatusUpdatedAt,
	)
	if err != nil {
		return projection{}, fmt.Errorf("authoritative status: %w", err)
	}
	maliciousURI := "at://" + bobDID + "/" + pullrequest.StatusCollection + "/" + statusRKey
	err = tx.QueryRow(ctx, `
		SELECT record.author_did, record.cid, status.author_did, status.state
		FROM network.records AS record
		JOIN network.pull_request_statuses AS status ON status.uri = record.uri
		WHERE record.uri = $1 AND record.deleted_at IS NULL AND status.deleted_at IS NULL`, maliciousURI).Scan(
		&got.maliciousRawAuthor, &got.maliciousRawCID, &got.maliciousStatusAuthor, &got.maliciousStatusState,
	)
	if err != nil {
		return projection{}, fmt.Errorf("malicious raw status: %w", err)
	}
	err = tx.QueryRow(ctx, `
		SELECT pull_request_count, open_pull_request_count,
		       (SELECT count(*) FROM network.pull_request_statuses WHERE pull_request_uri = $1 AND deleted_at IS NULL),
		       (SELECT count(*) FROM network.pull_request_reviews WHERE pull_request_uri = $1 AND deleted_at IS NULL),
		       (SELECT count(*) FROM network.repositories WHERE uri = $2 AND deleted_at IS NULL)
		FROM network.repositories WHERE uri = $3 AND deleted_at IS NULL`, pullRequestURI, sourceURI, targetURI).Scan(
		&got.repositoryCount, &got.openRepositoryCount, &got.statusCount, &got.liveReviewCount, &got.sourceRepositoryCount,
	)
	if err != nil {
		return projection{}, fmt.Errorf("repository counters: %w", err)
	}
	return got, nil
}

func verifyProjection(got projection, wantSourceRepositoryCount int64, expectation projectionExpectation) error {
	prRecord, err := pullRequestRecord()
	if err != nil {
		return err
	}
	mergeSHA := expectation.mergeSHA
	if expectation.state == "merged" && mergeSHA == "" {
		mergeSHA = got.mergeCommitSHA
	}
	statusUpdatedAt := updatedAt
	if expectation.state == "merged" {
		statusUpdatedAt = mergeAt
	}
	authoritativeRecord, err := statusRecordAt(expectation.state, mergeSHA, statusUpdatedAt)
	if err != nil {
		return err
	}
	reviewRecord, err := reviewRecord()
	if err != nil {
		return err
	}
	wantPRCID := deterministicCID(prRecord)
	wantStatusCID := deterministicCID(authoritativeRecord)
	wantReviewCID := deterministicCID(reviewRecord)
	maliciousRecord, err := statusRecord("closed", "")
	if err != nil {
		return err
	}
	wantMaliciousCID := deterministicCID(maliciousRecord)
	wantStatusURI := "at://" + hostedDID + "/" + pullrequest.StatusCollection + "/" + statusRKey

	checks := []struct {
		name string
		ok   bool
	}{
		{name: "pull request envelope and content", ok: got.prURI == pullRequestURI && got.prCID == wantPRCID && got.authorDID == bobDID && got.sourceURI == sourceURI && got.sourceCID == repositoryCID && got.sourceBranch == sourceBranch && got.targetURI == targetURI && got.targetCID == repositoryCID && got.targetBranch == targetBranch && got.headSHA == headSHA && got.title == title && got.body == body && got.createdAt.Equal(createdAt) && got.updatedAt.Equal(updatedAt)},
		{name: "target-authoritative state", ok: got.state == expectation.state && got.statusURI == wantStatusURI && got.statusCID == wantStatusCID && got.mergeCommitSHA == mergeSHA && got.authoritativeStatusCID == wantStatusCID && got.authoritativeStatusAuthor == hostedDID && got.authoritativeStatusSubject == pullRequestURI && got.authoritativeStatusSubjectCID == wantPRCID && got.authoritativeStatusTarget == targetURI && got.authoritativeTargetCID == repositoryCID && got.authoritativeStatusState == expectation.state && got.authoritativeMergeSHA == mergeSHA && got.authoritativeStatusCreatedAt.Equal(createdAt) && got.authoritativeStatusUpdatedAt.Equal(statusUpdatedAt)},
		{name: "one live common review", ok: got.reviewCount == 1 && got.liveReviewCount == 1 && got.reviewURI == reviewURI && got.reviewCID == wantReviewCID && got.reviewAuthor == commonDID && got.reviewVerdict == "approve" && got.reviewBody == reviewBody && got.reviewSubjectURI == pullRequestURI && got.reviewSubjectCID == wantPRCID},
		{name: "repository counters", ok: got.repositoryCount == 1 && got.openRepositoryCount == map[bool]int64{true: 1, false: 0}[expectation.state == "open"]},
		{name: "source repository isolation", ok: got.sourceRepositoryCount == wantSourceRepositoryCount},
		{name: "malicious status retained but ignored", ok: got.statusCount == 2 && got.maliciousRawAuthor == bobDID && got.maliciousRawCID == wantMaliciousCID && got.maliciousStatusAuthor == bobDID && got.maliciousStatusState == "closed" && got.statusURI != "at://"+bobDID+"/"+pullrequest.StatusCollection+"/"+statusRKey},
		{name: "actual canonical merged SHA", ok: expectation.state != "merged" || (validGitSHA(mergeSHA) && got.mergeCommitSHA == got.authoritativeMergeSHA)},
	}
	for _, testCase := range checks {
		if !testCase.ok {
			return fmt.Errorf("%s failed: %+v", testCase.name, got)
		}
	}
	return nil
}

func canonicalFixtures() ([]fixture, error) {
	derivedStatusRKey, err := pullrequest.StatusRecordKey(pullRequestURI)
	if err != nil {
		return nil, fmt.Errorf("derive deterministic status rkey: %w", err)
	}
	if derivedStatusRKey != statusRKey {
		return nil, fmt.Errorf("deterministic status rkey = %q, want %q", derivedStatusRKey, statusRKey)
	}
	prRecord, err := pullRequestRecord()
	if err != nil {
		return nil, err
	}
	authoritativeStatus, err := statusRecord("open", "")
	if err != nil {
		return nil, err
	}
	maliciousStatus, err := statusRecord("closed", "")
	if err != nil {
		return nil, err
	}
	review, err := reviewRecord()
	if err != nil {
		return nil, err
	}
	testCases := []struct {
		name       string
		id         int64
		did        string
		collection string
		rkey       string
		record     []byte
	}{
		{name: "malicious Bob-authored target status", id: 30, did: bobDID, collection: pullrequest.StatusCollection, rkey: statusRKey, record: maliciousStatus},
		{name: "target-owner authoritative status", id: 31, did: hostedDID, collection: pullrequest.StatusCollection, rkey: statusRKey, record: authoritativeStatus},
		{name: "common reviewer review", id: 32, did: commonDID, collection: pullrequest.ReviewCollection, rkey: reviewRKey, record: review},
		{name: "Bob pull request", id: 33, did: bobDID, collection: pullrequest.Collection, rkey: pullRequestRKey, record: prRecord},
	}
	fixtures := make([]fixture, 0, len(testCases))
	for _, testCase := range testCases {
		event, marshalErr := json.Marshal(map[string]any{
			"id": testCase.id, "type": "record",
			"record": map[string]any{
				"live": true, "rev": "3kzfcijpj2z2a", "did": testCase.did, "collection": testCase.collection,
				"rkey": testCase.rkey, "action": "create", "cid": deterministicCID(testCase.record), "record": json.RawMessage(testCase.record),
			},
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("encode %s event: %w", testCase.name, marshalErr)
		}
		fixtures = append(fixtures, fixture{name: testCase.name, event: event})
	}
	return fixtures, nil
}

func pullRequestRecord() ([]byte, error) {
	return json.Marshal(struct {
		Type             string                `json:"$type"`
		SourceRepository pullrequest.StrongRef `json:"sourceRepository"`
		TargetRepository pullrequest.StrongRef `json:"targetRepository"`
		SourceBranch     string                `json:"sourceBranch"`
		TargetBranch     string                `json:"targetBranch"`
		HeadSHA          string                `json:"headSHA"`
		Title            string                `json:"title"`
		Body             string                `json:"body"`
		CreatedAt        string                `json:"createdAt"`
		UpdatedAt        string                `json:"updatedAt"`
	}{pullrequest.Collection, pullrequest.StrongRef{URI: sourceURI, CID: repositoryCID}, pullrequest.StrongRef{URI: targetURI, CID: repositoryCID}, sourceBranch, targetBranch, headSHA, title, body, createdAt.Format(syntax.AtprotoDatetimeLayout), updatedAt.Format(syntax.AtprotoDatetimeLayout)})
}

func statusRecord(state, commit string) ([]byte, error) {
	return statusRecordAt(state, commit, updatedAt)
}

func statusRecordAt(state, commit string, statusUpdatedAt time.Time) ([]byte, error) {
	prRecord, err := pullRequestRecord()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type             string                `json:"$type"`
		Subject          pullrequest.StrongRef `json:"subject"`
		TargetRepository pullrequest.StrongRef `json:"targetRepository"`
		State            string                `json:"state"`
		MergeCommitSHA   string                `json:"mergeCommitSHA,omitempty"`
		CreatedAt        string                `json:"createdAt"`
		UpdatedAt        string                `json:"updatedAt"`
	}{pullrequest.StatusCollection, pullrequest.StrongRef{URI: pullRequestURI, CID: deterministicCID(prRecord)}, pullrequest.StrongRef{URI: targetURI, CID: repositoryCID}, state, commit, createdAt.Format(syntax.AtprotoDatetimeLayout), statusUpdatedAt.Format(syntax.AtprotoDatetimeLayout)})
}

func statusEvent(record []byte) ([]byte, error) {
	event, err := json.Marshal(map[string]any{
		"id": 34, "type": "record",
		"record": map[string]any{
			"live": true, "rev": "3kzfcijpj2z2b", "did": hostedDID, "collection": pullrequest.StatusCollection,
			"rkey": statusRKey, "action": "update", "cid": deterministicCID(record), "record": json.RawMessage(record),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode merged status update event: %w", err)
	}
	return event, nil
}

func reviewRecord() ([]byte, error) {
	prRecord, err := pullRequestRecord()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type      string                `json:"$type"`
		Subject   pullrequest.StrongRef `json:"subject"`
		Verdict   string                `json:"verdict"`
		Body      string                `json:"body"`
		CreatedAt string                `json:"createdAt"`
		UpdatedAt string                `json:"updatedAt"`
	}{pullrequest.ReviewCollection, pullrequest.StrongRef{URI: pullRequestURI, CID: deterministicCID(prRecord)}, "approve", reviewBody, createdAt.Format(syntax.AtprotoDatetimeLayout), updatedAt.Format(syntax.AtprotoDatetimeLayout)})
}

func deterministicCID(record []byte) string {
	digest := sha256.Sum256(record)
	raw := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
}

func validGitSHA(value string) bool {
	return (len(value) == 40 || len(value) == 64) && strings.Trim(value, "0123456789abcdef") == "" && strings.Trim(value, "0") != ""
}

func deliver(ctx context.Context, baseURL string, event []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/internal/federation/tap", bytes.NewReader(event))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("admin", requiredEnv("ADENOSINE_TAP_ADMIN_PASSWORD"))
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("status = %d, want 204: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s is required\n", name)
		os.Exit(2)
	}
	return value
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
