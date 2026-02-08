# Multi-Provider Authentication

This document describes how to configure authentication for the MCP Agent Builder application deployed on Kubernetes.

## Overview

The application supports multiple authentication providers:

| Provider | Type | Description |
|----------|------|-------------|
| `simple` | Credentials | Username/password from `AUTH_USERS` env var |
| `cognito` | OAuth | AWS Cognito Hosted UI |
| `supabase` | OAuth/Credentials | Supabase Auth |

## Configuration

Authentication is configured via environment variables in `shared/configmap.yaml`.

### Basic Settings

```yaml
# Enable multi-user mode (required for any authentication)
MULTI_USER_MODE: "true"

# Comma-separated list of enabled providers
AUTH_PROVIDERS: "cognito"  # Options: simple, cognito, supabase
```

### Simple Provider (AUTH_USERS)

For simple deployments without external OAuth:

```yaml
AUTH_PROVIDERS: "simple"
AUTH_USERS: "admin:password123,user1:secret456"
```

Users are defined in `user:password` format, comma-separated.

### AWS Cognito Provider

```yaml
AUTH_PROVIDERS: "cognito"
COGNITO_USER_POOL_ID: "ap-south-1_XXXXXXXXX"
COGNITO_CLIENT_ID: "xxxxxxxxxxxxxxxxxxxxxxxxxx"
COGNITO_DOMAIN: "your-domain.auth.ap-south-1.amazoncognito.com"
AWS_REGION: "ap-south-1"
```

### Supabase Provider

```yaml
AUTH_PROVIDERS: "supabase"
SUPABASE_URL: "https://xxx.supabase.co"
SUPABASE_ANON_KEY: "eyJxxx..."
```

### Multiple Providers

You can enable multiple providers simultaneously:

```yaml
AUTH_PROVIDERS: "simple,cognito"
```

Users will see buttons for each provider on the login page.

---

## AWS Cognito Setup

### Prerequisites

- AWS CLI configured with appropriate permissions
- Target region for Cognito (should match your deployment region)

### Step 1: Create User Pool

```bash
REGION="ap-south-1"
POOL_NAME="mcpagent-auth"

aws cognito-idp create-user-pool \
  --pool-name "$POOL_NAME" \
  --region $REGION \
  --auto-verified-attributes email \
  --username-attributes email \
  --username-configuration CaseSensitive=false \
  --policies 'PasswordPolicy={MinimumLength=8,RequireUppercase=true,RequireLowercase=true,RequireNumbers=true,RequireSymbols=false}' \
  --admin-create-user-config 'AllowAdminCreateUserOnly=false' \
  --account-recovery-setting 'RecoveryMechanisms=[{Priority=1,Name=verified_email}]'
```

Note the `Id` from the response (e.g., `ap-south-1_YhXWOPgST`).

### Step 2: Add Domain for Hosted UI

```bash
USER_POOL_ID="ap-south-1_YhXWOPgST"
DOMAIN_PREFIX="mcpagent-auth"  # Must be globally unique

aws cognito-idp create-user-pool-domain \
  --domain "$DOMAIN_PREFIX" \
  --user-pool-id "$USER_POOL_ID" \
  --region $REGION
```

Domain will be: `{DOMAIN_PREFIX}.auth.{REGION}.amazoncognito.com`

### Step 3: Create App Client

```bash
USER_POOL_ID="ap-south-1_YhXWOPgST"
CLIENT_NAME="mcpagent-web"
CALLBACK_URL="https://your-domain.com/auth/callback"
LOGOUT_URL="https://your-domain.com"

aws cognito-idp create-user-pool-client \
  --user-pool-id "$USER_POOL_ID" \
  --client-name "$CLIENT_NAME" \
  --region $REGION \
  --no-generate-secret \
  --explicit-auth-flows ALLOW_USER_SRP_AUTH ALLOW_REFRESH_TOKEN_AUTH \
  --supported-identity-providers COGNITO \
  --callback-urls "$CALLBACK_URL" \
  --logout-urls "$LOGOUT_URL" \
  --allowed-o-auth-flows code \
  --allowed-o-auth-scopes openid email profile \
  --allowed-o-auth-flows-user-pool-client \
  --prevent-user-existence-errors ENABLED
```

