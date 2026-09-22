# OpenTimestamps (OTS) Anchoring — the "Permanent Hash" explained

**What this doc is:** the whole story of how a completed Ganji deal becomes a
permanent, blockchain-timestamped entry on a freelancer's CV — in plain
English first, then with the jargon mapped so you can navigate the code.

---

## TL;DR (the whole thing in one paragraph)

When a deal is approved, every deliverable file gets a **fingerprint** (a
SHA-256 hash of its exact contents). Ganji sends that fingerprint to a free
public "notary" called an **OpenTimestamps calendar**, which collects millions
of fingerprints a day, folds them all into one single summary number (the
**merkle root**), publishes that number *inside a real Bitcoin transaction*,
and emails back a **receipt** (the `.ots` proof) saying "your fingerprint is
in today's summary, and that summary went into Bitcoin block #891686." From
then on, anyone can check the receipt against the real Bitcoin ledger and
prove, forever, that your file existed with exactly that content on that day —
no Ganji, no middleman, no trust required. That fingerprint, permanently glued
to Bitcoin history, is the "**permanent hash**" of your work.

---

## Why any of this exists

A freelancer's CV needs to say *"I did this work, and it really happened"*.
Ganji verifies hashes, but that only proves *Ganji* recorded something. The
question is: how do you prove it to an outsider, 10 years from now, without
asking anyone to trust Ganji's database?

Bitcoin is the answer: once a fingerprint is baked into a Bitcoin block, that
fact is *public and impossible to rewrite*. OTS is just the cheap, clever way
to bet that many fingerprints can all get baked into Bitcoin using almost no
blockchain space.

---

## The story: mail, a notary, a ledger, and a library

Forget computers for a second. Here is the same idea with paper:

**1. You make a copy of your work and seal it in a box.**
The box's sealed state is your *hash*: you can't open it, but you can prove
the same box hasn't been touched. If one word changes, it's a different box.

**2. You send the sealed box to a notary.**
The notary is an *OpenTimestamps calendar*. Every day the notary gets
thousands of these boxes from thousands of people. It doesn't open them — it
just lines them all up.

**3. The notary folds every box into one page of a ledger.**
The notary combines all day's boxes into one summary number — the *merkle
root*. Change any single box and the summary number changes, so the summary
is a commitment to *all* of them at once.

**4. The notary publishes that page in the world's permanent record.**
The daily summary goes into a transaction that gets mined into Bitcoin. The
*block height* is the page number — "today's summary is on page 891686 of
history."

**5. The notary gives you a receipt.**
The `.ots` proof file says: *"your box was in today's box-list, that list's
summary is X, and X went into block 891686."* The receipt even carries the
exact position of your box in the list and a tiny Bitcoin transaction.

**6. Anyone can verify the receipt at the public library.**
The library is a *blockchain explorer* (esplora / mempool.space / your own
node). They take your receipt, open the real history at page 891686, and check
that the summary on that page really is X. If it matches — your box was there.

That's it. That's the whole system. Every term below is just a name for one of
these six steps.

---

## Jargon → plain English (the map you need)

| Jargon | Plain English | In Ganji's code |
|---|---|---|
| **Artifact** | The deliverable file | blob at `STORAGE_PATH`, key `deals/<dealID>/<hex><ext>` |
| **Hash / digest / SHA-256** | The sealed box: a 64-char fingerprint of the file's exact bytes | `cv_entries.hash`, computed in `cv/service.go insertAnchors` |
| **OpenTimestamps (OTS)** | The protocol + tooling for "get Bitcoin timestamps cheaply" | `internal/ots/` |
| **Calendar / pool** | The free notary service | `a.pool.opentimestamps.org`, `b.pool.opentimestamps.org` (`internal/ots/client.go`) |
| **Digest submission** | Mailing the sealed box to the notary | `POST <calendar>/digest` with the raw 32 bytes |
| **Merkle tree / merkle root** | The daily ledger page: one number that commits to every fingerprint submitted that day | computed by `ots.Sequence.Compute` |
| **.ots proof file** | The receipt: digest + instructions to find your box + the Bitcoin transaction | `cv_entries.ots_proof`, a complete serialized `.ots` file |
| **Pending** | Receipt exists but the ledger page isn't published yet (calendar mines in batches, ~10 min–days) | sequence ends in a *calendar* attestation |
| **Confirmed** | The ledger page is published; the receipt now carries a real block height | sequence ends in a *bitcoin* attestation |
| **Block height** | Page number in Bitcoin's permanent history | `ots_block_height` / `OTSBlockHeight` |
| **Commitment** | The daily summary number your fingerprint belongs to (what "upgrade" asks about) | `seq.Compute(digest)`, hex-encoded into `GET <calendar>/timestamp/<hex>` |
| **Upgrade** | Asking the notary "did my page get published yet?", and if yes getting the receipt with the page number filled in | `ots.Client.Upgrade` |
| **Esplora / chain source** | The public library: a real Bitcoin data feed | `verifyer.NewEsploraClient(url, timeout)` via `OTS_ESPLORA_URL` |
| **Offline verification** | Re-reading the receipt and re-doing the arithmetic — the math holds, but you haven't checked the real library | `ots.VerifyProof` |
| **Chain verification** | Also going to the library and confirming the page really exists with exactly that summary | `ots.Verifier.Verify` with a chain |

