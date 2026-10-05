# Garage 61 Permission Request

## Suggested recipient

- Email: [legal@garage61.net](mailto:legal@garage61.net)
- Garage 61 contact page: <https://garage61.net/support-the-project>

The Terms of Service name `legal@garage61.net` for rights-related contact. If
Garage 61 prefers product/API questions elsewhere, ask the recipient to forward
the request or identify the correct contact.

## Relevant Garage 61 pages

- Terms of Service: <https://garage61.net/docs/terms-of-service>
- Privacy Policy: <https://garage61.net/docs/privacy>
- Telemetry privacy controls: <https://garage61.net/docs/usage/privacy>
- Contact page: <https://garage61.net/support-the-project>

## Draft email

**To:** legal@garage61.net  
**Subject:** Permission request for local Garage 61 brake-trace aggregation in LapDog

Hello Garage 61 team,

I maintain LapDog, an open-source Windows tray application for iRacing. LapDog
runs entirely on the user's computer, reads local iRacing telemetry, stores its
data in a local SQLite database, and serves its interface only on the loopback
address. The project is available at <https://github.com/blezek/lapdog>.

I am considering adding a pedal-training feature called Brake-it. Brake-it would
use Garage 61 telemetry to derive practice targets for a selected car and track.
Before incorporating or publishing any Garage 61-derived material, I would like
your express permission and guidance on the permitted API and attribution model.

The proposed process is:

1. A developer runs the generator locally; LapDog would not operate a hosted
   proxy, telemetry service, or shared Garage 61 account.
2. The generator uses a specific, dedicated Garage 61 bearer token supplied for
   this purpose. The token remains on the local machine, is read from the
   `GARAGE61_TOKEN` environment variable, is never embedded in LapDog, and is
   never committed or included in generated output.
3. The generator requests only the laps and telemetry that the token is
   authorized to view, for explicitly selected cars and tracks, and respects any
   rate limits and endpoint restrictions you specify.
4. Processing occurs locally. The generator identifies braking events, aligns
   them by lap position, and combines multiple examples into averaged brake
   traces and aggregate timing/pressure statistics.
5. LapDog would publish only the approved aggregate output: averaged brake
   traces or derived practice-target parameters, car and track labels, aggregate
   percentiles, the number of contributing laps, generation method, and generator
   version. It would not publish raw telemetry, authentication material, driver
   names, driver IDs, driver slugs, or other account details.
6. The resulting Brake-it feature would also run locally inside LapDog. It would
   not send a user's pedal practice or LapDog history to a remote service.

Would Garage 61 grant permission for this use and for distribution of those
aggregate outputs in LapDog's public repository and release binaries?

I would also appreciate guidance on the following points:

- Is there an approved public API or integration flow we should use instead of
  the currently available authenticated endpoints?
- May the generator itself be published as open-source code, provided it contains
  no token or downloaded telemetry?
- Are averaged brake traces and derived timing/pressure parameters considered an
  acceptable aggregate derivative, and are there limits on the number of source
  laps or drivers that may contribute?
- What attribution wording and links would you like displayed in LapDog and its
  documentation?
- What rate limits, caching rules, deletion obligations, or refresh intervals
  should the generator follow?
- Should only the requesting user's own laps be used, or may laps visible to that
  user under Garage 61's sharing controls contribute to an aggregate?
- If a source driver later changes privacy settings or deletes a lap, is there an
  expected process for removing or regenerating an already published aggregate?

Finally, what is Garage 61's policy on exporting links to individual laps for
users who are not authenticated with Garage 61? In particular:

- Is there a stable, public lap URL that an unauthenticated user may open?
- May LapDog publish those links as provenance without publishing the driver's
  name or embedding the telemetry?
- If a lap requires authentication or is visible only through a user's/team's
  permissions, should LapDog omit the link entirely?
- Are lap IDs or analysis URLs themselves considered information that should not
  be redistributed?

I have reviewed the following documents and will follow any more specific terms
you provide:

- <https://garage61.net/docs/terms-of-service>
- <https://garage61.net/docs/privacy>
- <https://garage61.net/docs/usage/privacy>

I will not merge Garage 61-derived data into LapDog or publish it in a release
until the permitted scope is clear. I am happy to share the current generator,
the proposed minimized output schema, and example aggregate records for review.

Thank you,

Dan Blezek  
LapDog  
<https://github.com/blezek/lapdog>

## Before sending

- Confirm that `legal@garage61.net` is the desired recipient or use the current
  contact route on the Garage 61 site.
- Replace “specific, dedicated Garage 61 bearer token” if Garage 61 has already
  issued or named a different credential type.
- Attach or link a redacted example output that contains no driver or lap
  identifiers.
- Do not attach the current generated scenario JSON; it contains driver and
  lap-level provenance.