Note the `ClientId` from the response.

### Step 4: Update ConfigMap

Edit `shared/configmap.yaml`:

```yaml
AUTH_PROVIDERS: "cognito"
COGNITO_USER_POOL_ID: "ap-south-1_YhXWOPgST"
COGNITO_CLIENT_ID: "2gahrd23uppdrlil01naotmvme"
COGNITO_DOMAIN: "mcpagent-auth.auth.ap-south-1.amazoncognito.com"
AWS_REGION: "ap-south-1"
```

### Step 5: Deploy

```bash
./deploy/k8s/scripts/deploy-k8s.sh --build agent frontend
```

---

## User Management

### Cognito: Self-Signup

By default, users can sign up via the Cognito Hosted UI. They'll receive an email verification code.

### Cognito: Admin Create User

```bash
aws cognito-idp admin-create-user \
  --user-pool-id ap-south-1_YhXWOPgST \
  --username user@example.com \
  --user-attributes Name=email,Value=user@example.com \
  --temporary-password TempPass123! \
  --region ap-south-1
```

User will need to change password on first login.

### Cognito: Set Permanent Password

```bash
aws cognito-idp admin-set-user-password \
  --user-pool-id ap-south-1_YhXWOPgST \
  --username user@example.com \
  --password NewPassword123! \
  --permanent \
  --region ap-south-1
```

### Cognito: List Users

```bash
aws cognito-idp list-users \
  --user-pool-id ap-south-1_YhXWOPgST \
  --region ap-south-1
```

### Cognito: Delete User

```bash
aws cognito-idp admin-delete-user \
  --user-pool-id ap-south-1_YhXWOPgST \
  --username user@example.com \
  --region ap-south-1
```

### Cognito: Disable Self-Signup

To allow only admin-created users:

```bash
aws cognito-idp update-user-pool \
  --user-pool-id ap-south-1_YhXWOPgST \
  --admin-create-user-config AllowAdminCreateUserOnly=true \
  --region ap-south-1
```

---

## Authentication Flow

### OAuth Flow (Cognito/Supabase)

```
┌─────────┐     ┌─────────┐     ┌──────────┐     ┌─────────┐
│ Browser │     │Frontend │     │ Backend  │     │ Cognito │
└────┬────┘     └────┬────┘     └────┬─────┘     └────┬────┘
     │               │               │                │
     │ Click Login   │               │                │
     ├──────────────>│               │                │
     │               │ POST /auth/start              │
     │               ├──────────────>│                │
     │               │ {auth_url, state}             │
     │               │<──────────────┤                │
     │               │               │                │
     │ Redirect to Cognito           │                │
     │<──────────────┤               │                │
     │               │               │                │
     │ Login at Cognito Hosted UI    │                │
     ├───────────────────────────────────────────────>│
     │               │               │                │
     │ Redirect to /auth/callback?code=xxx&state=yyy │
     │<───────────────────────────────────────────────┤
     │               │               │                │
     │ GET /auth/callback            │                │
     ├───────────────────────────────>│                │
     │               │               │ Exchange code  │
     │               │               ├───────────────>│
     │               │               │ User info      │
     │               │               │<───────────────┤
     │               │               │                │
     │ {token, user} │               │                │
     │<───────────────────────────────┤                │
     │               │               │                │
     │ Store JWT, redirect to /      │                │
     ├──────────────>│               │                │
```

### Credentials Flow (Simple/Supabase)

```
┌─────────┐     ┌─────────┐     ┌──────────┐
│ Browser │     │Frontend │     │ Backend  │
└────┬────┘     └────┬────┘     └────┬─────┘
     │               │               │
     │ Enter credentials             │
     ├──────────────>│               │
     │               │ POST /auth/login
     │               │ {username, password, provider}
     │               ├──────────────>│
     │               │               │ Validate
     │               │ {token, user} │
     │               │<──────────────┤
     │               │               │
     │ Store JWT, redirect to /      │
     │<──────────────┤               │
```

