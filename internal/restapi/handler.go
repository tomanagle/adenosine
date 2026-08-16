package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
	localatproto "github.com/adenosine-dev/adenosine/internal/atproto"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/comment"
	"github.com/adenosine-dev/adenosine/internal/federation"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	localidentity "github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/moderation"
	"github.com/adenosine-dev/adenosine/internal/notification"
	"github.com/adenosine-dev/adenosine/internal/organization"
	"github.com/adenosine-dev/adenosine/internal/owner"
	"github.com/adenosine-dev/adenosine/internal/passkey"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/repository"
	searchservice "github.com/adenosine-dev/adenosine/internal/search"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/adenosine-dev/adenosine/internal/syncproxy"
	"github.com/adenosine-dev/adenosine/internal/transfer"
	"github.com/adenosine-dev/adenosine/internal/triage"
	webhookservice "github.com/adenosine-dev/adenosine/internal/webhook"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxJSONBody           = 32 * 1024
	maxWebAuthnVerifyBody = 128 * 1024
)

type SessionAuthenticator interface {
	Authenticate(context.Context, string) (auth.SessionIdentity, error)
}

type LoginService interface {
	Start(context.Context, string, string) (string, error)
	Complete(context.Context, url.Values) (localidentity.LoginResult, error)
}

type LocalSessionManager interface {
	RevokeSession(context.Context, string, uuid.UUID) error
}

type PasskeyManager interface {
	BeginRegistration(context.Context, string, uuid.UUID, string) (passkey.BeginResult, error)
	FinishRegistration(context.Context, string, uuid.UUID, string, []byte) (passkey.CredentialSummary, error)
	BeginLogin(context.Context) (passkey.BeginResult, error)
	FinishLogin(context.Context, string, []byte) (passkey.LoginResult, error)
	List(context.Context, string) ([]passkey.CredentialSummary, error)
	Revoke(context.Context, string, uuid.UUID) error
}

type AccountReader interface {
	GetAccount(context.Context, string) (auth.Account, error)
}

type OwnerResolver interface {
	Resolve(context.Context, string) (owner.Owner, error)
}

type OAuthMetadataProvider interface {
	ClientMetadata() localatproto.ClientMetadata
}

type TokenAuthenticator interface {
	Authenticate(context.Context, string) (auth.AccessToken, error)
}

type TokenManager interface {
	CreateToken(context.Context, auth.CreateTokenInput) (auth.AccessToken, string, error)
	ListTokens(context.Context, string) ([]auth.AccessToken, error)
	RevokeToken(context.Context, string, uuid.UUID) error
}

type SSHKeyManager interface {
	CreateSSHKey(context.Context, auth.CreateSSHKeyInput) (auth.SSHKey, error)
	ListSSHKeys(context.Context, string) ([]auth.SSHKey, error)
	RevokeSSHKey(context.Context, string, uuid.UUID) error
}

type RepositoryManager interface {
	Create(context.Context, repository.CreateInput) (repository.Repository, error)
	GetByOwnerSlug(context.Context, string, string) (repository.Repository, error)
	ListByOrganization(context.Context, uuid.UUID) ([]repository.Repository, error)
}

type RepositoryTransferManager interface {
	Initiate(context.Context, repository.Repository, string, string) (transfer.Transfer, error)
	Get(context.Context, uuid.UUID, string) (transfer.Transfer, error)
	Page(context.Context, repository.ID, string, *uuid.UUID, int) (transfer.Page, error)
	Accept(context.Context, uuid.UUID, string) (transfer.Transfer, error)
	Cancel(context.Context, uuid.UUID, string) (transfer.Transfer, error)
}

type TriageManager interface {
	ListLabels(context.Context, triage.RepositoryRoute, string, int, string) ([]triage.Label, error)
	GetLabel(context.Context, triage.RepositoryRoute, string, string) (triage.Label, error)
	CreateLabel(context.Context, string, triage.RepositoryRoute, triage.LabelInput) (triage.Label, error)
	UpdateLabel(context.Context, string, triage.RepositoryRoute, string, triage.LabelInput) (triage.Label, error)
	DeleteLabel(context.Context, string, triage.RepositoryRoute, string) error
	ListMilestones(context.Context, triage.RepositoryRoute, string, int, string) ([]triage.Milestone, error)
	GetMilestone(context.Context, triage.RepositoryRoute, string, string) (triage.Milestone, error)
	CreateMilestone(context.Context, string, triage.RepositoryRoute, triage.MilestoneInput) (triage.Milestone, error)
	UpdateMilestone(context.Context, string, triage.RepositoryRoute, string, triage.MilestoneInput) (triage.Milestone, error)
	DeleteMilestone(context.Context, string, triage.RepositoryRoute, string) error
	GetMetadata(context.Context, triage.RepositoryRoute, triage.SubjectKind, string, string) (triage.Metadata, error)
	PutMetadata(context.Context, string, triage.RepositoryRoute, triage.SubjectKind, string, triage.MetadataInput) (triage.Metadata, error)
	DeleteMetadata(context.Context, string, triage.RepositoryRoute, triage.SubjectKind, string) error
}

type repositoryForkManager interface {
	SyncFork(context.Context, repository.Repository) (repository.ForkSync, error)
}

type repositoryLifecycleManager interface {
	Update(context.Context, repository.Repository, repository.SettingsInput) (repository.Repository, error)
	Delete(context.Context, repository.Repository, string, time.Duration) (repository.Deletion, error)
	GetDeletion(context.Context, uuid.UUID) (repository.Deletion, error)
	RestoreDeletion(context.Context, uuid.UUID) (repository.Repository, error)
}

type OrganizationRepositoryPageManager interface {
	PageByOrganization(context.Context, uuid.UUID, string, *uuid.UUID, int) (repository.Page, error)
}

type RepositoryEndpointBuilder interface {
	For(repository.Repository) (web, gitHTTPS, gitSSH string)
}

type NetworkRepositoryDiscovery interface {
	ListNetworkRepositories(context.Context, int, string) (federation.DiscoveryPage, error)
}

type SearchManager interface {
	Repositories(context.Context, string, searchservice.Sort, int, string, string) (searchservice.RepositoryPage, error)
	Profiles(context.Context, string, searchservice.Sort, int, string, string) (searchservice.ProfilePage, error)
}

type networkRepositoryResolver interface {
	ResolveRepository(context.Context, string, string, string) (federation.DiscoveryRepository, error)
}

type networkRepositoryURIResolver interface {
	ResolveRepositoryByURI(context.Context, string, string) (federation.DiscoveryRepository, error)
}

type repositoryForkPager interface {
	PageForks(context.Context, string, string, int, string) (searchservice.ForkPage, error)
}

type issueDetailResolver interface {
	ResolveIssue(context.Context, string, string, string) (issue.ProjectedIssue, error)
}

type collaborationReader interface {
	ResolveProfile(context.Context, string, string) (profile.Profile, error)
	ListIssues(context.Context, string, string) (issue.Projection, error)
	ListStars(context.Context, string, string) (star.Projection, error)
	ListPullRequests(context.Context, string, string) (pullrequest.Projection, error)
	ResolvePullRequest(context.Context, string, string) (pullrequest.ProjectedPullRequest, error)
	ListPullRequestReviews(context.Context, string, string) ([]pullrequest.ProjectedReview, error)
}

type collaborationPager interface {
	PageIssues(context.Context, string, string, int, string) (searchservice.IssuePage, error)
	PageStars(context.Context, string, string, int, string) (searchservice.StarPage, error)
	PagePullRequests(context.Context, string, string, int, string) (searchservice.PullRequestPage, error)
	PagePullRequestReviews(context.Context, string, string, int, string) (searchservice.PullRequestReviewPage, error)
}

type filteredCollaborationPager interface {
	PageIssuesFiltered(context.Context, string, string, int, string, searchservice.TriageFilter) (searchservice.IssuePage, error)
	PagePullRequestsFiltered(context.Context, string, string, int, string, searchservice.TriageFilter) (searchservice.PullRequestPage, error)
}

func profileReadError(err error) error {
	if errors.Is(err, searchservice.ErrNotFound) {
		return profile.ErrNotFound
	}
	return err
}

