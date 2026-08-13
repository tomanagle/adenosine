package atproto

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type fixedClock struct{ now time.Time }

func (clock *fixedClock) Now() time.Time { return clock.now }

type oauthStateRow struct {
	hash      []byte
	encrypted []byte
	createdAt time.Time
	expiresAt time.Time
}

type oauthCredentialRow struct {
	did       string
	hash      []byte
	encrypted []byte
	createdAt time.Time
	updatedAt time.Time
}

type memoryOAuthDB struct {
	mu          sync.Mutex
	rows        map[string]oauthStateRow
	credentials map[string]oauthCredentialRow
}

func newMemoryOAuthDB() *memoryOAuthDB {
	return &memoryOAuthDB{rows: make(map[string]oauthStateRow), credentials: make(map[string]oauthCredentialRow)}
}

func (db *memoryOAuthDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if strings.Contains(query, "INSERT INTO auth.oauth_states") {
		key := string(args[0].([]byte))
		if _, exists := db.rows[key]; exists {
			return pgconn.CommandTag{}, errors.New("duplicate OAuth state")
		}
		db.rows[key] = oauthStateRow{
			hash:      bytes.Clone(args[0].([]byte)),
			encrypted: bytes.Clone(args[1].([]byte)),
			createdAt: args[2].(pgtype.Timestamptz).Time,
			expiresAt: args[3].(pgtype.Timestamptz).Time,
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(query, "INSERT INTO auth.oauth_credentials") {
		did := args[0].(string)
		hash := args[1].([]byte)
		key := credentialRowKey(did, hash)
		createdAt := args[3].(pgtype.Timestamptz).Time
		if existing, ok := db.credentials[key]; ok {
			createdAt = existing.createdAt
		}
		db.credentials[key] = oauthCredentialRow{
			did: did, hash: bytes.Clone(hash), encrypted: bytes.Clone(args[2].([]byte)),
			createdAt: createdAt, updatedAt: args[4].(pgtype.Timestamptz).Time,
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if strings.Contains(query, "DELETE FROM auth.oauth_states") {
		key := string(args[0].([]byte))
		delete(db.rows, key)
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	if strings.Contains(query, "DELETE FROM auth.oauth_credentials") {
		delete(db.credentials, credentialRowKey(args[0].(string), args[1].([]byte)))
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (*memoryOAuthDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db *memoryOAuthDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.mu.Lock()
	defer db.mu.Unlock()
	if strings.Contains(query, "FROM auth.oauth_credentials") && strings.Contains(query, "ORDER BY updated_at DESC") {
		var latest oauthCredentialRow
		found := false
		for _, row := range db.credentials {
			if row.did == args[0].(string) && (!found || row.updatedAt.After(latest.updatedAt)) {
				latest, found = row, true
			}
		}
		if !found {
			return oauthStubRow{err: pgx.ErrNoRows}
		}
		return oauthStubRow{values: []any{bytes.Clone(latest.hash), bytes.Clone(latest.encrypted)}}
	}
	if strings.Contains(query, "FROM auth.oauth_credentials") {
		row, exists := db.credentials[credentialRowKey(args[0].(string), args[1].([]byte))]
		if !exists {
			return oauthStubRow{err: pgx.ErrNoRows}
		}
		return oauthStubRow{values: []any{bytes.Clone(row.encrypted)}}
	}
	if !strings.Contains(query, "expires_at > $2") || !strings.Contains(query, "RETURNING encrypted_payload") {
		return oauthStubRow{err: errors.New("consume query is not atomic and expiry-bound")}
	}
	key := string(args[0].([]byte))
	row, exists := db.rows[key]
	activeAt := args[1].(pgtype.Timestamptz).Time
	if !exists || !row.expiresAt.After(activeAt) {
		return oauthStubRow{err: pgx.ErrNoRows}
	}
	delete(db.rows, key)
	return oauthStubRow{values: []any{bytes.Clone(row.encrypted)}}
}

func (db *memoryOAuthDB) GetLatestOAuthCredential(_ context.Context, did string) ([]byte, []byte, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var latest oauthCredentialRow
	found := false
	for _, row := range db.credentials {
		if row.did == did && (!found || row.updatedAt.After(latest.updatedAt)) {
			latest = row
			found = true
		}
	}
	if !found {
		return nil, nil, pgx.ErrNoRows
	}
	return bytes.Clone(latest.hash), bytes.Clone(latest.encrypted), nil
}

func credentialRowKey(did string, hash []byte) string { return did + "\x00" + string(hash) }

func (db *memoryOAuthDB) onlyRow(t *testing.T) oauthStateRow {
	t.Helper()
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(db.rows))
	}
	for _, row := range db.rows {
		return row
	}
	return oauthStateRow{}
}

type oauthStubRow struct {
	values []any
	err    error
}

func (row oauthStubRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index := range destinations {
		reflect.ValueOf(destinations[index]).Elem().Set(reflect.ValueOf(row.values[index]))
	}
	return nil
}

func newTestStore(t *testing.T, db *memoryOAuthDB, clock *fixedClock) *PostgresClientAuthStore {
	t.Helper()
	store, err := buildPostgresClientAuthStore(
		dbgen.New(db),
		bytes.Repeat([]byte{0x42}, 32),
		bytes.Repeat([]byte{0x43}, 32),
		clock,
		rand.Reader,
		db,
	)
	if err != nil {
		t.Fatalf("build store: %v", err)
	}
	return store
}

func testRequest(state string) oauth.AuthRequestData {
	return oauth.AuthRequestData{
		State:                   state,
		AuthServerURL:           "https://auth.example",
		Scopes:                  []string{"atproto"},
		RequestURI:              "urn:request:secret",
		AuthServerTokenEndpoint: "https://auth.example/token",
		PKCEVerifier:            "pkce-super-secret",
		DPoPAuthServerNonce:     "nonce-super-secret",
		DPoPPrivateKeyMultibase: "private-key-super-secret",
	}
}

func TestPostgresClientAuthStoreEncryptsRequestState(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name string
	}{{
		name: "encrypted state",
	}}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, &fixedClock{now: now})
			info := testRequest("state-super-secret")
			if err := store.SaveAuthRequestInfo(context.Background(), info); err != nil {
				t.Fatalf("save request: %v", err)
			}
			row := db.onlyRow(t)
			hash := sha256.Sum256([]byte(info.State))
			if !bytes.Equal(row.hash, hash[:]) {
				t.Fatalf("stored state hash = %x", row.hash)
			}
			for _, plaintext := range []string{info.State, info.PKCEVerifier, info.DPoPAuthServerNonce, info.DPoPPrivateKeyMultibase, info.RequestURI} {
				if bytes.Contains(row.encrypted, []byte(plaintext)) {
					t.Fatalf("encrypted payload contains plaintext %q", plaintext)
				}
				if bytes.Contains(row.hash, []byte(plaintext)) {
					t.Fatalf("state hash contains plaintext %q", plaintext)
				}
			}
			if !row.createdAt.Equal(now) || !row.expiresAt.Equal(now.Add(10*time.Minute)) {
				t.Fatalf("stored times = %v, %v", row.createdAt, row.expiresAt)
			}
			consumed, err := store.GetAuthRequestInfo(context.Background(), info.State)
			if err != nil {
				t.Fatalf("consume request: %v", err)
			}
			if !reflect.DeepEqual(*consumed, info) {
				t.Fatalf("consumed info = %#v", consumed)
			}
		})
	}
}

