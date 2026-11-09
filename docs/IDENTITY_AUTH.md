# Identity & Authorization

## Roles

- **Freelancer** = authenticated user. `deals.freelancer_id` points at `users`.
  Deal creation, artifact/verification upload, submit, payee-invoice and share-
  link rotation are all freelancer-owner-gated.

- **Client** = just an email address. `deals.client_email` (lowercased) is
  captured at deal creation; the client needs **no account** to receive the
  link, pay, and even approve delivery straight from it
  (`POST /public/deals/:shareToken/approve` — the share token is the
  authorization). Milestone accept/reject/exit are also share-token-only
  (`POST /public/deals/:shareToken/milestones/:milestoneID/accept-next`,
  `.../reject`, `POST /public/deals/:shareToken/exit`). Disputing still
  requires an account: they sign up with that same email; the JWT carries
  `email` (set by `middleware/auth.go`), and
  `strings.EqualFold(deal.ClientEmail, email)` authorizes dispute, authed
  approve, and deal viewing. Symmetric exit is allowed for both participants
  via `POST /deals/:dealID/exit` (authed) or the public exit endpoint.

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
| `GET /auth/me` | ✓ (self) | ✓ (self) | ✓ (self) |
| `GET /public/deals/:shareToken` | public (share token) | public (share token) | public (share token) |
| `GET /public/deals/:shareToken/milestones` | public (share token) | public (share token) | public (share token) |
| `POST /public/deals/:shareToken/approve` | public (share token) | public (share token) | public (share token) |
| `POST /public/deals/:shareToken/milestones/:milestoneID/accept-next` | public (share token) | public (share token) | public (share token) |
| `POST /public/deals/:shareToken/milestones/:milestoneID/reject` | public (share token) | public (share token) | public (share token) |
| `POST /public/deals/:shareToken/exit` | public (share token) | public (share token) | public (share token) |
| `POST /deals/:dealID/milestones` | ✓ | ✗ | ✗ |
| `GET /deals/:dealID/milestones` | ✓ | ✓ | ✓ |
| `PATCH /deals/:dealID/milestones/:milestoneID` | ✓ | ✗ | ✗ |
| `POST /deals/:dealID/exit` | ✓ | ✓ | ✗ |
| `POST /disputes/deals/:dealID/milestones/:milestoneID/resolve` | ✗ | ✗ | ✓ |
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
- **Link-bearer-scoped**: public approve (`POST /public/deals/:shareToken/approve`) — share token authorizes the same settle+payout as authed approve; dispute is deliberately excluded. Public milestone accept-next/reject/exit are similarly share-token-only (reject/exit post-submission go to review, not a money move).
- **Freelancer-scoped**: submit, payee-invoice, share-link, milestone create/update (must be deal owner)
- **Participant-scoped (exit)**: `POST /deals/:dealID/exit` accepts freelancer owner **or** client by email match (symmetric exit; pre-submission refunds, post-submission → dispute)
- **Generic status PATCH**: freelancer-only but **blocked** from money states
  (`locked`, `released`, `disputed`, `refunded`)
- **Operator-scoped**: dispute queue, deal resolve, milestone resolve (`POST /disputes/deals/:dealID/milestones/:milestoneID/resolve`), reconcile