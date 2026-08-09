package federation

import (
	"context"
	"errors"
	"testing"

	"github.com/adenosine-dev/adenosine/internal/star"
)

type memoryRecord struct {
	eventID int64
	value   *RepositoryRecord
	deleted bool
}

type memoryStore struct {
	receipts map[int64]string
	records  map[string]memoryRecord
	stars    map[string]memoryStar
	counts   map[string]int
	cursor   int64
	fail     error
}

type memoryStar struct {
	eventID       int64
	repositoryURI string
	repositoryCID string
	deleted       bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		receipts: make(map[int64]string), records: make(map[string]memoryRecord),
		stars: make(map[string]memoryStar), counts: make(map[string]int),
	}
}

func (store *memoryStore) Store(_ context.Context, event Event, rejection string) (bool, error) {
	if store.fail != nil {
		err := store.fail
		store.fail = nil
		return false, err
	}
	if _, ok := store.receipts[event.ID]; ok {
		return true, nil
	}
	if event.Record != nil && event.Record.Collection == RepositoryCollection {
		current := store.records[event.Record.URI]
		if event.ID > current.eventID {
			store.records[event.Record.URI] = memoryRecord{
				eventID: event.ID, value: event.Record.Repository, deleted: event.Record.Action == "delete",
			}
		}
		store.recomputeStars(event.Record.URI)
	}
	if event.Record != nil && event.Record.Collection == StarCollection {
		current, exists := store.stars[event.Record.URI]
		if event.ID > current.eventID {
			if event.Record.Action == "delete" {
				if exists {
					current.eventID = event.ID
					current.deleted = true
					store.stars[event.Record.URI] = current
					store.recomputeStars(current.repositoryURI)
				}
			} else {
				store.stars[event.Record.URI] = memoryStar{
					eventID: event.ID, repositoryURI: event.Record.Star.RepositoryURI,
					repositoryCID: event.Record.Star.RepositoryCID,
				}
				store.recomputeStars(event.Record.Star.RepositoryURI)
			}
		}
	}
	store.receipts[event.ID] = rejection
	if event.ID > store.cursor {
		store.cursor = event.ID
	}
	return false, nil
}

func (store *memoryStore) recomputeStars(repositoryURI string) {
	repository, exists := store.records[repositoryURI]
	if !exists || repository.deleted {
		return
	}
	count := 0
	for _, value := range store.stars {
		if value.repositoryURI == repositoryURI && !value.deleted {
			count++
		}
	}
	store.counts[repositoryURI] = count
}

func TestProcessorReplayCreateUpdateDeleteAndStaleGuards(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "at least once lifecycle"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryStore()
			processor := &Processor{store: store}
			create := repositoryRecord("Project")
			update := repositoryRecord("Renamed")
			events := []string{
				recordEnvelope(10, RepositoryCollection, "project", "create", create),
				recordEnvelope(10, RepositoryCollection, "project", "create", create),
				recordEnvelope(12, RepositoryCollection, "project", "update", update),
				recordEnvelope(11, RepositoryCollection, "project", "delete", ""),
				recordEnvelope(13, RepositoryCollection, "project", "delete", ""),
				recordEnvelope(9, RepositoryCollection, "project", "create", create),
			}
			for index, body := range events {
				result, err := processor.Process(context.Background(), []byte(body))
				if err != nil {
					t.Fatalf("event %d: %v", index, err)
				}
				if index == 1 && !result.Duplicate {
					t.Fatal("replayed receipt was not a no-op")
				}
				if index == 2 {
					uri := "at://" + testDID + "/" + RepositoryCollection + "/project"
					projection := store.records[uri]
					if projection.deleted || projection.value == nil || projection.value.Name != "Renamed" || projection.eventID != 12 {
						t.Fatalf("updated projection = %#v", projection)
					}
				}
			}
			uri := "at://" + testDID + "/" + RepositoryCollection + "/project"
			projection := store.records[uri]
			if !projection.deleted || projection.eventID != 13 {
				t.Fatalf("projection = %#v", projection)
			}
			if store.cursor != 13 || len(store.receipts) != 5 {
				t.Fatalf("cursor/receipts = %d/%d", store.cursor, len(store.receipts))
			}
		})
	}
}

func TestProcessorDurablyRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
		body string
	}{
		{name: "wrong record type", body: recordEnvelope(21, ProfileCollection, "self", "create", `{"$type":"wrong.type","createdAt":"2026-08-09T12:00:00Z"}`)},
		{name: "unsupported collection", body: recordEnvelope(22, "com.example.record", "value", "create", `{"$type":"com.example.record"}`)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryStore()
			processor := &Processor{store: store}
			result, err := processor.Process(context.Background(), []byte(testCase.body))
			if err != nil || result.Outcome != "rejected" || result.Rejection == "" {
				t.Fatalf("result/error = %#v / %v", result, err)
			}
			replayed, err := processor.Process(context.Background(), []byte(testCase.body))
			if err != nil || !replayed.Duplicate || store.receipts[result.EventID] == "" {
				t.Fatalf("replay/receipt = %#v / %q / %v", replayed, store.receipts[result.EventID], err)
			}
		})
	}
}

