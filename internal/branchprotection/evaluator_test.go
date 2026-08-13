package branchprotection

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type evaluationMemoryStore struct {
	policies          []Protection
	states            map[string]string
	reviews           ReviewSummary
	signers           []AllowedSigner
	observedStatusSHA *string
	observedReviewSHA *string
}

func (store evaluationMemoryStore) Policies(context.Context, repository.ID) ([]Protection, error) {
	return store.policies, nil
}
func (store evaluationMemoryStore) StatusStates(_ context.Context, _ repository.ID, sha string, _ []string) (map[string]string, error) {
	if store.observedStatusSHA != nil {
		*store.observedStatusSHA = sha
	}
	return store.states, nil
}
func (store evaluationMemoryStore) Reviews(_ context.Context, _ repository.ID, _ string, sha string, _ bool) (ReviewSummary, error) {
	if store.observedReviewSHA != nil {
		*store.observedReviewSHA = sha
	}
	return store.reviews, nil
}
func (store evaluationMemoryStore) Signers(context.Context) ([]AllowedSigner, error) {
	return store.signers, nil
}

type evaluationInspector struct {
	ancestor       bool
	commits        []string
	verifyErr      error
	observedOldSHA string
	observedNewSHA string
}

func (inspector *evaluationInspector) IsAncestor(_ context.Context, _ repository.ID, oldSHA, newSHA string) (bool, error) {
	inspector.observedOldSHA, inspector.observedNewSHA = oldSHA, newSHA
	return inspector.ancestor, nil
}
func (inspector *evaluationInspector) NewCommits(_ context.Context, _ repository.ID, oldSHA, newSHA string) ([]string, error) {
	inspector.observedOldSHA, inspector.observedNewSHA = oldSHA, newSHA
	return inspector.commits, nil
}
func (inspector *evaluationInspector) VerifySSHSignatures(context.Context, repository.ID, []string, []AllowedSigner) error {
	return inspector.verifyErr
}

func TestEvaluatorAuthorize(t *testing.T) {
	t.Parallel()
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	evidenceSHA := strings.Repeat("c", 40)
	repositoryID := repository.ID(uuid.MustParse("0198a851-2a89-7ae2-a370-dc68883e3af1"))
	testCases := []struct {
		name       string
		update     RefUpdate
		store      evaluationMemoryStore
		inspector  evaluationInspector
		wantReject bool
		wantReason string
	}{
		{name: "unprotected branch", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/feature"}, store: evaluationMemoryStore{}, inspector: evaluationInspector{ancestor: true}},
		{name: "force push", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", DenyForcePush: true}}}, inspector: evaluationInspector{}, wantReject: true, wantReason: "non-fast-forward"},
		{name: "deletion", update: RefUpdate{OldSHA: oldSHA, NewSHA: strings.Repeat("0", 40), Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", DenyDeletion: true}}}, inspector: evaluationInspector{ancestor: true}, wantReject: true, wantReason: "deletion"},
		{name: "exact status context missing", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequiredStatusChecks: []string{"CI/Test"}}}, states: map[string]string{"ci/test": "success"}}, inspector: evaluationInspector{ancestor: true}, wantReject: true, wantReason: `"CI/Test"`},
		{name: "merge evidence uses pull request head", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main", EvidenceSHA: evidenceSHA}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequiredStatusChecks: []string{"ci/test"}, RequiredApprovals: 1}}, states: map[string]string{"ci/test": "success"}, reviews: ReviewSummary{Found: true, ApprovalCount: 1}}, inspector: evaluationInspector{ancestor: true}},
		{name: "approvals satisfied", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequiredApprovals: 2}}, reviews: ReviewSummary{Found: true, ApprovalCount: 2}}, inspector: evaluationInspector{ancestor: true}},
		{name: "changes requested", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequiredApprovals: 1}}, reviews: ReviewSummary{Found: true, ApprovalCount: 2, ChangesRequested: true}}, inspector: evaluationInspector{ancestor: true}, wantReject: true, wantReason: "requests changes"},
		{name: "missing pull request", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequiredApprovals: 1}}}, inspector: evaluationInspector{ancestor: true}, wantReject: true, wantReason: "open pull request"},
		{name: "unsigned commit", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/heads/main"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "main", RequireSignedCommits: true}}, signers: []AllowedSigner{{Principal: "did:plc:alice", PublicKey: "ssh-ed25519 AAAA"}}}, inspector: evaluationInspector{commits: []string{newSHA}, verifyErr: errors.New("unsigned")}, wantReject: true, wantReason: "trusted SSH signature"},
		{name: "tag is outside branch policy", update: RefUpdate{OldSHA: oldSHA, NewSHA: newSHA, Ref: "refs/tags/v1"}, store: evaluationMemoryStore{policies: []Protection{{Pattern: "*", DenyDeletion: true}}}, inspector: evaluationInspector{ancestor: true}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			inspector := testCase.inspector
			var statusSHA, reviewSHA string
			if testCase.update.EvidenceSHA != "" {
				testCase.store.observedStatusSHA = &statusSHA
				testCase.store.observedReviewSHA = &reviewSHA
			}
			err := newEvaluator(testCase.store, &inspector).Authorize(context.Background(), repositoryID, []RefUpdate{testCase.update})
			var rejection *Rejection
			if errors.As(err, &rejection) != testCase.wantReject {
				t.Fatalf("Authorize() error = %v, want rejection %v", err, testCase.wantReject)
			}
			if testCase.wantReason != "" && !strings.Contains(err.Error(), testCase.wantReason) {
				t.Fatalf("Authorize() error = %v, want reason %q", err, testCase.wantReason)
			}
			if inspector.observedOldSHA != "" && (inspector.observedOldSHA != testCase.update.OldSHA || inspector.observedNewSHA != testCase.update.NewSHA) {
				t.Fatalf("inspector object IDs = %s/%s, want %s/%s", inspector.observedOldSHA, inspector.observedNewSHA, testCase.update.OldSHA, testCase.update.NewSHA)
			}
			if testCase.update.EvidenceSHA != "" && (statusSHA != testCase.update.EvidenceSHA || reviewSHA != testCase.update.EvidenceSHA) {
				t.Fatalf("policy evidence object IDs = %s/%s, want %s", statusSHA, reviewSHA, testCase.update.EvidenceSHA)
			}
		})
	}
}
