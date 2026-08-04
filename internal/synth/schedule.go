package synth

import (
	"math"
	"math/rand"
	"time"
)

// Weeks is how many weeks of history the generator produces.
const Weeks = 104

// Weekly driving-hour bounds. The distribution is deliberately kept inside
// these bounds every week rather than allowing empty weeks, so the dashboard
// has a continuous two-year series to render.
const (
	MinWeeklyHours = 8.0
	MaxWeeklyHours = 15.0
)

// ScheduleOptions configures schedule generation.
type ScheduleOptions struct {
	// End is the last day covered. The schedule runs backwards from the Monday
	// of this day's week for Weeks weeks.
	End time.Time
	// Seed makes generation deterministic, so the same dataset is produced on
	// every run and golden tests remain stable.
	Seed int64
}

// BuildSchedule produces the full two-year list of weekends in chronological
// order.
func BuildSchedule(opts ScheduleOptions) []*Weekend {
	rng := rand.New(rand.NewSource(opts.Seed))

	// Walk back to the Monday that starts the earliest week.
	end := time.Date(opts.End.Year(), opts.End.Month(), opts.End.Day(), 0, 0, 0, 0, time.UTC)
	// limit is exclusive, so activity on the final day itself is kept.
	limit := end.AddDate(0, 0, 1)
	weekday := (int(end.Weekday()) + 6) % 7 // Monday = 0
	lastMonday := end.AddDate(0, 0, -weekday)
	firstMonday := lastMonday.AddDate(0, 0, -7*(Weeks-1))

	var out []*Weekend
	subsession := 30000000
	sessionID := 220000

	for week := 0; week < Weeks; week++ {
		monday := firstMonday.AddDate(0, 0, 7*week)
		progress := float64(week) / float64(Weeks-1)

		target := MinWeeklyHours + rng.Float64()*(MaxWeeklyHours-MinWeeklyHours)
		remaining := target * 3600

		season := week / 13
		raceWeek := week%13 + 1

		events := planWeek(rng, week, progress, remaining)
		for _, ev := range events {
			// Skip Sunday entirely: the synthetic driver never races on one.
			day := ev.weekday
			if day == 0 {
				day = 6
			}
			date := monday.AddDate(0, 0, day-1)
			startHour := 18 + rng.Intn(4) // evenings, with weekend variation
			if day >= 5 {
				startHour = 9 + rng.Intn(10)
			}
			started := time.Date(date.Year(), date.Month(), date.Day(),
				startHour, rng.Intn(60), 0, 0, time.UTC)

			// The final week is partial: the schedule is built from whole weeks,
			// so days past the requested end must be dropped rather than
			// producing history that has not happened yet.
			if !started.Before(limit) {
				continue
			}

			subsession++
			sessionID++
			w := buildWeekend(rng, ev, started, subsession, sessionID, season, raceWeek, progress)
			out = append(out, w)
		}
	}
	return out
}

// plannedEvent is a weekend the scheduler has decided to include, before its
// details are filled in.
type plannedEvent struct {
	flavour EventFlavour
	// weekday is a Go time.Weekday value. Sunday is never produced.
	weekday int
	// driveSeconds is the driving time this event should contribute.
	driveSeconds float64
	leagueIndex  int
	seriesIndex  int
	carIndex     int
}

