# Environment-Based API Key Defaults

Set API keys and LLM configuration via environment variables. Supports two modes: **default** (users configure providers and keys in the frontend) and **restricted** (single provider and keys from server env; users cannot change them).

---

## Two Modes

| Mode | Who sets API key / provider | What users see |
|------|-----------------------------|----------------|
| **Default** | Users set their own provider and API keys in the frontend. | All 6 providers (OpenRouter, Bedrock, OpenAI, Vertex, Anthropic, Azure). Optional env vars can pre-fill keys for convenience. |
| **Restricted** | You set one provider and API key via env. Server uses these for every request. | Only the chosen provider (e.g. Vertex/Gemini). Provider and API key are fixed; users do not configure them (optional: lock UI so they cannot change). |

---

## Current Behavior (Where Config Comes From)

When the server handles a **query** (chat or workflow), the source of provider, model, and API keys depends on the lock state:

### When `LLM_CONFIG_LOCKED=false` (default mode)

- **Provider and model:** From `req.LLMConfig.Primary` (or legacy `req.Provider` / `req.ModelID`) — all from frontend.
- **API keys:** From `req.LLMConfig.APIKeys`. If missing, the server passes `nil`.
- The frontend fetches `/api/llm-config/defaults` (which includes keys from env), then sends those values in every query request.

### When `LLM_CONFIG_LOCKED=true` (restricted mode)

- **Provider and model:** From `getPrimaryProviderAndModelFromDefaults()` — reads env, ignores frontend request.
- **API keys:** From `buildProviderAPIKeysFromEnv()` — reads `os.Getenv()` directly, ignores frontend request.
- **Fallbacks:** Disabled (`nil`).
- The frontend receives no API keys in `/api/llm-config/defaults` (stripped by `stripSecretsFromMap()`).

