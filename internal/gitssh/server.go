// Package gitssh implements authenticated Git transport over SSH.
package gitssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	gitservice "github.com/adenosine-dev/adenosine/internal/git"
	"github.com/adenosine-dev/adenosine/internal/observability"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/ssh"
)

const (
	accountDIDPermission      = "adenosine.account_did"
	keyIDPermission           = "adenosine.ssh_key_id"
	maxConnections            = 128
	maxSessions               = 64
	defaultHandshakeTimeout   = 10 * time.Second
	defaultSessionIdleTimeout = 2 * time.Minute
)

type keyAuthenticator interface {
	Lookup(context.Context, ssh.PublicKey) (auth.SSHIdentity, error)
	RecordUse(context.Context, uuid.UUID) error
}

type repositoryResolver interface {
	GetByOwnerSlug(context.Context, string, string) (repository.Repository, error)
}

type repositoryAuthorizer interface {
	CanReadRepository(context.Context, string, repository.ID) (bool, error)
	CanWriteRepository(context.Context, string, repository.ID) (bool, error)
}

type sessionPacker interface {
	UploadPackSession(context.Context, repository.ID, io.Reader, io.Writer, string) error
	ReceivePackSession(context.Context, repository.ID, io.Reader, io.Writer, string) error
}

type pushEventWriter interface {
	GitPushReceived(context.Context, repository.Repository) error
}

// Server accepts public-key authenticated Git SSH sessions.
type Server struct {
	address      string
	config       *ssh.ServerConfig
	logger       *slog.Logger
	keys         keyAuthenticator
	repositories repositoryResolver
	authorizer   repositoryAuthorizer
	git          sessionPacker
	events       pushEventWriter

	mu                 sync.Mutex
	listener           net.Listener
	connections        map[net.Conn]struct{}
	wait               sync.WaitGroup
	connectionSlots    chan struct{}
	sessionSlots       chan struct{}
	handshakeTimeout   time.Duration
	sessionIdleTimeout time.Duration
}

// NewServer constructs an SSH Git server from initialized dependencies.
func NewServer(address string, signer ssh.Signer, logger *slog.Logger, keys keyAuthenticator, repositories repositoryResolver, authorizer repositoryAuthorizer, git sessionPacker, events pushEventWriter) *Server {
	server := &Server{
		address:            address,
		logger:             logger,
		keys:               keys,
		repositories:       repositories,
		authorizer:         authorizer,
		git:                git,
		events:             events,
		connections:        make(map[net.Conn]struct{}),
		connectionSlots:    make(chan struct{}, maxConnections),
		sessionSlots:       make(chan struct{}, maxSessions),
		handshakeTimeout:   defaultHandshakeTimeout,
		sessionIdleTimeout: defaultSessionIdleTimeout,
	}
	server.config = &ssh.ServerConfig{
		MaxAuthTries:  3,
		ServerVersion: "SSH-2.0-Adenosine",
		PublicKeyCallback: func(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if metadata.User() != "git" {
				return nil, auth.ErrUnauthorized
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			identity, err := server.keys.Lookup(ctx, key)
			if err != nil {
				return nil, err
			}
			return &ssh.Permissions{Extensions: map[string]string{
				accountDIDPermission: identity.AccountDID,
				keyIDPermission:      identity.KeyID.String(),
			}}, nil
		},
		VerifiedPublicKeyCallback: func(_ ssh.ConnMetadata, _ ssh.PublicKey, permissions *ssh.Permissions, _ string) (*ssh.Permissions, error) {
			keyID, err := uuid.Parse(permissions.Extensions[keyIDPermission])
			if err != nil {
				return nil, fmt.Errorf("parse authenticated SSH key ID: %w", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.keys.RecordUse(ctx, keyID); err != nil {
				return nil, err
			}
			return permissions, nil
		},
	}
	server.config.AddHostKey(signer)
	return server
}

// Address returns the configured listen address.
func (server *Server) Address() string { return server.address }

// ListenAndServe accepts SSH connections until shutdown.
func (server *Server) ListenAndServe() error {
	listener, err := net.Listen("tcp", server.address)
	if err != nil {
		return fmt.Errorf("listen for SSH: %w", err)
	}
	return server.serve(listener)
}

func (server *Server) serve(listener net.Listener) error {
	server.mu.Lock()
	server.listener = listener
	server.mu.Unlock()
	server.logger.Info("adenosine listening", "component", "ssh", "address", listener.Addr().String())

	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return net.ErrClosed
			}
			return fmt.Errorf("accept SSH connection: %w", err)
		}
		select {
		case server.connectionSlots <- struct{}{}:
		default:
			_ = connection.Close()
			continue
		}
		server.mu.Lock()
		server.connections[connection] = struct{}{}
		server.mu.Unlock()
		server.wait.Add(1)
		go server.handleConnection(connection)
	}
}

// Shutdown stops accepting connections and waits for active sessions.
func (server *Server) Shutdown(ctx context.Context) error {
	server.mu.Lock()
	listener := server.listener
	connections := make([]net.Conn, 0, len(server.connections))
	for connection := range server.connections {
		connections = append(connections, connection)
	}
	server.mu.Unlock()
	var shutdownErr error
	if listener != nil {
		shutdownErr = listener.Close()
	}
	for _, connection := range connections {
		shutdownErr = errors.Join(shutdownErr, connection.Close())
	}
	done := make(chan struct{})
	go func() {
		server.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return ignoreClosed(shutdownErr)
	case <-ctx.Done():
		return errors.Join(ignoreClosed(shutdownErr), ctx.Err())
	}
}