// planWeek decides which events fill one week's driving budget.
//
// Composition is weighted so that official racing dominates, league nights
// recur on their fixed evenings, and the rarer flavours — AI, hosted, offline
// testing, time trial — appear often enough across two years to exercise every
// classifier branch without swamping the dataset.
func planWeek(rng *rand.Rand, week int, progress, budget float64) []plannedEvent {
	var events []plannedEvent
	remaining := budget

	take := func(fraction float64) float64 {
		s := remaining * fraction
		remaining -= s
		return s
	}

	// League nights. Both leagues run most weeks, each on its own evening.
	for li, lg := range Leagues {
		if rng.Float64() < 0.78 {
			events = append(events, plannedEvent{
				flavour:      FlavourLeague,
				weekday:      lg.Weekday,
				driveSeconds: take(0.30),
				leagueIndex:  li,
				carIndex:     lg.CarIndex,
			})
		}
	}

	// One or two official race weekends, on a weekday and often the weekend.
	officialCount := 1
	if rng.Float64() < 0.55 {
		officialCount = 2
	}
	for i := 0; i < officialCount; i++ {
		si := rng.Intn(len(SeriesList))
		day := 6 // Saturday
		if i == 0 {
			day = 1 + rng.Intn(5) // Monday to Friday
		}
		events = append(events, plannedEvent{
			flavour:      FlavourOfficialRace,
			weekday:      day,
			driveSeconds: take(0.45),
			seriesIndex:  si,
			carIndex:     SeriesList[si].CarIndex,
		})
	}

	// Rarer flavours, on a rotating basis keyed to the week so that each recurs
	// predictably across the two years rather than clustering by chance.
	switch week % 4 {
	case 0:
		si := rng.Intn(len(SeriesList))
		events = append(events, plannedEvent{
			flavour:      FlavourAI,
			weekday:      1 + rng.Intn(5),
			driveSeconds: take(0.45),
			seriesIndex:  si,
			carIndex:     SeriesList[si].CarIndex,
		})
	case 1:
		events = append(events, plannedEvent{
			flavour:      FlavourHosted,
			weekday:      1 + rng.Intn(6),
			driveSeconds: take(0.40),
			carIndex:     rng.Intn(len(Cars)),
		})
	case 2:
		events = append(events, plannedEvent{
			flavour:      FlavourOfflineTest,
			weekday:      1 + rng.Intn(5),
			driveSeconds: take(0.45),
			carIndex:     rng.Intn(len(Cars)),
		})
	case 3:
		if rng.Float64() < 0.6 {
			events = append(events, plannedEvent{
				flavour:      FlavourTimeTrial,
				weekday:      1 + rng.Intn(6),
				driveSeconds: take(0.35),
				carIndex:     rng.Intn(len(Cars)),
			})
		}
	}

	// Whatever budget is left goes to standalone public practice, which is what
	// a real driver spends idle evenings on. The last one absorbs the remainder
	// so the week's total lands on its target instead of leaving a slice unspent.
	for remaining > 300 {
		si := rng.Intn(len(SeriesList))
		share := 0.55
		if remaining < 3600 {
			share = 1.0
		}
		events = append(events, plannedEvent{
			flavour:      FlavourOfficialPractice,
			weekday:      1 + rng.Intn(6),
			driveSeconds: take(share),
			seriesIndex:  si,
			carIndex:     SeriesList[si].CarIndex,
		})
	}
	return events
}

