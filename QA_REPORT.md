# QA Report - Ganji Project

**Date:** 2025-10-30  
**Reviewed:** Backend (Go/Gin), Frontend (Next.js/TypeScript), Architecture, Tests

## Executive Summary

Ganji is a well-architected Lightning Network escrow system with strong security design around money flows, preimage handling, and dispute resolution. The codebase demonstrates careful attention to double-payment prevention, atomic transitions, and security. However, there are several bugs, weaknesses, and improvement opportunities across backend, frontend, and infrastructure.

---

## CRITICAL ISSUES (Fix Immediately)

### Backend Security/Correctness

#### 1. CORS Configuration May Be Too Permissive
- **Location:** `backend/cmd/api/router.go:80-86`
- **Issue:** CORS allows all origins from `cfg.FrontendURLs` with `AllowCredentials: true`. If the env var is misconfigured or empty array, behavior depends on library - could be problematic. Also accepts credentials with wildcard-like configs.
- **Risk:** Cross-site request forgery or credential leakage
- **Recommendation:** Ensure only explicit trusted origins, validate config on startup, consider stricter headers

#### 2. JWT Secret Handling
- **Location:** `backend/internal/config/config.go:92-94`
- **Issue:** Both `JWT_SECRET` and `JWT_REFRESH_SECRET` are required but there's no minimum entropy/length validation
- **Risk:** Weak secrets could be brute-forced
- **Recommendation:** Enforce minimum length (e.g., 32+ bytes), fail fast on weak secrets in production

#### 3. Rate Limiting Not Implemented
- **Location:** `backend/internal/middleware/ratelimit.go` (empty file), mentioned as stub in docs
- **Issue:** Public endpoints (auth/login, auth/signup, webhook, public share links) have no rate limiting
- **Risk:** Brute force on passwords, token enumeration, DoS
- **Recommendation:** Implement rate limiting (e.g., Redis-based or in-memory) especially for auth endpoints and public routes

#### 4. Database Connection in Health Check
- **Location:** `backend/internal/health/handler.go:24-44`, `backend/internal/db/connection.go:41`
- **Issue:** Health handler correctly uses `PingContext` with timeout (good), but `db.Open`'s `pingWithRetry` uses `dbConn.Ping()` without context - blocks during startup retries. Also noted in known issues #15 partially addressed? Actually health handler DOES use PingContext now - looking at the code, line 24-44 in health/handler.go uses `PingContext(ctx)` correctly. Known issue says "health/handler.go... calls dbConn.Ping() (unbounded context)" but current code shows PingContext is used. Maybe already fixed? Or different version.
- **Status:** Current code looks correct (uses PingContext). Good.

#### 5. Webhook Signature Verification
- **Location:** `backend/internal/webhook/handler.go`, signature verification exists
- **Issue:** When `LNBITS_WEBHOOK_SECRET` is empty, signature verification is skipped (`if h.secret != "" && ...`). In production this is dangerous.
- **Risk:** Anyone can POST fake webhook events
- **Recommendation:** Make webhook secret required in production, log warning in dev if missing, or reject webhooks if secret not configured

---

## HIGH PRIORITY ISSUES

### Backend

#### 6. Error Information Leakage
- **Location:** Various handlers (e.g., `webhook/handler.go:134-147`, `deals/handler.go` error handling)
- **Issue:** Some errors logged with full details but responses might leak info. Also in webhook, on internal errors it logs and returns generic message - good. But need to ensure no stack traces or internal details leak to clients.
- **Recommendation:** Audit all error responses to ensure they don't expose internal paths, SQL errors, etc.

#### 7. Refresh Token Storage & Cleanup
- **Location:** `backend/internal/auth/` - known issue says "Refresh-token GC: `refresh_tokens` rows are never cleaned up after expiry"
- **Issue:** Table grows unbounded as tokens accumulate
- **Risk:** DB bloat, performance degradation
- **Recommendation:** Add periodic cleanup job (cron/worker), or cleanup on refresh/logout, add index on expires_at

#### 8. Orphaned Files in Storage
- **Location:** `backend/internal/storage/` - files uploaded to artifacts
- **Issue:** When deals are deleted? No delete endpoints exist per docs. Also failed uploads, or artifacts for deleted deals - files accumulate forever
- **Risk:** Disk space exhaustion
- **Recommendation:** Implement cleanup strategy, add deletion endpoints, track storage usage

#### 9. Preimage Storage Security
- **Location:** `backend/internal/deals/types.go:108-155`, deal model stores `Preimage` in DB (cleared in views but stored)
- **Issue:** Preimage is the escrow settle secret - if DB is compromised, attacker could settle holds. It's needed for settlement but extremely sensitive.
- **Risk:** Fund theft if DB leaked
- **Recommendation:** Consider encryption at rest for preimages, or key derivation, audit access logs, ensure DB backups are encrypted

#### 10. Email Notifications - Missing Client Notifications
- **Location:** `backend/internal/email/service.go`, known issue #16
- **Issue:** Only freelancers notified, clients get no updates (payment confirmation, work submitted, etc.)
- **Risk:** Poor UX, clients may not know to approve
- **Recommendation:** Extend notifier to handle client emails

#### 11. Context Timeout Handling
- **Location:** Various places - e.g., LNbits calls might not have proper timeouts
- **Issue:** External API calls need timeouts to prevent hanging
- **Recommendation:** Ensure all HTTP calls to LNbits have timeouts, context cancellation propagated

### Frontend

#### 12. Token Storage in localStorage
- **Location:** Frontend API client (mentioned in report)
- **Issue:** JWT tokens in localStorage are vulnerable to XSS
- **Risk:** Token theft if XSS occurs
- **Recommendation:** Consider httpOnly cookies for refresh tokens, or at least be aware of XSS risk. Add CSP headers. Short-lived access tokens.