See [How API Keys Flow in Locked Mode](#how-api-keys-flow-in-locked-mode) for the detailed flow diagram.

---

## How the Defaults API Works

- **Endpoint:** `GET /api/llm-config/defaults`
- **Server:** Builds the response using `llm.GetLLMDefaults()` (reads env) and `getSupportedProviders()` (reads `SUPPORTED_LLM_PROVIDERS`).
- **Frontend:** Calls this once on load, then merges the response with any saved user config and **sends** provider, model, and API keys **in every query request**. The server does not re-read env when handling that query.

---

## New user experience (empty localStorage)

When a **new user** visits the site for the first time, they have no localStorage: no saved API keys, no published LLMs, no prior config.

### What happens today

1. **On first load** the frontend calls `GET /api/llm-config/defaults` and merges the response into the store. For a new user there is no “saved” state, so **defaults win**:
   - **primaryConfig** is set to **defaults.primary_config** (provider + model_id from backend/env).
   - Each provider config (e.g. **vertexConfig**, **openrouterConfig**) is filled from defaults, including **api_key** when you set it via env.
2. **Published LLMs (savedLLMs)** are **not** set by the defaults API. They come only from localStorage. So **savedLLMs stays empty** for a new user.
3. **availableLLMs** (the list used for the “select LLM” dropdown) is built **only from savedLLMs**. So for a new user the dropdown is **empty**.
4. **Chat still works** without publishing: the UI uses **primaryConfig** as the current LLM and builds the request from **primaryConfig** + API keys from the provider configs (which were pre-filled from defaults). So if you set env (e.g. Vertex API key + primary model), a new user can send a message immediately and the backend receives the right provider, model, and keys.
5. **The gap:** The sidebar shows the current model (from primaryConfig), but the **Published LLM** tab shows “No published LLMs yet. Configure a model in Provider tabs and publish it here.” To get an entry in that list (and in the LLM dropdown), the new user must open a Provider tab (already pre-filled from defaults) and click **Publish** once. So they do **not** have to type an API key again, but they do have to **publish** once if they want the default to appear in the Published list and in the dropdown.

**Summary for today:** New user gets defaults pre-filled and can chat right away. To see the default in the “Published LLM” list and in the LLM selector, they must manually publish once from the (already pre-filled) Provider tab.

### With default published LLMs (now implemented)

- The defaults API returns **default_published_llms** (e.g. one "Gemini" entry with provider, model_id).
- The frontend merges or replaces **savedLLMs** with that list on load.
- **New users** see the default (e.g. "Gemini") already in the Published LLM tab and in the LLM dropdown — **no manual publish step**. They can select it and chat immediately with zero configuration.

---

## Supported Variables

### API keys and provider-specific defaults

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `ANTHROPIC_PRIMARY_MODEL` | Default model (defaults to `claude-sonnet-4-20250514`) |
| `AZURE_AI_API_KEY` | Azure OpenAI API key |
| `AZURE_AI_ENDPOINT` | Azure OpenAI endpoint URL |
| `AZURE_PRIMARY_MODEL` | Default Azure model |
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `OPENAI_API_KEY` | OpenAI API key |

(Vertex/Gemini: typically uses GCP/Vertex env vars in the `mcpagent/llm` package, e.g. project/location; see that package for exact names.)

### Restricting which providers appear in the UI

| Variable | Description |
|----------|-------------|
| `SUPPORTED_LLM_PROVIDERS` | Comma-separated list of providers to show. Valid: `openrouter`, `bedrock`, `openai`, `vertex`, `anthropic`, `azure`. If unset, all six are shown. Example: `vertex` for Gemini-only. |

### Locking LLM configuration (restricted mode)

| Variable | Description |
|----------|-------------|
| `LLM_CONFIG_LOCKED` | When `true` or `1`: (1) Defaults API never returns `api_key`; (2) server ignores `req.LLMConfig` and uses env for provider, model, and API keys; (3) frontend shows locked message instead of editable modal; (4) default published LLMs list is read-only. |

### Locking MCP server configuration (restricted mode)

| Variable | Description |
|----------|-------------|
| `MCP_CONFIG_LOCKED` | When `true` or `1`: (1) MCP config GET endpoint returns `mcp_config_locked: true`; (2) MCP config POST (save) returns 403 Forbidden; (3) add/edit/remove server endpoints return 403 Forbidden; (4) frontend shows read-only view with locked message. |

### Default published LLMs

| Variable | Description |
|----------|-------------|
| `DEFAULT_PUBLISHED_LLMS` | Optional. JSON array of default published LLM entries (each: `id`, `name`, `provider`, `model_id`, etc.). When `LLM_CONFIG_LOCKED=true`, `api_key` is omitted from entries. |
| `DEFAULT_PUBLISHED_LLMS_PATH` | Optional. Path to a JSON file containing the same array. Used if `DEFAULT_PUBLISHED_LLMS` is not set. If neither is set, one entry is built from primary config (provider + model_id from env). |

### Multi-User Authentication

| Variable | Description |
|----------|-------------|
| `MULTI_USER_MODE` | When `true`: JWT authentication required for all API requests. When `false` (default): single-user mode, no login required. |
| `AUTH_SECRET` | JWT signing secret. **Required in production.** Default: dev-only secret (warning logged). |
| `DEFAULT_USER_ID` | User ID for single-user mode. Default: `default-user`. |
| `AUTH_PROVIDERS` | Comma-separated list of enabled auth providers. Valid: `simple`, `cognito`, `supabase`. Example: `simple,cognito`. |

### Simple Auth Provider

| Variable | Description |
|----------|-------------|
| `AUTH_USERS` | Comma-separated `user:password` pairs. Example: `admin:password123,user1:secret456`. |

### AWS Cognito Provider

| Variable | Description |
|----------|-------------|
| `COGNITO_USER_POOL_ID` | AWS Cognito User Pool ID (e.g., `us-east-1_xxxxx`). |
| `COGNITO_CLIENT_ID` | Cognito App Client ID. |
| `COGNITO_DOMAIN` | Cognito hosted UI domain (e.g., `myapp.auth.us-east-1.amazoncognito.com`). |
| `AWS_REGION` | AWS region for Cognito (e.g., `us-east-1`). |

### Supabase Auth Provider

| Variable | Description |
|----------|-------------|
| `SUPABASE_URL` | Supabase project URL (e.g., `https://xxx.supabase.co`). |
| `SUPABASE_ANON_KEY` | Supabase anonymous (public) key. |

See [Multi-User Authentication](./multi_user_authentication.md) for detailed documentation on authentication providers and per-user workspace isolation.

---

## How API Keys Flow in Locked Mode

When `LLM_CONFIG_LOCKED=true`, API keys **never touch the frontend**. They flow directly from server environment to the LLM provider:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         SERVER ENVIRONMENT                          │
│  .env file or container env vars:                                   │
│  OPENROUTER_API_KEY=sk-or-...                                       │
│  ANTHROPIC_API_KEY=sk-ant-...                                       │
│  LLM_CONFIG_LOCKED=true                                             │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    server.go (handleQuery)                          │
│                                                                     │
│  1. Request arrives (frontend sends NO api keys)                    │
│                                                                     │
│  2. Check: isLLMConfigLocked() → true                               │
│                                                                     │
│  3. Call buildProviderAPIKeysFromEnv():                             │
│     keys := &llm.ProviderAPIKeys{}                                  │
│     keys.OpenRouter = os.Getenv("OPENROUTER_API_KEY")  ← reads env  │
│     keys.Anthropic = os.Getenv("ANTHROPIC_API_KEY")    ← reads env  │
│     return keys                                                     │
│                                                                     │
│  4. Pass keys into agent config:                                    │
│     agent.NewAgent(agent.AgentConfig{                               │
│         APIKeys: buildProviderAPIKeysFromEnv(),  ← injected here    │
│         Provider: finalProvider,                                    │
│         ModelID: finalModelID,                                      │
│     })                                                              │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         mcpagent/llm                                │
│                                                                     │
│  Uses APIKeys from config to call OpenRouter/Anthropic/etc API      │
└─────────────────────────────────────────────────────────────────────┘
```

### Step-by-step flow

| Step | What Happens |
|------|--------------|
| 1 | Frontend sends request with NO `api_key` (stripped from defaults response) |
| 2 | Server receives request, checks `LLM_CONFIG_LOCKED=true` |
| 3 | Server calls `buildProviderAPIKeysFromEnv()` which reads `os.Getenv("OPENROUTER_API_KEY")` etc. |
| 4 | Keys are passed directly into `agent.AgentConfig.APIKeys` |
| 5 | Agent uses those keys to call the LLM provider |

### Key code paths

**Provider and model selection** (server.go lines ~1708-1722):
```go
if isLLMConfigLocked() {
    // Ignore request LLM config; use server env only
    finalProvider, finalModelID = getPrimaryProviderAndModelFromDefaults()
    fallbacks = nil
}
```

**API key injection** (server.go lines ~2502-2505):
```go
APIKeys: func() *llm.ProviderAPIKeys {
    if isLLMConfigLocked() {
        return buildProviderAPIKeysFromEnv()  // reads from os.Getenv()
    }
    // otherwise use req.LLMConfig.APIKeys from frontend
}(),
```

---

## Restricted Mode and Server-Side Env (Implemented)

To support "only Gemini, server uses env; users don't configure provider or API keys":

### 1. Restrict UI to one provider

- Set **`SUPPORTED_LLM_PROVIDERS=vertex`** (or the single provider you want).
- The defaults API returns only that provider in `supported_providers`; the frontend shows only that provider.

### 2. Lock the UI

- **Backend:** Env var **`LLM_CONFIG_LOCKED=true`**. When set, includes **`llm_config_locked: true`** in the `GET /api/llm-config/defaults` response.
- **Frontend:** When `llm_config_locked === true`, LLM Configuration modal shows a read-only message: **"LLM settings are locked by admin. Contact your administrator to enable new LLMs or models."** with the current model displayed (e.g. "Current: vertex — gemini-pro"). The full editable tabs are not shown.
- **Security:** When **`LLM_CONFIG_LOCKED=true`**, the defaults API **never** includes `api_key` or `endpoint` (e.g. Azure tenant URL) in the response. The `stripSecretsFromMap()` function recursively removes these from all configs and `default_published_llms`.

### 3. Server uses env for the actual LLM call (enforced when locked)

When `LLM_CONFIG_LOCKED=true`, the server **ignores** client-sent LLM config entirely:

- **Provider and model:** Taken from `getPrimaryProviderAndModelFromDefaults()` (reads env), restricted to `SUPPORTED_LLM_PROVIDERS`.
- **API keys:** Built by `buildProviderAPIKeysFromEnv()` which reads `os.Getenv()` for each provider.
- **Fallbacks:** Cleared (`fallbacks = nil`) — only the primary from env is used.
- **Result:** Lock is enforced on the server. Crafted HTTP requests or edited localStorage cannot override provider or keys.

### 4. Example: Gemini-only deployment

```bash
# Only show Vertex/Gemini in UI
SUPPORTED_LLM_PROVIDERS=vertex

# Lock so users cannot change provider or enter keys
LLM_CONFIG_LOCKED=true

# Server uses these keys (never sent to browser)
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
# Or for API key auth:
VERTEX_API_KEY=your-vertex-api-key
```

### 5. Example: Locked MCP servers deployment

```bash
# Lock MCP servers so users cannot add/edit/remove
MCP_CONFIG_LOCKED=true

# Users can only use the pre-configured MCP servers from the base config file
# They can still enable/disable servers, but cannot modify the configuration
```

### 6. Example: Fully locked deployment

```bash
# Lock both LLM and MCP configuration
LLM_CONFIG_LOCKED=true
MCP_CONFIG_LOCKED=true

# Restrict to specific LLM provider
SUPPORTED_LLM_PROVIDERS=vertex

# Server-side credentials (never sent to browser)
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

---

## Default Published LLMs (Implemented)

The backend can provide a list of **default published LLMs** that appear in the frontend's "Published LLM" tab. When locked, this list is read-only.

### 1. Backend: defaults API returns published LLMs

The `GET /api/llm-config/defaults` response includes:
- **`default_published_llms`**: Array of SavedLLM-like objects (id, name, provider, model_id, etc.). When `LLM_CONFIG_LOCKED=true`, `api_key` and `endpoint` are stripped.
- **`default_published_llms_locked`**: Boolean tied to `LLM_CONFIG_LOCKED`. When `true`, frontend treats the list as read-only.

**Source priority** (in `getDefaultPublishedLLMs()`):
1. **`DEFAULT_PUBLISHED_LLMS`** env var — JSON array string
2. **`DEFAULT_PUBLISHED_LLMS_PATH`** env var — path to JSON file
3. **Auto-generated** — one entry built from primary_config (provider + model_id)

### 2. Frontend: merge/replace savedLLMs

In `loadDefaultsFromBackend` (useLLMStore.ts):
- **When not locked:** Merge by `id` first, then by `provider+model_id`. Add missing defaults; keep user-added LLMs.
- **When locked:** Replace `savedLLMs` entirely with `default_published_llms`.

### 3. Frontend: lock the Published LLM tab

In **LibraryTab**:
- When `defaultPublishedLLMsLocked === true`: Delete button is hidden.
- Users can only select from the default LLMs as primary; cannot add or remove.

### 4. Example: Gemini-only with locked published LLMs

```bash
LLM_CONFIG_LOCKED=true
SUPPORTED_LLM_PROVIDERS=vertex
# Auto-generates one published LLM: "Default (vertex)" with model from env
```

Or with custom list:
```bash
DEFAULT_PUBLISHED_LLMS='[{"id":"gemini-pro","name":"Gemini Pro","provider":"vertex","model_id":"gemini-1.5-pro"}]'
```

---

## Local Development

Add to `agent_go/.env`:
```bash
ANTHROPIC_API_KEY=sk-ant-...
```

For Gemini-only (Vertex) with UI restricted to one provider:
```bash
SUPPORTED_LLM_PROVIDERS=vertex
# Plus Vertex/Gemini env vars required by mcpagent/llm (e.g. project, location, credentials)
```

## Azure Deployment

Pass via Terraform variable:
```bash
export TF_VAR_anthropic_api_key="sk-ant-..."
terraform apply
```

Or in `terraform.tfvars` (do not commit):
```hcl
anthropic_api_key = "sk-ant-..."
```

---

## Risks, edge cases, and open questions

This section records known risks, logic/edge cases, env semantics, and omissions so they can be addressed in implementation or follow-up.

### Security

| Issue | Severity | Status |
|-------|----------|--------|
| API keys sent to browser in locked mode | High | ✅ **Fixed:** `stripSecretsFromMap()` removes `api_key` and `endpoint` from all configs when `LLM_CONFIG_LOCKED=true`. |
| Lock only enforced in UI (bypassable via manual HTTP or localStorage) | High | ✅ **Fixed:** Server ignores `req.LLMConfig` entirely when locked; uses `buildProviderAPIKeysFromEnv()` for keys. |

### Logic / edge cases

| Issue | Severity | Notes |
|-------|----------|--------|
| **Merge vs replace collision:** Same `id` or same `provider`+`model_id` between default and user-created entry; or defaults with different `id` but same provider+model. | Medium | Define conflict resolution: e.g. defaults use reserved `id` prefix (`default-*`); merge by `id` first, then by provider+model_id; server default wins over user when id matches. |
| **Lock state transitions:** User had custom LLMs → admin enables lock → are custom LLMs lost or restored when unlocked? Locked → unlocked → does old localStorage resurface? | Medium | Define: when unlocking, either restore from localStorage (if still valid) or keep server defaults as new baseline. When locking, replace savedLLMs with server list; consider not persisting locked list to localStorage so unlock can restore previous. |
| **primaryConfig vs savedLLMs sync:** When locked, primaryConfig must reference an entry that exists in savedLLMs (default_published_llms). | Medium | On load, when locked, set primaryConfig to the first (or designated) default_published_llm so "selected LLM" always exists. |

### Env configuration

| Issue | Severity | Notes |
|-------|----------|--------|
| **Conflicting env:** e.g. `SUPPORTED_LLM_PROVIDERS=vertex` but default_published_llms contains an OpenAI entry; empty or malformed `DEFAULT_PUBLISHED_LLMS`; model ID in default doesn’t exist. | Medium | Validate at startup or when building defaults: filter default_published_llms to supported providers only; validate JSON and model existence; log warnings and fall back to empty list or primary_config-derived single entry. |
| **Flag semantics:** Plan previously mentioned `LLM_CONFIG_LOCKED`, `DEFAULT_PUBLISHED_LLMS_LOCKED`, `USE_SERVER_LLM_ENV`. | Medium | **Clarified:** Use a single **`LLM_CONFIG_LOCKED`** for (1) lock UI, (2) server ignores req.LLMConfig, (3) no secrets in defaults response, (4) default published LLMs list locked. No separate flags unless a future use case requires "lock published list only." |

### Omissions (not yet in plan)

| Topic | Severity | Suggestion |
|-------|----------|------------|
| **Error handling:** Locked mode but user sends wrong provider/model. | Low–Medium | Server already ignores req.LLMConfig when locked; return 200 with server-chosen config. Optionally return a hint in response that config was server-overridden. |
| **Key rotation:** Rotate server-side API keys without downtime. | Low | Standard: update env and restart or use a secrets manager that reloads; no change to this feature. |
| **Audit logging:** Log when server-side keys are used for a request. | Low–Medium | Add structured log (e.g. "llm_config_locked=true, provider=vertex") per request for audit. |
| **Rate limiting:** When keys are server-controlled, abuse can exhaust quota. | Low–Medium | Handle at infra or API gateway; document recommendation. |
| **Mid-session lock change:** Defaults loaded once on first load; admin changes lock state later. | Low | Frontend could refetch defaults on interval or on focus; or document that lock change requires refresh. |

---

## Summary

| Topic | Status | Implementation |
|-------|--------|----------------|
| Where provider/model/API keys come from for a query | ✅ Implemented | When `LLM_CONFIG_LOCKED=true`, server ignores `req.LLMConfig` and uses env via `buildProviderAPIKeysFromEnv()` |
| Restrict which providers show in UI | ✅ Implemented | `SUPPORTED_LLM_PROVIDERS` filters providers in defaults API and UI |
| Prevent users from changing provider/keys in UI | ✅ Implemented | `llm_config_locked` in defaults API; frontend shows read-only modal |
| Use API keys from env on the server for the LLM call | ✅ Implemented | When locked, `buildProviderAPIKeysFromEnv()` reads keys from `os.Getenv()` and passes to agent |
| Default published LLMs | ✅ Implemented | `default_published_llms` and `default_published_llms_locked` in defaults API; frontend merges or replaces savedLLMs |
| Strip secrets from API response | ✅ Implemented | `stripSecretsFromMap()` removes `api_key` and `endpoint` recursively when locked |
