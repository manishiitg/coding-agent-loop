# Multi-User Authentication & Workspace Isolation

This document describes the multi-provider authentication system and per-user workspace isolation feature.

## Table of Contents

1. [Overview](#overview)
2. [Authentication Modes](#authentication-modes)
3. [Authentication Providers](#authentication-providers)
4. [Per-User Workspace Isolation](#per-user-workspace-isolation)
5. [Configuration](#configuration)
6. [API Reference](#api-reference)
7. [Architecture](#architecture)

---

## Overview

The MCP Agent Builder supports two modes of operation:

| Mode | Description | Use Case |
|------|-------------|----------|
| **Single-User** | No authentication required, uses default user ID | Local development, personal use |
| **Multi-User** | JWT authentication with multiple provider support | Team deployment, production |

In both modes, workspace files are organized per-user to ensure data isolation.

---

## Authentication Modes

### Single-User Mode (Default)

When `MULTI_USER_MODE` is not set or set to `false`:

- No login required
- All requests use a default user ID (`DEFAULT_USER_ID` env or `"default-user"`)
- Per-user folders are stored under `/_users/default-user/`
- Suitable for local development and personal deployments

### Multi-User Mode

When `MULTI_USER_MODE=true`:

- JWT authentication required for all API requests
- Multiple authentication providers supported
- Each user gets isolated workspace folders
- Per-user folders stored under `/_users/{userID}/`

---

## Authentication Providers

The system supports multiple authentication providers that can be enabled simultaneously.

### Available Providers

| Provider | Type | Description |
|----------|------|-------------|
| `simple` | Credentials | Username/password from environment variable |
| `cognito` | OAuth | AWS Cognito User Pool with hosted UI |
| `supabase` | OAuth | Supabase Auth |

### Simple Provider

Basic username/password authentication using environment variables.

**Configuration:**
```bash
AUTH_PROVIDERS=simple
AUTH_USERS=admin:password123,user1:secret456
```

**Features:**
- No external service dependencies
- Users defined directly in environment
- Good for development and simple deployments

### Cognito Provider

OAuth authentication via AWS Cognito hosted UI.

**Configuration:**
```bash
AUTH_PROVIDERS=cognito
COGNITO_USER_POOL_ID=us-east-1_xxxxx
COGNITO_CLIENT_ID=xxxxxxxxx
COGNITO_DOMAIN=myapp.auth.us-east-1.amazoncognito.com
AWS_REGION=us-east-1
```

**Features:**
- Google Workspace integration
- Enterprise SSO support
- User pool management via AWS Console

### Supabase Provider

OAuth authentication via Supabase Auth.

**Configuration:**
```bash
AUTH_PROVIDERS=supabase
SUPABASE_URL=https://xxx.supabase.co
SUPABASE_ANON_KEY=eyJxxx
```

**Features:**
- Multiple social login options
- Email/password authentication
- Row-level security integration

### Multiple Providers

Enable multiple providers simultaneously:

```bash
AUTH_PROVIDERS=simple,cognito,supabase
AUTH_USERS=admin:password123
COGNITO_USER_POOL_ID=us-east-1_xxxxx
COGNITO_CLIENT_ID=xxxxxxxxx
COGNITO_DOMAIN=myapp.auth.us-east-1.amazoncognito.com
SUPABASE_URL=https://xxx.supabase.co
SUPABASE_ANON_KEY=eyJxxx
```

The login page will display all configured providers.

---

## Per-User Workspace Isolation

### Folder Structure

The workspace uses a hybrid folder model:

```
/app/workspace-docs/
├── _users/                    # Per-user isolated folders
│   ├── default/               # Fallback for single-user mode
│   │   ├── Chats/             # User's chat history
│   │   └── Downloads/         # User's downloads
│   └── user-abc123/           # Multi-user: each user gets own folder
│       ├── Chats/
│       └── Downloads/
├── skills/                    # Shared across all users
├── Workspace/                 # Shared across all users
└── Workflow/                  # Shared across all users
```

### Folder Classification

| Folder | Type | Description |
|--------|------|-------------|
| `Chats/` | **Per-User** | Chat session outputs, user-specific |
| `Downloads/` | **Per-User** | User downloads and imports |
| `skills/` | **Shared** | Installed skills/templates |
| `Workspace/` | **Shared** | General workspace metadata |
| `Workflow/` | **Shared** | Workflow definitions and runs |

### How It Works

1. **User ID Resolution:**
   - Multi-user mode: User ID from JWT token claims
   - Single-user mode: Default user ID from environment

2. **Path Routing:**
   - Requests to `Chats/*` or `Downloads/*` → `/_users/{userID}/Chats/*`
   - Requests to `skills/*`, `Workspace/*`, `Workflow/*` → root level

3. **Automatic Migration:**
   - On startup, existing `Chats/` and `Downloads/` at root level are migrated to `/_users/default/`
   - One-time migration for backwards compatibility

### User ID Flow

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   AuthMiddleware│ ──► │  Agent Context  │ ──► │ Workspace Client│
│   (extracts ID) │     │  (user_id key)  │     │ (X-User-ID hdr) │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                                        │
                                                        ▼
                                                ┌─────────────────┐
                                                │  Workspace API  │
                                                │ (resolves path) │
                                                └─────────────────┘
```

---

## Configuration

### Environment Variables

#### Core Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `MULTI_USER_MODE` | `false` | Enable multi-user authentication |
| `AUTH_SECRET` | dev default | JWT signing secret (required in production) |
| `DEFAULT_USER_ID` | `default-user` | Default user ID for single-user mode |

#### Simple Provider

| Variable | Required | Description |
|----------|----------|-------------|
| `AUTH_USERS` | Yes | Comma-separated `user:pass` pairs |

#### Cognito Provider

| Variable | Required | Description |
|----------|----------|-------------|
| `COGNITO_USER_POOL_ID` | Yes | AWS Cognito User Pool ID |
| `COGNITO_CLIENT_ID` | Yes | Cognito App Client ID |
| `COGNITO_DOMAIN` | Yes | Cognito hosted UI domain |
| `AWS_REGION` | Yes | AWS region (e.g., `us-east-1`) |

#### Supabase Provider

| Variable | Required | Description |
|----------|----------|-------------|
| `SUPABASE_URL` | Yes | Supabase project URL |
| `SUPABASE_ANON_KEY` | Yes | Supabase anonymous key |

### Example Configurations

#### Development (Single-User)

```bash
# No authentication required
MULTI_USER_MODE=false
```

#### Development with Simple Auth

```bash
MULTI_USER_MODE=true
AUTH_PROVIDERS=simple
AUTH_USERS=admin:admin123
AUTH_SECRET=dev-secret-change-me
```

#### Production with Cognito

```bash
MULTI_USER_MODE=true
AUTH_PROVIDERS=cognito
AUTH_SECRET=your-production-secret
COGNITO_USER_POOL_ID=us-east-1_xxxxx
COGNITO_CLIENT_ID=xxxxxxxxx
COGNITO_DOMAIN=myapp.auth.us-east-1.amazoncognito.com
AWS_REGION=us-east-1
```

---

## API Reference

### Authentication Endpoints

#### Get Auth Mode

```http
GET /api/auth/mode
```

**Response:**
```json
{
  "multi_user_mode": true,
  "providers": [
    {"name": "simple", "type": "credentials"},
    {"name": "cognito", "type": "oauth"}
  ]
}
```

#### Login (Credentials)

```http
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "password123",
  "provider": "simple"
}
```

**Response:**
```json
{
  "token": "eyJhbGc...",
  "user": {
    "user_id": "abc123",
    "username": "admin",
    "provider": "simple"
  }
}
```

#### Start OAuth Flow

```http
GET /api/auth/start?provider=cognito
```

**Response:** Redirects to OAuth provider

#### OAuth Callback

```http
GET /api/auth/callback?provider=cognito&code=xxx&state=xxx
```

**Response:** Exchanges code for app JWT and redirects to frontend

### Workspace API Headers

The workspace API uses the `X-User-ID` header for per-user folder routing:

```http
GET /api/documents?folder=Chats
X-User-ID: user-abc123
```

This header is automatically set by the agent API based on the authenticated user.

---

## Architecture

### Authentication Flow

```
┌──────────┐                                    ┌──────────────┐
│ Frontend │  1. GET /api/auth/mode             │   Backend    │
│          │◄──────────────────────────────────►│              │
│          │  2. Show provider buttons          │              │
│          │                                    │              │
│          │  3a. POST /api/auth/login (simple) │              │
│          │──────────────────────────────────►│              │
│          │◄──────────────────────────────────│              │
│          │  4a. JWT token                     │              │
│          │                                    │              │
│          │  3b. GET /api/auth/start (OAuth)   │              │
│          │──────────────────────────────────►│              │
│          │  4b. Redirect to OAuth provider    │              │
│          │                                    │              │
│          │  5. OAuth callback with code       │              │
│          │◄──────────────────────────────────│              │
│          │  6. App JWT token                  │              │
└──────────┘                                    └──────────────┘
```

### Key Files

#### Backend

| File | Description |
|------|-------------|
| `agent_go/cmd/server/auth_middleware.go` | JWT validation, user context |
| `agent_go/cmd/server/auth_providers.go` | Provider interface, implementations |
| `agent_go/cmd/server/user_auth_routes.go` | Login, OAuth routes |
| `agent_go/pkg/workspace/client.go` | Workspace client with user ID |
| `agent_go/pkg/common/types.go` | Context keys including `UserIDKey` |

#### Workspace API

| File | Description |
|------|-------------|
| `workspace/utils/path.go` | Per-user path resolution |
| `workspace/handlers/documents.go` | Document handlers with user routing |
| `workspace/server.go` | Startup migration |

#### Frontend

| File | Description |
|------|-------------|
| `frontend/src/stores/useAuthStore.ts` | Auth state management |
| `frontend/src/pages/Login.tsx` | Login page with providers |
| `frontend/src/pages/AuthCallback.tsx` | OAuth callback handler |

---

## Security Considerations

### JWT Tokens

- Tokens expire after 24 hours
- Signed with HMAC-SHA256
- Contains: user_id, username, email, provider

### Password Storage

- Simple provider stores passwords in environment variables
- Not suitable for production with many users
- Use OAuth providers for production deployments

### User ID Validation

- User IDs are sanitized (alphanumeric, hyphens, underscores only)
- Maximum length: 128 characters
- Invalid IDs fall back to `"default"`

### Path Security

- All paths validated against directory traversal attacks
- Per-user folders isolated under `/_users/{userID}/`
- Users cannot access other users' files through API

---

## Migration

### From Single-User to Multi-User

1. Set `MULTI_USER_MODE=true`
2. Configure at least one auth provider
3. Existing files in `Chats/` and `Downloads/` will be migrated to `/_users/default/`
4. Existing users can continue with the same data after migration

### From Legacy Workspace

On first startup with this feature:

1. Server checks for existing `Chats/` and `Downloads/` at root level
2. If found with content, moves them to `/_users/default/`
3. Creates per-user folder structure
4. Shared folders remain unchanged

No manual intervention required - migration is automatic and one-time.
