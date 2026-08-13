package federation

import (
	"context"
	"fmt"
	"sort"
	"time"

	dbgen "github.com/adenosine-dev/adenosine/internal/database/generated"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type transactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type postgresStore struct {
	db       transactionBeginner
	consumer string
	now      func() time.Time
}

func (store *postgresStore) Store(ctx context.Context, event Event, rejection string) (duplicate bool, err error) {
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := dbgen.New(tx)

	duplicate, err = queries.HasFederationReceipt(ctx, dbgen.HasFederationReceiptParams{Consumer: store.consumer, EventID: event.ID})
	if err != nil {
		return false, fmt.Errorf("check receipt: %w", err)
	}
	if duplicate {
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("commit duplicate receipt: %w", err)
		}
		return true, nil
	}

	now := store.now().UTC()
	if rejection == "" {
		if err := project(ctx, queries, event, now); err != nil {
			return false, err
		}
	}
	if err := queries.AdvanceFederationCursor(ctx, dbgen.AdvanceFederationCursorParams{
		Consumer: store.consumer, EventID: event.ID, UpdatedAt: pgTime(now),
	}); err != nil {
		return false, fmt.Errorf("advance cursor: %w", err)
	}
	outcome := "applied"
	if rejection != "" {
		outcome = "rejected"
	}
	if err := queries.InsertFederationReceipt(ctx, dbgen.InsertFederationReceiptParams{
		Consumer: store.consumer, EventID: event.ID, Outcome: outcome,
		Rejection: pgText(rejection), ReceivedAt: pgTime(now),
	}); err != nil {
		return false, fmt.Errorf("insert receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit transaction: %w", err)
	}
	return false, nil
}

