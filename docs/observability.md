# Observability

Adenosine emits structured JSON logs to stdout and uses vendor-neutral OpenTelemetry.
Set standard `OTEL_*` environment variables, including `OTEL_EXPORTER_OTLP_ENDPOINT`, to
enable OTLP/HTTP traces and metrics. Without an endpoint, providers remain local no-op
exporters. Export setup errors fail startup; collector availability is not a request-path
dependency, and providers flush during graceful shutdown.

HTTP requests receive `X-Request-ID`. Error envelopes repeat it as `error.request_id`, and
request completion logs include request ID, trace ID, method, route, status, duration, and
response size. Use the request ID to correlate client errors with logs and the trace ID to
follow instrumented boundaries.

Never add credentials, cookies, OAuth values, PATs, private repository data, request
bodies, Git pack bytes, or high-cardinality DIDs/repository IDs as metric attributes.
Record bounded operation names and error classes. Runtime errors should be wrapped with
useful context before they reach logging/trace boundaries rather than logged repeatedly at
every layer.

The development Compose stack includes Grafana LGTM at <http://localhost:3001> and exports
OTLP from the Go and Electric services. Production collector configuration, dashboards,
alerts, and SLO guidance are not yet hardened; follow
[issue #1](https://github.com/tomanagle/adenosine/issues/1).
