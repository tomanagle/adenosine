// Package githttp implements the Git Smart HTTP transport.
package githttp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/observability"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxUploadPackRequest  = 16 << 20
	maxReceivePackRequest = 2 << 30
)

type repositoryResolver interface {
	GetByOwnerSlug(context.Context, string, string) (repository.Repository, error)
}

type uploadPacker interface {
	UploadPack(context.Context, repository.ID, io.Reader, io.Writer, gitservice.PackOptions) error
}

type receivePacker interface {
	ReceivePack(context.Context, repository.ID, io.Reader, io.Writer, gitservice.PackOptions) error
}

type writeAuthorizer interface {
	AuthorizeWrite(context.Context, *http.Request, repository.Repository) error
}

type pushEventWriter interface {
	GitPushReceived(context.Context, repository.Repository) error
}

// Handler serves anonymous reads for public repositories.
type Handler struct {
	repositories repositoryResolver
	git          interface {
		uploadPacker
		receivePacker
	}
	authorizer writeAuthorizer
	events     pushEventWriter
}

// NewHandler constructs the public Git Smart HTTP handler.
func NewHandler(repositories repositoryResolver, git interface {
	uploadPacker
	receivePacker
}, authorizer writeAuthorizer, events pushEventWriter) *Handler {
	return &Handler{repositories: repositories, git: git, authorizer: authorizer, events: events}
}

// ServeHTTP routes only allow-listed Git Smart HTTP operations.
func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner, slug, operation, ok := parsePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	repo, err := handler.repositories.GetByOwnerSlug(r.Context(), owner, slug)
	if err != nil || repo.State != repository.StateActive || repo.Visibility != repository.VisibilityPublic {
		http.NotFound(w, r)
		return
	}
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/githttp").Start(r.Context(), "githttp."+operation)
	defer span.End()
	span.SetAttributes(attribute.String("git.operation", operation), attribute.String("git.transport", "http"))
	ctx = gitservice.WithTransport(ctx, "http")
	r = r.WithContext(ctx)

	w.Header().Set("Cache-Control", "no-cache, max-age=0, must-revalidate")
	w.Header().Set("Expires", "Fri, 01 Jan 1980 00:00:00 GMT")
	w.Header().Set("Pragma", "no-cache")
	protocol := r.Header.Get("Git-Protocol")
	if protocol != "" && protocol != "version=1" && protocol != "version=2" {
		http.Error(w, "unsupported Git protocol", http.StatusBadRequest)
		return
	}

	switch {
	case r.Method == http.MethodGet && operation == "info/refs" && r.URL.Query().Get("service") == "git-upload-pack":
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, "001e# service=git-upload-pack\n0000"); err != nil {
			return
		}
		if err := handler.git.UploadPack(r.Context(), repo.ID, nil, w, gitservice.PackOptions{
			AdvertiseRefs: true,
			Protocol:      protocol,
		}); err != nil {
			handler.logFailure(r.Context(), span, repo, operation, err)
		}
	case r.Method == http.MethodPost && operation == "git-upload-pack":
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadPackRequest)
		w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
		w.WriteHeader(http.StatusOK)
		if err := handler.git.UploadPack(r.Context(), repo.ID, r.Body, w, gitservice.PackOptions{Protocol: protocol}); err != nil {
			handler.logFailure(r.Context(), span, repo, operation, err)
		}
	case r.Method == http.MethodGet && operation == "info/refs" && r.URL.Query().Get("service") == "git-receive-pack":
		if !handler.authorizeWrite(w, r, repo) {
			return
		}
		w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		if _, err := io.WriteString(w, "001f# service=git-receive-pack\n0000"); err != nil {
			return
		}
		if err := handler.git.ReceivePack(r.Context(), repo.ID, nil, w, gitservice.PackOptions{AdvertiseRefs: true, Protocol: protocol}); err != nil {
			handler.logFailure(r.Context(), span, repo, operation, err)
		}
	case r.Method == http.MethodPost && operation == "git-receive-pack":
		if !handler.authorizeWrite(w, r, repo) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxReceivePackRequest)
		w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
		w.WriteHeader(http.StatusOK)
		if err := handler.git.ReceivePack(r.Context(), repo.ID, r.Body, w, gitservice.PackOptions{Protocol: protocol}); err != nil {
			handler.logFailure(r.Context(), span, repo, operation, err)
			return
		}
		eventContext, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		if err := handler.events.GitPushReceived(eventContext, repo); err != nil {
			handler.logFailure(eventContext, span, repo, "publish-push", err)
		}
	default:
		http.NotFound(w, r)
	}
}

func (handler *Handler) logFailure(ctx context.Context, span trace.Span, repo repository.Repository, operation string, err error) {
	span.SetStatus(codes.Error, "Git HTTP operation failed")
	attrs := []any{"component", "git-http", "operation", operation, "repository_id", repo.ID.String()}
	attrs = append(attrs, gitservice.FailureAttrs(err)...)
	slog.Default().ErrorContext(ctx, "Git HTTP operation failed", append(attrs, observability.CorrelationAttrs(ctx)...)...)
}

func (handler *Handler) authorizeWrite(w http.ResponseWriter, r *http.Request, repo repository.Repository) bool {
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/githttp").Start(r.Context(), "githttp.authorize",
		trace.WithAttributes(attribute.String("git.operation", "receive-pack"), attribute.String("git.transport", "http")))
	var err error
	if repo.ArchivedAt != nil {
		err = auth.ErrForbidden
	} else {
		err = handler.authorizer.AuthorizeWrite(ctx, r.WithContext(ctx), repo)
	}
	span.SetAttributes(attribute.Bool("authorization.allowed", err == nil))
	if err != nil {
		span.SetStatus(codes.Error, "authorization denied")
	}
	span.End()
	switch {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="Adenosine Git"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	default:
		http.Error(w, "permission denied", http.StatusForbidden)
	}
	return false
}

func parsePath(path string) (owner, slug, operation string, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if (len(parts) != 3 && len(parts) != 4) || parts[0] == "" || parts[0] == "." || parts[0] == ".." || !strings.HasSuffix(parts[1], ".git") {
		return "", "", "", false
	}
	slug = strings.TrimSuffix(parts[1], ".git")
	if slug == "" {
		return "", "", "", false
	}
	if len(parts) == 4 && parts[2] == "info" && parts[3] == "refs" {
		operation = "info/refs"
		return parts[0], slug, operation, true
	}
	if len(parts) == 3 && parts[2] == "git-upload-pack" {
		return parts[0], slug, parts[2], true
	}
	if len(parts) == 3 && parts[2] == "git-receive-pack" {
		return parts[0], slug, parts[2], true
	}
	return "", "", "", false
}