func project(ctx context.Context, queries *dbgen.Queries, event Event, indexedAt time.Time) error {
	if event.Identity != nil {
		identity := event.Identity
		if err := queries.UpsertFederationIdentity(ctx, dbgen.UpsertFederationIdentityParams{
			Did: identity.DID, Handle: pgText(identity.Handle), Status: identity.Status,
			IsActive: identity.IsActive, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		}); err != nil {
			return fmt.Errorf("project identity: %w", err)
		}
		if err := queries.ProjectIdentityHandle(ctx, dbgen.ProjectIdentityHandleParams{
			Did: identity.DID, Handle: pgText(identity.Handle), IndexedAt: pgTime(indexedAt), HandleSourceEventID: pgInt8(event.ID),
		}); err != nil {
			return fmt.Errorf("project identity handle: %w", err)
		}
		return nil
	}

	record := event.Record
	if record == nil {
		return fmt.Errorf("project event: missing validated payload")
	}
	if err := lockIssueProjection(ctx, queries, record); err != nil {
		return err
	}
	if err := lockPullRequestProjection(ctx, queries, record); err != nil {
		return err
	}
	lockURI := ""
	if record.Collection == RepositoryCollection {
		lockURI = record.URI
	} else if record.Star != nil {
		lockURI = record.Star.RepositoryURI
	} else if record.Collection == StarCollection {
		repositoryURI, lookupErr := queries.GetFederationStarRepositoryURI(ctx, record.URI)
		if lookupErr != nil && lookupErr != pgx.ErrNoRows {
			return fmt.Errorf("resolve star repository for lock: %w", lookupErr)
		}
		if lookupErr == nil {
			lockURI = repositoryURI
		}
	}
	if lockURI != "" {
		if err := queries.LockFederationRepositoryStars(ctx, lockURI); err != nil {
			return fmt.Errorf("lock repository star projection: %w", err)
		}
	}
	if record.Action == "delete" {
		if err := queries.TombstoneFederationRecord(ctx, dbgen.TombstoneFederationRecordParams{
			Uri: record.URI, AuthorDid: record.DID, Collection: record.Collection, Rkey: record.RKey,
			IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		}); err != nil {
			return fmt.Errorf("tombstone raw record: %w", err)
		}
		if record.Collection == ProfileCollection {
			if err := queries.TombstoneFederationProfile(ctx, dbgen.TombstoneFederationProfileParams{
				Did: record.DID, ProfileUri: pgText(record.URI), IndexedAt: pgTime(indexedAt), SourceEventID: pgInt8(event.ID),
			}); err != nil {
				return fmt.Errorf("tombstone profile: %w", err)
			}
			return nil
		}
		if record.Collection == OrganizationCollection {
			return queries.TombstoneFederationOrganizationProjection(ctx, dbgen.TombstoneFederationOrganizationProjectionParams{Uri: record.URI, DeletedAt: pgTime(indexedAt), SourceEventID: event.ID})
		}
		if record.Collection == OrganizationGrantCollection {
			return queries.TombstoneFederationOrganizationGrant(ctx, dbgen.TombstoneFederationOrganizationGrantParams{Uri: record.URI, DeletedAt: pgTime(indexedAt), SourceEventID: event.ID})
		}
		if record.Collection == OrganizationMembershipCollection {
			return queries.TombstoneFederationOrganizationMembership(ctx, dbgen.TombstoneFederationOrganizationMembershipParams{Uri: record.URI, DeletedAt: pgTime(indexedAt), SourceEventID: event.ID})
		}
		if record.Collection == OrganizationRevocationCollection {
			return queries.TombstoneFederationOrganizationRevocation(ctx, dbgen.TombstoneFederationOrganizationRevocationParams{Uri: record.URI, DeletedAt: pgTime(indexedAt), SourceEventID: event.ID})
		}
		if record.Collection == StarCollection {
			repositoryURI, err := queries.TombstoneFederationStar(ctx, dbgen.TombstoneFederationStarParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone star: %w", err)
			}
			if err := queries.RecomputeFederationStarCount(ctx, repositoryURI); err != nil {
				return fmt.Errorf("recompute star count: %w", err)
			}
			return nil
		}
		if record.Collection == IssueCollection {
			repositoryURI, err := queries.TombstoneFederationIssue(ctx, dbgen.TombstoneFederationIssueParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone issue: %w", err)
			}
			if err := queries.RecomputeFederationIssueCounts(ctx, repositoryURI); err != nil {
				return fmt.Errorf("recompute issue counts: %w", err)
			}
			if err := queries.RecomputeFederationRepositoryCommentCount(ctx, repositoryURI); err != nil {
				return fmt.Errorf("recompute repository comment count: %w", err)
			}
			return nil
		}
		if record.Collection == issue.CommentCollection {
			childIssueURIs, childErr := queries.ListFederationCommentChildIssueURIs(ctx, pgText(record.URI))
			if childErr != nil {
				return fmt.Errorf("list comment children before tombstone: %w", childErr)
			}
			issueURI, err := queries.TombstoneFederationIssueComment(ctx, dbgen.TombstoneFederationIssueCommentParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone issue comment: %w", err)
			}
			if err := recomputeCommentCounts(ctx, queries, issueURI); err != nil {
				return err
			}
			return recomputeChildCommentCounts(ctx, queries, childIssueURIs)
		}
		if record.Collection == IssueStatusCollection {
			target, err := queries.TombstoneFederationIssueStatus(ctx, dbgen.TombstoneFederationIssueStatusParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone issue status: %w", err)
			}
			return recomputeIssueStateAndCounts(ctx, queries, target.IssueUri)
		}
		if record.Collection == PullRequestCollection {
			targetRepositoryURI, err := queries.TombstoneFederationPullRequest(ctx, dbgen.TombstoneFederationPullRequestParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone pull request: %w", err)
			}
			if err := queries.RecomputeFederationPullRequestCounts(ctx, targetRepositoryURI); err != nil {
				return fmt.Errorf("recompute pull request counts: %w", err)
			}
			return nil
		}
		if record.Collection == PullRequestStatusCollection {
			target, err := queries.TombstoneFederationPullRequestStatus(ctx, dbgen.TombstoneFederationPullRequestStatusParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone pull request status: %w", err)
			}
			return recomputePullRequestStateAndCounts(ctx, queries, target.PullRequestUri)
		}
		if record.Collection == PullRequestReviewCollection {
			pullRequestURI, err := queries.TombstoneFederationPullRequestReview(ctx, dbgen.TombstoneFederationPullRequestReviewParams{
				Uri: record.URI, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
			})
			if err == pgx.ErrNoRows {
				return nil
			}
			if err != nil {
				return fmt.Errorf("tombstone pull request review: %w", err)
			}
			return recomputePullRequestReviewCount(ctx, queries, pullRequestURI)
		}
		if err := queries.TombstoneFederationRepository(ctx, dbgen.TombstoneFederationRepositoryParams{
			Uri: record.URI, OwnerDid: record.DID, Rkey: record.RKey, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		}); err != nil {
			return fmt.Errorf("tombstone repository: %w", err)
		}
		if err := queries.RecomputeFederationRepositoryCount(ctx, record.DID); err != nil {
			return fmt.Errorf("recompute repository count: %w", err)
		}
		if err := queries.RecomputeFederationStarCount(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute star count: %w", err)
		}
		if err := queries.RecomputeFederationIssueCounts(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute issue counts: %w", err)
		}
		if err := queries.RecomputeFederationRepositoryCommentCount(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute repository comment count: %w", err)
		}
		if err := queries.RecomputeFederationPullRequestCounts(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute pull request counts: %w", err)
		}
		return nil
	}

	createdAt := recordTime(record)
	if err := queries.UpsertFederationRecord(ctx, dbgen.UpsertFederationRecordParams{
		Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Collection: record.Collection,
		Rkey: record.RKey, Record: record.Raw, RecordCreatedAt: pgTime(createdAt),
		IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
	}); err != nil {
		return fmt.Errorf("upsert raw record: %w", err)
	}
	if record.Profile != nil {
		value := record.Profile
		if err := queries.UpsertFederationProfile(ctx, dbgen.UpsertFederationProfileParams{
			Did: record.DID, ProfileUri: pgText(record.URI), ProfileCid: pgText(record.CID),
			DisplayName: pgText(value.DisplayName), Bio: pgText(value.Bio), Website: pgText(value.Website),
			Location: pgText(value.Location), RecordCreatedAt: pgTime(value.CreatedAt),
			IndexedAt: pgTime(indexedAt), SourceEventID: pgInt8(event.ID),
		}); err != nil {
			return fmt.Errorf("upsert profile: %w", err)
		}
		if err := queries.RecomputeFederationRepositoryCount(ctx, record.DID); err != nil {
			return fmt.Errorf("recompute repository count: %w", err)
		}
		return nil
	}
	if record.Organization != nil {
		value := record.Organization
		if err := queries.UpsertFederationOrganization(ctx, dbgen.UpsertFederationOrganizationParams{Uri: record.URI, Cid: pgText(record.CID), CreatorDid: record.DID, Rkey: record.RKey, Slug: pgText(value.Slug), Name: pgText(value.Name), Description: pgText(value.Description), Website: pgText(value.Website), Location: pgText(value.Location), RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID}); err != nil {
			return fmt.Errorf("upsert organization: %w", err)
		}
		return nil
	}
	if record.OrganizationGrant != nil {
		value := record.OrganizationGrant
		var expiresAt pgtype.Timestamptz
		if value.ExpiresAt != nil {
			expiresAt = pgTime(*value.ExpiresAt)
		}
		if err := queries.UpsertFederationOrganizationGrant(ctx, dbgen.UpsertFederationOrganizationGrantParams{Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey, OrganizationUri: value.Organization.URI, OrganizationCid: value.Organization.CID, SubjectDid: pgText(value.Subject), Role: pgText(value.Role), AuthorityUri: pgText(value.Authority.URI), AuthorityCid: pgText(value.Authority.CID), RecordCreatedAt: pgTime(value.CreatedAt), ExpiresAt: expiresAt, IndexedAt: pgTime(indexedAt), SourceEventID: event.ID}); err != nil {
			return fmt.Errorf("upsert organization grant: %w", err)
		}
		return nil
	}
	if record.OrganizationMembership != nil {
		value := record.OrganizationMembership
		if err := queries.UpsertFederationOrganizationMembership(ctx, dbgen.UpsertFederationOrganizationMembershipParams{Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey, OrganizationUri: value.Organization.URI, OrganizationCid: value.Organization.CID, GrantUri: value.Grant.URI, GrantCid: value.Grant.CID, Visibility: pgText(value.Visibility), RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID}); err != nil {
			return fmt.Errorf("upsert organization membership: %w", err)
		}
		return nil
	}
	if record.OrganizationRevocation != nil {
		value := record.OrganizationRevocation
		if err := queries.UpsertFederationOrganizationRevocation(ctx, dbgen.UpsertFederationOrganizationRevocationParams{Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey, OrganizationUri: value.Organization.URI, OrganizationCid: value.Organization.CID, GrantUri: value.Grant.URI, GrantCid: value.Grant.CID, SubjectDid: pgText(value.Subject), AuthorityUri: pgText(value.Authority.URI), AuthorityCid: pgText(value.Authority.CID), RecordCreatedAt: pgTime(value.CreatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID}); err != nil {
			return fmt.Errorf("upsert organization revocation: %w", err)
		}
		return nil
	}
	if record.Star != nil {
		value := record.Star
		repositoryURI, err := queries.UpsertFederationStar(ctx, dbgen.UpsertFederationStarParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			RepositoryUri: value.RepositoryURI, RepositoryCid: value.RepositoryCID,
			RecordCreatedAt: pgTime(value.CreatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert star: %w", err)
		}
		if err := queries.RecomputeFederationStarCount(ctx, repositoryURI); err != nil {
			return fmt.Errorf("recompute star count: %w", err)
		}
		return nil
	}
	if record.Issue != nil {
		value := record.Issue
		previousRepositoryURI, previousErr := queries.GetFederationIssueRepositoryURI(ctx, record.URI)
		if previousErr != nil && previousErr != pgx.ErrNoRows {
			return fmt.Errorf("resolve previous issue repository: %w", previousErr)
		}
		repositoryURI, err := queries.UpsertFederationIssue(ctx, dbgen.UpsertFederationIssueParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
			Title: value.Title, Body: value.Body, RecordCreatedAt: pgTime(value.CreatedAt),
			RecordUpdatedAt: pgTime(value.UpdatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert issue: %w", err)
		}
		if err := queries.RecomputeFederationIssueState(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute issue state: %w", err)
		}
		if err := queries.RecomputeFederationIssueCommentCount(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute issue comment count: %w", err)
		}
		if err := queries.RecomputeFederationIssueCounts(ctx, repositoryURI); err != nil {
			return fmt.Errorf("recompute issue counts: %w", err)
		}
		if previousErr == nil && previousRepositoryURI != repositoryURI {
			if err := queries.RecomputeFederationIssueCounts(ctx, previousRepositoryURI); err != nil {
				return fmt.Errorf("recompute previous repository issue counts: %w", err)
			}
			if err := queries.RecomputeFederationRepositoryCommentCount(ctx, previousRepositoryURI); err != nil {
				return fmt.Errorf("recompute previous repository comment count: %w", err)
			}
		}
		if err := queries.RecomputeFederationRepositoryCommentCount(ctx, repositoryURI); err != nil {
			return fmt.Errorf("recompute repository comment count: %w", err)
		}
		return nil
	}
	if record.IssueComment != nil {
		value := record.IssueComment
		childIssueURIs, childErr := queries.ListFederationCommentChildIssueURIs(ctx, pgText(record.URI))
		if childErr != nil {
			return fmt.Errorf("list comment children before upsert: %w", childErr)
		}
		parentURI, parentCID := "", ""
		if value.Parent != nil {
			parentURI, parentCID = value.Parent.URI, value.Parent.CID
		}
		previousIssueURI, previousErr := queries.GetFederationIssueCommentIssueURI(ctx, record.URI)
		if previousErr != nil && previousErr != pgx.ErrNoRows {
			return fmt.Errorf("resolve previous comment issue: %w", previousErr)
		}
		issueURI, err := queries.UpsertFederationIssueComment(ctx, dbgen.UpsertFederationIssueCommentParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			IssueUri: value.Subject.URI, IssueCid: value.Subject.CID, ParentUri: pgText(parentURI), ParentCid: pgText(parentCID),
			Body: value.Body, RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt),
			IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert issue comment: %w", err)
		}
		if previousErr == nil && previousIssueURI != issueURI {
			if err := recomputeCommentCounts(ctx, queries, previousIssueURI); err != nil {
				return err
			}
		}
		if err := recomputeCommentCounts(ctx, queries, issueURI); err != nil {
			return err
		}
		return recomputeChildCommentCounts(ctx, queries, childIssueURIs)
	}
	if record.IssueStatus != nil {
		value := record.IssueStatus
		target, err := queries.UpsertFederationIssueStatus(ctx, dbgen.UpsertFederationIssueStatusParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			IssueUri: value.Subject.URI, IssueCid: value.Subject.CID,
			RepositoryUri: value.Repository.URI, RepositoryCid: value.Repository.CID,
			State: string(value.State), RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt),
			IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert issue status: %w", err)
		}
		return recomputeIssueStateAndCounts(ctx, queries, target.IssueUri)
	}
	if record.PullRequest != nil {
		value := record.PullRequest
		targetRepositoryURI, err := queries.UpsertFederationPullRequest(ctx, dbgen.UpsertFederationPullRequestParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			SourceRepositoryUri: value.SourceRepository.URI, SourceRepositoryCid: value.SourceRepository.CID,
			SourceBranch: value.SourceBranch, TargetRepositoryUri: value.TargetRepository.URI,
			TargetRepositoryCid: value.TargetRepository.CID, TargetBranch: value.TargetBranch,
			HeadSha: value.HeadSHA, Title: value.Title, Body: value.Body,
			RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt),
			IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert pull request: %w", err)
		}
		if err := queries.RecomputeFederationPullRequestState(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute pull request state: %w", err)
		}
		if err := queries.RecomputeFederationPullRequestReviewCount(ctx, record.URI); err != nil {
			return fmt.Errorf("recompute pull request review count: %w", err)
		}
		if err := queries.RecomputeFederationPullRequestCounts(ctx, targetRepositoryURI); err != nil {
			return fmt.Errorf("recompute pull request counts: %w", err)
		}
		return nil
	}
	if record.PullRequestStatus != nil {
		value := record.PullRequestStatus
		target, err := queries.UpsertFederationPullRequestStatus(ctx, dbgen.UpsertFederationPullRequestStatusParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			PullRequestUri: value.Subject.URI, PullRequestCid: value.Subject.CID,
			TargetRepositoryUri: value.TargetRepository.URI, TargetRepositoryCid: value.TargetRepository.CID,
			State: string(value.State), MergedCommitSha: pgText(value.MergeCommitSHA),
			RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt),
			IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert pull request status: %w", err)
		}
		return recomputePullRequestStateAndCounts(ctx, queries, target.PullRequestUri)
	}
	if record.PullRequestReview != nil {
		value := record.PullRequestReview
		pullRequestURI, err := queries.UpsertFederationPullRequestReview(ctx, dbgen.UpsertFederationPullRequestReviewParams{
			Uri: record.URI, Cid: pgText(record.CID), AuthorDid: record.DID, Rkey: record.RKey,
			PullRequestUri: value.Subject.URI, PullRequestCid: value.Subject.CID,
			Body: value.Body, Verdict: string(value.Verdict), RecordCreatedAt: pgTime(value.CreatedAt),
			RecordUpdatedAt: pgTime(value.UpdatedAt), IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
		})
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("upsert pull request review: %w", err)
		}
		return recomputePullRequestReviewCount(ctx, queries, pullRequestURI)
	}
	value := record.Repository
	if value == nil {
		return fmt.Errorf("project repository: missing decoded value")
	}
	organizationURI, organizationCID := "", ""
	if value.Organization != nil {
		organizationURI, organizationCID = value.Organization.URI, value.Organization.CID
	}
	if err := queries.UpsertFederationRepository(ctx, dbgen.UpsertFederationRepositoryParams{
		Uri: record.URI, Cid: pgText(record.CID), OwnerDid: record.DID, Rkey: record.RKey,
		Slug: pgText(value.Slug), Name: pgText(value.Name), Description: pgText(value.Description),
		DefaultBranch: pgText(value.DefaultBranch), GitHttps: pgText(value.GitHTTPS), GitSsh: pgText(value.GitSSH), Web: pgText(value.Web),
		OrganizationUri: pgText(organizationURI), OrganizationCid: pgText(organizationCID),
		RecordCreatedAt: pgTime(value.CreatedAt), RecordUpdatedAt: pgTime(value.UpdatedAt),
		IndexedAt: pgTime(indexedAt), SourceEventID: event.ID,
	}); err != nil {
		return fmt.Errorf("upsert repository: %w", err)
	}
	if err := queries.RecomputeFederationRepositoryCount(ctx, record.DID); err != nil {
		return fmt.Errorf("recompute repository count: %w", err)
	}
	if err := queries.RecomputeFederationStarCount(ctx, record.URI); err != nil {
		return fmt.Errorf("recompute star count: %w", err)
	}
	if err := queries.RecomputeFederationIssueCounts(ctx, record.URI); err != nil {
		return fmt.Errorf("recompute issue counts: %w", err)
	}
	if err := queries.RecomputeFederationRepositoryCommentCount(ctx, record.URI); err != nil {
		return fmt.Errorf("recompute repository comment count: %w", err)
	}
	pullRequestURIs, err := queries.ListFederationRepositoryPullRequestURIs(ctx, record.URI)
	if err != nil {
		return fmt.Errorf("list repository pull requests: %w", err)
	}
	for _, pullRequestURI := range pullRequestURIs {
		if err := queries.RecomputeFederationPullRequestState(ctx, pullRequestURI); err != nil {
			return fmt.Errorf("recompute pull request state: %w", err)
		}
		if err := queries.RecomputeFederationPullRequestReviewCount(ctx, pullRequestURI); err != nil {
			return fmt.Errorf("recompute pull request review count: %w", err)
		}
	}
	if err := queries.RecomputeFederationPullRequestCounts(ctx, record.URI); err != nil {
		return fmt.Errorf("recompute pull request counts: %w", err)
	}
	return nil
}