func TestPostgresClientAuthStoreDetectsTamperingAndPayloadMismatch(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		mutate func(*PostgresClientAuthStore, *memoryOAuthDB, string)
	}{
		{name: "ciphertext", mutate: func(_ *PostgresClientAuthStore, db *memoryOAuthDB, state string) {
			hash := sha256.Sum256([]byte(state))
			row := db.rows[string(hash[:])]
			row.encrypted[len(row.encrypted)-1] ^= 0xff
			db.rows[string(hash[:])] = row
		}},
		{name: "payload state", mutate: func(store *PostgresClientAuthStore, db *memoryOAuthDB, state string) {
			hash := sha256.Sum256([]byte(state))
			row := db.rows[string(hash[:])]
			payload := []byte(`{"state":"different"}`)
			nonce := row.encrypted[:store.stateAEAD.NonceSize()]
			row.encrypted = store.stateAEAD.Seal(bytes.Clone(nonce), nonce, payload, hash[:])
			db.rows[string(hash[:])] = row
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newMemoryOAuthDB()
			clock := &fixedClock{now: time.Now()}
			store := newTestStore(t, db, clock)
			state := "expected-state"
			if err := store.SaveAuthRequestInfo(context.Background(), testRequest(state)); err != nil {
				t.Fatal(err)
			}
			db.mu.Lock()
			testCase.mutate(store, db, state)
			db.mu.Unlock()
			_, err := store.GetAuthRequestInfo(context.Background(), state)
			if !errors.Is(err, ErrStateInvalid) {
				t.Fatalf("error = %v, want ErrStateInvalid", err)
			}
		})
	}
}

