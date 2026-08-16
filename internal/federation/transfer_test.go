package federation

import (
	"testing"

	"github.com/adenosine-dev/adenosine/internal/transfer"
)

func TestDecodeRepositoryTransferRecords(t *testing.T) {
	sourceURI := "at://" + testDID + "/" + RepositoryCollection + "/source"
	proposalRKey := "0198a8512a897ae2a370dc68883e3af4"
	proposalURI := "at://" + testDID + "/" + RepositoryTransferCollection + "/" + proposalRKey
	successorURI := "at://" + testBobDID + "/" + RepositoryCollection + "/successor"
	acceptanceRKey := transfer.AcceptanceRecordKey(proposalURI)
	proposal := `{"$type":"` + RepositoryTransferCollection + `","repository":{"uri":"` + sourceURI + `","cid":"` + testCID + `"},"destinationDID":"` + testBobDID + `","destinationOwner":"bob.test","createdAt":"2026-08-09T12:00:00Z","expiresAt":"2026-08-16T12:00:00Z"}`
	acceptance := `{"$type":"` + RepositoryTransferAcceptanceCollection + `","proposal":{"uri":"` + proposalURI + `","cid":"` + testCID + `"},"repository":{"uri":"` + successorURI + `","cid":"` + testCID + `"},"createdAt":"2026-08-10T12:00:00Z"}`
	testCases := []struct {
		name, did, collection, rkey, record string
		wantProposal, wantAcceptance        bool
		wantError                           bool
	}{
		{name: "proposal", did: testDID, collection: RepositoryTransferCollection, rkey: proposalRKey, record: proposal, wantProposal: true},
		{name: "acceptance", did: testBobDID, collection: RepositoryTransferAcceptanceCollection, rkey: acceptanceRKey, record: acceptance, wantAcceptance: true},
		{name: "proposal key shape", did: testDID, collection: RepositoryTransferCollection, rkey: "wrong", record: proposal, wantError: true},
		{name: "acceptance key binding", did: testBobDID, collection: RepositoryTransferAcceptanceCollection, rkey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", record: acceptance, wantError: true},
		{name: "proposal expiry order", did: testDID, collection: RepositoryTransferCollection, rkey: proposalRKey, record: `{"$type":"` + RepositoryTransferCollection + `","repository":{"uri":"` + sourceURI + `","cid":"` + testCID + `"},"destinationDID":"` + testBobDID + `","destinationOwner":"bob.test","createdAt":"2026-08-16T12:00:00Z","expiresAt":"2026-08-09T12:00:00Z"}`, wantError: true},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			event, err := DecodeEvent([]byte(recordEnvelopeForDID(int64(index+80), testCase.did, testCase.collection, testCase.rkey, "create", testCase.record)))
			if (err != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, want error %v", err, testCase.wantError)
			}
			if err == nil && ((event.Record.RepositoryTransfer != nil) != testCase.wantProposal || (event.Record.RepositoryTransferAcceptance != nil) != testCase.wantAcceptance) {
				t.Fatalf("decoded transfer = %#v", event.Record)
			}
		})
	}
}

func TestDecodeRepositoryTransferLinks(t *testing.T) {
	predecessor := "at://" + testDID + "/" + RepositoryCollection + "/source"
	successor := "at://" + testBobDID + "/" + RepositoryCollection + "/successor"
	testCases := []struct {
		name, from, to string
		wantError      bool
	}{
		{name: "predecessor", from: predecessor},
		{name: "successor", to: successor},
		{name: "both distinct", from: predecessor, to: successor},
		{name: "same predecessor and successor", from: predecessor, to: predecessor, wantError: true},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			links := ""
			if testCase.from != "" {
				links += `,"transferredFrom":{"uri":"` + testCase.from + `","cid":"` + testCID + `"}`
			}
			if testCase.to != "" {
				links += `,"transferredTo":{"uri":"` + testCase.to + `","cid":"` + testCID + `"}`
			}
			record := `{"$type":"` + RepositoryCollection + `","slug":"project","name":"Project","defaultBranch":"main","git":{"https":"https://code.example/project.git"},"web":"https://code.example/project","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"` + links + `}`
			_, err := DecodeEvent([]byte(recordEnvelope(int64(index+90), RepositoryCollection, "project", "update", record)))
			if (err != nil) != testCase.wantError {
				t.Fatalf("DecodeEvent() error = %v, want error %v", err, testCase.wantError)
			}
		})
	}
}