func (handler *apiHandler) GetOwner(w http.ResponseWriter, r *http.Request, name generated.OwnerNamePath) {
	value, err := handler.deps.Owners.Resolve(r.Context(), name)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	response := generated.Owner{CanonicalName: value.CanonicalName, Kind: generated.OwnerKind(value.Kind)}
	switch value.Kind {
	case owner.KindAccount:
		response.AccountDid = &value.AccountDID
	case owner.KindOrganization:
		slug := generated.NullableOrganizationSlug(value.OrganizationSlug)
		response.OrganizationSlug = &slug
	default:
		handler.writeError(w, r, fmt.Errorf("unknown owner kind %q", value.Kind))
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (handler *apiHandler) ListNotifications(w http.ResponseWriter, r *http.Request, params generated.ListNotificationsParams) {
	identity, err := handler.notificationIdentity(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, cursor := collectionParameters(params.Limit, params.Cursor)
	unread := params.Unread != nil && *params.Unread
	page, err := handler.deps.Notifications.Page(r.Context(), identity.accountDID, unread, cursor, limit)
	if err != nil {
		if errors.Is(err, notification.ErrValidation) {
			handler.writeMalformed(w, r, err)
		} else {
			handler.writeError(w, r, err)
		}
		return
	}
	items := make([]generated.Notification, len(page.Items))
	for index, value := range page.Items {
		items[index] = notificationResponse(value)
	}
	writeJSON(w, http.StatusOK, generated.NotificationList{Items: items, Page: generated.Page{NextCursor: pointerUnlessEmpty(page.NextCursor)}})
}

func (handler *apiHandler) UpdateNotification(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.notificationIdentity(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateNotificationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if err := handler.deps.Notifications.SetRead(r.Context(), identity.accountDID, uuid.UUID(id), request.Read, time.Now().UTC()); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) DeleteNotification(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.notificationIdentity(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Notifications.Dismiss(r.Context(), identity.accountDID, uuid.UUID(id), time.Now().UTC()); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) notificationIdentity(r *http.Request, mutation bool) (principal, error) {
	identity, err := handler.authenticate(r)
	if err != nil {
		return principal{}, err
	}
	if mutation && identity.session && !handler.validOrigin(r) {
		return principal{}, auth.ErrForbidden
	}
	if !identity.session && (identity.repositoryID != nil || (!slices.Contains(identity.scopes, auth.ScopeRepositoryRead) && !slices.Contains(identity.scopes, auth.ScopeRepositoryWrite))) {
		return principal{}, auth.ErrForbidden
	}
	return identity, nil
}

type StarManager interface {
	Get(context.Context, string) (star.Projection, error)
	Create(context.Context, string, string) (star.Star, error)
	Delete(context.Context, string, string) error
}

type IssueManager interface {
	Get(context.Context, string) (issue.Projection, error)
	Create(context.Context, string, issue.CreateInput) (issue.Issue, error)
	PutStatus(context.Context, string, issue.StatusInput) (issue.Status, error)
}

type PullRequestManager interface {
	List(context.Context, string) (pullrequest.Projection, error)
	Get(context.Context, string) (pullrequest.ProjectedPullRequest, error)
	Create(context.Context, string, pullrequest.CreateInput) (pullrequest.PullRequest, error)
	Refresh(context.Context, string) (pullrequest.Result, error)
	Reviews(context.Context, string) ([]pullrequest.ProjectedReview, error)
	CreateReview(context.Context, string, pullrequest.ReviewInput) (pullrequest.Review, error)
	PutStatus(context.Context, string, pullrequest.StatusInput) (pullrequest.Status, error)
	Merge(context.Context, string, pullrequest.MergeInput) (pullrequest.MergeResult, error)
}

type CommentManager interface {
	Get(context.Context, string, string) (comment.Projection, error)
	Create(context.Context, string, comment.CreateInput) (issue.Comment, error)
	Delete(context.Context, string, string) error
}

type CommentPageManager interface {
	GetPage(context.Context, string, string, int, string) (comment.Projection, error)
}

type ModerationManager interface {
	Block(context.Context, string, string) error
	Unblock(context.Context, string, string) error
	ListBlocks(context.Context, string) ([]moderation.BlockedDID, error)
	Hide(context.Context, string, string) error
	Unhide(context.Context, string, string) error
	ListHidden(context.Context, string) ([]moderation.HiddenRecord, error)
}

type NotificationManager interface {
	Page(context.Context, string, bool, string, int) (notification.Page, error)
	SetRead(context.Context, string, uuid.UUID, bool, time.Time) error
	Dismiss(context.Context, string, uuid.UUID, time.Time) error
}

type WebhookManager interface {
	Create(context.Context, repository.ID, webhookservice.CreateInput, time.Time) (webhookservice.Webhook, error)
	Get(context.Context, repository.ID, uuid.UUID) (webhookservice.Webhook, error)
	Page(context.Context, repository.ID, *uuid.UUID, int) (webhookservice.Page[webhookservice.Webhook], error)
	Update(context.Context, repository.ID, uuid.UUID, webhookservice.UpdateInput, time.Time) (webhookservice.Webhook, error)
	Delete(context.Context, repository.ID, uuid.UUID, time.Time) error
	Deliveries(context.Context, repository.ID, uuid.UUID, *uuid.UUID, int) (webhookservice.Page[webhookservice.Delivery], error)
	Redeliver(context.Context, repository.ID, uuid.UUID, uuid.UUID, time.Time) (webhookservice.Delivery, error)
}

type BranchProtectionManager interface {
	Create(context.Context, repository.ID, branchprotection.Input, time.Time) (branchprotection.Protection, error)
	Get(context.Context, repository.ID, uuid.UUID) (branchprotection.Protection, error)
	Page(context.Context, repository.ID, *uuid.UUID, int) (branchprotection.Page, error)
	Update(context.Context, repository.ID, uuid.UUID, branchprotection.Input, time.Time) (branchprotection.Protection, error)
	Delete(context.Context, repository.ID, uuid.UUID) error
}

type RepositoryActivityWriter interface {
	RepositoryActivity(context.Context, string, string, any) error
}

type ProfileManager interface {
	Get(context.Context, string) (profile.Profile, error)
	Update(context.Context, string, profile.UpdateInput) (profile.Profile, error)
}

type OrganizationManager interface {
	Create(context.Context, organization.CreateInput) (organization.Organization, error)
	Update(context.Context, organization.UpdateInput) (organization.Organization, error)
	GetBySlug(context.Context, string) (organization.Organization, error)
	ListForAccount(context.Context, string) ([]organization.Organization, error)
	ListMembers(context.Context, organization.ID) ([]organization.Member, error)
	GetMember(context.Context, organization.ID, string) (organization.Member, error)
	Invite(context.Context, organization.InviteInput) (organization.Invitation, error)
	Accept(context.Context, uuid.UUID, string) (organization.Member, error)
	ListPendingInvitations(context.Context, string) ([]organization.Invitation, error)
	ListInvitations(context.Context, organization.ID, string) ([]organization.Invitation, error)
	RevokeInvitation(context.Context, organization.ID, uuid.UUID, string) error
	SetVisibility(context.Context, organization.ID, string, organization.MembershipVisibility) (organization.Member, error)
	ChangeRole(context.Context, organization.ID, string, string, organization.Role) (organization.Member, error)
	Remove(context.Context, organization.ID, string, string) error
	AuditEvents(context.Context, organization.ID, string, int, *uuid.UUID) (organization.AuditPage, error)
}

type OrganizationPageManager interface {
	PageForAccount(context.Context, string, *uuid.UUID, int) (organization.Page[organization.Organization], error)
	PageMembers(context.Context, organization.ID, bool, string, int) (organization.Page[organization.Member], error)
	PageInvitations(context.Context, organization.ID, string, *uuid.UUID, int) (organization.Page[organization.Invitation], error)
	PagePendingInvitations(context.Context, string, *uuid.UUID, int) (organization.Page[organization.Invitation], error)
}

type OrganizationTeamManager interface {
	Create(context.Context, organization.CreateTeamInput) (organization.Team, error)
	Update(context.Context, organization.UpdateTeamInput) (organization.Team, error)
	Delete(context.Context, organization.ID, uuid.UUID, string) error
	List(context.Context, organization.ID, string) ([]organization.Team, error)
	Members(context.Context, organization.ID, uuid.UUID, string) ([]organization.TeamMember, error)
	PutMember(context.Context, organization.ID, uuid.UUID, string, string, organization.TeamRole) (organization.TeamMember, error)
	RemoveMember(context.Context, organization.ID, uuid.UUID, string, string) error
	Repositories(context.Context, organization.ID, uuid.UUID, string) ([]organization.TeamRepository, error)
	PutRepository(context.Context, organization.ID, uuid.UUID, string, uuid.UUID, organization.RepositoryRole) (organization.TeamRepository, error)
	RemoveRepository(context.Context, organization.ID, uuid.UUID, string, uuid.UUID) error
}

type OrganizationTeamPageManager interface {
	PageList(context.Context, organization.ID, string, *uuid.UUID, int) (organization.Page[organization.Team], error)
	PageMembers(context.Context, organization.ID, uuid.UUID, string, string, int) (organization.Page[organization.TeamMember], error)
	PageRepositories(context.Context, organization.ID, uuid.UUID, string, *uuid.UUID, int) (organization.Page[organization.TeamRepository], error)
}

type OrganizationCollaboratorManager interface {
	List(context.Context, organization.ID, uuid.UUID, string, string, int) (organization.CollaboratorPage, error)
	Put(context.Context, organization.ID, uuid.UUID, string, string, organization.RepositoryRole) (organization.RepositoryCollaborator, error)
	Remove(context.Context, organization.ID, uuid.UUID, string, string) error
}

type RepositoryAuthorizer interface {
	CanReadRepository(context.Context, string, repository.ID) (bool, error)
}

type repositoryWriteAuthorizer interface {
	CanWriteRepository(context.Context, string, repository.ID) (bool, error)
}

type repositoryAdminAuthorizer interface {
	CanAdminRepository(context.Context, string, repository.ID) (bool, error)
}

type GitReader interface {
	Branches(context.Context, repository.ID, string) ([]gitservice.Branch, error)
	Tags(context.Context, repository.ID) ([]gitservice.Tag, error)
	Tree(context.Context, repository.ID, string, string) (gitservice.Tree, error)
	BlobMetadata(context.Context, repository.ID, string) (gitservice.BlobMetadata, error)
	StreamBlob(context.Context, repository.ID, string, io.Writer) error
	Commits(context.Context, repository.ID, string, int) ([]gitservice.CommitSummary, error)
	Commit(context.Context, repository.ID, string) (gitservice.Commit, error)
	Diff(context.Context, repository.ID, string, string) (gitservice.Diff, error)
	MergeBase(context.Context, repository.ID, string, string) (string, error)
}

type SyncProxy interface {
	Forward(http.ResponseWriter, *http.Request, syncproxy.Shape, syncproxy.Policy) error
}

// FederationProcessor applies a single event delivered by Tap.
type FederationProcessor interface {
	Process(context.Context, []byte) error
}

// FederationDependencies configures the internal Tap webhook transport.
type FederationDependencies struct {
	Processor        FederationProcessor
	TapAdminPassword string
}

// Dependencies contains the application capabilities exposed by REST.
type Dependencies struct {
	Sessions                    SessionAuthenticator
	Login                       LoginService
	LocalSessions               LocalSessionManager
	Passkeys                    PasskeyManager
	Accounts                    AccountReader
	OAuthMetadata               OAuthMetadataProvider
	TokenAuth                   TokenAuthenticator
	Tokens                      TokenManager
	SSHKeys                     SSHKeyManager
	Profiles                    ProfileManager
	Owners                      OwnerResolver
	Organizations               OrganizationManager
	Teams                       OrganizationTeamManager
	Collaborators               OrganizationCollaboratorManager
	Repositories                RepositoryManager
	Transfers                   RepositoryTransferManager
	Triage                      TriageManager
	Endpoints                   RepositoryEndpointBuilder
	Discovery                   NetworkRepositoryDiscovery
	Search                      SearchManager
	Stars                       StarManager
	Issues                      IssueManager
	PullRequests                PullRequestManager
	Comments                    CommentManager
	Moderation                  ModerationManager
	Notifications               NotificationManager
	Webhooks                    WebhookManager
	BranchProtections           BranchProtectionManager
	Activity                    RepositoryActivityWriter
	Authorization               RepositoryAuthorizer
	Git                         GitReader
	Sync                        SyncProxy
	Federation                  *FederationDependencies
	RepositoryDeletionRetention time.Duration
}

type principal struct {
	accountDID   string
	sessionID    uuid.UUID
	session      bool
	scopes       []string
	repositoryID *repository.ID
}

type apiHandler struct {
	readiness    readinessChecker
	logger       *slog.Logger
	baseURL      string
	origin       string
	secureCookie bool
	deps         Dependencies
}

func newAPIHandler(baseURL string, readiness readinessChecker, logger *slog.Logger, deps Dependencies) *apiHandler {
	origin := canonicalOrigin(baseURL)
	parsedBase, _ := url.Parse(baseURL)
	return &apiHandler{
		readiness: readiness, logger: logger, baseURL: strings.TrimSuffix(baseURL, "/"), origin: origin,
		secureCookie: parsedBase != nil && parsedBase.Scheme == "https", deps: deps,
	}
}

func canonicalOrigin(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	return scheme + "://" + host
}

func (handler *apiHandler) GetLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, generated.Health{Status: generated.Ok})
}

func (handler *apiHandler) GetReadiness(w http.ResponseWriter, r *http.Request) {
	if err := handler.readiness.Ping(r.Context()); err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
		writeJSON(w, http.StatusServiceUnavailable, generated.Health{Status: generated.Unavailable})
		return
	}
	writeJSON(w, http.StatusOK, generated.Health{Status: generated.Ok})
}

func (handler *apiHandler) GetCurrentIdentity(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.authenticate(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	response := generated.CurrentIdentity{Did: identity.accountDID}
	if handler.deps.Accounts != nil {
		account, err := handler.deps.Accounts.GetAccount(r.Context(), identity.accountDID)
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		response.Handle = account.Handle
	}
	writeJSON(w, http.StatusOK, response)
}

func (handler *apiHandler) StartATProtoLogin(w http.ResponseWriter, r *http.Request) {
	var request generated.StartATProtoLoginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	authorizationURL, err := handler.deps.Login.Start(r.Context(), request.Identifier, "")
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.StartATProtoLoginResponse{AuthorizationUrl: authorizationURL})
}

func (handler *apiHandler) CompleteATProtoLogin(w http.ResponseWriter, r *http.Request, _ generated.CompleteATProtoLoginParams) {
	result, err := handler.deps.Login.Complete(r.Context(), r.URL.Query())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.setSessionCookie(w, result.SessionPlaintext, result.SessionExpiresAt)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (handler *apiHandler) BeginPasskeyRegistration(w http.ResponseWriter, r *http.Request, _ generated.BeginPasskeyRegistrationParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.BeginPasskeyRegistrationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	result, err := handler.deps.Passkeys.BeginRegistration(r.Context(), identity.accountDID, identity.sessionID, request.Name)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyOptionsResponse{Options: result.Options, CeremonyToken: result.Token})
}

func (handler *apiHandler) VerifyPasskeyRegistration(w http.ResponseWriter, r *http.Request, _ generated.VerifyPasskeyRegistrationParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	request, err := decodePasskeyVerification(w, r)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	credential, err := handler.deps.Passkeys.FinishRegistration(r.Context(), identity.accountDID, identity.sessionID, request.CeremonyToken, request.Response)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/passkeys/"+credential.ID.String())
	writeJSON(w, http.StatusCreated, passkeyResponse(credential))
}

func (handler *apiHandler) BeginPasskeyLogin(w http.ResponseWriter, r *http.Request, _ generated.BeginPasskeyLoginParams) {
	if !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	result, err := handler.deps.Passkeys.BeginLogin(r.Context())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, passkeyOptionsResponse{Options: result.Options, CeremonyToken: result.Token})
}

func (handler *apiHandler) VerifyPasskeyLogin(w http.ResponseWriter, r *http.Request, _ generated.VerifyPasskeyLoginParams) {
	if !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	request, err := decodePasskeyVerification(w, r)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	result, err := handler.deps.Passkeys.FinishLogin(r.Context(), request.CeremonyToken, request.Response)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.setSessionCookie(w, result.SessionPlaintext, result.SessionExpiresAt)
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListPasskeys(w http.ResponseWriter, r *http.Request, params generated.ListPasskeysParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	credentials, err := handler.deps.Passkeys.List(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Passkey, len(credentials))
	for index, credential := range credentials {
		data[index] = passkeyResponse(credential)
	}
	limit, cursor := paginationInputs(params.Limit, params.Cursor)
	items, next, err := paginate(data, limit, cursor, "passkeys:"+identity.accountDID, func(value generated.Passkey) string { return value.Id.String() })
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.PasskeyList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) DeletePasskey(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, _ generated.DeletePasskeyParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Passkeys.Revoke(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) setSessionCookie(w http.ResponseWriter, plaintext string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: "adenosine_session", Value: plaintext, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode,
	})
}

func (handler *apiHandler) GetOAuthClientMetadata(w http.ResponseWriter, _ *http.Request) {
	metadata := handler.deps.OAuthMetadata.ClientMetadata()
	writeJSON(w, http.StatusOK, generated.OAuthClientMetadata{
		ClientId: metadata.ClientID, ApplicationType: metadata.ApplicationType, GrantTypes: metadata.GrantTypes,
		Scope: metadata.Scope, ResponseTypes: metadata.ResponseTypes, RedirectUris: metadata.RedirectURIs,
		DpopBoundAccessTokens: metadata.DPoPBoundAccessTokens, TokenEndpointAuthMethod: metadata.TokenEndpointAuthMethod,
	})
}

func (handler *apiHandler) Logout(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.LocalSessions.RevokeSession(r.Context(), identity.accountDID, identity.sessionID); err != nil {
		handler.writeError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "adenosine_session", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: handler.secureCookie, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) GetDeveloperProfile(w http.ResponseWriter, r *http.Request, did string) {
	if reader, ok := handler.deps.Search.(collaborationReader); ok {
		viewerDID, err := handler.optionalSessionViewer(r)
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		developerProfile, err := reader.ResolveProfile(r.Context(), did, viewerDID)
		if err != nil {
			handler.writeError(w, r, profileReadError(err))
			return
		}
		w.Header().Set("Vary", "Cookie")
		writeJSON(w, http.StatusOK, developerProfileResponse(developerProfile))
		return
	}
	developerProfile, err := handler.deps.Profiles.Get(r.Context(), did)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, developerProfileResponse(developerProfile))
}

func (handler *apiHandler) UpdateDeveloperProfile(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateDeveloperProfileRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	developerProfile, err := handler.deps.Profiles.Update(r.Context(), identity.accountDID, profile.UpdateInput{
		DisplayName: optionalString(request.DisplayName), Bio: optionalString(request.Bio),
		Website: optionalString(request.Website), Location: optionalString(request.Location),
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, developerProfileResponse(developerProfile))
}

func (handler *apiHandler) ListAccessTokens(w http.ResponseWriter, r *http.Request, params generated.ListAccessTokensParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	tokens, err := handler.deps.Tokens.ListTokens(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.AccessToken, len(tokens))
	for index, token := range tokens {
		data[index] = accessTokenResponse(token)
	}
	limit, cursor := paginationInputs(params.Limit, params.Cursor)
	items, next, err := paginate(data, limit, cursor, "tokens:"+identity.accountDID, func(value generated.AccessToken) string { return value.Id.String() })
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.AccessTokenList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateAccessToken(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateAccessTokenRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	scopes := make([]string, len(request.Scopes))
	for index, scope := range request.Scopes {
		scopes[index] = string(scope)
	}
	var repositoryID *repository.ID
	if request.RepositoryId != nil {
		value := repository.ID(*request.RepositoryId)
		repositoryID = &value
	}
	token, plaintext, err := handler.deps.Tokens.CreateToken(r.Context(), auth.CreateTokenInput{
		AccountDID: identity.accountDID, Name: request.Name, Scopes: scopes,
		RepositoryID: repositoryID, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	response := createdAccessTokenResponse(token, plaintext)
	w.Header().Set("Location", "/api/v1/tokens/"+token.ID.String())
	writeJSON(w, http.StatusCreated, response)
}

func (handler *apiHandler) RevokeAccessToken(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Tokens.RevokeToken(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListSSHKeys(w http.ResponseWriter, r *http.Request, params generated.ListSSHKeysParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	keys, err := handler.deps.SSHKeys.ListSSHKeys(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.SSHKey, len(keys))
	for index, key := range keys {
		data[index] = sshKeyResponse(key)
	}
	limit, cursor := paginationInputs(params.Limit, params.Cursor)
	items, next, err := paginate(data, limit, cursor, "ssh-keys:"+identity.accountDID, func(value generated.SSHKey) string { return value.Id.String() })
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.SSHKeyList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateSSHKey(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateSSHKeyRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	key, err := handler.deps.SSHKeys.CreateSSHKey(r.Context(), auth.CreateSSHKeyInput{
		AccountDID: identity.accountDID, Name: request.Name, AuthorizedKey: request.PublicKey,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/ssh-keys/"+key.ID.String())
	writeJSON(w, http.StatusCreated, sshKeyResponse(key))
}

func (handler *apiHandler) RevokeSSHKey(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.SSHKeys.RevokeSSHKey(r.Context(), identity.accountDID, uuid.UUID(id)); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizations(w http.ResponseWriter, r *http.Request, params generated.ListOrganizationsParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organizations:" + identity.accountDID
	var organizations []organization.Organization
	var next *string
	if pager, ok := handler.deps.Organizations.(OrganizationPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageForAccount(r.Context(), identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		organizations = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		organizations, err = handler.deps.Organizations.ListForAccount(r.Context(), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Organization, 0, len(organizations))
	for _, value := range organizations {
		member, memberErr := handler.deps.Organizations.GetMember(r.Context(), value.ID, identity.accountDID)
		if memberErr != nil {
			handler.writeError(w, r, memberErr)
			return
		}
		data = append(data, organizationResponse(value, &member.Role))
	}
	items := data
	if _, ok := handler.deps.Organizations.(OrganizationPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.Organization) string { return value.Id.String() })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateOrganizationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	basePermission := organization.BasePermissionRead
	if request.BasePermission != nil {
		basePermission = organization.BasePermission(*request.BasePermission)
	}
	membersCanCreate := true
	if request.MembersCanCreateRepositories != nil {
		membersCanCreate = *request.MembersCanCreateRepositories
	}
	value, err := handler.deps.Organizations.Create(r.Context(), organization.CreateInput{
		Slug: string(request.Slug), Name: request.Name, Description: optionalString(request.Description),
		Website: optionalString(request.Website), Location: optionalString(request.Location), CreatorDID: identity.accountDID,
		BasePermission: basePermission, MembersCanCreateRepo: membersCanCreate,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	role := organization.RoleOwner
	w.Header().Set("Location", "/api/v1/organizations/"+value.Slug)
	writeJSON(w, http.StatusCreated, organizationResponse(value, &role))
}

func (handler *apiHandler) GetOrganization(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug) {
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil || value.State != organization.StateActive {
		handler.writeError(w, r, organization.ErrNotFound)
		return
	}
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var viewerRole *organization.Role
	if viewerDID != "" {
		member, memberErr := handler.deps.Organizations.GetMember(r.Context(), value.ID, viewerDID)
		if memberErr == nil {
			viewerRole = &member.Role
		} else if !errors.Is(memberErr, organization.ErrNotFound) {
			handler.writeError(w, r, memberErr)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, organizationResponse(value, viewerRole))
}

func (handler *apiHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	current, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateOrganizationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.Update(r.Context(), organization.UpdateInput{
		OrganizationID: current.ID, ActorDID: identity.accountDID, Name: request.Name,
		Description: optionalString(request.Description), Website: optionalString(request.Website), Location: optionalString(request.Location),
		BasePermission: organization.BasePermission(request.BasePermission), MembersCanCreateRepo: request.MembersCanCreateRepositories,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	role := organization.RoleOwner
	writeJSON(w, http.StatusOK, organizationResponse(value, &role))
}

func (handler *apiHandler) ListOrganizationInvitations(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, params generated.ListOrganizationInvitationsParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-owner-invitations:" + value.ID.String()
	var invitations []organization.Invitation
	var next *string
	if pager, ok := handler.deps.Organizations.(OrganizationPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageInvitations(r.Context(), value.ID, identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		invitations = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		invitations, err = handler.deps.Organizations.ListInvitations(r.Context(), value.ID, identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.OrganizationInvitation, len(invitations))
	for index, invitation := range invitations {
		items[index] = organizationInvitationResponse(invitation)
	}
	pageItems := items
	if _, ok := handler.deps.Organizations.(OrganizationPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		pageItems, next, err = paginate(items, requestedLimit, cursor, scope, func(value generated.OrganizationInvitation) string { return value.Id.String() })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationInvitationList{Items: pageItems, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) RevokeOrganizationInvitation(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, invitationID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Organizations.RevokeInvitation(r.Context(), value.ID, uuid.UUID(invitationID), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizationAuditEvents(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, params generated.ListOrganizationAuditEventsParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-audit:" + value.ID.String()
	var after *uuid.UUID
	if encoded != "" {
		cursor, cursorErr := decodePageCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		id, parseErr := uuid.Parse(cursor.Key)
		if parseErr != nil {
			handler.writeMalformed(w, r, errInvalidCursor)
			return
		}
		after = &id
	}
	page, err := handler.deps.Organizations.AuditEvents(r.Context(), value.ID, identity.accountDID, limit, after)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.OrganizationAuditEvent, len(page.Items))
	for index, event := range page.Items {
		items[index] = organizationAuditEventResponse(event)
	}
	var next *string
	if page.NextCursor != nil {
		encodedNext, encodeErr := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: page.NextCursor.String()})
		if encodeErr != nil {
			handler.writeError(w, r, encodeErr)
			return
		}
		next = &encodedNext
	}
	writeJSON(w, http.StatusOK, generated.OrganizationAuditEventList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) ListOrganizationMembers(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, params generated.ListOrganizationMembersParams) {
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil || value.State != organization.StateActive {
		handler.writeError(w, r, organization.ErrNotFound)
		return
	}
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	showPrivate := false
	if viewerDID != "" {
		_, memberErr := handler.deps.Organizations.GetMember(r.Context(), value.ID, viewerDID)
		showPrivate = memberErr == nil
		if memberErr != nil && !errors.Is(memberErr, organization.ErrNotFound) {
			handler.writeError(w, r, memberErr)
			return
		}
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-members:" + string(slug) + ":" + viewerDID
	var members []organization.Member
	var next *string
	if pager, ok := handler.deps.Organizations.(OrganizationPageManager); ok {
		after, cursorErr := decodeCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageMembers(r.Context(), value.ID, showPrivate, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		members = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		members, err = handler.deps.Organizations.ListMembers(r.Context(), value.ID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.OrganizationMember, 0, len(members))
	for _, member := range members {
		if showPrivate || member.Visibility == organization.VisibilityPublic {
			data = append(data, organizationMemberResponse(member))
		}
	}
	items := data
	if _, ok := handler.deps.Organizations.(OrganizationPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.OrganizationMember) string { return value.Did })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.OrganizationMemberList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) InviteOrganizationMember(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.InviteOrganizationMemberRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	role := organization.RoleMember
	if request.Role != nil {
		role = organization.Role(*request.Role)
	}
	invitation, err := handler.deps.Organizations.Invite(r.Context(), organization.InviteInput{
		OrganizationID: value.ID, ActorDID: identity.accountDID, InviteeDID: request.Did, Role: role,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, organizationInvitationResponse(invitation))
}

func (handler *apiHandler) ListOrganizationInvitationsForCurrentUser(w http.ResponseWriter, r *http.Request, params generated.ListOrganizationInvitationsForCurrentUserParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-invitations:" + identity.accountDID
	var invitations []organization.Invitation
	var next *string
	if pager, ok := handler.deps.Organizations.(OrganizationPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PagePendingInvitations(r.Context(), identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		invitations = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		invitations, err = handler.deps.Organizations.ListPendingInvitations(r.Context(), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.OrganizationInvitation, len(invitations))
	for index, invitation := range invitations {
		data[index] = organizationInvitationResponse(invitation)
	}
	items := data
	if _, ok := handler.deps.Organizations.(OrganizationPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.OrganizationInvitation) string { return value.Id.String() })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationInvitationList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) AcceptOrganizationInvitation(w http.ResponseWriter, r *http.Request, invitationID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	member, err := handler.deps.Organizations.Accept(r.Context(), uuid.UUID(invitationID), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationMemberResponse(member))
}

func (handler *apiHandler) UpdateOrganizationMember(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, memberDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateOrganizationMemberRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if (request.Role == nil) == (request.Visibility == nil) {
		handler.writeError(w, r, organization.ErrValidation)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var member organization.Member
	if request.Role != nil {
		member, err = handler.deps.Organizations.ChangeRole(r.Context(), value.ID, identity.accountDID, memberDID, organization.Role(*request.Role))
	} else if identity.accountDID != memberDID {
		err = organization.ErrForbidden
	} else {
		member, err = handler.deps.Organizations.SetVisibility(r.Context(), value.ID, identity.accountDID, organization.MembershipVisibility(*request.Visibility))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationMemberResponse(member))
}

func (handler *apiHandler) RemoveOrganizationMember(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, memberDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Organizations.Remove(r.Context(), value.ID, identity.accountDID, memberDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizationTeams(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, params generated.ListOrganizationTeamsParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-teams:" + string(slug) + ":" + identity.accountDID
	var teams []organization.Team
	var next *string
	if pager, ok := handler.deps.Teams.(OrganizationTeamPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageList(r.Context(), value.ID, identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		teams = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		teams, err = handler.deps.Teams.List(r.Context(), value.ID, identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.OrganizationTeam, len(teams))
	for index, team := range teams {
		data[index] = organizationTeamResponse(team)
	}
	items := data
	if _, ok := handler.deps.Teams.(OrganizationTeamPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.OrganizationTeam) string { return value.Id.String() })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationTeamList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateOrganizationTeam(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateOrganizationTeamRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	visibility := organization.TeamVisibilityVisible
	if request.Visibility != nil {
		visibility = organization.TeamVisibility(*request.Visibility)
	}
	var parentTeamID *uuid.UUID
	if request.ParentTeamId != nil {
		id := uuid.UUID(*request.ParentTeamId)
		parentTeamID = &id
	}
	team, err := handler.deps.Teams.Create(r.Context(), organization.CreateTeamInput{OrganizationID: value.ID, ActorDID: identity.accountDID, ParentTeamID: parentTeamID, Slug: request.Slug, Name: request.Name, Description: optionalString(request.Description), Visibility: visibility})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, organizationTeamResponse(team))
}

func (handler *apiHandler) UpdateOrganizationTeam(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateOrganizationTeamRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	var parentTeamID *uuid.UUID
	if request.ParentTeamId != nil {
		id := uuid.UUID(*request.ParentTeamId)
		parentTeamID = &id
	}
	team, err := handler.deps.Teams.Update(r.Context(), organization.UpdateTeamInput{
		OrganizationID: value.ID, TeamID: uuid.UUID(teamID), ActorDID: identity.accountDID,
		ParentTeamID: parentTeamID, Name: request.Name, Description: optionalString(request.Description), Visibility: organization.TeamVisibility(request.Visibility),
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationTeamResponse(team))
}

func (handler *apiHandler) DeleteOrganizationTeam(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Teams.Delete(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizationTeamMembers(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID, params generated.ListOrganizationTeamMembersParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-team-members:" + teamID.String() + ":" + identity.accountDID
	var members []organization.TeamMember
	var next *string
	if pager, ok := handler.deps.Teams.(OrganizationTeamPageManager); ok {
		after, cursorErr := decodeCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageMembers(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		members = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		members, err = handler.deps.Teams.Members(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.OrganizationTeamMember, len(members))
	for index, member := range members {
		data[index] = organizationTeamMemberResponse(member)
	}
	items := data
	if _, ok := handler.deps.Teams.(OrganizationTeamPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.OrganizationTeamMember) string { return value.Did })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationTeamMemberList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) PutOrganizationTeamMember(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID, memberDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutOrganizationTeamMemberRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	role := organization.TeamRoleMember
	if request.Role != nil {
		role = organization.TeamRole(*request.Role)
	}
	member, err := handler.deps.Teams.PutMember(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, memberDID, role)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationTeamMemberResponse(member))
}

func (handler *apiHandler) RemoveOrganizationTeamMember(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID, memberDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Teams.RemoveMember(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, memberDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizationRepositories(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, params generated.ListOrganizationRepositoriesParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if _, err = handler.deps.Organizations.GetMember(r.Context(), value.ID, identity.accountDID); err != nil {
		handler.writeError(w, r, organization.ErrForbidden)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-repositories:" + string(slug)
	var repositories []repository.Repository
	var next *string
	if pager, ok := handler.deps.Repositories.(OrganizationRepositoryPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageByOrganization(r.Context(), uuid.UUID(value.ID), identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		repositories = page.Items
		if page.NextCursor != nil {
			next, err = encodeCollectionCursor(scope, page.NextCursor.String())
		}
	} else {
		repositories, err = handler.deps.Repositories.ListByOrganization(r.Context(), uuid.UUID(value.ID))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.Repository, len(repositories))
	for index, repo := range repositories {
		repo.OrganizationSlug = value.Slug
		items[index] = handler.repositoryResponse(repo)
	}
	pageItems := items
	if _, ok := handler.deps.Repositories.(OrganizationRepositoryPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		pageItems, next, err = paginate(items, requestedLimit, cursor, scope, func(value generated.Repository) string { return string(value.Slug) })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.RepositoryList{Items: pageItems, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) ListOrganizationRepositoryCollaborators(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, repositoryID openapi_types.UUID, params generated.ListOrganizationRepositoryCollaboratorsParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-repository-collaborators:" + value.ID.String() + ":" + repositoryID.String()
	after := ""
	if encoded != "" {
		cursor, cursorErr := decodePageCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		after = cursor.Key
	}
	page, err := handler.deps.Collaborators.List(r.Context(), value.ID, uuid.UUID(repositoryID), identity.accountDID, after, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.OrganizationRepositoryCollaborator, len(page.Items))
	for index, collaborator := range page.Items {
		items[index] = organizationRepositoryCollaboratorResponse(collaborator)
	}
	var next *string
	if page.NextCursor != nil {
		encodedNext, encodeErr := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: *page.NextCursor})
		if encodeErr != nil {
			handler.writeError(w, r, encodeErr)
			return
		}
		next = &encodedNext
	}
	writeJSON(w, http.StatusOK, generated.OrganizationRepositoryCollaboratorList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) PutOrganizationRepositoryCollaborator(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, repositoryID openapi_types.UUID, collaboratorDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutOrganizationRepositoryCollaboratorRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	collaborator, err := handler.deps.Collaborators.Put(r.Context(), value.ID, uuid.UUID(repositoryID), identity.accountDID, collaboratorDID, organization.RepositoryRole(request.Role))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationRepositoryCollaboratorResponse(collaborator))
}

func (handler *apiHandler) RemoveOrganizationRepositoryCollaborator(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, repositoryID openapi_types.UUID, collaboratorDID string) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Collaborators.Remove(r.Context(), value.ID, uuid.UUID(repositoryID), identity.accountDID, collaboratorDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListOrganizationTeamRepositories(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID openapi_types.UUID, params generated.ListOrganizationTeamRepositoriesParams) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "organization-team-repositories:" + teamID.String() + ":" + identity.accountDID
	var repositories []organization.TeamRepository
	var next *string
	if pager, ok := handler.deps.Teams.(OrganizationTeamPageManager); ok {
		after, cursorErr := decodeUUIDCollectionCursor(encoded, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		page, pageErr := pager.PageRepositories(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, after, limit)
		if pageErr != nil {
			handler.writeError(w, r, pageErr)
			return
		}
		repositories = page.Items
		next, err = encodeCollectionCursor(scope, page.NextCursor)
	} else {
		repositories, err = handler.deps.Teams.Repositories(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.OrganizationTeamRepository, len(repositories))
	for index, repository := range repositories {
		items[index] = organizationTeamRepositoryResponse(repository)
	}
	pageItems := items
	if _, ok := handler.deps.Teams.(OrganizationTeamPageManager); !ok {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		pageItems, next, err = paginate(items, requestedLimit, cursor, scope, func(value generated.OrganizationTeamRepository) string { return value.RepositoryId.String() })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.OrganizationTeamRepositoryList{Items: pageItems, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) PutOrganizationTeamRepository(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID, repositoryID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutOrganizationTeamRepositoryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	assignment, err := handler.deps.Teams.PutRepository(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, uuid.UUID(repositoryID), organization.RepositoryRole(request.Role))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, organizationTeamRepositoryResponse(assignment))
}

func (handler *apiHandler) RemoveOrganizationTeamRepository(w http.ResponseWriter, r *http.Request, slug generated.OrganizationSlug, teamID, repositoryID openapi_types.UUID) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(slug))
	if err == nil {
		err = handler.deps.Teams.RemoveRepository(r.Context(), value.ID, uuid.UUID(teamID), identity.accountDID, uuid.UUID(repositoryID))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) CreateRepository(w http.ResponseWriter, r *http.Request, _ generated.CreateRepositoryParams) {
	identity, err := handler.authenticate(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if !identity.session && (!slices.Contains(identity.scopes, auth.ScopeRepositoryWrite) || identity.repositoryID != nil) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	if identity.session && !handler.validOrigin(r) {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	var request generated.CreateRepositoryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	visibility := repository.VisibilityPublic
	if request.Visibility != nil {
		visibility = repository.Visibility(*request.Visibility)
	}
	defaultBranch := "main"
	if request.DefaultBranch != nil {
		defaultBranch = *request.DefaultBranch
	}
	var organizationID *uuid.UUID
	var organizationSlug string
	var organizationAT *repository.ATIdentity
	if request.Organization != nil {
		value, organizationErr := handler.deps.Organizations.GetBySlug(r.Context(), string(*request.Organization))
		if organizationErr != nil {
			handler.writeError(w, r, organizationErr)
			return
		}
		member, memberErr := handler.deps.Organizations.GetMember(r.Context(), value.ID, identity.accountDID)
		if memberErr != nil || (member.Role != organization.RoleOwner && !value.MembersCanCreateRepo) {
			handler.writeError(w, r, organization.ErrForbidden)
			return
		}
		id := uuid.UUID(value.ID)
		organizationID, organizationSlug = &id, value.Slug
		organizationAT = &repository.ATIdentity{URI: value.ATURI, CID: value.ATCID}
	}
	repo, err := handler.deps.Repositories.Create(r.Context(), repository.CreateInput{
		OwnerDID: identity.accountDID, Slug: request.Slug, DisplayName: optionalString(request.DisplayName),
		OrganizationID: organizationID, OrganizationSlug: organizationSlug, OrganizationAT: organizationAT,
		Description: optionalString(request.Description), Visibility: visibility, DefaultBranch: defaultBranch,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+repositoryRouteOwner(repo)+"/"+repo.Slug)
	writeJSON(w, http.StatusCreated, handler.repositoryResponse(repo))
}

func (handler *apiHandler) ListRepositoryForks(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, params generated.ListRepositoryForksParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	source, _, err := handler.resolveForkSource(r, string(owner), string(slug), viewerDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	pager, ok := handler.deps.Search.(repositoryForkPager)
	if !ok {
		handler.writeError(w, r, repository.ErrNotFound)
		return
	}
	limit, cursor := collectionParameters(params.Limit, params.Cursor)
	page, err := pager.PageForks(r.Context(), source.URI, viewerDID, limit, cursor)
	if err != nil {
		if searchRequestError(err) {
			handler.writeMalformed(w, r, err)
			return
		}
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.Repository, len(page.Repositories))
	for index, value := range page.Repositories {
		items[index] = networkRepositoryResponse(value)
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.RepositoryForkList{Items: items, ForkCount: page.ForkCount, Page: generated.Page{NextCursor: page.NextCursor}})
}

func (handler *apiHandler) CreateRepositoryFork(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath, _ generated.CreateRepositoryForkParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateRepositoryForkRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	source, sourceRepository, err := handler.resolveForkSource(r, string(owner), string(slug), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	destinationSlug := sourceRepository.Slug
	if request.Slug != nil {
		destinationSlug = string(*request.Slug)
	}
	organizationID, organizationSlug, organizationAT, err := handler.repositoryOrganization(r, identity.accountDID, request.Organization)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	created, err := handler.deps.Repositories.Create(r.Context(), repository.CreateInput{
		OwnerDID: identity.accountDID, OrganizationID: organizationID, OrganizationSlug: organizationSlug, OrganizationAT: organizationAT,
		ForkedFrom: &source, Slug: destinationSlug, DisplayName: sourceRepository.DisplayName, Description: sourceRepository.Description,
		Visibility: repository.VisibilityPublic, DefaultBranch: sourceRepository.DefaultBranch,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+repositoryRouteOwner(created)+"/"+created.Slug)
	writeJSON(w, http.StatusCreated, handler.repositoryResponse(created))
}

func (handler *apiHandler) SyncRepositoryFork(w http.ResponseWriter, r *http.Request, owner generated.RepositoryOwnerPath, slug generated.RepositorySlugPath) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	repo, err := handler.deps.Repositories.GetByOwnerSlug(r.Context(), string(owner), string(slug))
	if err != nil || repo.State != repository.StateActive {
		handler.writeError(w, r, repository.ErrNotFound)
		return
	}
	authorizer, ok := handler.deps.Authorization.(repositoryWriteAuthorizer)
	if !ok {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	allowed, err := authorizer.CanWriteRepository(r.Context(), identity.accountDID, repo.ID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if !allowed {
		handler.writeError(w, r, auth.ErrForbidden)
		return
	}
	manager, ok := handler.deps.Repositories.(repositoryForkManager)
	if !ok {
		handler.writeError(w, r, repository.ErrValidation)
		return
	}
	result, err := manager.SyncFork(r.Context(), repo)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.RepositoryForkSync{BeforeSha: result.BeforeSHA, AfterSha: result.AfterSHA, Updated: result.Updated})
}

func (handler *apiHandler) resolveForkSource(r *http.Request, owner, slug, viewerDID string) (repository.ForkSource, repository.Repository, error) {
	local, localErr := handler.deps.Repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if localErr == nil {
		if local.State != repository.StateActive || local.Visibility != repository.VisibilityPublic || local.ATURI == "" || local.ATCID == "" {
			return repository.ForkSource{}, repository.Repository{}, repository.ErrNotFound
		}
		id := local.ID
		gitHTTPS := handler.baseURL + "/" + repositoryRouteOwner(local) + "/" + local.Slug + ".git"
		if handler.deps.Endpoints != nil {
			_, gitHTTPS, _ = handler.deps.Endpoints.For(local)
		}
		return repository.ForkSource{URI: local.ATURI, CID: local.ATCID, GitHTTPS: gitHTTPS, LocalRepositoryID: &id}, local, nil
	}
	if !errors.Is(localErr, repository.ErrNotFound) {
		return repository.ForkSource{}, repository.Repository{}, localErr
	}
	resolver, ok := handler.deps.Search.(networkRepositoryResolver)
	if !ok {
		return repository.ForkSource{}, repository.Repository{}, repository.ErrNotFound
	}
	projected, err := resolver.ResolveRepository(r.Context(), owner, slug, viewerDID)
	if errors.Is(err, searchservice.ErrNotFound) {
		return repository.ForkSource{}, repository.Repository{}, repository.ErrNotFound
	}
	if err != nil {
		return repository.ForkSource{}, repository.Repository{}, err
	}
	var localID *repository.ID
	if projected.LocalRepositoryID != nil {
		id := repository.ID(*projected.LocalRepositoryID)
		localID = &id
	}
	metadata := repository.Repository{Slug: projected.Slug, DisplayName: projected.Name, Description: projected.Description, DefaultBranch: projected.DefaultBranch}
	return repository.ForkSource{URI: projected.URI, CID: projected.CID, GitHTTPS: projected.GitHTTPS, LocalRepositoryID: localID}, metadata, nil
}

func (handler *apiHandler) repositoryOrganization(r *http.Request, actorDID string, slug *generated.OrganizationSlug) (*uuid.UUID, string, *repository.ATIdentity, error) {
	if slug == nil {
		return nil, "", nil, nil
	}
	value, err := handler.deps.Organizations.GetBySlug(r.Context(), string(*slug))
	if err != nil {
		return nil, "", nil, err
	}
	member, err := handler.deps.Organizations.GetMember(r.Context(), value.ID, actorDID)
	if err != nil || (member.Role != organization.RoleOwner && !value.MembersCanCreateRepo) {
		return nil, "", nil, organization.ErrForbidden
	}
	id := uuid.UUID(value.ID)
	return &id, value.Slug, &repository.ATIdentity{URI: value.ATURI, CID: value.ATCID}, nil
}

func (handler *apiHandler) GetRepository(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err == nil {
		viewerDID, _ := handler.optionalSessionViewer(r)
		if viewerDID != "" {
			if authorizer, ok := handler.deps.Authorization.(repositoryAdminAuthorizer); ok {
				repo.ViewerCanAdmin, _ = authorizer.CanAdminRepository(r.Context(), viewerDID, repo.ID)
			}
		}
		response := handler.repositoryResponse(repo)
		if repo.Visibility == repository.VisibilityPublic && repo.ATURI != "" {
			if resolver, ok := handler.deps.Search.(networkRepositoryURIResolver); ok {
				projected, resolveErr := resolver.ResolveRepositoryByURI(r.Context(), repo.ATURI, viewerDID)
				if resolveErr != nil && !errors.Is(resolveErr, searchservice.ErrNotFound) {
					handler.writeError(w, r, resolveErr)
					return
				}
				if resolveErr == nil {
					applyRepositoryCounters(&response, projected)
				}
			}
		}
		w.Header().Set("Vary", "Cookie")
		writeJSON(w, http.StatusOK, response)
		return
	}
	if !errors.Is(err, repository.ErrNotFound) {
		handler.writeError(w, r, err)
		return
	}
	resolver, ok := handler.deps.Search.(networkRepositoryResolver)
	if !ok {
		handler.writeError(w, r, repository.ErrNotFound)
		return
	}
	viewerDID, sessionErr := handler.optionalSessionViewer(r)
	if sessionErr != nil {
		handler.writeError(w, r, sessionErr)
		return
	}
	networkRepository, resolveErr := resolver.ResolveRepository(r.Context(), owner, string(slug), viewerDID)
	if resolveErr != nil {
		if errors.Is(resolveErr, searchservice.ErrNotFound) {
			resolveErr = repository.ErrNotFound
		}
		handler.writeError(w, r, resolveErr)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, networkRepositoryResponse(networkRepository))
}

func (handler *apiHandler) UpdateRepository(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	identity, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateRepositoryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	input := repository.SettingsInput{
		OwnerAlias: owner, Slug: repo.Slug, DisplayName: repo.DisplayName, Description: repo.Description,
		Visibility: repo.Visibility, DefaultBranch: repo.DefaultBranch, Archived: repo.ArchivedAt != nil,
	}
	if request.Slug != nil {
		input.Slug = string(*request.Slug)
	}
	if request.DisplayName != nil {
		input.DisplayName = *request.DisplayName
	}
	if request.Description != nil {
		input.Description = *request.Description
	}
	if request.Visibility != nil {
		input.Visibility = repository.Visibility(*request.Visibility)
	}
	if request.DefaultBranch != nil {
		input.DefaultBranch = *request.DefaultBranch
	}
	if request.Archived != nil {
		input.Archived = *request.Archived
	}
	manager, ok := handler.deps.Repositories.(repositoryLifecycleManager)
	if !ok {
		handler.writeError(w, r, repository.ErrValidation)
		return
	}
	updated, err := manager.Update(r.Context(), repo, input)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	updated.ViewerCanAdmin = true
	canonical := "/api/v1/repositories/" + repositoryRouteOwner(updated) + "/" + updated.Slug
	w.Header().Set("Content-Location", canonical)
	_ = identity
	writeJSON(w, http.StatusOK, handler.repositoryResponse(updated))
}

func (handler *apiHandler) DeleteRepository(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	identity, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	manager, ok := handler.deps.Repositories.(repositoryLifecycleManager)
	if !ok {
		handler.writeError(w, r, repository.ErrValidation)
		return
	}
	retention := handler.deps.RepositoryDeletionRetention
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	deletion, err := manager.Delete(r.Context(), repo, identity.accountDID, retention)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	location := "/api/v1/repository-deletions/" + deletion.ID.String()
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusAccepted, repositoryDeletionResponse(deletion))
}

func (handler *apiHandler) ListRepositoryWebhooks(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListRepositoryWebhooksParams) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	after, err := decodeUUIDCollectionCursor(encoded, "repository-webhooks:"+repo.ID.String())
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	page, err := handler.deps.Webhooks.Page(r.Context(), repo.ID, after, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.RepositoryWebhook, len(page.Items))
	for index, value := range page.Items {
		items[index] = repositoryWebhookResponse(value)
	}
	var next *string
	if page.NextCursor != nil {
		next, err = encodeCollectionCursor("repository-webhooks:"+repo.ID.String(), page.NextCursor.String())
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.RepositoryWebhookList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateRepositoryWebhook(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateRepositoryWebhookRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	value, err := handler.deps.Webhooks.Create(r.Context(), repo.ID, webhookservice.CreateInput{URL: request.Url, Secret: request.Secret, Events: webhookEventStrings(request.Events), Enabled: enabled}, time.Now().UTC())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	location := "/api/v1/repositories/" + owner + "/" + string(slug) + "/webhooks/" + value.ID.String()
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, repositoryWebhookResponse(value))
}

func (handler *apiHandler) GetRepositoryWebhook(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Webhooks.Get(r.Context(), repo.ID, uuid.UUID(id))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repositoryWebhookResponse(value))
}

func (handler *apiHandler) UpdateRepositoryWebhook(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.UpdateRepositoryWebhookRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Webhooks.Update(r.Context(), repo.ID, uuid.UUID(id), webhookservice.UpdateInput{URL: request.Url, Secret: request.Secret, Events: webhookEventStrings(request.Events), Enabled: request.Enabled}, time.Now().UTC())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repositoryWebhookResponse(value))
}

func (handler *apiHandler) DeleteRepositoryWebhook(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err == nil {
		err = handler.deps.Webhooks.Delete(r.Context(), repo.ID, uuid.UUID(id), time.Now().UTC())
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, webhookID openapi_types.UUID, params generated.ListWebhookDeliveriesParams) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "webhook-deliveries:" + uuid.UUID(webhookID).String()
	after, err := decodeUUIDCollectionCursor(encoded, scope)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	page, err := handler.deps.Webhooks.Deliveries(r.Context(), repo.ID, uuid.UUID(webhookID), after, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.WebhookDelivery, len(page.Items))
	for index, value := range page.Items {
		items[index] = webhookDeliveryResponse(value)
	}
	var next *string
	if page.NextCursor != nil {
		next, err = encodeCollectionCursor(scope, page.NextCursor.String())
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.WebhookDeliveryList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateWebhookRedelivery(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, webhookID openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateWebhookRedeliveryRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Webhooks.Redeliver(r.Context(), repo.ID, uuid.UUID(webhookID), uuid.UUID(request.DeliveryId), time.Now().UTC())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	location := "/api/v1/repositories/" + owner + "/" + string(slug) + "/webhooks/" + uuid.UUID(webhookID).String() + "/deliveries"
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, webhookDeliveryResponse(value))
}

func (handler *apiHandler) ListBranchProtections(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListBranchProtectionsParams) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encoded := collectionParameters(params.Limit, params.Cursor)
	scope := "branch-protections:" + repo.ID.String()
	after, err := decodeUUIDCollectionCursor(encoded, scope)
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	page, err := handler.deps.BranchProtections.Page(r.Context(), repo.ID, after, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	items := make([]generated.BranchProtection, len(page.Items))
	for index, value := range page.Items {
		items[index] = branchProtectionResponse(value)
	}
	var next *string
	if page.NextCursor != nil {
		next, err = encodeCollectionCursor(scope, page.NextCursor.String())
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.BranchProtectionList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateBranchProtection(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.BranchProtectionInput
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.BranchProtections.Create(r.Context(), repo.ID, branchProtectionInput(request), time.Now().UTC())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/repositories/"+owner+"/"+string(slug)+"/branch-protections/"+value.ID.String())
	writeJSON(w, http.StatusCreated, branchProtectionResponse(value))
}

func (handler *apiHandler) GetBranchProtection(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.BranchProtections.Get(r.Context(), repo.ID, uuid.UUID(id))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, branchProtectionResponse(value))
}

func (handler *apiHandler) UpdateBranchProtection(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.BranchProtectionInput
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.BranchProtections.Update(r.Context(), repo.ID, uuid.UUID(id), branchProtectionInput(request), time.Now().UTC())
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, branchProtectionResponse(value))
}

func (handler *apiHandler) DeleteBranchProtection(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, id openapi_types.UUID) {
	_, repo, err := handler.requireRepositoryAdmin(r, owner, string(slug), true)
	if err == nil {
		err = handler.deps.BranchProtections.Delete(r.Context(), repo.ID, uuid.UUID(id))
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) GetRepositoryDeletion(w http.ResponseWriter, r *http.Request, deletion openapi_types.UUID) {
	identity, err := handler.authenticate(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	manager, ok := handler.deps.Repositories.(repositoryLifecycleManager)
	if !ok {
		handler.writeError(w, r, repository.ErrNotFound)
		return
	}
	value, err := manager.GetDeletion(r.Context(), uuid.UUID(deletion))
	if err == nil {
		err = handler.authorizeRepositoryAdmin(r.Context(), identity, value.RepositoryID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, repositoryDeletionResponse(value))
}

func (handler *apiHandler) RestoreRepositoryDeletion(w http.ResponseWriter, r *http.Request, deletion openapi_types.UUID) {
	identity, err := handler.authenticate(r)
	if err == nil && identity.session && !handler.validOrigin(r) {
		err = auth.ErrForbidden
	}
	manager, ok := handler.deps.Repositories.(repositoryLifecycleManager)
	if !ok && err == nil {
		err = repository.ErrNotFound
	}
	var value repository.Deletion
	if err == nil {
		value, err = manager.GetDeletion(r.Context(), uuid.UUID(deletion))
	}
	if err == nil {
		err = handler.authorizeRepositoryAdmin(r.Context(), identity, value.RepositoryID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	restored, err := manager.RestoreDeletion(r.Context(), uuid.UUID(deletion))
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	restored.ViewerCanAdmin = true
	location := "/api/v1/repositories/" + repositoryRouteOwner(restored) + "/" + restored.Slug
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusOK, handler.repositoryResponse(restored))
}

func (handler *apiHandler) requireRepositoryAdmin(r *http.Request, owner, slug string, mutation bool) (principal, repository.Repository, error) {
	identity, err := handler.authenticate(r)
	if err != nil {
		return principal{}, repository.Repository{}, err
	}
	if mutation && identity.session && !handler.validOrigin(r) {
		return principal{}, repository.Repository{}, auth.ErrForbidden
	}
	repo, err := handler.deps.Repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if err != nil || repo.State != repository.StateActive {
		return principal{}, repository.Repository{}, repository.ErrNotFound
	}
	if err := handler.authorizeRepositoryAdmin(r.Context(), identity, repo.ID); err != nil {
		return principal{}, repository.Repository{}, err
	}
	return identity, repo, nil
}

func (handler *apiHandler) authorizeRepositoryAdmin(ctx context.Context, identity principal, repositoryID repository.ID) error {
	if !identity.session {
		if !slices.Contains(identity.scopes, auth.ScopeRepositoryWrite) || (identity.repositoryID != nil && *identity.repositoryID != repositoryID) {
			return auth.ErrForbidden
		}
	}
	authorizer, ok := handler.deps.Authorization.(repositoryAdminAuthorizer)
	if !ok {
		return auth.ErrForbidden
	}
	allowed, err := authorizer.CanAdminRepository(ctx, identity.accountDID, repositoryID)
	if err != nil {
		return fmt.Errorf("authorize repository administration: %w", err)
	}
	if !allowed {
		return auth.ErrForbidden
	}
	return nil
}

func (handler *apiHandler) ListNetworkRepositories(w http.ResponseWriter, r *http.Request, params generated.ListNetworkRepositoriesParams) {
	limit := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	page, err := handler.deps.Discovery.ListNetworkRepositories(r.Context(), limit, cursor)
	if err != nil {
		if errors.Is(err, federation.ErrInvalidCursor) || errors.Is(err, federation.ErrInvalidLimit) {
			handler.writeMalformed(w, r, err)
			return
		}
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Repository, len(page.Repositories))
	for index, repo := range page.Repositories {
		data[index] = networkRepositoryResponse(repo)
	}
	writeJSON(w, http.StatusOK, generated.NetworkRepositoryList{Items: data, Page: generated.Page{NextCursor: page.NextCursor}})
}

func (handler *apiHandler) SearchRepositories(w http.ResponseWriter, r *http.Request, params generated.SearchRepositoriesParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, cursor, sort := searchParameters(r, params.Limit, params.Cursor, params.Sort)
	page, err := handler.deps.Search.Repositories(r.Context(), string(params.Q), searchservice.Sort(sort), limit, cursor, viewerDID)
	if err != nil {
		if searchRequestError(err) {
			handler.writeMalformed(w, r, err)
			return
		}
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Repository, len(page.Repositories))
	for index, repository := range page.Repositories {
		data[index] = networkRepositoryResponse(repository)
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.RepositorySearchPage{Items: data, Page: generated.Page{NextCursor: page.NextCursor}})
}

func (handler *apiHandler) SearchProfiles(w http.ResponseWriter, r *http.Request, params generated.SearchProfilesParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, cursor, sort := searchParameters(r, params.Limit, params.Cursor, params.Sort)
	page, err := handler.deps.Search.Profiles(r.Context(), string(params.Q), searchservice.Sort(sort), limit, cursor, viewerDID)
	if err != nil {
		if searchRequestError(err) {
			handler.writeMalformed(w, r, err)
			return
		}
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.DeveloperProfile, len(page.Profiles))
	for index, developerProfile := range page.Profiles {
		data[index] = developerProfileResponse(developerProfile)
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.ProfileSearchPage{Items: data, Page: generated.Page{NextCursor: page.NextCursor}})
}

func searchParameters[S ~string](r *http.Request, limit *generated.SearchLimit, cursor *generated.SearchCursor, sort *S) (int, string, string) {
	var limitValue int
	if limit != nil {
		limitValue = int(*limit)
	}
	var cursorValue, sortValue string
	if cursor != nil {
		cursorValue = string(*cursor)
	}
	if sort != nil {
		sortValue = string(*sort)
	}
	if _, presented := r.URL.Query()["sort"]; presented {
		sortValue = r.URL.Query().Get("sort")
	}
	return limitValue, cursorValue, sortValue
}

func searchRequestError(err error) bool {
	return errors.Is(err, searchservice.ErrInvalidQuery) || errors.Is(err, searchservice.ErrInvalidSort) ||
		errors.Is(err, searchservice.ErrInvalidLimit) || errors.Is(err, searchservice.ErrInvalidCursor)
}

func (handler *apiHandler) GetSyncRepositories(w http.ResponseWriter, r *http.Request, _ generated.GetSyncRepositoriesParams) {
	handler.sync(w, r, syncproxy.Repositories)
}

func (handler *apiHandler) PostSyncRepositories(w http.ResponseWriter, r *http.Request, _ generated.PostSyncRepositoriesParams) {
	handler.sync(w, r, syncproxy.Repositories)
}

func (handler *apiHandler) GetSyncProfiles(w http.ResponseWriter, r *http.Request, _ generated.GetSyncProfilesParams) {
	handler.sync(w, r, syncproxy.Profiles)
}

func (handler *apiHandler) PostSyncProfiles(w http.ResponseWriter, r *http.Request, _ generated.PostSyncProfilesParams) {
	handler.sync(w, r, syncproxy.Profiles)
}

func (handler *apiHandler) GetSyncStars(w http.ResponseWriter, r *http.Request, _ generated.GetSyncStarsParams) {
	handler.sync(w, r, syncproxy.Stars)
}

func (handler *apiHandler) PostSyncStars(w http.ResponseWriter, r *http.Request, _ generated.PostSyncStarsParams) {
	handler.sync(w, r, syncproxy.Stars)
}

func (handler *apiHandler) GetSyncIssues(w http.ResponseWriter, r *http.Request, _ generated.GetSyncIssuesParams) {
	handler.sync(w, r, syncproxy.Issues)
}

func (handler *apiHandler) PostSyncIssues(w http.ResponseWriter, r *http.Request, _ generated.PostSyncIssuesParams) {
	handler.sync(w, r, syncproxy.Issues)
}

func (handler *apiHandler) GetSyncIssueComments(w http.ResponseWriter, r *http.Request, _ generated.GetSyncIssueCommentsParams) {
	handler.sync(w, r, syncproxy.IssueComments)
}

func (handler *apiHandler) PostSyncIssueComments(w http.ResponseWriter, r *http.Request, _ generated.PostSyncIssueCommentsParams) {
	handler.sync(w, r, syncproxy.IssueComments)
}

func (handler *apiHandler) GetSyncPullRequests(w http.ResponseWriter, r *http.Request, _ generated.GetSyncPullRequestsParams) {
	handler.sync(w, r, syncproxy.PullRequests)
}

func (handler *apiHandler) PostSyncPullRequests(w http.ResponseWriter, r *http.Request, _ generated.PostSyncPullRequestsParams) {
	handler.sync(w, r, syncproxy.PullRequests)
}

func (handler *apiHandler) GetSyncPullRequestReviews(w http.ResponseWriter, r *http.Request, _ generated.GetSyncPullRequestReviewsParams) {
	handler.sync(w, r, syncproxy.PullRequestReviews)
}

func (handler *apiHandler) PostSyncPullRequestReviews(w http.ResponseWriter, r *http.Request, _ generated.PostSyncPullRequestReviewsParams) {
	handler.sync(w, r, syncproxy.PullRequestReviews)
}

func (handler *apiHandler) sync(w http.ResponseWriter, r *http.Request, shape syncproxy.Shape) {
	w.Header().Set("Vary", "Cookie, Authorization")
	if handler.deps.Sync == nil {
		handler.writeAPIError(w, r, http.StatusServiceUnavailable, "sync_disabled", "Realtime sync is not configured", syncproxy.ErrDisabled)
		return
	}
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	policy := syncproxy.Policy{BrowserSession: viewerDID != ""}
	if viewerDID != "" {
		if handler.deps.Moderation == nil {
			handler.writeAPIError(w, r, http.StatusBadGateway, "sync_unavailable", "Realtime sync is unavailable", errors.New("sync moderation is unavailable"))
			return
		}
		blocks, err := handler.deps.Moderation.ListBlocks(r.Context(), viewerDID)
		if err != nil {
			handler.writeAPIError(w, r, http.StatusBadGateway, "sync_unavailable", "Realtime sync is unavailable", err)
			return
		}
		hidden, err := handler.deps.Moderation.ListHidden(r.Context(), viewerDID)
		if err != nil {
			handler.writeAPIError(w, r, http.StatusBadGateway, "sync_unavailable", "Realtime sync is unavailable", err)
			return
		}
		for _, value := range blocks {
			policy.BlockedDIDs = append(policy.BlockedDIDs, value.DID)
		}
		for _, value := range hidden {
			policy.HiddenRecordURIs = append(policy.HiddenRecordURIs, value.URI)
		}
	}
	if err := handler.deps.Sync.Forward(w, r, shape, policy); err != nil {
		if r.Context().Err() != nil {
			return
		}
		switch {
		case errors.Is(err, syncproxy.ErrMalformed):
			handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The sync request is malformed", err)
		case errors.Is(err, syncproxy.ErrBodyTooLarge):
			handler.writeAPIError(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "The sync request body is too large", err)
		case errors.Is(err, syncproxy.ErrDisabled):
			handler.writeAPIError(w, r, http.StatusServiceUnavailable, "sync_disabled", "Realtime sync is not configured", err)
		default:
			handler.writeAPIError(w, r, http.StatusBadGateway, "sync_unavailable", "Realtime sync is unavailable", err)
		}
	}
}

func (handler *apiHandler) GetStars(w http.ResponseWriter, r *http.Request, params generated.GetStarsParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var projection star.Projection
	var next *string
	if pager, ok := handler.deps.Search.(collaborationPager); ok {
		limit, cursor := collectionParameters(params.Limit, params.Cursor)
		page, pageErr := pager.PageStars(r.Context(), params.RepositoryUri, viewerDID, limit, cursor)
		if pageErr != nil {
			handler.writeMalformed(w, r, pageErr)
			return
		}
		projection, next = page.Projection, page.NextCursor
	} else if reader, ok := handler.deps.Search.(collaborationReader); ok {
		projection, err = reader.ListStars(r.Context(), params.RepositoryUri, viewerDID)
	} else {
		projection, err = handler.deps.Stars.Get(r.Context(), params.RepositoryUri)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Star, len(projection.Stars))
	for index, value := range projection.Stars {
		data[index] = projectedStarResponse(value)
	}
	items := data
	if _, paged := handler.deps.Search.(collaborationPager); !paged {
		limit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, limit, cursor, "stars:"+params.RepositoryUri+":"+viewerDID, func(value generated.Star) string { return value.Uri })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.StarList{StarCount: projection.StarCount, Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) PutStar(w http.ResponseWriter, r *http.Request, params generated.PutStarParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	value, err := handler.deps.Stars.Create(r.Context(), identity.accountDID, params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, generated.StarMutation{Star: starEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) DeleteStar(w http.ResponseWriter, r *http.Request, params generated.DeleteStarParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Stars.Delete(r.Context(), identity.accountDID, params.RepositoryUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) GetIssues(w http.ResponseWriter, r *http.Request, params generated.GetIssuesParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var projection issue.Projection
	var next *string
	filter := issueTriageFilter(params)
	if pager, ok := handler.deps.Search.(filteredCollaborationPager); ok {
		limit, cursor := collectionParameters(params.Limit, params.Cursor)
		page, pageErr := pager.PageIssuesFiltered(r.Context(), params.RepositoryUri, viewerDID, limit, cursor, filter)
		if pageErr != nil {
			if errors.Is(pageErr, searchservice.ErrInvalidFilter) {
				handler.writeError(w, r, pageErr)
			} else {
				handler.writeMalformed(w, r, pageErr)
			}
			return
		}
		projection, next = page.Projection, page.NextCursor
	} else if !filter.Empty() {
		handler.writeError(w, r, searchservice.ErrInvalidFilter)
		return
	} else if reader, ok := handler.deps.Search.(collaborationReader); ok {
		projection, err = reader.ListIssues(r.Context(), params.RepositoryUri, viewerDID)
	} else {
		projection, err = handler.deps.Issues.Get(r.Context(), params.RepositoryUri)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]projectedIssueJSON, len(projection.Issues))
	for index, value := range projection.Issues {
		data[index] = projectedIssueResponse(value)
	}
	items := data
	if _, paged := handler.deps.Search.(filteredCollaborationPager); !paged {
		limit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, limit, cursor, "issues:"+params.RepositoryUri+":"+viewerDID, func(value projectedIssueJSON) string { return value.URI })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, issueListJSON{IssueCount: projection.IssueCount, OpenIssueCount: projection.OpenIssueCount, Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetIssue(w http.ResponseWriter, r *http.Request, params generated.GetIssueParams) {
	if resolver, ok := handler.deps.Search.(issueDetailResolver); ok {
		viewerDID, err := handler.optionalSessionViewer(r)
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		value, err := resolver.ResolveIssue(r.Context(), params.RepositoryUri, params.IssueUri, viewerDID)
		if err == nil {
			w.Header().Set("Vary", "Cookie")
			writeJSON(w, http.StatusOK, projectedIssueResponse(value))
			return
		}
		if !errors.Is(err, searchservice.ErrNotFound) {
			handler.writeError(w, r, err)
			return
		}
		handler.writeError(w, r, issue.ErrNotFound)
		return
	}
	projection, err := handler.deps.Issues.Get(r.Context(), params.RepositoryUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	for _, value := range projection.Issues {
		if value.URI == params.IssueUri {
			writeJSON(w, http.StatusOK, projectedIssueResponse(value))
			return
		}
	}
	handler.writeError(w, r, issue.ErrNotFound)
}

func (handler *apiHandler) CreateIssue(w http.ResponseWriter, r *http.Request, _ generated.CreateIssueParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateIssueRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Issues.Create(r.Context(), identity.accountDID, issue.CreateInput{
		RepositoryURI: request.RepositoryUri, Title: request.Title, Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "issue.created", request.RepositoryUri, map[string]any{"issue_uri": value.URI, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.IssueMutation{Issue: issueEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) PutIssueStatus(w http.ResponseWriter, r *http.Request, _ generated.PutIssueStatusParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutIssueStatusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Issues.PutStatus(r.Context(), identity.accountDID, issue.StatusInput{IssueURI: request.IssueUri, State: issue.State(request.State)})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "issue.status_changed", request.IssueUri, map[string]any{"issue_uri": request.IssueUri, "state": request.State, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.IssueStatusMutation{Status: issueStatusEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) ListPullRequests(w http.ResponseWriter, r *http.Request, params generated.ListPullRequestsParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var projection pullrequest.Projection
	var next *string
	filter := pullRequestTriageFilter(params)
	if pager, ok := handler.deps.Search.(filteredCollaborationPager); ok {
		limit, cursor := collectionParameters(params.Limit, params.Cursor)
		page, pageErr := pager.PagePullRequestsFiltered(r.Context(), params.RepositoryUri, viewerDID, limit, cursor, filter)
		if pageErr != nil {
			if errors.Is(pageErr, searchservice.ErrInvalidFilter) {
				handler.writeError(w, r, pageErr)
			} else {
				handler.writeMalformed(w, r, pageErr)
			}
			return
		}
		projection, next = page.Projection, page.NextCursor
	} else if !filter.Empty() {
		handler.writeError(w, r, searchservice.ErrInvalidFilter)
		return
	} else if reader, ok := handler.deps.Search.(collaborationReader); ok {
		projection, err = reader.ListPullRequests(r.Context(), params.RepositoryUri, viewerDID)
	} else {
		projection, err = handler.deps.PullRequests.List(r.Context(), params.RepositoryUri)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.PullRequest, len(projection.PullRequests))
	for index, value := range projection.PullRequests {
		data[index] = projectedPullRequestResponse(value)
	}
	items := data
	if _, paged := handler.deps.Search.(filteredCollaborationPager); !paged {
		limit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, limit, cursor, "pull-requests:"+params.RepositoryUri+":"+viewerDID, func(value generated.PullRequest) string { return value.Uri })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.PullRequestList{PullRequestCount: projection.PullRequestCount, OpenPullRequestCount: projection.OpenPullRequestCount, Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetPullRequest(w http.ResponseWriter, r *http.Request, params generated.GetPullRequestParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var value pullrequest.ProjectedPullRequest
	if reader, ok := handler.deps.Search.(collaborationReader); ok {
		value, err = reader.ResolvePullRequest(r.Context(), params.PullRequestUri, viewerDID)
	} else {
		value, err = handler.deps.PullRequests.Get(r.Context(), params.PullRequestUri)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, projectedPullRequestResponse(value))
}

func (handler *apiHandler) CreatePullRequest(w http.ResponseWriter, r *http.Request, _ generated.CreatePullRequestParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreatePullRequestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.PullRequests.Create(r.Context(), identity.accountDID, pullrequest.CreateInput{
		SourceRepositoryURI: request.SourceRepositoryUri, TargetRepositoryURI: request.TargetRepositoryUri,
		SourceBranch: request.SourceBranch, TargetBranch: request.TargetBranch, HeadSHA: request.HeadSha, Title: request.Title, Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "pull_request.created", request.TargetRepositoryUri, map[string]any{"pull_request_uri": value.URI, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.PullRequestMutation{PullRequest: pullRequestEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) GetPullRequestDiff(w http.ResponseWriter, r *http.Request, params generated.GetPullRequestDiffParams) {
	w.Header().Set("Vary", "Cookie")
	if reader, ok := handler.deps.Search.(collaborationReader); ok {
		viewerDID, err := handler.optionalSessionViewer(r)
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		if _, err := reader.ResolvePullRequest(r.Context(), params.PullRequestUri, viewerDID); err != nil {
			handler.writeError(w, r, err)
			return
		}
	}
	result, err := handler.deps.PullRequests.Refresh(r.Context(), params.PullRequestUri)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.PullRequestDiff{MergeBase: result.MergeBase, HeadRef: result.HeadRef, Diff: diffResponse(result.Diff)})
}

func (handler *apiHandler) ListPullRequestReviews(w http.ResponseWriter, r *http.Request, params generated.ListPullRequestReviewsParams) {
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var values []pullrequest.ProjectedReview
	var next *string
	if pager, ok := handler.deps.Search.(collaborationPager); ok {
		limit, cursor := collectionParameters(params.Limit, params.Cursor)
		page, pageErr := pager.PagePullRequestReviews(r.Context(), params.PullRequestUri, viewerDID, limit, cursor)
		if pageErr != nil {
			handler.writeMalformed(w, r, pageErr)
			return
		}
		values, next = page.Reviews, page.NextCursor
	} else if reader, ok := handler.deps.Search.(collaborationReader); ok {
		values, err = reader.ListPullRequestReviews(r.Context(), params.PullRequestUri, viewerDID)
	} else {
		values, err = handler.deps.PullRequests.Reviews(r.Context(), params.PullRequestUri)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.PullRequestReview, len(values))
	for index, value := range values {
		data[index] = projectedPullRequestReviewResponse(value)
	}
	items := data
	if _, paged := handler.deps.Search.(collaborationPager); !paged {
		limit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, limit, cursor, "pull-request-reviews:"+params.PullRequestUri+":"+viewerDID, func(value generated.PullRequestReview) string { return value.Uri })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	w.Header().Set("Vary", "Cookie")
	writeJSON(w, http.StatusOK, generated.PullRequestReviewList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreatePullRequestReview(w http.ResponseWriter, r *http.Request, _ generated.CreatePullRequestReviewParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreatePullRequestReviewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.PullRequests.CreateReview(r.Context(), identity.accountDID, pullrequest.ReviewInput{PullRequestURI: request.PullRequestUri, Verdict: pullrequest.Verdict(request.Verdict), Body: request.Body})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "review.created", request.PullRequestUri, map[string]any{"review_uri": value.URI, "pull_request_uri": request.PullRequestUri, "verdict": request.Verdict, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.PullRequestReviewMutation{Review: pullRequestReviewEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) PutPullRequestStatus(w http.ResponseWriter, r *http.Request, _ generated.PutPullRequestStatusParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutPullRequestStatusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if request.State != generated.PutPullRequestStatusRequestState("open") && request.State != generated.PutPullRequestStatusRequestState("closed") {
		handler.writeError(w, r, &pullrequest.ValidationError{Field: "state", Problem: "must be open or closed"})
		return
	}
	value, err := handler.deps.PullRequests.PutStatus(r.Context(), identity.accountDID, pullrequest.StatusInput{PullRequestURI: request.PullRequestUri, State: pullrequest.State(request.State)})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "pull_request.status_changed", request.PullRequestUri, map[string]any{"pull_request_uri": request.PullRequestUri, "state": request.State, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.PullRequestStatusMutation{Status: pullRequestStatusEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) MergePullRequest(w http.ResponseWriter, r *http.Request, _ generated.MergePullRequestParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.MergePullRequestRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if request.Strategy != generated.MergePullRequestRequestStrategyMergeCommit && request.Strategy != generated.MergePullRequestRequestStrategySquash {
		handler.writeError(w, r, &pullrequest.ValidationError{Field: "strategy", Problem: "must be merge-commit or squash"})
		return
	}
	result, err := handler.deps.PullRequests.Merge(r.Context(), identity.accountDID, pullrequest.MergeInput{
		PullRequestURI: request.PullRequestUri, Strategy: gitservice.MergeStrategy(request.Strategy),
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.PullRequestMerge{
		MergeCommitSha: result.Git.NewSHA, OldSha: result.Git.OldSHA, HeadSha: result.Git.HeadSHA,
		TargetRef: result.Git.TargetRef, Strategy: generated.PullRequestMergeStrategy(result.Git.Strategy),
		Status: pullRequestStatusEnvelopeResponse(result.Status),
	})
}

func (handler *apiHandler) GetIssueComments(w http.ResponseWriter, r *http.Request, params generated.GetIssueCommentsParams) {
	w.Header().Set("Vary", "Cookie")
	viewerDID, err := handler.optionalSessionViewer(r)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	limit, encodedCursor := collectionParameters(params.Limit, params.Cursor)
	scope := "issue-comments:" + params.IssueUri + ":" + viewerDID
	afterURI := ""
	if encodedCursor != "" {
		cursor, cursorErr := decodePageCursor(encodedCursor, scope)
		if cursorErr != nil {
			handler.writeMalformed(w, r, cursorErr)
			return
		}
		afterURI = cursor.Key
	}
	var projection comment.Projection
	if pager, ok := handler.deps.Comments.(CommentPageManager); ok {
		projection, err = pager.GetPage(r.Context(), params.IssueUri, viewerDID, limit+1, afterURI)
	} else {
		projection, err = handler.deps.Comments.Get(r.Context(), params.IssueUri, viewerDID)
	}
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	values := projection.Comments
	var next *string
	if _, paged := handler.deps.Comments.(CommentPageManager); paged && len(values) > limit {
		values = values[:limit]
		encoded, cursorErr := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: values[len(values)-1].URI})
		if cursorErr != nil {
			handler.writeError(w, r, cursorErr)
			return
		}
		next = &encoded
	}
	data := make([]generated.Comment, len(values))
	for index, value := range values {
		data[index] = projectedCommentResponse(value)
	}
	items := data
	if _, paged := handler.deps.Comments.(CommentPageManager); !paged {
		requestedLimit, cursor := paginationInputs(params.Limit, params.Cursor)
		items, next, err = paginate(data, requestedLimit, cursor, scope, func(value generated.Comment) string { return value.Uri })
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, generated.CommentList{CommentCount: projection.CommentCount, Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) CreateIssueComment(w http.ResponseWriter, r *http.Request, _ generated.CreateIssueCommentParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.CreateCommentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	value, err := handler.deps.Comments.Create(r.Context(), identity.accountDID, comment.CreateInput{
		IssueURI: request.IssueUri, ParentURI: optionalString(request.ParentUri), Body: request.Body,
	})
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	handler.recordRepositoryActivity(r, "issue.comment_created", request.IssueUri, map[string]any{"comment_uri": value.URI, "issue_uri": request.IssueUri, "actor_did": identity.accountDID})
	writeJSON(w, http.StatusAccepted, generated.CommentMutation{Comment: commentEnvelopeResponse(value), Projected: false})
}

func (handler *apiHandler) recordRepositoryActivity(r *http.Request, eventType, subjectURI string, payload any) {
	if handler.deps.Activity == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	if err := handler.deps.Activity.RepositoryActivity(ctx, eventType, subjectURI, payload); err != nil {
		handler.logger.ErrorContext(ctx, "record repository activity", "event_type", eventType, "request_id", requestIDFromContext(ctx), "error", err)
	}
}

func (handler *apiHandler) DeleteIssueComment(w http.ResponseWriter, r *http.Request, params generated.DeleteIssueCommentParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Comments.Delete(r.Context(), identity.accountDID, params.CommentUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (handler *apiHandler) GetModeration(w http.ResponseWriter, r *http.Request) {
	identity, err := handler.requireSession(r, false)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	blocks, err := handler.deps.Moderation.ListBlocks(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	hidden, err := handler.deps.Moderation.ListHidden(r.Context(), identity.accountDID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	blockedDIDs := make([]string, len(blocks))
	for index, value := range blocks {
		blockedDIDs[index] = value.DID
	}
	hiddenRecords := make([]string, len(hidden))
	for index, value := range hidden {
		hiddenRecords[index] = value.URI
	}
	writeJSON(w, http.StatusOK, generated.Moderation{BlockedDids: blockedDIDs, HiddenRecords: hiddenRecords})
}

func (handler *apiHandler) PutBlockedDID(w http.ResponseWriter, r *http.Request, _ generated.PutBlockedDIDParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutBlockedDIDRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Block(r.Context(), identity.accountDID, request.BlockedDid); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) DeleteBlockedDID(w http.ResponseWriter, r *http.Request, params generated.DeleteBlockedDIDParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Unblock(r.Context(), identity.accountDID, params.BlockedDid); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) PutHiddenRecord(w http.ResponseWriter, r *http.Request, _ generated.PutHiddenRecordParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	var request generated.PutHiddenRecordRequest
	if err := decodeJSON(w, r, &request); err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Hide(r.Context(), identity.accountDID, request.RecordUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) DeleteHiddenRecord(w http.ResponseWriter, r *http.Request, params generated.DeleteHiddenRecordParams) {
	identity, err := handler.requireSession(r, true)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.deps.Moderation.Unhide(r.Context(), identity.accountDID, params.RecordUri); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *apiHandler) ListRepositoryBranches(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListRepositoryBranchesParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	branches, err := handler.deps.Git.Branches(r.Context(), repo.ID, repo.DefaultBranch)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Branch, len(branches))
	for index, branch := range branches {
		data[index] = generated.Branch{Name: branch.Name, Sha: branch.SHA, Default: branch.Default}
	}
	limit, cursor := paginationInputs(params.Limit, params.Cursor)
	items, next, err := paginate(data, limit, cursor, "repository-branches:"+repo.ID.String(), func(value generated.Branch) string { return value.Name })
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.BranchList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) ListRepositoryTags(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListRepositoryTagsParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	tags, err := handler.deps.Git.Tags(r.Context(), repo.ID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.Tag, len(tags))
	for index, tag := range tags {
		data[index] = generated.Tag{Name: tag.Name, Sha: tag.SHA, ObjectType: tag.ObjectType, TargetSha: tag.PeeledSHA, TargetType: tag.PeeledType}
	}
	limit, cursor := paginationInputs(params.Limit, params.Cursor)
	items, next, err := paginate(data, limit, cursor, "repository-tags:"+repo.ID.String(), func(value generated.Tag) string { return value.Name })
	if err != nil {
		handler.writeMalformed(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.TagList{Items: items, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetRepositoryTree(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryTreeParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	revision := repo.DefaultBranch
	if params.Rev != nil {
		revision = *params.Rev
	}
	treePath := ""
	if params.Path != nil {
		treePath = *params.Path
	}
	tree, err := handler.deps.Git.Tree(r.Context(), repo.ID, revision, treePath)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	entries := make([]generated.TreeEntry, len(tree.Entries))
	for index, entry := range tree.Entries {
		var size *int64
		if entry.Size >= 0 {
			value := entry.Size
			size = &value
		}
		entries[index] = generated.TreeEntry{Name: entry.Name, Path: entry.Path, Mode: entry.Mode,
			Type: generated.TreeEntryType(entry.Type), Sha: entry.SHA, Size: size}
	}
	writeJSON(w, http.StatusOK, generated.Tree{Revision: revision, CommitSha: tree.CommitSHA, Path: tree.Path, Entries: entries})
}

func (handler *apiHandler) GetRepositoryBlob(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, sha string) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	metadata, err := handler.deps.Git.BlobMetadata(r.Context(), repo.ID, sha)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	w.Header().Set("ETag", `"`+metadata.SHA+`"`)
	if repo.Visibility == repository.VisibilityPublic {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	if err := handler.deps.Git.StreamBlob(r.Context(), repo.ID, sha, w); err != nil {
		handler.logger.ErrorContext(r.Context(), "stream repository blob", "request_id", requestIDFromContext(r.Context()), "error", err)
		trace.SpanFromContext(r.Context()).RecordError(err)
	}
}

func (handler *apiHandler) ListRepositoryCommits(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.ListRepositoryCommitsParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	ref := repo.DefaultBranch
	if params.Ref != nil {
		ref = *params.Ref
	}
	scope := "repository-commits:" + repo.ID.String() + ":" + ref
	if params.Cursor != nil && *params.Cursor != "" {
		cursor, err := decodePageCursor(string(*params.Cursor), scope)
		if err != nil {
			handler.writeMalformed(w, r, err)
			return
		}
		ref = cursor.Key + "^"
	}
	limit := 30
	if params.Limit != nil {
		limit = *params.Limit
	}
	commits, err := handler.deps.Git.Commits(r.Context(), repo.ID, ref, limit)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	data := make([]generated.CommitSummary, len(commits))
	for index, commit := range commits {
		data[index] = commitSummaryResponse(commit)
	}
	var next *string
	if len(commits) == limit && len(commits[len(commits)-1].Parents) > 0 {
		encoded, err := encodePageCursor(pageCursor{Version: cursorVersion, Scope: scope, Key: commits[len(commits)-1].SHA})
		if err != nil {
			handler.writeError(w, r, err)
			return
		}
		next = &encoded
	}
	writeJSON(w, http.StatusOK, generated.CommitList{Items: data, Page: generated.Page{NextCursor: next}})
}

func (handler *apiHandler) GetRepositoryCommit(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, revision string) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	commit, err := handler.deps.Git.Commit(r.Context(), repo.ID, revision)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, commitResponse(commit))
}

func (handler *apiHandler) GetRepositoryDiff(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryDiffParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	diff, err := handler.deps.Git.Diff(r.Context(), repo.ID, params.Base, params.Head)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	files := make([]generated.DiffFile, len(diff.Files))
	for index, file := range diff.Files {
		files[index] = generated.DiffFile{Status: file.Status, OldPath: file.OldPath, NewPath: file.NewPath,
			Additions: file.Additions, Deletions: file.Deletions}
	}
	writeJSON(w, http.StatusOK, generated.Diff{BaseSha: diff.BaseSHA, HeadSha: diff.HeadSHA, Files: files, Patch: diff.Patch})
}

func (handler *apiHandler) GetRepositoryMergeBase(w http.ResponseWriter, r *http.Request, owner string, slug generated.RepositorySlug, params generated.GetRepositoryMergeBaseParams) {
	repo, err := handler.readableRepository(r, owner, slug)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	sha, err := handler.deps.Git.MergeBase(r.Context(), repo.ID, params.A, params.B)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, generated.MergeBase{Sha: sha})
}

func (handler *apiHandler) readableRepository(r *http.Request, owner, slug string) (repository.Repository, error) {
	repo, err := handler.deps.Repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if err != nil || repo.State != repository.StateActive {
		return repository.Repository{}, repository.ErrNotFound
	}
	if repo.Visibility == repository.VisibilityPublic {
		return repo, nil
	}
	identity, err := handler.authenticate(r)
	if err != nil {
		return repository.Repository{}, err
	}
	if !identity.session {
		if (!slices.Contains(identity.scopes, auth.ScopeRepositoryRead) && !slices.Contains(identity.scopes, auth.ScopeRepositoryWrite)) ||
			(identity.repositoryID != nil && *identity.repositoryID != repo.ID) {
			return repository.Repository{}, repository.ErrNotFound
		}
	}
	allowed, err := handler.deps.Authorization.CanReadRepository(r.Context(), identity.accountDID, repo.ID)
	if err != nil {
		return repository.Repository{}, fmt.Errorf("authorize repository read: %w", err)
	}
	if !allowed {
		return repository.Repository{}, repository.ErrNotFound
	}
	return repo, nil
}

func (handler *apiHandler) authenticate(r *http.Request) (principal, error) {
	if cookie, err := r.Cookie("adenosine_session"); err == nil {
		if handler.deps.Sessions == nil {
			return principal{}, auth.ErrUnauthorized
		}
		identity, err := handler.deps.Sessions.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			return principal{}, err
		}
		return principal{accountDID: identity.AccountDID, sessionID: identity.SessionID, session: true}, nil
	}
	plaintext, ok := bearerToken(r)
	if !ok || handler.deps.TokenAuth == nil {
		return principal{}, auth.ErrUnauthorized
	}
	token, err := handler.deps.TokenAuth.Authenticate(r.Context(), plaintext)
	if err != nil {
		return principal{}, err
	}
	return principal{accountDID: token.AccountDID, scopes: token.Scopes, repositoryID: token.RepositoryID}, nil
}

func (handler *apiHandler) optionalSessionViewer(r *http.Request) (string, error) {
	cookie, err := r.Cookie("adenosine_session")
	if errors.Is(err, http.ErrNoCookie) {
		return "", nil
	}
	if err != nil || handler.deps.Sessions == nil {
		return "", auth.ErrUnauthorized
	}
	identity, err := handler.deps.Sessions.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return "", err
	}
	return identity.AccountDID, nil
}

func (handler *apiHandler) requireSession(r *http.Request, mutation bool) (principal, error) {
	identity, err := handler.authenticate(r)
	if err != nil {
		return principal{}, err
	}
	if !identity.session {
		return principal{}, auth.ErrForbidden
	}
	if mutation && !handler.validOrigin(r) {
		return principal{}, auth.ErrForbidden
	}
	return identity, nil
}

func (handler *apiHandler) validOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == handler.origin
}

func bearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", false
	}
	value := strings.TrimPrefix(values[0], "Bearer ")
	return value, value != "" && !strings.ContainsAny(value, " \t\r\n")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeJSONLimit(w, r, destination, maxJSONBody)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}

type passkeyVerificationRequest struct {
	CeremonyToken string          `json:"ceremony_token"`
	Response      json.RawMessage `json:"response"`
}

type passkeyOptionsResponse struct {
	Options       json.RawMessage `json:"options"`
	CeremonyToken string          `json:"ceremony_token"`
}

func decodePasskeyVerification(w http.ResponseWriter, r *http.Request) (passkeyVerificationRequest, error) {
	var request passkeyVerificationRequest
	if err := decodeJSONLimit(w, r, &request, maxWebAuthnVerifyBody); err != nil {
		return passkeyVerificationRequest{}, err
	}
	var object map[string]json.RawMessage
	if len(request.Response) == 0 || json.Unmarshal(request.Response, &object) != nil || object == nil {
		return passkeyVerificationRequest{}, errors.New("response must be a JSON object")
	}
	return request, nil
}

func (handler *apiHandler) writeMalformed(w http.ResponseWriter, r *http.Request, err error) {
	handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The request is malformed", err)
}

func (handler *apiHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		handler.writeAPIError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required", err)
	case errors.Is(err, auth.ErrForbidden):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "Permission denied", err)
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, repository.ErrNotFound), errors.Is(err, owner.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found", err)
	case errors.Is(err, auth.ErrConflict), errors.Is(err, repository.ErrAlreadyExists):
		handler.writeAPIError(w, r, http.StatusConflict, "conflict", "The request conflicts with existing state", err)
	case errors.Is(err, auth.ErrValidation), errors.Is(err, repository.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The request is invalid", err)
	case errors.Is(err, webhookservice.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested webhook resource was not found", err)
	case errors.Is(err, webhookservice.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The webhook request is invalid", err)
	case errors.Is(err, branchprotection.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested branch protection was not found", err)
	case errors.Is(err, branchprotection.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "branch_protection_conflict", "The branch protection already exists", err)
	case errors.Is(err, branchprotection.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The branch protection request is invalid", err)
	case errors.Is(err, organization.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested organization resource was not found", err)
	case errors.Is(err, organization.ErrAlreadyExists):
		handler.writeAPIError(w, r, http.StatusConflict, "organization_conflict", "The organization or membership already exists", err)
	case errors.Is(err, organization.ErrForbidden):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "You do not have permission to manage this organization", err)
	case errors.Is(err, organization.ErrLastOwner):
		handler.writeAPIError(w, r, http.StatusConflict, "last_organization_owner", "An organization must retain at least one owner", err)
	case errors.Is(err, organization.ErrCreatorOwner):
		handler.writeAPIError(w, r, http.StatusConflict, "organization_creator_owner", "The organization creator must remain an owner while they control its AT Protocol root", err)
	case errors.Is(err, organization.ErrInvitation):
		handler.writeAPIError(w, r, http.StatusConflict, "invitation_unavailable", "The invitation is expired, revoked, or belongs to another account", err)
	case errors.Is(err, organization.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The organization request is invalid", err)
	case errors.Is(err, profile.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "profile_provider_unavailable", "The profile provider is unavailable", err)
	case errors.Is(err, profile.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, profile.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The profile is invalid", err)
	case errors.Is(err, profile.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested profile was not found", err)
	case errors.Is(err, star.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested repository was not found", err)
	case errors.Is(err, star.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The star request is invalid", err)
	case errors.Is(err, star.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, star.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "star_conflict", "The star conflicts with existing state", err)
	case errors.Is(err, star.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "star_provider_unavailable", "The star provider is unavailable", err)
	case errors.Is(err, issue.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested issue or repository was not found", err)
	case errors.Is(err, issue.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The issue request is invalid", err)
	case errors.Is(err, issue.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, issue.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "issue_conflict", "The issue conflicts with existing state", err)
	case errors.Is(err, issue.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "issue_provider_unavailable", "The issue provider is unavailable", err)
	case errors.Is(err, pullrequest.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested pull request or repository was not found", err)
	case errors.Is(err, pullrequest.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The pull request is invalid", err)
	case errors.Is(err, pullrequest.ErrPermissionDenied):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "Permission denied", err)
	case errors.Is(err, pullrequest.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusConflict, "atproto_authorization_required", "AT Protocol authorization is required", err)
	case errors.Is(err, pullrequest.ErrConflict), errors.Is(err, pullrequest.ErrProjectionChanged):
		handler.writeAPIError(w, r, http.StatusConflict, "pull_request_conflict", "The pull request conflicts with existing state", err)
	case errors.Is(err, pullrequest.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "pull_request_provider_unavailable", "The pull request provider is unavailable", err)
	case errors.Is(err, transfer.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested repository transfer was not found", err)
	case errors.Is(err, transfer.ErrForbidden):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "Permission denied", err)
	case errors.Is(err, transfer.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "repository_transfer_conflict", "The repository transfer conflicts with existing state", err)
	case errors.Is(err, transfer.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The repository transfer request is invalid", err)
	case errors.Is(err, transfer.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "repository_transfer_provider_unavailable", "The repository transfer provider is unavailable", err)
	case errors.Is(err, triage.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested triage resource was not found", err)
	case errors.Is(err, triage.ErrAuthorization):
		handler.writeAPIError(w, r, http.StatusForbidden, "permission_denied", "You do not have permission to manage repository triage", err)
	case errors.Is(err, triage.ErrConflict):
		handler.writeAPIError(w, r, http.StatusConflict, "triage_conflict", "The triage record conflicts with existing state", err)
	case errors.Is(err, triage.ErrValidation):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The triage request is invalid", err)
	case errors.Is(err, triage.ErrProvider):
		handler.writeAPIError(w, r, http.StatusBadGateway, "triage_provider_unavailable", "The triage provider is unavailable", err)
	case errors.Is(err, gitservice.ErrInvalidInput):
		handler.writeAPIError(w, r, http.StatusBadRequest, "malformed_request", "The Git revision, path, or object ID is invalid", err)
	case errors.Is(err, gitservice.ErrObjectNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested Git object was not found", err)
	case errors.Is(err, gitservice.ErrUnsupportedObject):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "unsupported_git_object", "The Git object type is unsupported for this operation", err)
	case errors.Is(err, gitservice.ErrOutputLimit):
		handler.writeAPIError(w, r, http.StatusRequestEntityTooLarge, "git_output_too_large", "The repository output exceeds the supported limit", err)
	case errors.Is(err, gitservice.ErrForkDiverged):
		handler.writeAPIError(w, r, http.StatusConflict, "fork_diverged", "The fork has commits that are not in its upstream", err)
	case errors.Is(err, gitservice.ErrRemoteInput):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "invalid_fork_source", "The fork source is not a safe canonical Git endpoint", err)
	case errors.Is(err, gitservice.ErrRemoteAddress):
		handler.writeAPIError(w, r, http.StatusBadGateway, "fork_upstream_unavailable", "The fork upstream is unavailable", err)
	case errors.Is(err, searchservice.ErrNotFound):
		handler.writeAPIError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found", err)
	case errors.Is(err, searchservice.ErrInvalidFilter):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "validation_failed", "The collaboration filter is invalid", err)
	case errors.Is(err, localatproto.ErrInvalidIdentifier):
		handler.writeAPIError(w, r, http.StatusUnprocessableEntity, "invalid_atproto_identifier", "The AT Protocol handle or DID is invalid", err)
	case errors.Is(err, localatproto.ErrProviderFailure):
		handler.writeAPIError(w, r, http.StatusBadGateway, "oauth_provider_unavailable", "The AT Protocol OAuth provider is unavailable", err)
	case errors.Is(err, localatproto.ErrCallbackFailure):
		handler.writeAPIError(w, r, http.StatusBadRequest, "oauth_callback_failed", "The AT Protocol OAuth callback could not be verified", err)
	default:
		handler.writeAPIError(w, r, http.StatusInternalServerError, "internal_error", "An internal error occurred", err)
	}
}

func (handler *apiHandler) writeAPIError(w http.ResponseWriter, r *http.Request, status int, code, message string, err error) {
	if status >= 500 {
		handler.logger.ErrorContext(r.Context(), "REST request failed", "request_id", requestIDFromContext(r.Context()), "error", err)
	}
	writeJSON(w, status, generated.ErrorResponse{Error: generated.Error{
		Code: code, Message: message, RequestId: requestIDFromContext(r.Context()),
	}})
}

func accessTokenResponse(token auth.AccessToken) generated.AccessToken {
	scopes := make([]generated.AccessTokenScopes, len(token.Scopes))
	for index, scope := range token.Scopes {
		scopes[index] = generated.AccessTokenScopes(scope)
	}
	var repositoryID *openapi_types.UUID
	if token.RepositoryID != nil {
		value := openapi_types.UUID(*token.RepositoryID)
		repositoryID = &value
	}
	return generated.AccessToken{Id: openapi_types.UUID(token.ID), Name: token.Name, Prefix: token.Prefix,
		Scopes: scopes, RepositoryId: repositoryID, CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt}
}

func createdAccessTokenResponse(token auth.AccessToken, plaintext string) generated.CreatedAccessToken {
	scopes := make([]generated.CreatedAccessTokenScopes, len(token.Scopes))
	for index, scope := range token.Scopes {
		scopes[index] = generated.CreatedAccessTokenScopes(scope)
	}
	var repositoryID *openapi_types.UUID
	if token.RepositoryID != nil {
		value := openapi_types.UUID(*token.RepositoryID)
		repositoryID = &value
	}
	return generated.CreatedAccessToken{Id: openapi_types.UUID(token.ID), Name: token.Name, Prefix: token.Prefix,
		Token: plaintext, Scopes: scopes, RepositoryId: repositoryID, CreatedAt: token.CreatedAt,
		ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt}
}

func sshKeyResponse(key auth.SSHKey) generated.SSHKey {
	return generated.SSHKey{Id: openapi_types.UUID(key.ID), Name: key.Name, Algorithm: key.Algorithm,
		PublicKey: key.PublicKey, Fingerprint: key.Fingerprint, CreatedAt: key.CreatedAt, LastUsedAt: key.LastUsedAt}
}

func organizationResponse(value organization.Organization, viewerRole *organization.Role) generated.Organization {
	var role *generated.OrganizationViewerRole
	if viewerRole != nil {
		converted := generated.OrganizationViewerRole(*viewerRole)
		role = &converted
	}
	return generated.Organization{
		Id: openapi_types.UUID(value.ID), Slug: value.Slug, Name: value.Name,
		Description: pointerUnlessEmpty(value.Description), Website: pointerUnlessEmpty(value.Website), Location: pointerUnlessEmpty(value.Location),
		CreatorDid: value.CreatorDID, BasePermission: generated.OrganizationBasePermission(value.BasePermission),
		MembersCanCreateRepositories: value.MembersCanCreateRepo, State: generated.OrganizationState(value.State),
		Uri: pointerUnlessEmpty(value.ATURI), Cid: pointerUnlessEmpty(value.ATCID), ViewerRole: role,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func organizationMemberResponse(value organization.Member) generated.OrganizationMember {
	return generated.OrganizationMember{
		Did: value.AccountDID, Handle: pointerUnlessEmpty(value.Handle), Role: generated.OrganizationMemberRole(value.Role),
		Visibility: generated.OrganizationMemberVisibility(value.Visibility), JoinedAt: value.JoinedAt, UpdatedAt: value.UpdatedAt,
	}
}

func organizationInvitationResponse(value organization.Invitation) generated.OrganizationInvitation {
	return generated.OrganizationInvitation{
		Id: openapi_types.UUID(value.ID), OrganizationId: openapi_types.UUID(value.OrganizationID),
		OrganizationSlug: pointerUnlessEmpty(value.OrganizationSlug), OrganizationName: pointerUnlessEmpty(value.OrganizationName),
		InviteeDid: value.InviteeDID, InvitedByDid: value.InvitedByDID, Role: generated.OrganizationInvitationRole(value.Role),
		CreatedAt: value.CreatedAt, ExpiresAt: value.ExpiresAt,
	}
}

func organizationTeamResponse(value organization.Team) generated.OrganizationTeam {
	var viewerRole *generated.OrganizationTeamViewerRole
	if value.ViewerRole != "" {
		role := generated.OrganizationTeamViewerRole(value.ViewerRole)
		viewerRole = &role
	}
	var parentTeamID *openapi_types.UUID
	if value.ParentTeamID != nil {
		id := openapi_types.UUID(*value.ParentTeamID)
		parentTeamID = &id
	}
	return generated.OrganizationTeam{Id: openapi_types.UUID(value.ID), OrganizationId: openapi_types.UUID(value.OrganizationID), ParentTeamId: parentTeamID, Slug: value.Slug, Name: value.Name, Description: pointerUnlessEmpty(value.Description), Visibility: generated.OrganizationTeamVisibility(value.Visibility), ViewerRole: viewerRole, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func organizationAuditEventResponse(value organization.AuditEvent) generated.OrganizationAuditEvent {
	metadata := map[string]interface{}{}
	if len(value.Metadata) > 0 {
		_ = json.Unmarshal(value.Metadata, &metadata)
	}
	return generated.OrganizationAuditEvent{Id: openapi_types.UUID(value.ID), ActorDid: value.ActorDID, Action: value.Action, TargetType: value.TargetType, TargetId: value.TargetID, RequestId: pointerUnlessEmpty(value.RequestID), Metadata: metadata, CreatedAt: value.CreatedAt}
}

func organizationTeamMemberResponse(value organization.TeamMember) generated.OrganizationTeamMember {
	return generated.OrganizationTeamMember{Did: value.AccountDID, Handle: pointerUnlessEmpty(value.Handle), Role: generated.OrganizationTeamMemberRole(value.Role), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func organizationTeamRepositoryResponse(value organization.TeamRepository) generated.OrganizationTeamRepository {
	return generated.OrganizationTeamRepository{RepositoryId: openapi_types.UUID(value.RepositoryID), RepositorySlug: value.RepositorySlug, Role: generated.OrganizationTeamRepositoryRole(value.Role), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func organizationRepositoryCollaboratorResponse(value organization.RepositoryCollaborator) generated.OrganizationRepositoryCollaborator {
	return generated.OrganizationRepositoryCollaborator{RepositoryId: openapi_types.UUID(value.RepositoryID), RepositorySlug: pointerUnlessEmpty(value.RepositorySlug), Did: value.AccountDID, Handle: pointerUnlessEmpty(value.Handle), Role: generated.OrganizationRepositoryCollaboratorRole(value.Role), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func passkeyResponse(credential passkey.CredentialSummary) generated.Passkey {
	return generated.Passkey{Id: openapi_types.UUID(credential.ID), Name: credential.Name, CreatedAt: credential.CreatedAt, LastUsedAt: credential.LastUsedAt}
}

func developerProfileResponse(value profile.Profile) generated.DeveloperProfile {
	var recordCreatedAt *time.Time
	if !value.RecordCreatedAt.IsZero() {
		recordCreatedAt = &value.RecordCreatedAt
	}
	return generated.DeveloperProfile{
		Did: value.DID, Uri: pointerUnlessEmpty(value.URI), Cid: pointerUnlessEmpty(value.CID),
		Handle: pointerUnlessEmpty(value.Handle), DisplayName: pointerUnlessEmpty(value.DisplayName),
		Bio: pointerUnlessEmpty(value.Bio), AvatarRef: pointerUnlessEmpty(value.AvatarRef),
		Website: pointerUnlessEmpty(value.Website), Location: pointerUnlessEmpty(value.Location),
		RepositoryCount: value.RepositoryCount, ContributionCount: value.ContributionCount,
		RecordCreatedAt: recordCreatedAt, IndexedAt: value.IndexedAt,
	}
}

func commitIdentityResponse(identity gitservice.CommitIdentity) generated.CommitIdentity {
	return generated.CommitIdentity{Name: identity.Name, Email: identity.Email, Date: identity.Time}
}

func commitSummaryResponse(commit gitservice.CommitSummary) generated.CommitSummary {
	return generated.CommitSummary{Sha: commit.SHA, Parents: commit.Parents, Summary: commit.Summary,
		Author: commitIdentityResponse(commit.Author), Committer: commitIdentityResponse(commit.Committer)}
}

func commitResponse(commit gitservice.Commit) generated.Commit {
	return generated.Commit{Sha: commit.SHA, Parents: commit.Parents, Summary: commit.Summary, Message: commit.Message,
		Author: commitIdentityResponse(commit.Author), Committer: commitIdentityResponse(commit.Committer)}
}

func diffResponse(diff gitservice.Diff) generated.Diff {
	files := make([]generated.DiffFile, len(diff.Files))
	for index, file := range diff.Files {
		files[index] = generated.DiffFile{Status: file.Status, OldPath: file.OldPath, NewPath: file.NewPath, Additions: file.Additions, Deletions: file.Deletions}
	}
	return generated.Diff{BaseSha: diff.BaseSHA, HeadSha: diff.HeadSHA, Files: files, Patch: diff.Patch}
}

func (handler *apiHandler) repositoryResponse(repo repository.Repository) generated.Repository {
	webURL := handler.baseURL + "/" + repo.OwnerDID + "/" + repo.Slug
	gitHTTPSURL := webURL + ".git"
	gitSSHURL := ""
	if handler.deps.Endpoints != nil {
		webURL, gitHTTPSURL, gitSSHURL = handler.deps.Endpoints.For(repo)
	}
	id := openapi_types.UUID(repo.ID)
	viewerCanAdmin := repo.ViewerCanAdmin
	var forkedFrom *generated.RepositoryStrongRef
	if repo.ForkedFrom != nil {
		forkedFrom = &generated.RepositoryStrongRef{Uri: repo.ForkedFrom.URI, Cid: repo.ForkedFrom.CID}
	}
	return generated.Repository{
		Id: &id, Uri: pointerUnlessEmpty(repo.ATURI), Cid: pointerUnlessEmpty(repo.ATCID), Slug: repo.Slug, DisplayName: pointerUnlessEmpty(repo.DisplayName),
		Description: pointerUnlessEmpty(repo.Description), Visibility: generated.RepositoryVisibility(repo.Visibility),
		State: generated.RepositoryState(repo.State), DefaultBranch: repo.DefaultBranch,
		Archived:       repo.ArchivedAt != nil,
		ViewerCanAdmin: &viewerCanAdmin,
		ForkedFrom:     forkedFrom, ForkCount: repo.ForkCount,
		Owner: repositoryOwnerResponse(repo), CreatedAt: repo.CreatedAt, UpdatedAt: repo.UpdatedAt,
		Hosting: generated.RepositoryHosting{Local: true, WebUrl: webURL, GitHttpsUrl: gitHTTPSURL, GitSshUrl: pointerUnlessEmpty(gitSSHURL), SourceBrowsing: generated.Local},
	}
}

func repositoryDeletionResponse(value repository.Deletion) generated.RepositoryDeletion {
	return generated.RepositoryDeletion{Id: openapi_types.UUID(value.ID), RepositoryId: openapi_types.UUID(value.RepositoryID), RequestedAt: value.RequestedAt, PurgeAfter: value.PurgeAfter}
}

func notificationResponse(value notification.Notification) generated.Notification {
	return generated.Notification{
		Id: openapi_types.UUID(value.ID), Kind: generated.NotificationKind(value.Kind), ActorDid: value.ActorDID,
		RepositoryUri: value.RepositoryURI, Owner: value.Owner, RepositorySlug: value.RepositorySlug,
		SubjectUri: value.SubjectURI, SubjectKind: generated.NotificationSubjectKind(value.SubjectKind),
		Title: value.Title, OccurredAt: value.OccurredAt, Read: value.Read,
	}
}

func repositoryWebhookResponse(value webhookservice.Webhook) generated.RepositoryWebhook {
	events := make([]generated.WebhookEvent, len(value.Events))
	for index, event := range value.Events {
		events[index] = generated.WebhookEvent(event)
	}
	return generated.RepositoryWebhook{Id: openapi_types.UUID(value.ID), Url: value.URL, Events: events, Enabled: value.Enabled, HasSecret: generated.RepositoryWebhookHasSecret(true), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func webhookDeliveryResponse(value webhookservice.Delivery) generated.WebhookDelivery {
	return generated.WebhookDelivery{Id: openapi_types.UUID(value.ID), WebhookId: openapi_types.UUID(value.WebhookID), Event: generated.WebhookEvent(value.EventType), Attempts: value.Attempts, ResponseStatus: value.ResponseStatus, ResponseBody: pointerUnlessEmpty(value.ResponseBody), DeliveredAt: value.DeliveredAt, FailedAt: value.FailedAt, LastErrorCode: pointerUnlessEmpty(value.LastErrorCode), CreatedAt: value.CreatedAt}
}

func webhookEventStrings(values []generated.WebhookEvent) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func branchProtectionInput(value generated.BranchProtectionInput) branchprotection.Input {
	return branchprotection.Input{Pattern: string(value.Pattern), DenyForcePush: value.DenyForcePush, DenyDeletion: value.DenyDeletion}
}

func branchProtectionResponse(value branchprotection.Protection) generated.BranchProtection {
	return generated.BranchProtection{Id: openapi_types.UUID(value.ID), Pattern: generated.BranchProtectionPattern(value.Pattern), DenyForcePush: value.DenyForcePush, DenyDeletion: value.DenyDeletion, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func networkRepositoryResponse(repo federation.DiscoveryRepository) generated.Repository {
	var id *openapi_types.UUID
	if repo.LocalRepositoryID != nil {
		value := openapi_types.UUID(*repo.LocalRepositoryID)
		id = &value
	}
	owner := generated.RepositoryOwner{Did: repo.OwnerDID, Handle: pointerUnlessEmpty(repo.OwnerHandle), Kind: repositoryOwnerKind(generated.RepositoryOwnerKindAccount)}
	if repo.OrganizationSlug != "" {
		slug := generated.OrganizationSlug(repo.OrganizationSlug)
		owner.Kind = repositoryOwnerKind(generated.RepositoryOwnerKindOrganization)
		owner.OrganizationSlug = &slug
	}
	var forkedFrom *generated.RepositoryStrongRef
	if repo.ForkedFrom != nil {
		forkedFrom = &generated.RepositoryStrongRef{Uri: repo.ForkedFrom.URI, Cid: repo.ForkedFrom.CID}
	}
	return generated.Repository{
		Id: id, Uri: &repo.URI, Cid: pointerUnlessEmpty(repo.CID), Slug: repo.Slug, DisplayName: pointerUnlessEmpty(repo.Name), Archived: false,
		Description: pointerUnlessEmpty(repo.Description), Visibility: generated.RepositoryVisibilityPublic,
		State: generated.RepositoryStateActive, DefaultBranch: repo.DefaultBranch,
		Owner:      owner,
		ForkedFrom: forkedFrom, ForkCount: repo.ForkCount,
		StarCount: repo.StarCount, IssueCount: repo.IssueCount, OpenIssueCount: repo.OpenIssueCount,
		CommentCount: repo.CommentCount, PullRequestCount: repo.PullRequestCount, OpenPullRequestCount: repo.OpenPullRequestCount,
		Hosting: generated.RepositoryHosting{Local: repo.LocalRepositoryID != nil, WebUrl: repo.Web,
			GitHttpsUrl: repo.GitHTTPS, GitSshUrl: pointerUnlessEmpty(repo.GitSSH), SourceBrowsing: sourceBrowsing(repo)},
		CreatedAt: repo.CreatedAt, UpdatedAt: repo.UpdatedAt,
	}
}

func applyRepositoryCounters(response *generated.Repository, projection federation.DiscoveryRepository) {
	response.ForkCount = projection.ForkCount
	response.StarCount = projection.StarCount
	response.IssueCount = projection.IssueCount
	response.OpenIssueCount = projection.OpenIssueCount
	response.CommentCount = projection.CommentCount
	response.PullRequestCount = projection.PullRequestCount
	response.OpenPullRequestCount = projection.OpenPullRequestCount
}

func repositoryOwnerResponse(repo repository.Repository) generated.RepositoryOwner {
	owner := generated.RepositoryOwner{Did: repo.OwnerDID, Kind: repositoryOwnerKind(generated.RepositoryOwnerKindAccount)}
	if repo.OrganizationSlug != "" {
		slug := generated.OrganizationSlug(repo.OrganizationSlug)
		owner.Kind = repositoryOwnerKind(generated.RepositoryOwnerKindOrganization)
		owner.OrganizationSlug = &slug
	}
	return owner
}

func repositoryOwnerKind(value generated.RepositoryOwnerKind) *generated.RepositoryOwnerKind {
	return &value
}

func repositoryRouteOwner(repo repository.Repository) string {
	if repo.OrganizationSlug != "" {
		return repo.OrganizationSlug
	}
	return repo.OwnerDID
}

func sourceBrowsing(repo federation.DiscoveryRepository) generated.RepositoryHostingSourceBrowsing {
	if repo.LocalRepositoryID != nil {
		return generated.Local
	}
	return generated.CanonicalHost
}

func projectedStarResponse(value star.Star) generated.Star {
	return generated.Star{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Target.URI, RepositoryCid: value.Target.CID,
		CreatedAt: value.CreatedAt, IndexedAt: value.IndexedAt,
	}
}

func starEnvelopeResponse(value star.Star) generated.StarEnvelope {
	return generated.StarEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Target.URI, RepositoryCid: value.Target.CID, CreatedAt: value.CreatedAt,
	}
}

type issueListJSON struct {
	IssueCount     int64                `json:"issue_count"`
	OpenIssueCount int64                `json:"open_issue_count"`
	Items          []projectedIssueJSON `json:"items"`
	Page           generated.Page       `json:"page"`
}

type projectedIssueJSON struct {
	URI           string               `json:"uri"`
	CID           string               `json:"cid"`
	AuthorDID     string               `json:"author_did"`
	RepositoryURI string               `json:"repository_uri"`
	RepositoryCID string               `json:"repository_cid"`
	Title         string               `json:"title"`
	Body          string               `json:"body"`
	State         generated.IssueState `json:"state"`
	StatusURI     *string              `json:"status_uri"`
	StatusCID     *string              `json:"status_cid"`
	CommentCount  int64                `json:"comment_count"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
	IndexedAt     time.Time            `json:"indexed_at"`
}

func projectedIssueResponse(value issue.ProjectedIssue) projectedIssueJSON {
	return projectedIssueJSON{
		URI: value.URI, CID: value.CID, AuthorDID: value.AuthorDID,
		RepositoryURI: value.Repository.URI, RepositoryCID: value.Repository.CID,
		Title: value.Title, Body: value.Body, State: generated.IssueState(value.State),
		StatusURI: pointerUnlessEmpty(value.Status.URI), StatusCID: pointerUnlessEmpty(value.Status.CID),
		CommentCount: value.CommentCount, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt,
	}
}

func issueEnvelopeResponse(value issue.Issue) generated.IssueEnvelope {
	return generated.IssueEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
		Title: value.Title, Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func issueStatusEnvelopeResponse(value issue.Status) generated.IssueStatusEnvelope {
	return generated.IssueStatusEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID,
		RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
		State: generated.IssueStatusEnvelopeState(value.State), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func projectedPullRequestResponse(value pullrequest.ProjectedPullRequest) generated.PullRequest {
	return generated.PullRequest{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		SourceRepositoryUri: value.SourceRepository.URI, SourceRepositoryCid: value.SourceRepository.CID, SourceBranch: value.SourceBranch,
		TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID, TargetBranch: value.TargetBranch,
		HeadSha: value.HeadSHA, Title: value.Title, Body: value.Body, State: generated.PullRequestState(value.State),
		StatusUri: pointerUnlessEmpty(value.Status.URI), StatusCid: pointerUnlessEmpty(value.Status.CID), MergedCommitSha: pointerUnlessEmpty(value.MergedCommitSHA),
		ReviewCount: value.ReviewCount, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt}
}

func pullRequestEnvelopeResponse(value pullrequest.PullRequest) generated.PullRequestEnvelope {
	return generated.PullRequestEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		SourceRepositoryUri: value.SourceRepository.URI, SourceRepositoryCid: value.SourceRepository.CID, SourceBranch: value.SourceBranch,
		TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID, TargetBranch: value.TargetBranch,
		HeadSha: value.HeadSHA, Title: value.Title, Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func projectedPullRequestReviewResponse(value pullrequest.ProjectedReview) generated.PullRequestReview {
	return generated.PullRequestReview{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, Verdict: generated.PullRequestReviewVerdict(value.Verdict), Body: value.Body,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt}
}

func pullRequestReviewEnvelopeResponse(value pullrequest.Review) generated.PullRequestReviewEnvelope {
	return generated.PullRequestReviewEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, Verdict: generated.PullRequestReviewEnvelopeVerdict(value.Verdict), Body: value.Body,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func pullRequestStatusEnvelopeResponse(value pullrequest.Status) generated.PullRequestStatusEnvelope {
	return generated.PullRequestStatusEnvelope{Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID, PullRequestUri: value.Subject.URI,
		PullRequestCid: value.Subject.CID, TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID,
		State: generated.PullRequestStatusEnvelopeState(value.State), MergeCommitSha: pointerUnlessEmpty(value.MergeCommitSHA), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func projectedCommentResponse(value comment.ProjectedComment) generated.Comment {
	var parentURI, parentCID *string
	if value.Parent != nil {
		parentURI, parentCID = &value.Parent.URI, &value.Parent.CID
	}
	return generated.Comment{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID, ParentUri: parentURI, ParentCid: parentCID,
		Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, IndexedAt: value.IndexedAt,
	}
}

func commentEnvelopeResponse(value issue.Comment) generated.CommentEnvelope {
	var parentURI, parentCID *string
	if value.Parent != nil {
		parentURI, parentCID = &value.Parent.URI, &value.Parent.CID
	}
	return generated.CommentEnvelope{
		Uri: value.URI, Cid: value.CID, AuthorDid: value.AuthorDID,
		IssueUri: value.Subject.URI, IssueCid: value.Subject.CID, ParentUri: parentURI, ParentCid: parentCID,
		Body: value.Body, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func pointerUnlessEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