// buildWeekend fills in a planned event's track, car, field and session list.
func buildWeekend(
	rng *rand.Rand, ev plannedEvent, started time.Time,
	subsession, sessionID, season, raceWeek int, progress float64,
) *Weekend {
	car := Cars[ev.carIndex%len(Cars)]

	// Official series rotate track weekly; other flavours pick freely.
	var track Track
	if ev.flavour == FlavourOfficialRace || ev.flavour == FlavourOfficialPractice {
		track = Tracks[(raceWeek-1+ev.seriesIndex*3)%len(Tracks)]
	} else {
		track = Tracks[rng.Intn(len(Tracks))]
	}

	// Pace improves over the two years, with week-to-week variation so the lap
	// time trend is a believable downward scatter rather than a clean line.
	pace := 1.048 - 0.052*progress + (rng.Float64()-0.5)*0.014
	// The climb stops short of dominating the field. Ending well above
	// FieldStrength produced a driver who qualified second and won almost every
	// race by the final season, which reads as a broken dataset rather than an
	// improving one.
	iRating := int(1450 + 780*progress + float64(rng.Intn(90)-45))
	incidentRate := 4.6 - 3.1*progress + (rng.Float64()-0.5)*0.8
	if incidentRate < 0.4 {
		incidentRate = 0.4
	}

	w := &Weekend{
		Flavour:             ev.flavour,
		StartedAt:           started,
		Track:               track,
		Car:                 car,
		BaseLapS:            track.BaseLapS * car.PaceFactor,
		PaceFactor:          pace,
		IncidentRatePerHour: incidentRate,
		DriverIRating:       iRating,
		SimMode:             "full",
		RaceWeek:            raceWeek,
		SeasonID:            4700 + season,
		AirTempC:            18 + rng.Float64()*14,
		TrackTempC:          24 + rng.Float64()*20,
	}

	switch ev.flavour {
	case FlavourOfficialRace:
		s := SeriesList[ev.seriesIndex]
		w.SubSessionID = subsession
		w.SessionID = sessionID
		w.SeriesID = s.ID
		w.Official = 1
		w.EventType = "Race"
		w.DriverCarIdx = rng.Intn(12)
		w.Opponents = humanField(rng, 15+rng.Intn(20))
		w.Sessions = raceWeekendSessions(rng, w, ev.driveSeconds, s.RaceLaps, "Practice", "Open Qualify")

	case FlavourOfficialPractice:
		s := SeriesList[ev.seriesIndex]
		w.SubSessionID = subsession
		w.SessionID = sessionID
		w.SeriesID = s.ID
		w.Official = 1
		w.EventType = "Practice"
		w.DriverCarIdx = rng.Intn(8)
		w.Opponents = humanField(rng, 4+rng.Intn(12))
		w.Sessions = []Session{practiceSession(rng, w, 0, "Open Practice", ev.driveSeconds)}

	case FlavourLeague:
		lg := Leagues[ev.leagueIndex]
		w.SubSessionID = subsession
		w.SessionID = sessionID
		w.LeagueID = lg.ID
		w.Official = 0
		w.EventType = "Race"
		w.DriverCarIdx = rng.Intn(10)
		w.Opponents = humanField(rng, 12+rng.Intn(10))
		w.Sessions = raceWeekendSessions(rng, w, ev.driveSeconds, lg.RaceLaps, "Practice", "Lone Qualify")

	case FlavourHosted:
		w.SubSessionID = subsession
		w.SessionID = sessionID
		w.Official = 0
		w.EventType = "Race"
		w.DriverCarIdx = rng.Intn(8)
		w.Opponents = humanField(rng, 6+rng.Intn(10))
		w.Sessions = raceWeekendSessions(rng, w, ev.driveSeconds, 12+rng.Intn(10), "Practice", "")

	case FlavourAI:
		// AI events are offline, so the sim reports no subsession.
		w.SubSessionID = 0
		w.SessionID = 0
		w.Official = 0
		w.EventType = "Race"
		w.DriverCarIdx = 0
		w.Opponents = aiField(rng, 11+rng.Intn(12))
		// Most AI weekends report CarIsAI; a deliberate minority do not, which
		// forces the classifier's documented heuristic to be exercised by real
		// fixture data rather than only by synthetic unit tests.
		w.EmitCarIsAI = rng.Float64() < 0.8
		w.Sessions = raceWeekendSessions(rng, w, ev.driveSeconds, 10+rng.Intn(12), "Practice", "")

	case FlavourOfflineTest:
		w.SubSessionID = 0
		w.SessionID = 0
		w.Official = 0
		w.EventType = "Test"
		w.DriverCarIdx = 0
		w.Opponents = nil
		w.Sessions = []Session{practiceSession(rng, w, 0, "Offline Testing", ev.driveSeconds)}

	case FlavourTimeTrial:
		w.SubSessionID = subsession
		w.SessionID = sessionID
		w.Official = 1
		w.EventType = "Time Trial"
		w.DriverCarIdx = 0
		w.Opponents = nil
		w.Sessions = []Session{practiceSession(rng, w, 0, "Time Trial", ev.driveSeconds)}
	}
	return w
}

// practiceSession builds a single non-race session sized to a driving budget.
func practiceSession(rng *rand.Rand, w *Weekend, num int, rawType string, driveSeconds float64) Session {
	lap := w.LapSeconds()
	// Round rather than truncate. Truncating loses up to a full lap per session,
	// and with several sessions a week that bias is enough to push a week below
	// the intended eight-hour floor.
	laps := int(math.Round(driveSeconds / lap))
	if laps < 1 {
		laps = 1
	}
	drive := float64(laps) * lap
	return Session{
		Num:           num,
		RawType:       rawType,
		GarageSeconds: garageTime(rng, drive, 0.22, 0.55),
		PitSeconds:    pitTime(rng, drive),
		Laps:          laps,
		TimeLimitS:    0,
		ReplaySeconds: replayTime(rng),
	}
}

// garageTime returns non-driving connected time: menus, setup screens, sitting
// in the garage between runs.
//
// It scales with the length of the session rather than being a fixed amount,
// because a two-hour practice involves proportionally more faffing than a
// ten-minute one. This is what makes the utilisation ratio the dashboard shows
// land in a believable range instead of implying the driver never left the car.
func garageTime(rng *rand.Rand, driveSeconds, lo, hi float64) float64 {
	return 60 + driveSeconds*(lo+rng.Float64()*(hi-lo))
}

