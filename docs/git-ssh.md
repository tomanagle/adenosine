# Git over SSH

Adenosine serves stateful `git-upload-pack` and `git-receive-pack` sessions over SSH. The development endpoint defaults to:

```text
ssh://git@localhost:2222/<owner>/<repository>.git
```

Connections require the `git` SSH user and an active public key registered to a local Adenosine account. Public repositories are readable by authenticated keys. Private repository reads and all pushes additionally use the repository owner and collaborator permissions stored in PostgreSQL.

The server accepts only these exact command forms:

```text
git-upload-pack '<owner>/<repository>.git'
git-receive-pack '<owner>/<repository>.git'
```

Shells, PTYs, subsystems, forwarding, additional arguments, and unsafe repository paths are rejected. Public owner and repository names resolve through PostgreSQL; client input is never used as a filesystem path or executed through a shell.

The host key path is configured with `ADENOSINE_SSH_HOST_KEY_PATH`. Development startup creates an Ed25519 key once in the persistent `instance_state` volume. Production deployments must provision and preserve this file with mode `0600`; changing it changes the instance's SSH host identity.

User key material submitted through `POST /api/v1/ssh-keys` is parsed as an OpenSSH authorized-key line. Adenosine stores only its canonical public key, algorithm, and SHA-256 fingerprint. Private keys, SSH certificates, and legacy DSA keys are rejected. Active keys can be listed and revoked through the session-authenticated credential API.

Successful pushes write the same `git.push_received` PostgreSQL outbox event as Smart HTTP pushes.
