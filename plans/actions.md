# Adenosine Actions — Implementation Plan

> GitHub Actions-compatible CI/CD for Adenosine, with a forge-hosted control plane and replaceable runner compute.

## 1. Goal

A repository should be able to contain a familiar workflow:

```yaml
name: Test

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - run: go test ./...
```

Adenosine plans and schedules it, while a separate runner executes arbitrary code.

```text
Git push / PR / manual trigger
            |
            v
   Adenosine control plane
   ├─ workflow parser
   ├─ planner
   ├─ scheduler
   ├─ secrets/permissions
   ├─ logs/artifacts metadata
   ├─ checks/status
   └─ runner protocol
            |
            v
       Runner compute
   ┌────────┼──────────┐
   v        v          v
 local   dedicated   ephemeral
 runner   runner      cloud VM
```

The forge process itself **never executes user workflow commands**.

## 2. Compatibility strategy

Treat GitHub Actions syntax as a compatibility target, not an immutable external standard.

Support both:

```text
.adenosine/workflows/*.yml
.github/workflows/*.yml
```

Resolution:

1. use `.adenosine/workflows` when present;
2. otherwise fall back to `.github/workflows`.

Every unsupported construct must produce an explicit validation warning/error. Never silently ignore unsupported syntax.

### v0.1 compatibility profile

Triggers:

```text
push
pull_request
workflow_dispatch
```

Workflow/job features:

```text
name
on
env
permissions
concurrency
jobs
runs-on
needs
if
timeout-minutes
continue-on-error
strategy.matrix
strategy.fail-fast
```

Steps:

```text
name
id
run
uses
with
env
if
working-directory
shell
timeout-minutes
continue-on-error
```

Contexts:

```text
github
env
job
steps
runner
matrix
strategy
needs
secrets
vars
inputs
```

Later:

```text
schedule
repository_dispatch
workflow_call
reusable workflows
service containers
job containers
environments
manual approvals
```

## 3. Actions themselves

Support:

```text
Node actions
Docker actions
Composite actions
```

Resolve familiar references:

```yaml
- uses: actions/checkout@v4
```

using an instance-configurable default source:

```text
ADENOSINE_ACTIONS_DEFAULT_SOURCE=https://github.com
```

Also support fully qualified sources:

```yaml
- uses: https://code.example.com/actions/checkout@v4
```

Record the exact:

```text
Git URL
requested ref
resolved commit SHA
action metadata digest
```

for every action used in a historical run.

Instance/repository policy should eventually support:

```text
allow any action
allow approved hosts only
allow approved repositories only
require commit-SHA pinning
```

## 4. Workflow engine spike

Before building a workflow interpreter from scratch, spike `nektos/act`.

Determine whether Adenosine can:

```text
consume it as a Go library
inject custom contexts/events
replace artifact/cache endpoints
replace action source resolution
capture structured step output/logs
propagate cancellation
separate planning from execution
```

Possible outcomes:

```text
A. use upstream act
B. maintain a small adapter/fork
C. build a focused Adenosine engine
```

Do not hand-build GitHub's expression/action runtime until this spike proves it necessary.

## 5. Process boundaries

### Adenosine forge binary

Owns the Actions **control plane**:

```text
workflow discovery
workflow validation
event matching
matrix expansion
job DAG
job queue
runner registration
runner matching
leases
job tokens
secrets/variables
artifact/cache metadata
OIDC
checks/status
REST API
runner protocol
```

### `adenosine-runner`

A separate Go binary/process.

Owns:

```text
registration
job claim
workspace preparation
isolation
action resolution/execution
step execution
logs
artifacts
cache
OIDC requests
cleanup
result reporting
```

Even on the same machine, forge and runner remain separate processes.

## 6. Runner modes

### Local runner

Same physical host, separate process/container.

Good for:

```text
personal repos
trusted private repos
development
```

Not recommended for untrusted public PRs.

### Dedicated runner

Separate runner host.

Recommended normal production self-hosting.

```text
Forge host                 Runner host
Adenosine  <--- HTTPS ---> adenosine-runner
Postgres                   Docker/Podman
Git storage                job containers
```

No inbound public port is required on the runner.

### Ephemeral runner

Fresh VM/microVM per job:

```text
queued job
   |
provision machine
   |
one-job runner registration
   |
execute
   |
upload result
   |
destroy machine
```

Recommended long-term for hosted/public/untrusted workloads.

## 7. Isolation

Runner executor interface:

```go
type Executor interface {
    Prepare(ctx context.Context, job Job) (Environment, error)
    Run(ctx context.Context, env Environment, step Step) (StepResult, error)
    Cleanup(ctx context.Context, env Environment) error
}
```

Initial implementation:

```text
DockerExecutor
```

Optional trusted-only:

```text
HostExecutor
```

Later:

```text
PodmanExecutor
FirecrackerExecutor
KubernetesExecutor
```

Do not expose the forge host's Docker socket directly to workflow containers.

For local Docker development, prefer:

```text
adenosine-runner
      |
dedicated DinD/runner Docker daemon
      |
job containers
```

