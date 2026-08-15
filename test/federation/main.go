package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	tapPath             = "/internal/federation/tap"
	networkPath         = "/api/v1/network/repositories"
	searchPath          = "/api/v1/search/repositories"
	testCID             = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	staleCID            = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdj"
	hostedDID           = "did:plc:cccccccccccccccccccccccc"
	hostedRKey          = "0198a8512a897ae2a370dc68883e3af5"
	hostedURI           = "at://" + hostedDID + "/dev.adenosine.repo/" + hostedRKey
	transferRKey        = "0198a8512a897ae2a370dc68883e3af6"
	transferProposalURI = "at://" + hostedDID + "/dev.adenosine.repositoryTransfer/" + transferRKey
	transferredRKey     = hostedRKey
	transferredURI      = "at://" + starAuthorDID + "/dev.adenosine.repo/" + transferredRKey
	hostedGit           = "https://adenosine-a-tls/" + hostedDID + "/hosted-repo.git"
	transferredGit      = "https://adenosine-a-tls/bob.example/hosted-repo.git"
	sourceURI           = "at://did:plc:bbbbbbbbbbbbbbbbbbbbbbbb/dev.adenosine.repo/b-only"
	sourceGit           = "https://adenosine-b-tls/did:plc:bbbbbbbbbbbbbbbbbbbbbbbb/b-only.git"
	hostedReadme        = "# Hosted Federation Repository\n"
	starAuthorDID       = "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb"
	starCollection      = "dev.adenosine.star"
	issueCollection     = "dev.adenosine.issue"
	commentCollection   = "dev.adenosine.issueComment"
	issueTitle          = "Federated issue from Bob"
	issueBody           = "Created through B against A's projected repository."
	rootCommentBody     = "Bob's federated root comment."
	replyCommentBody    = "Bob's reply to the exact root observation."
)

var (
	starCreatedAt  = time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
	issueCreatedAt = time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	rootCommentAt  = time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	replyCommentAt = time.Date(2026, 8, 9, 15, 1, 0, 0, time.UTC)
)

type instance struct {
	name string
	url  string
}

type fixture struct {
	name      string
	instances []string
	body      string
}

type repository struct {
	URI                  string `json:"uri"`
	CID                  string `json:"cid"`
	Slug                 string `json:"slug"`
	StarCount            int64  `json:"star_count"`
	IssueCount           int64  `json:"issue_count"`
	OpenIssueCount       int64  `json:"open_issue_count"`
	CommentCount         int64  `json:"comment_count"`
	PullRequestCount     int64  `json:"pull_request_count"`
	OpenPullRequestCount int64  `json:"open_pull_request_count"`
	Owner                struct {
		DID string `json:"did"`
	} `json:"owner"`
	Hosting struct {
		Local       bool   `json:"local"`
		GitHTTPSURL string `json:"git_https_url"`
	} `json:"hosting"`
}

type page struct {
	Data []repository `json:"items"`
	Page struct {
		NextCursor *string `json:"next_cursor"`
	} `json:"page"`
}

type starPage struct {
	StarCount int64 `json:"star_count"`
	Data      []struct {
		URI           string    `json:"uri"`
		CID           string    `json:"cid"`
		AuthorDID     string    `json:"author_did"`
		RepositoryURI string    `json:"repository_uri"`
		RepositoryCID string    `json:"repository_cid"`
		CreatedAt     time.Time `json:"created_at"`
	} `json:"items"`
}

type issuePage struct {
	IssueCount     int64       `json:"issue_count"`
	OpenIssueCount int64       `json:"open_issue_count"`
	Data           []issueView `json:"items"`
}

type issueView struct {
	URI           string    `json:"uri"`
	CID           string    `json:"cid"`
	AuthorDID     string    `json:"author_did"`
	RepositoryURI string    `json:"repository_uri"`
	RepositoryCID string    `json:"repository_cid"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	State         string    `json:"state"`
	StatusURI     *string   `json:"status_uri"`
	StatusCID     *string   `json:"status_cid"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CommentCount  int64     `json:"comment_count"`
}

