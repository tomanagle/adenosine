# Git Smart HTTP

Adenosine delegates Git object and pack protocol behavior to the configured native Git executable. Pack request and response bodies stream directly between HTTP and `git upload-pack` or `git receive-pack`; they are never accumulated in application memory.

Public repositories support anonymous clone and fetch through:

```text
GET  /<owner>/<repository>.git/info/refs?service=git-upload-pack
POST /<owner>/<repository>.git/git-upload-pack
```

Push uses the corresponding `git-receive-pack` discovery and RPC endpoints. Supply a
personal access token (PAT) as the HTTP Basic password. Adenosine ignores the Basic username:
it does not need to match the owner or account because the PAT determines the account. The
token must be active, include the `repository:write` scope, satisfy any repository restriction
on the token, and belong to the repository owner or a collaborator with write access. See
[API authentication](api-authentication.md) for PAT creation and revocation.

## Development workflow

Start the development stack as described in [development and testing](development.md). The
default Git HTTP base URL is `http://127.0.0.1:8080`.

Replace every angle-bracketed value below before running the commands:

```sh
BASE_URL='http://127.0.0.1:8080'
OWNER='<owner>'
REPOSITORY='<repository>'
CHECKOUT='<local-checkout-directory>'
BRANCH='<new-branch-name>'
TAG='<new-tag-name>'
```

Public repositories can be cloned and fetched without credentials:

```sh
git clone "$BASE_URL/$OWNER/$REPOSITORY.git" "$CHECKOUT"
git -C "$CHECKOUT" fetch --prune origin
```

For pushes, use an ephemeral askpass helper rather than putting the PAT in the remote URL,
shell history, command arguments, or Git configuration. The fixed username below can be any
non-empty value because Adenosine ignores it. The PAT is read without terminal echo and
exists only in the current shell environment and temporary helper process.

```sh
askpass_dir="$(mktemp -d)"
askpass="$askpass_dir/askpass.sh"
printf '%s\n' \
  '#!/bin/sh' \
  'case "$1" in' \
  '  *Username*) printf "%s\n" adenosine ;;' \
  '  *Password*) printf "%s\n" "$ADENOSINE_PAT" ;;' \
  '  *) exit 1 ;;' \
  'esac' >"$askpass"
chmod 700 "$askpass"

printf 'PAT (requires repository:write): ' >&2
IFS= read -r -s ADENOSINE_PAT
printf '\n' >&2
export ADENOSINE_PAT

git_with_pat() {
  GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 \
    git -c credential.helper= "$@"
}

git_with_pat -C "$CHECKOUT" push origin "HEAD:refs/heads/$BRANCH"
git -C "$CHECKOUT" tag "$TAG"
git_with_pat -C "$CHECKOUT" push origin "refs/tags/$TAG:refs/tags/$TAG"

git_with_pat -C "$CHECKOUT" push origin ":refs/heads/$BRANCH"
git_with_pat -C "$CHECKOUT" push origin ":refs/tags/$TAG"
```

Clean up immediately after the authenticated operations, including after a failed push:

```sh
unset ADENOSINE_PAT
unset -f git_with_pat
rm -rf -- "$askpass_dir"
unset askpass askpass_dir
```

Git may retry after an authentication challenge. A final `401 Unauthorized` means the Basic
credentials were missing, the password was empty, or the PAT was invalid, expired, or
revoked. A `403 Forbidden` means the PAT authenticated but lacks `repository:write`, is
restricted to a different repository, or its account does not have write access to this
repository. Creating a new PAT does not grant repository access by itself.

After a successful receive-pack request, Adenosine records a `git.push_received` event in the PostgreSQL outbox for asynchronous post-push work. Branch updates, tag updates, and deletions use the same path.

Repository paths are resolved from PostgreSQL metadata and immutable repository IDs. Public owner and slug values are never interpolated into filesystem paths.

The transport supports Git protocol versions 1 and 2 through the standard `Git-Protocol` header.