---

## Troubleshooting

### "No authentication providers configured"

Ensure `AUTH_PROVIDERS` is set and at least one provider is properly configured.

### Cognito: "Invalid or expired state parameter"

- State has a 10-minute expiration
- User may have taken too long to authenticate
- Possible CSRF attack if state was tampered with

### Cognito: "Failed to authenticate with provider"

Check:
1. `COGNITO_CLIENT_ID` is correct
2. `COGNITO_DOMAIN` is correct (full domain, not just prefix)
3. Callback URL in Cognito matches your app URL exactly
4. App client has OAuth flows enabled

### 401 from nginx before reaching app

If you have nginx basic auth at the ingress level, it will block requests before they reach Cognito auth. Either:
1. Remove nginx basic auth annotations from ingress
2. Or pass basic auth credentials in addition to Cognito

---

## Security Considerations

1. **HTTPS Required**: OAuth callback must use HTTPS in production
2. **State Parameter**: Used for CSRF protection, expires after 10 minutes
3. **JWT Expiry**: App JWTs expire after 24 hours
4. **No Client Secret**: App client is configured without a secret (public client) since it's a SPA
5. **Token Storage**: JWT is stored in localStorage - consider sessionStorage for stricter security

---

## Google Workspace SSO (Domain-Restricted)

Google SSO is configured to allow only @citymall.live users to sign in.

### How It Works

1. User clicks "Sign in with AWS Cognito"
2. Cognito Hosted UI shows "Continue with Google" option
3. User signs in with their @citymall.live Google account
4. Google OAuth app (configured as "Internal") restricts to citymall.live domain
5. User is authenticated and redirected back to the app

### Google OAuth Configuration

| Setting | Value |
|---------|-------|
| Google Client ID | `594743484568-fm9fflom1i0unvv8n0la12u6tl6gste6.apps.googleusercontent.com` |
| OAuth Type | Internal (domain-restricted) |
| Allowed Domain | @citymall.live |
| Redirect URI | `https://mcpagent-auth.auth.ap-south-1.amazoncognito.com/oauth2/idpresponse` |

### Adding Google SSO to Another Cognito Pool

```bash
# Add Google as identity provider
aws cognito-idp create-identity-provider \
  --user-pool-id YOUR_POOL_ID \
  --provider-name Google \
  --provider-type Google \
  --provider-details '{"client_id":"GOOGLE_CLIENT_ID","client_secret":"GOOGLE_SECRET","authorize_scopes":"profile email openid"}' \
  --attribute-mapping 'email=email,name=name,username=sub' \
  --region ap-south-1

# Update app client to support Google
aws cognito-idp update-user-pool-client \
  --user-pool-id YOUR_POOL_ID \
  --client-id YOUR_CLIENT_ID \
  --supported-identity-providers COGNITO Google \
  --callback-urls "https://your-domain.com/auth/callback" \
  --allowed-o-auth-flows code \
  --allowed-o-auth-scopes openid email profile \
  --allowed-o-auth-flows-user-pool-client \
  --region ap-south-1
```

---

## Current Configuration

| Setting | Value |
|---------|-------|
| User Pool ID | `ap-south-1_YhXWOPgST` |
| Client ID | `2gahrd23uppdrlil01naotmvme` |
| Domain | `mcpagent-auth.auth.ap-south-1.amazoncognito.com` |
| Region | `ap-south-1` |
| Callback URL | `https://analytics-agent.citymall.live/auth/callback` |
| Self-Signup | Enabled |
| Google SSO | Enabled (@citymall.live only) |

### Test Users (Email/Password)

| User | Password |
|------|----------|
| manish.prakash@citymall.live | Citymall@123 |
| nverdhan@citymall.live | Citymall@123 |
