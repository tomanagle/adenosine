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
	"time"

	"github.com/adenosine-dev/adenosine/internal/auth"
	"github.com/adenosine-dev/adenosine/internal/repository"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/crypto/ssh"
)

const (
	accountDIDPermission = "adenosine.account_did"
	keyIDPermission      = "adenosine.ssh_key_id"
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

	mu          sync.Mutex
	listener    net.Listener
	connections map[net.Conn]struct{}
	wait        sync.WaitGroup
}

// NewServer constructs an SSH Git server from initialized dependencies.
func NewServer(address string, signer ssh.Signer, logger *slog.Logger, keys keyAuthenticator, repositories repositoryResolver, authorizer repositoryAuthorizer, git sessionPacker, events pushEventWriter) *Server {
	server := &Server{
		address:      address,
		logger:       logger,
		keys:         keys,
		repositories: repositories,
		authorizer:   authorizer,
		git:          git,
		events:       events,
		connections:  make(map[net.Conn]struct{}),
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
	defer func() {
		server.mu.Lock()
		delete(server.connections, connection)
		server.mu.Unlock()
		_ = connection.Close()
	}()
	sshConnection, channels, requests, err := ssh.NewServerConn(connection, server.config)
	if err != nil {
		return
	}
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
		sessions.Add(1)
		go func() {
			defer sessions.Done()
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

func (server *Server) execute(ctx context.Context, accountDID string, parsed command, protocol string, channel ssh.Channel) error {
	ctx, span := otel.Tracer("github.com/adenosine-dev/adenosine/internal/gitssh").Start(ctx, "gitssh."+parsed.operation)
	defer span.End()
	span.SetAttributes(attribute.String("git.operation", parsed.operation))
	repo, err := server.repositories.GetByOwnerSlug(ctx, parsed.owner, parsed.slug)
	if err != nil || repo.State != repository.StateActive {
		span.SetStatus(codes.Error, "repository unavailable")
		return repository.ErrNotFound
	}
	allowed := false
	if parsed.operation == "upload-pack" {
		allowed, err = server.authorizer.CanReadRepository(ctx, accountDID, repo.ID)
	} else {
		allowed, err = server.authorizer.CanWriteRepository(ctx, accountDID, repo.ID)
	}
	if err != nil {
		return fmt.Errorf("authorize SSH repository operation: %w", err)
	}
	if !allowed {
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

func ignoreClosed(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
