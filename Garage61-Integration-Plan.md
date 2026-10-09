# Garage61 OAuth and Verification Integration Plan

**Status:** design only; nothing in this document is implemented.  Research and
API review performed 2026-09-23.  Registration correspondence updated
2026-10-08; final callback registration and public-client behavior remain
unconfirmed.

## Goal

Use Garage61 as an independent identity and driving-data provider for two
features:

1. Match laps recorded locally by LapDog to the same laps in Garage61, so a
   future analysis feature can use Garage61 metadata or telemetry that the user
   is allowed to see.
2. Let a user publish their own LapDog totals and laps to a global leaderboard,
   initially at `https://lapdog.blezek.com`, while preventing one user from
   submitting data under another user's identity.

OAuth proves which Garage61 account authorized LapDog.  It does **not** make the
local LapDog database trustworthy.  The hosted service must independently read
Garage61 data before it labels a submitted value verified.

## What the Garage61 API establishes

The current [authentication documentation](https://garage61.net/developer/authentication)
specifies OAuth 2 Authorization Code Grant, recommends PKCE, issues access and
refresh tokens, and requires replacement of a refresh token when rotation
returns a new one.  Garage61 explicitly says not to ask users for their Garage61
username or password and recommends `golang.org/x/oauth2` for Go clients.

The published endpoints are:

- authorization: `https://garage61.net/app/account/oauth`
- token exchange and refresh: `https://garage61.net/api/oauth/token`
- optional OIDC user info: `https://garage61.net/api/oauth/userinfo`
- REST API base: `https://garage61.net/api/v1`
- machine-readable contract: [OpenAPI v1](https://garage61.net/api/openapi/v1.json)

The API currently provides the following useful evidence:

- `GET /me` returns the stable Garage61 user ID, mutable profile slug and the
  permissions granted to the token.
- `GET /me/accounts` returns linked simulator accounts.  An iRacing account has
  `platform: "iracing"` and an external `id`; this is the value to compare with
  LapDog's nullable `sessions.driver_user_id`.  Confirm with Garage61 that this
  external ID is the iRacing customer ID before relying on it in production.
- `GET /me/statistics` returns daily aggregates by user, car, track and session
  type: event count, `timeOnTrack`, laps and clean laps.  It needs no documented
  optional OAuth scope.
- `GET /laps`, `GET /laps/{id}` and `GET /laps/{id}/csv` require the
  `driving_data` scope.  Lap records include driver, event, session/run/lap
  numbers, car and track platform IDs, start time, lap time, clean/incomplete/
  off-track/pit/discontinuity flags and telemetry visibility.

Garage61's [lap endpoint](https://garage61.net/developer/endpoints/v1/findLaps)
states that results honor the authorizing user's privacy view.  By default an
application is limited to the user and teammates, and global visible-data search
needs separate approval.  LapDog should always request `drivers=me`; teammate
visibility must never be used as proof of the current user's driving.

The API is read-only for the proposed data.  LapDog uploads claims to the LapDog
service, not to Garage61.  The service verifies those claims by querying
Garage61 with the submitting user's authorization.

## Recommended architecture

The design recommends **two Garage61 OAuth applications** and one LapDog
account/device binding; Garage61 has not yet confirmed that arrangement.
This keeps the installed open-source program a public client, keeps the website
secret on the server, and avoids copying either side's refresh token to the
other side.

### Repository and deployment boundary

Build the hosted application in a **new repository**, separate from this LapDog
repository.  The two systems have different trust and deployment boundaries:
LapDog is a local, single-user Windows executable with an embedded interface and
SQLite database, while the hosted service accepts untrusted Internet traffic,
holds confidential OAuth credentials, stores multiple users' data and requires
independent deployment, monitoring, retention and incident response.  Keeping
them together would also make it easier to accidentally include server secrets
or server-only behavior in the distributed client.

This repository continues to own everything shipped in `lapdog.exe`:

- the desktop Garage61 OAuth flow and Windows credential storage;
- local Garage61 API access, lap matching and cached metadata;
- the device-pairing and upload clients; and
- the existing embedded React interface.

The new hosted-service repository owns:

- the public website and confidential Garage61 OAuth flow;
- accounts, browser sessions and paired-device management;
- the upload API, verification queue/workers and leaderboards; and
- hosted database migrations, operational configuration and public policy
  pages.

Start the hosted system as one repository and one coherently deployable
application, with internal boundaries between its web interface, API and
verification worker.  Do not split those parts into separate repositories
until their deployment or ownership needs actually diverge.

The repository boundary is a versioned network contract, not shared database
models or source packages.  Define a canonical OpenAPI specification (or an
equivalently precise schema) in the hosted-service repository, version the
upload and pairing endpoints, and keep a generated client or checked-in schema
snapshot here.  Both repositories must run contract tests against the same
sanitized fixtures.  Protocol changes must remain backward compatible for a
documented desktop support window because installed clients will not all update
at once.

| Component | OAuth client | Redirect | Token owner | Purpose |
|---|---|---|---|---|
| Windows LapDog | public/localhost client, no client secret | loopback HTTP | Windows credential store | local lap matching and future local analysis |
| Hosted LapDog service | confidential web client | HTTPS website callback | encrypted server-side secret store | website login, identity binding and independent leaderboard verification |

Garage61's current application UI describes a client as public when all its
registered redirect hosts are loopback (`localhost` or `127.0.0.1`) and says no
client secret is required for such an application.  A client secret embedded in
LapDog source or its executable would not be a secret.  Do not combine the
loopback and public-website redirects into one application unless Garage61
explicitly confirms that doing so preserves the public-client/no-secret model.

The stable identity chain is:

```text
Garage61 /me.id
  -> Garage61 /me/accounts[platform=iracing].id
  -> LapDog account's verified iRacing customer ID
  -> uploaded sessions where sessions.driver_user_id has the same value
```

Store the Garage61 ID as the provider subject.  Store the Garage61 slug and
display name only as changeable presentation data.  Never use a name or slug as
an authorization key.

### Desktop OAuth flow

1. The local Go server generates a high-entropy `state`, PKCE verifier and S256
   challenge.  Keep them only for a short-lived pending attempt, with an expiry
   and one-use flag.
2. It opens the user's normal browser at the Garage61 authorization endpoint,
   requesting the minimum scope (`driving_data`) and the exact loopback redirect.
3. Garage61 redirects to LapDog's loopback callback.  The handler accepts only
   loopback requests, requires the exact `state`, rejects replays and exchanges
   the code with the verifier.  Do not put a permissive CORS header on this
   endpoint.
4. Before persisting credentials, call `/me` and `/me/accounts`, require the
   expected permission and require exactly one selected/confirmed linked
   iRacing account.  Compare it with all non-null local `driver_user_id` values;
   a mismatch is displayed and blocks publication, but need not block private
   Garage61 analysis if the user deliberately has multiple local drivers.
5. Store access and refresh tokens in Windows Credential Manager.  SQLite keeps
   only the Garage61 user ID/slug, selected iRacing customer ID, granted scopes,
   expiry, credential reference, last successful sync and status.  Never expose
   tokens to React, logs, diagnostics, captures or exports.
6. Refresh shortly before expiry or once after a 401.  Serialize refreshes,
   atomically replace a rotated refresh token, and stop retrying on revocation.
   Disconnect deletes credentials and nonessential cached Garage61 data.

Do not use a fixed custom URI scheme.  A loopback callback is bound to this
running process and PKCE protects an intercepted code.  Bind `127.0.0.1`, not all
interfaces.  The requested desktop/CLI callback is
`http://127.0.0.1:47047/oauth/callback`.  Port 47047 is acceptable whether
Garage61 matches it exactly or permits arbitrary loopback ports.  If exact
matching is required, keep the callback available on 47047 even when the
interface uses a different configured port; the listener arrangement remains
an implementation decision.  Do not assume ephemeral ports are supported
until Garage61 answers.

### Website OAuth flow

1. `/login/garage61` creates a short-lived server-side transaction containing a
   random `state`, PKCE verifier, intended return path and browser-session hash.
   Set a Secure, HttpOnly, SameSite=Lax correlation cookie.
2. Redirect to Garage61 with the web client ID, exact HTTPS callback,
   `state`, S256 challenge and only `driving_data`.
3. The callback verifies the cookie and state before exchanging the code from
   the server.  It then calls `/me` and `/me/accounts`; upsert the account by
   `(provider='garage61', provider_subject=/me.id)`, not by email or display
   name.  Reject attempts to attach a Garage61 subject or iRacing ID already
   bound to another LapDog account, and require an explicit merge/recovery flow.
4. Store the web refresh token encrypted with a key kept outside the database.
   Hash normal LapDog session tokens; use secure cookies and CSRF protection for
   state-changing browser requests.  Never give a Garage61 token to browser
   JavaScript.
5. A disconnect revokes the local grant if Garage61 exposes a supported
   revocation operation; otherwise delete tokens and record the limitation.
   Published data needs a separate, explicit delete/unpublish choice.

OAuth should be the website's login and re-verification mechanism, not merely a
button that copies a claimed Garage61 ID into a user profile.

### Pairing the installed application with the website

Garage61 OAuth identifies the person; it does not authenticate arbitrary uploads
from their PC to LapDog's service.  Add an explicit device-pairing step:

1. The desktop creates a per-installation signing key in Windows Credential
   Manager and asks the server for a short-lived, single-use pairing transaction.
2. It opens an HTTPS URL on `lapdog.blezek.com`.  The user completes the website
   Garage61 flow if necessary and sees the Garage61/iRacing identity that will be
   linked.
3. After explicit approval, the server binds the desktop public key to that
   authenticated LapDog account.  The desktop receives only a scoped device ID
   and upload credential, never the website Garage61 refresh token.
4. Every upload is sent over HTTPS, signed or authenticated by that device, and
   assigned to the account derived from the credential.  Ignore any account,
   Garage61 user or iRacing customer ID supplied in the body for authorization.
5. The website lists paired devices with last use and supports revocation.

This stops user A from naming user B in an upload.  It does not stop user A from
fabricating data for user A, which is why leaderboard verification remains a
server-side Garage61 comparison.

## Local lap matching

Match conservatively and preserve ambiguity.  Do not attach the nearest lap and
call it correct.

1. Fetch only `drivers=me`, paged and bounded by the local session/lap time
   window.  Cache Garage61 car and track catalogs and map using `platform` plus
   `platform_id`; do not join on display names.
2. Candidate keys, strongest first, are: authenticated driver ID; Garage61
   iRacing car and track platform IDs; event/session/run/lap number where an
   exact LapDog equivalent exists; UTC start-time proximity; lap time within a
   measured tolerance; and compatible pit/clean flags.
3. Store the Garage61 lap ID only for a unique candidate above an explicit
   confidence threshold.  Store match method, score/version and matched time so
   a changed algorithm can re-evaluate it.  Zero candidates is `unmatched` and
   multiple plausible candidates is `ambiguous`, never an arbitrary match.
4. A Garage61 event ID must not be assumed to equal iRacing `subsession_id`
   without written confirmation.  Prototype with real paired data to determine
   whether event/session/lap fields provide a deterministic join and to measure
   clock and lap-time tolerances.
5. Download CSV only on demand, only when `canViewTelemetry` is true, and keep it
   local by default.  Respect Garage61 privacy changes and deletion.  A stored
   foreign lap ID is a reference, not permission to republish telemetry.
6. Handle `429` using the returned retry delay, add jitter, and coalesce
   concurrent requests.  Cache immutable lap detail and catalog lookups, but
   periodically re-check visibility before analysis that exposes Garage61 data.

Proposed local tables should be additive rather than changing the meaning of
existing `sessions` or `laps`: `garage61_connection`, `garage61_lap_match`, and
optionally a versioned catalog map.  Nullable values must remain nullable.

## Leaderboard ingestion and verification

### Upload contract

Upload a minimized, versioned claim set.  Exclude `classify_source_json`, capture
files, opponent names/IDs and raw telemetry.  At minimum a session claim needs
the local UUID, authenticated driver's iRacing customer ID, UTC bounds, category,
car/track iRacing IDs, and the counters the selected sharing tier permits.  A
lap claim needs its UUID/session UUID, lap number, UTC time if known, lap time,
and relevant clean/pit/incident flags.

Make uploads idempotent on `(account_id, local_uuid)` and use revision numbers or
content hashes.  Server ownership always comes from the device credential.  A
session with a null driver identity, an offline placeholder, or a driver ID that
does not equal the account's verified iRacing ID cannot enter a verified board.

### Evidence levels

Verification is a property of each submitted row/aggregate, not of the user:

| Level | Server evidence | What may be claimed |
|---|---|---|
| `garage61_lap` | unique own-driver `/laps` match | that lap, its time and Garage61 quality flags |
| `garage61_daily_total` | own `/me/statistics` bucket | up to Garage61's laps, clean laps and time-on-track for that day/car/track/session type |
| `identity_only` | `/me` plus linked iRacing account | who submitted it, not that the driving value is true |
| `failed` / `ambiguous` | conflict or no unique evidence | no verified claim |

Garage61 `timeOnTrack` is the closest external check for LapDog
`driving_seconds`; first verify its semantics and tolerances with real sessions.
It cannot validate LapDog `connected_seconds` or `in_car_seconds`, and the site
must not label those counters Garage61-verified.  For daily totals, accept no
more than the independently returned bucket; do not allocate one Garage61 total
across several competing uploads without deterministic rules.

The verifier runs from the hosted service with that account's web OAuth grant,
never trusting matches asserted by the desktop.  Record provider response
identifiers, check time, verifier version and a minimized evidence snapshot.
Reverify board leaders and recent edits, and downgrade rather than silently keep
the badge when authorization is revoked or source data disappears.  Decide and
publish whether historical verification remains as an audit fact after a user
disconnects.

### Abuse controls

- Require current Garage61 authorization for first publication and periodically
  for continued verified status.
- Enforce one Garage61 subject and one iRacing account binding per LapDog account;
  make identity changes slow, visible and audited.
- Rate-limit login, pairing, upload and verification per account/device/IP.
- Put uploads through a queue with size/count/date limits; reject future dates,
  duplicate Garage61 lap IDs for the same board and structurally impossible
  values before consuming Garage61 quota.
- Keep unverified and verified boards visibly separate.  Never convert a
  plausible client claim into a verified value.
- Provide account export, device revocation, leaderboard unpublish and account
  deletion.  Define retention for tokens, evidence and audit records before
  launch.

## Callback registration correspondence (2026-10-08)

The original April 1 application request named Lapdog, requested OAuth2 with
`driving_data`, and described comparing Garage61 laps for consistency in braking,
turn-in, apex and speed.  On June 28, Simon rejected `lapdog://callback` as an
invalid callback and requested a redirect URL.

On October 6, Daniel proposed the hosts `127.0.0.1:47047`,
`lapdog.blezek.com` and `brake-it.blezek.com`, with the deployment direction
still undecided.  On October 8, Simon said the callback domains looked okay
and asked for the scheme and full URI for each.  That response does not confirm
registration of the complete URLs or approval of a particular client model.

The October 8 reply supplies these exact redirect URIs, superseding the earlier
callback paths and development-port suggestions in this plan:

| Intended use | Requested redirect URI |
|---|---|
| Local desktop / CLI | `http://127.0.0.1:47047/oauth/callback` |
| Hosted LapDog | `https://lapdog.blezek.com/oauth/callback` |
| Hosted Brake It | `https://brake-it.blezek.com/oauth/callback` |

The reply also asks two questions that remain unanswered in the supplied
correspondence:

1. Can the locally installed app authenticate as a public client using PKCE
   only, without a client secret?  A user's machine cannot safely hold an
   embedded application secret.
2. For the `127.0.0.1` redirect, is port 47047 matched exactly, or is any port
   allowed in the RFC 8252 style?  Port 47047 is acceptable either way.

These are requested callbacks, not confirmed registrations.  The correspondence
does not establish whether Garage61 will assign them to one application or
separate public and confidential clients.  Retain the two-client recommendation
above pending confirmation; do not infer that combining HTTP loopback and HTTPS
website redirects preserves secret-free desktop authentication.  The Brake It
URL is a hosted option, not a decision to migrate the LapDog website.

Use the requested loopback callback during local development unless another URI
is explicitly registered.  Do not assume a Vite callback on port 5173 or a
hosted-service callback on port 8080 is approved.  Any future production domain
needs its exact `/oauth/callback` URI registered before cutover.  During a domain
migration, test login and existing refresh grants before removing the old
callback.

Also tell Garage61 the non-callback URLs they may need for application review:

- homepage: `https://lapdog.blezek.com/`
- privacy policy: `https://lapdog.blezek.com/privacy`
- terms: `https://lapdog.blezek.com/terms`
- account connections/revocation help: `https://lapdog.blezek.com/settings/connections`

These pages must exist before production approval; they are not OAuth redirect
targets.

## Questions for Garage61 before implementation

1. Pending from the October 8 reply: can the desktop client use PKCE only,
   without a client secret?  Also confirm the proposed split into public
   loopback and confidential hosted clients, including the Brake It callback,
   and whether PKCE S256 is supported and required for both client types.
2. Pending from the October 8 reply: is the loopback port matched exactly or
   may it vary (RFC 8252 style)?  Port 47047 is acceptable either way.  Confirm
   registration of all three exact URIs above, including HTTP for local
   desktop/CLI use.
3. What token endpoint authentication method is required for the confidential
   client, and what exact parameters are required for authorization, exchange,
   refresh and revocation?  Is there a revocation endpoint?
4. Does `/me/accounts` return the canonical iRacing customer ID in `id`, as a
   decimal string?  Can one Garage61 user link multiple iRacing accounts, and
   can a single iRacing account move between Garage61 users?
5. Does `drivers=me` guarantee that every returned `/laps` row was driven by the
   authenticated user?  Can the service query laps by event ID or iRacing
   subsession ID, and is a Garage61 event ID related to that subsession ID?
6. Is `/me/statistics` authorized for ordinary OAuth clients without an extra
   scope?  Precisely how is `timeOnTrack` defined, what timezone defines `day`,
   how quickly does it settle, and are historical values corrected later?
7. Is use of `/me/statistics` and `/laps` as evidence for a public leaderboard
   permitted?  May LapDog store Garage61 lap IDs and minimized verification
   snapshots, and for how long after revocation/deletion?
8. What per-user/application rate limits, caching rules, attribution, privacy-
   change handling and deletion obligations apply?  Does `429` always include
   `details.retryAfterSeconds`?
9. Does `driving_data` require application approval and separate user consent?
   Can the hosted client receive only general identity/statistics permission if
   individual lap verification is disabled?
10. Are webhooks available for authorization revocation, account unlinking, lap
    deletion or completed data processing, and how are webhook signatures
    verified?

Do not implement around guessed answers to questions 1--7.

## Delivery phases and mechanical verification

### Phase 0: provider agreement and real-data spike

- Obtain written answers and application registrations.
- With a developer account, capture sanitized `/me`, `/me/accounts`,
  `/me/statistics` and `/laps` responses for the same real session as LapDog.
- Establish deterministic ID mappings and measured time/lap tolerances.
- Check whether delayed Garage61 processing changes match results.
- Write down permitted storage, display, attribution and deletion behavior.

Exit criterion: one real online session and its laps can be joined without names
or undocumented ID equivalence, and the time statistic's meaning is known.

### Phase 1: local connection and matching

- Implement this phase in the existing LapDog repository.
- Add the desktop public-client flow, credential storage, status/disconnect UI,
  API client, rate-limit handling and additive match tables.
- Add conservative matching and an inspectable matched/ambiguous/unmatched UI.
- Keep all analysis and downloaded telemetry local.

Required negative tests include wrong/replayed/expired state, callback without a
pending attempt, intercepted code without verifier, driver mismatch, rotated
refresh token, revoked grant, concurrent refresh, 429, ambiguous laps and
privacy-denied CSV.  For every new assertion, delete or invert the production
check and confirm its test fails before restoring it.

### Phase 2: hosted identity and device pairing

- Create the hosted-service repository and implement the website, server-side
  OAuth and persistence there; keep the desktop pairing client in this
  repository.
- Establish the versioned pairing/upload contract and cross-repository contract
  fixtures before either side depends on it.
- Build the confidential web flow, account model, encrypted credential store,
  session/CSRF controls, recovery rules and device pairing/revocation.
- Threat-model login CSRF, account pre-hijacking, OAuth subject collision,
  pairing-code theft, token leakage and replayed uploads.
- Commission HTTPS, privacy/terms/connections pages, secret rotation, backups,
  audit retention and alerting before accepting other users.

Exit criterion: two test users cannot bind each other's Garage61 identity or
upload into each other's account, including under concurrent/replayed requests.

### Phase 3: upload and independent verification

- Add the desktop upload client here and idempotent ingestion in the hosted
  repository, both conforming to the minimized/versioned upload contract.
- Implement server-side statistics and lap verification with explicit evidence
  states; publish no badge until verification succeeds.
- Reconcile edits/deletes, quota failures and revoked provider access.
- Launch privately, compare a sample of real Garage61 and LapDog values, then
  publish only metrics whose labels match what Garage61 actually establishes.

### Phase 4: domain migration

- Register the new exact hosted callback and deploy it before changing links.
- Support both domains during a bounded migration, test new login and refresh,
  update provider/application/privacy URLs, then retire the old callback.
- The desktop loopback client should remain unchanged by a website domain move;
  only its pairing start URL needs a signed/configured service-origin update.

## Explicit non-goals for the first release

- No Garage61 password collection, personal-access-token UI or shared service
  account.
- No claim that OAuth alone validates a lap or time total.
- No verified connected-time or in-car-time leaderboard.
- No teammate/global lap ingestion, raw Garage61 telemetry publication or
  opponent data upload.
- No client secret in the desktop binary, React bundle, repository, SQLite,
  logs, diagnostics or CI.
- No automatic merge based on matching name, email, slug or unverified body ID.

## Related LapDog design constraints

This plan extends rather than replaces
[`docs/server-design-brainstorming.md`](docs/server-design-brainstorming.md).
That document's central conclusion still holds: the local open-source client can
be modified, its SQLite database can be edited and synthetic telemetry already
exists in this repository.  Garage61 provides a valuable independent oracle for
the data it recorded, while claims outside that oracle must remain visibly
unverified.
