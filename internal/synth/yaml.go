package synth

import (
	"fmt"
	"strings"
)

// renderYAML builds a session-info YAML document for a weekend.
//
// The layout follows Appendix B of documentation/telemetry_11_23_15.pdf: top
// level keys at column zero, children indented one space, array entries marked
// with a dash at the child indent and their fields two further columns in.
//
// upToSession bounds which sessions have results filled in. Passing -1 means no
// results at all, which is what the document looks like when a weekend begins.
// Raising it as the weekend progresses reproduces the real behaviour where
// ResultsPositions and QualifyResultsInfo populate only as sessions conclude —
// the thing an .ibt file cannot represent and the reason captures carry the YAML
// repeatedly rather than once.
func renderYAML(w *Weekend, upToSession int) string {
	var b strings.Builder
	b.WriteString("---\n")

	writeWeekendInfo(&b, w)
	writeSessionInfo(&b, w, upToSession)
	writeQualifyResults(&b, w, upToSession)
	writeDriverInfo(&b, w)

	b.WriteString("...\n")
	return b.String()
}

func writeWeekendInfo(b *strings.Builder, w *Weekend) {
	fmt.Fprintf(b, "WeekendInfo:\n")
	fmt.Fprintf(b, " TrackName: %s\n", trackSlug(w.Track.Name))
	fmt.Fprintf(b, " TrackID: %d\n", w.Track.ID)
	fmt.Fprintf(b, " TrackLength: %0.2f km\n", w.Track.LengthKm)
	fmt.Fprintf(b, " TrackDisplayName: %s\n", w.Track.Name)
	fmt.Fprintf(b, " TrackDisplayShortName: %s\n", shortTrackName(w.Track.Name))
	fmt.Fprintf(b, " TrackConfigName: %s\n", w.Track.Config)
	fmt.Fprintf(b, " TrackCity: Unknown\n")
	fmt.Fprintf(b, " TrackCountry: Unknown\n")
	fmt.Fprintf(b, " TrackNumTurns: %d\n", 8+w.Track.ID%12)
	fmt.Fprintf(b, " TrackType: road course\n")
	fmt.Fprintf(b, " TrackAirTemp: %0.2f C\n", w.AirTempC)
	fmt.Fprintf(b, " TrackSurfaceTemp: %0.2f C\n", w.TrackTempC)
	fmt.Fprintf(b, " SeriesID: %d\n", w.SeriesID)
	fmt.Fprintf(b, " SeasonID: %d\n", w.SeasonID)
	fmt.Fprintf(b, " SessionID: %d\n", w.SessionID)
	fmt.Fprintf(b, " SubSessionID: %d\n", w.SubSessionID)
	fmt.Fprintf(b, " LeagueID: %d\n", w.LeagueID)
	fmt.Fprintf(b, " Official: %d\n", w.Official)
	fmt.Fprintf(b, " RaceWeek: %d\n", w.RaceWeek)
	fmt.Fprintf(b, " EventType: %s\n", w.EventType)
	fmt.Fprintf(b, " Category: Road\n")
	fmt.Fprintf(b, " SimMode: %s\n", w.SimMode)
	fmt.Fprintf(b, " TeamRacing: 0\n")
	fmt.Fprintf(b, " MinDrivers: 0\n")
	fmt.Fprintf(b, " MaxDrivers: 0\n")
	fmt.Fprintf(b, " NumCarClasses: 1\n")
	fmt.Fprintf(b, " NumCarTypes: 1\n")
	fmt.Fprintf(b, " WeekendOptions:\n")
	fmt.Fprintf(b, "  NumStarters: %d\n", len(w.Opponents)+1)
	fmt.Fprintf(b, "  StartingGrid: 2x2 inline pole on left\n")
	fmt.Fprintf(b, "  QualifyScoring: best lap\n")
	fmt.Fprintf(b, "  CourseCautions: off\n")
	fmt.Fprintf(b, "  StandingStart: 0\n")
	fmt.Fprintf(b, "  Restarts: single file\n")
	fmt.Fprintf(b, "  WeatherType: static\n")
	fmt.Fprintf(b, "  Skies: partly cloudy\n")
	fmt.Fprintf(b, "  Unofficial: %d\n", 1-w.Official)
	fmt.Fprintf(b, "  NightMode: 0\n")
	fmt.Fprintf(b, "  IsFixedSetup: 0\n")
	fmt.Fprintf(b, "  HardcoreLevel: 1\n")
	fmt.Fprintf(b, " TelemetryOptions:\n")
	fmt.Fprintf(b, "  TelemetryDiskFile: \"\"\n")
}