// pitTime returns time in the car but stationary in the pit box, which accrues
// in-car time without accruing driving time.
func pitTime(rng *rand.Rand, driveSeconds float64) float64 {
	return 15 + driveSeconds*(0.02+rng.Float64()*0.06)
}

// raceWeekendSessions builds a practice, optional qualifying, and race session
// sharing a driving budget.
func raceWeekendSessions(
	rng *rand.Rand, w *Weekend, driveSeconds float64, raceLaps int,
	practiceType, qualifyType string,
) []Session {
	lap := w.LapSeconds()

	// The race distance is fixed by the series, and qualifying is a couple of
	// laps. Practice absorbs exactly what is left, so the event delivers the
	// driving budget it was allocated rather than systematically undershooting.
	raceDrive := float64(raceLaps) * lap
	qualLaps := 0
	if qualifyType != "" {
		qualLaps = 2 + rng.Intn(3)
	}
	rest := driveSeconds - raceDrive - float64(qualLaps)*lap

	var out []Session
	num := 0

	practiceLaps := int(math.Round(rest / lap))
	if practiceLaps < 2 {
		practiceLaps = 2
	}
	practiceDrive := float64(practiceLaps) * lap
	out = append(out, Session{
		Num:           num,
		RawType:       practiceType,
		GarageSeconds: garageTime(rng, practiceDrive, 0.25, 0.6),
		PitSeconds:    pitTime(rng, practiceDrive),
		Laps:          practiceLaps,
		TimeLimitS:    900,
		ReplaySeconds: replayTime(rng),
	})
	num++

	if qualLaps > 0 {
		qualDrive := float64(qualLaps) * lap
		out = append(out, Session{
			Num: num,
			// Qualifying involves a lot of waiting for a gap, so its garage
			// share is deliberately the highest of the three.
			RawType:       qualifyType,
			GarageSeconds: garageTime(rng, qualDrive, 0.5, 1.3),
			PitSeconds:    pitTime(rng, qualDrive),
			Laps:          qualLaps,
			TimeLimitS:    600,
			ReplaySeconds: 0,
		})
		num++
	}

	out = append(out, Session{
		Num: num,
		// A race is the most efficient use of time: gridding up and the
		// cool-down lap are the only non-driving parts.
		RawType:       "Race",
		GarageSeconds: garageTime(rng, raceDrive, 0.06, 0.18),
		PitSeconds:    pitTime(rng, raceDrive),
		Laps:          raceLaps,
		LapLimit:      raceLaps,
		ReplaySeconds: replayTime(rng),
	})
	return out
}

// replayTime returns how long the driver watched a replay, which must
// contribute to no counter. Most sessions have none.
func replayTime(rng *rand.Rand) float64 {
	if rng.Float64() < 0.22 {
		return 45 + rng.Float64()*260
	}
	return 0
}

// FieldStrength is the rating the synthetic opponent pool is centred on.
//
// It is deliberately fixed rather than drawn around the local driver. Centring the
// field on the driver would hold their relative position constant, so two years of
// improving pace would produce no improvement in grid slots or results — which is
// precisely what an earlier version did.
const FieldStrength = 2050

// humanField builds a field of human opponents with plausible ratings.
func humanField(rng *rand.Rand, n int) []Opponent {
	if n > MaxCars-1 {
		n = MaxCars - 1
	}
	out := make([]Opponent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Opponent{
			Name:    opponentNames[(i*7+rng.Intn(5))%len(opponentNames)],
			UserID:  400000 + rng.Intn(300000),
			IRating: clampInt(FieldStrength+rng.Intn(1600)-800, 350, 6500),
			IsAI:    false,
		})
	}
	return out
}

// aiField builds a field of AI opponents.
func aiField(rng *rand.Rand, n int) []Opponent {
	if n > MaxCars-1 {
		n = MaxCars - 1
	}
	out := make([]Opponent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, Opponent{
			Name:    opponentNames[(i*11+3)%len(opponentNames)],
			UserID:  0,
			IRating: 0,
			IsAI:    true,
		})
	}
	return out
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
