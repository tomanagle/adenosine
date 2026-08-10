# Adenosine — Implementation Plan

> A federated, self-hostable Git forge built in Go, using standard Git protocols for source control and AT Protocol for identity, discovery, and collaboration metadata.

## 1. Product thesis

Adenosine is not a new version control system and it is not a replacement for Git.

It is a **Git forge** in the same broad category as GitHub, GitLab, Gitea, and Forgejo, but designed around a different ownership model:

- Git remains the source-control protocol.
- Every Adenosine instance can host Git repositories.
- Repositories can be discovered across instances.
- AT Protocol provides portable identity and the federated metadata network.
- A repository has a portable identity that is not permanently tied to one hostname.
- Issues, stars, pull-request metadata, reviews, and other public collaboration objects can be represented as ATProto records.
- Any Adenosine instance can index the public Adenosine network and present repositories hosted elsewhere.
- A user can use the normal `git` CLI without installing an Adenosine-specific Git client.

The desired user experience is:

```bash
git clone https://code.alice.dev/alice/project.git
cd project

git checkout -b fix/widget
# edit
git push -u origin fix/widget
```

while a completely different Adenosine instance can display:

```text
alice.dev/project
Hosted by: code.alice.dev
Stars: 842
Issues: 12
Pull requests: 3
```

The core design principles are:

```text
Git = source code + history + refs + clone/fetch/push

ATProto = portable identity + network discovery + portable public collaboration records

REST API = stable application contract for every UI, CLI, bot, integration, and service

Postgres = authoritative local application state + indexed projection of the public network

Electric = optional realtime read path from an instance's Postgres projection to clients

Adenosine Web = one first-party client of the public APIs, not a privileged frontend
```

Adenosine is **API-first, dependency-injection-first, network-first, and self-hosting-first**.

An independently developed UI must be capable of doing everything the official web application can do using documented public interfaces. The official `web` package must not call Go internals, private server functions, hidden endpoints, or the database directly.

Every independently deployed Adenosine instance joins the same logical public network by publishing and indexing Adenosine ATProto records. An instance is not an isolated GitLab-style island: it can discover public repositories hosted elsewhere and locally present network-wide repositories, stars, issues, pull requests, reviews, and other federated metadata.

Realtime is a presentation/read-path capability, not a new source of truth. REST remains the stable write contract. Electric may deliver changes from PostgreSQL to capable clients, while clients that only implement REST remain fully functional.

Self-hosting must be a primary product experience. A user should be able to run Adenosine locally with Docker Compose in minutes, deploy it to a supported cloud using maintained infrastructure-as-code, and upgrade or back it up using scripts shipped in the repository.

---

# 2. Goals

## v1 goals

A useful v1 should support:

1. Sign in with an ATProto identity.
2. Register an existing ATProto account with an Adenosine instance.
3. Add SSH public keys and/or HTTPS credentials.
4. Create a public Git repository.
5. Clone, fetch, pull, and push using the standard Git CLI.
6. Browse files, directories, commits, and branches in the web UI.
7. Publish a repository record to the owner's ATProto repo.
8. Discover repositories hosted by other Adenosine instances.
9. Display remote repositories in the local Adenosine UI.
10. Create public issues and comments as ATProto records.
11. Create and review pull requests.
12. Merge a pull request into a repository hosted by the local instance.
13. Star repositories.
14. Expose a well-documented public HTTP API.
15. Run a complete Adenosine instance with a small number of services.

## Explicit non-goals for v1

Do not build these initially:

- GitHub Actions replacement.
- Arbitrary CI runner infrastructure.
- Package registry.
- Container registry.
- Codespaces.
- Git LFS hosting beyond basic compatibility work.
- Private federation.
- Project boards.
- Discussions.
- Advanced branch protection.
- Enterprise SSO.
- Custom Git protocol.
- Custom Git object storage implementation.
- A full ATProto relay.
- A custom PDS.

These can be added after the basic forge is proven.

---

# 3. High-level architecture

Adenosine is deliberately split into **protocol surfaces**, **application/domain services**, and **infrastructure adapters**.

```text
                                      Public Internet
                                             |
                     +-----------------------+------------------------+
                     |                       |                        |
                HTTPS :443                SSH :22              ATProto network
                     |                       |                        |
                     v                       v                        v
          +---------------------+    +---------------+       Relay / PDS / Tap
          |   adenosine-web     |    | adenosine-sshd|              |
          |                     |    +-------+-------+              |
          | REST API            |            |                      v
          | OpenAPI docs        |            |              +------------------+
          | Git Smart HTTP      |            |              | adenosine-indexer |
          | Electric sync proxy |            |              +---------+--------+
          +----------+----------+            |                        |
                     |                       |                        |
                     +-----------+-----------+------------------------+
                                 |
                         dependency-injected
                         application services
                                 |
           +---------------------+-------------------------+
           |                     |                         |
           v                     v                         v
      PostgreSQL            repository store          ATProto client
           |                     |
           |                     v
           |                 native `git`
           |
           +--------------------------+
                                      |
                                  Electric
                              Postgres Sync
                                      |
                          authenticated sync API
                                      |
                                      v
                          +-----------------------+
                          |   web/ TanStack Start |
                          |                       |
                          | TanStack Query        |
                          | TanStack DB           |
                          | Electric collections  |
                          +-----------------------+
```

Recommended deployable processes:

```text
adenosine web
adenosine sshd
adenosine indexer
electric                 # recommended for realtime UI; optional for API-only installs
tap                      # recommended ATProto ingestion component
postgres
```

For the simplest installation:

```text
adenosine serve
postgres
electric
tap
```

where `adenosine serve` runs HTTP, SSH, jobs, and indexing orchestration in one Go process.

Electric enhances client reactivity; it must not be required for Git, REST compatibility, or federation correctness.

## 3.1 Canonical local write path

```text
Official Web / third-party UI / CLI / bot
                |
                | REST mutation
                v
         /api/v1/...
                |
         auth + validation
                |
        application service
                |
       PostgreSQL / Git / PDS
                |
          durable outbox
```

## 3.2 Realtime local read path

```text
Postgres
   |
logical replication
   |
Electric
   |
Adenosine authenticated sync endpoint
   |
TanStack DB Electric collection
   |
live query
   |
React component updates
```

Electric is a read-path sync engine. Writes continue through the public REST API.

## 3.3 Federation ingestion path

```text
Remote user/instance publishes Adenosine record
                |
                v
              PDS
                |
                v
        ATProto relay/network
                |
                v
              Tap
                |
                v
       adenosine indexer
                |
         validates/projects
                |
                v
          local Postgres
                |
         +------+------+
         |             |
         v             v
       REST          Electric
       reads         realtime
         |             |
         +------+------+
                |
             clients
```

A remote star, issue, PR, review, or repository update can therefore become realtime locally without any instance-to-instance websocket protocol. ATProto delivers the network event; the local indexer projects it; Electric propagates the changed projection to connected clients.

## 3.4 Git data path

Git never passes through ATProto or Electric:

```text
git CLI
   |
HTTPS / SSH
   |
Adenosine Git transport
   |
native Git
   |
bare repository
```

## 3.5 What "joining the network" means

An Adenosine instance participates in the public network when it:

1. understands the public Adenosine Lexicons;
2. publishes records for local users/projects into ATProto;
3. consumes/indexes public Adenosine records from ATProto;
4. resolves project identities and their current Git hosts;
5. exposes that network projection through its public REST API;
6. optionally exposes the same projection through realtime sync;
7. interacts with repositories hosted by other instances through standard Git URLs.

No central Adenosine directory or instance registry is required for discovery.

## 3.6 Network-wide UI behavior

A user visiting `forge.example` should be able to search for and view:

```text
repository hosted by forge.example
repository hosted by code.alice.dev
repository hosted by git.company.org
repository implemented by another compatible forge
```

from the same UI.

The UI should visibly distinguish:

```text
Hosted here
Hosted remotely at <host>
```

but the discovery and collaboration experience should otherwise feel unified.

# 4. Why separate the applications

## `adenosine-web`

Responsibilities:

- Web UI.
- JSON/API endpoints.
- ATProto login/OAuth integration.
- Repository creation.
- Repository browsing.
- Smart Git HTTP.
- Issue/PR/review UI.
- Webhooks.
- Admin interface.
- Health endpoints.

It is safe to combine the human web application and Git Smart HTTP in v1 because both use HTTP and share authorization/domain logic.

At scale, Git HTTP can be extracted into `adenosine-git-http`.

## `adenosine-sshd`

Responsibilities:

- Listen for SSH connections.
- Authenticate SSH public keys.
- Resolve the account.
- Parse the Git command requested by the client.
- Authorize repository access.
- Execute `git-upload-pack` or `git-receive-pack`.
- Stream stdin/stdout directly between the SSH channel and Git subprocess.

Keep SSH separate because:

- it is a different network protocol;
- it often needs different port/network configuration;
- failures or load from large SSH pushes should not interfere with the web app;
- it creates a clean eventual scaling boundary.

## `adenosine-indexer`

Responsibilities:

- Consume Adenosine ATProto records from Tap/Relay.
- Validate Lexicon records.
- Resolve DIDs.
- Project distributed records into local query tables.
- Maintain search/discovery indexes.
- Handle updates/deletes.
- Resume from cursors after restart.
- Periodically repair missing/inconsistent indexed data.

The indexer must be considered a **derived-data system**. ATProto is the public record source; the index is rebuilt/recoverable.

---

# 5. Repository structure

Use one open-source monorepo containing the Go server, public API specification, generated clients, first-party TanStack Start app, infrastructure-as-code, and self-hosting scripts.

```text
adenosine/
├── cmd/
│   └── adenosine/
│       └── main.go
│
├── api/
│   ├── openapi.yaml                  # canonical REST contract
│   ├── examples/
│   └── generated/
│       └── go/
│
├── internal/
│   ├── di/
│   │   ├── container.go
│   │   ├── providers.go
│   │   └── testing.go
│   ├── app/
│   ├── auth/
│   ├── identity/
│   ├── repository/
│   ├── git/
│   ├── storage/
│   ├── githttp/
│   ├── gitssh/
│   ├── atproto/
│   ├── federation/
│   ├── issues/
│   ├── pullrequest/
│   ├── review/
│   ├── star/
│   ├── search/
│   ├── event/
│   ├── webhook/
│   ├── sync/
│   │   ├── service.go
│   │   ├── shapes.go
│   │   └── proxy.go
│   ├── restapi/
│   │   ├── server.go
│   │   ├── handlers/
│   │   ├── middleware/
│   │   ├── mapper/
│   │   ├── pagination.go
│   │   └── errors.go
│   ├── database/
│   └── observability/
│
├── migrations/
│
├── lexicons/
│   ├── dev.adenosine.profile.json
│   ├── dev.adenosine.repo.json
│   ├── dev.adenosine.issue.json
│   ├── dev.adenosine.issue.comment.json
│   ├── dev.adenosine.issue.status.json
│   ├── dev.adenosine.pullRequest.json
│   ├── dev.adenosine.pullRequest.status.json
│   ├── dev.adenosine.review.json
│   └── dev.adenosine.star.json
│
├── packages/
│   ├── api-client/
│   │   ├── package.json
│   │   └── src/generated/
│   └── lexicons/
│       └── package.json
│
├── web/
│   ├── package.json
│   ├── app.config.ts
│   ├── vite.config.ts
│   └── src/
│       ├── routes/
│       ├── components/
│       ├── features/
│       │   ├── repositories/
│       │   ├── issues/
│       │   ├── pulls/
│       │   ├── reviews/
│       │   ├── stars/
│       │   └── explore/
│       ├── api/
│       │   ├── client.ts
│       │   ├── queries.ts
│       │   └── mutations.ts
│       ├── db/
│       │   ├── collections/
│       │   ├── electric.ts
│       │   └── schema.ts
│       └── sync/
│
├── infra/
│   ├── observability/
│   │   ├── otel-collector.yaml
│   │   ├── otel-collector.dev.yaml
│   │   ├── dashboards/
│   │   │   ├── overview.json
│   │   │   ├── git.json
│   │   │   └── federation.json
│   │   └── README.md
│   ├── pulumi/
│   │   ├── components/
│   │   │   ├── adenosine/
│   │   │   ├── postgres/
│   │   │   ├── electric/
│   │   │   └── storage/
│   │   ├── railway/
│   │   │   ├── Pulumi.yaml
│   │   │   ├── Pulumi.example.yaml
│   │   │   └── index.ts
│   │   ├── aws/
│   │   │   ├── Pulumi.yaml
│   │   │   ├── Pulumi.example.yaml
│   │   │   └── index.ts
│   │   └── local/
│   ├── caddy/
│   │   └── Caddyfile
│   └── systemd/
│
├── dev/
│   ├── Dockerfile
│   ├── docker-compose.yml
│   ├── entrypoint.sh
│   ├── air.toml
│   └── .env.example
│
├── scripts/
│   ├── bootstrap.sh
│   ├── dev.sh
│   ├── generate.sh
│   ├── migrate.sh
│   ├── backup.sh
│   ├── restore.sh
│   ├── upgrade.sh
│   ├── doctor.sh
│   ├── deploy-railway.sh
│   └── deploy-aws.sh
│
├── docs/
│   ├── architecture.md
│   ├── api.md
│   ├── api-versioning.md
│   ├── dependency-injection.md
│   ├── observability.md
│   ├── realtime.md
│   ├── federation.md
│   ├── self-hosting.md
│   ├── deployment-railway.md
│   ├── deployment-aws.md
│   ├── backups.md
│   ├── upgrading.md
│   ├── git-http.md
│   ├── git-ssh.md
│   ├── lexicons.md
│   ├── security.md
│   └── contributing.md
│
├── test/
│   ├── contract/
│   ├── integration/
│   ├── git/
│   ├── federation/
│   └── realtime/
│
├── go.mod
├── go.sum
├── package.json
├── pnpm-workspace.yaml
├── Makefile
├── README.md
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

The critical client boundary is:

```text
web/
   |
   | REST + documented sync protocol only
   v
public Adenosine interfaces
   |
   v
Go application
```

The critical deployment boundary is:

```text
same Adenosine image/binary
        |
        +--> Docker Compose
        +--> Railway
        +--> AWS
        +--> bare VM/systemd
```

Cloud deployment templates must consume the same documented environment variables and persistent-storage contracts rather than encoding cloud-specific application behavior.

# 6. Dependency injection and package design

Dependency injection is a foundational design requirement.

Use **explicit constructor injection** rather than global state, package singletons, service locators, or hidden dependency lookup.

A runtime DI framework is not required. The dependency graph should be visible and boring enough that an open-source contributor can follow it from `main.go`.

## 6.1 Composition root

Only the executable/composition layer constructs concrete infrastructure.

```go
func buildApplication(cfg Config) (*Application, error) {
    db, err := database.Open(cfg.DatabaseURL)
    if err != nil {
        return nil, err
    }

    repoStore := storage.NewFilesystemRepositoryStore(cfg.RepositoryRoot)
    gitRunner := git.NewCommandRunner(cfg.GitBinary)
    gitService := git.NewService(gitRunner, repoStore)

    repositoryStore := repository.NewPostgresStore(db)
    accountStore := identity.NewPostgresStore(db)
    authorizer := auth.NewAuthorizer(db)

    atClient := atproto.NewClient(cfg.ATProto)

    repositoryService := repository.NewService(
        repositoryStore,
        repoStore,
        gitService,
        atClient,
    )

    api := restapi.NewServer(
        repositoryService,
        authorizer,
    )

    return app.New(...), nil
}
```

Concrete construction lives in:

```text
cmd/adenosine
internal/di
```

Domain packages must not construct their own database pools, HTTP clients, Git runners, clocks, ID generators, event buses, or ATProto clients.

## 6.2 Inject capabilities, not a giant container

Avoid:

```go
type RepositoryService struct {
    Container *di.Container
}
```

Prefer:

```go
type RepositoryService struct {
    repos     RepositoryStore
    git       GitService
    publisher Publisher
    events    EventWriter
    clock     Clock
}
```

Constructor:

```go
func NewRepositoryService(
    repos RepositoryStore,
    git GitService,
    publisher Publisher,
    events EventWriter,
    clock Clock,
) *RepositoryService
```

## 6.3 Interfaces live near consumers

Do not create one giant `interfaces` package.

A domain package defines the narrow capability it needs:

```go
package repository

type GitRepositoryCreator interface {
    Init(ctx context.Context, id ID) error
}

type Publisher interface {
    PublishRepository(ctx context.Context, repo Repository) (ATRef, error)
}
```

Adapters satisfy these interfaces naturally.

## 6.4 Inject deterministic infrastructure

Inject:

```text
Clock
IDGenerator
TokenGenerator
GitRunner
RepositoryStore
EventWriter
ATProtoPublisher
DIDResolver
WebhookSender
RemoteGitFetcher
```

This gives fast unit tests without mocking entire subsystems.

## 6.5 Domain-oriented packages

Avoid a global MVC layout:

```text
controllers/
services/
repositories/
models/
```

Prefer:

```text
repository/
issues/
pullrequest/
git/
atproto/
federation/
```

Transport packages adapt protocols to domain services:

```text
restapi/
githttp/
gitssh/
```

## 6.6 Dependency direction

```text
REST / Git HTTP / SSH / indexer / worker
                 |
                 v
           domain services
                 |
         capability interfaces
                 |
      +----------+-----------+
      |                      |
      v                      v
Postgres/filesystem       external systems
native Git               ATProto/Electric
```

Domain code must not depend on HTTP handlers, generated OpenAPI wire models, React, or Electric.

## 6.7 Public API models are edge models

Generated OpenAPI types stay at the REST edge.

```text
OpenAPI request
     |
REST mapper
     |
domain input
     |
service
     |
domain result
     |
REST mapper
     |
OpenAPI response
```

This prevents accidental coupling between the public API and internal persistence models.

# 7. Stable identifiers

Do not make owner/name the internal identity of a repository.

Use an opaque stable ID:

```text
repo_01J...
```

or UUID/UUIDv7.

A local repository has:

```text
ID               stable local ID
OwnerDID         owner's ATProto DID
Slug             human-readable slug
DefaultBranch    e.g. main
Visibility       public/private
ATURI            canonical ATProto record URI
StorageKey       stable storage key
CreatedAt
UpdatedAt
```

A public repo's canonical network identity should ultimately be its AT URI, for example:

```text
at://did:plc:abc123/dev.adenosine.repo/3kxyz...
```

The following should be mutable aliases:

```text
alice/project
code.alice.dev/alice/project.git
```

This lets hosting move while project identity survives.

---

# 8. Git repository storage

## v1

Use bare repositories on local disk.

Example:

```text
/var/lib/adenosine/repos/
├── 01/
│   └── repo_01JABC....git
├── 02/
└── ...
```

Do not derive the physical path from owner/slug.

Good:

```text
repo ID -> deterministic storage key -> path
```

Bad:

```text
/repos/alice/project.git
```

because renames and ownership transfers become filesystem moves.

Use sharding on the stable ID:

```text
/var/lib/adenosine/repos/4a/19/<repo-id>.git
```

## RepositoryStore interface

```go
type RepositoryStore interface {
    InitBare(ctx context.Context, id repository.ID) error
    Path(ctx context.Context, id repository.ID) (string, error)
    Exists(ctx context.Context, id repository.ID) (bool, error)
    Delete(ctx context.Context, id repository.ID) error
}
```

Only the storage package knows physical paths.

This allows future storage implementations:

```text
FilesystemRepositoryStore
RemoteRepositoryStore
ReplicatedRepositoryStore
```

without contaminating domain code.

---

# 9. Native Git strategy

Do not implement Git object storage or pack protocol manually in v1.

Use the system `git` binary as the source of truth.

Operations include:

```bash
git init --bare
git upload-pack
git receive-pack
git for-each-ref
git cat-file --batch
git ls-tree
git rev-list
git diff
git merge-base
git merge-tree
```

Reasons:

- mature;
- battle-tested;
- protocol-compatible;
- handles pack negotiation;
- supports protocol v2;
- avoids subtle corruption/security bugs;
- lets Adenosine focus on the forge.

Wrap native Git behind one package:

```go
type Service interface {
    Init(ctx context.Context, repo repository.ID) error

    UploadPack(
        ctx context.Context,
        repo repository.ID,
        input io.Reader,
        output io.Writer,
        opts UploadPackOptions,
    ) error

    ReceivePack(
        ctx context.Context,
        repo repository.ID,
        input io.Reader,
        output io.Writer,
        opts ReceivePackOptions,
    ) error

    Refs(ctx context.Context, repo repository.ID) ([]Ref, error)
    Tree(ctx context.Context, repo repository.ID, rev string, path string) (Tree, error)
    Blob(ctx context.Context, repo repository.ID, rev string, path string) (io.ReadCloser, error)
    Commits(ctx context.Context, repo repository.ID, ref string, limit int) ([]Commit, error)
    Diff(ctx context.Context, repo repository.ID, base, head string) (Diff, error)
    MergeBase(ctx context.Context, repo repository.ID, a, b string) (string, error)
}
```

This boundary is extremely important.

It means the application can eventually replace particular native Git operations without changing the rest of the system.

---

# 10. Normal Git CLI compatibility

No custom Git client is required.

A user sees:

```bash
git clone https://code.example.com/alice/project.git
```

or:

```bash
git clone git@code.example.com:alice/project.git
```

Everything after this point is standard Git transport.

Adenosine-specific CLI tooling may later exist for:

```bash
adenosine repo create
adenosine pr create
adenosine issue create
adenosine auth login
```

but it must not be required for:

```text
clone
fetch
pull
push
branch
tag
merge
```

---

# 11. Smart HTTP network flow

Git supports Smart HTTP.

The important request classes are service discovery and RPC.

For clone/fetch:

```text
Git CLI
  |
  | GET /alice/project.git/info/refs?service=git-upload-pack
  v