func lockPullRequestProjection(ctx context.Context, queries *dbgen.Queries, record *RecordEvent) error {
	if record.Collection != RepositoryCollection && record.Collection != PullRequestCollection && record.Collection != PullRequestStatusCollection && record.Collection != PullRequestReviewCollection {
		return nil
	}
	pullRequests := make(map[string]struct{})
	repositories := make(map[string]struct{})
	if record.Collection == RepositoryCollection {
		repositories[record.URI] = struct{}{}
		uris, err := queries.ListFederationRepositoryPullRequestURIs(ctx, record.URI)
		if err != nil {
			return fmt.Errorf("resolve repository pull requests for lock: %w", err)
		}
		for _, uri := range uris {
			pullRequests[uri] = struct{}{}
		}
	}
	if record.PullRequest != nil {
		pullRequests[record.URI] = struct{}{}
		repositories[record.PullRequest.TargetRepository.URI] = struct{}{}
	}
	if record.PullRequestStatus != nil {
		pullRequests[record.PullRequestStatus.Subject.URI] = struct{}{}
		repositories[record.PullRequestStatus.TargetRepository.URI] = struct{}{}
	}
	if record.PullRequestReview != nil {
		pullRequests[record.PullRequestReview.Subject.URI] = struct{}{}
	}
	if record.Collection == PullRequestCollection {
		targetURI, err := queries.GetFederationPullRequestTargetRepositoryURI(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve pull request target for lock: %w", err)
		}
		if err == nil {
			pullRequests[record.URI] = struct{}{}
			repositories[targetURI] = struct{}{}
		}
	}
	if record.Collection == PullRequestStatusCollection {
		target, err := queries.GetFederationPullRequestStatusTarget(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve pull request status target for lock: %w", err)
		}
		if err == nil {
			pullRequests[target.PullRequestUri] = struct{}{}
			repositories[target.TargetRepositoryUri] = struct{}{}
		}
	}
	if record.Collection == PullRequestReviewCollection {
		uri, err := queries.GetFederationPullRequestReviewSubject(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve pull request review subject for lock: %w", err)
		}
		if err == nil {
			pullRequests[uri] = struct{}{}
		}
	}
	orderedPullRequests := sortedKeys(pullRequests)
	for _, uri := range orderedPullRequests {
		if err := queries.LockFederationPullRequest(ctx, uri); err != nil {
			return fmt.Errorf("lock pull request projection: %w", err)
		}
		targetURI, err := queries.GetFederationPullRequestTargetRepositoryURI(ctx, uri)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve locked pull request target: %w", err)
		}
		if err == nil {
			repositories[targetURI] = struct{}{}
		}
	}
	for _, uri := range sortedKeys(repositories) {
		if err := queries.LockFederationRepositoryPullRequests(ctx, uri); err != nil {
			return fmt.Errorf("lock repository pull request projection: %w", err)
		}
	}
	return nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lockIssueProjection(ctx context.Context, queries *dbgen.Queries, record *RecordEvent) error {
	if record.Collection != RepositoryCollection && record.Collection != IssueCollection && record.Collection != issue.CommentCollection && record.Collection != IssueStatusCollection {
		return nil
	}
	repositories := make(map[string]struct{})
	issues := make(map[string]struct{})
	if record.Collection == RepositoryCollection {
		repositories[record.URI] = struct{}{}
	}
	if record.Issue != nil {
		repositories[record.Issue.Repository.URI] = struct{}{}
		issues[record.URI] = struct{}{}
	}
	if record.Collection == IssueCollection {
		repositoryURI, err := queries.GetFederationIssueRepositoryURI(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve issue repository for lock: %w", err)
		}
		if err == nil {
			repositories[repositoryURI] = struct{}{}
		}
	}
	if record.IssueStatus != nil {
		issues[record.IssueStatus.Subject.URI] = struct{}{}
		repositories[record.IssueStatus.Repository.URI] = struct{}{}
		repositoryURI, err := queries.GetFederationIssueRepositoryURI(ctx, record.IssueStatus.Subject.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve status issue repository for lock: %w", err)
		}
		if err == nil {
			repositories[repositoryURI] = struct{}{}
		}
	}
	if record.IssueComment != nil {
		issues[record.IssueComment.Subject.URI] = struct{}{}
	}
	if record.Collection == issue.CommentCollection {
		issueURI, err := queries.GetFederationIssueCommentIssueURI(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve comment issue for lock: %w", err)
		}
		if err == nil {
			issues[issueURI] = struct{}{}
		}
		childIssueURIs, childErr := queries.ListFederationCommentChildIssueURIs(ctx, pgText(record.URI))
		if childErr != nil {
			return fmt.Errorf("resolve comment child issues for lock: %w", childErr)
		}
		for _, childIssueURI := range childIssueURIs {
			issues[childIssueURI] = struct{}{}
		}
	}
	if record.Collection == IssueStatusCollection {
		target, err := queries.GetFederationIssueStatusTarget(ctx, record.URI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve issue status target for lock: %w", err)
		}
		if err == nil {
			repositories[target.RepositoryUri] = struct{}{}
			repositoryURI, issueErr := queries.GetFederationIssueRepositoryURI(ctx, target.IssueUri)
			if issueErr != nil && issueErr != pgx.ErrNoRows {
				return fmt.Errorf("resolve projected status issue repository for lock: %w", issueErr)
			}
			if issueErr == nil {
				repositories[repositoryURI] = struct{}{}
			}
		}
	}
	orderedIssues := make([]string, 0, len(issues))
	for issueURI := range issues {
		orderedIssues = append(orderedIssues, issueURI)
	}
	sort.Strings(orderedIssues)
	for _, issueURI := range orderedIssues {
		if err := queries.LockFederationIssueComments(ctx, issueURI); err != nil {
			return fmt.Errorf("lock issue comment projection: %w", err)
		}
		repositoryURI, err := queries.GetFederationIssueRepositoryForComment(ctx, issueURI)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("resolve comment repository for lock: %w", err)
		}
		if err == nil {
			repositories[repositoryURI] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(repositories))
	for repositoryURI := range repositories {
		ordered = append(ordered, repositoryURI)
	}
	sort.Strings(ordered)
	for _, repositoryURI := range ordered {
		if err := queries.LockFederationRepositoryIssues(ctx, repositoryURI); err != nil {
			return fmt.Errorf("lock repository issue projection: %w", err)
		}
	}
	return nil
}

