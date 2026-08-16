package restapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adenosine-dev/adenosine/internal/release"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
)

type fakeReleaseManager struct {
	repositoryID  repository.ID
	publicRelease release.Release
	draftRelease  release.Release
	asset         release.Asset
	includeDrafts bool
	upload        release.AssetInput
	body          string
}

func (manager *fakeReleaseManager) Create(_ context.Context, _ repository.ID, input release.CreateInput) (release.Release, error) {
	value := manager.publicRelease
	value.Name, value.Body, value.TagName, value.Prerelease = input.Name, input.Body, input.TagName, input.Prerelease
	return value, nil
}
func (manager *fakeReleaseManager) Get(_ context.Context, _ repository.ID, id uuid.UUID) (release.Release, error) {
	if id == manager.publicRelease.ID {
		return manager.publicRelease, nil
	}
	if id == manager.draftRelease.ID {
		return manager.draftRelease, nil
	}
	return release.Release{}, release.ErrNotFound
}
func (manager *fakeReleaseManager) Page(_ context.Context, _ repository.ID, includeDrafts bool, _ *uuid.UUID, _ int) (release.Page[release.Release], error) {
	manager.includeDrafts = includeDrafts
	items := []release.Release{manager.publicRelease}
	if includeDrafts {
		items = append(items, manager.draftRelease)
	}
	return release.Page[release.Release]{Items: items}, nil
}
func (manager *fakeReleaseManager) Update(_ context.Context, _ repository.ID, id uuid.UUID, input release.UpdateInput) (release.Release, error) {
	value, err := manager.Get(context.Background(), manager.repositoryID, id)
	value.Name, value.Body, value.Prerelease = input.Name, input.Body, input.Prerelease
	return value, err
}
func (*fakeReleaseManager) Delete(context.Context, repository.ID, uuid.UUID) error { return nil }
func (manager *fakeReleaseManager) UploadAsset(_ context.Context, _ repository.ID, _ uuid.UUID, input release.AssetInput) (release.Asset, error) {
	manager.upload = input
	body, err := io.ReadAll(input.Body)
	manager.body = string(body)
	return manager.asset, err
}
func (manager *fakeReleaseManager) GetAsset(_ context.Context, _ repository.ID, releaseID, assetID uuid.UUID) (release.Asset, error) {
	if releaseID != manager.asset.ReleaseID || assetID != manager.asset.ID {
		return release.Asset{}, release.ErrNotFound
	}
	return manager.asset, nil
}
func (manager *fakeReleaseManager) PageAssets(context.Context, repository.ID, uuid.UUID, *uuid.UUID, int) (release.Page[release.Asset], error) {
	return release.Page[release.Asset]{Items: []release.Asset{manager.asset}}, nil
}
func (manager *fakeReleaseManager) OpenAsset(context.Context, release.Asset) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(manager.body)), nil
}
func (*fakeReleaseManager) DeleteAsset(context.Context, repository.ID, uuid.UUID, uuid.UUID) error {
	return nil
}