func TestPostgresClientAuthStoreConsumesOnlyOnceUnderConcurrency(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "32 concurrent consumers"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, &fixedClock{now: time.Now()})
			const state = "one-use-state"
			if err := store.SaveAuthRequestInfo(context.Background(), testRequest(state)); err != nil {
				t.Fatal(err)
			}
			var successes atomic.Int32
			var failures atomic.Int32
			var wait sync.WaitGroup
			for range 32 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					_, err := store.GetAuthRequestInfo(context.Background(), state)
					if err == nil {
						successes.Add(1)
					} else if errors.Is(err, ErrStateNotFound) {
						failures.Add(1)
					} else {
						t.Errorf("consume error: %v", err)
					}
				}()
			}
			wait.Wait()
			if successes.Load() != 1 || failures.Load() != 31 {
				t.Fatalf("successes = %d, not found = %d", successes.Load(), failures.Load())
			}
		})
	}
}

func TestPostgresClientAuthStoreRejectsExpiredState(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "expired state"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			clock := &fixedClock{now: now}
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, clock)
			if err := store.SaveAuthRequestInfo(context.Background(), testRequest("expired-state")); err != nil {
				t.Fatal(err)
			}
			clock.now = now.Add(10 * time.Minute)
			if _, err := store.GetAuthRequestInfo(context.Background(), "expired-state"); !errors.Is(err, ErrStateNotFound) {
				t.Fatalf("expired error = %v", err)
			}
			if err := store.DeleteAuthRequestInfo(context.Background(), "missing"); err != nil {
				t.Fatalf("delete missing request: %v", err)
			}
		})
	}
}

func TestBuildPostgresClientAuthStoreRequiresAES256Key(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		stateSize      int
		credentialSize int
		wantErr        bool
	}{
		{name: "credential key empty", stateSize: 32, credentialSize: 0, wantErr: true},
		{name: "state key empty", credentialSize: 32, wantErr: true},
		{name: "credential key 16 bytes", stateSize: 32, credentialSize: 16, wantErr: true},
		{name: "state key 16 bytes", stateSize: 16, credentialSize: 32, wantErr: true},
		{name: "credential key 24 bytes", stateSize: 32, credentialSize: 24, wantErr: true},
		{name: "state key 24 bytes", stateSize: 24, credentialSize: 32, wantErr: true},
		{name: "credential key 31 bytes", stateSize: 32, credentialSize: 31, wantErr: true},
		{name: "state key 31 bytes", stateSize: 31, credentialSize: 32, wantErr: true},
		{name: "credential key 33 bytes", stateSize: 32, credentialSize: 33, wantErr: true},
		{name: "state key 33 bytes", stateSize: 33, credentialSize: 32, wantErr: true},
		{name: "both keys 32 bytes", stateSize: 32, credentialSize: 32},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := buildPostgresClientAuthStore(dbgen.New(newMemoryOAuthDB()), make([]byte, testCase.stateSize), make([]byte, testCase.credentialSize), &fixedClock{}, bytes.NewReader(nil))
			if testCase.wantErr && err == nil {
				t.Fatalf("state/credential key sizes %d/%d accepted", testCase.stateSize, testCase.credentialSize)
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("32-byte key rejected: %v", err)
			}
		})
	}
}

func TestPostgresClientAuthStoreEncryptsAndIsolatesResumableSessions(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "two isolated sessions"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			clock := &fixedClock{now: now}
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, clock)
			did := syntax.DID("did:plc:abcdefghijklmnopqrstuvwx")
			first := oauth.ClientSessionData{
				AccountDID: did, SessionID: "browser-one", Scopes: []string{"atproto"},
				AccessToken: "access-secret", RefreshToken: "refresh-secret", DPoPPrivateKeyMultibase: "private-secret",
			}
			second := first
			second.SessionID = "browser-two"
			second.AccessToken = "other-access-secret"
			if err := store.SaveSession(context.Background(), first); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveSession(context.Background(), second); err != nil {
				t.Fatal(err)
			}
			if len(db.credentials) != 2 {
				t.Fatalf("credential rows = %d, want 2", len(db.credentials))
			}
			for _, row := range db.credentials {
				for _, secret := range []string{row.did, first.SessionID, second.SessionID, first.AccessToken, first.RefreshToken, first.DPoPPrivateKeyMultibase} {
					if secret != row.did && bytes.Contains(row.encrypted, []byte(secret)) {
						t.Fatalf("encrypted credentials contain %q", secret)
					}
				}
				if !row.createdAt.Equal(now) || !row.updatedAt.Equal(now) {
					t.Fatalf("credential timestamps = %v, %v", row.createdAt, row.updatedAt)
				}
			}
			loaded, err := store.GetSession(context.Background(), did, first.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*loaded, first) {
				t.Fatalf("loaded session = %#v", loaded)
			}
			if _, err := store.GetSession(context.Background(), did, "missing"); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("missing session error = %v", err)
			}
			if err := store.DeleteSession(context.Background(), did, first.SessionID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.GetSession(context.Background(), did, first.SessionID); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("deleted session error = %v", err)
			}
		})
	}
}

