# User accounts, product access, and workflow sharing

**Status:** design agreed 2026-09-02; all four phases built and deployed to the
RTS Video Studio box the same day. Kept as the reference for the model.
**Related:** `docs/core/multi_user_authentication.md` (current auth),
`docs/bugs/pulse_platform/plat-262.md` (read-only enforcement, reused as is),
`deploy/aws-ec2/server/auth-gateway.go` (Video Studio gateway).

## What this changes, in one paragraph

Today authentication knows several kinds of identity (an env-var user list,
OAuth, a credential-free single user, and a per-product gateway identity that
makes every Video Studio visitor the same person) but there is no user
directory, no per-workflow ownership, and read-only is a site-wide tier set
through env vars. This design adds one user directory as a JSON file, gives
every workflow an owner and read-only grants, lets an admin enable products
per user, and makes Video Studio users log in as themselves. The runtime's
existing read-only enforcement is kept exactly as it is and simply keyed on
"is this user an owner of this workflow" instead of a global tier.

## The model

Two places a permission can live, nothing else.

**On the account** (AgentWorks user record):
- `admin: true|false`. Admins manage users, set product access, and can open
  any workflow. The first admin is named in config, never inferred.
- `can_create: true|false`. Controls creation of new workflows. `true` is a
  normal member that owns what they create.
- `can_edit: true|false` (optional). Controls whether an account may own and
  edit workflows explicitly assigned to it. When absent it follows
  `can_create`, preserving existing records. `can_create: false` plus
  `can_edit: true` is a contributor: it can own assigned workflows but cannot
  create or duplicate workflows. Both false is read-only.
- `products: ["agentworks", "video-studio", ...]`. Which product surfaces the
  user may open. A member with the list absent gets all products; a
  read-only user with it absent gets none.

**On the workflow** (`Workflow/<id>/manifest.json`, `access` block):
- `owners: [user_id, ...]`. The creator is the first owner. Owners edit, run,
  share, transfer, delete. There is deliberately no editor tier: to let a
  colleague edit, add them as an owner.
- `readers: [user_id, ...]`. Read-only, with exactly PLAT-262 semantics:
  chat, trigger and watch runs, inspect files and DB; no mutating tools, no
  shell writes. Added either by an owner (sharing) or by an admin (assigning
  a specific workflow to a user).

Workshop vs run mode is unrelated to permissions and is left untouched.

Products have no roles of their own. Video Studio projects are per user
(under `_users/<id>/Chats/Video Studio/projects`) and not shareable in this
phase; a project manifest can carry the same `access` block later if wanted.

## Storage: JSON, deliberately

Tens of users, a few grants per workflow, one server process. The repo
already stores permission-like state as JSON with a mutex and atomic
temp-then-rename writes; this follows that pattern. SQLite (already present
for pulse/report data) would only earn its place with multiple replicas.

`config/users.json` (global, admin-managed, mode 0600):

```json
{
  "users": [
    {
      "id": "3f9a...",            // stable; sha256("user:"+username)[:16] to match today's AUTH_USERS ids
      "username": "manish",
      "email": "m@example.com",
      "password_hash": "$argon2id$...",   // absent for SSO-only accounts
      "sso": {"provider": "cognito", "external_id": "..."},  // optional
      "admin": true,
      "can_create": true,
      "can_edit": true,
      "products": ["agentworks", "video-studio"],
      "disabled": false,
      "created_at": "2026-09-02T12:00:00Z"
    }
  ]
}
```

Workflow manifest gains:

```json
"access": { "owners": ["3f9a..."], "readers": ["a1b2..."] }
```

Migration: a manifest without `access` gets `owners: [created_by]` on first
read, or `[<first admin>]` when `created_by` is empty. Existing
`config/workflow-user-permissions.json` and `config/user-product-access.json`
are read once into the new shapes and then ignored. `AUTH_USERS` keeps
working as a bootstrap: its users are imported into `users.json` on first
start (password hashed at import) and the env var can then be dropped.

## Login: both

- **Password**: `POST /api/auth/login` checks `users.json` (argon2id).
  Admins create users and set an initial password from the admin page; users
  change their own. No self-registration (unchanged).
