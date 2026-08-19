package sessionyaml

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"gopkg.in/yaml.v3"
)

// Parse decodes a session-info YAML document. Unknown keys are ignored.
func Parse(b []byte) (*Info, error) {
	// iRacing's session-info buffer is not consistently UTF-8. Real driver names
	// with accented characters have arrived as Windows-1252 bytes (for example,
	// e9 for é), which yaml.v3 correctly rejects as invalid UTF-8. ASCII and valid
	// UTF-8 pass through byte-for-byte; only an invalid document is decoded from
	// the Windows character set used by the simulator on Windows.
	if !utf8.Valid(b) {
		decoded, err := charmap.Windows1252.NewDecoder().Bytes(b)
		if err != nil {
			return nil, fmt.Errorf("sessionyaml: decode Windows-1252: %w", err)
		}
		b = decoded
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errors.New("sessionyaml: empty document")
	}
	var i Info
	if err := yaml.Unmarshal(b, &i); err != nil {
		// iRacing can replace an unavailable driver's name with the unquoted
		// placeholder "? ?". At the start of a plain YAML scalar, "? " is
		// mapping syntax rather than text, so one opponent's placeholder makes
		// the simulator's whole document invalid. Quote only that observed shape
		// on simulator-owned identity fields, then retry. Other parse failures
		// remain failures instead of being hidden by a general-purpose repair.
		if repaired, changed := quoteIdentityPlaceholders(b); changed {
			if retryErr := yaml.Unmarshal(repaired, &i); retryErr == nil {
				return &i, nil
			}
		}
		return nil, fmt.Errorf("sessionyaml: unmarshal: %w", err)
	}
	return &i, nil
}

func quoteIdentityPlaceholders(b []byte) ([]byte, bool) {
	lines := strings.Split(string(b), "\n")
	changed := false
	for n, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		for _, key := range []string{"UserName", "AbbrevName", "Initials", "TeamName"} {
			prefix := key + ": "
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			value := strings.TrimPrefix(trimmed, prefix)
			if value != "?" && !strings.HasPrefix(value, "? ") {
				break
			}
			indent := line[:len(line)-len(trimmed)]
			lines[n] = indent + prefix + strconv.Quote(value)
			changed = true
			break
		}
	}
	return []byte(strings.Join(lines, "\n")), changed
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

// Identity is who the local driver is and where their ratings stood.
//
// Every field is a pointer because absent and zero are different facts here. An
// iRating of zero is a real value for an unrated licence, and a session driven
// offline may carry no rating at all — reporting either as 0 would invent a number.
type Identity struct {
	UserID       *int
	IRating      *int
	LicString    *string
	LicLevel     *int
	LicSubLevel  *int
	SafetyRating *float64
}

// MyIdentity returns the local driver's identity and ratings.
//
// The customer id prefers DriverInfo.DriverUserID, which states it directly, and
// falls back to the drivers array entry. Both are present in practice; the direct
// field cannot be wrong if the array is reordered, and the fallback covers a document
// that omits it.
func (i *Info) MyIdentity() Identity {
	var id Identity
	if i == nil {
		return id
	}

	if i.DriverInfo.DriverUserID != 0 {
		v := i.DriverInfo.DriverUserID
		id.UserID = &v
	}

	me, ok := i.Me()
	if !ok {
		return id
	}
	if id.UserID == nil && me.UserID != 0 {
		v := me.UserID
		id.UserID = &v
	}
	// Ratings are read only from an online session.
	//
	// Offline sessions — AI races, offline testing, solo practice — do not carry the
	// driver's real ratings. A real capture reported IRating 1, LicLevel 1, LicSubLevel 1
	// and LicString "R 0.01" for an established account, and those values were stored as
	// though observed: on the progression chart they read as a collapse to nothing, a
	// fabricated cliff in the one chart whose purpose is showing how the ratings moved.
	//
	// Absent is the honest answer and it costs nothing downstream: store.Ratings emits a
	// point only where a rating is non-NULL, so an offline session simply does not appear.
	//
	// The identity itself is kept either way. The customer id is correct offline — it is
	// the account that drove — and it is what says whose data a database holds.
	if !i.isOnline() {
		return id
	}

	// iRating is taken even when zero, because zero is a licence state rather than a
	// missing reading — but only once a driver entry was actually found.
	ir := me.IRating
	id.IRating = &ir
	if me.LicString != "" {
		ls := me.LicString
		id.LicString = &ls
	}
	if me.LicLevel != 0 {
		v := me.LicLevel
		id.LicLevel = &v
	}
	if me.LicSubLevel != 0 {
		v := me.LicSubLevel
		id.LicSubLevel = &v
	}
	if sr, ok := SafetyRating(me); ok {
		id.SafetyRating = &sr
	}
	return id
}

// isOnline reports whether the document describes a session the iRacing service
// registered, as opposed to one driven offline.
//
// SubSessionID is the marker, and it is structural rather than a heuristic: the service
// allocates it, so an offline session has none. A real offline capture shows
// SubSessionID 0 alongside Official 0 and Unofficial 1, and the generator models the
// same thing — FlavourAI and FlavourOfflineTest set it to zero.
//
// Official alone would be the wrong test: a hosted or league session is online but not
// official, and reports real ratings.
func (i *Info) isOnline() bool { return i.WeekendInfo.SubSessionID != 0 }

// SafetyRating returns the driver's Safety Rating as a number.
//
// Two sources state it and they are read in a deliberate order. LicString is the
// string the simulator shows the driver — "A 3.55" — so the number in it is the one
// they would recognise, and it is preferred. LicSubLevel carries the same value
// scaled by a hundred and is used when the string is absent or unparseable.
//
// Reading only LicSubLevel would be simpler and is what the field appears to be for,
// but it would report a value the driver has never seen if the two ever disagree.
func SafetyRating(d Driver) (float64, bool) {
	if d.LicString != "" {
		// The class is a letter and the rating follows it: "A 3.55", "R 2.50".
		if f := strings.LastIndexByte(d.LicString, ' '); f >= 0 {
			if v, err := strconv.ParseFloat(strings.TrimSpace(d.LicString[f+1:]), 64); err == nil {
				return v, true
			}
		}
	}
	if d.LicSubLevel != 0 {
		return float64(d.LicSubLevel) / 100.0, true
	}
	return 0, false
}