rather than arbitrary jobs controlling the host Docker daemon.

## 8. Runner labels

Use familiar `runs-on` semantics:

```yaml
runs-on:
  - self-hosted
  - linux
  - arm64
```

A runner advertises labels:

```text
self-hosted
linux
arm64
ubuntu-latest
```

A job is eligible when:

```text
job.required_labels ⊆ runner.labels
```

Runner-local config maps labels to execution environments:

```yaml
environments:
  ubuntu-latest:
    executor: docker
    image: ghcr.io/adenosine/runner-images:ubuntu-latest
```

Keep workflow-visible labels separate from implementation-specific image configuration.

## 9. Runner scopes

Support:

```text
instance
owner/organization
repository
```

Runner selection considers:

```text
scope
labels
online state
free capacity
trust policy
repository policy
```

The scheduler, not the runner, owns placement decisions.

## 10. Public runner protocol

The runner protocol must be:

```text
public
versioned
documented
conformance-tested
independent of the official runner
```

Suggested namespace:

```text
/api/v1/actions/runner/v1/...
```

This creates room for third-party providers similar to Blacksmith/Depot.

Use outbound HTTPS from runner to server. No inbound runner port.

v1 can use long polling; streaming transport can be added later.

## 11. Registration

Generate one-time scoped registration tokens.

Example:

```bash
adenosine-runner register \
  --instance https://code.example.com \
  --token <registration-token>
```

Runner sends:

```json
{
  "name": "builder-01",
  "labels": ["linux", "x64", "ubuntu-latest"],
  "version": "0.1.0",
  "max_concurrency": 2
}
```

Server returns a durable runner credential.

Registration token:

```text
short-lived
single-use
scope-bound
```

Runner credential:

```text
hashed at rest
revocable
not a user token
cannot read arbitrary repositories
```

## 12. Job claiming

Runner long-polls:

```text
POST /api/v1/actions/runner/v1/jobs/claim
```

with:

```text
runner ID
labels/capabilities
available slots
runner/protocol version
```

Server returns a leased job envelope containing only what that runner/job needs.

A job attempt has:

```text
attempt ID
lease ID
lease expiry
job-scoped execution token
trace context
```

Runner heartbeats extend the lease.

## 13. Failure semantics

CI cannot guarantee exactly-once external side effects.

If a runner deploys something and dies before reporting success, the control plane cannot know whether deployment happened.

Therefore:

```text
do not blindly auto-rerun interrupted deployment jobs
mark attempts explicitly
support manual/policy rerun
use concurrency/environment protection
fence stale attempt results
```

A stale attempt must never overwrite a newer attempt's server-side state.

## 14. Cancellation

Cancellation propagates through runner heartbeat/session responses.

Runner:

```text
graceful stop
bounded wait
force kill
cleanup
report cancelled
```

All Go execution paths use context cancellation.

## 15. Trigger architecture

Consume internal Adenosine events:

```text
git.push_received
pull_request.created
pull_request.synchronized
workflow.dispatch_requested
```

Do not trigger Actions by making HTTP calls back into the same application.

Local Git/PR state creates durable internal events, which the Actions trigger evaluator consumes.

## 16. Cross-instance PRs

If:

```text
target repo -> Instance A
source repo -> Instance B
```

Instance A controls target-repo CI.

```text
federated PR update
      |
Instance A indexer
      |
target host refreshes PR source Git objects
      |
Actions trigger on A
      |
A's allowed runners execute
```

Execution is **not federated**.

The target repository host controls:

```text
workflow
secrets
runner trust
required checks
compute cost
merge rules
```

## 17. Fork/PR security

For untrusted fork PRs, default to:

```text
workflow definition from trusted target/base
code under test from PR source/head
read-only job token
no secrets
no privileged OIDC
restricted cache writes
no local-host runner unless explicitly allowed
```

Do not implement a `pull_request_target` equivalent until it has its own threat model.

## 18. Job token

Each job gets a short-lived scoped token analogous to `GITHUB_TOKEN`.

Expose native:

```text
ADENOSINE_TOKEN
```

and compatibility:

```text
GITHUB_TOKEN
```

where appropriate.

Workflow permissions:

```yaml
permissions:
  contents: read
  pull-requests: write
```

map to Adenosine capabilities.

Untrusted fork PRs cannot escalate these defaults.

## 19. Environment compatibility

Populate common GitHub-compatible variables:

```text
GITHUB_ACTIONS
GITHUB_WORKFLOW
GITHUB_RUN_ID
GITHUB_RUN_NUMBER
GITHUB_JOB
GITHUB_ACTOR
GITHUB_REPOSITORY
GITHUB_SHA
GITHUB_REF
GITHUB_HEAD_REF
GITHUB_BASE_REF
GITHUB_WORKSPACE
GITHUB_SERVER_URL
GITHUB_API_URL
RUNNER_OS
RUNNER_ARCH
RUNNER_TEMP
```

Also native:

```text
ADENOSINE_ACTIONS
ADENOSINE_INSTANCE_URL
ADENOSINE_REPOSITORY_URI
ADENOSINE_ACTOR_DID
ADENOSINE_RUN_ID
ADENOSINE_JOB_ID
```

