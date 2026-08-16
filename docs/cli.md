# Command-line client

The `adenosine` binary is both the server entry point and the official public-API client.
The client uses the generated Go REST client and standard `git` commands; it does not read
the server database or call internal endpoints.

## Login

Create a personal access token with the permissions needed for the intended operations,
then pass it through standard input:

```sh
printf '%s\n' "$ADENOSINE_TOKEN" | adenosine login \
  --host https://forge.example \
  --token-stdin
```

The token is never accepted as a command-line argument or printed. Credentials are stored
per normalized server URL in the operating system's user configuration directory under
`adenosine/hosts.json`. The directory is mode `0700` and the file is mode `0600`. Set
`ADENOSINE_CONFIG_DIR` to choose a different location.

## Repositories and issues

```sh
adenosine repo create --description "My project" project
adenosine repo view alice/project
adenosine issue create --repo alice/project --title "Bug" --body "Details"
adenosine issue list --repo alice/project --limit 30
adenosine issue view --repo alice/project at://did:plc:alice/dev.adenosine.issue/example
```

Repository arguments use the public `OWNER/REPO` route. The CLI resolves that route through
REST and uses the returned portable repository URI for collaboration records, including
repositories hosted by another Adenosine instance.

## Pull requests

```sh
adenosine pr create \
  --source-repo alice/project \
  --target-repo team/project \
  --source-branch feature \
  --target-branch main \
  --head 0123456789abcdef0123456789abcdef01234567 \
  --title "Add feature"

adenosine pr view at://did:plc:alice/dev.adenosine.pullRequest/example
adenosine pr checkout --branch review-example \
  at://did:plc:alice/dev.adenosine.pullRequest/example
adenosine pr merge --strategy squash \
  at://did:plc:alice/dev.adenosine.pullRequest/example
```

Checkout asks the documented REST API for the canonical source Git Smart HTTP URL and immutable
head SHA, then runs `git fetch` and `git checkout`. This is the same flow for local and remote
source repositories. Credentials are not added to Git command arguments.

All subcommands that return data accept `--json` for stable contract-shaped JSON. Common
flags must precede positional arguments. Collection commands consume the API's opaque
`page.next_cursor` value unchanged and never interpret cursor contents. Pass it back with
`issue list --cursor <value>` to request the next page.
