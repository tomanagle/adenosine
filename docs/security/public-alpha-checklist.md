# Public-Alpha Security Checklist

Status meanings: **automated** has a focused test/control in this repository; **operator** requires deployment enforcement; **accepted risk** is not implemented for alpha.

| Audit area | Status | Evidence or required action |
| --- | --- | --- |
| Native Git uses one runner, no shell, argument vectors | automated | `internal/git/runner.go`; command/remote tests |
| SSH exact allow-list and metacharacter/quoting rejection | automated | `internal/gitssh/command.go`; `command_test.go` |
| Revision/path option injection and `--` separation | automated | Git read/history/remote tests |
| Git stderr, process groups, cancellation, admission, duration and slots | automated | 32 KiB buffer, process-group kill, five-second admission, 30-minute total timeout, 16 slots; `runner_limits_test.go` |
| Unprivileged UID, cgroups, process/file/disk quotas | operator | run with no shell/sudo, writable storage only, finite PID/memory/disk quotas |
| Immutable-ID physical paths and traversal/symlink escape | automated | storage package and tests; owner/slug never form physical paths |
| Staged quarantine deletion | accepted risk | deletion workflow is not implemented; backups may retain data |
| PR-fetch SSRF schemes, DNS/IP transitions, redirects | automated | HTTPS/public-IP validation, pinning, redirect denial, IPv4/IPv6 tests |
| Private-network self-host exception | operator | route through an explicit isolated allow-list proxy; no broad in-app bypass |
| Webhook SSRF, signatures, limits, retry/deletion | accepted risk | webhook delivery does not exist |
| Hashed sessions/PATs, scoped/revocable PATs | automated | auth service/store tests |
| OAuth credential encryption and external key | automated/operator | encryption tests; inject/rotate 32-byte key outside database/image |
| Public SSH keys only, malformed-key rejection, DID auth | automated | SSH auth/service tests and parser |
| Auth rate limiting and revoked-credential abuse | partial/operator | SSH has three tries; revocation/scope tested; edge rate limits required |
| Electric schema/shape isolation | automated/operator | predefined proxy shapes/tests; use restricted database/publication role |
| No sensitive browser rows or secret logging | automated/operator | explicit shape columns and telemetry contract; audit backend retention/access |
| Pack/body/remote/federation output bounds | automated | HTTP, runner, read/archive/diff, remote-fetch and Tap limits/tests |
| Sustained streams, goroutines and slot cleanup | partial | Git slots/admission and SSH handshake/idle/slot cleanup have focused tests; deployed load/cgroup test required |
| Telemetry memory/retry/cardinality | automated/operator | Collector memory/batch/queue bounds; fixed code dimensions; backend quotas required |
| Lexicon/domain validation and record authorship | automated | federation event and projection tests |
| Malicious Markdown/HTML/external resources | partial/accepted risk | store treats Markdown as data; each current/future renderer needs sanitizer tests |
| Local blocks, hidden records/repos and reports | automated | moderation schema/service/projection tests |
| Moderation does not mutate source records | automated | separate moderation schema and derived filtering |
| Telemetry credential/content redaction | automated/operator | SQL operation-only spans and bounded metric labels; inspect Collector processors/backend |
| pprof and health/doctor secrecy | operator | no public pprof route; expose health only; keep diagnostics internal |
| Outbox W3C producer context and malformed extraction fallback | partial | migration 000013 and event tests; no worker exists, so runtime propagation is not wired |
| Push-event durability after receive-pack | accepted risk | ref update precedes outbox insert; failures are logged but no hook journal/reconciler exists |
| Failed push trace and correlated boundary error | partial/live | HTTP/SSH/native spans, metrics and logs implemented; exercise against live Collector/backend |
| Delayed federation trace and alert | partial/live | validate/project spans and metrics implemented; true source lag lacks upstream timestamp/head |
| Backup encryption, restoration and deletion retention | operator | schedule encrypted backups, restrict keys, test restore, publish retention policy |

## Release gate

- Run `make test` and `make lint`; resolve all security and race failures.
- Exercise malformed SSH commands, revision/path injection, canceled/oversized Git requests, invalid Tap events, revoked/scoped credentials, and moderation visibility in the target environment.
- Confirm no public PostgreSQL, Collector administration, pprof, SSH host-key file, repository directory, or secret-bearing diagnostic endpoint.
- Confirm TLS, edge request/auth rate limits, cgroup/PID/file/disk quotas, database least privilege, secret rotation, encrypted backups, telemetry retention, and alert routing.
- Capture one failed Smart HTTP and one failed SSH push trace. Verify ingress, authorization, native Git, bounded metric dimensions, and exactly one actionable component error.
- Capture one rejected and one intentionally delayed federation event. Verify validate/project spans and throughput/error metrics; record that end-to-end source lag is unavailable until Tap provides source time/head.