Do not pretend unsupported GitHub API behavior exists merely because an environment variable is present.

## 20. Secrets and variables

Secrets:

```text
repository
owner/org later
environment later
```

Store encrypted with envelope encryption.

GET/list APIs return names/metadata only, never secret values.

Only applicable secrets are delivered through the short-lived job context.

Secret masking in logs is defense in depth, not a security boundary.

Variables are non-secret and can use the same scopes.

## 21. OIDC

Each Adenosine instance should be an OIDC issuer for workflow jobs.

Endpoints:

```text
/.well-known/openid-configuration
/actions/oidc/jwks
/api/v1/actions/oidc/token
```

Workflow must request:

```yaml
permissions:
  id-token: write
```

Useful claims:

```text
iss
aud
sub
repository_uri
repository_id
owner_did
workflow_ref
workflow_sha
run_id
job_id
ref
sha
event_name
environment
actor_did
```

Tokens:

```text
short-lived
job-bound
audience-bound
signed with rotating keys
```

This allows AWS/GCP/Azure-style temporary workload credentials instead of long-lived cloud secrets.

## 22. Generic checks/status subsystem

Actions must not own PR checks.

Build a generic check system first:

```go
type CheckPublisher interface {
    Started(ctx context.Context, check Check) error
    Completed(ctx context.Context, result CheckResult) error
}
```

Producers can be:

```text
Adenosine Actions
Jenkins
Buildkite
CircleCI
custom CI
```

PR merge protection consumes generic check state.

Required checks are tied to the current PR head SHA.

## 23. Federated check visibility

Actions execution stays local to the target repo host, but public safe status metadata may federate.

Potential Lexicon:

```text
dev.adenosine.check
```

Safe fields:

```text
repo URI
commit SHA
check name
status
conclusion
started/completed time
details URL
```

Never federate:

```text
secrets
private logs
runner credentials
execution tokens
private artifacts
```

## 24. Data model

Create:

```sql
CREATE SCHEMA actions;
```

Core tables:

```text
actions.repository_settings
actions.workflows
actions.workflow_runs
actions.jobs
actions.job_dependencies
actions.job_attempts
actions.job_steps
actions.runners
actions.runner_labels
actions.runner_registration_tokens
actions.concurrency_locks
actions.secrets
actions.variables
actions.artifacts
actions.caches
actions.log_streams
actions.oidc_keys
```

Git remains canonical for workflow YAML; DB workflow rows are indexes/metadata.

### `actions.repository_settings`

```sql
CREATE TABLE actions.repository_settings (
    repository_id UUID PRIMARY KEY
        REFERENCES core.repositories(id) ON DELETE CASCADE,

    enabled BOOLEAN NOT NULL DEFAULT true,

    workflow_permissions TEXT NOT NULL DEFAULT 'read',
    allow_fork_workflows BOOLEAN NOT NULL DEFAULT true,
    require_fork_approval BOOLEAN NOT NULL DEFAULT true,
    allow_fork_secrets BOOLEAN NOT NULL DEFAULT false,
    allow_fork_write_token BOOLEAN NOT NULL DEFAULT false,

    artifact_retention_days INTEGER NOT NULL DEFAULT 30,
    log_retention_days INTEGER NOT NULL DEFAULT 30,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

### `actions.workflows`

```sql
CREATE TABLE actions.workflows (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL
        REFERENCES core.repositories(id) ON DELETE CASCADE,

    path TEXT NOT NULL,
    name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    last_seen_sha TEXT,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    UNIQUE(repository_id, path)
);
```

### `actions.workflow_runs`

```sql
CREATE TABLE actions.workflow_runs (
    id UUID PRIMARY KEY,
    repository_id UUID NOT NULL
        REFERENCES core.repositories(id) ON DELETE CASCADE,

    workflow_id UUID
        REFERENCES actions.workflows(id) ON DELETE SET NULL,

    run_number BIGINT NOT NULL,
    attempt_number INTEGER NOT NULL DEFAULT 1,

    event_name TEXT NOT NULL,
    event_payload JSONB NOT NULL,
    actor_did TEXT,

    head_sha TEXT NOT NULL,
    head_ref TEXT,
    base_sha TEXT,
    base_ref TEXT,

    workflow_path TEXT NOT NULL,
    workflow_sha TEXT NOT NULL,
    workflow_digest TEXT NOT NULL,
    workflow_source TEXT NOT NULL,
    normalized_plan JSONB NOT NULL,

    status TEXT NOT NULL,
    conclusion TEXT,
    concurrency_key TEXT,

    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    UNIQUE(repository_id, run_number, attempt_number)
);

CREATE INDEX workflow_runs_repo_created_idx
ON actions.workflow_runs(repository_id, created_at DESC);