func TestPostgresClientAuthStoreUsesFreshNonceAndIdentityAAD(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "nonce and identity AAD"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, &fixedClock{now: time.Now()})
			did := syntax.DID("did:plc:abcdefghijklmnopqrstuvwx")
			session := oauth.ClientSessionData{AccountDID: did, SessionID: "browser", Scopes: []string{"atproto"}, AccessToken: "secret"}
			if err := store.SaveSession(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(session.SessionID))
			first := bytes.Clone(db.credentials[credentialRowKey(did.String(), hash[:])].encrypted)
			if err := store.SaveSession(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			second := db.credentials[credentialRowKey(did.String(), hash[:])].encrypted
			if bytes.Equal(first, second) {
				t.Fatal("credential upsert reused nonce/ciphertext")
			}
			otherDID := syntax.DID("did:plc:zyxwvutsrqponmlkjihgfedc")
			db.credentials[credentialRowKey(otherDID.String(), hash[:])] = oauthCredentialRow{encrypted: bytes.Clone(second)}
			if _, err := store.GetSession(context.Background(), otherDID, session.SessionID); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("AAD identity substitution error = %v", err)
			}
		})
	}
}

func TestPostgresClientAuthStoreLoadsLatestSessionUsingHashAAD(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name string }{{name: "latest session"}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
			clock := &fixedClock{now: now}
			db := newMemoryOAuthDB()
			store := newTestStore(t, db, clock)
			did := syntax.DID(canonicalDID)
			first := oauth.ClientSessionData{AccountDID: did, SessionID: "first-secret-session", AccessToken: "first-token"}
			second := oauth.ClientSessionData{AccountDID: did, SessionID: "latest-secret-session", AccessToken: "latest-token"}
			if err := store.SaveSession(context.Background(), first); err != nil {
				t.Fatal(err)
			}
			clock.now = now.Add(time.Second)
			if err := store.SaveSession(context.Background(), second); err != nil {
				t.Fatal(err)
			}
			loaded, err := store.GetLatestSession(context.Background(), did)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.SessionID != second.SessionID || loaded.AccessToken != second.AccessToken {
				t.Fatalf("latest session = %#v", loaded)
			}

			hash := sha256.Sum256([]byte(second.SessionID))
			row := db.credentials[credentialRowKey(did.String(), hash[:])]
			nonce := row.encrypted[:store.credentialAEAD.NonceSize()]
			ciphertext := row.encrypted[store.credentialAEAD.NonceSize():]
			if _, err := store.credentialAEAD.Open(nil, nonce, ciphertext, sessionAAD(did.String(), []byte(second.SessionID))); err == nil {
				t.Fatal("credential envelope authenticated with raw session ID AAD")
			}
			row.hash[0] ^= 0xff
			db.credentials[credentialRowKey(did.String(), hash[:])] = row
			if _, err := store.GetLatestSession(context.Background(), did); !errors.Is(err, ErrSessionInvalid) {
				t.Fatalf("tampered latest hash error = %v", err)
			}
		})
	}
}

func TestPostgresClientAuthStoreLoadsLatestSessionForEveryLoaderWiring(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name   string
		latest func(*memoryOAuthDB) []latestCredentialLoader
	}{
		{
			name:   "loader omitted",
			latest: func(*memoryOAuthDB) []latestCredentialLoader { return nil },
		},
		{
			// The client forwards an optional loader from its build options, so an
			// absent override reaches the store as a nil interface.
			name:   "absent override forwarded as a nil loader",
			latest: func(*memoryOAuthDB) []latestCredentialLoader { return []latestCredentialLoader{nil} },
		},
		{
			name:   "injected loader",
			latest: func(db *memoryOAuthDB) []latestCredentialLoader { return []latestCredentialLoader{db} },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := newMemoryOAuthDB()
			store, err := buildPostgresClientAuthStore(
				dbgen.New(db),
				bytes.Repeat([]byte{0x42}, 32),
				bytes.Repeat([]byte{0x43}, 32),
				&fixedClock{now: time.Now()},
				rand.Reader,
				testCase.latest(db)...,
			)
			if err != nil {
				t.Fatalf("build store: %v", err)
			}
			did := syntax.DID(canonicalDID)
			session := oauth.ClientSessionData{
				AccountDID: did, SessionID: "browser-secret-session", AccessToken: "access-secret",
			}
			if err := store.SaveSession(context.Background(), session); err != nil {
				t.Fatal(err)
			}

			loaded, err := store.GetLatestSession(context.Background(), did)
			if err != nil {
				t.Fatalf("latest session error = %v", err)
			}
			if loaded.SessionID != session.SessionID || loaded.AccessToken != session.AccessToken {
				t.Fatalf("latest session = %#v", loaded)
			}
		})
	}
}
