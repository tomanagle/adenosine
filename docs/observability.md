# Observability

Adenosine emits structured JSON logs to stdout and vendor-neutral OpenTelemetry traces and metrics over OTLP/HTTP. The Collector owns batching, retry, memory limits, and backend export. Collector failure does not affect readiness or request handling. Provider construction fails fast at startup, while shutdown flush is capped at five seconds.

## Resource identity

Every trace and metric has `service.name`, `service.version`, `service.instance.id`, and `deployment.environment.name`. Defaults are `adenosine`, build module version or VCS revision, hostname, and `development`. `OTEL_RESOURCE_ATTRIBUTES` overrides fallback identity and may add deployment metadata such as region; standard `OTEL_SERVICE_NAME` overrides `service.name` from that list. Set version, instance, and environment with `service.version`, `service.instance.id`, and `deployment.environment.name` in `OTEL_RESOURCE_ATTRIBUTES`. Resource attributes must not contain repository IDs, DIDs, Git SHAs, or other tenant identifiers.

Set `OTEL_EXPORTER_OTLP_ENDPOINT` on Adenosine to enable OTLP/HTTP export. Standard OTel exporter TLS, headers, timeout, and compression variables apply. `OTEL_TRACES_SAMPLER` supports `always_on`, `always_off`, `traceidratio`, `parentbased_always_on`, `parentbased_always_off`, and `parentbased_traceidratio`; ratio samplers require `OTEL_TRACES_SAMPLER_ARG` from 0 through 1. The default is `parentbased_always_on`. Invalid sampler configuration fails startup. Without an endpoint providers remain local no-export providers. Sampling is configuration, never domain logic.

## Correlation

HTTP requests receive `X-Request-ID`. Completion logs contain request ID, trace ID, method, route, status, duration, and response size. Git HTTP, Git SSH, and federation ownership-boundary errors contain `trace_id`, `span_id`, component, bounded operation, and a repository ID or federation event ID where needed for investigation.

Outbox rows persist nullable `traceparent` and `tracestate`, and producers inject the active W3C context. `event.ContextFromOutbox` is a tested extraction helper whose missing and malformed context fallback leaves its input context unchanged. No outbox worker exists on this branch, so runtime propagation is not wired or claimed. Baggage is intentionally not persisted.

## Metrics

Metric dimensions are fixed allow-lists. No repository UUID, DID, URI, owner, slug, Git SHA, ref, error text, or payload appears in a metric attribute.

| Metric | Unit | Dimensions |
| --- | --- | --- |
| `adenosine.git.commands.active` | commands | `git.operation`, `git.transport` |
| `adenosine.git.commands` | commands | operation, transport, `git.result` |
| `adenosine.git.command.duration` | seconds | operation, transport, result |
| `adenosine.git.bytes` | bytes | operation, transport, `git.direction` |
| `adenosine.federation.events` | events | bounded collection, result, duplicate |
| `adenosine.federation.errors` | errors | `federation.stage` |
| `adenosine.federation.processing.duration` | seconds | bounded collection, result, duplicate |
| `adenosine.db.client.connections` | connections | `state=used|idle|max` |
| `adenosine.db.client.connection.waits` | waits | none |
| `adenosine.db.client.connection.wait.duration` | seconds | none |
| `adenosine.http.server.requests` | requests | method, route, status class |
| `adenosine.http.server.duration` | seconds | method, route, status class |

Native Git has a process-wide limit of 16 commands, a five-second admission wait, and a 30-minute total deadline that begins before admission. It also has process-group cancellation, a five-second process wait delay, and 32 KiB bounded stderr. Smart HTTP caps upload-pack requests at 16 MiB and receive-pack requests at 2 GiB. SSH caps active connections at 128 and sessions at 64, limits handshakes to ten seconds, and closes authenticated pre-exec or active sessions after two minutes without network activity.

## Collector and dashboards

`infra/observability/otel-collector.yaml` is a bounded Collector baseline. Set `TELEMETRY_BACKEND_OTLP_ENDPOINT`; configure `TELEMETRY_BACKEND_OTLP_HEADERS` only through a secret source. Versioned Grafana dashboards and Prometheus-compatible rules are in `infra/observability/dashboards` and `infra/observability/alerts.yaml`.

Dashboard queries assume an OTel Collector Prometheus exporter or a backend that translates OTel metric names to Prometheus underscores. Validate translated names in the selected backend before importing.

## Data handling

Never record credentials, cookies, OAuth values, PATs, SSH key material, authorization headers, request/pack/body/file content, commit messages, user Markdown, SQL statements, or SQL parameters. PostgreSQL spans expose only the allow-listed operation verb. Errors are wrapped through internal layers and logged once at the component ownership boundary.

## Current gaps

- There is no outbox dispatcher on this branch, so pending count, oldest age, and attempt metrics cannot yet be emitted from a worker lifecycle. The alert file detects missing outbox telemetry rather than pretending to measure backlog.
- Receive-pack updates repository refs before `git.push_received` is inserted. A failed post-push insert is correlated and logged, but there is no durable hook journal or repository reconciler to recreate it, especially for Smart HTTP after the protocol result has succeeded. Push-event delivery is therefore best-effort until ref updates and durable event intent share a hook/journal boundary.
- There is no webhook delivery subsystem, so webhook backlog and failure telemetry do not exist.
- Tap events contain an ordered ID but no authoritative source timestamp. Processing throughput, duration, validation, projection failures, and the persisted cursor are observable; true source-to-projection lag requires an upstream timestamp or Tap cursor-head signal.
- Structured logs are written to stdout. Shipping them to a backend is the container/runtime operator's responsibility; the Go service does not yet use an OTel Logs SDK.
- Failed-push and delayed-federation acceptance traces require a running Collector, PostgreSQL, Git client, Tap source, and telemetry backend. Unit tests verify instrumentation behavior but cannot demonstrate a live cross-system trace.