---

## How it works in Ganji — file by file

### Step 0 — What gets stored (the data model)

Migration `backend/migrations/000008_add_ots_proof_to_cv_entries.up.sql`
extends the CV entries table (`cv_entries`), one row per artifact anchor:

| Column | Meaning |
|---|---|
| `hash` | The permanent hash: `sha256(artifact file bytes)` as hex |
| `verified_at` | When the deal was approved (the CV's "release time") |
| `ots_proof` | The full `.ots` receipt file (tiny, a few KB) |
| `ots_submitted_at` | When Ganji first mailed the fingerprint to the notary |
| `ots_confirmed_at` | When the worker saw the receipt become Bitcoin-confirmed |

### Step 1 — Approving a deal "releases" the CV entries

`backend/internal/deals/service.go:562` — the approve path calls
`s.cv.AnchorReleasedDeal(freelancerID, dealID)`. The two services are glued
together in `backend/cmd/api/router.go` via `deals.WithCVAnchorer(cvService)`.

### Step 2 — Hashing the files

`backend/internal/cv/service.go` → `AnchorReleasedDeal` → `insertAnchors`:

```go
r, _ := s.store.Open(ctx, c.StorageKey)   // read the deliverable blob
h := s.hasher()                            // sha256
io.Copy(h, r)
digest := h.Sum(nil)                       // ← THE permanent hash
repo.InsertAnchor(ctx, c.ArtifactID, hex(digest))
```

It hashes the **file contents** (not the storage key) — that was a fixed bug
("Bug #11" in `explained.md`). Same bytes in → same hash out. One changed byte
→ a totally different hash, so the hash *is* the file's identity.

### Step 3 — Mailing the fingerprint to the notary (OTS submit)

Right after anchoring, a background goroutine calls
`ots.Client.Submit(ctx, digest)` (`backend/internal/ots/client.go`):

```http
POST https://a.pool.opentimestamps.org/digest      # raw 32-byte digest, not JSON
Accept: application/vnd.opentimestamps.v1          # "send me a receipt"
```

The calendar answers with a *pending* receipt fragment. Ganji wraps it in a
real `.ots` file (`ots.File{Digest, Sequences}`) and serializes it. If the
first pool is down it tries `b.pool.opentimestamps.org`; each pool forwards the
digest to a member calendar, and the receipt records which one.

### Step 4 — Storing the pending receipt

`cv.Service` stores it via
`repo.UpdateAnchorOTS(artifactID, otsProof, &submittedAt, nil)` → sets
`ots_proof`, `ots_submitted_at`, leaves `ots_confirmed_at` NULL.

Now the row's hash is *in the notary's queue* but not yet in Bitcoin. This
whole submit is **best-effort**: if every calendar is down, the CV still works
— the hash is anchored in Ganji; OTS just doesn't happen (it logs and moves
on).

### Step 5 — The upgrade worker (pending → confirmed)

`backend/cmd/api/workers.go` → `runOTSUpgradeLoop` runs `UpgradeOTSProofs`
immediately at boot and then every `OTS_UPGRADE_INTERVAL_SECONDS` (default
6 hours), each run capped at 5 minutes:

1. `repo.ListPendingOTSAnchors` — rows where `ots_proof` is set but
   `ots_confirmed_at` is NULL.
2. For each, `ots.Client.Upgrade(ctx, proof)` (`client.go`):
   - parse the `.ots` file, find the pending sequence;
   - compute the commitment (`seq.Compute(digest)` = the daily summary your
     fingerprint lives in);
   - `GET <calendar>/timestamp/<64-hex-commitment>`:
     - **404** → not mined yet → `ErrProofNotReady` → skip, retry next cycle;
     - **200** → the calendar returns the *bitcoin tail* (the merkle path +
       the real transaction) → splice it onto the proof → re-serialize.
3. On success: `repo.UpdateAnchorOTS(artifactID, upgraded, &submittedAt, &now)`
   → `ots_confirmed_at` is set. The receipt is now *confirmed*.

### Step 6 — Verification: `GET /cv/:slug/verify/:entryID`

`backend/internal/cv/handler.go` → `cv.Service.VerifyEntry`:

```go
valid := rec.Hash == recomputedHex   // re-hash the current blob, compare
...
if len(rec.OTSProof) > 0 && rec.OTSConfirmedAt != nil {
    ver, err := s.verifyOTSProof(rec.OTSProof, hashBytes)  // internal/ots/verify.go
    result.OTSVerified     = true
    result.OTSBlockHeight  = ver.BlockHeight
    result.OTSConfirmedAt  = ver.Timestamp (chain) or the DB-confirmed time
}
```

Two things are verified, at two trust levels:

**Level 1 — Offline (always on, no network).** `ots.VerifyProof` re-reads the
receipt and:
1. parses the `.ots` file;
2. checks the digest **inside the receipt** equals the artifact's hash — if
   the proof was made for a different file, it fails;
