# Server-side collection: database choice and scaling

**Status: brainstorming. No decision made, nothing built.** Recorded 2026-08-06 so the measurements do not have to be taken again.

The question asked: if a web application collected LapDog data from around 5,000 users, would SQLite be an appropriate choice — with users selecting how much they share, from everything down to a simple roll-up of hours?

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

## Open question to settle before any of this is built

`position_events` stores opponent names — 40 distinct drivers in the dataset — and the session YAML carries every competitor's customer ID and iRating.

Locally this is correct and the standing rule is to never anonymise other drivers: it is the user's own capture of a session they took part in.

Centralising it is a different posture. It means processing personal data belonging to people who never used the service and never consented, which is a GDPR concern regardless of how public race results are.

The fix is cheap if decided up front — upload opponents as stable per-user pseudonyms or bare ordinals, and keep real names client-side only. Retrofitting it after 5,000 users have uploaded is a migration plus a disclosure.

**This changes the schema, so it should be resolved before designing the upload endpoint.**

## When revisiting

The measurements above hold unless the schema changes materially. What was deliberately not done: no endpoint design, no schema, no projection queries, no auth or transport decisions.

The first three questions to answer, in order:

1. Opponent data — pseudonyms or names? Determines the shared schema.
2. Per-user SQLite files for the detail tier, or all tiers in Postgres?
3. What is the actual expected user weight? Every figure here extrapolates from one *heavy* synthetic user, so 52 GB is an upper bound rather than a forecast.