func writeSessionInfo(b *strings.Builder, w *Weekend, upToSession int) {
	fmt.Fprintf(b, "SessionInfo:\n")
	fmt.Fprintf(b, " NumSessions: %d\n", len(w.Sessions))
	fmt.Fprintf(b, " Sessions:\n")

	for i := range w.Sessions {
		s := &w.Sessions[i]
		fmt.Fprintf(b, " - SessionNum: %d\n", s.Num)
		fmt.Fprintf(b, "   SessionLaps: %s\n", s.LapsField())
		fmt.Fprintf(b, "   SessionTime: %s\n", s.TimeField())
		fmt.Fprintf(b, "   SessionNumLapsToAvg: 0\n")
		fmt.Fprintf(b, "   SessionType: %s\n", s.RawType)
		fmt.Fprintf(b, "   SessionTrackRubberState: moderately high usage\n")

		if i > upToSession || len(s.Results) == 0 {
			// Not yet run, so the sim emits the key with no entries.
			fmt.Fprintf(b, "   ResultsPositions:\n")
			fmt.Fprintf(b, "   ResultsAverageLapTime: -1.0000\n")
			fmt.Fprintf(b, "   ResultsNumCautionFlags: 0\n")
			fmt.Fprintf(b, "   ResultsNumLeadChanges: 0\n")
			fmt.Fprintf(b, "   ResultsLapsComplete: -1\n")
			fmt.Fprintf(b, "   ResultsOfficial: 0\n")
			continue
		}

		fmt.Fprintf(b, "   ResultsPositions:\n")
		for _, r := range s.Results {
			fmt.Fprintf(b, "   - Position: %d\n", r.Position)
			fmt.Fprintf(b, "     ClassPosition: %d\n", r.ClassPosition)
			fmt.Fprintf(b, "     CarIdx: %d\n", r.CarIdx)
			fmt.Fprintf(b, "     Lap: %d\n", r.LapsComplete)
			fmt.Fprintf(b, "     Time: %0.3f\n", r.TotalTimeS)
			fmt.Fprintf(b, "     FastestLap: %d\n", r.FastestLap)
			fmt.Fprintf(b, "     FastestTime: %0.3f\n", r.FastestTimeS)
			fmt.Fprintf(b, "     LastTime: %0.3f\n", r.FastestTimeS)
			fmt.Fprintf(b, "     LapsLed: %d\n", r.LapsLed)
			fmt.Fprintf(b, "     LapsComplete: %d\n", r.LapsComplete)
			fmt.Fprintf(b, "     LapsDriven: %0.3f\n", float64(r.LapsComplete))
			fmt.Fprintf(b, "     Incidents: %d\n", r.Incidents)
			fmt.Fprintf(b, "     ReasonOutId: %d\n", r.ReasonOutID)
			fmt.Fprintf(b, "     ReasonOutStr: %s\n", reasonOutString(r.ReasonOutID))
		}
		fmt.Fprintf(b, "   ResultsAverageLapTime: %0.4f\n", s.AverageLapS)
		fmt.Fprintf(b, "   ResultsNumCautionFlags: %d\n", s.CautionFlags)
		fmt.Fprintf(b, "   ResultsNumLeadChanges: %d\n", s.LeadChanges)
		fmt.Fprintf(b, "   ResultsLapsComplete: %d\n", s.LapsComplete)
		fmt.Fprintf(b, "   ResultsOfficial: %d\n", w.Official)
	}
}