func TestProcessorReturnsStorageFailuresForRetry(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{
		{name: "transient transaction failure"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryStore()
			store.fail = errors.New("database unavailable")
			processor := &Processor{store: store}
			body := recordEnvelope(30, ProfileCollection, "self", "create", `{"$type":"dev.adenosine.profile","createdAt":"2026-08-09T12:00:00Z"}`)
			if _, err := processor.Process(context.Background(), []byte(body)); err == nil || len(store.receipts) != 0 {
				t.Fatalf("first processing error/receipts = %v/%d", err, len(store.receipts))
			}
			result, err := processor.Process(context.Background(), []byte(body))
			if err != nil || result.Duplicate || result.Outcome != "applied" {
				t.Fatalf("retry result/error = %#v / %v", result, err)
			}
		})
	}
}

func TestProcessorStarReplayLifecycleStaleAndUnknownDelete(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	starRKey, err := star.RecordKey(repositoryURI)
	if err != nil {
		t.Fatal(err)
	}
	starURI := "at://" + testDID + "/" + StarCollection + "/" + starRKey
	create := starRecord(repositoryURI, testCID, "2026-08-09T12:00:00Z")
	updatedCID := "bafybeihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"
	update := starRecord(repositoryURI, updatedCID, "2026-08-09T12:00:00Z")
	testCases := []struct {
		name          string
		events        []string
		wantEventID   int64
		wantCID       string
		wantDeleted   bool
		wantStars     int
		wantReceipts  int
		wantDuplicate bool
	}{
		{
			name: "create replay update stale create and delete",
			events: []string{
				recordEnvelope(50, RepositoryCollection, "project", "create", repositoryRecord("Project")),
				recordEnvelope(51, StarCollection, starRKey, "create", create),
				recordEnvelope(51, StarCollection, starRKey, "create", create),
				recordEnvelope(53, StarCollection, starRKey, "update", update),
				recordEnvelope(52, StarCollection, starRKey, "create", create),
				recordEnvelope(54, StarCollection, starRKey, "delete", ""),
			},
			wantEventID: 54, wantCID: updatedCID, wantDeleted: true, wantReceipts: 5, wantDuplicate: true,
		},
		{
			name: "unknown delete leaves typed stars empty",
			events: []string{
				recordEnvelope(60, StarCollection, starRKey, "delete", ""),
			},
			wantReceipts: 1,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryStore()
			processor := &Processor{store: store}
			duplicateSeen := false
			for _, body := range testCase.events {
				result, err := processor.Process(context.Background(), []byte(body))
				if err != nil {
					t.Fatal(err)
				}
				duplicateSeen = duplicateSeen || result.Duplicate
			}
			projection, exists := store.stars[starURI]
			if testCase.wantEventID == 0 {
				if exists {
					t.Fatalf("unexpected typed star = %#v", projection)
				}
			} else if !exists || projection.eventID != testCase.wantEventID || projection.repositoryCID != testCase.wantCID || projection.deleted != testCase.wantDeleted {
				t.Fatalf("typed star = %#v", projection)
			}
			if store.counts[repositoryURI] != testCase.wantStars || len(store.receipts) != testCase.wantReceipts || duplicateSeen != testCase.wantDuplicate {
				t.Fatalf("count/receipts/duplicate = %d/%d/%t", store.counts[repositoryURI], len(store.receipts), duplicateSeen)
			}
		})
	}
}

func TestProcessorRecomputesStarsWhenRepositoryArrivesAfterStar(t *testing.T) {
	t.Parallel()
	repositoryURI := "at://" + testDID + "/" + RepositoryCollection + "/project"
	starRKey, err := star.RecordKey(repositoryURI)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name   string
		events []string
	}{
		{
			name: "star before repository",
			events: []string{
				recordEnvelope(70, StarCollection, starRKey, "create", starRecord(repositoryURI, testCID, "2026-08-09T12:00:00Z")),
				recordEnvelope(71, RepositoryCollection, "project", "create", repositoryRecord("Project")),
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := newMemoryStore()
			processor := &Processor{store: store}
			for _, body := range testCase.events {
				if _, err := processor.Process(context.Background(), []byte(body)); err != nil {
					t.Fatal(err)
				}
			}
			if store.counts[repositoryURI] != 1 {
				t.Fatalf("star count = %d", store.counts[repositoryURI])
			}
		})
	}
}

func repositoryRecord(name string) string {
	return `{"$type":"dev.adenosine.repo","slug":"project","name":"` + name + `","defaultBranch":"main","git":{"https":"https://code.example/project.git"},"web":"https://code.example/project","createdAt":"2026-08-09T12:00:00Z","updatedAt":"2026-08-09T12:00:00Z"}`
}
