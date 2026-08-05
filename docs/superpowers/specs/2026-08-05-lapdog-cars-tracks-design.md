# Cars and Tracks: per-entity review pages

**Status:** approved 2026-08-05. Supersedes nothing; extends the interface described in `2026-08-04-lapdog-design.md`.

## 1. What this adds

Two new pages, `Cars` and `Tracks`, each answering "how do I drive *this one thing*, and am I getting better at it?" The dashboard answers that question across everything at once; these answer it per car and per track, in much more detail.

Nothing in the collector, the capture format or the database schema changes. Every metric here is computable from what is already recorded, which is why this is a single plan with no migration.

## 2. Where the metric set comes from

The metric set is drawn from what existing sim-racing tools actually display, researched and adversarially verified on 2026-08-05. 99 candidate claims were extracted from 21 sources, 25 verified, 14 confirmed and 10 refuted. The verification synthesis step failed on API capacity, so the claims below are cited individually rather than as a merged report.

Three findings shaped the design more than the rest.

**The closest existing analogue ships no sector data at all.** iRacing Insights has dedicated per-car and per-track statistics tables, and displays no sector times, no optimal lap and no lap-time standard deviation; its lap metrics are best lap, average lap, a consistency percentage, the absolute delta between average and best, and counts of total, clean and incident laps ([iracinginsights.app](https://iracinginsights.app/), 3-0). This matters because it removes what looked like the blocking limitation. LapDog cannot compute sector times — see §8 — and it turns out the direct precedent does not use them either.

**Consistency has a published, reproducible definition.** Simresults defines it as the best lap minus the mean of the non-best laps, reported both as a percentage and an absolute delta, excluding pit laps, the first lap and outlier laps ([simresults.net](https://simresults.net/), 3-0). iRacing Insights colour-codes a consistency percentage green at ≥98% and yellow at ≥95%, which implies the same percentage convention. tracc.eu instead uses plain standard deviation of lap times ([tracc.eu](https://tracc.eu/acc-race-visualiser/), 3-0). The percentage form was chosen so the number means the same thing here as in the tools a driver already uses.

**Incidents are severity-weighted points normalised by exposure.** iRacing weights incidents 1×, 2× and 4×, and counts only the highest when several occur in quick succession ([iRacing support](https://support.iracing.com/support/solutions/articles/31000156960-iracing-how-to-safety-rating), 3-0). Safety Rating normalises them by *corners driven* — a per-track corner multiplier times laps completed — not by races or laps (same source, 3-0). Two consequences follow in §5.

### 2.1 What the research says LapDog cannot be

Garage 61 and Virtual Racing School both centre on comparison against a *faster reference driver*: Garage 61 treats the delta graph against a reference lap, plotted along track distance, as the first thing to look at, and deliberately benchmarks against a driver about one second faster rather than the fastest, because the alien's technique is harder to copy ([garage61.net](https://garage61.net/whatsnew), 3-0 and 3-0). VRS supplies comparison telemetry through coach data packs and teammate data ([virtualracingschool.com](https://virtualracingschool.com/faq/), 3-0).

LapDog has no other driver's telemetry and no distance-based traces, so that entire category is out of reach. **Its analogue is comparison against your own past self**: personal best, best-of-range, and month-by-month progression. This is stated here so the absence reads as a decision rather than an oversight.

### 2.2 A caution on the refuted claims

Ten claims were refuted, including Race Companion's five-axis spider chart and its named widget set. Refuted here means the verifiers judged the *cited source did not support the claim* — not that the underlying metric is worthless. Qualifying-versus-race pace delta appeared only inside a refuted claim, yet it is computable and plainly useful, so it is included in §5 on its own merits with no citation attached.

## 3. Navigation

```
Dashboard · Cars · Tracks · Sessions · Laps · Export        [Settings pinned bottom]
```

Cars and Tracks sit beside Dashboard because all three are aggregate views. Sessions and Laps are record-level and stay together after them.

Cars uses the vendored `car-sports` icon. **Tracks has no suitable icon in the vendored set** — the 25 Material Design Icons currently embedded contain no road or map glyph. One SVG must be added: `road-variant.svg` into `internal/ui/icons/mdi/`, a `RoadVariant` constant in `icons.go`, and an entry in `All`. The existing Pictogrammers licence covers it, and `internal/ui/icons` already has a test asserting every name in `All` resolves, so a forgotten file fails the build rather than rendering a blank space.

## 4. Layout and routing

One route per page, `/cars` and `/tracks`, each a three-part split pane matching the vocabulary `Sessions` established:

```
┌──────────────┬────────────────────────────────────────────┐
│ CARS         │  Porsche 911 GT3 R · GT3                   │
│──────────────│  492.5 h · 473 sessions · 14,536 laps      │
│▸Porsche 911  │ ┌────────┬────────┬────────┬────────┐      │
│  492h ·14.5k │ │ 90.9%  │  2.81  │  +7.63 │   52   │      │
│              │ │ clean  │pts/100k│ gained │  wins  │      │
│ Dallara F3   │ └────────┴────────┴────────┴────────┘      │
│  378h ·13.3k │                                            │
│              │  PACE BY TRACK          (table)            │
│ BMW M4 GT4   │  PROGRESSION            (line)             │
│  179h · 5.1k │  TIME BY CATEGORY       (stacked bar)      │
│              │  RACECRAFT              (stats)            │
│ Mazda MX-5   │  RIVALS                 (table)            │
│  165h · 3.5k │                                            │
└──────────────┴────────────────────────────────────────────┘
```

The left column ranks by driving time within the active range. The selection lives in the URL so a car page is linkable and survives a reload, and with no selection the first entry is shown, as `Sessions` already does.

**Correction, 2026-08-05.** This section originally specified `/cars?car=173`, "matching the existing filter-in-URL pattern in `useFilter`". Matching that pattern is exactly what broke it: `useFilter` already reads `car` and `track` as *hard filters*, so storing the selection under the same key made every click apply a filter to the list being chosen from. The left column collapsed to the single selected row on the first click — measured directly, `/api/entities?by=car&range=all` returns nine or more cars while `/api/entities?by=car&car_id=105&range=all` returns one — and the only recovery also discarded the range and session filters.

The selection therefore uses a key outside `useFilter`'s vocabulary. Hiding the dropdown (§4.1) prevents a *second visible control* for the same dimension; it does nothing about a parameter name collision, and the two concerns must not be conflated. The selection is also validated against the ids actually present in the current list rather than trusted, so a hand-edited value or one carried across from the other page falls back to the first entry instead of reaching the API and failing.

### 4.1 Filter semantics

The pages honour the global filter bar, so "last 90 days" answers how you are driving *lately*. Two consequences are deliberate:

- **The car dropdown is hidden on `/cars`, and the track dropdown on `/tracks`.** The page's own dimension is set by the left-column selection; offering a second control for the same thing invites the two to disagree. The *other* dimension stays available, so "this car, at this track" is expressible.
- **Personal bests are computed unfiltered** and shown beside the in-range best. A PB is a fact about your whole history, and the comparison "PB 1:01.395, best in range 1:01.9" is what tells a driver they are off form. Every other number on the page reflects the range.

## 5. Metrics

### 5.1 Headline stats

Only quantities comparable across tracks appear here. Lap times do not, for the reason in §5.2.

| Stat | Definition |
|---|---|
| Driving time | `SUM(driving_seconds)`, excluding garage, pit box and replay |
| Sessions, laps | `COUNT(*)`, `SUM(laps_completed)` |
| Distance | `SUM(laps_completed × track_length_km)` |
| Clean laps | Timed non-pit laps with `incidents_on_lap = 0`, as a percentage |
| Incident points per 100 km | `100 × SUM(incidents) / distance_km` |
| Positions gained | `AVG(starting_position − finish_position)`, races with both recorded |
| Wins, podiums | `finish_position = 1`, `finish_position ≤ 3` |

**Incidents are labelled "incident points" everywhere in the interface, not "incidents".** The sim's counter is severity-weighted, so a number like 5,482 is points and not a count of events; calling it a count overstates how often something went wrong.

**Per 100 km rather than per hour.** Safety Rating normalises by corners driven, which LapDog cannot reproduce without per-track corner counts. Distance is the closest available proxy for exposure and is far better than time, which punishes a driver for a long practice stint at a slow track. The existing dashboard figure of incidents per hour is left alone; this is a per-entity view where exposure differs wildly between entries.

### 5.2 Pace by track

A car's best lap is meaningless as a single number — a lap at Lime Rock and a lap at Monza are not the same quantity. The car page therefore has no headline lap time. Instead, a table with one row per track, ordered by laps:

Values below are measured from the development database for the Porsche 911 GT3 R, not invented, so the shape of real output is on record:

| Track | PB (all time) | Consistency | Laps | Sessions |
|---|---|---|---|---|
| Lime Rock Park | 1:01.395 | 99.10% | 1,428 | 17 |
| Brands Hatch Circuit | 1:27.195 | 99.13% | 1,310 | 26 |
| Virginia International Raceway | 2:02.713 | 99.14% | 1,193 | 29 |

The in-range best and its delta to PB are additional columns, omitted from this illustration because with the range set to all time they are equal to the PB by definition.

The track page mirrors this exactly, with one row per car. This symmetry is what allows one implementation to serve both pages.

### 5.3 Consistency

```
per session:  consistency % = 100 × session_best / mean(session's other laps)
              consistency Δ = mean(session's other laps) − session_best
reported:     the mean of those per-session values across the range
```

**Consistency is computed within a session and then averaged, not computed across all of a car's laps at once.** This is not a detail. Simresults measures a single race session, where the laps being compared share fuel load, tyre state, track conditions and the driver's skill on that day. Applying the same formula to every lap a car has ever done at a track measures something else entirely — it conflates repeatability with two years of improvement and varying conditions.

The difference is large enough to invert the verdict. Measured on the development database for one car at three tracks, the across-all-laps form gives 96.5% to 97.7%, while the per-session form gives 99.1% to 99.2%. Under the ≥98% threshold the first form labels a demonstrably consistent driver as merely fair, and the borrowed thresholds become meaningless. The per-session figure also proved stable: across 31 sessions at one track it ranged only from 98.92% to 99.53%.

Sessions with fewer than 5 qualifying laps are excluded from the average entirely rather than being averaged in with a noisy value. If no session qualifies, the figure is an em dash.

Excluded from each session's calculation:

- **Pit laps** (`is_pit_lap = 1`) — an in-lap or out-lap is not an attempt at a fast lap.
- **The first timed lap of each session** — it starts from a standing or rolling start and is not comparable.
- **Outliers, defined as laps slower than 110% of that session's own best lap.** The baseline is the session's best, not the car's all-time best at the track, for the same reason the whole metric is per-session: a wet session or an early one would otherwise have most of its laps discarded as outliers rather than measured. Simresults does not publish its outlier rule, so this threshold is chosen here on the precedent of motorsport's 107% rule, loosened slightly because a traffic lap in a race is normal rather than exceptional. It is documented in the code as a chosen value, not a derived one.

The suppression rule matters because a single lap is trivially 100% consistent, which is a lie, and one false figure discredits every honest number beside it.

Thresholds follow the convention the research found: ≥98% good, ≥95% fair, below that poor. These colour the value using the existing status palette, always with the number visible, never colour alone. They are only meaningful against the per-session definition above.

### 5.4 Progression

Best lap per calendar month for the selected entity at one track, as a line. The track defaults to the most-driven one and is selectable, since a single line mixing tracks would be nonsense.

This is the page's answer to "am I improving", and it is the closest LapDog gets to the reference-lap comparison that Garage 61 and VRS are built on: the reference is your own earlier self.

### 5.5 Time by category

The existing `StackedByCategory` component, unchanged, showing driving time split by session type and event context. It already folds a long tail into "Other" and already has a table view.

### 5.6 Racecraft

| Stat | Definition |
|---|---|
| Passes made / passed | `position_events` with `cause = 'OnTrack'`, human races only |
| Average grid → finish | `AVG(starting_position)`, `AVG(finish_position)` |
| Qualifying vs race pace | Race best lap minus qualifying best, paired on `subsession_id` |

Position events with causes `OpponentPit` and `OpponentOffWorld` are excluded from passes: inheriting a place because someone else stopped is not overtaking. This matches the existing dashboard treatment.

### 5.7 Rivals

Per-opponent head-to-head, deferred in `2026-08-04-lapdog-design.md` §18.3 and delivered here because `position_events.opponent_name` is already recorded:

| Driver | Passed them | Lost to | Net |
|---|---|---|---|
| N. Achterberg | 46 | 29 | +17 |
| C. Ferreira | 45 | 34 | +11 |

On-track causes only, human races only. Opponent names are shown as recorded, per the standing rule never to anonymise other drivers.

The panel is empty for a driver who only practises or races AI, so it renders the existing `Empty` component with an explanation rather than an empty table.

## 6. Store queries

Five new functions in `internal/store`, each taking a dimension so one implementation serves both pages — the same approach `Breakdown(f, by)` already uses with its `breakdownExpr` map:

```go
func (s *Store) EntityList(f Filter, by string) ([]EntityRow, error)
func (s *Store) EntityStats(f Filter, by string, id int) (EntityStats, error)
func (s *Store) EntityPace(f Filter, by string, id int) ([]PaceRow, error)
func (s *Store) EntityProgression(f Filter, by string, id, otherID int) ([]MonthRow, error)
func (s *Store) Rivals(f Filter) ([]RivalRow, error)
```

`by` accepts `"car"` and `"track"`; anything else is an error rather than a silent default, so a typo in a query string cannot quietly return the wrong dimension.

### 6.1 The fan-out rule

**Session-level and lap-level aggregates must be computed in separate subqueries and then joined.** Joining `laps` to `sessions` inside one aggregate multiplies every session-level sum by that session's lap count. Measured on the development dataset, a naive join reported 28,895 driving hours against a true total of 1,242.6 — a plausible-looking number more than twenty times too large.

The correct shape, verified to produce per-car hours summing to exactly the overall total:

```sql
WITH sess AS (SELECT car_id, SUM(driving_seconds) ... FROM sessions GROUP BY car_id),
     lap  AS (SELECT s.car_id, MIN(l.lap_time_s) ... FROM laps l JOIN sessions s ... GROUP BY s.car_id)
SELECT ... FROM sess LEFT JOIN lap USING (car_id)
```

`LEFT JOIN`, not `JOIN`: a car with sessions but no timed laps must still appear, showing its time with an em dash for pace.

### 6.2 Performance

Every query in this set was prototyped against the 1,331-session development database and returned in under 30 ms, the slowest being the per-car aggregate at 30 ms and per-track pace at 7 ms. No new indexes are required; `idx_laps_session` and `idx_laps_time` already cover these access paths.

## 7. API

| Endpoint | Returns |
|---|---|
| `GET /api/entities?by=car\|track` | Left-column list, ranked by driving time |
| `GET /api/entity?by=&id=` | Headline stats for one entity |
| `GET /api/pace?by=&id=` | Pace rows for the opposite dimension |
| `GET /api/progression?by=&id=&other=` | Best lap per month at one track |
| `GET /api/rivals` | Opponent head-to-head rows |

The category split reuses `GET /api/breakdown`. All accept the existing filter parameters.

**Correction, 2026-08-05.** An earlier draft of this section claimed the endpoints default `ExcludeAI` on "like every current read endpoint". That was wrong about the existing code: `exclude_ai` parses to false by default, no store query hard-codes the exclusion, and it is surfaced as a user checkbox that starts off. Exactly one caller opts in — the dashboard's grid-to-finish panel — and `lapdogctl summary` computes a separate human-only total, which is where the "AI excluded" wording in its output comes from.

The new endpoints follow that same architecture: the caller decides. Where the spec says a metric is human-only — the rivals panel in §5.7 and the racecraft figures in §5.6 — the frontend passes `excludeAi` explicitly, as the dashboard already does for its races panel. Putting the exclusion inside the query instead would make the parameter a lie, and changing the API-wide default would silently alter existing endpoints.

Small endpoints rather than one aggregate response, for a specific reason: the frontend keeps previous data as placeholder data per query, which is what stops charts unmounting and therefore what makes them animate across a filter change. One fat endpoint would collapse five independently-cached queries into one and lose that.

An unknown or absent id returns 404, matching `GET /api/sessions/{id}`. An unknown `by` returns 400.

## 8. What this deliberately does not do

**Sector times, optimal lap and theoretical best.** Every tool researched that offers these derives them from sector data: Race Ninja's "Perfect Lap" combines a driver's fastest sectors ([race.ninja](https://www.race.ninja/help/getting-started/what-is-a-perfect-lap), 2-1), and Simresults shows a "Best possible" lap broken out by sector ([simresults.net](https://simresults.net/), 3-0). The iRacing SDK exposes no sector-time variable; `SplitTimeInfo.Sectors[]` gives only boundary *percentages*, so a sector time must be derived by timing a `LapDistPct` crossing. At LapDog's poll rate of once per second — a requirement, not an accident — that is accurate to about a second, against the hundredths that sector analysis needs. Lap times are exact because the sim reports completed values; sector times would have to be sampled.

This stays deferred in `2026-08-04-lapdog-design.md` §18.3 and is not solvable by better SQL. It would need a much higher poll rate for the purpose, which is a different spec.

**Laps led, iRating and Safety Rating deltas, and strength of field.** All four appear in iRacing Insights' per-car tables and none is currently captured. They are additions to the collector, not to this feature.

**Corners per incident.** Requires a per-track corner count, which the telemetry does not provide. §5.1 uses distance instead and says so.

## 9. Testing

Go tests against the committed fixtures:

- Each query's shape and ordering, for both `by=car` and `by=track`.
- **A fan-out regression test asserting that per-entity driving hours sum to the total from `Totals`.** This is the specific failure §6.1 describes, it produces a plausible number rather than an error, and it is the one bug in this feature that could ship unnoticed.
- Consistency: the percentage and delta against hand-computed values; that pit laps, first laps and >110% laps are excluded; and that a session with fewer than 5 qualifying laps is dropped from the average rather than contributing a noisy value.
- **That consistency is computed per session and then averaged, not across the pooled laps.** A test with two sessions whose laps are individually tight but sit a second apart from each other must report high consistency. Pooling them reports low consistency, and both forms return a plausible percentage, so nothing but an explicit test distinguishes them. This is the correction that §5.3 records; it was found by measuring both forms rather than by reading the formula.
- `by` validation rejecting an unknown dimension.
- `LEFT JOIN` behaviour: an entity with sessions but no timed laps still appears.

**The outlier exclusion needs hand-authored fixture laps.** Measured against the synthetic dataset, 100% of laps for every car fall within 110% of their car+track best, and the per-session baseline is tighter still — the generator produces distributions too tight to contain any outlier, so a test relying on it would never execute the exclusion branch. The fixture must include a deliberately slow lap.

Handler tests: 404 on unknown id, 400 on unknown `by`, filter parameters reaching the store, and AI excluded by default.

Frontend: vitest for the derived formatting and the consistency threshold colouring, including the suppressed-value case. The existing `verify-layout` and `verify-animation` tools cover the new charts once the pages exist.

A note for whoever reviews the result on the development dataset: the synthetic data is optimistic. It shows 52 wins in 128 races and race laps 0.77 s *faster* than qualifying laps, which is backwards from reality. Both are generator artefacts, not query bugs, and the interface should not be tuned to make them look plausible.