func recomputeCommentCounts(ctx context.Context, queries *dbgen.Queries, issueURI string) error {
	if err := queries.RecomputeFederationIssueCommentCount(ctx, issueURI); err != nil {
		return fmt.Errorf("recompute issue comment count: %w", err)
	}
	repositoryURI, err := queries.GetFederationIssueRepositoryForComment(ctx, issueURI)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve comment repository for count: %w", err)
	}
	if err := queries.RecomputeFederationRepositoryCommentCount(ctx, repositoryURI); err != nil {
		return fmt.Errorf("recompute repository comment count: %w", err)
	}
	return nil
}

func recomputeChildCommentCounts(ctx context.Context, queries *dbgen.Queries, issueURIs []string) error {
	for _, issueURI := range issueURIs {
		if err := recomputeCommentCounts(ctx, queries, issueURI); err != nil {
			return err
		}
	}
	return nil
}

func recomputeIssueStateAndCounts(ctx context.Context, queries *dbgen.Queries, issueURI string) error {
	if err := queries.RecomputeFederationIssueState(ctx, issueURI); err != nil {
		return fmt.Errorf("recompute issue state: %w", err)
	}
	repositoryURI, err := queries.GetFederationIssueRepositoryURI(ctx, issueURI)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve issue repository for counts: %w", err)
	}
	if err := queries.RecomputeFederationIssueCounts(ctx, repositoryURI); err != nil {
		return fmt.Errorf("recompute issue counts: %w", err)
	}
	return nil
}