func (server *Server) handleConnection(connection net.Conn) {
	defer server.wait.Done()
	defer func() { <-server.connectionSlots }()
	defer func() {
		server.mu.Lock()
		delete(server.connections, connection)
		server.mu.Unlock()
		_ = connection.Close()
	}()
	activity := newActivityConn(connection)
	activity.setTimeout(server.handshakeTimeout)
	sshConnection, channels, requests, err := ssh.NewServerConn(activity, server.config)
	if err != nil {
		return
	}
	activity.setTimeout(server.sessionIdleTimeout)
	defer sshConnection.Close()
	go ssh.DiscardRequests(requests)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = sshConnection.Wait()
		cancel()
	}()
	var sessions sync.WaitGroup
	for channel := range channels {
		if channel.ChannelType() != "session" {
			_ = channel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		accepted, channelRequests, err := channel.Accept()
		if err != nil {
			continue
		}
		select {
		case server.sessionSlots <- struct{}{}:
		default:
			_ = accepted.Close()
			continue
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			defer func() { <-server.sessionSlots }()
			server.handleSession(ctx, sshConnection, accepted, channelRequests)
		}()
	}
	sessions.Wait()
}

func (server *Server) handleSession(ctx context.Context, connection *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	protocol := ""
	for request := range requests {
		switch request.Type {
		case "env":
			var payload struct{ Name, Value string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil || payload.Name != "GIT_PROTOCOL" || (payload.Value != "version=1" && payload.Value != "version=2") {
				_ = request.Reply(false, nil)
				continue
			}
			protocol = payload.Value
			_ = request.Reply(true, nil)
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
				_ = request.Reply(false, nil)
				return
			}
			parsed, err := parseCommand(payload.Command)
			if err != nil {
				_ = request.Reply(false, nil)
				return
			}
			_ = request.Reply(true, nil)
			status := uint32(0)
			if err := server.execute(ctx, connection.Permissions.Extensions[accountDIDPermission], parsed, protocol, channel); err != nil {
				status = 1
				_, _ = io.WriteString(channel.Stderr(), "Adenosine: repository unavailable or access denied\n")
			}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func (server *Server) execute(ctx context.Context, accountDID string, parsed command, protocol string, channel ssh.Channel) (resultErr error) {
	ctx = gitservice.WithTransport(ctx, "ssh")
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/gitssh").Start(ctx, "gitssh."+parsed.operation)
	defer func() {
		if resultErr != nil {
			span.SetStatus(codes.Error, "Git SSH operation failed")
			attrs := []any{"component", "git-ssh", "operation", parsed.operation}
			attrs = append(attrs, gitservice.FailureAttrs(resultErr)...)
			server.logger.ErrorContext(ctx, "Git SSH operation failed", append(attrs, observability.CorrelationAttrs(ctx)...)...)
		}
		span.End()
	}()
	span.SetAttributes(attribute.String("git.operation", parsed.operation))
	repo, err := server.repositories.GetByOwnerSlug(ctx, parsed.owner, parsed.slug)
	if err != nil || repo.State != repository.StateActive {
		span.SetStatus(codes.Error, "repository unavailable")
		return repository.ErrNotFound
	}
	allowed := false
	authorizeCtx, authorizeSpan := otel.Tracer("github.com/adenosine-dev/adenosine/internal/gitssh").Start(ctx, "gitssh.authorize",
		trace.WithAttributes(attribute.String("git.operation", parsed.operation), attribute.String("git.transport", "ssh")))
	if parsed.operation == "upload-pack" {
		allowed, err = server.authorizer.CanReadRepository(authorizeCtx, accountDID, repo.ID)
	} else {
		allowed, err = server.authorizer.CanWriteRepository(authorizeCtx, accountDID, repo.ID)
	}
	authorizeSpan.SetAttributes(attribute.Bool("authorization.allowed", allowed && err == nil))
	if err != nil || !allowed {
		authorizeSpan.SetStatus(codes.Error, "authorization denied")
	}
	authorizeSpan.End()
	if err != nil {
		return fmt.Errorf("authorize SSH repository operation: %w", err)
	}
	if !allowed {
		return auth.ErrForbidden
	}
	if parsed.operation == "receive-pack" && repo.ArchivedAt != nil {
		return auth.ErrForbidden
	}
	if parsed.operation == "upload-pack" {
		return server.git.UploadPackSession(ctx, repo.ID, channel, channel, protocol)
	}
	if err := server.git.ReceivePackSession(ctx, repo.ID, channel, channel, protocol); err != nil {
		return err
	}
	eventContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return server.events.GitPushReceived(eventContext, repo)
}

type activityConn struct {
	net.Conn
	timeoutNanos atomic.Int64
}

func newActivityConn(connection net.Conn) *activityConn {
	return &activityConn{Conn: connection}
}

func (connection *activityConn) setTimeout(timeout time.Duration) {
	connection.timeoutNanos.Store(int64(timeout))
	connection.refreshDeadline()
}

func (connection *activityConn) Read(value []byte) (int, error) {
	connection.refreshDeadline()
	return connection.Conn.Read(value)
}

func (connection *activityConn) Write(value []byte) (int, error) {
	connection.refreshDeadline()
	return connection.Conn.Write(value)
}

func (connection *activityConn) refreshDeadline() {
	timeout := time.Duration(connection.timeoutNanos.Load())
	if timeout > 0 {
		_ = connection.Conn.SetDeadline(time.Now().Add(timeout))
	}
}

func ignoreClosed(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
