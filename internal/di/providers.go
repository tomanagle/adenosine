// Package di is the explicit application composition root.
package di

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/adenosine-dev/adenosine/internal/app"
	"github.com/adenosine-dev/adenosine/internal/atproto"
	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/branchprotection"
	"github.com/adenosine-dev/adenosine/internal/comment"
	"github.com/adenosine-dev/adenosine/internal/config"
	"github.com/adenosine-dev/adenosine/internal/database"
	"github.com/adenosine-dev/adenosine/internal/event"
	"github.com/adenosine-dev/adenosine/internal/federation"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/githttp"
	"github.com/adenosine-dev/adenosine/internal/gitssh"
	"github.com/adenosine-dev/adenosine/internal/identity"
	"github.com/adenosine-dev/adenosine/internal/issue"
	"github.com/adenosine-dev/adenosine/internal/moderation"
	"github.com/adenosine-dev/adenosine/internal/notification"
	"github.com/adenosine-dev/adenosine/internal/observability"
	"github.com/adenosine-dev/adenosine/internal/organization"
	"github.com/adenosine-dev/adenosine/internal/owner"
	"github.com/adenosine-dev/adenosine/internal/passkey"
	"github.com/adenosine-dev/adenosine/internal/profile"
	"github.com/adenosine-dev/adenosine/internal/pullrequest"
	"github.com/adenosine-dev/adenosine/internal/release"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/adenosine-dev/adenosine/internal/restapi"
	"github.com/adenosine-dev/adenosine/internal/search"
	"github.com/adenosine-dev/adenosine/internal/star"
	"github.com/adenosine-dev/adenosine/internal/storage"
	"github.com/adenosine-dev/adenosine/internal/syncproxy"
	"github.com/adenosine-dev/adenosine/internal/webhook"
)

// Must constructs the application or panics during startup.
func Must(ctx context.Context, cfg config.Config) *app.Application {
	application, err := build(ctx, cfg)
	if err != nil {
		panic(err)
	}
	return application
}

