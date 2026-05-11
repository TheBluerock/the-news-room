# 07 — Auth

## Overview

The auth service issues and validates JWT RS256 tokens and enforces Casbin RBAC policies. It is the only service that knows about users. All other services validate tokens by calling auth's gRPC endpoint — they never implement their own token validation.

---

## JWT RS256

**Algorithm:** RS256 (asymmetric) — the private key signs tokens; other services verify with the public key only.

**Key storage:** RS256 key pair in Vault at `secret/data/auth` (`jwt_private_key`, `jwt_public_key`). Auth service reads at startup and refreshes every 5 minutes.

**Token lifetime:** 24 hours (configurable). Refresh tokens: 30 days.

**Key rotation:**
- New key pair generated in Vault
- 15-minute overlap window: both old and new keys are valid simultaneously
- Auth service publishes new public key to `GET /auth/jwks.json` (JWKS endpoint)
- After 15 minutes, old key removed from JWKS
- Other services that cache the public key must respect the JWKS TTL

**Token claims:**

```json
{
  "sub": "user-uuid",
  "email": "user@example.com",
  "role": "editor",
  "market": "italy",
  "jti": "unique-token-id",
  "iat": 1234567890,
  "exp": 1234654290
}
```

`market` is `null` for admin users who have access to all markets.

---

## Token Revocation

Revoked tokens are added to a Redis blocklist immediately:

```
SET jwt:blocked:<jti> 1 EXAT <token_expiry_unix>
```

TTL is set to match token expiry — the key auto-expires when the token would have anyway, preventing unbounded growth.

Any service validating a token must check the blocklist before accepting it. Checking happens in the auth gRPC `ValidateToken` method — callers do not implement their own blocklist check.

**Revocation triggers:**
- User logout
- Admin force-logout
- Password change
- GDPR `DELETE /api/user/data` request

---

## Casbin RBAC

**Model:** Subject-Permission (p) rules stored in PostgreSQL `auth_svc.casbin_rule` table. Loaded into memory at startup. Changes take effect within 30 seconds (polling interval).

### Roles

| Role | Description | Market scope |
|------|-------------|-------------|
| `admin` | Full access, user management, audit log read | All markets |
| `editor` | Read/write articles, submit corrections, approve/reject | Assigned market only |
| `viewer` | Read-only access to published articles | Assigned market only |
| `moderation-service` | Internal — write to moderation topics | All markets |
| `analytics-service` | Internal — read trending, write quality scores | All markets |

### Example Policies

```
p, admin, /api/admin/*, *
p, admin, /api/*/audit, GET
p, editor, /api/articles/*, GET
p, editor, /api/articles/*, POST
p, editor, /api/corrections/*, POST
p, viewer, /api/articles/*, GET
```

### Market Scoping

Editor and viewer roles have an assigned market. A request from an editor with `market: italy` to `/api/articles/usa/...` is rejected at the RBAC layer, before reaching any business logic.

Admin users have `market: null` — this bypasses market scoping checks.

---

## gRPC API

```protobuf
service AuthService {
    rpc ValidateToken(ValidateTokenRequest) returns (ValidateTokenResponse);
    rpc IssueToken(IssueTokenRequest) returns (IssueTokenResponse);
    rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
    rpc CheckPermission(CheckPermissionRequest) returns (CheckPermissionResponse);
}
```

**ValidateToken:** Checks signature, expiry, and Redis blocklist. Returns user claims on success.

**CheckPermission:** Casbin enforcement. Other services call this for resource-level authorization beyond what the JWT claims provide.

---

## Health and Observability

Auth exposes on port 8090:
- `/health` — always 200
- `/ready` — 200 when PostgreSQL (for casbin_rule), Redis (for blocklist), and Vault (for JWT keys) are all reachable
- `/metrics` — Prometheus: `auth_token_validations_total{result}`, `auth_token_issues_total`, `auth_permission_checks_total{result}`

---

## GDPR: User Data Deletion

`DELETE /api/user/data` (requires authenticated user or admin):

1. Publishes `user.data.deletion.requested` event to RedPanda (1 partition, guaranteed order)
2. Soft-deletes user row: sets `deleted_at = now()` in `auth_svc.users`
3. Revokes all active tokens for that user (Redis blocklist)
4. Other services consume `user.data.deletion.requested` and anonymise their data within 30 days

The event payload includes `user_id` and `requested_at`. Each consuming service is responsible for its own anonymisation logic.

---

## Frontend Integration

The public site is a statically-generated Astro + Svelte app deployed on Vercel; it contains no editor functionality and does not authenticate.

The Admin UI (editorial dashboard, moderation queue, correction form) lives in a separate application — not part of the public site, not in this repo. The admin app stores the JWT in an `httpOnly` cookie (not localStorage). The cookie is sameSite=Strict, Secure in production. Refresh is handled via a silent background request to `/api/auth/refresh` before expiry.

The admin UI does not perform RBAC — it renders based on claims in the JWT, but all enforcement happens in the auth service and individual service middleware.