func TestReleaseRoutes(t *testing.T) {
	t.Parallel()
	publicID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000001")
	draftID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000002")
	assetID := uuid.MustParse("0198aaaa-0000-7000-8000-000000000003")
	repositoryID := repository.ID(uuid.MustParse("0198aaaa-0000-7000-8000-000000000010"))
	testCases := []struct {
		name              string
		method            string
		path              string
		body              string
		contentType       string
		session           bool
		pat               bool
		wantStatus        int
		wantBody          string
		wantIncludeDrafts bool
		wantUploadName    string
		wantCache         string
	}{
		{name: "anonymous list hides drafts", method: http.MethodGet, path: "/api/v1/repositories/alice/project/releases", wantStatus: http.StatusOK, wantBody: `"viewer_can_manage":false`},
		{name: "maintainer list includes drafts", method: http.MethodGet, path: "/api/v1/repositories/alice/project/releases", session: true, wantStatus: http.StatusOK, wantBody: `"viewer_can_manage":true`, wantIncludeDrafts: true},
		{name: "anonymous draft is hidden", method: http.MethodGet, path: "/api/v1/repositories/alice/project/releases/" + draftID.String(), wantStatus: http.StatusNotFound},
		{name: "asset upload streams body", method: http.MethodPost, path: "/api/v1/repositories/alice/project/releases/" + publicID.String() + "/assets?name=release.tar.gz", body: "asset bytes", contentType: "application/gzip", pat: true, wantStatus: http.StatusCreated, wantBody: `"sha256"`, wantUploadName: "release.tar.gz"},
		{name: "public asset download is immutable", method: http.MethodGet, path: "/api/v1/repositories/alice/project/releases/" + publicID.String() + "/assets/" + assetID.String(), wantStatus: http.StatusOK, wantBody: "asset bytes", wantCache: "public, max-age=31536000, immutable"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			now := time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC)
			manager := &fakeReleaseManager{
				repositoryID:  repositoryID,
				publicRelease: release.Release{ID: publicID, RepositoryID: repositoryID, TagName: "v1", TargetSHA: strings.Repeat("a", 40), Name: "Version 1", State: release.StatePublished, CreatedByDID: "did:plc:alice", CreatedAt: now, UpdatedAt: now, PublishedAt: &now},
				draftRelease:  release.Release{ID: draftID, RepositoryID: repositoryID, TagName: "v2", TargetSHA: strings.Repeat("b", 40), Name: "Version 2", State: release.StateDraft, CreatedByDID: "did:plc:alice", CreatedAt: now, UpdatedAt: now},
				asset:         release.Asset{ID: assetID, ReleaseID: publicID, RepositoryID: repositoryID, Name: "release.tar.gz", ContentType: "application/gzip", SizeBytes: 11, SHA256: strings.Repeat("c", 64), CreatedAt: now},
				body:          "asset bytes",
			}
			repo := repository.Repository{ID: repositoryID, OwnerDID: "alice", Slug: "project", Visibility: repository.VisibilityPublic, State: repository.StateActive}
			server, err := NewServer(":0", "http://localhost:8080", fakeReadiness{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Dependencies{
				Sessions: fakeSessions{}, TokenAuth: fakeTokenAuth{}, Repositories: fixedRepositoryManager{repository: repo},
				Authorization: fakeAuthorization{}, Releases: manager,
			}, nil)
			if err != nil {
				t.Fatalf("NewServer(): %v", err)
			}
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
				request.Header.Set("X-Asset-Content-Type", testCase.contentType)
			}
			if testCase.session {
				request.AddCookie(&http.Cookie{Name: "adenosine_session", Value: "valid-session"})
				request.Header.Set("Origin", "http://localhost:8080")
			}
			if testCase.pat {
				request.Header.Set("Authorization", "Bearer valid-pat")
			}
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.wantBody != "" && !strings.Contains(response.Body.String(), testCase.wantBody) {
				t.Fatalf("body = %q, want containing %q", response.Body.String(), testCase.wantBody)
			}
			if manager.includeDrafts != testCase.wantIncludeDrafts {
				t.Fatalf("includeDrafts = %t, want %t", manager.includeDrafts, testCase.wantIncludeDrafts)
			}
			if manager.upload.Name != testCase.wantUploadName {
				t.Fatalf("upload name = %q, want %q", manager.upload.Name, testCase.wantUploadName)
			}
			if testCase.wantUploadName != "" && (manager.body != testCase.body || manager.upload.SizeBytes != int64(len(testCase.body))) {
				t.Fatalf("upload = %#v, body = %q", manager.upload, manager.body)
			}
			if response.Header().Get("Cache-Control") != testCase.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", response.Header().Get("Cache-Control"), testCase.wantCache)
			}
		})
	}
}
