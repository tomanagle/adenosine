# Deployment conformance

Run `scripts/conformance.sh --config target.env --non-interactive` against Compose,
Railway, AWS, or another deployment. The versioned contract is documented in
`test/deployment/conformance.env.example`; no provider identifier is accepted.

The foundation suite proves live/ready health, API docs, release-matching OpenAPI, and real
Git Smart HTTP clone, fetch, authenticated push, tag, branch deletion, and tag deletion.
When a pinned SSH endpoint and registered test key are supplied it also performs real SSH
clone/fetch. Temporary refs are unique and cleanup runs on success, failure, or interruption.
PATs are passed through `GIT_ASKPASS`, never a URL or command argument.

Operator-owned telemetry and portable backup/clean-restore implementations plug in through
commands. Missing optional capabilities are printed as explicit skips. A target cannot claim
full conformance while required capabilities are skipped; Railway currently has an explicit
SSH limitation until its TCP proxy is configured.

Targets explicitly declare comma-separated capabilities. Official `aws` and `railway`
targets must declare health, docs, OpenAPI, Git HTTP, web, immutable build identity, SSH,
telemetry, and backup/restore; a missing declaration or required hook fails. Custom targets
may intentionally omit a capability and receive a visible skip. Backup conformance leaves a
fixture branch present through backup and proves its commit survives clean restore.

This is a foundation for issue #13, not a substitute for the existing federation black-box
programs. Identity login, two-instance federation, issues/comments, pull requests/reviews,
AppView/realtime convergence, telemetry outage injection, and a provider-independent backup
format still require test identities and operator implementations that do not yet exist as a
stable public deployment contract. They must be added to this same runner rather than copied
into provider-specific suites.