Adenosine HTTP
  |
  | resolve repository
  | authorize READ
  v
git upload-pack --stateless-rpc --advertise-refs
  |
  v
Git CLI

Git CLI
  |
  | POST /alice/project.git/git-upload-pack
  v
Adenosine HTTP
  |
  | authorize READ
  | stream body -> git stdin
  | stream git stdout -> response
  v
git upload-pack --stateless-rpc
```

For push:

```text
Git CLI
  |
  | GET /alice/project.git/info/refs?service=git-receive-pack
  v
Adenosine
  |
  | authenticate
  | authorize WRITE
  v
git receive-pack --advertise-refs

Git CLI
  |
  | POST /alice/project.git/git-receive-pack
  v
Adenosine
  |
  | authenticate
  | authorize WRITE
  | stream directly
  v
git receive-pack
  |
  | refs changed
  v
post-receive processing
```

Critical rule:

**Never buffer a Git packfile into memory.**

Use:

```text
request body -> child stdin
child stdout -> response body
```

with bounded buffers and context cancellation.

---

# 12. HTTP Git authentication

Public repository reads:

```text
anonymous -> allowed
```

Pushes:

```text
authenticated identity -> permission check -> allowed/denied
```

HTTPS authentication should initially support a personal access token or app password.

Example:

```bash
git clone https://code.example.com/alice/project.git
```

Public clone does not require credentials.

For push:

```text
HTTP Basic username: handle or fixed username
HTTP Basic password: generated access token
```

Do not use the user's ATProto account password as a Git password.

Tokens should be:

- random;
- hashed at rest;
- scoped;
- revocable;
- optionally repository-specific;
- last-used timestamped.

Later, support Git Credential Manager/browser OAuth flows.

---

# 13. SSH Git network flow

Standard Git SSH usage:

```bash
git clone git@code.example.com:alice/project.git
```

Git opens SSH and requests a command similar to:

```text
git-upload-pack 'alice/project.git'
```

or:

```text
git-receive-pack 'alice/project.git'
```

Adenosine's SSH server does:

```text
TCP :22
  |
  v
SSH handshake
  |
  v
public-key authentication
  |
  | fingerprint -> account/DID
  v
session command
  |
  | parse command safely
  v
repository resolution
  |
  v
permission check
  |
  +---- read ----> git-upload-pack
  |
  +---- write ---> git-receive-pack
                      |
                      v
                bare repository
```

## SSH security rules

Never execute the received SSH command through a shell.

Do not do:

```go
exec.Command("sh", "-c", clientCommand)
```

Parse only an allow-list:

```text
git-upload-pack '<repo>'
git-receive-pack '<repo>'
git-upload-archive '<repo>'   # optional
```

Reject everything else.

Repository path input must resolve through the repository service, not direct filesystem interpolation.

---

# 14. SSH key model

Database:

```text
ssh_keys
--------
id
account_did
name
algorithm
public_key
fingerprint
created_at
last_used_at
revoked_at
```

On authentication:

```text
public key
   |
fingerprint
   |
lookup
   |
DID/account
   |
permission service
```

No generated `authorized_keys` file is necessary if the Go SSH server performs key lookup itself.

---

# 15. Push processing

Git push should have a very short synchronous critical path.

Synchronous:

1. Authenticate user.
2. Authorize write.
3. Run Git receive-pack.
4. Enforce mandatory ref rules.
5. Atomically update refs.
6. Return result to Git client.

Asynchronous:

1. Refresh branch metadata.
2. Refresh commit summaries.
3. Re-evaluate open PRs.
4. Publish push/activity records if appropriate.
5. Deliver webhooks.
6. Update search.
7. Generate notifications.
8. Schedule maintenance such as `git gc`.

Architecture:

```text
git push
   |
receive-pack
   |
refs updated
   |
write OutboxEvent in PostgreSQL
   |
return to client
   |
   +------------------------ asynchronous ---------------------+
                              |
                         event worker
                              |
            +-----------------+------------------+
            |                 |                  |
         webhooks          indexing          federation
```

Use a PostgreSQL outbox first instead of Kafka/NATS.

It is simpler and gives durable work without another required service.

---

# 16. Event/outbox model

Example table:

```sql
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0
);
```

Events:

```text
repository.created
repository.deleted
git.refs_updated
git.push_received
issue.created
issue.updated
pull_request.created
pull_request.updated
pull_request.merged
star.created
atproto.record_publish_requested
webhook.delivery_requested
```

Use `FOR UPDATE SKIP LOCKED` workers.

Do not introduce a dedicated distributed queue until it is actually required.

---

# 17. ATProto role

ATProto should not be in the Git transport hot path.

A Git clone must still work if:

- the Relay is down;
- Tap is restarting;
- the AppView is rebuilding;
- another Adenosine instance is unavailable.

For a locally hosted repository:

```text
Git source availability != ATProto availability
```

ATProto provides:

- owner identity;
- stable DID-based authorship;
- repository discovery;
- repository metadata;
- stars;
- issues;
- comments;
- pull-request metadata;
- reviews;
- cross-instance discovery.

---

# 18. Proposed Lexicons

Use a project-owned NSID domain.

Placeholder:

```text
dev.adenosine.*
```

Actual production NSIDs should use a domain controlled by the project.

Initial collections:

```text
dev.adenosine.profile
dev.adenosine.repo
dev.adenosine.star
dev.adenosine.issue
dev.adenosine.issue.comment
dev.adenosine.issue.status
dev.adenosine.pullRequest
dev.adenosine.pullRequest.status
dev.adenosine.review
```

Potential later collections:

```text
dev.adenosine.release
dev.adenosine.follow
dev.adenosine.fork
dev.adenosine.reaction
dev.adenosine.status
```

---

# 19. Repository ATProto record

Conceptual shape:

```json
{
  "$type": "dev.adenosine.repo",
  "name": "project",
  "description": "Example repository",
  "defaultBranch": "main",
  "git": {
    "https": "https://code.alice.dev/alice/project.git",
    "ssh": "git@code.alice.dev:alice/project.git"
  },
  "web": "https://code.alice.dev/alice/project",
  "createdAt": "2026-08-08T08:00:00Z"
}
```

The AT URI becomes the portable identity:

```text
at://did:plc:alice/dev.adenosine.repo/<rkey>
```

If Alice changes hosts:

```text
old:
https://forge-one.example/alice/project.git

new:
https://git.alice.dev/alice/project.git
```

the record can update its Git endpoints while retaining the same AT URI.

That is a central Adenosine feature.

---

# 20. Issue model

An issue should be authored in the creator's ATProto repository.

Conceptual record:

```json
{
  "$type": "dev.adenosine.issue",
  "subject": {
    "uri": "at://did:plc:alice/dev.adenosine.repo/abc",
    "cid": "..."
  },
  "title": "Clone fails over SSH",
  "body": "Steps to reproduce...",
  "createdAt": "..."
}
```

This means:

```text
repository owner = Alice
issue author     = Bob

Alice's PDS contains repository record.
Bob's PDS contains issue record referring to repository.
```

An AppView/indexer assembles:

```text
Repo
├── issue from Bob
├── issue from Carol
└── issue from Dave
```

---

# 21. Comments, stars, and reviews

Follow the same pattern.

A star is owned by the person starring:

```text
Bob's repo
└── star -> Alice's repo
```

A comment is owned by its author:

```text
Carol's repo
└── issue.comment -> Bob's issue
```

A review is owned by its reviewer:

```text
Dave's repo
└── review -> pull request
```

The local database is an indexed projection, not the canonical public source for remote records.

---

# 22. Pull request design

Pull requests are harder because Git data and federated metadata meet.

A PR should identify:

```text
target repository AT URI
target branch
source repository AT URI
source branch
head commit SHA
base commit / merge base where relevant
title
body
author
state
createdAt
```

Conceptually:

```json
{
  "$type": "dev.adenosine.pullRequest",
  "target": {
    "repo": "at://did:plc:alice/dev.adenosine.repo/abc",
    "branch": "main"
  },
  "source": {
    "repo": "at://did:plc:bob/dev.adenosine.repo/xyz",
    "branch": "fix/widget",
    "head": "5f31..."
  },
  "title": "Fix widget rendering",
  "body": "...",
  "createdAt": "..."
}
```

A PR is meaningful only if Adenosine can retrieve the source Git objects.

Therefore the source repository must expose a standard Git fetch endpoint.

---

# 23. Pull request network flow

Example:

```text
Alice hosts:
code.alice.dev/alice/server

Bob forks/hosts:
git.bob.dev/bob/server
```

Bob opens PR against Alice.

ATProto contains the PR record.

Alice's Adenosine instance indexes it.

When viewing the PR:

```text
Alice Adenosine
      |
      | resolve source repo AT record
      v
git.bob.dev/bob/server.git
      |
      | fetch required refs/objects
      v
local temporary PR object namespace
      |
      v
diff / merge analysis
```

Do not rely on browser-to-remote-host Git operations.

The target host should fetch source Git data server-to-server.

---

# 24. PR object storage

Avoid creating a normal local branch automatically.

Fetch source objects into controlled refs such as:

```text
refs/adenosine/pull/<pr-id>/head
```

Then calculate:

```bash
git merge-base <target> <head>
git diff <merge-base>..<head>
```

The target repository can retain the fetched objects while the PR is open.

This also makes a PR review resilient to transient source-host downtime.

Periodically fetch when the PR head changes.

---

# 25. Merge flow

Only the target host can authoritatively merge into the target Git repository.

Flow:

```text
User clicks Merge
      |
      v
HTTP API
      |
authenticate
      |
authorize target WRITE/MERGE
      |
load PR
      |
ensure latest target + source heads
      |
validate mergeability
      |
create merge / fast-forward / squash
      |
update target ref atomically
      |
emit git.refs_updated
      |
publish PR state update
```

Supported v1 merge types:

```text
merge commit
squash merge
rebase merge  # optional later
```

Start with merge commit + squash.

---

# 26. PR state ownership

There is an important federated-data design problem:

**Who is authoritative for PR state?**

The PR author owns the original PR record, but the target repository owner must be authoritative about whether it has been merged or rejected.

Do not let the author simply self-declare:

```text
merged: true
```

as authoritative.

A better model is:

```text
PR proposal record
    authored by PR creator

PR status/decision record
    authored by target repository owner/service
```

For example:

```text
dev.adenosine.pullRequest
dev.adenosine.pullRequest.status
```

The target repository controls status values like:

```text
open
merged
closed
rejected
```

This should be resolved in the Lexicon design before PR federation is considered stable.

---

# 27. ATProto ingestion

Do not implement the entire ATProto sync stack in v1.

Use Tap or an equivalent ATProto synchronization component to:

- consume relay/firehose events;
- verify repositories/events;
- backfill;
- filter to Adenosine collections;
- resume from cursors;
- expose simple events to `adenosine-indexer`.

Conceptually:

```text
ATProto Relay
     |
     v
    Tap
     |
     | filtered JSON/event stream
     v
adenosine-indexer
     |
     | validate Lexicon
     | resolve references
     | upsert projection
     v
PostgreSQL
```

Store the consumer cursor transactionally with projected updates where possible.

---

# 28. Federation projection tables

Keep local-host authoritative tables separate from federated projections where useful.

Example:

```text
repositories
local_repositories

federated_issues
federated_issue_comments
federated_pull_requests
federated_reviews
federated_stars
federation_cursor
```

Each indexed record should include:

```text
uri
cid
author_did
collection
rkey
record JSONB
indexed_at
created_at
deleted_at
```

Domain-specific columns can be projected for fast queries.

Example:

```text
federated_issues
----------------
uri
cid
author_did
subject_repo_uri
title
body
state
created_at
indexed_at
record_json
```

Keep raw validated record JSON initially. It makes protocol evolution and reindexing easier.

---

# 29. Repository discovery

When a user visits:

```text
https://adenosine.example/explore
```

the instance queries its local AppView projection.

It does not query every remote host in real time.

Flow:

```text
Remote user's PDS
       |
       v
ATProto network
       |
       v
Relay/Tap
       |
       v
Adenosine indexer
       |
       v
local PostgreSQL
       |
       v
Explore/Search UI
```

This gives fast local reads while preserving decentralized publication.

---

# 30. Remote repository page

Suppose a user visits an Adenosine instance that does not host the repository.

```text
viewer
  |
  v
adenosine.example/alice/project
  |
  v
local indexed repo metadata
```

Metadata can be rendered locally.

For source browsing there are two options.

## v1 recommendation

Redirect or proxy users toward the canonical host for detailed tree/blob browsing.

Show:

```text
Hosted on code.alice.dev
```

with links.

## Later

Implement read-through repository caching/mirroring:

```text
remote Git host
     |
     | git fetch
     v
local mirror cache
     |
     v
tree/blob/diff UI
```

Do not require every Adenosine instance to mirror every Git repository.

---

# 31. Optional public Git replication

A future feature can allow instances to opt into mirroring.

ATProto record could advertise:

```text
replicationAllowed: true
```

or the project policy can define public repos as mirrorable.

Then:

```text
canonical host
      |
      +------> mirror A
      |
      +------> mirror B
```

Repository metadata could advertise mirrors:

```text
canonical:
https://git.alice.dev/alice/project.git

mirrors:
https://mirror.example/...
```

Important: this is a later feature. Do not make replication a v1 requirement.

---

# 32. Identity, login, and shared developer profiles

Adenosine must **not create a separate network identity per instance**.

A user's ATProto DID is their Adenosine identity across the entire network.

```text
                         did:plc:tom
                             |
           +-----------------+-----------------+
           |                                   |
           v                                   v
    code.alice.dev                       git.bob.dev
      local session                        local session
           |                                   |
           +------------- same DID ------------+
```

A user may need to authenticate with two independently operated instances because browser cookies and local authorization are instance-specific, but this is **signing in twice, not signing up twice**.

There is no instance-owned username/password account for a normal user.

## 32.1 Identity invariant

```text
Global identity          = DID
Human-readable identity  = current ATProto handle
Public developer profile = ATProto record/profile projection
Browser session          = local instance state
Git credentials          = local instance credentials mapped to DID
Repository permissions   = local repository/host authorization
```

The DID is authoritative.

Handles are mutable and must never be primary keys or authorization identifiers.

## 32.2 Shared Adenosine profile

Define:

```text
dev.adenosine.profile
```

for Adenosine-specific public developer metadata that should follow a user across instances.

Conceptual record:

```json
{
  "$type": "dev.adenosine.profile",
  "displayName": "Tom Nagle",
  "bio": "Software engineer",
  "website": "https://example.com",
  "location": "Queensland, Australia",
  "createdAt": "..."
}
```

Reuse existing ATProto identity/profile fields where that is semantically appropriate rather than duplicating data merely to own it.

The Adenosine profile should contain only developer-network-specific fields that Adenosine actually needs.

Every instance indexes this profile and can display it without the user creating a local profile.

Updating the canonical profile once should eventually update every participating Adenosine AppView.

## 32.3 Local account row is a projection/relationship, not identity ownership

Each instance keeps a small local row keyed by DID:

```text
accounts
--------
did
handle_cache
created_at
first_seen_at
last_seen_at
last_login_at
```

This row means:

> This instance has seen/interacted with this global identity.

It does not mean:

> This instance owns this user's account.

Do not store locally authoritative copies of:

```text
username
password_hash
public bio
public avatar
public profile name
```

for normal ATProto users.

Profile data belongs in the network projection/cache and is refreshable/rebuildable.

## 32.4 Authentication

Authentication should use ATProto's supported OAuth/authentication flow.

```text
Browser
   |
Continue with ATProto
   |
ATProto authorization
   |
DID resolved
   |
local Adenosine session
```

The local session stores the minimum information needed to authenticate subsequent requests.

OAuth tokens/DPoP material or equivalent sensitive credentials must be encrypted at rest when persistence is required.

## 32.5 Git authentication

Git authentication remains instance-local because the Git host must authorize access to local repositories.

```text
ATProto DID
    |
    +--> SSH public keys on Instance A
    |
    +--> PAT/access tokens on Instance A
```

Another instance may require its own Git credential registration, but that credential maps to the **same DID**.

A later network-wide credential/delegation mechanism can be considered, but v1 should not publish SSH keys or access tokens into public ATProto records.

## 32.6 Network-wide contribution profile

Because public Adenosine records are authored by DIDs, an instance can build a contribution view from network data:

```text
@tom.example

Repositories
Issues opened
Pull requests
Reviews
Stars
Recent activity
```

The same DID should produce the same conceptual profile on any compatible Adenosine AppView, subject to indexing lag and local moderation policy.

Contribution counts are derived/indexed data, not fields a user can self-assert.

## 32.7 Authorization remains local

Global identity does not imply global permissions.

```text
did:plc:tom

Alice/repo-a -> write
Bob/repo-b   -> read
Carol/repo-c -> no special permission
```

Repository owners/hosts remain authoritative for write, merge, admin, and moderation permissions on resources they control.

# 33. Authorization

Authorization should be its own domain service.

Example permissions:

```text
repo.read
repo.write
repo.admin
repo.merge
issue.triage
```

For a v1 public repo:

```text
anonymous            read
owner                read/write/admin/merge
explicit collaborator read/write
```

Organizations can come later.

API:

```go
type Authorizer interface {
    Can(
        ctx context.Context,
        actor identity.DID,
        action Action,
        resource Resource,
    ) error
}
```

Git HTTP and SSH must call exactly the same authorization service.

---

# 34. Repository browser

The web UI needs:

```text
files/tree
blob viewer
README rendering
branches
tags
commit history
commit details
diffs
```

Initially use native Git.

### List tree

```bash
git ls-tree -z <rev>:<path>
```

### Blob

Prefer `git cat-file`.

For high request volume, maintain persistent:

```bash
git cat-file --batch
```

processes rather than spawning a process for every object read.

### History

```bash
git log
git rev-list
```

but produce machine-readable output and parse carefully.

Never parse human-localized Git output.

---

# 35. Git command execution rules

Create one central command runner.

Responsibilities:

- executable path;
- context cancellation;
- environment;
- timeout policy;
- stderr capture with limits;
- exit-code mapping;
- metrics;
- tracing;
- process-group cleanup;
- safe argument construction.

Never build shell strings.

Good:

```go
exec.CommandContext(ctx, "git", "ls-tree", "-z", rev)
```

Bad:

```go
exec.CommandContext(ctx, "sh", "-c", "git ls-tree "+rev)
```

Treat revision names and pathspecs as untrusted input.

Use `--` separators where applicable.

---

# 36. Public REST API

The REST API is a **first-class product surface**.

The official web app, future mobile clients, CLI, bots, integrations, alternative UIs, and third-party services must all be able to use the same public API.

## 36.1 API-first contract

Canonical specification:

```text
api/openapi.yaml
```

Use OpenAPI 3.x as the public contract.

Generate from it:

```text
Go request/response types
Go server interfaces
TypeScript types
TypeScript API client
interactive API reference
contract tests
```

The source of truth must not be ad-hoc handler annotations.

CI:

```text
edit api/openapi.yaml
        |
        +--> lint
        +--> generate Go
        +--> generate TypeScript
        +--> generate docs
        +--> verify generated files are clean
        +--> run contract tests
```

Use `oapi-codegen` or an equivalent mature generator for the Go edge.

## 36.2 API surface

Example:

```text
GET    /api/v1/me

POST   /api/v1/repositories
GET    /api/v1/repositories/{owner}/{repo}
PATCH  /api/v1/repositories/{owner}/{repo}
DELETE /api/v1/repositories/{owner}/{repo}