#### 13. Missing Error Boundaries
- **Issue:** No React error boundaries visible in structure
- **Risk:** App crashes on component errors
- **Recommendation:** Add error boundaries

#### 14. No Input Sanitization on Frontend
- **Issue:** Forms send data as-is; backend validates but frontend should sanitize
- **Risk:** XSS if data rendered unsafely (check how data is rendered)
- **Recommendation:** Ensure React's built-in escaping is used, validate on frontend too

#### 15. API Client Missing Methods
- **Location:** `FRONTEND_REPORT.md` mentions `GET /auth/me` not in client, CV types missing
- **Issue:** Incomplete API coverage
- **Recommendation:** Add missing methods for settings, CV pages

---

## MEDIUM PRIORITY ISSUES

### Backend

#### 16. Logging
- **Issue:** Uses `log.Printf` throughout, no structured logging, no request correlation IDs
- **Recommendation:** Migrate to `slog` (Go 1.21+), add trace/request IDs

#### 17. Configuration Validation
- **Issue:** No validation that LNbits is configured if needed? Or that webhook URL format is valid
- **Recommendation:** Validate URLs, required pairs (e.g., if LNBitsURL set, need keys)

#### 18. SQL Injection Protection
- **Status:** Using parameterized queries with `database/sql` and prepared statements pattern - looks safe. Good.

#### 19. Password Policy
- **Location:** `backend/internal/auth/service.go:78-99` - only checks min 8 chars
- **Issue:** Weak password policy
- **Recommendation:** Consider complexity requirements, or at least check common passwords

#### 20. Slug Generation
- **Location:** `auth/service.go:180-235` - could have collisions, retries up to 100 times
- **Issue:** If many users have similar names, could be slow or fail; also potential enumeration
- **Recommendation:** Use UUID suffix or better strategy

#### 21. Transaction Handling
- **Status:** Generally good - uses transactions properly, rollback in defer, careful with commit errors
- **Issue:** In some places error handling in deferred rollbacks could mask issues? Defer rollback is safe after commit (returns ErrTxDone) as noted in comments - good.

#### 22. Time Zones
- **Issue:** Using `time.Now()` without timezone awareness - server time matters for expiries
- **Recommendation:** Ensure UTC everywhere, document server TZ

### Frontend

#### 23. TypeScript Strictness
- **Location:** `frontend/tsconfig.json` not shown but types exist
- **Issue:** Need to verify strict mode is enabled
- **Recommendation:** Ensure `strict: true`

#### 24. No Loading States Consistency
- **Issue:** Some components may lack loading states
- **Recommendation:** Audit all async operations

#### 25. Accessibility
- **Issue:** No obvious a11y considerations mentioned
- **Recommendation:** Add ARIA labels, keyboard navigation, focus management

---

## LOW PRIORITY / IMPROVEMENTS

### Backend

#### 26. Dead Code
- **Known issue:** `pkg/hash/preimage.go` empty, `pkg/sanitize` empty, `ErrPaymentNotPaid` unused
- **Recommendation:** Remove dead code

#### 27. API Versioning
- **Issue:** No API versioning (e.g., `/v1/`)
- **Recommendation:** Consider for future compatibility

#### 28. Metrics/Observability
- **Issue:** No metrics (Prometheus), tracing
- **Recommendation:** Add basic metrics for critical paths

#### 29. Docker/Deployment
- **Files exist:** `docker-compose.yml`, `lnbits-docker-compose.yml`, `backend/Dockerfile`
- **Recommendation:** Review for security (non-root user, multi-stage builds)

### Frontend

#### 30. Bundle Size
- **Issue:** Next.js 16 with minimal deps - good so far
- **Recommendation:** Monitor as app grows

#### 31. Image Optimization
- **Issue:** If images added later, use Next Image

#### 32. SEO
- **Issue:** Marketing pages need meta tags

---

## POSITIVE OBSERVATIONS

✅ **Excellent money flow design** - Two-phase release with payout attempt tracking prevents double payments  
✅ **Proper transaction handling** - Row locking, careful phase separation (DB vs network)  
✅ **Security-conscious** - Secrets redacted from API responses, preimage handling careful  
✅ **Good test coverage** - Tests exist for auth, deals, cv, lnbits, webhook, middleware  
✅ **Dispute resolution properly gated** - Operator-only, can't be auto-resolved  
✅ **Share link security** - Revocable tokens, not exposing internal IDs  
✅ **Webhook hardening** - Body size limits, signature verification  
✅ **Graceful shutdown** - Proper signal handling in main  
✅ **Migration system** - Using golang-migrate  
✅ **Type safety** - Good TypeScript usage on frontend  

---

## TESTING OBSERVATIONS

- All backend tests pass (as shown earlier)
- Tests cover critical paths (service tests, handler tests)
- Missing integration tests? Or e2e tests
- Frontend has no test files visible

---

## IMMEDIATE ACTION ITEMS

1. **Implement rate limiting** on auth and public endpoints
2. **Make webhook secret required** in production or fail securely
3. **Add refresh token cleanup job**
4. **Encrypt preimages at rest** or add additional DB security
5. **Add client notifications** via email
6. **Implement GET /auth/me in frontend API client**
7. **Add security headers** (CSP, HSTS) on frontend/backend
8. **Review and fix any CORS misconfiguration risks**

---

## CONCLUSION

This is a well-engineered project with strong security practices around financial operations. The main gaps are operational concerns (rate limiting, cleanup jobs, monitoring) and some UX/completeness items (notifications, frontend pages). The core escrow logic is solid with good defensive programming against race conditions and double payments.
