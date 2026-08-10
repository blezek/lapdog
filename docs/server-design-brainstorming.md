# Server-side collection: storage, scaling, and preventing spoofed results

**Status: brainstorming. No decision made, nothing built.** Storage measurements recorded 2026-08-06; the anti-spoofing half added 2026-08-10 after a second discussion. Written so neither the measurements nor the research have to be repeated.

Two questions, asked a few days apart.

**Storage.** If a web application collected LapDog data from around 5,000 users, would SQLite be an appropriate choice — with users selecting how much they share, from everything down to a simple roll-up of hours?

**Integrity.** If that data drove public leaderboards, what controls would stop people spoofing results?

---

# Part one: storage and scaling

## Measurements

Taken from `.dataset.db`, the synthetic two-year dataset for one heavy user. These are real figures, not estimates, and they are the reason the conclusion below is about operations rather than performance.

| Measure | Value |
|---|---|
| Database size | 26 MB |
| Sessions | 1,331 |
| Laps | 37,084 |
| Position events | 4,223 |
| Span | 2024-08-12 to 2026-08-04 |

The size breakdown matters more than the total. `sessions` accounts for 17.6 MB of the 26 MB, which is not what 1,331 rows should cost:

| Component | Size | Note |
|---|---|---|
| `classify_source_json` | 15.9 MB | 61% of the whole database, averaging 12,515 bytes per session |
| Everything else | 10.1 MB | sessions, laps, position events, indexes |

`classify_source_json` is the session-YAML subset each classification was derived from. It exists so a wrong classification rule stays fixable retroactively, on the machine that recorded it. It is local provenance and has no reason to be transmitted.

**So the transmissible payload for a heavy user over two years is about 10 MB, not 26 MB.** Median users will be a fraction of that.

Extrapolated to 5,000 users of this weight, at full detail with provenance excluded: **roughly 52 GB**.

Write rate, assuming completed sessions are uploaded in a batch rather than streamed live: 5,000 users at roughly two sessions a day is about 10,000 uploads a day, or about 3 per second at an evening and weekend peak.

## Conclusion on SQLite

Nothing in the above stresses SQLite. In WAL mode it handles thousands of small transactions per second and terabyte-scale files, so 3 writes per second against 52 GB is not close to a limit. "Is SQLite fast enough" is the wrong question.

The reasons to choose something else are operational:

- SQLite is in-process, so exactly one application server may write. That means no horizontal scaling, no rolling deploy without dropping writes, and one machine as a single point of failure.
- Backup and point-in-time recovery require setting up Litestream or equivalent — and rehearsing restores, which is the step that actually gets skipped.
- Schema migrations run against a live single writer.

None of that is fatal at this size; plenty of services run exactly this way. But it is a durability and availability commitment made on day one, on behalf of other people's data.

**Leaning: managed Postgres for the shared store.** At 5,000 users that is roughly $20–50 a month and includes PITR, a read replica and concurrent writers. The engineering time saved over single-server operations exceeds the cost difference. This is an operations judgement, not a claim about database capability.

## Where SQLite would genuinely win

One file per user, for the raw detail tier. This is a real fit rather than a compromise, because the domain is naturally sharded — nothing queries across users' laps except leaderboards and aggregates.

- 5,000 independent writers, so no write contention at all.
- Export is "send the file".
- Deletion is `unlink`, which is the cleanest consent-revocation story of any option considered. Row deletion in any engine leaves data in free pages until a vacuum; removing a file does not.

Turso/libSQL and Cloudflare D1 productise this pattern. A hybrid — per-user SQLite for detail, one Postgres for shared aggregates — is defensible.

## The tier design, which matters more than the storage choice

**Enforce sharing tiers client-side, not server-side.** If a user elects to share hours only, the server must never receive lap times. Filter at upload.

The reason is not efficiency. A tier enforced on the server is a promise about server behaviour: unverifiable by the user, and worthless after a breach. A tier enforced at upload is a property of the system.

LapDog already has the pieces. `store.Filter` selects subsets, the export path already streams exactly what the interface displays through the same predicate, and `uploaded_at` columns exist on all three tables (`internal/store/migrations/0001_init.sql:46`, `:70`, `:90`) with an index — upload was anticipated in the original schema.

A tier ladder following the existing grain:

| Tier | Payload | Per user, 2 years |
|---|---|---|
| Hours | Per-month, per-category totals | ~2 KB |
| Sessions | Session rows; no laps, no provenance | ~4 MB |
| Detail | Laps and position events as well | ~10 MB |

Sparse tiers mean the shared schema has to treat absent and zero as different values — the same distinction the `driver_*` identity columns already make, and for the same reason.