func writeQualifyResults(b *strings.Builder, w *Weekend, upToSession int) {
	fmt.Fprintf(b, "QualifyResultsInfo:\n")
	fmt.Fprintf(b, " Results:\n")

	qi := w.qualifyIndex()
	if qi < 0 || qi > upToSession || len(w.QualifyResults) == 0 {
		// Qualifying has not run, so the section is present but empty. This is
		// exactly why the collector re-reads the YAML rather than caching it
		// once at session start.
		return
	}
	for _, q := range w.QualifyResults {
		fmt.Fprintf(b, " - Position: %d\n", q.Position)
		fmt.Fprintf(b, "   ClassPosition: %d\n", q.ClassPosition)
		fmt.Fprintf(b, "   CarIdx: %d\n", q.CarIdx)
		fmt.Fprintf(b, "   FastestLap: %d\n", q.FastestLap)
		fmt.Fprintf(b, "   FastestTime: %0.3f\n", q.FastestTimeS)
	}
}

func writeDriverInfo(b *strings.Builder, w *Weekend) {
	fmt.Fprintf(b, "DriverInfo:\n")
	fmt.Fprintf(b, " DriverCarIdx: %d\n", w.DriverCarIdx)
	fmt.Fprintf(b, " DriverUserID: %d\n", DriverUserID)
	fmt.Fprintf(b, " DriverCarIdleRPM: %0.3f\n", w.Car.RedlineRPM*0.12)
	fmt.Fprintf(b, " DriverCarRedLine: %0.3f\n", w.Car.RedlineRPM)
	fmt.Fprintf(b, " DriverCarFuelMaxLtr: %0.3f\n", w.Car.FuelCapacityL)
	fmt.Fprintf(b, " DriverCarMaxFuelPct: 1.000\n")
	fmt.Fprintf(b, " DriverPitTrkPct: %0.6f\n", 0.03)
	fmt.Fprintf(b, " DriverCarEstLapTime: %0.4f\n", w.BaseLapS)
	fmt.Fprintf(b, " DriverSetupName: baseline.sto\n")
	fmt.Fprintf(b, " DriverSetupIsModified: 0\n")
	fmt.Fprintf(b, " DriverSetupLoadTypeName: user\n")
	fmt.Fprintf(b, " DriverSetupPassedTech: 1\n")
	fmt.Fprintf(b, " Drivers:\n")

	writeDriver := func(carIdx int, name string, userID, iRating int, isAI bool) {
		fmt.Fprintf(b, " - CarIdx: %d\n", carIdx)
		fmt.Fprintf(b, "   UserName: %s\n", name)
		fmt.Fprintf(b, "   AbbrevName: %s\n", name)
		fmt.Fprintf(b, "   Initials: %s\n", initials(name))
		fmt.Fprintf(b, "   UserID: %d\n", userID)
		fmt.Fprintf(b, "   TeamID: 0\n")
		fmt.Fprintf(b, "   TeamName: %s\n", name)
		fmt.Fprintf(b, "   CarNumber: \"%d\"\n", carIdx+1)
		fmt.Fprintf(b, "   CarNumberRaw: %d\n", carIdx+1)
		fmt.Fprintf(b, "   CarPath: %s\n", w.Car.Path)
		fmt.Fprintf(b, "   CarClassID: %d\n", w.Car.ClassID)
		fmt.Fprintf(b, "   CarID: %d\n", w.Car.ID)
		fmt.Fprintf(b, "   CarScreenName: %s\n", w.Car.Name)
		fmt.Fprintf(b, "   CarScreenNameShort: %s\n", w.Car.ShortName)
		fmt.Fprintf(b, "   CarClassShortName: %s\n", w.Car.ClassName)
		fmt.Fprintf(b, "   CarClassRelSpeed: 0\n")
		fmt.Fprintf(b, "   CarClassLicenseLevel: 0\n")
		// Ratings, as the simulator actually reports them.
		//
		// Offline sessions do not carry real ratings. A capture from a real AI practice
		// session gave an established account IRating 1, LicLevel 1, LicSubLevel 1 and
		// LicString "R 0.01", and every AI opponent IRating 0 — placeholders, not the
		// account's standing. LapDog discards ratings from offline documents for exactly
		// that reason, and the generator reports the placeholders so the fixtures encode
		// why rather than only that.
		//
		// SubSessionID is the marker, matching sessionyaml's own gate: the service
		// allocates it, so an offline weekend has none.
		if w.SubSessionID == 0 {
			ir := 0
			if !isAI {
				ir = 1
			}
			fmt.Fprintf(b, "   IRating: %d\n", ir)
			fmt.Fprintf(b, "   LicLevel: %d\n", 1)
			fmt.Fprintf(b, "   LicSubLevel: %d\n", 1)
			fmt.Fprintf(b, "   LicString: R 0.01\n")
		} else {
			fmt.Fprintf(b, "   IRating: %d\n", iRating)
			fmt.Fprintf(b, "   LicLevel: %d\n", 13)
			fmt.Fprintf(b, "   LicSubLevel: %d\n", 355)
			fmt.Fprintf(b, "   LicString: A 3.55\n")
		}
		fmt.Fprintf(b, "   IsSpectator: 0\n")
		fmt.Fprintf(b, "   ClubName: Midwest\n")
		fmt.Fprintf(b, "   DivisionName: Division 3\n")
		// CarIsAI postdates the bundled SDK documentation. It is emitted only
		// for AI events, and only when the weekend is flagged as reporting it,
		// so that both the field-present and field-absent classifier paths are
		// exercised by the dataset. See spec section 6.5.
		if w.EmitCarIsAI {
			flag := 0
			if isAI {
				flag = 1
			}
			fmt.Fprintf(b, "   CarIsAI: %d\n", flag)
		}
	}

	writeDriver(w.DriverCarIdx, DriverName, DriverUserID, w.DriverIRating, false)
	for i, o := range w.Opponents {
		idx := i
		if idx >= w.DriverCarIdx {
			idx++
		}
		writeDriver(idx, o.Name, o.UserID, o.IRating, o.IsAI)
	}

	fmt.Fprintf(b, "SplitTimeInfo:\n")
	fmt.Fprintf(b, " Sectors:\n")
	fmt.Fprintf(b, " - SectorNum: 0\n")
	fmt.Fprintf(b, "   SectorStartPct: 0.000000\n")
	fmt.Fprintf(b, " - SectorNum: 1\n")
	fmt.Fprintf(b, "   SectorStartPct: 0.333333\n")
	fmt.Fprintf(b, " - SectorNum: 2\n")
	fmt.Fprintf(b, "   SectorStartPct: 0.666667\n")
}

