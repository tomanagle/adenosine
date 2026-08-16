package atproto

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/triage"
)

func TestPublishRepositoryLabelUsesCallerCAS(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		swapCID   string
		record    triage.LabelRecord
		wantErr   error
		wantSwap  *string
		wantPosts int
	}{
		{name: "create uses explicit absent CAS", record: triageLabelRecord(), wantPosts: 1},
		{name: "update uses projected CID", swapCID: profileCID, record: triageLabelRecord(), wantSwap: stringPointer(profileCID), wantPosts: 1},
		{name: "forged repository authority is rejected before provider", record: triage.LabelRecord{Repository: triage.StrongRef{URI: "at://did:plc:other/dev.adenosine.repo/project", CID: profileCID}, Name: "bug", Color: "a0b1c2", CreatedAt: issueTime, UpdatedAt: issueTime}, wantErr: triage.ErrAuthorization},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			uri := "at://" + canonicalDID + "/" + triage.LabelCollection + "/" + issueRKey
			api := &fakeStarAPI{putOutput: &putRecordOutput{URI: uri, CID: profileCID}}
			store := &starSessionStore{}
			client, _ := newStarClient(t, api, store)
			var err error
			if testCase.swapCID == "" {
				_, err = client.CreateLabel(context.Background(), canonicalDID, issueRKey, testCase.record)
			} else {
				_, err = client.PutLabel(context.Background(), canonicalDID, issueRKey, testCase.swapCID, testCase.record)
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if len(api.postCalls) != testCase.wantPosts {
				t.Fatalf("post calls = %d, want %d", len(api.postCalls), testCase.wantPosts)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(triagePutRecordInput)
				if !ok || input.Collection != triage.LabelCollection || input.RKey != issueRKey || !equalStringPointers(input.SwapRecord, testCase.wantSwap) {
					t.Fatalf("input = %#v", api.postCalls[0].input)
				}
			}
		})
	}
}

func TestDeleteTriageRecordUsesCompareAndSwap(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name       string
		collection string
		wantErr    error
		wantPosts  int
	}{
		{name: "deletes a label with projected CID", collection: triage.LabelCollection, wantPosts: 1},
		{name: "rejects an unrelated collection", collection: "com.example.record", wantErr: triage.ErrValidation},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			api := &fakeStarAPI{}
			client, _ := newStarClient(t, api, &starSessionStore{})
			err := client.DeleteTriageRecord(context.Background(), canonicalDID, testCase.collection, issueRKey, profileCID)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("error = %v, want %v", err, testCase.wantErr)
			}
			if len(api.postCalls) != testCase.wantPosts {
				t.Fatalf("post calls = %d, want %d", len(api.postCalls), testCase.wantPosts)
			}
			if testCase.wantPosts == 1 {
				input, ok := api.postCalls[0].input.(triageDeleteRecordInput)
				if !ok || input.Collection != triage.LabelCollection || input.RKey != issueRKey || input.SwapRecord != profileCID {
					t.Fatalf("input = %#v", api.postCalls[0].input)
				}
			}
		})
	}
}

func triageLabelRecord() triage.LabelRecord {
	return triage.LabelRecord{
		Repository: triage.StrongRef{URI: "at://" + canonicalDID + "/dev.adenosine.repo/project", CID: profileCID},
		Name:       "bug", Color: "a0b1c2", Description: "Broken behavior",
		CreatedAt: time.Date(2026, time.August, 16, 1, 2, 3, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.August, 16, 1, 2, 3, 0, time.UTC),
	}
}