GET    /api/v1/repositories/{owner}/{repo}/tree
GET    /api/v1/repositories/{owner}/{repo}/blobs/{sha}
GET    /api/v1/repositories/{owner}/{repo}/commits
GET    /api/v1/repositories/{owner}/{repo}/branches
GET    /api/v1/repositories/{owner}/{repo}/tags

GET    /api/v1/repositories/{owner}/{repo}/issues
POST   /api/v1/repositories/{owner}/{repo}/issues
GET    /api/v1/repositories/{owner}/{repo}/issues/{issue}

GET    /api/v1/repositories/{owner}/{repo}/pulls
POST   /api/v1/repositories/{owner}/{repo}/pulls
GET    /api/v1/repositories/{owner}/{repo}/pulls/{pull}
POST   /api/v1/repositories/{owner}/{repo}/pulls/{pull}/merge
POST   /api/v1/repositories/{owner}/{repo}/pulls/{pull}/reviews

GET    /api/v1/repositories/{owner}/{repo}/stars
PUT    /api/v1/repositories/{owner}/{repo}/star
DELETE /api/v1/repositories/{owner}/{repo}/star

GET    /api/v1/network/repositories
GET    /api/v1/network/activity
GET    /api/v1/search/repositories

GET    /api/v1/ssh-keys
POST   /api/v1/ssh-keys
DELETE /api/v1/ssh-keys/{id}

GET    /api/v1/tokens
POST   /api/v1/tokens
DELETE /api/v1/tokens/{id}

GET    /api/v1/webhooks
POST   /api/v1/webhooks
```

Remote federated repositories use the same resource representations as local repositories, with location metadata:

```json
{
  "uri": "at://did:plc:alice/dev.adenosine.repo/abc",
  "name": "project",
  "owner": {
    "did": "did:plc:alice",
    "handle": "alice.example"
  },
  "hosting": {
    "local": false,
    "web_url": "https://code.alice.dev/alice/project",
    "git_https_url": "https://code.alice.dev/alice/project.git",
    "git_ssh_url": "git@code.alice.dev:alice/project.git"
  },
  "stats": {
    "stars": 842,
    "open_issues": 12,
    "open_pull_requests": 3
  }
}
```

## 36.3 Documentation on every instance

Every instance exposes:

```text
/docs/api
/openapi.json
```

Optionally:

```text
/openapi.yaml
```

Documentation includes:

- authentication;
- pagination;
- filters/sorting;
- rate limits;
- API versioning;
- error codes;
- idempotency;
- federation semantics;
- eventual consistency;
- webhooks/signatures;
- curl examples;
- generated client examples.

A self-hoster gets documentation matching the exact installed release.

## 36.4 Stable error envelope

```json
{
  "error": {
    "code": "repository_not_found",
    "message": "Repository was not found",
    "request_id": "req_...",
    "details": {}
  }
}
```

Clients depend on `code`, never message text.

## 36.5 Cursor pagination

Network-scale collections use cursor pagination:

```text
GET /api/v1/network/repositories?limit=50&cursor=...
```

```json
{
  "data": [],
  "page": {
    "next_cursor": "..."
  }
}
```

## 36.6 Versioning

Start with:

```text
/api/v1
```

Rules:

- additive fields are allowed;
- removal/rename requires a new version or formal deprecation;
- supported OpenAPI specs remain available;
- REST API versions and ATProto Lexicon evolution are separate concerns.

## 36.7 All writes go through REST

The official web app performs application mutations through REST:

```text
repository creation
issue/comment creation
PR creation
review
merge
star/unstar
SSH-key management
token management
webhook management
```

Do not hide core product mutations inside TanStack Start server functions.

If a server function exists for frontend orchestration, it cannot have privileges or capabilities unavailable through the documented API.

## 36.8 Authentication

Support:

```text
browser OAuth/session
personal access tokens
future OAuth application/service tokens
```

The official web app is not privileged.

## 36.9 Idempotency

Support `Idempotency-Key` for important retryable operations, especially:

```text
repository creation
PR merge
webhook creation
other externally retried writes
```

## 36.10 Contract tests

Run black-box tests against:

```text
development server
Docker image
release candidate
```

The suite should interact only through the documented HTTP contract.

# 37. Web application — TanStack Start + realtime

The official web application is a standalone TypeScript client built with:

```text
TanStack Start
TanStack Router
TanStack Query
TanStack DB
Electric
React
```

TanStack Start provides the app shell, routing, SSR/streaming, and server/frontend build model.

TanStack Query handles conventional request/response server state.

TanStack DB provides reactive client collections and live queries.

Electric-backed TanStack DB collections provide realtime data for PostgreSQL-backed projections.

## 37.1 Hard boundary

The web app has no direct database access and no privileged Go interface.

Allowed data interfaces:

```text
public REST API
documented Electric/sync endpoints
ATProto/browser OAuth where required
```

If the official UI can do something a third-party UI cannot do through public interfaces, fix the API.

## 37.2 Use TanStack Query for

```text
Git tree/blob reads
commit history
large diffs
downloads
admin/configuration operations
REST mutations
non-realtime endpoints
```

Do not copy Git object data into Postgres just to make it realtime.

## 37.3 Use TanStack DB for

```text
repository metadata
network discovery
issues
comments
pull requests
reviews
stars
notifications
activity
branch metadata projections
```

Use Electric-backed collections when the underlying projection lives in Postgres and realtime materially improves the UI.

TanStack DB query collections can be used as an incremental REST-backed step for resources that do not yet use Electric.

## 37.4 Realtime write/read flow

```text
                  WRITE
React UI
   |
optimistic TanStack DB mutation
   |
generated REST client
   |
POST/PATCH/DELETE /api/v1/...
   |
Go application service
   |
Postgres transaction
   |
commit
   |
   +--------------------------------+
                                    |
                                 Electric
                                    |
                               allowed Shape
                                    |
                                    v
                        TanStack DB collection
                                    |
                         optimistic reconciliation
                                    |
                                    v
                              React live query
```

REST is authoritative for writes. Electric is the synchronized read path.

## 37.5 Federation appears realtime

```text
remote star/issue/PR/review
          |
          v
       ATProto
          |
          v
         Tap
          |
          v
       indexer
          |
          v
   local Postgres projection
          |
          v
       Electric
          |
          v
      browser
```

A federated application can therefore feel realtime without inventing a custom realtime federation protocol.

## 37.6 Authenticated sync endpoints

Do not expose unrestricted Electric access.

Adenosine exposes documented sync endpoints such as:

```text
GET /api/v1/sync/repositories
GET /api/v1/sync/issues
GET /api/v1/sync/pull-requests
GET /api/v1/sync/stars
GET /api/v1/sync/activity
```

The server:

1. authenticates if required;
2. chooses/validates an allowed Shape;
3. enforces visibility/moderation policy;
4. signs/proxies the Electric request;
5. never exposes database credentials to the browser.

Public network shapes may allow anonymous reads but remain predefined and constrained.

## 37.7 REST-only clients remain first-class

Electric is optional for clients.

A third-party UI may simply use:

```text
GET /api/v1/network/repositories
GET /api/v1/repositories/.../issues
POST /api/v1/repositories/.../issues
```

and refetch/poll.

Therefore:

```text
Electric unavailable != REST unavailable
```

and:

```text
not using TanStack DB != second-class client
```

## 37.8 Optimistic updates

For a star:

```text
click Star
   |
optimistic collection update
   |
PUT REST /star
   |
Postgres commits
   |
Electric emits committed state
   |
collection reconciles
```

Apply the same approach to comments, issues, and PR actions where safe.

## 37.9 SSR

TanStack Start can SSR:

```text
repository landing pages
README
issue pages
PR pages
explore/search
```

SSR should call the same generated public API client where practical.

After hydration, TanStack DB/Electric provides live updates.

## 37.10 Generated public client

`packages/api-client` is generated from `api/openapi.yaml`.

The official web app imports it:

```ts
import { createAdenosineClient } from "@adenosine/api-client"
```

The same package can be published for third-party developers.

Do not scatter handwritten `fetch("/api/...")` calls throughout the web app.

## 37.11 Feature layout

```text
web/src/
├── routes/
├── features/
│   └── repositories/
│       ├── components/
│       ├── queries.ts
│       ├── mutations.ts
│       └── collections.ts
├── api/
│   └── client.ts
├── db/
│   ├── collections/
│   └── schema.ts
└── sync/
```

# 38. PostgreSQL database schema

Use PostgreSQL as:

1. the authoritative store for **local operational/application state**;
2. the queryable **projection of the public Adenosine network**;
3. the durable queue/outbox for asynchronous local work.

PostgreSQL is **not** the storage location for Git objects.

Use:

```text
pgx
sqlc
plain SQL migrations
```

Generate UUIDv7 IDs in Go so the application does not depend on a particular PostgreSQL version for UUIDv7 support.

Use:

```text
UUID        local immutable IDs
TEXT        DID / AT URI / CID / handles / slugs
TIMESTAMPTZ every timestamp
JSONB       validated raw federated record snapshots / payloads
BYTEA       only where genuinely binary
```

Prefer `TEXT + CHECK` for evolving application states rather than PostgreSQL enums that make rolling/self-hosted upgrades more awkward.

## 38.1 PostgreSQL schema separation

Use PostgreSQL schemas to make trust boundaries visible:

```text
auth.*       secrets, sessions, credentials
core.*       authoritative local forge state
network.*    public/rebuildable ATProto projections
ops.*        outbox, webhooks, cursors, maintenance
```

Electric should receive a database role with access only to explicitly safe tables/views.

It must never have SELECT access to:

```text
auth.sessions
auth.access_tokens
auth.oauth_credentials
auth.ssh_keys private material if any
webhook secrets
instance secrets
```

A useful role model:

```text
adenosine_app       normal application DB role
adenosine_migrate   migration/DDL role
adenosine_sync      restricted Electric read role
adenosine_backup    backup role
```

Initial migration:

```sql
CREATE SCHEMA auth;
CREATE SCHEMA core;
CREATE SCHEMA network;
CREATE SCHEMA ops;
```

`core.*` and `auth.*` use normal relational foreign keys aggressively.

`network.*` is different: **do not require hard foreign keys between federated AT URIs**. ATProto records may arrive out of order, be temporarily unavailable, be hidden by local moderation, or be replayed during rebuilds. The projector validates semantic relationships and reconciliation repairs unresolved references.

This allows:

```text
issue arrives before repository projection
PR arrives before source repository projection
review arrives before PR backfill
```

without rejecting valid federation events.

## 38.2 Extensions

Keep required extensions minimal.

If an extension is optional, Adenosine should still run without it.

Postgres full-text search is available without requiring an external search service.

If `pg_trgm` is used for fuzzy repository/handle lookup, deployment scripts must install/check it explicitly.

## 38.3 Identity/auth tables

### `core.accounts`

A local relationship/cache for a globally owned DID.

```sql
CREATE TABLE core.accounts (
    did             TEXT PRIMARY KEY,
    handle_cache    TEXT,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT accounts_did_nonempty CHECK (length(did) > 0)
);

CREATE INDEX accounts_handle_cache_idx
    ON core.accounts (lower(handle_cache));
```

Do **not** store a locally authoritative bio/avatar/display name here.

Those belong to the network profile projection.

### `auth.sessions`

```sql
CREATE TABLE auth.sessions (
    id                  UUID PRIMARY KEY,
    account_did         TEXT NOT NULL
                            REFERENCES core.accounts(did)
                            ON DELETE CASCADE,
    token_hash          BYTEA NOT NULL UNIQUE,
    created_at          TIMESTAMPTZ NOT NULL,
    expires_at          TIMESTAMPTZ NOT NULL,
    last_seen_at        TIMESTAMPTZ,
    revoked_at          TIMESTAMPTZ,
    user_agent_hash     BYTEA,
    ip_prefix           TEXT
);

CREATE INDEX sessions_account_active_idx
    ON auth.sessions (account_did, expires_at)
    WHERE revoked_at IS NULL;