CREATE INDEX workflow_runs_sha_idx
ON actions.workflow_runs(repository_id, head_sha);
```

### `actions.jobs`

```sql
CREATE TABLE actions.jobs (
    id UUID PRIMARY KEY,
    workflow_run_id UUID NOT NULL
        REFERENCES actions.workflow_runs(id) ON DELETE CASCADE,

    job_key TEXT NOT NULL,
    name TEXT NOT NULL,

    matrix JSONB,
    required_labels TEXT[] NOT NULL,

    status TEXT NOT NULL,
    conclusion TEXT,

    runner_id UUID,
    current_attempt_id UUID,

    timeout_seconds INTEGER NOT NULL,
    continue_on_error BOOLEAN NOT NULL DEFAULT false,

    created_at TIMESTAMPTZ NOT NULL,
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    UNIQUE(workflow_run_id, job_key)
);

CREATE INDEX actions_jobs_queue_idx
ON actions.jobs(queued_at)
WHERE status = 'queued';
```

### Dependencies

```sql
CREATE TABLE actions.job_dependencies (
    job_id UUID NOT NULL
        REFERENCES actions.jobs(id) ON DELETE CASCADE,

    depends_on_job_id UUID NOT NULL
        REFERENCES actions.jobs(id) ON DELETE CASCADE,

    PRIMARY KEY(job_id, depends_on_job_id)
);
```

### Attempts

```sql
CREATE TABLE actions.job_attempts (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL
        REFERENCES actions.jobs(id) ON DELETE CASCADE,

    attempt_number INTEGER NOT NULL,

    runner_id UUID,
    lease_id UUID,
    lease_expires_at TIMESTAMPTZ,
    last_heartbeat_at TIMESTAMPTZ,

    status TEXT NOT NULL,
    conclusion TEXT,

    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    failure_code TEXT,
    failure_message TEXT,

    UNIQUE(job_id, attempt_number)
);
```

### Steps

```sql
CREATE TABLE actions.job_steps (
    id UUID PRIMARY KEY,
    job_attempt_id UUID NOT NULL
        REFERENCES actions.job_attempts(id) ON DELETE CASCADE,

    step_index INTEGER NOT NULL,
    step_key TEXT,
    name TEXT NOT NULL,

    status TEXT NOT NULL,
    conclusion TEXT,

    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    exit_code INTEGER,

    UNIQUE(job_attempt_id, step_index)
);
```

### Runners

```sql
CREATE TABLE actions.runners (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,

    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,

    credential_hash BYTEA NOT NULL UNIQUE,

    version TEXT NOT NULL,
    os TEXT,
    arch TEXT,

    max_concurrency INTEGER NOT NULL DEFAULT 1,

    state TEXT NOT NULL,
    ephemeral BOOLEAN NOT NULL DEFAULT false,

    registered_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
```

### Runner labels

```sql
CREATE TABLE actions.runner_labels (
    runner_id UUID NOT NULL
        REFERENCES actions.runners(id) ON DELETE CASCADE,

    label TEXT NOT NULL,

    PRIMARY KEY(runner_id, label)
);

CREATE INDEX runner_labels_label_idx
ON actions.runner_labels(label);
```

### Registration tokens

```sql
CREATE TABLE actions.runner_registration_tokens (
    id UUID PRIMARY KEY,
    token_hash BYTEA NOT NULL UNIQUE,

    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,

    created_by_did TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);
```

### Concurrency locks

```sql
CREATE TABLE actions.concurrency_locks (
    repository_id UUID NOT NULL
        REFERENCES core.repositories(id) ON DELETE CASCADE,

    concurrency_key TEXT NOT NULL,

    workflow_run_id UUID NOT NULL
        REFERENCES actions.workflow_runs(id) ON DELETE CASCADE,

    acquired_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY(repository_id, concurrency_key)
);
```

### Secrets

```sql
CREATE TABLE actions.secrets (
    id UUID PRIMARY KEY,

    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    name TEXT NOT NULL,

    ciphertext BYTEA NOT NULL,
    key_version INTEGER NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    UNIQUE(scope_type, scope_id, name)
);
```

### Variables

```sql
CREATE TABLE actions.variables (
    id UUID PRIMARY KEY,

    scope_type TEXT NOT NULL,
    scope_id TEXT NOT NULL,
    name TEXT NOT NULL,
    value TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    UNIQUE(scope_type, scope_id, name)
);
```

### Artifacts

```sql
CREATE TABLE actions.artifacts (
    id UUID PRIMARY KEY,

    workflow_run_id UUID NOT NULL
        REFERENCES actions.workflow_runs(id) ON DELETE CASCADE,

    job_id UUID
        REFERENCES actions.jobs(id) ON DELETE SET NULL,

    name TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,

    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);
```

### Caches

```sql
CREATE TABLE actions.caches (
    id UUID PRIMARY KEY,

    repository_id UUID NOT NULL
        REFERENCES core.repositories(id) ON DELETE CASCADE,

    cache_key TEXT NOT NULL,
    version TEXT NOT NULL,
    ref TEXT,

    object_key TEXT NOT NULL UNIQUE,
    size_bytes BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,
    last_accessed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ
);

CREATE INDEX action_cache_lookup_idx
ON actions.caches(repository_id, cache_key, version);
```

### Log metadata

```sql
CREATE TABLE actions.log_streams (
    id UUID PRIMARY KEY,

    job_attempt_id UUID NOT NULL
        REFERENCES actions.job_attempts(id) ON DELETE CASCADE,

    object_prefix TEXT NOT NULL,
    byte_length BIGINT NOT NULL DEFAULT 0,
    chunk_count INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,

    UNIQUE(job_attempt_id)
);
```

### OIDC keys

```sql
CREATE TABLE actions.oidc_keys (
    id UUID PRIMARY KEY,

    key_id TEXT NOT NULL UNIQUE,
    private_ciphertext BYTEA NOT NULL,
    public_jwk JSONB NOT NULL,

    created_at TIMESTAMPTZ NOT NULL,
    not_before TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ
);
```

## 25. Storage

Use:

```text
Postgres
  -> scheduler/run/runner metadata

Git POSIX storage
  -> workflow YAML
  -> source/action repos

ObjectStore
  -> logs
  -> artifacts
  -> caches
```

Reuse Adenosine's object-store abstraction:

```text
FilesystemObjectStore
S3ObjectStore
```

Self-hosters do not need S3.

Cloud installations should usually use S3-compatible object storage for Actions logs/artifacts/cache.

## 26. Artifacts

Large artifact bytes should not pass through Go application memory.

Preferred cloud flow:

```text
Runner
  |
request upload capability
  |
Adenosine checks job/quota
  |
signed upload URL
  |
Runner -----------> Object Store
  |
finalize metadata
```

Filesystem mode can stream via Adenosine.

Record SHA-256 for artifacts.

## 27. Cache

Cache is optional acceleration.

Use familiar:

```text
key
restore-keys
version
ref/trust scope
```

Protect against fork cache poisoning.

Untrusted fork jobs should not write into cache namespaces later consumed by privileged target-branch jobs unless policy explicitly allows it.

## 28. Logs

Workflow output is not ordinary application logging.

Runner sends ordered, bounded chunks:

```text
attempt ID
sequence
offset
compressed bytes
```

Server handles duplicate chunks idempotently.

Active log tail:

```text
SSE/WebSocket/stream endpoint
```

Completed logs compact to object storage.

Do not sync raw job log bytes through Electric.

## 29. Public REST API

All Actions user-facing APIs live in `api/openapi.yaml`.

### Workflows

```text
GET  /api/v1/repositories/{owner}/{repo}/actions/workflows
GET  /api/v1/repositories/{owner}/{repo}/actions/workflows/{workflow}
POST /api/v1/repositories/{owner}/{repo}/actions/workflows/{workflow}/dispatch
POST /api/v1/repositories/{owner}/{repo}/actions/validate
```

### Runs/jobs

```text
GET  /api/v1/repositories/{owner}/{repo}/actions/runs
GET  /api/v1/repositories/{owner}/{repo}/actions/runs/{run}
POST /api/v1/repositories/{owner}/{repo}/actions/runs/{run}/cancel
POST /api/v1/repositories/{owner}/{repo}/actions/runs/{run}/rerun
POST /api/v1/repositories/{owner}/{repo}/actions/runs/{run}/rerun-failed

GET  /api/v1/repositories/{owner}/{repo}/actions/runs/{run}/jobs
GET  /api/v1/repositories/{owner}/{repo}/actions/jobs/{job}
GET  /api/v1/repositories/{owner}/{repo}/actions/jobs/{job}/logs
```

### Artifacts/cache

```text
GET    /api/v1/repositories/{owner}/{repo}/actions/artifacts
GET    /api/v1/repositories/{owner}/{repo}/actions/artifacts/{artifact}
GET    /api/v1/repositories/{owner}/{repo}/actions/artifacts/{artifact}/download
DELETE /api/v1/repositories/{owner}/{repo}/actions/artifacts/{artifact}

GET    /api/v1/repositories/{owner}/{repo}/actions/caches
DELETE /api/v1/repositories/{owner}/{repo}/actions/caches/{cache}
```

### Runners

```text
GET    /api/v1/repositories/{owner}/{repo}/actions/runners
POST   /api/v1/repositories/{owner}/{repo}/actions/runners/registration-token
DELETE /api/v1/repositories/{owner}/{repo}/actions/runners/{runner}
```

### Secrets/variables

```text
GET    /api/v1/repositories/{owner}/{repo}/actions/secrets
PUT    /api/v1/repositories/{owner}/{repo}/actions/secrets/{name}
DELETE /api/v1/repositories/{owner}/{repo}/actions/secrets/{name}

GET    /api/v1/repositories/{owner}/{repo}/actions/variables
PUT    /api/v1/repositories/{owner}/{repo}/actions/variables/{name}
DELETE /api/v1/repositories/{owner}/{repo}/actions/variables/{name}
```

Secret reads return metadata only.

## 30. Runner protocol endpoints

Suggested v1:

```text
POST /api/v1/actions/runner/v1/register
POST /api/v1/actions/runner/v1/jobs/claim
POST /api/v1/actions/runner/v1/jobs/{job}/heartbeat
POST /api/v1/actions/runner/v1/jobs/{job}/started

POST /api/v1/actions/runner/v1/jobs/{job}/steps/{step}/started
POST /api/v1/actions/runner/v1/jobs/{job}/steps/{step}/completed

POST /api/v1/actions/runner/v1/jobs/{job}/logs

POST /api/v1/actions/runner/v1/jobs/{job}/artifacts/request-upload
POST /api/v1/actions/runner/v1/jobs/{job}/artifacts/finalize

POST /api/v1/actions/runner/v1/jobs/{job}/oidc-token
POST /api/v1/actions/runner/v1/jobs/{job}/complete
```

Protocol docs must specify:

```text
authentication
idempotency
lease rules
retry rules
cancellation
version negotiation
log ordering
artifact protocol
error codes
```

## 31. Scheduler

Scheduler owns:

```text
DAG readiness
matrix jobs
conditions
runner matching
capacity
concurrency groups
leases
timeouts
cancellation
```

Runner is a worker, not a scheduler.

Job state:

```text
waiting_dependencies
waiting_approval
queued
in_progress
completed
```

Conclusion:

```text
success
failure
cancelled
skipped
timed_out
interrupted
```

Use DB locking/uniqueness for concurrency correctness; not just in-memory state.

## 32. OpenTelemetry

Instrument server + runner.

Important traces:

```text
workflow.trigger
workflow.parse
workflow.plan
job.enqueue
job.claim
job.execute
step.execute
cache.restore
cache.save
artifact.upload
oidc.issue
check.publish
job.complete
```

Propagate trace context in the job envelope.

Use producer/consumer span semantics across queue delay.

Metrics:

```text
adenosine.actions.jobs.queued
adenosine.actions.jobs.running
adenosine.actions.queue.duration
adenosine.actions.scheduler.no_runner
adenosine.actions.runner.online
adenosine.actions.runner.capacity
adenosine.actions.cache.hit
adenosine.actions.artifact.bytes
adenosine.runner.job.duration
adenosine.runner.step.duration
adenosine.runner.cleanup.failures
```

No repository ID/DID/job ID/SHA metric labels.

Do not export raw workflow command output into OTel by default.

## 33. Go standards

Actions follows core Adenosine Go rules:

```text
constructor DI
small consumer-owned interfaces
context first
no service locator
no mutable globals
errors.Is / errors.As
pgx + sqlc
slog + OTel
bounded goroutines
stream large payloads
```

Runtime code does not panic.

Packages that construct required startup values own a public `Must` function backed by a
private error-returning implementation. Runtime code does not panic. Do not add a generic
helper in `main`; the composition root should remain plain.

```go
func Must() Config {
    value, err := load()
    if err != nil {
        panic(err)
    }
    return value
}
```

## 34. Suggested repository structure

```text
cmd/
├── adenosine/
└── adenosine-runner/

internal/actions/
├── workflow/
├── planner/
├── scheduler/
├── runs/
├── checks/
├── secrets/
├── artifacts/
├── cache/
├── logs/
├── oidc/
├── runnerproto/
└── store/

runner/
├── app/
├── client/
├── executor/
│   ├── docker/
│   └── host/
├── engine/
├── workspace/
├── logs/
├── artifacts/
├── cache/
└── observability/

docs/actions/
├── architecture.md
├── compatibility.md
├── workflow-syntax.md
├── runner-protocol.md
├── runner-security.md
├── self-hosted-runners.md
├── oidc.md
└── troubleshooting.md

test/actions/
├── workflows/
├── compatibility/
├── runner/
├── security/
└── e2e/
```

## 35. Compatibility tests

Maintain fixtures for:

```text
basic-run
checkout
setup-go
env
expressions
needs
matrix
continue-on-error
timeouts
permissions
secrets
artifacts
cache
docker-action
node-action
composite-action
pull-request
workflow-dispatch
concurrency
```

Where feasible, compare behavior against GitHub Actions reference semantics.

Compatibility means execution behavior, not merely YAML parsing.

## 36. Failure tests

Test:

```text
runner process dies
forge restarts
database disconnects
object store fails
Docker daemon dies
network link drops
job times out
step times out
job cancelled
duplicate log chunk
duplicate completion
runner revoked during execution
```

The result must be deterministic and recoverable.

## 37. Local environment

The existing Adenosine rule remains:

```bash
make dev
```

is enough.

Once Actions is implemented, `make dev` also starts:

```text
adenosine-runner
dedicated development Docker/DinD execution environment
```

Normal `make test`, `make lint`, and `make generate` use native host Go and Bun. Docker
owns local services, `doctor`/`shell`/`psql`, and black-box E2E.

Optional debugging:

```text
make actions-test
make actions-e2e
make actions-runner-logs
make actions-reset
```

## 38. Realtime

Use:

```text
REST = write/control contract
Electric = realtime projected state
```

Electric-safe data:

```text
workflow runs
jobs
steps
runner status
artifact metadata
```

Never Electric-sync:

```text
secrets
runner credentials
job execution tokens
OIDC private keys
```

Live log bytes use a dedicated stream.

## 39. UI — near the end

Only build full UI after Actions works through REST + runner protocol.

TanStack Start/Query/DB UI:

```text
Actions tab
workflow list
run list
run DAG
live logs
cancel/rerun
artifacts
cache management
runner management
secrets/variables
compatibility validation
```

The UI uses only generated public APIs plus documented realtime/log streams.

Runner settings should make trust explicit:

```text
Local runner
  trusted workloads only

Dedicated runners
  recommended self-hosted

Managed runners
  ephemeral hosted compute
```

## 40. Self-hosting — after core correctness

Small trusted setup:

```text
same machine
├─ Adenosine
├─ Postgres
├─ Git storage
└─ separate runner container + job isolation
```

Recommended production:

```text
Forge host
├─ Adenosine
├─ Postgres
└─ Git storage

Runner host
├─ adenosine-runner
└─ Docker/Podman
```

Public/untrusted:

```text
ephemeral one-job runners
```

## 41. Third-party runner ecosystem

Publish protocol docs + conformance tests so external providers can implement:

```text
runner registration
claim
heartbeat
logs
artifacts
OIDC
completion
cancellation
```

A third party should be able to offer:

```text
"Adenosine runners"
```

without forking Adenosine.

## 42. JIT runners

Later endpoint:

```text
POST /api/v1/actions/runners/jit-config
```

JIT config is:

```text
short-lived
specific scope
specific labels
optionally specific job
one registration
one job
auto-revoked
```

This becomes the autoscaling primitive for managed providers.

## 43. Adenosine Cloud runners

Keep hosted compute outside the open-source forge domain.

```text
Customer's normal Adenosine instance
             |
             | open runner/provider protocol
             v
     Adenosine Cloud Runners
             |
       ephemeral compute
```

The customer can still choose:

```text
own runner
third-party runner
Adenosine managed runner
```

The open-source instance does not become SaaS-multitenant because managed runners exist.

## 44. Step-by-step implementation order

UI and cloud compute intentionally come late.

### Phase 1 — Architecture and compatibility

1. Write ADR: control plane vs runner.
2. Write ADR: open/versioned runner protocol.
3. Spike `nektos/act`.
4. Decide use/adapt/fork/build.
5. Define v0.1 workflow compatibility profile.
6. Create workflow fixture suite.

**Exit:** Adenosine can parse/validate representative workflows and has a documented compatibility boundary.

### Phase 2 — Generic checks

7. Build generic commit/check REST API.
8. Add checks to PR views/data model.
9. Add required-check merge rules.
10. Prove a fake external CI can publish checks.

**Exit:** Actions is not required for CI integration.

### Phase 3 — Control-plane persistence/API

11. Add `actions` Postgres schema.
12. Add sqlc stores.
13. Add workflow/run/job OpenAPI endpoints.
14. Add runner-management OpenAPI endpoints.
15. Add repository Actions settings.
16. Add OTel spans/metrics for the empty scheduler.

**Exit:** control-plane state is API-first and observable.

### Phase 4 — Workflow planning

17. Discover workflow files directly from Git.
18. Snapshot workflow YAML/SHA/digest.
19. Parse/normalize plan.
20. Implement push trigger.
21. Implement `workflow_dispatch`.
22. Implement safe `pull_request` trigger.
23. Implement `needs` DAG.
24. Implement matrix expansion.
25. Implement conditions.
26. Implement concurrency-group planning.

**Exit:** events deterministically create queued jobs without a runner.

### Phase 5 — Runner protocol

27. Implement one-time registration tokens.
28. Implement runner registration.
29. Implement labels/scopes/capacity.
30. Implement long-poll job claim.
31. Implement leases/heartbeats.
32. Implement cancellation.
33. Add protocol version negotiation.
34. Publish runner protocol docs.

**Exit:** a fake remote runner can reliably claim/complete a job.

### Phase 6 — Real runner

35. Create separate `adenosine-runner` Go binary.
36. Add Docker executor.
37. Add workspace lifecycle.
38. Integrate selected workflow engine.
39. Execute `run:` steps.
40. Execute composite actions.
41. Execute Node actions.
42. Execute Docker actions.
43. Resolve local actions.
44. Resolve remote Git actions.
45. Record resolved action SHAs.

**Exit:** `push -> workflow -> runner -> test result` works end-to-end.

### Phase 7 — Security identity

46. Implement job-scoped token.
47. Implement workflow `permissions`.
48. Implement repo secrets.
49. Implement variables.
50. Implement fork read-only/no-secret policy.
51. Add runner resource limits.
52. Add runner cleanup/security tests.

**Exit:** trusted and untrusted jobs receive different capabilities correctly.

### Phase 8 — Logs/artifacts/cache

53. Implement ordered streamed log chunks.
54. Add live log endpoint.
55. Store completed logs in ObjectStore.
56. Add artifact upload/finalize/download.
57. Add SHA-256 artifact metadata.
58. Add cache lookup/upload.
59. Add fork cache-poisoning protections.
60. Add retention/quotas.

**Exit:** normal CI workflows can persist output without bloating Postgres.

### Phase 9 — OIDC and concurrency

61. Implement OIDC discovery/JWK endpoints.
62. Implement job OIDC tokens.
63. Test with a local relying party.
64. Implement concurrency locks.
65. Implement cancel-in-progress.
66. Add manual rerun/interrupted-attempt semantics.

**Exit:** deployment workflows can use short-lived identity rather than static cloud secrets.

### Phase 10 — Federated status

67. Wire Actions into generic checks.
68. Design `dev.adenosine.check` if needed.
69. Federate only safe public check metadata.
70. Verify another instance can display target-repo check state.

**Exit:** PRs look complete across the Adenosine network without federating execution internals.

### Phase 11 — Resilience and observability

71. Add full OTel control-plane-to-runner traces.
72. Add queue/runner utilization metrics.
73. Add failure injection tests.
74. Test server restart during queued/running jobs.
75. Test runner loss.
76. Test ObjectStore outage.
77. Test cancellation/timeouts.
78. Add Actions dashboards/alerts.

**Exit:** operators can explain why a job is waiting/failing.

### Phase 12 — Realtime

79. Add Electric-safe run/job/step projections.
80. Verify REST-only client remains fully functional.
81. Verify realtime client receives state transitions.
82. Keep logs on dedicated streaming endpoint.

**Exit:** realtime is an optional read accelerator, not a dependency.

### Phase 13 — UI

83. Build Actions TanStack Start routes.
84. Build workflow/run list.
85. Build job DAG.
86. Build live log UI.
87. Build rerun/cancel.
88. Build artifact/cache UI.
89. Build runner settings.
90. Build secrets/variables UI.
91. Build compatibility validator UX.

**Exit:** every UI operation maps to the public REST/sync contract.

### Phase 14 — Easy self-hosting

92. Add runner to `make dev`.
93. Add trusted local runner Compose profile.
94. Add `adenosine-runner register`.
95. Add runner `doctor`.
96. Document dedicated runner deployment.
97. Add backup/retention guidance for Actions object data.

**Exit:** a self-hoster can enable Actions without assembling bespoke infrastructure.

### Phase 15 — Ephemeral/third-party compute

98. Implement JIT one-job runner registration.
99. Publish runner conformance test suite.
100. Build a minimal external autoscaling proof.
101. Validate an independently provisioned runner can complete a job.

**Exit:** compute is demonstrably replaceable.

### Phase 16 — Managed cloud runners

102. Build a provider/provisioner outside core forge.
103. Start with one cloud/compute target.
104. Create fresh compute per job.
105. Add runner classes.
106. Add immutable usage metering.
107. Add billing in the Adenosine Cloud control plane.
108. Add managed runner UI as one option alongside self-hosted.

**Exit:** users can pay for compute without surrendering ownership of their Adenosine instance.

## 45. v0.1 definition

v0.1 is compelling when:

```text
Alice has a normal Adenosine repo.

.github/workflows/test.yml exists.

Alice pushes.

Adenosine:
- discovers workflow
- creates run
- plans jobs
- queues job

A separate registered runner:
- claims job
- creates isolated environment
- checks out exact SHA
- runs tests
- streams logs
- reports result

A generic check appears on the commit/PR.

A second Adenosine instance displaying the federated PR can see:
✓ test

No workflow command ran inside the forge process.
No Adenosine Cloud service was required.
Vanilla Git still works normally.
```

## 46. Non-negotiable invariants

1. Forge process never executes user workflow commands.
2. Runner compute is replaceable.
3. Runner protocol is public/versioned.
4. GitHub Actions compatibility is explicit and tested.
5. Official UI uses only public APIs.
6. Generic checks exist independently of Actions.
7. Execution belongs to the target repository host; it is not federated arbitrary compute.
8. Public safe check results may federate; secrets/logs/runner internals do not.
9. Fork PRs get conservative permissions/no secrets by default.
10. OIDC is preferred over long-lived deployment credentials.
11. Runner credential and job token are separate principals.
12. Postgres stores state; ObjectStore stores logs/artifacts/cache.
13. Same-host runner is trusted-workload convenience, not the public-repo default.
14. Ephemeral runners are the recommended managed/public model.
15. OTel covers server + runner but not raw workflow output by default.
16. Runtime Go code returns errors; panic is startup-only.
17. `make dev` remains the single local setup command.
18. UI and managed-cloud compute come after protocol correctness.

## 47. Current primary references

- GitHub Actions workflow syntax:
  https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax

- GitHub self-hosted runners:
  https://docs.github.com/en/actions/reference/runners/self-hosted-runners

- GitHub Actions security:
  https://docs.github.com/en/actions/reference/security/secure-use

- GitHub OIDC:
  https://docs.github.com/en/actions/concepts/security/openid-connect

- GitHub Actions REST API:
  https://docs.github.com/en/rest/actions

- GitHub runner source:
  https://github.com/actions/runner

- Gitea Actions design:
  https://docs.gitea.com/usage/actions/design/

- Forgejo Actions overview:
  https://forgejo.org/docs/latest/user/actions/overview/

- Forgejo Actions security:
  https://forgejo.org/docs/latest/user/actions/security/

- nektos/act:
  https://github.com/nektos/act
