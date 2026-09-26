# Identity & Authorization

## Roles

- **Freelancer** = authenticated user. `deals.freelancer_id` points at `users`.
  Deal creation, artifact/verification upload, submit, payee-invoice and share-
  link rotation are all freelancer-owner-gated.

- **Client** = just an email address. `deals.client_email` (lowercased) is
  captured at deal creation; the client needs **no account** to receive the
  link and pay. When they're ready to review, they sign up with that same
  email; the JWT carries `email` (set by `middleware/auth.go`), and
  `strings.EqualFold(deal.ClientEmail, email)` authorizes approve/dispute and
  deal viewing.

- **Operator** = promoted from `OPERATOR_EMAILS` env var at boot. Role rides
  in the access token as `is_operator` claim. Routes sit behind
  `OperatorRequired` middleware.

## Endpoint Access Matrix

| Endpoint | Freelancer | Client (email match) | Operator |
|---|---|---|---|
| `POST /deals` | ✓ | ✗ | ✗ |
| `GET /deals` | ✓ | ✓ | ✓ |
| `GET /deals/:id` | ✓ | ✓ | ✓ |
| `GET /deals/:id/payment` | ✓ | ✗ | ✓ |
| `PATCH /deals/:id/status` | ✓ (non-money) | ✗ | ✓ |
| `POST /deals/:id/submit` | ✓ | ✗ | ✗ |
| `POST /deals/:id/approve` | ✗ | ✓ | ✗ |
| `POST /deals/:id/dispute` | ✗ | ✓ | ✗ |
| `PATCH /deals/:id/payee-invoice` | ✓ | ✗ | ✗ |
| `POST /deals/:id/share-link` | ✓ | ✗ | ✗ |
| `GET /public/deals/:shareToken` | ✓ | ✓ (no auth) | ✓ |
| `GET /deals/:id/artifacts` | ✓ | ✓ | ✓ |
| `GET /deals/:id/artifacts/:artifactID` | ✓ | ✓ | ✓ |
| `GET /deals/:id/artifacts/:artifactID/download` | ✓ | ✓ | ✓ |
| `GET /disputes` | ✗ | ✗ | ✓ |
| `POST /disputes/:id/resolve` | ✗ | ✗ | ✓ |
| `POST /deals/:id/reconcile` | ✗ | ✗ | ✓ |
| `GET /cv/:slug` | public | public | public |
| `GET /cv/:slug/verify/:entryID` | public | public | public |

## Key Implementation Details

- JWT contains: `user_id` (freelancer's UUID), `email` (for client matching), `is_operator` (bool)
- `AuthRequired` middleware validates JWT, sets `userID` and `email` in Gin context
- `OperatorRequired` middleware checks `is_operator` claim
- Client authorization: `strings.EqualFold(deal.ClientEmail, email_from_token)` — case-insensitive
- Freelancer authorization: `deal.FreelancerID == userID_from_token`
- Public share link: no auth, `share_token` only

## Money-State Endpoints

- **Client-scoped**: approve, dispute (must match `client_email`)
- **Freelancer-scoped**: submit, payee-invoice, share-link (must be deal owner)
- **Generic status PATCH**: freelancer-only but **blocked** from money states
  (`locked`, `released`, `disputed`, `refunded`)
- **Operator-scoped**: dispute queue, resolve, reconcile