- **SSO**: Cognito/Supabase stay as they are. On first SSO login the user is
  created in `users.json` with `can_create: false` and no products, so an
  admin has to switch them on. This is the safe default for an open sign-in
  provider.
- JWT claims stay identity-only (no roles in the token); permissions are
  re-read per request from `users.json`, as today.

## Video Studio: users log in as themselves

The gateway's shared `ACCESS_PASSWORD` cookie is replaced by the app's own
login. The gateway already has this mode (`GATEWAY_DISABLE_PASSWORD_GATE`,
live on Dominion). With it on, the browser talks to the agent API with a
real per-user JWT, `MULTI_USER_MODE=true`, and each user gets their own
`_users/<id>` tree: own projects, own secrets, own Claude token, own history.
Product access is checked at `/api/agent-profiles/{id}/...`: a user without
`video-studio` in `products` gets 403 and the surface switcher hides it.

Migration on the box: everything today lives under the `default` user (the
gateway remaps its service identity to it). That tree is renamed to the
first admin's id in one step during the switch-over.

## Enforcement points (all existing code, re-keyed)

| Today | Becomes |
|---|---|
| `workflowAccessForIdentity` (env tiers) | `workflowAccessFor(userID, workflowID)`: owner if admin or in `owners`; read if in `readers`; none otherwise |
| `currentUserIsReadOnly` in the query path (`server.go:3192`) | same flag, from the per-workflow answer |
| `requireWorkflowWriteAccess` route wrapper | owner-of-this-workflow check through `can_edit`; creation and duplication routes use the separate `can_create` gate |
| `requireWorkflowOwnerAccess` (4 admin routes) | `requireAdmin` |
| `filterWorkflowManifestsForUser` / `userAllowedWorkflowID` | list = owned + readers + all if admin; open = same |
| `config/user-product-access.json` | `products` on the user record; checked at product-profile routes and in the surface switcher |
| `WorkflowAccessPopup.tsx` (edits the global tier file) | Share popup on a workflow: add owner / add reader by username or email, remove, transfer |
| `GET /api/auth/users` (unused) | backs the admin page and the share popup's user picker |

Hardening folded in, because the new model makes these real holes:
- The agent server stamps `X-User-ID` on the workspace proxy from the JWT;
  the browser's own header is ignored.
- A session with an empty owner is no longer readable by everyone.
- `AUTH_USERS` plaintext comparison goes away with the import.

## Admin page (frontend)

Under Settings, admins only: user list (add, disable, reset password, choose
Admin/Member/Contributor/Read-only, product checkboxes), and per user the
workflows they own or read. Owners see a Share button on their own workflows.
Read-only users see neither.

## Phases, each shippable alone

1. **User directory + login** (built): `users.json`, argon2id, import from
   `AUTH_USERS`, admin named in config (`ADMIN_USERS=<username>`), admin page
   ("Users & access", `frontend/src/components/admin/UsersAdminPanel.tsx`)
   with user CRUD and product toggles, `/api/admin/users`, `/api/auth/password`.
   Env tiers still honoured underneath for identities the directory does not know.
2. **Video Studio as real users** (built): gateway password gate off, the
   gateway verifies the app JWT itself and stamps `X-User-ID`, login rate
   limited, `deploy/aws-ec2/migrate-to-user-accounts.sh` moves the `default`
   tree to the admin.
3. **Workflow ownership and sharing** (built): `access` block
   (`workflow_access.go`), `created_by` alone still names the owner on older
   manifests, a manifest with neither keeps the account-tier behaviour;
   Share popup (`WorkflowSharePopup.tsx`) with `/api/workflow/access` and
   `/api/users/directory`; list annotates `my_access`; per-workflow checks on
   manifest get/update/delete/duplicate, the workspace proxy, schedules, and
   the query path's read-only gate. Readers may run, trigger and stop.
4. **Hardening** (built): the agent's workspace proxy and the gateway both
   stamp `X-User-ID` from the verified token; a session with no recorded
   owner is visible only to admins (or the single local user); passwords
   are hashed. The legacy tier/product files still load for identities the
   directory does not know, so nothing needs deleting on day one.

## Out of scope for now

Roles inside products, shareable Video Studio projects, teams or
organisations, multiple server replicas.