func build(ctx context.Context, cfg config.Config) (*app.Application, error) {
	logger, shutdownTelemetry := observability.Must(ctx)

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		_ = shutdownTelemetry(ctx)
		return nil, fmt.Errorf("open database: %w", err)
	}

	repositoryStorage, err := storage.NewFilesystem(cfg.RepositoryRoot)
	if err != nil {
		db.Close()
		_ = shutdownTelemetry(ctx)
		return nil, fmt.Errorf("open repository storage: %w", err)
	}
	releaseAssetStorage := release.MustBlobStore(ctx, release.BlobStoreConfig{
		Backend:        cfg.ReleaseAssetBackend,
		FilesystemRoot: cfg.ReleaseAssetRoot,
		S3: release.S3Config{
			Endpoint: cfg.ReleaseAssetS3Endpoint, Region: cfg.ReleaseAssetS3Region, Bucket: cfg.ReleaseAssetS3Bucket,
			AccessKeyID: cfg.ReleaseAssetS3AccessKeyID, SecretAccessKey: cfg.ReleaseAssetS3SecretKey,
			SessionToken: cfg.ReleaseAssetS3SessionToken, PathStyle: cfg.ReleaseAssetS3PathStyle,
		},
	})
	git := gitservice.NewService(gitservice.NewRunner(cfg.GitBinary), repositoryStorage)
	oauthClient := atproto.Must(cfg.BaseURL, db.Queries(), cfg.OAuthStateKey, cfg.OAuthCredentialKey, atproto.SystemClock{})
	repositoryEndpoints := repository.Must(cfg.BaseURL, cfg.SSHHost, cfg.SSHPort)
	repositoryStore := repository.NewPostgresStore(db.Queries())
	repositories := repository.NewService(
		repositoryStore,
		git,
		repository.SystemClock{},
		repository.UUIDv7Generator{},
		oauthClient,
		repositoryEndpoints,
	)
	authStore := auth.NewPostgresStore(db.Queries())
	clock := auth.SystemClock{}
	sessionService := auth.NewSessionService(authStore, clock, auth.UUIDv7Generator{}, auth.RandomSessionSecretGenerator{}, cfg.SessionLifetime)
	passkeys := passkey.Must(cfg.BaseURL, passkey.NewPostgresStore(db.Queries()), sessionService, auth.SystemClock{}, auth.UUIDv7Generator{}, passkey.RandomSecretGenerator{})
	loginService := identity.NewLoginService(oauthClient, authStore, sessionService, clock)
	profiles := profile.NewService(profile.NewPostgresStore(db.Queries()), oauthClient, clock)
	discovery := federation.NewDiscoveryService(federation.NewPostgresDiscoveryStore(db.Queries()))
	searchService := search.NewService(search.NewPostgresStore(db.Queries()))
	stars := star.NewService(star.NewPostgresStore(db.Queries()), oauthClient, atproto.SystemClock{})
	issues := issue.NewService(issue.NewPostgresStore(db.Queries()), oauthClient, atproto.SystemClock{}, authStore)
	comments := comment.NewService(comment.NewPostgresStore(db.Queries()), oauthClient, atproto.SystemClock{})
	eventWriter := event.NewWriter(db.Queries())
	pullRequests := pullrequest.NewApplicationService(pullrequest.NewPostgresStore(db.Queries()), git, oauthClient, atproto.SystemClock{}, authStore, eventWriter)
	moderationService := moderation.NewService(moderation.NewPostgresStore(db.Queries()), atproto.SystemClock{})
	notifications := notification.NewStore(db.Queries())
	webhooks, err := webhook.NewService(db.Queries(), cfg.OAuthCredentialKey)
	if err != nil {
		db.Close()
		_ = shutdownTelemetry(ctx)
		return nil, fmt.Errorf("create webhook service: %w", err)
	}
	webhookWorker := webhook.NewWorker(db.Queries(), webhooks)
	repositoryPurgeWorker := repository.NewPurgeWorker(repositoryStore, git)
	branchProtections := branchprotection.NewService(db.Queries(), git)
	releases, err := release.NewService(
		release.NewPostgresStore(db, db.Queries()),
		releaseAssetStorage,
		git,
		release.SystemClock{},
		release.UUIDv7Generator{},
		release.Limits{AssetBytes: cfg.ReleaseAssetMaxBytes, ReleaseBytes: cfg.ReleaseMaxBytes, RepositoryBytes: cfg.RepositoryReleaseMaxBytes},
	)
	if err != nil {
		db.Close()
		_ = shutdownTelemetry(ctx)
		return nil, fmt.Errorf("create release service: %w", err)
	}
	organizations := organization.NewService(
		organization.NewPostgresStore(db, db.Queries()),
		organization.SystemClock{},
		organization.UUIDv7Generator{},
		oauthClient,
	)
	organizationTeams := organization.NewTeamService(organization.NewPostgresStore(db, db.Queries()), organization.SystemClock{}, organization.UUIDv7Generator{})
	organizationCollaborators := organization.NewCollaboratorService(organization.NewPostgresStore(db, db.Queries()), organization.SystemClock{})
	syncProxy := syncproxy.Must(cfg.ElectricURL, cfg.ElectricSecret)
	var federationDependencies *restapi.FederationDependencies
	if cfg.TapConsumer != "" {
		federationDependencies = &restapi.FederationDependencies{
			Processor:        federationProcessor{processor: federation.NewProcessor(db, cfg.TapConsumer), logger: logger},
			TapAdminPassword: cfg.TapAdminPassword,
		}
	}
	gitAuthorizer := auth.NewGitAuthorizer(authStore, authStore, auth.SystemClock{})
	gitHTTP := githttp.NewHandler(repositories, git, gitAuthorizer, eventWriter)
	sshServer := gitssh.NewServer(
		cfg.SSHListenAddr,
		gitssh.MustHostSigner(cfg.SSHHostKeyPath),
		logger,
		auth.NewSSHKeyAuthenticator(authStore, auth.SystemClock{}),
		repositories,
		authStore,
		git,
		eventWriter,
	)

	server, err := restapi.NewServer(cfg.ListenAddr, cfg.BaseURL, db, logger, restapi.Dependencies{
		Sessions:                    auth.NewSessionAuthenticator(authStore, clock),
		Login:                       loginService,
		LocalSessions:               sessionService,
		Passkeys:                    passkeys,
		Accounts:                    authStore,
		OAuthMetadata:               oauthClient,
		TokenAuth:                   auth.NewTokenAuthenticator(authStore, clock),
		Tokens:                      auth.NewTokenService(authStore, clock, auth.UUIDv7Generator{}, auth.RandomSecretGenerator{}),
		SSHKeys:                     auth.NewSSHKeyService(authStore, clock, auth.UUIDv7Generator{}),
		Profiles:                    profiles,
		Owners:                      owner.NewPostgresResolver(db.Queries()),
		Organizations:               organizations,
		Teams:                       organizationTeams,
		Collaborators:               organizationCollaborators,
		Federation:                  federationDependencies,
		Repositories:                repositories,
		Endpoints:                   repositoryEndpoints,
		Discovery:                   discovery,
		Search:                      searchService,
		Stars:                       stars,
		Issues:                      issues,
		Comments:                    comments,
		PullRequests:                pullRequests,
		Moderation:                  moderationService,
		Notifications:               notifications,
		Webhooks:                    webhooks,
		BranchProtections:           branchProtections,
		Releases:                    releases,
		Activity:                    eventWriter,
		Authorization:               authStore,
		Git:                         git,
		Sync:                        syncProxy,
		RepositoryDeletionRetention: cfg.RepositoryDeletionRetention,
	}, gitHTTP)
	if err != nil {
		db.Close()
		_ = shutdownTelemetry(ctx)
		return nil, fmt.Errorf("create REST server: %w", err)
	}

	return app.NewWithWorkers(server, sshServer, logger, cfg.ShutdownTimeout, []app.Worker{webhookWorker, repositoryPurgeWorker},
		shutdownTelemetry,
		func(context.Context) error {
			db.Close()
			return nil
		},
	), nil
}

type federationProcessor struct {
	processor *federation.Processor
	logger    *slog.Logger
}

func (processor federationProcessor) Process(ctx context.Context, body []byte) error {
	result, err := processor.processor.Process(ctx, body)
	if err != nil {
		attrs := []any{"component", "federation", "operation", "process", "error_class", "processing"}
		processor.logger.ErrorContext(ctx, "federation event processing failed", append(attrs, observability.CorrelationAttrs(ctx)...)...)
		return err
	}
	if result.Outcome == "rejected" {
		attrs := []any{"component", "federation", "operation", "validate", "event_id", result.EventID}
		processor.logger.WarnContext(ctx, "federation event rejected", append(attrs, observability.CorrelationAttrs(ctx)...)...)
	}
	return nil
}
