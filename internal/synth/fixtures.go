package synth

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// FixtureSeed is the seed the committed fixture set is generated with. Changing
// it changes every committed capture, so it is a constant rather than an option.
const FixtureSeed int64 = 19770413

// FixtureCase describes one committed fixture and what it exists to exercise.
type FixtureCase struct {
	// Name is the capture's filename stem, chosen to read as documentation in a
	// test failure message.
	Name string
	// Why records the specific ingestion behaviour this fixture covers.
	Why string

	flavour     EventFlavour
	seriesIndex int
	leagueIndex int
	carIndex    int
	trackIndex  int
	driveSecs   float64
	emitCarIsAI bool
	forceNoAI   bool
}

// FixtureCases is the committed fixture set.
//
// The full two-year dataset is far too large to commit — roughly 275 MB of
// gzipped captures — so it is generated on demand and gitignored. This much
// smaller set is committed instead, chosen so that every classifier branch and
// both AI-detection paths have a real capture behind them rather than only a
// synthetic in-memory Info value.
var FixtureCases = []FixtureCase{
	{
		Name: "official-race-weekend", flavour: FlavourOfficialRace,
		Why:         "practice inside a race weekend classifies as Race Practice; SessionNum advances 0->1->2; qualifying and finish results populate mid-capture",
		seriesIndex: 0, carIndex: 0, trackIndex: 0, driveSecs: 2400,
	},
	{
		Name: "public-practice", flavour: FlavourOfficialPractice,
		Why:         "a practice-only official weekend classifies as Public Practice, not Race Practice",
		seriesIndex: 1, carIndex: 1, trackIndex: 1, driveSecs: 1500,
	},
	{
		Name: "league-race-weekend", flavour: FlavourLeague,
		Why:         "LeagueID beats every other context rule; league practice, qualifying and race all carry it",
		leagueIndex: 0, carIndex: 0, trackIndex: 2, driveSecs: 2700,
	},
	{
		Name: "hosted-race", flavour: FlavourHosted,
		Why:        "unofficial, non-league, online session falls through to Hosted",
		carIndex:   2, trackIndex: 3, driveSecs: 1800,
	},
	{
		Name: "ai-race-field-present", flavour: FlavourAI,
		Why:         "CarIsAI present on the driver entries drives AIDetection=field and context AI",
		seriesIndex: 0, carIndex: 0, trackIndex: 4, driveSecs: 1500, emitCarIsAI: true,
	},
	{
		Name: "ai-race-field-absent", flavour: FlavourAI,
		Why:         "CarIsAI absent forces the documented heuristic: offline race, unofficial, no league, several drivers -> AIDetection=heuristic",
		seriesIndex: 0, carIndex: 3, trackIndex: 5, driveSecs: 1500, forceNoAI: true,
	},
	{
		Name: "offline-test-drive", flavour: FlavourOfflineTest,
		Why:        "Offline Testing normalises to OfflineTest and always counts, with no setting to disable it",
		carIndex:   4, trackIndex: 6, driveSecs: 2100,
	},
	{
		Name: "time-trial", flavour: FlavourTimeTrial,
		Why:        "Time Trial gets its own context and label rather than being folded into practice",
		carIndex:   3, trackIndex: 7, driveSecs: 1200,
	},
	{
		Name: "short-session", flavour: FlavourOfficialPractice,
		Why:         "a session below the minimum recordable length must be discarded by the collector",
		seriesIndex: 2, carIndex: 5, trackIndex: 11, driveSecs: 20,
	},
}

// WriteFixtures generates the committed fixture set into dir.
func WriteFixtures(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("synth: create %s: %w", dir, err)
	}

	// A fixed date keeps the fixtures stable: nothing about them may depend on
	// when the generator was run, or the committed files would churn.
	base := time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC) // a Monday

	var written []string
	for i, fc := range FixtureCases {
		rng := rand.New(rand.NewSource(FixtureSeed + int64(i)*104729))
		started := base.AddDate(0, 0, i)
		// Never place a fixture on a Sunday, matching the dataset's own rule.
		for started.Weekday() == time.Sunday {
			started = started.AddDate(0, 0, 1)
		}

		ev := plannedEvent{
			flavour:      fc.flavour,
			weekday:      int(started.Weekday()),
			driveSeconds: fc.driveSecs,
			leagueIndex:  fc.leagueIndex,
			seriesIndex:  fc.seriesIndex,
			carIndex:     fc.carIndex,
		}
		w := buildWeekend(rng, ev, started, 41000000+i, 331000+i, 0, fc.trackIndex+1, 0.5)

		// Pin the track so a fixture always uses the circuit its name implies.
		w.Track = Tracks[fc.trackIndex%len(Tracks)]
		w.BaseLapS = w.Track.BaseLapS * w.Car.PaceFactor
		// Pin pace so lap times are stable across regenerations.
		w.PaceFactor = 1.02
		w.IncidentRatePerHour = 3.0

		if fc.flavour == FlavourAI {
			w.EmitCarIsAI = fc.emitCarIsAI && !fc.forceNoAI
		}

		path := filepath.Join(dir, fc.Name+".lpd")
		if err := SimulateWeekend(w, path, FixtureSeed+int64(i)*7919); err != nil {
			return written, fmt.Errorf("synth: fixture %s: %w", fc.Name, err)
		}
		written = append(written, path)
	}
	return written, nil
}