```

Store only a hash of the browser session token.

### `auth.oauth_credentials`

Only when ATProto OAuth material must persist.

```sql
CREATE TABLE auth.oauth_credentials (
    account_did             TEXT PRIMARY KEY
                                REFERENCES core.accounts(did)
                                ON DELETE CASCADE,
    pds_url                 TEXT NOT NULL,
    issuer                  TEXT,
    scopes                  TEXT[] NOT NULL DEFAULT '{}',
    credential_ciphertext   BYTEA NOT NULL,
    key_version             INTEGER NOT NULL,
    expires_at              TIMESTAMPTZ,
    updated_at              TIMESTAMPTZ NOT NULL
);
```

The encrypted blob may contain refresh/access/DPoP material as required by the chosen ATProto OAuth implementation.

Encryption keys are instance secrets and never live in this table.

### `auth.oauth_states`

Short-lived login/PKCE state.

```sql
CREATE TABLE auth.oauth_states (
    id              UUID PRIMARY KEY,
    state_hash      BYTEA NOT NULL UNIQUE,
    payload         JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX oauth_states_expiry_idx
    ON auth.oauth_states (expires_at);
```

Expired rows are routinely deleted.

### `auth.ssh_keys`

```sql
CREATE TABLE auth.ssh_keys (
    id              UUID PRIMARY KEY,
    account_did     TEXT NOT NULL
                        REFERENCES core.accounts(did)
                        ON DELETE CASCADE,
    name            TEXT NOT NULL,
    algorithm       TEXT NOT NULL,
    public_key      TEXT NOT NULL,
    fingerprint     TEXT NOT NULL UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL,
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX ssh_keys_account_idx
    ON auth.ssh_keys (account_did)
    WHERE revoked_at IS NULL;
```

Adenosine stores public user SSH keys, never private user keys.

### `auth.access_tokens`

```sql
CREATE TABLE auth.access_tokens (
    id              UUID PRIMARY KEY,
    account_did     TEXT NOT NULL
                        REFERENCES core.accounts(did)
                        ON DELETE CASCADE,
    name            TEXT NOT NULL,
    token_prefix    TEXT NOT NULL,
    token_hash      BYTEA NOT NULL UNIQUE,
    scopes          TEXT[] NOT NULL,
    repository_id   UUID,
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX access_tokens_account_idx
    ON auth.access_tokens (account_did)
    WHERE revoked_at IS NULL;
```

`repository_id` is an optional restriction and receives its FK after `core.repositories` exists.

Never persist the plaintext token after creation.

## 38.4 Local repository tables

### `core.repositories`

This table exists only for repositories physically/authoritatively hosted by this instance.

```sql
CREATE TABLE core.repositories (
    id                  UUID PRIMARY KEY,
    owner_did           TEXT NOT NULL
                            REFERENCES core.accounts(did),
    slug                TEXT NOT NULL,
    display_name        TEXT,
    description         TEXT,
    visibility          TEXT NOT NULL DEFAULT 'public',
    state               TEXT NOT NULL DEFAULT 'creating',
    default_branch      TEXT NOT NULL DEFAULT 'main',

    storage_key         TEXT NOT NULL UNIQUE,

    at_uri              TEXT UNIQUE,
    at_cid              TEXT,

    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT repositories_slug_format
        CHECK (slug ~ '^[a-z0-9][a-z0-9._-]*$'),

    CONSTRAINT repositories_visibility
        CHECK (visibility IN ('public', 'private')),

    CONSTRAINT repositories_state
        CHECK (state IN ('creating', 'active', 'failed', 'deleting', 'deleted'))
);

CREATE UNIQUE INDEX repositories_owner_slug_active_uidx
    ON core.repositories (owner_did, lower(slug))
    WHERE deleted_at IS NULL;

CREATE INDEX repositories_owner_idx
    ON core.repositories (owner_did)
    WHERE deleted_at IS NULL;

CREATE INDEX repositories_at_uri_idx
    ON core.repositories (at_uri)
    WHERE at_uri IS NOT NULL;
```

`owner_did + slug` is a route/alias, not identity.

`id` is the local immutable identity.

`at_uri` is the public network identity for a federated public repository.

### `core.repository_aliases`

Preserve redirects after rename/handle changes.

```sql
CREATE TABLE core.repository_aliases (
    id              UUID PRIMARY KEY,
    repository_id   UUID NOT NULL
                        REFERENCES core.repositories(id)
                        ON DELETE CASCADE,
    owner_alias     TEXT NOT NULL,
    slug_alias      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX repository_alias_lookup_uidx
    ON core.repository_aliases (lower(owner_alias), lower(slug_alias));
```

### `core.repository_collaborators`

```sql
CREATE TABLE core.repository_collaborators (
    repository_id   UUID NOT NULL
                        REFERENCES core.repositories(id)
                        ON DELETE CASCADE,
    account_did     TEXT NOT NULL
                        REFERENCES core.accounts(did)
                        ON DELETE CASCADE,
    role            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (repository_id, account_did),

    CONSTRAINT repository_collaborator_role
        CHECK (role IN ('read', 'write', 'maintain', 'admin'))
);

CREATE INDEX repository_collaborators_account_idx
    ON core.repository_collaborators (account_did);
```

The repository owner does not need a collaborator row; ownership implies admin.

### Access-token FK

After repository creation:

```sql
ALTER TABLE auth.access_tokens
ADD CONSTRAINT access_tokens_repository_fk
FOREIGN KEY (repository_id)
REFERENCES core.repositories(id)
ON DELETE CASCADE;
```

## 38.5 Local Git projection/cache tables

Git remains authoritative for refs and objects.

These tables are disposable caches that make UI/API queries cheaper.

### `core.branch_cache`

```sql
CREATE TABLE core.branch_cache (
    repository_id   UUID NOT NULL
                        REFERENCES core.repositories(id)
                        ON DELETE CASCADE,
    name            TEXT NOT NULL,
    commit_sha      TEXT NOT NULL,
    protected       BOOLEAN NOT NULL DEFAULT false,
    updated_at      TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (repository_id, name)
);

CREATE INDEX branch_cache_repository_updated_idx
    ON core.branch_cache (repository_id, updated_at DESC);
```

### `core.repository_stats`

Derived local/network counters for fast pages.

```sql
CREATE TABLE core.repository_stats (
    repository_id       UUID PRIMARY KEY
                            REFERENCES core.repositories(id)
                            ON DELETE CASCADE,
    size_bytes          BIGINT NOT NULL DEFAULT 0,
    branch_count        INTEGER NOT NULL DEFAULT 0,
    tag_count           INTEGER NOT NULL DEFAULT 0,
    last_push_at        TIMESTAMPTZ,
    updated_at          TIMESTAMPTZ NOT NULL
);
```

Do not turn this into canonical Git state.

## 38.6 Raw ATProto records

### `network.records`

Keep the validated raw record snapshot so projections can be rebuilt/evolved.

```sql
CREATE TABLE network.records (
    uri             TEXT PRIMARY KEY,
    cid             TEXT,
    author_did      TEXT NOT NULL,
    collection      TEXT NOT NULL,
    rkey            TEXT NOT NULL,
    record          JSONB,
    created_at      TIMESTAMPTZ,
    indexed_at      TIMESTAMPTZ NOT NULL,
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX network_records_author_idx
    ON network.records (author_did, collection);

CREATE INDEX network_records_collection_idx
    ON network.records (collection, indexed_at DESC);

CREATE INDEX network_records_active_idx
    ON network.records (collection)
    WHERE deleted_at IS NULL;
```

On deletion, remove or null the raw content according to moderation/privacy policy while retaining the minimum tombstone required to process network state safely.

## 38.7 Shared profile projection

### `network.profiles`

One network-visible developer identity per DID.

```sql
CREATE TABLE network.profiles (
    did                 TEXT PRIMARY KEY,
    profile_uri         TEXT UNIQUE,
    profile_cid         TEXT,

    handle              TEXT,
    display_name        TEXT,
    bio                 TEXT,
    avatar_ref          TEXT,
    website             TEXT,
    location            TEXT,

    repository_count    BIGINT NOT NULL DEFAULT 0,
    contribution_count  BIGINT NOT NULL DEFAULT 0,

    record_created_at   TIMESTAMPTZ,
    indexed_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX network_profiles_handle_idx
    ON network.profiles (lower(handle));

CREATE INDEX network_profiles_display_name_idx
    ON network.profiles (lower(display_name));
```

`repository_count` and `contribution_count` are derived local AppView counters.

They are not trusted from the user's profile record.

## 38.8 Network repository projection

### `network.repositories`

This is the primary **network-wide discovery table**.

It contains local and remote public repositories using one representation.

```sql
CREATE TABLE network.repositories (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    owner_did           TEXT NOT NULL,
    slug                TEXT NOT NULL,
    name                TEXT,
    description         TEXT,
    default_branch      TEXT,

    web_url             TEXT NOT NULL,
    git_https_url       TEXT,
    git_ssh_url         TEXT,

    local_repository_id UUID
                            REFERENCES core.repositories(id)
                            ON DELETE SET NULL,

    star_count          BIGINT NOT NULL DEFAULT 0,
    open_issue_count    BIGINT NOT NULL DEFAULT 0,
    open_pr_count       BIGINT NOT NULL DEFAULT 0,

    record_created_at   TIMESTAMPTZ,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX network_repositories_owner_idx
    ON network.repositories (owner_did, lower(slug))
    WHERE deleted_at IS NULL;

CREATE INDEX network_repositories_local_idx
    ON network.repositories (local_repository_id)
    WHERE local_repository_id IS NOT NULL;

CREATE INDEX network_repositories_stars_idx
    ON network.repositories (star_count DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX network_repositories_recent_idx
    ON network.repositories (record_created_at DESC)
    WHERE deleted_at IS NULL;
```

A local public repo is projected here after its ATProto repository record exists.

That means Explore/Search/API code can query one table for the network rather than special-case local versus remote repositories.

## 38.9 Network issue projection

### `network.issues`

```sql
CREATE TABLE network.issues (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    subject_repo_uri    TEXT NOT NULL,
    title               TEXT NOT NULL,
    body                 TEXT NOT NULL,
    state               TEXT NOT NULL DEFAULT 'open',
    comment_count       BIGINT NOT NULL DEFAULT 0,

    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT network_issue_state
        CHECK (state IN ('open', 'closed'))
);

CREATE INDEX network_issues_repo_state_idx
    ON network.issues (subject_repo_uri, state, record_created_at DESC);

CREATE INDEX network_issues_author_idx
    ON network.issues (author_did, record_created_at DESC);
```

The projected `state` is the AppView's resolved authoritative state, potentially combining the author's proposal with target-repository decision records.

### `network.issue_status_records`

Keep maintainer/target-authored status assertions separately.

```sql
CREATE TABLE network.issue_status_records (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    issue_uri           TEXT NOT NULL,
    author_did          TEXT NOT NULL,
    state               TEXT NOT NULL,
    reason              TEXT,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT issue_status_state
        CHECK (state IN ('open', 'closed'))
);

CREATE INDEX issue_status_records_issue_idx
    ON network.issue_status_records (issue_uri, record_created_at DESC);
```

Only target-authorized status records affect the resolved issue state.

### `network.issue_comments`

```sql
CREATE TABLE network.issue_comments (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    issue_uri           TEXT NOT NULL,
    author_did          TEXT NOT NULL,
    body                 TEXT NOT NULL,
    reply_to_uri        TEXT,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX issue_comments_issue_idx
    ON network.issue_comments (issue_uri, record_created_at);

CREATE INDEX issue_comments_author_idx
    ON network.issue_comments (author_did, record_created_at DESC);
```

## 38.10 Pull request projection

### `network.pull_requests`

```sql
CREATE TABLE network.pull_requests (
    uri                     TEXT PRIMARY KEY,
    cid                     TEXT,
    author_did              TEXT NOT NULL,

    target_repo_uri         TEXT NOT NULL,
    target_branch           TEXT NOT NULL,

    source_repo_uri         TEXT NOT NULL,
    source_branch           TEXT NOT NULL,
    head_sha                TEXT NOT NULL,
    proposed_base_sha       TEXT,

    title                   TEXT NOT NULL,
    body                    TEXT NOT NULL,
    state                   TEXT NOT NULL DEFAULT 'open',

    review_count            BIGINT NOT NULL DEFAULT 0,
    comment_count           BIGINT NOT NULL DEFAULT 0,

    record_created_at       TIMESTAMPTZ NOT NULL,
    indexed_at              TIMESTAMPTZ NOT NULL,
    deleted_at              TIMESTAMPTZ,

    CONSTRAINT pull_request_state
        CHECK (state IN ('open', 'merged', 'closed', 'rejected'))
);

CREATE INDEX pull_requests_target_state_idx
    ON network.pull_requests (target_repo_uri, state, record_created_at DESC);

CREATE INDEX pull_requests_source_idx
    ON network.pull_requests (source_repo_uri, source_branch);

CREATE INDEX pull_requests_author_idx
    ON network.pull_requests (author_did, record_created_at DESC);
```

`state` is a resolved AppView field, not blindly trusted from the PR author's record.

### `network.pull_request_status_records`

```sql
CREATE TABLE network.pull_request_status_records (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    pull_request_uri    TEXT NOT NULL,
    author_did          TEXT NOT NULL,
    state               TEXT NOT NULL,
    merged_sha          TEXT,
    reason              TEXT,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT pull_request_status_state
        CHECK (state IN ('open', 'merged', 'closed', 'rejected'))
);

CREATE INDEX pull_request_status_pr_idx
    ON network.pull_request_status_records
       (pull_request_uri, record_created_at DESC);
```

Only a status record authorized by the target repository affects authoritative target state.

### `network.reviews`

```sql
CREATE TABLE network.reviews (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    pull_request_uri    TEXT NOT NULL,
    author_did          TEXT NOT NULL,
    verdict             TEXT NOT NULL,
    body                 TEXT,
    commit_sha          TEXT,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT review_verdict
        CHECK (verdict IN ('comment', 'approve', 'request_changes'))
);

CREATE INDEX reviews_pr_idx
    ON network.reviews (pull_request_uri, record_created_at);

CREATE INDEX reviews_author_idx
    ON network.reviews (author_did, record_created_at DESC);
```

### `core.pull_request_fetches`

Operational state for PRs targeting locally hosted repositories.

```sql
CREATE TABLE core.pull_request_fetches (
    pull_request_uri        TEXT PRIMARY KEY,
    target_repository_id   UUID NOT NULL
                                REFERENCES core.repositories(id)
                                ON DELETE CASCADE,
    fetched_head_sha       TEXT,
    local_ref              TEXT,
    source_git_url         TEXT,
    last_fetched_at        TIMESTAMPTZ,
    last_error_code        TEXT,
    updated_at             TIMESTAMPTZ NOT NULL
);
```

This is operational cache/state, not public PR truth.

## 38.11 Stars

### `network.stars`

```sql
CREATE TABLE network.stars (
    uri                 TEXT PRIMARY KEY,
    cid                 TEXT,
    author_did          TEXT NOT NULL,
    repository_uri      TEXT NOT NULL,
    record_created_at   TIMESTAMPTZ NOT NULL,
    indexed_at          TIMESTAMPTZ NOT NULL,
    deleted_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX stars_one_active_per_user_repo_uidx
    ON network.stars (author_did, repository_uri)
    WHERE deleted_at IS NULL;

CREATE INDEX stars_repo_idx
    ON network.stars (repository_uri, record_created_at DESC);

CREATE INDEX stars_author_idx
    ON network.stars (author_did, record_created_at DESC);
```

Repository star counts are updated transactionally by the projector or rebuilt from this table.

## 38.12 Outbox and workers

### `ops.outbox_events`

```sql
CREATE TABLE ops.outbox_events (
    id                  UUID PRIMARY KEY,
    type                TEXT NOT NULL,
    aggregate_type      TEXT NOT NULL,
    aggregate_id        TEXT NOT NULL,
    payload             JSONB NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL,
    available_at        TIMESTAMPTZ NOT NULL,
    claimed_at          TIMESTAMPTZ,
    claimed_by          TEXT,
    completed_at        TIMESTAMPTZ,
    attempts            INTEGER NOT NULL DEFAULT 0,
    last_error_code     TEXT,

    CONSTRAINT outbox_attempts_nonnegative
        CHECK (attempts >= 0)
);

CREATE INDEX outbox_ready_idx
    ON ops.outbox_events (available_at, created_at)
    WHERE completed_at IS NULL;
```

Workers claim batches using `FOR UPDATE SKIP LOCKED`.

Do not hold the DB transaction open while performing external network work.

Claim/update state, commit, perform the work, then record completion/retry.

## 38.13 Federation cursors

### `ops.federation_cursors`

```sql
CREATE TABLE ops.federation_cursors (
    consumer            TEXT PRIMARY KEY,
    cursor              TEXT NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);
```

Cursor advancement and projection writes should be coordinated so crashes produce replay rather than missed records.

Handlers/projectors must therefore be idempotent.

## 38.14 Webhooks

### `core.webhooks`

```sql
CREATE TABLE core.webhooks (
    id                  UUID PRIMARY KEY,
    repository_id       UUID NOT NULL
                            REFERENCES core.repositories(id)
                            ON DELETE CASCADE,
    url                 TEXT NOT NULL,
    secret_ciphertext   BYTEA NOT NULL,
    key_version         INTEGER NOT NULL,
    event_types         TEXT[] NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX webhooks_repository_idx
    ON core.webhooks (repository_id)
    WHERE enabled = true;
```

### `ops.webhook_deliveries`

```sql
CREATE TABLE ops.webhook_deliveries (
    id                  UUID PRIMARY KEY,
    webhook_id          UUID NOT NULL
                            REFERENCES core.webhooks(id)
                            ON DELETE CASCADE,
    event_id            UUID,
    attempt             INTEGER NOT NULL,
    request_body        JSONB NOT NULL,
    response_status     INTEGER,
    response_body       TEXT,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    next_attempt_at     TIMESTAMPTZ,
    error_code          TEXT
);

CREATE INDEX webhook_deliveries_retry_idx
    ON ops.webhook_deliveries (next_attempt_at)
    WHERE completed_at IS NULL;
```

Limit stored response body size.

## 38.15 Moderation

A local AppView must be able to hide abusive network content without mutating the source ATProto record.

### `core.moderation_rules`

```sql
CREATE TABLE core.moderation_rules (
    id              UUID PRIMARY KEY,
    subject_type    TEXT NOT NULL,
    subject         TEXT NOT NULL,
    action          TEXT NOT NULL,
    reason          TEXT,
    created_by_did  TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ,

    CONSTRAINT moderation_subject_type
        CHECK (subject_type IN ('did', 'record', 'repository')),

    CONSTRAINT moderation_action
        CHECK (action IN ('hide', 'block'))
);

CREATE INDEX moderation_subject_idx
    ON core.moderation_rules (subject_type, subject);
```

## 38.16 Electric-safe projections

Do not point Electric at arbitrary application tables.

Expose a deliberate realtime surface.

Options:

```text
network.repositories
network.profiles
network.issues
network.issue_comments
network.pull_requests
network.reviews
network.stars
```

plus explicitly designed safe local projections.

If joins/derived authorization make direct table shapes awkward, create dedicated projection tables rather than leaking sensitive tables.

The `adenosine_sync` role receives only the minimum privileges required:

```sql
GRANT USAGE ON SCHEMA network TO adenosine_sync;

GRANT SELECT ON
    network.profiles,
    network.repositories,
    network.issues,
    network.issue_comments,
    network.pull_requests,
    network.reviews,
    network.stars
TO adenosine_sync;
```

The HTTP sync proxy still applies local visibility/moderation constraints.

Database grants are defense in depth, not the entire authorization model.

## 38.17 Search indexes

Start with Postgres.

For repository search, add generated/searchable text or appropriate indexes based on profiling.

If `pg_trgm` is enabled:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX network_repositories_slug_trgm_idx
    ON network.repositories
    USING gin (slug gin_trgm_ops);

CREATE INDEX network_profiles_handle_trgm_idx
    ON network.profiles
    USING gin (handle gin_trgm_ops);
```

Keep this migration/configuration explicit because self-hosted Postgres providers differ.

## 38.18 Deletion strategy

Use hard deletion only where safe.

Local repositories:

```text
active -> deleting -> quarantined storage -> deleted
```

Federated records:

```text
record delete event
      |
projector
      |
deleted_at / tombstone
      |
derived counts updated
```

Auth credentials should be actually removed or irreversibly revoked according to their security semantics rather than retained as soft-deleted secrets forever.

## 38.19 Schema invariants

1. A DID is the global human identity.
2. No local numeric/user ID substitutes for DID in public authorship.
3. Handles are caches/aliases, never authorization keys.
4. Local repository UUID is immutable.
5. Public repository AT URI is network identity.
6. Git refs/objects are never canonicalized into Postgres.
7. `network.*` is rebuildable projection state.
8. `auth.*` is never exposed through Electric.
9. Secrets/tokens are hashed or encrypted, never plaintext.
10. Federation handlers are idempotent.
11. Every frequently used FK/filter/order path has an intentional index.
12. `created_at` is not overloaded as `updated_at`.
13. Timestamps are UTC `TIMESTAMPTZ`.
14. Application-generated UUIDv7 avoids DB-sequence coupling and improves index locality.
15. Schema changes are forward-compatible with self-hosted upgrades wherever practical.
16. `network.*` relationships are represented by AT URI values and resolved by projectors; ingestion does not depend on cross-record FK arrival order.
17. `core.*` must never depend on a rebuildable `network.*` row for its own durable existence.

## 38.20 Migrations and sqlc layout

```text
migrations/
├── 000001_schemas.sql
├── 000002_identity.sql
├── 000003_repositories.sql
├── 000004_network_records.sql
├── 000005_network_repositories.sql
├── 000006_issues.sql
├── 000007_pull_requests.sql
├── 000008_stars.sql
├── 000009_outbox.sql
└── ...

internal/database/
├── db.go
├── tx.go
├── queries/
│   ├── accounts.sql
│   ├── repositories.sql
│   ├── network_repositories.sql
│   ├── issues.sql
│   ├── pull_requests.sql
│   └── ...
└── generated/        # sqlc output
```

Prefer handwritten SQL + generated typed access through `sqlc` over a reflection-heavy ORM.

Repository/domain stores adapt generated query types to domain types.

Generated sqlc models must not leak into the REST contract or domain API.

# 39. Repository creation flow

User:

```text
New repository
name = project
```

Server:

```text
POST /api/v1/repos
       |
       v
authenticate ATProto DID
       |
       v
validate slug
       |
       v
begin DB transaction
       |
       +--> create repository record
       |
       +--> allocate storage key
       |
       v
commit DB
       |
       v
git init --bare
       |
       v
publish dev.adenosine.repo ATProto record
       |
       v
store AT URI/CID
```

Failure handling matters.

The DB/storage/ATProto operations cannot be one distributed transaction.

Use states:

```text
creating
active
failed
deleting
```

and retryable background jobs.

---

# 40. Existing repository onboarding

Support:

```bash
git remote add adenosine git@code.example.com:alice/project.git
git push -u adenosine --all
git push adenosine --tags
```

The web UI should show this after repository creation.

Also provide:

```bash
git remote set-url origin ...
```

No custom import format is required.

Later, a migration tool can mirror repositories from GitHub/GitLab.

---

# 41. Forks

In Git, a fork is primarily another Git repository with ancestry.

In Adenosine:

```text
repo A
   |
forkedFrom
   v
repo B
```

The relationship can be represented in repository metadata.

Creating a local fork:

```text
source Git endpoint
      |
git clone --bare / fetch
      |
new bare repository
      |
new dev.adenosine.repo record
```

Cross-instance forks are naturally possible because the source is simply another public Git endpoint.

---

# 42. Search

v1:

Use PostgreSQL full-text/trigram search.

Index:

```text
repository name
description
owner handle cache
topics
```

Do not deploy Elasticsearch/OpenSearch initially.

Search is over local AppView projections, so a self-hosted instance can search repositories from the wider Adenosine network that it has indexed.

---

# 43. Caching

Start without Redis.

Useful in-memory caches:

```text
DID -> resolved document
handle -> DID
repo owner/slug -> repo ID
SSH fingerprint -> account DID
```

Use bounded TTL caches.

Repository content itself is already efficiently represented by Git's object database and OS page cache.

Add Redis only if multi-process/shared cache requirements justify it.

---

# 44. Network ports

Suggested defaults:

```text
443   HTTPS web/API/Git HTTP
22    Git SSH

8080  internal web process
2222  internal/dev SSH process
```

Production reverse proxy:

```text
Caddy / nginx / HAProxy
        |
        +--> :8080 web
```

SSH generally goes directly to `adenosine-sshd`.

Alternative:

```text
host SSH daemon :22
Adenosine SSH   :2222
```

for installations that already require host SSH.

Document both deployment modes.

---

# 45. Hostnames

A simple instance can use one hostname:

```text
code.example.com
```

Web:

```text
https://code.example.com/alice/project
```

HTTPS clone:

```text
https://code.example.com/alice/project.git
```

SSH:

```text
git@code.example.com:alice/project.git
```

No need for separate `git.` and `www.` hosts initially.

---

# 46. Reverse proxy behavior

Git requests can be long-running and large.

Reverse-proxy configuration must:

- disable inappropriate request buffering;
- allow streaming responses;
- permit large bodies;
- use sensible long timeouts for Git;
- preserve client cancellation;
- avoid compressing already compressed packfiles unnecessarily.

Document tested Caddy and nginx configs.

---

# 47. Backpressure and resource control

Large clone/push operations can consume:

- network;
- disk throughput;
- CPU during pack generation;
- Git subprocess slots.

Introduce per-instance concurrency limits.

Example:

```text
max upload-pack processes: 64
max receive-pack processes: 16
```

Do not simply accept unlimited subprocesses.

Use semaphores in Go:

```go
type GitLimiter struct {
    uploads chan struct{}
    pushes  chan struct{}
}
```

Later support per-user/repository limits.

---

# 48. Repository locking

Git itself handles much of the ref locking required for safe pushes.

Adenosine must still coordinate forge-level actions such as:

```text
merge PR
delete repository
rename repository
maintenance
```

Use repository-level advisory/application locks when necessary.

Do not serialize ordinary reads.

---

# 49. Maintenance

Repositories need maintenance:

```text
git gc
git maintenance
commit-graph generation
repacking
```

Do not run expensive maintenance synchronously after every push.

Schedule it.

Example:

```text
push_count threshold
       or
repository size threshold
       or
periodic schedule
          |
          v
maintenance job
```

Limit concurrent maintenance jobs because they are I/O-intensive.

---

# 50. Security model

This project is exposed directly to untrusted Git clients and untrusted repository data, so security needs to be foundational.

Key rules:

1. No shell execution of client-controlled strings.
2. Repository filesystem paths come only from internal IDs.
3. Git subprocesses run as an unprivileged service account.
4. Consider per-process/container isolation later.
5. Limit pack/request sizes where practical.
6. Rate-limit authentication attempts.
7. Hash access tokens.
8. Validate SSH keys and fingerprints.
9. Validate all ATProto records against Lexicons.
10. Treat remote Git hosts as untrusted.
11. Protect against SSRF when fetching PR sources.
12. Restrict outbound Git fetches to resolved repository endpoints.
13. Validate DNS/IP transitions to prevent private-network SSRF.
14. Sanitize rendered Markdown/HTML.
15. Never trust Git author emails as authenticated identity.
16. Keep Git executable current.

---

# 51. SSRF is especially important

Federated pull requests potentially cause:

```text
ATProto record
   |
sourceGit = arbitrary URL
   |
Adenosine server fetches it
```

That is an SSRF primitive if not constrained.

Adenosine should derive Git URLs only from validated repository records and enforce:

```text
https/ssh only
deny localhost
deny link-local
deny private network ranges by default
validate DNS resolution
prevent redirect to private addresses
limit response size/time
```

Self-hosters should be able to explicitly configure internal-network behavior.

---

# 52. Private repositories

Do not federate private repositories in v1.

Support one of two approaches:

### Simplest v1

Only public repositories.

### Slightly later

Allow local private repositories that are intentionally not represented in public Adenosine Lexicons.

```text
public repo:
Git + ATProto federation

private repo:
Git + local Postgres authorization only
```

Do not invent home-grown encrypted ATProto private federation early.

---

# 53. Observability — OpenTelemetry first

Observability is a core platform capability.

Adenosine uses **OpenTelemetry as the telemetry contract** for traces, metrics, and structured application logs.

Application code must not be coupled directly to:

```text
Grafana
Datadog
Honeycomb
New Relic
AWS X-Ray
CloudWatch
Jaeger
Prometheus remote write
```

Instead:

```text
Adenosine processes
        |
        | OTLP
        v
OpenTelemetry Collector
        |
        +----> operator-selected trace backend
        +----> operator-selected metric backend
        +----> operator-selected log backend
```

The Collector owns batching, retry, filtering, enrichment, sampling policy where appropriate, and backend export.

This matters for an open-source/self-hosted product: operators can use the observability stack they already have without Adenosine-specific instrumentation changes.

## 53.1 Signals

Adenosine emits:

```text
traces
metrics
logs
```

through OpenTelemetry.

Health checks remain normal HTTP endpoints:

```text
/health/live
/health/ready
```

Do not make application liveness depend on the telemetry backend.

If the Collector/backend is unavailable, Adenosine continues serving requests and Git traffic. Telemetry uses bounded queues and must never create unbounded memory pressure.

## 53.2 Go observability package

Centralize setup:

```text
internal/observability/
├── observability.go
├── trace.go
├── metrics.go
├── logging.go
├── resources.go
└── shutdown.go
```

The DI composition root builds one observability provider and injects:

```text
*trace.TracerProvider
*metric.MeterProvider
*log.LoggerProvider / slog handler bridge where used
*slog.Logger
```

or narrow wrapper/capability types where they improve testability.

Domain packages should obtain tracers/meters from injected observability dependencies or deliberately scoped providers rather than constructing exporters.

Exporter/Collector configuration belongs at startup.

## 53.3 Standard OpenTelemetry configuration

Prefer standard OTel environment variables so existing deployment tooling works naturally:

```text
OTEL_SERVICE_NAME=adenosine
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=development,service.version=dev
OTEL_TRACES_SAMPLER=always_on
```

Production deployment templates override sampling/export configuration appropriately.

Adenosine-specific configuration should only exist where standard OTel environment variables cannot express a required behavior.

## 53.4 Resource identity

Every signal should identify:

```text
service.name
service.version
service.instance.id
deployment.environment.name
```

For split roles, use clear service names such as:

```text
adenosine.web
adenosine.sshd
adenosine.indexer
adenosine.worker
```

For `adenosine serve`, a single service may expose multiple internal components; spans include:

```text
adenosine.component = web|sshd|indexer|worker
```

Do not put high-cardinality user/repository values in resource attributes.

## 53.5 HTTP tracing

Instrument inbound REST/Git HTTP and outbound HTTP using OpenTelemetry HTTP instrumentation.

Trace:

```text
incoming REST requests
Git Smart HTTP discovery
git-upload-pack RPC
git-receive-pack RPC
ATProto/PDS calls
remote Git metadata/fetch calls
webhook deliveries
Electric proxy/auth requests
```

Use standard HTTP semantic conventions where available.

A normal REST trace might look like:

```text
HTTP POST /api/v1/repositories
    |
    +-- auth.session.resolve
    |
    +-- repository.create
    |      |
    |      +-- db.repository.insert
    |      +-- git.init
    |      +-- outbox.write
    |
    +-- response
```

## 53.6 Git tracing

Git is a major operational surface and gets first-class spans.

Recommended spans:

```text
git.init
git.upload_pack
git.receive_pack
git.fetch_remote
git.diff
git.merge_base
git.merge
git.maintenance
git.command
```

Useful trace attributes:

```text
adenosine.repository.id
adenosine.git.operation
adenosine.git.transport = http|ssh|internal
adenosine.git.exit_code
adenosine.git.protocol_version
```

Do not attach:

```text
packfile contents
file contents
commit message bodies
access tokens
credentials
full request bodies
```

Repository IDs are acceptable for trace/log debugging, but should not become high-cardinality metric labels.

## 53.7 SSH tracing

An SSH connection starts a root/server span because a normal Git CLI will not propagate an OpenTelemetry trace context over SSH.

Trace:

```text
ssh.connection
   |
   +-- ssh.authenticate_key
   +-- ssh.authorize_repository
   +-- git.upload_pack / git.receive_pack
```

Useful attributes:

```text
network.transport
server.address
adenosine.git.operation
adenosine.repository.id
auth.method = ssh_public_key
```

Avoid storing raw public keys or credentials in telemetry.

## 53.8 PostgreSQL tracing

Instrument database calls using OpenTelemetry-compatible pgx instrumentation where practical.

Important operations include:

```text
repository lookup/create
permission lookup
network projection writes
outbox claim/complete
federation cursor update
search
```

Database spans must not capture secret SQL parameters.

Prefer statement/operation metadata and sanitized query identifiers over arbitrary argument values.

## 53.9 Federation tracing

Federation is asynchronous and must remain traceable.

Recommended spans:

```text
atproto.publish
atproto.resolve_did
federation.consume
federation.validate
federation.project
federation.reconcile
```

Attach:

```text
atproto.collection
adenosine.federation.operation
adenosine.federation.result
```

Do not use DID/AT URI as metric dimensions.

They may appear in traces/logs where operationally necessary, subject to the project's privacy policy.

## 53.10 Async trace propagation through the outbox

When a synchronous operation creates an outbox event, preserve W3C trace context so the eventual worker can correlate its work.

Add to `ops.outbox_events`:

```sql
ALTER TABLE ops.outbox_events
    ADD COLUMN traceparent TEXT,
    ADD COLUMN tracestate TEXT;
```

Producer:

```text
HTTP/API span
   |
   +-- write outbox event + trace context
```

Consumer:

```text
worker claims event
   |
restore/link trace context
   |
CONSUMER span
   |
perform webhook / ATProto publish / indexing work
```

Use producer/consumer span semantics rather than pretending asynchronous work is one long synchronous call.

If context is missing or invalid, start a new trace rather than failing the job.

## 53.11 Metrics

Use the OpenTelemetry Meter API.

Prefer standard instrumentation metrics where they exist, plus a small `adenosine.*` namespace for product-specific behavior.

Key custom metrics:

```text
adenosine.git.operations.active
adenosine.git.operation.duration
adenosine.git.bytes
adenosine.git.failures

adenosine.federation.events
adenosine.federation.projection.duration
adenosine.federation.errors
adenosine.federation.lag

adenosine.outbox.pending
adenosine.outbox.age
adenosine.outbox.attempts

adenosine.webhook.delivery.duration
adenosine.webhook.failures

adenosine.repository.count
adenosine.repository.storage.bytes

adenosine.pr.remote_fetch.duration
adenosine.pr.remote_fetch.failures
```

Keep metric dimensions low-cardinality.

Good labels:

```text
operation=upload_pack
transport=http
result=success
collection=dev.adenosine.star
```

Bad labels:

```text
repository_id=<UUID>
user_did=<DID>
pull_request_uri=<AT URI>
git_sha=<SHA>
```

High-cardinality values belong in traces/logs, not metric labels.

## 53.12 Structured logs

Application code uses Go `slog`.

Logs are structured from the start.

Use the OpenTelemetry slog bridge/export path so logs can be correlated with traces while retaining normal container stdout logs.

Every request/job logger should include context where available:

```text
trace_id
span_id
request_id
component
repository_id
operation
```

Never log:

```text
access tokens
session tokens
OAuth credentials
private keys
webhook secrets
Authorization headers
Git pack/body contents
Electric secrets
database passwords
```

Avoid logging issue/comment/README bodies by default because they may contain secrets or sensitive user content.

## 53.13 Error recording

When an operation fails:

1. return the Go error normally;
2. record the error on the active span at the boundary that understands it;
3. set appropriate span status;
4. increment a bounded-cardinality failure metric where useful;
5. emit one structured log at the operational ownership boundary.

Do not produce the same stack of duplicate error logs at every layer.

## 53.14 Trace propagation

Support W3C:

```text
traceparent
tracestate
```

for HTTP.

Propagate context through:

```text
REST calls
ATProto HTTP
webhooks
internal HTTP
Electric proxy requests where useful
```

Git CLI HTTP requests generally start a new trace because external clients will not normally inject application trace context.

SSH sessions also start new traces.

## 53.15 Sampling

Development:

```text
100% tracing
```

Production:

- configurable through standard OTel settings;
- default to a sensible bounded head-sampling rate;
- allow the operator's Collector/backend to implement more advanced sampling;
- strongly consider preserving errors and unusually slow operations through Collector policy.

Do not compile sampling policy into domain code.

## 53.16 Local observability experience

`make dev` starts a local OpenTelemetry-compatible observability backend automatically.

Use Grafana's `otel-lgtm` development image or an equivalent maintained all-in-one development stack because it provides a convenient local OTel Collector plus traces, metrics, logs, and Grafana in one development container.

Local flow:

```text
Adenosine Go
TanStack Start
other instrumented services
       |
      OTLP
       |
       v
 local OTel/LGTM container
       |
  +----+----+----+
  |         |    |
traces   metrics logs
  |         |    |
  +---- Grafana--+
```

Default development URLs:

```text
Web app:    http://localhost:3000
REST API:   http://localhost:8080
API docs:   http://localhost:8080/docs/api
Git SSH:    localhost:2222
Grafana:    http://localhost:3001
```

OTLP ports may be exposed to the host for tooling:

```text
4317 gRPC
4318 HTTP
```

## 53.17 Production observability

Production templates include or support an OpenTelemetry Collector.

The application emits OTLP to a nearby Collector endpoint.

The Collector can export to:

```text
self-hosted Grafana stack
Grafana Cloud
AWS-native observability
Honeycomb
Datadog
New Relic
other OTLP-compatible backends
```

without changing Adenosine application code.

Do not require Grafana in production.

## 53.18 Dashboards

Ship useful versioned dashboards as part of the repository:

```text
infra/observability/dashboards/
├── overview.json
├── git.json
└── federation.json
```

### Overview

```text
request rate/error/latency
Git operations
outbox backlog
federation lag
database health
repository storage growth
```

### Git

```text
clone/fetch/push throughput
active pack processes
duration percentiles
bytes transferred
failure rate
maintenance duration
```

### Federation

```text
events/sec
lag
projection latency
validation failures
reconciliation failures
records by collection
```

Dashboards are conveniences, not the telemetry contract.

## 53.19 Operational alerts

Document recommended alerts, while leaving the alerting backend operator-controlled.

Examples:

```text
readiness failing
Git push error rate elevated
receive-pack latency elevated
outbox oldest-event age too high
federation lag above threshold
federation projection failures
repository disk near capacity
database pool exhaustion
webhook retry backlog
```

## 53.20 Profiling

Expose Go `pprof` only on an internal/admin listener that is disabled or network-restricted by default.

Never expose pprof publicly on the main internet-facing listener.

Profiles are complementary to OTel traces; they do not replace them.

## 53.21 Observability must not break the app

Instrumentation follows these invariants:

1. telemetry export is bounded;
2. telemetry failure does not fail Git/API requests;
3. telemetry does not contain credentials or Git content;
4. telemetry setup is dependency-injected;
5. telemetry exporters are created only at startup;
6. domain packages do not know the backend vendor;
7. trace/metric attributes follow low-cardinality/privacy rules;
8. OTel providers are flushed with a bounded timeout during graceful shutdown;
9. health endpoints do not require the Collector/backend to be healthy;
10. tests can inject no-op/in-memory OTel providers.

## 53.22 Reference implementation

Go should use the official OpenTelemetry Go SDK and maintained OpenTelemetry instrumentation/bridges where available.

The project should prefer OTLP export to a Collector rather than configuring every vendor exporter directly in application code.

Document exact pinned versions in `go.mod` and upgrade them deliberately.

# 54. Testing strategy

## Unit tests

Domain logic:

```text
permissions
repo state transitions
PR state
ATProto record validation
URL validation
SSH command parsing
```

## Git integration tests

Use real temporary bare repositories and the real Git binary.

Test through the actual CLI:

```bash
git clone
git fetch
git push
git push --tags
git push --delete
```

Do not mock Git for transport integration tests.

## HTTP integration

Start Adenosine on a random port and execute:

```bash
git clone http://127.0.0.1:<port>/alice/test.git
```

Assert files/history.

## SSH integration

Start `adenosine-sshd` with a generated test host key and user key.

Execute:

```bash
GIT_SSH_COMMAND="ssh ..." git clone ...
```

## Federation tests

Run:

```text
PDS/test fixture
Tap/event fixture
Adenosine A
Adenosine B
```

Publish a repo on A and verify B indexes/displays it.

---

# 55. Local development — Dockerized services, native tooling

The local developer experience has one hard requirement:

> **A contributor with Make + Docker + Docker Compose can run the complete Adenosine service stack with `make dev`; contributors use native Go and Bun for tests, linting, and generation.**

Running the application locally should not require host-installed:

```text
Postgres
Git server tooling
Electric
Tap
OpenTelemetry Collector
Grafana
Pulumi
```

Those runtime services run inside versioned containers. Contributors install the Go version declared by `go.mod` and the pinned Bun version for fast native code-quality commands. Generator versions remain pinned; sqlc is downloaded to a checksum-verified local tool cache and the OpenAPI generator is invoked through `go run`, so global installs are not required.

The host still needs a normal Git client to exercise the product as an end user.

## 55.1 Canonical command

From a fresh clone:

```bash
make dev
```

That is the documented happy path.

`make dev`:

1. checks Docker and Docker Compose availability;
2. creates `.env.local` from the committed example if missing;
3. generates safe development secrets if missing;
4. builds/pulls the pinned development images;
5. starts PostgreSQL;
6. waits for PostgreSQL readiness;
7. runs schema migrations;
8. starts from the committed generated OpenAPI/sqlc/Lexicon code;
9. starts Electric;
10. starts Tap/federation development dependencies;
11. starts the local OpenTelemetry/Grafana development backend;
12. starts the Go server with hot reload;
13. starts the TanStack Start web app with HMR;
14. waits for readiness;
15. prints the important local URLs.

No manual `make db`, `make migrate`, or service-specific startup sequence is required before it.

## 55.2 Makefile surface

Keep the Makefile intentionally small and memorable:

```make
.PHONY: dev dev-detached down reset logs test lint generate doctor shell psql e2e e2e-federation

dev:
	./scripts/dev.sh

dev-detached:
	./scripts/dev.sh --detach

down:
	docker compose -f dev/docker-compose.yml down

reset:
	./scripts/reset-dev.sh

logs:
	docker compose -f dev/docker-compose.yml logs -f

test:
	./scripts/test.sh

lint:
	./scripts/lint.sh

generate:
	./scripts/generate.sh

doctor:
	./scripts/docker-task.sh doctor

shell:
	docker compose ... exec adenosine sh

psql:
	docker compose ... exec postgres psql ...

e2e:
	./scripts/docker-task.sh e2e

e2e-federation:
	./scripts/e2e-federation.sh
```

The exact implementation can evolve, but these user-facing targets should remain stable.

## 55.3 Compose topology

Development Compose:

```text
                    +------------------+
browser ----------> | web              |
                    | TanStack Start    |
                    | :3000             |
                    +--------+---------+
                             |
                             | REST/sync
                             v
                    +------------------+
git HTTP ---------->| adenosine        |
git SSH :2222 ----->| Go/Air           |
                    | :8080            |
                    +--+----+----+-----+
                       |    |    |
                 +-----+    |    +-----------------+
                 |          |                      |
                 v          v                      v
             Postgres    Electric                 Tap
                 |          |
                 |          |
                 +----------+
                 |
                 | logical replication
                 |
                 +-------------------------------+
                                                 |
all instrumented services -----------------------+
                         OTLP                     |
                                                 v
                                      +--------------------+
                                      | local OTel/LGTM    |
                                      | Collector +        |
                                      | Grafana/backends   |
                                      | Grafana :3001      |
                                      +--------------------+
```

Compose services:

```text
postgres
adenosine
web
electric
tap
otel-lgtm
```

Add Caddy only if the local architecture needs one-origin routing/TLS for a feature being developed. Do not make local reverse proxy complexity mandatory when direct ports are sufficient.

## 55.4 Development entrypoint

Use an idempotent entrypoint in the Adenosine development image for startup preparation that must occur before the requested container command.

It owns:

```text
configuration validation
migration
development SSH host-key generation
seed/bootstrap data where enabled
```

The entrypoint completes this preparation and then uses `exec` to launch Air, a test command, or another requested process directly. Do not add a separate bootstrap service for work that belongs to one application's startup lifecycle.

Conceptually:

```sh
mkdir -p "$ADENOSINE_REPO_ROOT"
go mod download
exec "$@"
```

Generated files written through bind mounts must use the host UID/GID where supported so contributors do not end up with root-owned source files.

## 55.5 Development Dockerfiles

Use development targets for running the service stack, hot reload, and production-like runtime dependencies. Do not install lint or generator tooling into the runtime development image merely to wrap native developer commands in Docker.

Example:

```text
dev/Dockerfile
├── target: tools
│   ├── Go
│   ├── Git
│   └── runtime/service tooling
│
├── target: dev-go
│   ├── tools
│   └── Air/hot reload
│
└── target: production
    └── minimal runtime + git
```

The web development image contains:

```text
Node/Bun runtime chosen by the web package
Bun
TanStack tooling
```

Pin all tool versions.

## 55.6 Dependency caches

Use named Docker volumes for expensive dependency caches:

```text
go_mod_cache
go_build_cache
web_node_modules
pnpm_store
```

Bind mount source code.

This keeps the Dockerized service stack fast after the first run. Native Go and Bun commands use their normal host caches.

## 55.7 Hot reload

Go:

```text
source bind mount
   |
Air or equivalent watcher
   |
rebuild/restart adenosine
```

Web:

```text
source bind mount
   |
TanStack Start/Vite HMR
```

A backend code change should not require rebuilding the whole Compose stack manually.

A frontend code change should HMR normally.

## 55.8 Development database

Postgres is always containerized.

Use a persistent named volume during normal development:

```text
postgres_data_dev
```

`make down` preserves it.

`make reset` destroys local Adenosine development data after a clear warning/explicit implementation and recreates the environment.

Migrations run automatically through the bootstrap dependency.

## 55.9 Development Git storage

Use a named volume:

```text
adenosine_repos_dev
```

mounted at:

```text
/var/lib/adenosine/repos
```

This matches production semantics more closely than writing repositories into arbitrary source-tree directories.

## 55.10 Local URLs

After successful startup print:

```text
Adenosine web:     http://localhost:3000
REST API:          http://localhost:8080/api/v1
OpenAPI docs:      http://localhost:8080/docs/api
OpenAPI JSON:      http://localhost:8080/openapi.json

Git HTTP base:     http://localhost:8080
Git SSH:           ssh://git@localhost:2222

Grafana:           http://localhost:3001
Postgres:          localhost:5432
OTLP/gRPC:         localhost:4317
OTLP/HTTP:         localhost:4318
```

Electric/Tap internal ports do not need to be exposed publicly unless useful to developers.

## 55.11 Local telemetry

All Adenosine containers receive development OTel defaults:

```text
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-lgtm:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_TRACES_SAMPLER=always_on
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=development
```

Development sends 100% traces.

Grafana should come up with:

```text
trace datasource ready
metrics datasource ready
logs datasource ready
Adenosine dashboards provisioned
```

A contributor should be able to make one API request or Git clone, open Grafana, and immediately inspect its trace.

## 55.12 Development logging

`docker compose logs` remains useful.

Go logs structured JSON to stdout and also correlates/exports logs through the OTel path.

This gives both:

```bash
make logs
```

and rich trace-correlated logs in Grafana.

## 55.13 Code generation

Generated files are committed, but development should keep them correct.

`make generate` runs natively with pinned tools:

```text
sqlc
OpenAPI Go generation
OpenAPI TypeScript client generation
Lexicon type generation
formatting
```

`make dev` may run a fast generation/check step before startup so a fresh checkout is self-consistent.

CI verifies:

```bash
make generate
git diff --exit-code
```

The host needs Go and Bun, but sqlc is cached from a checksum-verified pinned release and the OpenAPI Go generator is invoked at a pinned version through `go run` rather than global installs.

## 55.14 Tests and linting are native

A contributor can run:

```bash
make test
make lint
make e2e
```

`make test` and `make lint` use the host Go and Bun toolchains for fast feedback. `make e2e` uses Docker because it exercises the assembled service topology. CI pins the same Go and Bun versions.

Pinned versions and generated-diff checks prevent:

```text
"works with my local Go version"
"my sqlc generated different output"
"my Node version behaves differently"
```

The CI images/tool versions should be derived from the same pinned development definitions where practical.

## 55.15 Federation development environment

Provide:

```bash
make e2e-federation
```

which starts two logically independent Adenosine instances with independent:

```text
Postgres
repository storage
base URL
sessions
```

but connected to the same test ATProto environment.

Acceptance flow:

```text
Instance A
   |
publish profile + repository + star/issue/PR
   |
ATProto test network
   |
Instance B
   |
index/project
   |
Electric
   |
browser/API sees update
```

This is essential because cross-instance behavior is the product's defining feature.

Do not rely only on unit-test mocks for federation.

## 55.16 Dev environment invariants

1. Fresh clone + `make dev` is enough.
2. Docker is the only required application toolchain.
3. Host Go/Node/Postgres versions do not affect development.
4. Migrations happen automatically before app readiness.
5. OTel works automatically in development.
6. Git HTTP and SSH are exercised against the real Git binary.
7. Generated code uses pinned containerized tools.
8. `make down` is non-destructive.
9. `make reset` is the explicit destructive reset.
10. Local topology resembles production boundaries closely enough to catch integration bugs.
11. Developer secrets are generated once and persisted locally.
12. No development secret is committed.

# 56. Configuration

Use environment variables.

Example:

```text
ADENOSINE_BASE_URL=https://code.example.com
ADENOSINE_LISTEN_ADDR=:8080

ADENOSINE_SSH_LISTEN_ADDR=:2222
ADENOSINE_SSH_HOST=code.example.com
ADENOSINE_SSH_PORT=22
ADENOSINE_SSH_HOST_KEY=/var/lib/adenosine/ssh/host_key

ADENOSINE_REPO_ROOT=/var/lib/adenosine/repos

DATABASE_URL=postgres://...

ADENOSINE_ATPROTO_TAP_URL=...
ADENOSINE_ATPROTO_COLLECTION_PREFIX=dev.adenosine

ADENOSINE_ELECTRIC_URL=http://electric:3000
ADENOSINE_ELECTRIC_SECRET=...
ADENOSINE_PUBLIC_SYNC_ENABLED=true

ADENOSINE_GIT_BINARY=/usr/bin/git

# OpenTelemetry — prefer standard environment variables
OTEL_SERVICE_NAME=adenosine
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.10
```

Parse once at startup into a typed config.

Fail fast on invalid configuration.

---

# 57. Self-hosting and deployment

**Ease of self-hosting is a core product requirement.**

The project should maintain multiple deployment paths, all based on the same container image/binary and environment-variable contract.

A user should not need to understand Adenosine internals to deploy a working network participant.

## 57.1 Supported deployment tiers

### Tier 1 — local development / evaluation

From the source repository:

```bash
make dev
```

starts the complete Dockerized development environment:

```text
Adenosine
TanStack Start web
Postgres
Electric
Tap
local OpenTelemetry/Grafana backend
```

with persistent development volumes and hot reload.

For a detached source-based evaluation environment:

```bash
make dev-detached
```

The README should get someone from clone to a healthy network-capable development instance with one command.

### Tier 2 — single VM / serious self-hosting

Recommended for most individuals and small organizations:

```text
Linux VM
├── Caddy
├── Adenosine
├── Electric
├── Tap
├── OpenTelemetry Collector
├── Postgres
└── local NVMe repository volume
```

Install using:

```bash
curl ... | sh
```

or preferably a transparent downloadable install script documented for inspection:

```bash
./scripts/bootstrap.sh
```

Support both Docker Compose and systemd.

### Tier 3 — managed platform

Officially maintained Pulumi deployments:

```text
Railway
AWS
```

Potential later:

```text
Fly.io
DigitalOcean
Hetzner
GCP
Azure
Kubernetes/Helm
```

Do not add providers unless they can be tested and maintained.

## 57.2 Docker image

Publish a multi-architecture image:

```text
ghcr.io/<project>/adenosine:<version>
```

Architectures:

```text
linux/amd64
linux/arm64
```

The image includes:

```text
adenosine Go binary
native git
web build/static assets if served by Go
migration support
health/doctor commands
```

Prefer one official image rather than separate `web`, `sshd`, and `indexer` images.

Entrypoints:

```bash
adenosine serve
adenosine web
adenosine sshd
adenosine indexer
adenosine migrate
adenosine doctor
```

## 57.3 Docker Compose

Official Compose should be production-usable for a single node.

```text
deploy/docker-compose.yml
```

Services:

```text
caddy
adenosine
postgres
electric
tap
otel-collector
```

The production Compose stack includes an OpenTelemetry Collector but does not require the development-only Grafana/LGTM backend. Operators may configure the Collector to export to their chosen observability backend.

Volumes:

```text
adenosine_repos
postgres_data
adenosine_state
```

A user workflow:

```bash
cp deploy/.env.example .env
./scripts/bootstrap.sh
docker compose -f deploy/docker-compose.yml up -d
./scripts/doctor.sh
```

Bootstrap should generate:

```text
instance secret
SSH host key
Electric secret
database password
other required local secrets
```

without overwriting existing secrets.

## 57.4 Deployment scripts

Ship scripts for routine operations:

```text
scripts/bootstrap.sh
scripts/migrate.sh
scripts/backup.sh
scripts/restore.sh
scripts/upgrade.sh
scripts/doctor.sh
scripts/deploy-railway.sh
scripts/deploy-aws.sh
```

Requirements:

- `set -euo pipefail`;
- noninteractive mode for CI;
- clear errors;
- rerunnable/idempotent where practical;
- `--help`;
- no hidden calls that mutate infrastructure unexpectedly;
- secrets never printed unless explicitly requested.

## 57.5 Pulumi philosophy

Infrastructure-as-code is maintained **inside the Adenosine repository**.

Use reusable Pulumi components so deployment targets share intent:

```text
Adenosine application
Postgres
Electric
persistent repository storage
TLS/domain
secrets
health checks
backup configuration
```

Provider-specific stacks map those capabilities to the target cloud.

Prefer TypeScript for Pulumi deployment code because the deployment ecosystem and provider examples are particularly strong there, even though the application backend is Go.

Infrastructure code is tooling, not application runtime.

## 57.6 Railway deployment

Railway is a good easy-deploy target because it supports container services, PostgreSQL, and persistent volumes.

Target architecture:

```text
Railway Project
├── adenosine
│   ├── image: official Adenosine image
│   └── volume: /var/lib/adenosine/repos
├── postgres
│   └── persistent database storage
├── electric
├── tap
└── otel-collector
```

Expose:

```text
Adenosine HTTPS
Adenosine SSH/TCP where Railway deployment capabilities permit
```

The Pulumi stack should create/configure:

```text
project/environment
Adenosine service
Postgres service
repository persistent volume
Electric service
Tap service
environment variables
domain/output values
```

Because Railway's infrastructure/provider surface can evolve, keep provider usage isolated inside:

```text
infra/pulumi/railway
```

and test it in CI/release smoke tests.

Provide:

```bash
./scripts/deploy-railway.sh
```

which roughly performs:

```text
check Pulumi/Railway credentials
select/create stack
prompt for domain/config
pulumi up
run migrations
run adenosine doctor
print instance URL + next steps
```

Also provide a Railway template/Compose-based deployment path where practical so Pulumi is not mandatory.

Railway persistent volumes are necessary for locally stored bare Git repositories.

## 57.7 AWS deployment

Provide one opinionated AWS architecture rather than an enormous configurable platform.

Recommended first AWS stack:

```text
Route53 / user DNS
        |
       ALB
        |
      ECS
        |
  +-----+-------+
  |             |
Adenosine    Electric/Tap
  |             OTel Collector
  +------ RDS PostgreSQL
  |
 persistent Git storage
```

Git repository storage is the key AWS design choice.

For the first AWS deployment, prefer a persistent POSIX filesystem accessible by Adenosine compute rather than pretending S3 is a Git filesystem.

A practical stack can use:

```text
ECS/Fargate or ECS/EC2 depending storage/SSH constraints
RDS PostgreSQL
EFS for repository storage
ALB for HTTPS
Route53 optional
ACM TLS
Secrets Manager / SSM
CloudWatch logs
scheduled backups
```

If Fargate constraints make SSH or repository I/O awkward, use ECS on EC2 for the canonical "full forge" template and provide HTTP-only Fargate as a later variant. Do not force an architecture merely because it looks serverless.

Pulumi stack provisions:

```text
VPC
subnets/security groups
ECS cluster/service
ECR/image reference
RDS
EFS
ALB
TLS/domain
secrets
logging
Electric
Tap
backup policy
```

Deploy:

```bash
./scripts/deploy-aws.sh
```

## 57.8 Pulumi stack inputs

Common configuration:

```text
domain
adenosineVersion
region
repositoryStorageSize/strategy
databaseSize
enableElectric
enableTap
publicRegistration
sshPort
backupRetention
```

Outputs:

```text
webUrl
gitHttpsBaseUrl
gitSshHost
apiDocsUrl
healthUrl
```

Do not expose provider-specific resource identifiers as the primary user-facing outputs.

## 57.9 One-click-ish deployment

The repository README should prominently offer:

```text
Docker Compose
Deploy to Railway
Deploy to AWS
Install on a VM
```

Each path gets:

```text
prerequisites
one main command
expected cost/scale characteristics
backup story
upgrade story
limitations
```

Avoid a 40-step wiki guide.

## 57.10 First-run experience

After deployment, visiting the instance should show setup status if configuration is incomplete.

`adenosine doctor` checks:

```text
Postgres connectivity
schema version
Git binary/version
repository storage writable
SSH host key
public base URL
ATProto connectivity
Tap connectivity
Electric connectivity
OpenTelemetry Collector reachability (warning, not readiness-critical)
logical replication requirements
outbound federation access
disk space
```

Example:

```text
✓ PostgreSQL
✓ migrations
✓ Git 2.x
✓ repository storage
✓ SSH
✓ ATProto
✓ Tap
✓ Electric
✓ public URL

Adenosine is ready.
```

## 57.11 Migrations and upgrades

Container startup must not silently run dangerous schema migrations in every replica.

Recommended:

```bash
adenosine migrate
```

Deployment scripts run migrations before replacing the application.

Upgrade flow:

```bash
./scripts/backup.sh
./scripts/upgrade.sh v0.8.0
```

`upgrade.sh`:

1. validates target version;
2. checks release migration notes;
3. runs backup/preflight;
4. pulls image/binary;
5. runs migrations;
6. restarts;
7. runs doctor;
8. keeps rollback instructions.

## 57.12 Backups

A backup includes:

```text
Postgres
Git repository volume
SSH host key
instance secrets/config required for identity
```

Provide a portable backup manifest.

Example:

```text
backup/
├── manifest.json
├── postgres.dump
├── repositories.tar.zst
├── ssh/
└── config/
```

Cloud templates should configure native snapshots where possible, but the Adenosine backup command remains provider-independent.

## 57.13 Releases

Every tagged release should produce:

```text
Linux amd64 binary
Linux arm64 binary
macOS binaries where useful
container image
checksums
SBOM
OpenAPI spec
generated TypeScript API package
migration notes
```

Deployment stacks accept an explicit Adenosine version; avoid deploying mutable `latest` in production templates.

## 57.14 Deployment compatibility contract

All official deployment methods must ultimately supply the same core contract:

```text
DATABASE_URL
ADENOSINE_BASE_URL
ADENOSINE_REPO_ROOT
ATProto configuration
Electric configuration
SSH configuration
OpenTelemetry/OTLP configuration
instance secrets
```

The application must not contain logic such as:

```go
if railway { ... }
if aws { ... }
```

Cloud-specific behavior belongs in IaC, not application domain code.

## 57.15 Self-hosting acceptance test

CI/release testing should prove:

```text
fresh Compose install
migrate
create account/session fixture
create repository
push with git CLI
clone with git CLI
API responds
OpenAPI docs respond
Electric sync updates
ATProto indexing starts
backup
restore into clean instance
```

This is as important as unit tests because "easy to deploy" is part of the product.

# 58. Single binary and simple process model

Compile one primary Go binary:

```bash
adenosine serve
adenosine web
adenosine sshd
adenosine indexer
adenosine worker
adenosine migrate
adenosine doctor
adenosine backup
adenosine restore
```

For small/self-hosted installations:

```bash
adenosine serve
```

runs:

```text
REST API
Git Smart HTTP
SSH
background outbox worker
federation projector/indexer client
static web assets / web integration as designed
```

in one process where feasible.

External required/recommended services remain:

```text
Postgres
Electric
Tap
```

For larger installations, split roles:

```text
adenosine web       x N
adenosine sshd      x N
adenosine indexer
adenosine worker    x N
```

The same binary and DI composition code powers all roles.

The official container image uses this same binary.

The frontend may be built separately by TanStack Start and either:

1. be served as its own container/process; or
2. have production assets integrated into the official deployment.

Whichever model is selected, it must not weaken the public-API boundary.

# 59. Scaling path

Do not architect v1 as a distributed system, but preserve the right boundaries.

## Stage 1

```text
1 VM
1 Postgres
local NVMe repos
all Adenosine components
```

## Stage 2

```text
load balancer
web x N
sshd x N
Postgres
dedicated repo node
```

## Stage 3

```text
edge/API nodes
      |
repo router
      |
+-----+-----+-----+
|           |     |
repo-1    repo-2 repo-3
NVMe      NVMe   NVMe
```

Database maps:

```text
repository_id -> storage_node_id
```

## Stage 4

Replication/mirroring.

Do not build Stage 3 or 4 until usage demands it.

---

# 60. Repository routing abstraction

From day one:

```go
type Locator interface {
    Locate(ctx context.Context, repoID repository.ID) (Location, error)
}
```

v1 always returns:

```text
localhost
```

Future:

```text
repo_123 -> git-storage-07
```

This prevents assumptions that every repository exists on the web server's filesystem.

---

# 61. Git storage node future protocol

Eventually:

```text
Git client
   |
load balancer
   |
Git frontend
   |
repo locator
   |
storage node
   |
native Git
```

Do not send packfiles through a generic JSON RPC layer.

If split later, use streaming HTTP/2, gRPC streaming, or direct routing so packfile data stays streaming end-to-end.

---

# 62. Webhooks

Repository owners can configure:

```text
URL
secret
events
enabled
```

Events:

```text
push
issue
pull_request
review
```

Delivery:

```text
outbox
  |
worker
  |
HTTP POST
  |
signed payload
```

Use HMAC signatures.

Store delivery history and retry with exponential backoff.

Protect webhook delivery against SSRF too.

---

# 63. Notifications

Do not make notifications canonical federated records initially.

Treat them as AppView-derived data:

```text
issue mentions me
review requested
PR merged
comment on my issue
```

A user's local Adenosine frontend can derive notification state from indexed events.

Later the notification architecture can evolve independently.

---

# 64. Markdown rendering

Repositories/issues/PRs contain hostile user content.

Markdown pipeline:

```text
Markdown
   |
renderer
   |
HTML sanitizer
   |
safe HTML
```

Do not allow arbitrary HTML/script execution.

README rendering should have strict resource policies for embedded external content.

---

# 65. Repository naming

Public route:

```text
/<handle-or-owner>/<repo>
```

But resolution internally should be:

```text
handle
  |
resolve DID
  |
DID + repo slug / indexed alias
  |
repository identity
```

Because handles can change.

Maintain aliases/redirects after:

```text
repo rename
handle change
```

---

# 66. Local vs remote repository resolution

Create a resolver abstraction:

```go
type Resolver interface {
    Resolve(ctx context.Context, owner, slug string) (ResolvedRepository, error)
}
```

Result:

```go
type ResolvedRepository struct {
    URI       string
    OwnerDID  string
    Name      string
    Local     bool
    LocalID   *repository.ID
    GitHTTPS  string
    GitSSH    string
    WebURL    string
}
```

This allows the web UI to treat local and federated repositories similarly.

---

# 67. API object identity

Public API objects should expose both:

```text
uri
cid
```

for federated records where applicable.

Avoid exposing only auto-increment database IDs.

Example:

```json
{
  "uri": "at://did:plc:.../dev.adenosine.issue/...",
  "cid": "...",
  "title": "...",
  "author": {
    "did": "did:plc:...",
    "handle": "bob.example"
  }
}
```

---

# 68. Consistency expectations

The system is federated and eventually consistent.

For example:

```text
Bob creates issue
     |
Bob's PDS
     |
relay
     |
Alice's indexer
     |
Alice sees issue
```

There may be a small delay.

The UI should not pretend federation is transactional.

When the local user creates a record:

1. write to their PDS;
2. optimistically project/display it locally;
3. reconcile when the network event arrives.

---

# 69. Deletions

ATProto records can disappear/change.

Indexer processing must support:

```text
create
update
delete
```

Do not assume records are append-only.

If a user deletes a star:

```text
star projection -> deleted
star count -> updated
```

If an issue author deletes an issue record, project the deletion according to protocol/product policy.

For moderation/audit reasons, decide whether local tombstone metadata is retained without retaining deleted content.

---

# 70. Moderation

Federation creates abuse problems immediately.

Even an early public release needs:

```text
block DID
hide record
hide repository
report
instance-level moderation rules
```

The local AppView controls what its users see even though records exist publicly on the network.

Keep moderation decisions in local storage initially.

Do not confuse:

```text
record existence on ATProto
```

with:

```text
record being displayed by this Adenosine instance
```

---

# 71. CLI beyond Git

Adenosine can eventually provide:

```bash
adenosine auth login
adenosine repo create
adenosine repo view
adenosine issue create
adenosine pr create
adenosine pr checkout
adenosine pr merge
```

But this is a convenience API client, similar to `gh`.

It should not replace Git.

A particularly useful command:

```bash
adenosine pr checkout 42
```

could resolve the federated source repo and configure/fetch it.

---

# 72. Optional Git remote helper

Much later, Adenosine could experiment with:

```text
git-remote-at
```

to resolve canonical AT URIs.

That might allow a syntax based on Git remote helpers.

But do not make this part of v1.

The universal compatibility path remains:

```text
ATProto repo record
       |
resolve
       |
standard HTTPS/SSH Git URL
       |
normal git CLI
```

---

# 73. Initial implementation milestones

## Milestone 0 — repository skeleton

Build:

- Go module.
- config.
- logging.
- Postgres connection.
- migrations.
- health endpoints.
- Makefile.
- CI.
- Docker Compose.

Success:

```bash
make dev
curl localhost:8080/health/ready
```

---

## Milestone 1 — local Git hosting over HTTP

Build:

- repository domain.
- filesystem `RepositoryStore`.
- repository create API.
- `git init --bare`.
- Smart HTTP discovery.
- `git-upload-pack`.
- `git-receive-pack`.
- basic token authentication.

Success:

```bash
git clone http://localhost:8080/alice/test.git
git commit ...
git push
```

This is the first critical technical milestone.

---

## Milestone 2 — SSH Git

Build:

- Go SSH server.
- host keys.
- user SSH key CRUD.
- fingerprint authentication.
- strict command parser.
- upload-pack.
- receive-pack.
- authorization.

Success:

```bash
git clone ssh://git@localhost:2222/alice/test.git
git push
```

---

## Milestone 3 — repository web browser

Build:

- tree listing.
- blob rendering.
- README.
- branches.
- commits.
- commit page.
- diff page.
- React frontend.

Success:

A user can use Adenosine as a minimal public Git forge without federation.

---

## Milestone 4 — ATProto identity and shared profile

Build:

- ATProto OAuth/login.
- DID-first account model.
- `dev.adenosine.profile` Lexicon.
- shared profile publication/indexing.
- handle resolution/cache.
- bind local Git credentials to DID.

Success:

There are no independent Adenosine username/password identities for normal users.

The same DID/profile appears consistently across two independently deployed Adenosine instances.

---

## Milestone 5 — repository federation

Build:

- `dev.adenosine.repo` Lexicon.
- publish record on repo creation.
- Tap integration.
- indexer.
- federated repository projection.
- Explore page.

Success:

```text
Instance A creates repo
      |
ATProto
      |
Instance B discovers repo
```

This proves the core idea.

---

## Milestone 6 — issues

Build:

- issue Lexicon.
- issue comment Lexicon.
- publishing.
- indexing.
- issue pages.
- local moderation primitives.

Success:

A user on instance B creates an issue against a repo hosted by instance A and both instances can display it.

---

## Milestone 7 — stars

Build:

- star Lexicon.
- indexing.
- counts.
- UI.

This is deliberately simple and is useful for testing high-volume cross-user references.

---

## Milestone 8 — pull requests

Build:

- PR Lexicon.
- PR status/decision Lexicon.
- source repo resolution.
- remote Git fetch.
- PR refs.
- merge-base.
- diff.
- comments/reviews.
- merge flow.

This is probably the hardest v1 feature.

---

## Milestone 9 — operational hardening

Build:

- rate limiting.
- process limits.
- maintenance scheduling.
- backup docs.
- metrics.
- SSRF protection.
- moderation.
- recovery/reindex commands.
- security audit checklist.

---

## Milestone 10 — public alpha

Requirements:

```text
git clone works reliably
git push works reliably
SSH works
ATProto identity works
cross-instance repo discovery works
cross-instance issue works
PR works
single-node install documented
upgrade/migration documented
```

At this point the project is genuinely differentiated from a normal Git forge.

---

# 74. First vertical slice to build

Do not begin with the full folder tree.

Start with:

```text
cmd/adenosine/main.go
internal/config
internal/database
internal/repository
internal/storage
internal/git
internal/githttp
```

The first vertical slice should be:

```text
POST /api/v1/repos
      |
repository.Service
      |
filesystem store
      |
git init --bare
```

then:

```text
git clone
```

then:

```text
git push
```

Only once that works should you build UI/federation.

The core risk to eliminate first is:

> Can Adenosine safely and cleanly host standard Git repositories using the normal Git CLI?

---

# 75. Suggested first commits

```text
chore: initialise Go module and development environment

feat: add application configuration and postgres connection

feat: add repository domain and migrations

feat: create bare repository storage

feat: implement git smart HTTP ref discovery

feat: implement git upload-pack RPC

feat: implement git receive-pack RPC

test: clone and push through real git CLI

feat: add repository access tokens

feat: add SSH public key storage

feat: implement Git SSH transport

feat: add basic repository browser

feat: authenticate users with AT Protocol

feat: publish repository records to AT Protocol

feat: index federated repository records
```

---

# 76. Important invariants

These are worth putting into `docs/architecture.md`.

### Git invariants

- Native Git owns Git repository semantics.
- Adenosine never rewrites Git object IDs.
- Git protocol remains standard.
- Packfiles are streamed.
- Client-controlled strings are never executed through a shell.

### identity invariants

- DID is the network-wide user identity.
- A user does not create a separate Adenosine identity per instance.
- Handle is a mutable display/lookup value.
- Public developer profile is shared through ATProto and projected locally.
- Local `core.accounts` rows do not own identity/profile data.
- Git commit author information is not authenticated identity.

### repository invariants

- Repository internal ID never changes.
- Filesystem path is derived from internal storage identity, not user-visible slug.
- AT URI is canonical identity for federated public repositories.

### Go/runtime invariants

- Normal runtime/domain code returns errors; it does not panic.
- `Must*` helpers are reserved for unrecoverable startup composition and are called only from `main`/its immediate composition root.
- Dependencies are constructor-injected; there is no service locator or mutable global application state.
- Context cancellation propagates through database, Git, HTTP, and federation operations.
- Unbounded Git/network payloads are streamed rather than read fully into memory.

### database invariants

- `auth.*` contains sensitive local credential/session state and is never an Electric/public API source.
- `core.*` contains authoritative local forge/operational state.
- `network.*` is rebuildable public federation projection state.
- Git objects/refs remain authoritative in Git, not PostgreSQL.
- The network-wide author key is DID and the network-wide public project key is AT URI.

### federation invariants

- ATProto is not required for an existing local Git clone/push to function.
- Remote ATProto records are untrusted input until validated.
- Local indexes are disposable/rebuildable derived state.
- Federation is eventually consistent.
- Private repository data is not published into the public federation layer.

---

# 77. Open architectural questions

These should be deliberately resolved through ADRs rather than accidentally encoded into implementation.

## ADR-001 — PR authority

How are target-owner decisions such as merge/close represented independently from the PR author's record?

Recommended: separate target-authored status/decision record.

## ADR-002 — repository transfer

If repository ownership moves from one DID to another, can canonical identity stay stable?

Because an AT URI embeds the repo owner DID, transfers require careful semantics.

Possible model:

```text
original project identity
    |
transfer/delegation record
    |
new owner record
```

Do not pretend this is solved by changing one field.

## ADR-003 — forks

Is fork ancestry a field in the child repository's record, an external relationship record, or both?

## ADR-004 — issues state

Who can close an issue?

Issue author owns the issue record, but repository maintainers need authoritative triage state.

Likely use the same proposal/target-decision pattern as PRs.

## ADR-005 — canonical clone endpoint availability

How should clients discover mirrors if the canonical host disappears?

This matters later for resilient federation.

---

# 78. Performance priorities

Optimize in this order:

1. Stream every Git operation.
2. Use local NVMe.
3. Avoid unnecessary Git subprocess spawning for browsing.
4. Limit concurrent pack generation.
5. Keep federation off Git hot paths.
6. Keep Postgres queries indexed/simple.
7. Cache DID/handle resolution.
8. Profile before replacing native Git.

Likely bottlenecks are:

```text
disk throughput
pack generation CPU
network throughput
large repository operations
```

not Go HTTP handler execution.

---

# 79. Go implementation standards

Adenosine should feel like an idiomatic, unsurprising Go codebase.

The standards below are project rules, not optional style suggestions.

## 79.1 Baseline stack

Recommended:

```text
Go current stable
net/http + Chi where routing ergonomics help
pgx
sqlc
golang.org/x/crypto/ssh
golang.org/x/sync/errgroup
slog
OpenTelemetry Go SDK
OTLP export via OpenTelemetry Collector
```

Prefer the standard library where it is already excellent.

Avoid framework abstractions that obscure:

```text
HTTP
context
io.Reader/io.Writer
errors
database transactions
process execution
```

## 79.2 `main` is the composition/startup boundary

Keep `cmd/adenosine/main.go` extremely small.

It is responsible for:

```text
load configuration
construct dependency graph
register shutdown signals
start selected application role
convert unrecoverable startup failure into process termination
```

It contains no repository, Git, REST, or federation business logic.

Adenosine should make the "panic only during startup" rule literal by keeping the panic helper in the `main` package:

```go
func must[T any](value T, err error) T {
    if err != nil {
        panic(err)
    }

    return value
}

func main() {
    ctx, stop := signal.NotifyContext(
        context.Background(),
        os.Interrupt,
        syscall.SIGTERM,
    )
    defer stop()

    cfg := must(config.Load())
    application := must(di.Build(cfg))

    if err := application.Run(ctx); err != nil {
        slog.Error("adenosine stopped", "error", err)
        os.Exit(1)
    }
}
```

`must` follows the normal Must convention:

```text
return the successfully constructed value
OR
panic because startup cannot continue
```

All reusable packages still expose ordinary error-returning APIs.

## 79.3 Panic policy

**`panic` is only permitted at the process startup boundary in `cmd/adenosine`.**

Runtime/domain/application packages do not panic.

Expected failures include:

```text
database unavailable
bad request
invalid Git revision
permission denied
remote host failure
ATProto failure
disk full
repository not found
malformed federated record
```

They must be returned as errors and handled.

Rules:

1. The only intentional `panic(...)` lives in the startup `must`/Must helper in the `main` package.
2. Reusable packages return `(T, error)` or `error`; they do not export panic-based constructors.
3. Never panic from HTTP handlers, SSH sessions, workers, domain services, Git services, federation projectors, stores, or adapters.
4. Never use panic for validation or ordinary invariant failures; return a typed/wrapped error.
5. Do not `recover` panics and convert programming bugs into normal REST responses.
6. A truly unexpected panic should crash the affected process and be visible to the supervisor/observability system rather than silently corrupting state.
7. CI should prevent new `panic(` calls outside the explicitly allow-listed startup file.

This keeps startup wiring concise while guaranteeing that normal request/job control flow is error-based.

## 79.4 Constructors and startup helpers

Constructors that can fail return errors:

```go
func Load() (Config, error)
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error)
func NewFilesystemStore(root string) (*Store, error)
func Build(cfg config.Config) (*Application, error)
```

Construction that cannot fail returns the concrete value:

```go
func NewAuthorizer(store Store) *Authorizer
```

`main` may wrap any startup-only `(T, error)` call with `must(...)`:

```go
cfg := must(config.Load())
application := must(di.Build(cfg))
```

Do not add `MustOpen`, `MustNewServer`, etc. throughout reusable packages merely for convenience. Keeping the panic helper at the executable boundary makes the rule mechanically understandable and enforceable.

## 79.5 Dependency injection

No global mutable dependencies.

No package-level database pool.

No singleton Git service.

No service locator.

No hidden `DefaultClient`.

Dependencies arrive through constructors.

Example:

```go
type Service struct {
    repos     RepositoryStore
    git       GitService
    publisher Publisher
    events    EventWriter
    clock     Clock
}

func NewService(
    repos RepositoryStore,
    git GitService,
    publisher Publisher,
    events EventWriter,
    clock Clock,
) *Service {
    return &Service{
        repos: repos,
        git: git,
        publisher: publisher,
        events: events,
        clock: clock,
    }
}
```

The DI composition root chooses concrete implementations.

## 79.6 Interfaces

Follow:

> Accept interfaces where substitutability is required; return concrete types where practical.

Interfaces should normally live in the package that **consumes** them.

Keep them small.

Good:

```go
type Clock interface {
    Now() time.Time
}

type RepositoryStore interface {
    Find(ctx context.Context, id ID) (Repository, error)
    Create(ctx context.Context, params CreateParams) (Repository, error)
}
```

Avoid speculative 20-method interfaces that merely mirror a concrete implementation.

Do not create interfaces solely so every struct has an interface.

## 79.7 Context

Any operation that can block on:

```text
network
database
filesystem/process
Git
ATProto
```

accepts `context.Context` as the first parameter.

```go
func (s *Service) Get(ctx context.Context, id ID) (Repository, error)
```

Rules:

- never store a request context on a long-lived struct;
- never replace an incoming context with `context.Background()` inside the call chain;
- propagate cancellation to pgx, HTTP requests, Git subprocesses, and remote fetches;
- derive explicit deadlines at transport/job boundaries where policy belongs;
- honor shutdown cancellation.

## 79.8 Errors

Wrap errors with useful operation context:

```go
return Repository{}, fmt.Errorf("load repository %s: %w", id, err)
```

Use `errors.Is` / `errors.As`.

Domain packages expose stable semantic errors:

```go
var ErrNotFound = errors.New("repository not found")
```

or typed errors where additional structured information is needed.

Do not compare error strings.

Do not expose raw Postgres/Git process error text directly to REST clients.

## 79.9 Logging

Use structured `slog`.

Log at boundaries where enough context exists to act on the error.

Avoid:

```text
store logs error
service logs same error
handler logs same error
```

Return errors upward and log once unless an intermediate layer is adding genuinely independent event information.

Prefer:

```go
logger.ErrorContext(
    ctx,
    "git receive-pack failed",
    "repository_id", repo.ID,
    "actor_did", actor,
    "error", err,
)
```

Never log:

```text
access tokens
session tokens
OAuth credentials
private keys
webhook secrets
raw Authorization headers
```

## 79.10 Goroutines and concurrency

Every goroutine has an owner and a shutdown path.

Prefer:

```go
errgroup.WithContext
```

for groups whose lifecycle is tied together.

Never create uncontrolled fire-and-forget goroutines from request handlers.

Bound concurrency for:

```text
Git subprocesses
remote PR fetches
webhook delivery
federation work
maintenance jobs
```

Channels should communicate ownership/work, not replace straightforward synchronous function calls.

The process must drain/cancel workers on graceful shutdown.

## 79.11 Streaming and memory

Use Go's I/O interfaces.

For Git:

```text
io.Reader
io.Writer
io.ReadCloser
```

instead of loading data into:

```text
[]byte
string
```

Packfiles, blobs, archives, and remote Git responses must stream.

Use bounded buffering.

Do not use `io.ReadAll` on untrusted/unbounded request or Git streams.

## 79.12 HTTP handlers

Handlers should be thin:

```text
parse request
authenticate
validate edge representation
call application service
map result/error
write response
```

No direct SQL.

No direct `exec.Command`.

No ATProto publication logic.

No filesystem path construction.

Generated OpenAPI server interfaces provide the REST edge; implementation delegates to domain services.

## 79.13 Database access

Use:

```text
pgxpool
sqlc
explicit transactions
```

Do not use a reflection-heavy ORM.

Transactions are passed explicitly.

A useful pattern:

```go
type DBTX interface {
    Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

or use sqlc's generated transaction support.

Keep transaction scope small and never hold one open during:

```text
ATProto network calls
webhook HTTP requests
remote Git fetch
long-running Git pack generation
```

Use outbox/state transitions instead.

## 79.14 Native process execution

Centralize native Git execution.

Never invoke a shell for dynamic commands.

```go
cmd := exec.CommandContext(ctx, gitBinary, args...)
```

not:

```go
exec.CommandContext(ctx, "sh", "-c", command)
```

Capture stderr with a strict size bound.

Ensure child processes terminate when contexts are cancelled.

Where required on Unix, manage process groups so subprocess trees do not survive request cancellation.

## 79.15 Resource cleanup

Immediately establish cleanup ownership.

Examples:

```go
rows, err := q.List(...)
if err != nil {
    return err
}
defer rows.Close()
```

```go
body, err := client.Do(req)
...
defer body.Body.Close()
```

Check meaningful close/flush errors when they affect correctness.

Do not defer inside an unbounded loop where resources would accumulate.

## 79.16 Package globals and `init`

Avoid mutable package globals.

Avoid `init()` for application setup.

Initialization order should be visible in the composition root.

Package-level constants and immutable sentinel errors are fine.

## 79.17 Configuration

Parse environment/config once at startup into typed structs.

Validate all required relationships before serving traffic.

Do not call `os.Getenv` throughout domain packages.

Use:

```go
cfg := config.MustLoad()
```

from `main`.

Config structs are immutable by convention after startup.

## 79.18 Domain types

Use meaningful types to prevent stringly typed APIs:

```go
type DID string
type RepositoryID uuid.UUID
type ATURI string
type CommitSHA string
type RepositorySlug string
```

Provide validation at boundaries.

Do not over-wrap every primitive if the type adds no semantic safety.

## 79.19 Time and IDs

Inject a clock/ID generator where deterministic testing matters.

```go
type Clock interface {
    Now() time.Time
}

type IDGenerator interface {
    New() uuid.UUID
}
```

Use UUIDv7 for local entity IDs.

Use UTC timestamps and let presentation layers localize them.

## 79.20 JSON

Public JSON shape comes from OpenAPI.

Avoid `map[string]any` for known API objects.

Use `json.RawMessage`/JSONB only for genuinely extensible or externally defined documents such as raw validated ATProto records.

Set intentional limits on request bodies before decoding.

## 79.21 Testing

### Unit tests

Prefer table-driven tests for pure/domain behavior.

Use lightweight fakes implementing narrow injected interfaces.

Avoid mock frameworks unless they provide clear value.

### Database tests

Test stores/queries against real PostgreSQL.

Do not use SQL-string mocks as the main correctness test for sqlc/pgx stores.

### Git tests

Use a real Git binary and temporary bare repositories.

### API tests

Run black-box contract tests against the OpenAPI surface.

### Race tests

CI runs:

```bash
go test -race ./...
```

for packages where supported/practical.

## 79.22 Code quality tooling

CI should run at minimum:

```bash
gofmt -w / format check
go vet ./...
go test ./...
go test -race ./...
staticcheck ./...
```

Use a small, intentional `golangci-lint` configuration if it adds checks without turning contribution into lint-rule archaeology.

Important checks include:

```text
unchecked errors
lost context/cancellation
ineffective assignments
body/resource leaks
shadowing where dangerous
unsafe conversions
```

Pin tool versions used by CI.

Also run a small project check that fails if `panic(` appears outside the allow-listed startup helper in `cmd/adenosine`. This turns the panic policy into an enforceable repository invariant rather than a convention people must remember.

## 79.23 Generated code

Generated files are clearly marked.

Do not hand-edit:

```text
sqlc output
OpenAPI-generated Go edge
OpenAPI-generated TS client
Lexicon-generated types
```

One command regenerates everything:

```bash
make generate
```

CI verifies generation is clean:

```bash
make generate
git diff --exit-code
```

## 79.24 Comments and documentation

Public exported APIs have useful Go doc comments.

Comment **why**, invariants, protocol subtleties, security constraints, and non-obvious Git/ATProto semantics.

Do not narrate obvious code.

Important packages should have `doc.go` where package-level architecture benefits contributors.

## 79.25 Avoid cleverness

Prefer:

```text
clear control flow
small functions
explicit dependencies
standard errors
standard context
standard io
plain SQL
```

over custom frameworks and meta-programming.

The goal is that an experienced Go contributor can open Adenosine and immediately understand where a request, Git operation, or federation event goes.

## 79.26 Example service style

```go
type Service struct {
    repos  RepositoryStore
    git    GitService
    events EventWriter
    clock  Clock
}

func NewService(
    repos RepositoryStore,
    git GitService,
    events EventWriter,
    clock Clock,
) *Service {
    return &Service{
        repos: repos,
        git: git,
        events: events,
        clock: clock,
    }
}

func (s *Service) Create(
    ctx context.Context,
    input CreateInput,
) (Repository, error) {
    if err := input.Validate(); err != nil {
        return Repository{}, fmt.Errorf("validate create repository input: %w", err)
    }

    repo, err := s.repos.Create(ctx, input)
    if err != nil {
        return Repository{}, fmt.Errorf("create repository record: %w", err)
    }

    if err := s.git.Init(ctx, repo.ID); err != nil {
        return Repository{}, fmt.Errorf("initialise git repository %s: %w", repo.ID, err)
    }

    return repo, nil
}
```

The example is intentionally plain.

Reliability comes from clear state transitions/outbox handling around multi-system operations, not from hiding those operations behind framework magic.

# 80. Error model

Define typed/domain errors.

Examples:

```text
repository.ErrNotFound
repository.ErrAlreadyExists
auth.ErrUnauthorized
auth.ErrForbidden
git.ErrInvalidRevision
git.ErrReceivePackFailed
federation.ErrInvalidRecord
```

Transport adapters map them to:

```text
HTTP status
SSH failure
logs
metrics
```

Do not let raw Postgres or `exec.ExitError` objects leak into API contracts.

---

# 81. Database transaction philosophy

Use transactions for local authoritative state.

Do not try to transactionally combine:

```text
Postgres
filesystem
Git refs
ATProto PDS
remote webhooks
```

Instead:

```text
local transaction
   |
durable state/outbox
   |
eventually perform external effect
```

This is critical for reliability.

---

# 82. Backups

A complete instance backup requires:

```text
PostgreSQL
repository storage directory
SSH server host key
instance configuration/secrets
```

Federated public records may be recoverable from ATProto, but local repository Git objects are not automatically recoverable unless mirrored/backed up.

Document:

```text
pg_dump
filesystem snapshot
restore
reindex federation
```

---

# 83. Repository deletion

Use staged deletion:

```text
active
  |
delete requested
  |
deleting
  |
ATProto record cleanup / tombstone
  |
filesystem cleanup
  |
deleted
```

Do not instantly `rm -rf` while an operation may still be active.

Quarantine repository data before final deletion to make accidental recovery possible for a configured period.

---

# 84. Git hooks

Do not allow arbitrary per-repository shell hooks in v1.

They complicate security dramatically.

Adenosine should implement its own controlled hook pipeline around receive-pack:

```text
pre-authorization
pre-ref-policy
Git receive-pack
post-ref event
```

If custom hooks ever exist, make them an advanced trusted-admin feature rather than user-uploaded server-side scripts.

---

# 85. Branch protection later

Architecture should make these possible:

```text
deny force push
deny delete
required reviews
required status checks
signed commits policy
```

but only implement minimal protected branch behavior after PRs exist.

Policies belong in the authorization/ref-update path before successful mutation.

---

# 86. Licensing and project posture

For a network protocol/ecosystem project, strongly consider:

```text
Apache-2.0
```

for the main implementation.

Keep Lexicons openly specified and implementation-independent.

The network becomes more credible if another project can implement:

```text
dev.adenosine.*
```

without running the Adenosine software itself.

The protocol is more important than the reference UI.

---

# 87. Long-term architecture

The interesting end-state is not:

```text
many isolated Adenosine installations
```

It is:

```text
                     AT Protocol
                         |
          +--------------+--------------+
          |              |              |
          v              v              v
   Adenosine A      Adenosine B     Other forge
      AppView          AppView       implementation
          |              |              |
          v              v              v
      Git host A      Git host B      Git host C
```

All participating in the same public developer collaboration graph.

Users should be able to:

- host code wherever they choose;
- move Git hosting without abandoning network identity;
- discover repositories from any compatible AppView;
- create issues across hosts;
- submit PRs across hosts;
- use their existing Git tooling;
- retain a DID-based identity independent of any particular forge provider.

That is the thing worth protecting in every implementation decision.

---

# 88. One-sentence architecture rule

When deciding whether something belongs in Git, ATProto, or Adenosine's local database:

> **If it is source-control state, let Git own it; if it is portable public social/collaboration state, publish it through ATProto; if it is local operational/index/cache state, keep it in Adenosine/Postgres.**

---

# 89. Recommended immediate build sequence

If starting the repository today, implement exactly this sequence:

```text
1. Dockerized local service stack + native developer tooling + `make dev`
2. Local OTel/LGTM backend + one traced health/API request
3. Go server + config + Postgres + DI composition root
4. SQL migrations + sqlc for identity/repository core schema
5. `api/openapi.yaml` with repository/account primitives
6. Generate Go API edge + TypeScript API client
7. Repository model/service
8. Bare repository creation
9. Smart HTTP read path
10. Real `git clone` integration test + trace
11. Smart HTTP write path
12. Real `git push` integration test + trace
13. Token authorization
14. Go SSH server
15. Real SSH clone/push integration tests + traces
16. TanStack Start shell using only generated REST client
17. Minimal repository browser
18. Electric + TanStack DB realtime projection proof
19. ATProto OAuth identity
20. Shared `dev.adenosine.profile`
21. Repository Lexicon
22. Publish repo record
23. Tap-fed indexer
24. Second-instance network discovery test
25. Watch remote repository/profile appear live through Electric
26. Trace a federation event end-to-end through local projection
27. Issues
28. Stars
29. Pull requests
30. Production Docker Compose + `doctor`
31. Railway Pulumi deployment with OTel Collector
32. AWS Pulumi deployment with OTel Collector
33. backup/restore/upgrade acceptance tests
34. hardening + public alpha
```

Do not start with issues, React polish, organizations, CI, replication, or distributed storage.

The defining proof for Adenosine is:

```text
normal Git CLI
      +
self-hosted Git
      +
portable ATProto repository identity
      +
another independent instance discovers it
```

Once that works end-to-end, the rest can be built incrementally.

---

# 89A. Product-level architecture acceptance criteria

Before a major feature is considered complete, ask:

### API-first

Can a third-party client use this feature entirely through documented public APIs?

### Realtime

If the feature is projection-backed and benefits from live updates, can it appear live through Electric without creating a second write API?

### Federation

If the feature is public and portable, does another independent Adenosine instance discover/project it through ATProto?

### Git compatibility

If the feature touches source control, does the normal Git CLI remain standard and unaffected?

### Dependency injection

Can the core behavior be tested by replacing infrastructure dependencies through constructor-injected interfaces?

### Self-hosting

Can a new user run the project with `make dev` and deploy the feature with the official Compose and Pulumi configurations without manual undocumented steps?

### Observability

Can an operator understand the feature through OTel traces, bounded-cardinality metrics, and structured correlated logs without introducing a vendor-specific SDK?

### Replaceability

Could somebody build a completely different frontend against this instance?

Could somebody implement a different compatible forge against the Adenosine Lexicons?

Those are core success criteria, not optional polish.

---

# 90. Reference material

The implementation should track the upstream specifications rather than relying on assumptions.

- OpenTelemetry Go: https://opentelemetry.io/docs/languages/go/
- OpenTelemetry Go instrumentation: https://opentelemetry.io/docs/languages/go/instrumentation/
- OpenTelemetry Collector: https://opentelemetry.io/docs/collector/
- OpenTelemetry Collector Docker: https://opentelemetry.io/docs/collector/install/docker/
- OpenTelemetry semantic conventions: https://opentelemetry.io/docs/specs/semconv/
- Grafana OTel LGTM development image: https://grafana.com/docs/opentelemetry/docker-lgtm/
- Git HTTP protocol: https://git-scm.com/docs/http-protocol
- Git protocol v2: https://git-scm.com/docs/protocol-v2
- Git pack protocol: https://git-scm.com/docs/pack-protocol
- `git-upload-pack`: https://git-scm.com/docs/git-upload-pack
- AT Protocol repository spec: https://atproto.com/specs/repository
- AT Protocol Lexicon spec: https://atproto.com/specs/lexicon
- AT Protocol sync spec: https://atproto.com/specs/sync
- AT Protocol glossary: https://atproto.com/guides/glossary
- ATProto Tap/backfill guide: https://atproto.com/guides/backfilling
- Indigo Go implementation/SDK ecosystem: https://github.com/bluesky-social/indigo

---

# 91. Definition of v0.1

Adenosine v0.1 is complete when two independently running instances can demonstrate:

```text
Alice logs into instance A with ATProto.
Alice's shared Adenosine profile is published/indexed.
Alice creates `hello-world`.
Alice pushes using the normal Git CLI.
Instance A publishes the repository record.
Instance B receives/indexes Alice's profile and repository.
Alice is represented by the same DID/profile on both instances.
Bob discovers Alice's repository on instance B.
Bob clones directly from instance A using normal Git.
Bob creates a federated issue using his DID.
Alice sees that issue on instance A.
```

That is a small enough milestone to build and a strong enough demo to explain why Adenosine exists.

# 92. Step-by-step implementation plan

This is the recommended implementation order. The sequence is intentionally backend- and protocol-first:

```text
foundation
    ↓
Git hosting
    ↓
public REST API
    ↓
identity
    ↓
ATProto federation
    ↓
issues / stars
    ↓
pull requests
    ↓
realtime
    ↓
web UI
    ↓
self-hosting polish
    ↓
cloud deployments
```

The web UI and cloud deployment targets are deliberately near the end. The first priority is to prove that Adenosine is a correct Git host, API-first, DID-first, and capable of participating in one shared federated network.

Do not build significant UI until those boundaries work through black-box tests.

---

## Phase 1 — Repository and development foundation

### Step 1 — Create the repository skeleton

Create the monorepo structure described earlier in this document, including:

```text
cmd/adenosine/
internal/
api/
lexicons/
migrations/
packages/
web/
infra/
scripts/
test/
```

Add the base Go module, Makefile, Dockerfile, Compose files, README, contributing guide, security policy, and license.

**Acceptance:** `make dev` can validate the Docker environment and start the initial development stack.

### Step 2 — Dockerize the local service stack

Implement the canonical:

```bash
make dev
```

flow first. Start Postgres, Adenosine, and the local OpenTelemetry development backend. Add Electric and Tap to the stack when their phases begin rather than forcing incomplete dependencies from day one.

Also implement:

```text
make dev-detached
make down
make reset
make logs
make shell
make psql
make test
make lint
make generate
make doctor
```

Use named volumes for Postgres, Go caches, and Git repository storage.

**Acceptance:** a fresh clone on a machine with Docker, Compose, Make, and Git can run `make dev` without host Go or PostgreSQL.

### Step 3 — Establish Go engineering rules

Implement and document the project-wide rules:

```text
constructor dependency injection
small consumer-owned interfaces
no mutable global dependencies
no service locator
context propagation
errors.Is / errors.As
slog structured logging
UUIDv7 IDs
pgx + sqlc
plain SQL migrations
```

Keep `cmd/adenosine/main.go` as the startup/composition boundary. Intentional `panic(...)` calls are restricted to startup constructors such as `config.Must` and `di.Must`, which fail fast before serving traffic.

Add CI enforcement against `panic(` elsewhere.

**Acceptance:** the application graph can be constructed through injected dependencies and core services unit-tested with fakes.

### Step 4 — Add OpenTelemetry foundation

Set up traces, metrics, correlated structured logs, OTLP export, and graceful provider shutdown before the application becomes complex.

Instrument startup, the health endpoint, and database connectivity.

**Acceptance:** `GET /health/ready` produces a trace visible in the bundled local observability stack and correlated logs include its trace ID.

---

## Phase 2 — Database and public API foundation

### Step 5 — Create the initial Postgres schemas

Create:

```sql
auth
core
network
ops
```

Initially migrate the local tables needed for accounts, sessions, SSH keys, PATs, repositories, collaborators, aliases, and the outbox. Add the rest of the full schema from this document as each feature arrives.

**Acceptance:** `make dev` automatically migrates a completely blank database.

### Step 6 — Add sqlc

Create typed queries for the first local domains:

```text
accounts
repositories
collaborators
access tokens
outbox
```

Generate all code natively with `make generate` using pinned Go/Bun tooling.

**Acceptance:** domain services do not issue raw SQL directly and no ORM is required.

### Step 7 — Establish the OpenAPI-first REST contract

Create `api/openapi.yaml` before substantial REST implementation. Start with health, current identity, repository create/get, and the common auth/error/pagination conventions.

Generate:

```text
Go API server edge
TypeScript API client
OpenAPI JSON
interactive API docs
```

**Acceptance:** `/docs/api` and `/openapi.json` are served and the generated server interface compiles.

---

## Phase 3 — Core Git hosting over HTTP

### Step 8 — Implement the repository domain

Build `internal/repository` and `internal/storage` with immutable repository IDs, slugs, repository states, and a filesystem repository store. Physical paths must use internal storage IDs, not public owner/slug paths.

**Acceptance:** the REST API creates a repository DB record and a bare Git repository on disk.

### Step 9 — Wrap native Git

Build `internal/git` around a central safe command runner supporting `git init --bare`, `upload-pack`, `receive-pack`, ref listing, tree/blob access, history, diff, and merge-base operations.

Enforce no shell, context cancellation, bounded stderr, streaming I/O, and OTel spans.

**Acceptance:** integration tests initialize and inspect real temporary repositories using the real Git binary.

### Step 10 — Implement Git Smart HTTP read path

Implement upload-pack service discovery and RPC. Stream directly between HTTP and Git without buffering packfiles.

**Acceptance:** a black-box test successfully runs:

```bash
git clone http://localhost:8080/alice/test.git
```

and the operation is visible as a trace.

### Step 11 — Implement Git Smart HTTP write path

Implement receive-pack discovery/RPC plus authentication, authorization, and ref-update event capture.

**Acceptance:** real Git CLI tests pass for push, tags, and branch deletion, and a second clone observes the updated refs.

### Step 12 — Add Git HTTP credentials

Implement hashed personal access tokens with scopes, expiry, revocation, and last-used tracking. Public clones remain anonymous; pushes require authorization.

**Acceptance:** unauthorized pushes fail and authorized pushes work with ordinary Git credential handling.

### Step 13 — Add post-push outbox events

Persist `git.refs_updated` / `git.push_received` and keep expensive follow-up work out of the synchronous push path.

**Acceptance:** the Git client receives success before asynchronous post-push processing runs.

---

## Phase 4 — Git over SSH

### Step 14 — Build the SSH server

Implement SSH host keys, user public-key authentication, strict Git command parsing, repository resolution, and shared authorization. Never execute the client's command through a shell.

**Acceptance:** a registered key can establish a Git SSH session against `localhost:2222`.

### Step 15 — Add standard SSH clone and push

Map `git-upload-pack` and `git-receive-pack` SSH commands to native Git using the same repository/auth services as HTTP Git.

**Acceptance:** black-box tests pass for normal `git clone` and `git push` over SSH and produce OTel traces.

---

## Phase 5 — Repository read APIs

### Step 16 — Add tree/blob APIs

Expose documented REST endpoints for branches, tags, tree listing, and blobs. Keep Git as the source of truth and stream large blobs.

**Acceptance:** a third-party REST client has enough data to build a basic repository browser.

### Step 17 — Add commit and diff APIs

Expose history, commit detail, diffs, and merge-base information through the public API.

**Acceptance:** all data needed for a useful code-review client is available without private endpoints.

---

## Phase 6 — ATProto identity

### Step 18 — Implement ATProto OAuth login

Use DID as canonical user identity. Add OAuth, DID resolution, handle caching, and local sessions. Do not create an Adenosine username/password identity.

**Acceptance:** a user signs into one instance using an existing ATProto identity.

### Step 19 — Implement shared developer profiles

Define and support `dev.adenosine.profile`. A local account row remains only a local relationship/cache around a global DID.

**Acceptance:** two independent test AppViews represent the same DID with the same shared developer profile.

### Step 20 — Bind local Git credentials to DID

Map SSH keys, PATs, and browser sessions to the same DID and use one authorization service across REST, Git HTTP, and Git SSH.

**Acceptance:** the application has one identity model across all local protocol surfaces.

---

## Phase 7 — Repository federation

### Step 21 — Define `dev.adenosine.repo`

Define the portable repository record with name, description, default branch, web endpoint, Git HTTPS endpoint, Git SSH endpoint, and timestamps. The record's AT URI becomes the public portable project identity.

**Acceptance:** creating a local public repository publishes a valid repository record.

### Step 22 — Introduce Tap and the federation indexer

Now add Tap to `make dev`. Build the federation consumer/projector and persist raw records, profiles, network repositories, and replay cursors. Projectors must be idempotent.

**Acceptance:** restarting/replaying federation ingestion does not duplicate or corrupt projections.

### Step 23 — Prove two-instance repository discovery

Automate a two-instance environment:

```text
Alice creates repo on A
A publishes it
ATProto carries it
B indexes it
B exposes it through REST
```

**Acceptance:** Instance B returns the repo from `/api/v1/network/repositories` without querying Instance A's database or any central Adenosine directory.

This is the first major proof of the product thesis.

### Step 24 — Resolve remote hosting endpoints

Resolve current web/Git endpoints from repository records while keeping clone traffic pointed directly at the host that actually owns the Git data.

**Acceptance:** a repo is discovered on B and cloned from A using vanilla Git.

---

## Phase 8 — Federated collaboration primitives

### Step 25 — Implement stars

Define `dev.adenosine.star`, add projection tables/counters and public REST endpoints. Stars are authored by the starring user's DID.

**Acceptance:** a star created through B against a repo hosted on A eventually appears on both AppViews.

### Step 26 — Implement issues

Define issue, issue-status, and issue-comment records. Preserve authority: the author owns issue content; the target repo controls authoritative state.

**Acceptance:** Bob on B opens an issue against Alice's repo on A and both instances display the same issue.

### Step 27 — Add comments and moderation

Support comments, replies, deletes/tombstones, blocked DIDs, hidden records, and local moderation rules.

**Acceptance:** an AppView can hide abusive content without mutating the source ATProto record.

---

## Phase 9 — Pull requests

### Step 28 — Define PR/review Lexicons

Define `dev.adenosine.pullRequest`, target-authored PR status records, and reviews. Encode source/target repository, branches, head SHA, title, and body explicitly.

**Acceptance:** PR records validate and project before any official UI exists.

### Step 29 — Fetch remote PR Git objects

For a PR targeting a local repo, resolve its source repo, apply SSRF protection, fetch server-to-server, and retain the head under controlled `refs/adenosine/pull/...` refs.

**Acceptance:** a target host computes a diff for a PR whose source branch is hosted on another instance.

### Step 30 — Implement PR REST APIs

Expose create/list/get/diff/reviews/status through the public API.

**Acceptance:** curl or a third-party CLI can complete the PR review flow without the official web app.

### Step 31 — Implement merge

Start with merge-commit and squash-merge strategies. Authorize, refresh source, verify mergeability, update Git atomically, emit events, and publish target-authored PR state.

**Acceptance:** a federated PR is merged entirely through REST and the result is fetchable with vanilla Git.

---

## Phase 10 — Realtime data layer

### Step 32 — Add Electric

Only after REST semantics and Postgres projections are stable, add Electric to development and production topology as the realtime read path. Never expose `auth.*`.

**Acceptance:** a safe projection-table change reaches a test client in realtime.

### Step 33 — Add documented sync endpoints

Expose safe documented sync surfaces for repositories, profiles, stars, issues, PRs, and reviews. REST remains fully functional for clients that do not use Electric.

**Acceptance:** a third-party client can choose REST-only or realtime sync without relying on official UI internals.

### Step 34 — Prove realtime federation

Test:

```text
remote ATProto event
    ↓
local indexer
    ↓
Postgres projection
    ↓
Electric
    ↓
connected client
```

**Acceptance:** an action on Instance A appears live to a client connected to Instance B without a reload.

---

## Phase 11 — Official web UI

Only now build the full first-party UI. At this point Adenosine should already be useful through Git and the REST API.

### Step 35 — Build the TanStack Start shell

Use TanStack Start/Router and the generated `@adenosine/api-client`. Do not add privileged server functions or hidden Go endpoints.

**Acceptance:** the app could theoretically be hosted independently from the Go server and still function through public interfaces.

### Step 36 — Build repository browsing with TanStack Query

Implement repo pages, trees, blobs, README, branches, tags, commits, and diffs using documented REST APIs.

**Acceptance:** no handwritten private endpoint is required.

### Step 37 — Add TanStack DB + Electric

Use reactive collections for shared profiles, network repo metadata, stars, issues, comments, PRs, reviews, and activity. Keep Git object reads on REST/TanStack Query.

**Acceptance:** cross-instance projected changes appear live in the official UI.

### Step 38 — Build collaboration UI

Add issues, comments, stars, PRs, reviews, merge, shared profiles, and network contribution history. Clearly show whether a repository is hosted locally or remotely without making remote projects second-class.

**Acceptance:** a DID's profile and contribution identity follows them across repos on different instances.

### Step 39 — Build Explore/Search

Build network-wide repository discovery and profile/search surfaces exclusively from the local AppView projection rather than request-time fanout to other instances.

**Acceptance:** a user naturally discovers projects hosted throughout the indexed network.

---

## Phase 12 — Self-hosting and operational hardening

### Step 40 — Productionize Docker Compose

Turn the local architecture into an officially supported single-node self-host deployment with Adenosine, Postgres, Electric, Tap, OTel Collector, Caddy, and persistent volumes.

Add bootstrap, migrate, doctor, backup, restore, and upgrade commands/scripts.

**Acceptance:** a new operator can deploy a production-capable instance without undocumented steps.

### Step 41 — Harden observability

Ship Collector config, dashboards, recommended alerts, and full instrumentation for REST, Git HTTP, SSH, Postgres, outbox, federation, PR fetch, and sync proxy paths.

**Acceptance:** an operator can diagnose a failed push or delayed federation event from traces, metrics, and logs.

### Step 42 — Prove backup/restore

Automate backup and clean restore of Postgres, Git repository storage, SSH host key, and required secrets/configuration.

**Acceptance:** the restored instance passes `adenosine doctor` and existing repositories still clone correctly.

### Step 43 — Security hardening

Audit Git command injection, path traversal, SSRF, webhook SSRF, OAuth/session security, SSH auth, rate limiting, large pack handling, Markdown sanitization, moderation, and resource exhaustion.

**Acceptance:** the documented threat model and public-alpha security checklist are satisfied.

---

## Phase 13 — Cloud deployment automation

Cloud targets come after the self-hosted local architecture is stable. The application must not be designed around Railway or AWS.

### Step 44 — Railway Pulumi deployment

Use the same environment/storage contracts as Compose to provision Adenosine, Postgres, persistent Git storage, Electric, Tap, OTel Collector, and domain/configuration.

**Acceptance:** the Railway deployment passes the same black-box Git/API/federation smoke suite as Compose.

### Step 45 — AWS Pulumi deployment

Create one opinionated AWS stack rather than many variants. Include networking, compute, RDS, POSIX Git storage, HTTPS/SSH routing, Electric, Tap, OTel Collector, and backups.

**Acceptance:** the same Adenosine release passes standard Git CLI, REST, identity, federation, and observability tests on AWS.

### Step 46 — Deployment conformance tests

Every officially supported deployment target must pass one common suite covering health, docs, repository creation, HTTP/SSH clone and push, identity/profile publication, federation, issues/stars/PRs, realtime projection, telemetry export, and backup/restore where applicable.

**Acceptance:** deployment choice does not alter application semantics.

---

## Phase 14 — Public alpha

### Step 47 — Contributor documentation

Ensure the repo clearly documents architecture, database schema, REST API, Lexicons, Go rules, DI, Git transport, federation, realtime, observability, local development, testing, security, and deployment.

**Acceptance:** an experienced contributor can make a meaningful backend change from repository docs alone.

### Step 48 — Prepare good first issues

Prepare bounded tasks across Go, Git, ATProto, OpenAPI, testing, observability, frontend, docs, and deployment. Do not make all newcomer tasks frontend-only.

**Acceptance:** external contributors have obvious entry points into different parts of the project.

### Step 49 — Release v0.1

The release should prove this full scenario:

```text
Alice has one ATProto identity.
Alice signs into Instance A.
Alice creates a repo and pushes with normal Git.
A publishes the project to ATProto.
B discovers Alice's shared profile and repository.

Bob signs into Instance B using his existing ATProto identity.
Bob discovers Alice's repo.
Bob clones directly from A with normal Git.
Bob stars it and opens an issue.
Bob hosts a source repo/branch and opens a PR.

A receives the federated collaboration records.
Alice reviews and merges the PR.
Both AppViews converge on the public state.
Connected clients receive projection changes in realtime.
```

Neither Alice nor Bob creates a new Adenosine identity for each instance.

**Acceptance:** this scenario is automated as far as practical and demonstrated in release documentation.

---

# 93. Implementation ordering rule

When deciding what to build next, use this priority:

```text
1. protocol correctness
2. data model correctness
3. public API completeness
4. federation correctness
5. security and observability
6. realtime
7. official UI
8. deployment convenience
9. scale optimizations
```

Do not let UI convenience create private APIs.

Do not let a cloud deployment dictate domain architecture.

Do not optimize distributed Git storage before a single-node forge has real usage.

The most important architectural proof remains:

> **Two independently operated Adenosine instances, two users with portable DID identities, ordinary Git clients, and one shared public collaboration network.**