## Open question: third-party data

`position_events` stores opponent names — 40 distinct drivers in the dataset — and the session YAML carries every competitor's customer ID and iRating.

Locally this is correct and the standing rule is to never anonymise other drivers: it is the user's own capture of a session they took part in.

Centralising it is a different posture. It means processing personal data belonging to people who never used the service and never consented, which is a GDPR concern regardless of how public race results are.

The fix is cheap if decided up front — upload opponents as stable per-user pseudonyms or bare ordinals, and keep real names client-side only. Retrofitting it after 5,000 users have uploaded is a migration plus a disclosure.

**This changes the schema, so it should be resolved before designing the upload endpoint.**

---

# Part two: preventing spoofed results

## What the data is for, and therefore what is at stake

The intended use is **public leaderboards and rankings**, across three categories:

| Board | Verifiability |
|---|---|
| Time and volume — hours, laps, sessions | Mostly **unverifiable in principle** |
| Pace — fastest lap per car and track | Verifiable when set in an online session |
| Racecraft and results — wins, positions gained, incidents | Almost entirely verifiable |

That spread is the whole design problem. A single "trusted or not" flag cannot describe a system where wins are checkable against an external record and solo practice hours are checkable against nothing at all.

Consistency and improvement boards were considered and dropped, so they are out of scope. They would inherit whatever trust the underlying laps carry and add no new integrity questions.

## The premise: the client cannot be trusted

LapDog runs on the user's own machine, reads the simulator's shared memory, and writes a local SQLite database. An attacker can edit that database directly, recompile the binary, feed fabricated shared memory, or replay someone else's capture as their own.

So nothing client-side is sound. Not code signing — they can run a modified build. Not an embedded API key — it is extractable. Not obfuscation, and there is no attestation hook to lean on. **Any control that depends on trusting the client is decoration.**

One project-specific aggravation deserves stating plainly: this repository ships `internal/synth`, a deterministic telemetry generator that produces captures structurally indistinguishable from real ones, for two years of fictional driving. It is a ready-made forgery tool, already written and tested, available to anyone who clones the repository. Plausibility checking alone is therefore weaker here than it would be for most projects.

## The options considered

**A. Verify against iRacing's own records.** For any session the iRacing service registered, check the claim against a record the user cannot write. Faking "won at Spa" stops being *spoof your client* and becomes *spoof iRacing*.

**B. Self-contained provenance and plausibility.** Upload the capture rather than the aggregate and recompute server-side; check physics bounds, monotonic clocks, and internal consistency such as laps × lap time ≈ driving time. Raises forgery cost from editing one number to synthesising a coherent telemetry stream — but see `internal/synth` above.

**C. Spot-check the visible.** Accept uploads unverified, retain provenance, and verify only the top of each board plus anything reported. On a leaderboard only the top matters, so this verifies what people actually look at, for almost no API volume. The gap is that someone can park just below the threshold.

## Recommendation

**A and C together, plus one concession that cannot be engineered away.**

Verify online sessions against iRacing — cheap, because most members race far fewer sessions than they practise. Spot-check board leaders and anything reported. And concede the part no control reaches: **solo practice hours cannot be ranked credibly.** Either do not rank them, or rank them in a table visibly and explicitly marked self-reported, separate from the verified boards.

The subtlety worth keeping: even verified sessions do not yield trustworthy *hours*. iRacing reports that a session happened, that a customer was in it, and how many laps they ran. It does not report how long someone sat in the garage. What verification buys for hours is not truth but a **bound** — a twenty-minute race cannot yield three hours of driving time. That bound is cheap and catches the crude inflation.

Two structural rules follow:

- **Verification is a property of a row, not of a user.** Store it per session — verified, unverifiable, or failed — so a board filters on evidence rather than on a reputation score standing in for it.
- **Tiers are enforced at upload, not on the server.** This is already recorded in Part One and applies doubly here: a tier the server enforces is a promise about server behaviour, unverifiable by the user and worthless after a breach.

## iRacing provides both controls, and this is now verified rather than assumed

Researched 2026-08-10 against iRacing's live documentation and live endpoints. This replaced guesswork, and it changed the design: two mechanisms that were sketched as awkward workarounds turned out to exist properly.

iRacing operates an OAuth 2.0 service, **"iRacing.com Auth Service"**, at `oauth.iracing.com`. Confirmed by direct request: `/oauth2/authorize` and `/oauth2/token` both return RFC 6749-conformant errors naming a missing `client_id`, and the `error_uri` leads to a full documentation book at `/oauth2/book/`.

