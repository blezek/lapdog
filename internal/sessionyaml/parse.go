package sessionyaml

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse decodes a session-info YAML document. Unknown keys are ignored.
func Parse(b []byte) (*Info, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errors.New("sessionyaml: empty document")
	}
	var i Info
	if err := yaml.Unmarshal(b, &i); err != nil {
		return nil, fmt.Errorf("sessionyaml: unmarshal: %w", err)
	}
	return &i, nil
}

// Me returns the local driver's entry, matched on DriverCarIdx.
func (i *Info) Me() (Driver, bool) {
	for _, d := range i.DriverInfo.Drivers {
		if d.CarIdx == i.DriverInfo.DriverCarIdx {
			return d, true
		}
	}
	return Driver{}, false
}

// SessionByNum returns the session with the given SessionNum.
func (i *Info) SessionByNum(n int) (Session, bool) {
	for _, s := range i.SessionInfo.Sessions {
		if s.SessionNum == n {
			return s, true
		}
	}
	return Session{}, false
}

// HasRaceSession reports whether any session in the weekend is a race.
//
// This is what separates race practice from public practice: a practice session
// inside a weekend that also has a race is race practice.
func (i *Info) HasRaceSession() bool {
	for _, s := range i.SessionInfo.Sessions {
		if IsRaceType(s.SessionType) {
			return true
		}
	}
	return false
}

// IsRaceType reports whether a raw SessionType string denotes a race. Heats and
// consolation races count, since they are raced wheel to wheel.
func IsRaceType(raw string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(raw), " ")) {
	case "race", "heat", "consolation":
		return true
	}
	return false
}

// TrackLengthKm extracts the numeric kilometre value from the "7.00 km" form
// the sim emits, returning 0 if it cannot be parsed.
func (i *Info) TrackLengthKm() float64 {
	f := strings.Fields(i.WeekendInfo.TrackLength)
	if len(f) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// AIOpponentCount returns the number of AI opponents excluding the local driver,
// and whether the CarIsAI field was present at all.
//
// fieldPresent false means the field is absent from every driver entry, which is
// the classifier's signal to use its documented heuristic instead.
func (i *Info) AIOpponentCount() (int, bool) {
	present := false
	count := 0
	for _, d := range i.DriverInfo.Drivers {
		if d.CarIsAI == nil {
			continue
		}
		present = true
		if *d.CarIsAI != 0 && d.CarIdx != i.DriverInfo.DriverCarIdx {
			count++
		}
	}
	return count, present
}

// MyResult returns the local driver's classified result for a session.
func (i *Info) MyResult(sessionNum int) (ResultPosition, bool) {
	s, ok := i.SessionByNum(sessionNum)
	if !ok {
		return ResultPosition{}, false
	}
	for _, r := range s.ResultsPositions {
		if r.CarIdx == i.DriverInfo.DriverCarIdx {
			return r, true
		}
	}
	return ResultPosition{}, false
}

// MyQualifyResult returns the local driver's qualifying result.
func (i *Info) MyQualifyResult() (QualifyResult, bool) {
	for _, q := range i.QualifyResultsInfo.Results {
		if q.CarIdx == i.DriverInfo.DriverCarIdx {
			return q, true
		}
	}
	return QualifyResult{}, false
}

// FieldSize returns how many cars were classified in a session, falling back to
// the count of non-spectator drivers before results exist.
//
// Position without field size is misleading: P5 of 6 is not P5 of 40.
func (i *Info) FieldSize(sessionNum int) int {
	if s, ok := i.SessionByNum(sessionNum); ok && len(s.ResultsPositions) > 0 {
		return len(s.ResultsPositions)
	}
	n := 0
	for _, d := range i.DriverInfo.Drivers {
		if d.IsSpectator == 0 {
			n++
		}
	}
	return n
}
