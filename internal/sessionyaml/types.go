// Package sessionyaml parses the subset of iRacing's session-info YAML string
// that LapDog needs. Unknown and missing keys are tolerated, because the sim
// adds fields over time and the set present depends on the session.
package sessionyaml

// Info is the parsed session-info document.
type Info struct {
	WeekendInfo        Weekend        `yaml:"WeekendInfo" json:"WeekendInfo"`
	SessionInfo        Sessions       `yaml:"SessionInfo" json:"SessionInfo"`
	QualifyResultsInfo QualifyResults `yaml:"QualifyResultsInfo" json:"QualifyResultsInfo"`
	DriverInfo         Drivers        `yaml:"DriverInfo" json:"DriverInfo"`
}

// Weekend holds the WeekendInfo section, which carries the identity and context
// fields the classifier depends on.
type Weekend struct {
	TrackName             string `yaml:"TrackName" json:"TrackName"`
	TrackID               int    `yaml:"TrackID" json:"TrackID"`
	TrackDisplayName      string `yaml:"TrackDisplayName" json:"TrackDisplayName"`
	TrackDisplayShortName string `yaml:"TrackDisplayShortName" json:"TrackDisplayShortName"`
	TrackConfigName       string `yaml:"TrackConfigName" json:"TrackConfigName"`

	// TrackLength is a string because the sim emits "7.00 km".
	TrackLength string `yaml:"TrackLength" json:"TrackLength"`

	SeriesID     int `yaml:"SeriesID" json:"SeriesID"`
	SeasonID     int `yaml:"SeasonID" json:"SeasonID"`
	SessionID    int `yaml:"SessionID" json:"SessionID"`
	SubSessionID int `yaml:"SubSessionID" json:"SubSessionID"`
	LeagueID     int `yaml:"LeagueID" json:"LeagueID"`
	Official     int `yaml:"Official" json:"Official"`
	RaceWeek     int `yaml:"RaceWeek" json:"RaceWeek"`

	EventType  string `yaml:"EventType" json:"EventType"`
	Category   string `yaml:"Category" json:"Category"`
	SimMode    string `yaml:"SimMode" json:"SimMode"`
	TeamRacing int    `yaml:"TeamRacing" json:"TeamRacing"`
}

// Sessions holds the SessionInfo section.
type Sessions struct {
	NumSessions int       `yaml:"NumSessions" json:"NumSessions"`
	Sessions    []Session `yaml:"Sessions" json:"Sessions"`
}

// Session is one entry in SessionInfo.Sessions.
type Session struct {
	SessionNum  int    `yaml:"SessionNum" json:"SessionNum"`
	SessionType string `yaml:"SessionType" json:"SessionType"`

	// SessionLaps and SessionTime are strings because the sim emits
	// "unlimited" and "900.0000 sec".
	SessionLaps string `yaml:"SessionLaps" json:"SessionLaps"`
	SessionTime string `yaml:"SessionTime" json:"SessionTime"`

	ResultsOfficial     int              `yaml:"ResultsOfficial" json:"ResultsOfficial"`
	ResultsLapsComplete int              `yaml:"ResultsLapsComplete" json:"ResultsLapsComplete"`
	ResultsPositions    []ResultPosition `yaml:"ResultsPositions" json:"ResultsPositions"`
}

// ResultPosition is one car's classified result in a session. These fields only
// populate as the session concludes.
type ResultPosition struct {
	Position      int     `yaml:"Position" json:"Position"`
	ClassPosition int     `yaml:"ClassPosition" json:"ClassPosition"`
	CarIdx        int     `yaml:"CarIdx" json:"CarIdx"`
	Lap           int     `yaml:"Lap" json:"Lap"`
	Time          float64 `yaml:"Time" json:"Time"`
	FastestLap    int     `yaml:"FastestLap" json:"FastestLap"`
	FastestTime   float64 `yaml:"FastestTime" json:"FastestTime"`
	LapsComplete  int     `yaml:"LapsComplete" json:"LapsComplete"`
	Incidents     int     `yaml:"Incidents" json:"Incidents"`
	ReasonOutId   int     `yaml:"ReasonOutId" json:"ReasonOutId"`
}

// QualifyResults holds the QualifyResultsInfo section, which is the
// authoritative qualifying result and only populates once qualifying has run.
type QualifyResults struct {
	Results []QualifyResult `yaml:"Results" json:"Results"`
}

// QualifyResult is one car's qualifying result.
type QualifyResult struct {
	Position      int     `yaml:"Position" json:"Position"`
	ClassPosition int     `yaml:"ClassPosition" json:"ClassPosition"`
	CarIdx        int     `yaml:"CarIdx" json:"CarIdx"`
	FastestLap    int     `yaml:"FastestLap" json:"FastestLap"`
	FastestTime   float64 `yaml:"FastestTime" json:"FastestTime"`
}

// Drivers holds the DriverInfo section.
type Drivers struct {
	DriverCarIdx        int      `yaml:"DriverCarIdx" json:"DriverCarIdx"`
	DriverCarEstLapTime float64  `yaml:"DriverCarEstLapTime" json:"DriverCarEstLapTime"`
	Drivers             []Driver `yaml:"Drivers" json:"Drivers"`
}

// Driver is one entry in DriverInfo.Drivers.
//
// CarIsAI is a pointer so that "absent" is distinguishable from "present and
// zero". The field is unverified against the bundled SDK documentation — see the
// design spec, section 6.5 — and that distinction is what lets the classifier
// know whether to trust it or fall back to its heuristic.
type Driver struct {
	CarIdx             int    `yaml:"CarIdx" json:"CarIdx"`
	UserName           string `yaml:"UserName" json:"UserName"`
	UserID             int    `yaml:"UserID" json:"UserID"`
	CarID              int    `yaml:"CarID" json:"CarID"`
	CarPath            string `yaml:"CarPath" json:"CarPath"`
	CarScreenName      string `yaml:"CarScreenName" json:"CarScreenName"`
	CarScreenNameShort string `yaml:"CarScreenNameShort" json:"CarScreenNameShort"`
	CarClassID         int    `yaml:"CarClassID" json:"CarClassID"`
	CarClassShortName  string `yaml:"CarClassShortName" json:"CarClassShortName"`
	IRating            int    `yaml:"IRating" json:"IRating"`
	LicString          string `yaml:"LicString" json:"LicString"`
	IsSpectator        int    `yaml:"IsSpectator" json:"IsSpectator"`
	CarIsAI            *int   `yaml:"CarIsAI" json:"CarIsAI,omitempty"`
}