3. replays the whole chain of operations: your fingerprint must lead through
   the merkle path to a real, well-formed **Bitcoin transaction** embedded in
   the proof, with a proper 32-byte merkle root;
4. reports the lowest attested **block height**.

This proves *"the receipt is internally consistent and commits to this exact
hash at block H."* It does **not** yet prove block H really exists — it trusts
what the calendar wrote.

**Level 2 — Against the live chain (when `OTS_ESPLORA_URL` is set).**
`Verifier.Verify` runs the offline check *first*, then for each attested
height fetches the **real block header** and compares the real block's merkle
root to the one the proof computes:

- **match** → actually anchored in Bitcoin; `OTSConfirmedAt` becomes the real
  block-header timestamp (a wall-clock time offline proof can't produce);
- **mismatch** → the calendar's page isn't in the real history (bug or
  forgery) → verification fails, `ots_verified=false`;
- **chain unreachable** → degrades: you still get the offline result, with
  `ots_confirmed_at` falling back to the time the worker recorded.

> **Security gap closed by Level 2:** offline replay proves the proof *claims*
> to be in block H. Only comparing with the real chain proves it *actually* is.

---

## Where is the "permanent hash"?

It's `cv_entries.hash` — the SHA-256 of the artifact's bytes — and nothing
about it changes over time. What OTS adds is the *receipt* beside it
(`cv_entries.ots_proof`) and the worker step that turns it from "a hash Ganji
recorded" into "a hash that is provably, publicly, un-rewritably embedded in
Bitcoin block #NNNNNN".

The round trip:

```
file bytes ──sha256──▶ 64-char hash ──POST /digest──▶ notary queue (pending)
                                                            │ (minutes–days)
                                                            ▼
Block #891686 ◀──mined── daily summary (merkle root) ◀── folding all fingerprints
                                                            │ receipt (.ots)
                                                            ▼
verify endpoint: digest matches? ──▶ offline replay ──▶ [esplora] real header?
                                                            ▼
            OTSVerified=true · OTSBlockHeight=891686 · OTSConfirmedAt=<block time>
```

Even if Ganji shuts down tomorrow, anyone holding the exported receipt can
re-prove the hash's block timestamp with any OTS tool against the public
blockchain.

---

## Configuration

| Env var | Default | Meaning |
|---|---|---|
| `OTS_ESPLORA_URL` | empty (offline only) | The "library": any esplora-compatible API (`https://blockstream.info/api`, your own mempool: `https://mempool.space/api`). Empty = receipts are checked offline only. |
| `OTS_ESPLORA_TIMEOUT_SECONDS` | `10` | Per esplora request timeout. |
| `OTS_UPGRADE_INTERVAL_SECONDS` | `21600` (6 h) | How often the worker asks calendars "mined yet?". Calendars mine on their own schedule, so this only needs to be faster than calendar cadence. |

`backend/.env.example` ships with `OTS_ESPLORA_URL=https://blockstream.info/api`
so a default `cp .env.example .env` setup gets full chain verification. Set it
back to empty to run purely offline.

---

## Trust model — what this proves, and what it does not

**Proves:**

- The artifact existed with *exactly these bytes* at least as early as the
  attested block (the proof was included in block N, so it must have been
  submitted before block N was mined).
- The CV line is cryptographically tied to that file (hash check + proof digest
  check both pass).
- Nobody can rewrite that history — the block is in Bitcoin.

**Does not prove:**

- *Who* uploaded the file. OTS timestamps facts, not identities.
- *When in the day* it was submitted — only "before the block's timestamp".
- Anything, if you skip the chain check. Offline-only verification trusts the
  calendar. A malicious (or buggy) calendar could hand out receipts for blocks
  that never existed — which is exactly why `OTS_ESPLORA_URL` exists.
- That the *storage key* hasn't changed — the hash and proof bind to file
  *content*; if someone swaps the blob behind a key, both the hash comparison
  and the proof digest check fail, and `ots_verified` goes false. That is the
  detection, not the prevention.

---

## Real worked example (from the tests)

`backend/internal/ots/testdata/flatearthers-united.txt` is a real proof from
the OpenTimestamps project:

- the file's SHA-256 digest is committed inside the `.ots` proof (`...ots`);
- the proof is **bitcoin-attested at block 891686**;
- `internal/ots/verify_test.go` replays it offline and, with a fake chain,
  exercises confirm / mismatch / degrade;
- `internal/cv/service_test.go` drives the whole stack through
  `VerifyEntry` and asserts `OTSVerified=true`, `OTSBlockHeight=891686`,
  and `OTSConfirmedAt` preserved from the DB when offline.

That fixture is the shortest real end-to-end proof that this pipeline works.

---

## Recap in one breath

> Approve the deal → Ganji hashes each deliverable (the permanent hash) →
> submits the hash to free public calendars (the notary) → stores the receipt
> (`.ots`) → a background worker checks until Bitcoin mines it (confirmed) →
> the verify endpoint replays the receipt offline, and with
> `OTS_ESPLORA_URL` also compares it against the real blockchain. Done — the
> hash is permanently, publicly anchored in Bitcoin, and every verification is
> checkable by anyone, forever.