// reasonOutString maps a reason-out code to the sim's label.
func reasonOutString(id int) string {
	switch id {
	case 0:
		return "Running"
	case 1:
		return "Disconnected"
	case 2:
		return "Retired"
	default:
		return "Running"
	}
}

// trackSlug approximates the sim's internal track directory name.
func trackSlug(name string) string {
	s := strings.ToLower(name)
	for _, cut := range []string{" ", "-", "'", ".", "ü", "ö", "å"} {
		switch cut {
		case "ü":
			s = strings.ReplaceAll(s, cut, "u")
		case "ö":
			s = strings.ReplaceAll(s, cut, "o")
		case "å":
			s = strings.ReplaceAll(s, cut, "a")
		default:
			s = strings.ReplaceAll(s, cut, "")
		}
	}
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

// shortTrackName trims a circuit's formal name to the sim's short form.
func shortTrackName(name string) string {
	for _, suffix := range []string{
		" International Racing Course", " International Raceway",
		" Street Circuit", " International", " Racing Course",
		" Circuit", " Raceway", " Park",
	} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	for _, prefix := range []string{"Circuit de ", "Circuit ", "Autodromo Nazionale ", "Mount "} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}

// initials renders a driver's initials the way the sim does.
func initials(name string) string {
	parts := strings.Fields(name)
	var out strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		out.WriteByte(p[0])
	}
	return out.String()
}