**Identity binding — the Identity Verification Workflow.** Request the `iracing.profile` scope through the Authorization Code flow, trade the code for a token, call `/oauth2/iracing/profile`, and receive `{"iracing_name": ..., "iracing_cust_id": ...}`. No refresh token is issued; the access token is used once and discarded. The documentation names the problem it replaces: *"Previously, clients might have used private messages on the iRacing forums or other means to accomplish this goal."*

This makes `driver_user_id` a **proven** claim rather than an asserted one, and it retires the workaround considered before the research: issuing a nonce the user had to set as their car number, then confirming it through the results API. That is no longer necessary.

**The verification oracle — the Data API Workflow.** A client may query `/data` on behalf of an authenticated member using the `iracing.auth` scope, without ever handling a password. Third-party clients register with the audience `data-server`.

iRacing also anticipates the service-account split directly, through **client roles**: *End-User Support* for member logins and identity verification, and *Internal Support* for data "available to any authenticated user; schedules, official session results, and the like". The documentation recommends separating the two and will issue a second `client_id` for it. That is exactly the split option A needs.

Relevant flow constraints:

- **Authorization Code flow** is preferred, and mandatory for any client whose code is distributed to end users, that cannot keep a secret, or that serves a broad user base. LapDog is all three, so this is the flow.
- **Password Limited flow** — an in-house revival of the deprecated Resource Owner Password Credentials grant — is capped at fewer than three users, all pre-registered by contacting iRacing, and bypasses 2FA. Not usable for a public service.

## The blocker

Client ID creation is **paused**, and this was current on the live page as of 2026-08-10:

> "We have paused the creation of OAuth client IDs while we evaluate existing 3rd party usage of iRacing's APIs and SDK. When creation of OAuth client IDs is enabled again, we will post on our forums and include a mention in the release notes."

There is no committed reopening date; the signal would be the forums and the release notes, whose only entry is `2025-09-05`. When registration reopens, processing takes up to ten days.

**Consequence for sequencing:** the design can be specified and built against the documented API now, but cannot ship until registration reopens. That is a scheduling dependency on a third party with no date attached.

## What the terms actually say

Quoted, because it bears on how much weight the design may place on the oracle:

> "iRacing does not provide explicit authorization to 3rd parties who develop software that works alongside the simulation or website(s)."

> "iRacing may modify these documents and their terms at any time and may restrict or remove access to various data at its discretion and without notice."

Usage is governed by the Terms of Use and EULA, Conditions of Use, and Privacy Policy that members accept at sign-up.

So access is sanctioned in mechanism and revocable without warning. **The design must degrade rather than break if the oracle disappears** — which is an independent argument for the verified-versus-self-reported tiering above, over any design that presumes the API is always there.

## Still undecided

The direction question was put and not answered, so it stays open:

1. **A + C with hours labelled self-reported** — the recommendation above. Strongest honest position; accepts a third-party dependency.
2. **Rank only what is verifiable** — drop the hours and practice-pace boards. Smallest attack surface, no awkward labelling, but abandons the metric closest to LapDog's purpose.
3. **Self-contained for now** — no external dependency, weaker guarantees stated plainly, `internal/synth` as a known hole.

Answering it is cheaper now than before the research, since option A is confirmed to exist and its cost is known.

## Sources

Fetched live on 2026-08-10:

- [iRacing.com Auth Service documentation](https://oauth.iracing.com/oauth2/book/index.html)
- [The same book as a single page](https://oauth.iracing.com/oauth2/book/print.html) — Client Registration, Client Roles, Identity Verification Workflow, Data API Workflow, Scopes, Password Limited Flow, Release Notes
- Live responses from `https://oauth.iracing.com/oauth2/authorize` and `https://oauth.iracing.com/oauth2/token`

---

## When revisiting

The measurements hold unless the schema changes materially, and the iRacing API shape is documented well enough to code against. What was deliberately not done: no endpoint design, no schema, no projection queries, no transport decisions, and no implementation of any kind.

Questions to answer, in the order that unblocks the most:

1. **Which integrity direction** — the three options under "Still undecided". Determines whether the iRacing dependency exists at all, and therefore most of the rest.
2. **Opponent data — pseudonyms or names?** Determines the shared schema. Unchanged from 2026-08-06 and still first among the schema questions.
3. **Has iRacing reopened OAuth client registration?** Check the forums and the release notes. Gates shipping, not designing.
4. **Per-user SQLite files for the detail tier, or all tiers in Postgres?**
5. **What is the actual expected user weight?** Every storage figure extrapolates from one *heavy* synthetic user, so 52 GB is an upper bound rather than a forecast.