func recomputePullRequestStateAndCounts(ctx context.Context, queries *dbgen.Queries, pullRequestURI string) error {
	if err := queries.RecomputeFederationPullRequestState(ctx, pullRequestURI); err != nil {
		return fmt.Errorf("recompute pull request state: %w", err)
	}
	targetRepositoryURI, err := queries.GetFederationPullRequestTargetRepositoryURI(ctx, pullRequestURI)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve pull request target for counts: %w", err)
	}
	if err := queries.RecomputeFederationPullRequestCounts(ctx, targetRepositoryURI); err != nil {
		return fmt.Errorf("recompute pull request counts: %w", err)
	}
	return nil
}

func recomputePullRequestReviewCount(ctx context.Context, queries *dbgen.Queries, pullRequestURI string) error {
	if err := queries.RecomputeFederationPullRequestReviewCount(ctx, pullRequestURI); err != nil {
		return fmt.Errorf("recompute pull request review count: %w", err)
	}
	return nil
}

func recordTime(record *RecordEvent) time.Time {
	if record.Profile != nil {
		return record.Profile.CreatedAt
	}
	if record.Repository != nil {
		return record.Repository.CreatedAt
	}
	if record.Star != nil {
		return record.Star.CreatedAt
	}
	if record.Issue != nil {
		return record.Issue.CreatedAt
	}
	if record.IssueComment != nil {
		return record.IssueComment.CreatedAt
	}
	if record.IssueStatus != nil {
		return record.IssueStatus.CreatedAt
	}
	if record.PullRequest != nil {
		return record.PullRequest.CreatedAt
	}
	if record.PullRequestStatus != nil {
		return record.PullRequestStatus.CreatedAt
	}
	if record.PullRequestReview != nil {
		return record.PullRequestReview.CreatedAt
	}
	if record.Organization != nil {
		return record.Organization.CreatedAt
	}
	if record.OrganizationGrant != nil {
		return record.OrganizationGrant.CreatedAt
	}
	if record.OrganizationMembership != nil {
		return record.OrganizationMembership.CreatedAt
	}
	if record.OrganizationRevocation != nil {
		return record.OrganizationRevocation.CreatedAt
	}
	return time.Time{}
}

func pgText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: !value.IsZero()}
}

func pgInt8(value int64) pgtype.Int8 { return pgtype.Int8{Int64: value, Valid: true} }
