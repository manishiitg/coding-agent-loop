# Google CLI authentication and agent terminals

Trusted Builder and workflow shells run the real `gog` binary directly. All
supported commands, flags, subprocesses, pipes and scripts remain available.
There is no command proxy or replacement executable.

The application and workspace executor resolve the same store in this order:

1. `GOG_HOME`, when configured.
2. `$XDG_CONFIG_HOME/agentworks/gog`.
3. `$HOME/.config/agentworks/gog` for the service user.

The workspace executor exports the resolved absolute `GOG_HOME` before sandbox
execution, independently of the shell's temporary `HOME` and cache directories.
It grants read/write access to that directory so gog can load credentials,
maintain locks and refresh tokens. The shell path preflight permits that same
store. Other host paths retain their existing restrictions. Restricted agent
profiles (`StrictAllowlist`) do not inherit this grant or gog keyring variables.
`AGENTWORKS_GOG_TERMINAL_ACCESS=false` disables automatic terminal access.

For a split server/workspace deployment, configure `GOG_HOME` on both processes
and mount the same credential store at that location in both containers. The
backend grant does not mount a host directory that is absent from the container.
Install gog in the execution environment as well as the application environment.
For a password-protected file keyring, supply `GOG_KEYRING_BACKEND` and
`GOG_KEYRING_PASSWORD` to both processes. Trusted terminals receive these values;
they are not printed in configuration or migration reports.

An agent can discover accounts and use the selected identity directly:

```sh
gog auth list --json
gog --account '<email>' --client '<client-name>' gmail search 'in:inbox' --json
```

`--home` is optional because `GOG_HOME` is already exported. Agents do not need a
workflow-specific `VAR_GOG_HOME`. An explicitly supplied `--home` pointing at the
configured store also passes preflight. Credentials in this shared store are
accessible to trusted terminal agents; account settings are not an isolation
boundary between those agents.

## Authentication ownership

New Google connections have `auth_backend: "gog"`. The browser consent flow
exchanges the one-time code, verifies the account identity and imports the
refresh token into gog. Import and verification must succeed before the app
reports the connection as authorized. The backend does not retain another copy
of the token or refresh it. Gmail delivery, status and existing Google service
tools use the connection's account/client selectors with gog.

OAuth client uploads are stored in gog. AgentWorks retains client metadata and
the connection registry (email, client name, default selection, enabled state,
requested capabilities). It reads legacy client-secret files only for accounts
that have not yet migrated. Actual scopes for gog accounts come from gog's
checked account listing, including accounts granted Gmail send without read.

`gws` stays installed and available. Existing legacy connections continue through
their previous authentication path until migration or reconnect succeeds.
Migrated accounts use gog; their old backend token files are no longer used.
Independent gws credentials and its host configuration are never deleted by the
migration command.

## Migrate an existing installation

From the repository root, verify the existing gog accounts without changing
configuration:

```sh
go run ./agent_go/cmd/gog-migrate --config /path/to/workspace-docs/config/gmail-config.json
```

After deploying the updated application and workspace executor, migrate:

```sh
go run ./agent_go/cmd/gog-migrate \
  --config /path/to/workspace-docs/config/gmail-config.json --apply
```

The command reuses valid gog accounts, imports a legacy token only when needed,
and verifies the email/client pair for every connection before saving. Failure
leaves the original registry and legacy credentials in place. Success writes a
backup of the registry and atomically replaces it with the migrated version.
Reload/restart the app after changing its configuration from the command line.

Once running the updated server, repeat with `--apply --retire-legacy` to remove
the verified duplicate AgentWorks token files and matching legacy client-secret
files. Client metadata remains. A mismatched legacy client secret is retained
and reported for review. Keep the legacy files until the new code is deployed;
an older server's sign-in flow still requires them.