type commentView struct {
	URI       string    `json:"uri"`
	CID       string    `json:"cid"`
	AuthorDID string    `json:"author_did"`
	IssueURI  string    `json:"issue_uri"`
	IssueCID  string    `json:"issue_cid"`
	ParentURI *string   `json:"parent_uri"`
	ParentCID *string   `json:"parent_cid"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IndexedAt time.Time `json:"indexed_at"`
}

type commentPage struct {
	CommentCount int64         `json:"comment_count"`
	Data         []commentView `json:"items"`
}

type authoritativeComment struct {
	URI, CID, AuthorDID, IssueURI, IssueCID, ParentURI, ParentCID, Body string
	CreatedAt, UpdatedAt                                                time.Time
}

var client = &http.Client{Timeout: 10 * time.Second}

func main() {
	phase := flag.String("phase", "seed", "acceptance phase: seed, star, issue, comments, comments-deleted, transfer, or final")
	flag.Parse()

	instances := map[string]instance{
		"a": {name: "A", url: requiredEnv("ADENOSINE_A_URL")},
		"b": {name: "B", url: requiredEnv("ADENOSINE_B_URL")},
	}
	password := requiredEnv("ADENOSINE_TAP_ADMIN_PASSWORD")

	var err error
	switch *phase {
	case "seed":
		err = seed(instances, password)
	case "star":
		err = verifyStars([]instance{instances["a"], instances["b"]})
	case "issue":
		err = verifyIssues([]instance{instances["a"], instances["b"]})
	case "comments":
		err = verifyComments([]instance{instances["a"], instances["b"]}, false)
	case "comments-deleted":
		err = verifyComments([]instance{instances["a"], instances["b"]}, true)
	case "transfer":
		err = verifyTransfer(instances, password)
	case "final":
		err = final(instances["b"])
	default:
		err = fmt.Errorf("unknown phase %q", *phase)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("federation acceptance phase %s passed\n", *phase)
}

func seed(instances map[string]instance, password string) error {
	hostedFixture := fixture{name: "A-hosted repository projected to both AppViews", instances: []string{"a", "b"}, body: hostedRepositoryEvent(13)}
	fixtures := []fixture{
		{name: "common identity", instances: []string{"a", "b"}, body: identityEvent(1, "did:plc:ewvi7nxzyoun6zhxrhs64oiz", "common.example")},
		{name: "A-only identity", instances: []string{"a"}, body: identityEvent(2, "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", "alice.example")},
		{name: "B identity projected for PR source", instances: []string{"a", "b"}, body: identityEvent(3, "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb", "bob.example")},
		{name: "A host identity projected to B", instances: []string{"b"}, body: identityEvent(4, hostedDID, "hosted.example")},
		{name: "common repository", instances: []string{"a", "b"}, body: repositoryEvent(10, "did:plc:ewvi7nxzyoun6zhxrhs64oiz", "common-repo", "Common repository")},
		{name: "A-only repository", instances: []string{"a"}, body: repositoryEvent(11, "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", "a-only", "A only")},
		{name: "B-only repository projected for PR fetch", instances: []string{"a", "b"}, body: sourceRepositoryEvent(12)},
		hostedFixture,
	}
	for _, value := range fixtures {
		for _, target := range value.instances {
			if err := deliver(instances[target], password, value.body); err != nil {
				return fmt.Errorf("deliver %s to instance %s: %w", value.name, instances[target].name, err)
			}
		}
	}

	assertions := []struct {
		name string
		at   instance
		want []string
		not  []string
	}{
		{name: "A projection includes required B PR source", at: instances["a"], want: []string{"a-only", "b-only", "common-repo", "hosted-repo"}},
		{name: "B projection isolation", at: instances["b"], want: []string{"b-only", "common-repo", "hosted-repo"}, not: []string{"a-only"}},
	}
	for _, assertion := range assertions {
		slugs, err := listAll(assertion.at)
		if err != nil {
			return fmt.Errorf("%s: %w", assertion.name, err)
		}
		if err := assertSlugs(slugs, assertion.want, assertion.not); err != nil {
			return fmt.Errorf("%s: %w", assertion.name, err)
		}
	}
	searchCases := []struct {
		name      string
		at        instance
		wantLocal bool
	}{
		{name: "A finds its hosted projection", at: instances["a"], wantLocal: true},
		{name: "B finds A hosted projection equally", at: instances["b"], wantLocal: false},
	}
	for _, testCase := range searchCases {
		matches, err := searchRepositories(testCase.at, "Hosted repository")
		if err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
		if len(matches) != 1 || matches[0].URI != hostedURI || matches[0].Hosting.Local != testCase.wantLocal || matches[0].Hosting.GitHTTPSURL != hostedGit {
			return fmt.Errorf("%s results = %#v", testCase.name, matches)
		}
	}

	replays := []struct {
		name    string
		at      instance
		body    string
		wantLen int
	}{
		{name: "duplicate common repository", at: instances["b"], body: fixtures[4].body, wantLen: 3},
	}
	for _, replay := range replays {
		if err := deliver(replay.at, password, replay.body); err != nil {
			return fmt.Errorf("%s: %w", replay.name, err)
		}
		slugs, err := listAll(replay.at)
		if err != nil {
			return fmt.Errorf("%s list: %w", replay.name, err)
		}
		if len(slugs) != replay.wantLen {
			return fmt.Errorf("%s changed projection count: got %d slugs %v, want %d", replay.name, len(slugs), slugs, replay.wantLen)
		}
	}

	cloneCases := []struct {
		name       string
		at         instance
		uri        string
		cid        string
		wantURL    string
		wantREADME string
	}{
		{name: "clone A repository discovered through B", at: instances["b"], uri: hostedURI, cid: testCID, wantURL: hostedGit, wantREADME: hostedReadme},
	}
	for _, testCase := range cloneCases {
		repository, err := findRepository(testCase.at, testCase.uri, testCase.cid)
		if err != nil {
			return fmt.Errorf("%s discovery: %w", testCase.name, err)
		}
		if repository.Hosting.GitHTTPSURL != testCase.wantURL {
			return fmt.Errorf("%s git_https_url = %q, want %q", testCase.name, repository.Hosting.GitHTTPSURL, testCase.wantURL)
		}
		if err := cloneAndVerify(repository.Hosting.GitHTTPSURL, testCase.wantREADME); err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
	}
	return nil
}

func verifyTransfer(instances map[string]instance, password string) error {
	proposal := map[string]any{
		"$type":          "dev.adenosine.repositoryTransfer",
		"repository":     map[string]string{"uri": hostedURI, "cid": testCID},
		"destinationDID": starAuthorDID, "destinationOwner": "bob.example",
		"createdAt": "2026-08-09T16:00:00Z", "expiresAt": "2026-08-16T16:00:00Z",
	}
	staleProposal := map[string]any{
		"$type":          "dev.adenosine.repositoryTransfer",
		"repository":     map[string]string{"uri": hostedURI, "cid": staleCID},
		"destinationDID": starAuthorDID, "destinationOwner": "bob.example",
		"createdAt": "2026-08-09T16:00:00Z", "expiresAt": "2026-08-16T16:00:00Z",
	}
	successor := map[string]any{
		"$type": "dev.adenosine.repo", "slug": "hosted-repo", "name": "Hosted repository", "defaultBranch": "main",
		"description": "Transferred repository lineage", "git": map[string]string{"https": "https://adenosine-a-tls/" + starAuthorDID + "/hosted-repo.git"},
		"web":             "https://adenosine-a-tls/" + starAuthorDID + "/hosted-repo",
		"transferredFrom": map[string]string{"uri": hostedURI, "cid": testCID},
		"createdAt":       "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T16:01:00Z",
	}
	staleSuccessor := map[string]any{
		"$type": "dev.adenosine.repo", "slug": "hosted-repo", "name": "Hosted repository", "defaultBranch": "main",
		"description": "Transferred repository lineage", "git": map[string]string{"https": "https://adenosine-a-tls/" + starAuthorDID + "/hosted-repo.git"},
		"web":             "https://adenosine-a-tls/" + starAuthorDID + "/hosted-repo",
		"transferredFrom": map[string]string{"uri": hostedURI, "cid": staleCID},
		"createdAt":       "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T16:01:00Z",
	}
	digest := sha256.Sum256([]byte(transferProposalURI))
	acceptanceRKey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
	acceptance := map[string]any{
		"$type":      "dev.adenosine.repositoryTransferAcceptance",
		"proposal":   map[string]string{"uri": transferProposalURI, "cid": testCID},
		"repository": map[string]string{"uri": transferredURI, "cid": testCID},
		"createdAt":  "2026-08-09T16:02:00Z",
	}
	redirect := map[string]any{
		"$type": "dev.adenosine.repo", "slug": "hosted-repo", "name": "Hosted repository", "defaultBranch": "main",
		"description":   "Cloned through isolated federation acceptance",
		"git":           map[string]string{"https": hostedGit, "ssh": "ssh://git@adenosine-a:2222/" + hostedDID + "/hosted-repo.git"},
		"web":           strings.TrimSuffix(hostedGit, ".git"),
		"transferredTo": map[string]string{"uri": transferredURI, "cid": testCID},
		"createdAt":     "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T16:03:00Z",
	}
	fixtures := map[string][]string{
		"a": {
			recordMutation(201, hostedDID, "dev.adenosine.repositoryTransfer", transferRKey, "create", staleProposal),
			recordMutationWithCID(202, starAuthorDID, "dev.adenosine.repo", transferredRKey, "create", staleCID, staleSuccessor),
			recordMutation(203, starAuthorDID, "dev.adenosine.repositoryTransferAcceptance", acceptanceRKey, "create", acceptance),
			recordMutation(204, hostedDID, "dev.adenosine.repo", hostedRKey, "update", redirect),
		},
		"b": {
			recordMutation(203, starAuthorDID, "dev.adenosine.repositoryTransferAcceptance", acceptanceRKey, "create", acceptance),
			recordMutation(204, hostedDID, "dev.adenosine.repo", hostedRKey, "update", redirect),
			recordMutation(201, hostedDID, "dev.adenosine.repositoryTransfer", transferRKey, "create", staleProposal),
			recordMutationWithCID(202, starAuthorDID, "dev.adenosine.repo", transferredRKey, "create", staleCID, staleSuccessor),
		},
	}
	for name, events := range fixtures {
		for _, event := range events {
			if err := deliver(instances[name], password, event); err != nil {
				return fmt.Errorf("deliver transfer event to %s: %w", instances[name].name, err)
			}
		}
	}
	for _, target := range []instance{instances["a"], instances["b"]} {
		matches, err := searchRepositories(target, "Hosted repository")
		if err != nil {
			return fmt.Errorf("search stale transfer projections on %s: %w", target.name, err)
		}
		uris := map[string]bool{}
		for _, match := range matches {
			uris[match.URI] = true
		}
		if len(matches) != 2 || !uris[hostedURI] || !uris[transferredURI] {
			return fmt.Errorf("%s stale-CID transfer projections were unexpectedly linked: %#v", target.name, matches)
		}
		response, err := client.Get(target.url + "/api/v1/issues?repository_uri=" + url.QueryEscape(transferredURI))
		if err != nil {
			return err
		}
		var issues issuePage
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&issues)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || issues.IssueCount != 0 || len(issues.Data) != 0 {
			return fmt.Errorf("%s stale-CID successor inherited issue lineage: status=%d value=%+v error=%v", target.name, response.StatusCode, issues, decodeErr)
		}
	}
	corrections := map[string][]string{
		"a": {
			recordMutation(205, hostedDID, "dev.adenosine.repositoryTransfer", transferRKey, "update", proposal),
			recordMutation(206, starAuthorDID, "dev.adenosine.repo", transferredRKey, "update", successor),
		},
		"b": {
			recordMutation(206, starAuthorDID, "dev.adenosine.repo", transferredRKey, "update", successor),
			recordMutation(205, hostedDID, "dev.adenosine.repositoryTransfer", transferRKey, "update", proposal),
		},
	}
	for name, events := range corrections {
		for _, event := range events {
			if err := deliver(instances[name], password, event); err != nil {
				return fmt.Errorf("deliver transfer correction to %s: %w", instances[name].name, err)
			}
		}
	}

	for _, target := range []instance{instances["a"], instances["b"]} {
		matches, err := searchRepositories(target, "Hosted repository")
		if err != nil || len(matches) != 1 || matches[0].URI != transferredURI {
			return fmt.Errorf("%s canonical transfer search = %#v: %w", target.name, matches, err)
		}
		canonical, err := findRepository(target, transferredURI, testCID)
		if err != nil {
			return fmt.Errorf("find successor projection on %s: %w", target.name, err)
		}
		if canonical.StarCount != 1 || canonical.IssueCount != 1 || canonical.OpenIssueCount != 1 ||
			canonical.CommentCount != 1 || canonical.PullRequestCount != 1 || canonical.OpenPullRequestCount != 0 {
			return fmt.Errorf("%s successor collaboration counts = %+v", target.name, canonical)
		}
		for _, owner := range []string{hostedDID, "hosted.example", starAuthorDID, "bob.example"} {
			projected, err := getRepositoryRoute(target, owner, "hosted-repo")
			if err != nil {
				return fmt.Errorf("resolve %s transfer route on %s: %w", owner, target.name, err)
			}
			if projected.URI != transferredURI {
				return fmt.Errorf("%s transfer route %s = %+v", target.name, owner, projected)
			}
		}
		response, err := client.Get(target.url + "/api/v1/issues?repository_uri=" + url.QueryEscape(transferredURI))
		if err != nil {
			return err
		}
		var issues issuePage
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&issues)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || len(issues.Data) != 1 || issues.Data[0].RepositoryURI != hostedURI {
			return fmt.Errorf("%s transferred issue projection status=%d value=%+v error=%v", target.name, response.StatusCode, issues, decodeErr)
		}
	}
	for _, gitURL := range []string{hostedGit, transferredGit} {
		if err := cloneAndVerify(gitURL, hostedReadme); err != nil {
			return fmt.Errorf("clone retained transfer route %s: %w", gitURL, err)
		}
	}
	return nil
}

func getRepositoryRoute(target instance, owner, slug string) (repository, error) {
	response, err := client.Get(target.url + "/api/v1/repositories/" + url.PathEscape(owner) + "/" + url.PathEscape(slug))
	if err != nil {
		return repository{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return repository{}, fmt.Errorf("GET repository status %d: %s", response.StatusCode, body)
	}
	var value repository
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return repository{}, err
	}
	return value, nil
}

func recordMutation(id int, did, collection, rkey, action string, record map[string]any) string {
	return recordMutationWithCID(id, did, collection, rkey, action, testCID, record)
}

func recordMutationWithCID(id int, did, collection, rkey, action, cid string, record map[string]any) string {
	recordJSON, _ := json.Marshal(record)
	return fmt.Sprintf(`{"id":%d,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":%q,"collection":%q,"rkey":%q,"action":%q,"cid":%q,"record":%s}}`, id, did, collection, rkey, action, cid, recordJSON)
}

func final(b instance) error {
	matches, err := searchRepositories(b, "Hosted repository")
	if err != nil || len(matches) != 1 || matches[0].URI != transferredURI || matches[0].Hosting.Local {
		return fmt.Errorf("B search after A and Electric stop = %#v: %w", matches, err)
	}
	if err := verifyStars([]instance{b}); err != nil {
		return fmt.Errorf("B star projection after A stops: %w", err)
	}
	if err := verifyIssues([]instance{b}); err != nil {
		return fmt.Errorf("B issue projection after A stops: %w", err)
	}
	if err := verifyComments([]instance{b}, true); err != nil {
		return fmt.Errorf("B comment projection after A stops: %w", err)
	}
	projectionCases := []struct {
		name string
		want []string
		not  []string
	}{
		{name: "B remains queryable after A stops", want: []string{"b-only", "common-repo", "hosted-repo"}, not: []string{"a-only"}},
	}
	for _, testCase := range projectionCases {
		slugs, err := listAll(b)
		if err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
		if err := assertSlugs(slugs, testCase.want, testCase.not); err != nil {
			return fmt.Errorf("%s: %w", testCase.name, err)
		}
	}

	requestCases := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{name: "malformed cursor", query: "?limit=1&cursor=%25%25%25malformed%25%25%25", wantStatus: http.StatusBadRequest},
	}
	for _, testCase := range requestCases {
		response, err := client.Get(b.url + networkPath + testCase.query)
		if err != nil {
			return fmt.Errorf("%s request: %w", testCase.name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("%s response: %w", testCase.name, readErr)
		}
		if response.StatusCode != testCase.wantStatus {
			return fmt.Errorf("%s status = %d, want %d: %s", testCase.name, response.StatusCode, testCase.wantStatus, strings.TrimSpace(string(body)))
		}
	}
	return nil
}

func searchRepositories(at instance, query string) ([]repository, error) {
	requestURL := at.url + searchPath + "?q=" + url.QueryEscape(query) + "&limit=10"
	response, err := client.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("GET search status %d: %s", response.StatusCode, body)
	}
	var result page
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func verifyIssues(instances []instance) error {
	var wantURI, wantCID string
	for _, testCase := range instances {
		response, err := client.Get(testCase.url + "/api/v1/issues?repository_uri=" + url.QueryEscape(hostedURI))
		if err != nil {
			return fmt.Errorf("GET issues from %s: %w", testCase.name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read issues from %s: %w", testCase.name, readErr)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET issues from %s status = %d: %s", testCase.name, response.StatusCode, strings.TrimSpace(string(body)))
		}
		var projection issuePage
		if err := json.Unmarshal(body, &projection); err != nil {
			return fmt.Errorf("decode issues from %s: %w", testCase.name, err)
		}
		if projection.IssueCount != 1 || projection.OpenIssueCount != 1 || len(projection.Data) != 1 {
			return fmt.Errorf("%s issue projection counts = %d/%d with %d records, want 1/1 with one record", testCase.name, projection.IssueCount, projection.OpenIssueCount, len(projection.Data))
		}
		got := projection.Data[0]
		parts := strings.Split(got.URI, "/")
		if len(parts) != 5 || parts[0] != "at:" || parts[2] != starAuthorDID || parts[3] != issueCollection || parts[4] == "" {
			return fmt.Errorf("%s issue URI = %q, want canonical Bob issue URI", testCase.name, got.URI)
		}
		if !compactUUIDv7(parts[4]) {
			return fmt.Errorf("%s issue rkey = %q, want canonical compact UUIDv7", testCase.name, parts[4])
		}
		record, err := json.Marshal(struct {
			Type       string `json:"$type"`
			Repository struct {
				URI string `json:"uri"`
				CID string `json:"cid"`
			} `json:"repository"`
			Title     string `json:"title"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
		}{
			Type: issueCollection, Repository: struct {
				URI string `json:"uri"`
				CID string `json:"cid"`
			}{URI: hostedURI, CID: testCID},
			Title: issueTitle, Body: issueBody, CreatedAt: issueCreatedAt.Format(time.RFC3339), UpdatedAt: issueCreatedAt.Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("encode expected issue: %w", err)
		}
		digest := sha256.Sum256(record)
		rawCID := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
		calculatedCID := "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawCID))
		if wantURI == "" {
			wantURI, wantCID = got.URI, calculatedCID
		}
		if got.URI != wantURI || got.CID != wantCID || got.CID != calculatedCID || got.AuthorDID != starAuthorDID ||
			got.RepositoryURI != hostedURI || got.RepositoryCID != testCID || got.Title != issueTitle || got.Body != issueBody ||
			got.State != "open" || got.StatusURI != nil || got.StatusCID != nil || !got.CreatedAt.Equal(issueCreatedAt) || !got.UpdatedAt.Equal(issueCreatedAt) {
			return fmt.Errorf("%s projected unexpected issue: %+v", testCase.name, got)
		}
		repository, err := findRepository(testCase, hostedURI, testCID)
		if err != nil {
			return fmt.Errorf("find issue repository on %s: %w", testCase.name, err)
		}
		if repository.IssueCount != 1 || repository.OpenIssueCount != 1 {
			return fmt.Errorf("%s network repository issue counts = %d/%d, want 1/1", testCase.name, repository.IssueCount, repository.OpenIssueCount)
		}
	}
	return nil
}

func verifyComments(instances []instance, rootDeleted bool) error {
	var want []authoritativeComment
	for _, testCase := range instances {
		projectedIssue, err := getFederatedIssue(testCase)
		if err != nil {
			return err
		}
		response, err := client.Get(testCase.url + "/api/v1/issues/comments?issue_uri=" + url.QueryEscape(projectedIssue.URI))
		if err != nil {
			return fmt.Errorf("GET comments from %s: %w", testCase.name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read comments from %s: %w", testCase.name, readErr)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET comments from %s status = %d: %s", testCase.name, response.StatusCode, strings.TrimSpace(string(body)))
		}
		var projection commentPage
		if err := json.Unmarshal(body, &projection); err != nil {
			return fmt.Errorf("decode comments from %s: %w", testCase.name, err)
		}
		wantCount := int64(2)
		if rootDeleted {
			wantCount = 1
		}
		if projection.CommentCount != wantCount || int64(len(projection.Data)) != wantCount {
			return fmt.Errorf("%s comment count/data = %d/%d, want %d/%d", testCase.name, projection.CommentCount, len(projection.Data), wantCount, wantCount)
		}

		var rootURI, rootCID string
		if rootDeleted {
			if projection.Data[0].ParentURI == nil || projection.Data[0].ParentCID == nil {
				return fmt.Errorf("%s surviving reply lost tombstoned parent reference: %+v", testCase.name, projection.Data[0])
			}
			rootURI, rootCID = *projection.Data[0].ParentURI, *projection.Data[0].ParentCID
		} else {
			rootURI, rootCID = projection.Data[0].URI, projection.Data[0].CID
			if projection.Data[0].ParentURI != nil || projection.Data[0].ParentCID != nil {
				return fmt.Errorf("%s root unexpectedly has parent: %+v", testCase.name, projection.Data[0])
			}
		}
		if err := validateCommentURI(rootURI); err != nil {
			return fmt.Errorf("%s root URI: %w", testCase.name, err)
		}
		calculatedRootCID, err := expectedCommentCID(projectedIssue.URI, projectedIssue.CID, "", "", rootCommentBody, rootCommentAt)
		if err != nil {
			return err
		}
		if rootCID != calculatedRootCID {
			return fmt.Errorf("%s root CID = %q, want %q", testCase.name, rootCID, calculatedRootCID)
		}
		replyIndex := 1
		if rootDeleted {
			replyIndex = 0
		}
		reply := projection.Data[replyIndex]
		if err := validateCommentURI(reply.URI); err != nil {
			return fmt.Errorf("%s reply URI: %w", testCase.name, err)
		}
		calculatedReplyCID, err := expectedCommentCID(projectedIssue.URI, projectedIssue.CID, rootURI, rootCID, replyCommentBody, replyCommentAt)
		if err != nil {
			return err
		}
		if reply.CID != calculatedReplyCID || reply.ParentURI == nil || *reply.ParentURI != rootURI || reply.ParentCID == nil || *reply.ParentCID != rootCID {
			return fmt.Errorf("%s reply envelope/reference is not exact: %+v", testCase.name, reply)
		}
		if !rootDeleted {
			root := projection.Data[0]
			if root.AuthorDID != starAuthorDID || root.IssueURI != projectedIssue.URI || root.IssueCID != projectedIssue.CID || root.Body != rootCommentBody ||
				!root.CreatedAt.Equal(rootCommentAt) || !root.UpdatedAt.Equal(rootCommentAt) {
				return fmt.Errorf("%s projected unexpected root: %+v", testCase.name, root)
			}
		}
		if reply.AuthorDID != starAuthorDID || reply.IssueURI != projectedIssue.URI || reply.IssueCID != projectedIssue.CID || reply.Body != replyCommentBody ||
			!reply.CreatedAt.Equal(replyCommentAt) || !reply.UpdatedAt.Equal(replyCommentAt) {
			return fmt.Errorf("%s projected unexpected reply: %+v", testCase.name, reply)
		}
		got := authoritativeComments(projection.Data)
		if want == nil {
			want = got
		} else if !reflect.DeepEqual(got, want) {
			return fmt.Errorf("%s authoritative comments differ from first AppView: got %+v want %+v", testCase.name, got, want)
		}
	}
	return nil
}

func getFederatedIssue(target instance) (issueView, error) {
	response, err := client.Get(target.url + "/api/v1/issues?repository_uri=" + url.QueryEscape(hostedURI))
	if err != nil {
		return issueView{}, fmt.Errorf("GET issue for comments from %s: %w", target.name, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return issueView{}, err
	}
	if response.StatusCode != http.StatusOK {
		return issueView{}, fmt.Errorf("GET issue for comments from %s status = %d: %s", target.name, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var projection issuePage
	if err := json.Unmarshal(body, &projection); err != nil {
		return issueView{}, err
	}
	if projection.IssueCount != 1 || len(projection.Data) != 1 {
		return issueView{}, fmt.Errorf("%s issue projection for comments is not exact", target.name)
	}
	return projection.Data[0], nil
}

func expectedCommentCID(issueURI, issueCID, parentURI, parentCID, body string, at time.Time) (string, error) {
	type strongRef struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	wire := struct {
		Type      string     `json:"$type"`
		Subject   strongRef  `json:"subject"`
		Parent    *strongRef `json:"parent,omitempty"`
		Body      string     `json:"body"`
		CreatedAt string     `json:"createdAt"`
		UpdatedAt string     `json:"updatedAt"`
	}{Type: commentCollection, Subject: strongRef{URI: issueURI, CID: issueCID}, Body: body, CreatedAt: at.Format(time.RFC3339), UpdatedAt: at.Format(time.RFC3339)}
	if parentURI != "" {
		wire.Parent = &strongRef{URI: parentURI, CID: parentCID}
	}
	record, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("encode expected comment: %w", err)
	}
	digest := sha256.Sum256(record)
	rawCID := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	return "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawCID)), nil
}

func validateCommentURI(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) != 5 || parts[0] != "at:" || parts[2] != starAuthorDID || parts[3] != commentCollection || !compactUUIDv7(parts[4]) {
		return fmt.Errorf("%q is not Bob's canonical comment URI", value)
	}
	return nil
}

func authoritativeComments(values []commentView) []authoritativeComment {
	result := make([]authoritativeComment, len(values))
	for index, value := range values {
		result[index] = authoritativeComment{
			URI: value.URI, CID: value.CID, AuthorDID: value.AuthorDID, IssueURI: value.IssueURI, IssueCID: value.IssueCID,
			ParentURI: optionalString(value.ParentURI), ParentCID: optionalString(value.ParentCID), Body: value.Body,
			CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
	}
	return result
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func compactUUIDv7(value string) bool {
	if len(value) != 32 || value[12] != '7' || !strings.ContainsRune("89ab", rune(value[16])) {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func verifyStars(instances []instance) error {
	rkeyDigest := sha256.Sum256([]byte(starCollection + "\x00" + hostedURI))
	rkey := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rkeyDigest[:]))
	record, err := json.Marshal(struct {
		Type    string `json:"$type"`
		Subject struct {
			URI string `json:"uri"`
			CID string `json:"cid"`
		} `json:"subject"`
		Created string `json:"createdAt"`
	}{
		Type: starCollection,
		Subject: struct {
			URI string `json:"uri"`
			CID string `json:"cid"`
		}{URI: hostedURI, CID: testCID},
		Created: starCreatedAt.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("encode expected star: %w", err)
	}
	digest := sha256.Sum256(record)
	rawCID := append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...)
	wantCID := "b" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawCID))
	wantURI := "at://" + starAuthorDID + "/" + starCollection + "/" + rkey

	for _, target := range instances {
		response, err := client.Get(target.url + "/api/v1/stars?repository_uri=" + url.QueryEscape(hostedURI))
		if err != nil {
			return fmt.Errorf("GET stars from %s: %w", target.name, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read stars from %s: %w", target.name, readErr)
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GET stars from %s status = %d: %s", target.name, response.StatusCode, strings.TrimSpace(string(body)))
		}
		var projection starPage
		if err := json.Unmarshal(body, &projection); err != nil {
			return fmt.Errorf("decode stars from %s: %w", target.name, err)
		}
		if projection.StarCount != 1 || len(projection.Data) != 1 {
			return fmt.Errorf("%s star projection count = %d with %d records, want exactly one", target.name, projection.StarCount, len(projection.Data))
		}
		got := projection.Data[0]
		if got.URI != wantURI || got.CID != wantCID || got.AuthorDID != starAuthorDID ||
			got.RepositoryURI != hostedURI || got.RepositoryCID != testCID || !got.CreatedAt.Equal(starCreatedAt) {
			return fmt.Errorf("%s projected unexpected star: %+v", target.name, got)
		}
		repository, err := findRepository(target, hostedURI, testCID)
		if err != nil {
			return fmt.Errorf("find starred repository on %s: %w", target.name, err)
		}
		if repository.StarCount != 1 {
			return fmt.Errorf("%s network repository star_count = %d, want 1", target.name, repository.StarCount)
		}
	}
	return nil
}

func findRepository(target instance, uri, cid string) (repository, error) {
	var cursor string
	for pageNumber := 1; pageNumber <= 20; pageNumber++ {
		endpoint := target.url + networkPath + "?limit=1"
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		response, err := client.Get(endpoint)
		if err != nil {
			return repository{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return repository{}, readErr
		}
		if response.StatusCode != http.StatusOK {
			return repository{}, fmt.Errorf("GET %s status = %d: %s", networkPath, response.StatusCode, strings.TrimSpace(string(body)))
		}
		var value page
		if err := json.Unmarshal(body, &value); err != nil {
			return repository{}, fmt.Errorf("decode repository page: %w", err)
		}
		for _, candidate := range value.Data {
			if candidate.URI == uri && candidate.CID == cid {
				return candidate, nil
			}
		}
		if value.Page.NextCursor == nil || *value.Page.NextCursor == "" {
			break
		}
		cursor = *value.Page.NextCursor
	}
	return repository{}, fmt.Errorf("repository %s with CID %s not found", uri, cid)
}

func cloneAndVerify(gitURL, wantREADME string) error {
	caPath := requiredEnv("GIT_SSL_CAINFO")
	if _, err := os.Stat(caPath); err != nil {
		return fmt.Errorf("inspect Git TLS CA: %w", err)
	}
	temporary, err := os.MkdirTemp("", "adenosine-federation-clone-")
	if err != nil {
		return fmt.Errorf("create clone directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	clonePath := filepath.Join(temporary, "repository")
	command := exec.Command("git", "-c", "http.sslCAInfo="+caPath, "clone", "--", gitURL, clonePath)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(string(output)))
	}
	contents, err := os.ReadFile(filepath.Join(clonePath, "README.md"))
	if err != nil {
		return fmt.Errorf("read cloned README: %w", err)
	}
	if string(contents) != wantREADME {
		return fmt.Errorf("README = %q, want %q", contents, wantREADME)
	}
	origin := exec.Command("git", "-C", clonePath, "remote", "get-url", "origin")
	output, err := origin.Output()
	if err != nil {
		return fmt.Errorf("read clone origin: %w", err)
	}
	if got := strings.TrimSpace(string(output)); got != gitURL {
		return fmt.Errorf("origin URL = %q, want %q", got, gitURL)
	}
	return nil
}

func deliver(target instance, password, body string) error {
	request, err := http.NewRequest(http.MethodPost, target.url+tapPath, bytes.NewBufferString(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("admin", password)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("status = %d, want 204: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func listAll(target instance) ([]string, error) {
	var slugs []string
	var cursor string
	seenCursors := map[string]bool{}
	for pageNumber := 1; pageNumber <= 20; pageNumber++ {
		endpoint := target.url + networkPath + "?limit=1"
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		response, err := client.Get(endpoint)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode == http.StatusNotFound {
			return nil, errors.New("GET /api/v1/network/repositories returned 404; the Step 23 public endpoint is not available in this source tree")
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s status = %d, want 200: %s", networkPath, response.StatusCode, strings.TrimSpace(string(body)))
		}
		var value page
		if err := json.Unmarshal(body, &value); err != nil {
			return nil, fmt.Errorf("decode repository page: %w", err)
		}
		repositories := value.Data
		if len(repositories) > 1 {
			return nil, fmt.Errorf("page %d returned %d repositories with limit=1", pageNumber, len(repositories))
		}
		for _, repository := range repositories {
			if repository.URI == "" || repository.CID != testCID || repository.Owner.DID == "" || (repository.Hosting.Local && repository.URI != hostedURI && repository.URI != sourceURI) {
				return nil, fmt.Errorf("invalid remote repository identity: %+v", repository)
			}
			slugs = append(slugs, repository.Slug)
		}
		if value.Page.NextCursor == nil || *value.Page.NextCursor == "" {
			sort.Strings(slugs)
			return slugs, nil
		}
		if seenCursors[*value.Page.NextCursor] {
			return nil, fmt.Errorf("pagination repeated cursor %q", *value.Page.NextCursor)
		}
		seenCursors[*value.Page.NextCursor] = true
		cursor = *value.Page.NextCursor
	}
	return nil, errors.New("pagination exceeded 20 pages")
}

func assertSlugs(got, want, forbidden []string) error {
	contains := func(values []string, candidate string) bool {
		for _, value := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	for _, slug := range want {
		if !contains(got, slug) {
			return fmt.Errorf("required slug %q absent from %v", slug, got)
		}
	}
	for _, slug := range forbidden {
		if contains(got, slug) {
			return fmt.Errorf("isolated slug %q unexpectedly present in %v", slug, got)
		}
	}
	return nil
}

func identityEvent(id int, did, handle string) string {
	return fmt.Sprintf(`{"id":%d,"type":"identity","identity":{"did":%q,"handle":%q,"is_active":true,"status":"active"}}`, id, did, handle)
}

func repositoryEvent(id int, did, slug, name string) string {
	record := map[string]any{
		"$type": "dev.adenosine.repo", "slug": slug, "name": name, "defaultBranch": "main",
		"git": map[string]string{"https": "https://code.example/" + slug + ".git"},
		"web": "https://code.example/" + slug, "createdAt": "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T12:00:00Z",
	}
	recordJSON, _ := json.Marshal(record) // The static fixture contains only JSON-supported values.
	return fmt.Sprintf(`{"id":%d,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":%q,"collection":"dev.adenosine.repo","rkey":%q,"action":"create","cid":%q,"record":%s}}`, id, did, slug, testCID, recordJSON)
}

func hostedRepositoryEvent(id int) string {
	record := map[string]any{
		"$type": "dev.adenosine.repo", "slug": "hosted-repo", "name": "Hosted repository", "defaultBranch": "main",
		"description": "Cloned through isolated federation acceptance",
		"git":         map[string]string{"https": hostedGit, "ssh": "ssh://git@adenosine-a:2222/" + hostedDID + "/hosted-repo.git"},
		"web":         strings.TrimSuffix(hostedGit, ".git"), "createdAt": "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T12:00:00Z",
	}
	recordJSON, _ := json.Marshal(record)
	return fmt.Sprintf(`{"id":%d,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":%q,"collection":"dev.adenosine.repo","rkey":%q,"action":"create","cid":%q,"record":%s}}`, id, hostedDID, hostedRKey, testCID, recordJSON)
}

func sourceRepositoryEvent(id int) string {
	record := map[string]any{
		"$type": "dev.adenosine.repo", "slug": "b-only", "name": "B only", "defaultBranch": "main",
		"description": "Federated pull request source repository",
		"git":         map[string]string{"https": sourceGit, "ssh": "ssh://git@adenosine-b:2222/did:plc:bbbbbbbbbbbbbbbbbbbbbbbb/b-only.git"},
		"web":         strings.TrimSuffix(sourceGit, ".git"), "createdAt": "2026-08-09T12:00:00Z", "updatedAt": "2026-08-09T12:00:00Z",
	}
	recordJSON, _ := json.Marshal(record)
	return fmt.Sprintf(`{"id":%d,"type":"record","record":{"live":true,"rev":"3kzfcijpj2z2a","did":"did:plc:bbbbbbbbbbbbbbbbbbbbbbbb","collection":"dev.adenosine.repo","rkey":"b-only","action":"create","cid":%q,"record":%s}}`, id, testCID, recordJSON)
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		fmt.Fprintf(os.Stderr, "%s is required\n", name)
		os.Exit(2)
	}
	return strings.TrimRight(value, "/")
}
