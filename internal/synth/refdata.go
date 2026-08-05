package synth

// Reference data for the synthetic world.
//
// Track and car names are real so the generated dataset reads naturally in the
// UI. The numeric IDs are synthetic and internally consistent but are not
// iRacing's actual IDs — nothing in LapDog depends on them matching, and
// inventing them avoids asserting facts that were never verified.

// Track is a circuit the synthetic driver visits.
type Track struct {
	ID       int
	Name     string
	Config   string
	LengthKm float64
	// BaseLapS is a reference lap time in seconds for a GT3-class car. Other
	// classes scale it by their PaceFactor.
	BaseLapS float64
	// PitLossS is the time cost of a pit stop, used to make in and out laps
	// visibly slower than a clean lap.
	PitLossS float64
}

// Tracks is the rotation the synthetic seasons draw from.
var Tracks = []Track{
	{ID: 18, Name: "Watkins Glen International", Config: "Boot", LengthKm: 5.43, BaseLapS: 118.4, PitLossS: 26},
	{ID: 341, Name: "Circuit de Spa-Francorchamps", Config: "Grand Prix Pits", LengthKm: 7.00, BaseLapS: 141.9, PitLossS: 30},
	{ID: 219, Name: "Road America", Config: "Full Course", LengthKm: 6.51, BaseLapS: 133.2, PitLossS: 28},
	{ID: 145, Name: "Circuit of the Americas", Config: "Grand Prix", LengthKm: 5.51, BaseLapS: 128.6, PitLossS: 27},
	{ID: 97, Name: "Autodromo Nazionale Monza", Config: "Grand Prix", LengthKm: 5.79, BaseLapS: 111.7, PitLossS: 25},
	{ID: 262, Name: "Suzuka International Racing Course", Config: "Grand Prix", LengthKm: 5.81, BaseLapS: 125.3, PitLossS: 29},
	{ID: 407, Name: "Nürburgring Nordschleife", Config: "Industriefahrten", LengthKm: 20.83, BaseLapS: 480.5, PitLossS: 40},
	{ID: 158, Name: "Brands Hatch Circuit", Config: "Grand Prix", LengthKm: 3.90, BaseLapS: 87.9, PitLossS: 22},
	{ID: 273, Name: "Mount Panorama Circuit", Config: "", LengthKm: 6.21, BaseLapS: 124.8, PitLossS: 30},
	{ID: 122, Name: "Sebring International Raceway", Config: "International", LengthKm: 6.02, BaseLapS: 137.1, PitLossS: 28},
	{ID: 199, Name: "Okayama International Circuit", Config: "Full", LengthKm: 3.70, BaseLapS: 97.4, PitLossS: 24},
	{ID: 311, Name: "Lime Rock Park", Config: "Grand Prix", LengthKm: 2.44, BaseLapS: 61.8, PitLossS: 20},
	{ID: 244, Name: "Virginia International Raceway", Config: "Full", LengthKm: 5.26, BaseLapS: 122.7, PitLossS: 26},
	{ID: 288, Name: "Circuit Zandvoort", Config: "Grand Prix", LengthKm: 4.26, BaseLapS: 100.9, PitLossS: 24},
	{ID: 176, Name: "Silverstone Circuit", Config: "Grand Prix", LengthKm: 5.89, BaseLapS: 124.1, PitLossS: 27},
	{ID: 133, Name: "Long Beach Street Circuit", Config: "", LengthKm: 3.17, BaseLapS: 83.6, PitLossS: 23},
}

// Car is a car the synthetic driver runs.
type Car struct {
	ID        int
	Name      string
	ShortName string
	Path      string
	ClassID   int
	ClassName string
	// PaceFactor scales a track's GT3 reference lap. Above 1 is slower.
	PaceFactor float64
	// FuelCapacityL and FuelPerLapL drive the fuel channel.
	FuelCapacityL float64
	FuelPerLapL   float64
	// RedlineRPM shapes the RPM channel.
	RedlineRPM float64
}

// Cars is the garage the synthetic driver picks from.
var Cars = []Car{
	{ID: 173, Name: "Porsche 911 GT3 R", ShortName: "Porsche 911 GT3 R", Path: "porsche991rgt3",
		ClassID: 2523, ClassName: "GT3", PaceFactor: 1.00, FuelCapacityL: 105, FuelPerLapL: 3.1, RedlineRPM: 9200},
	{ID: 132, Name: "Mazda MX-5 Cup", ShortName: "MX-5 Cup", Path: "mx5 mx52016",
		ClassID: 74, ClassName: "MX-5", PaceFactor: 1.42, FuelCapacityL: 45, FuelPerLapL: 1.4, RedlineRPM: 7200},
	{ID: 148, Name: "BMW M4 GT4", ShortName: "BMW M4 GT4", Path: "bmwm4gt4",
		ClassID: 2708, ClassName: "GT4", PaceFactor: 1.13, FuelCapacityL: 90, FuelPerLapL: 2.6, RedlineRPM: 7600},
	{ID: 105, Name: "Dallara F3", ShortName: "Dallara F3", Path: "dallaraf312",
		ClassID: 1131, ClassName: "F3", PaceFactor: 0.88, FuelCapacityL: 50, FuelPerLapL: 2.2, RedlineRPM: 12000},
	{ID: 191, Name: "Ferrari 296 GT3", ShortName: "Ferrari 296 GT3", Path: "ferrari296gt3",
		ClassID: 2523, ClassName: "GT3", PaceFactor: 0.99, FuelCapacityL: 105, FuelPerLapL: 3.0, RedlineRPM: 8000},
	{ID: 67, Name: "Ray FF1600", ShortName: "Ray FF1600", Path: "rayff1600",
		ClassID: 871, ClassName: "FF1600", PaceFactor: 1.31, FuelCapacityL: 40, FuelPerLapL: 1.2, RedlineRPM: 6800},
}

// Series is an official series the synthetic driver competes in.
type Series struct {
	ID       int
	Name     string
	CarIndex int
	// RaceLaps is the scheduled race distance.
	RaceLaps int
}

// SeriesList are the official series in the synthetic world.
var SeriesList = []Series{
	{ID: 411, Name: "GT3 Sprint Series", CarIndex: 0, RaceLaps: 18},
	{ID: 289, Name: "Global MX-5 Cup", CarIndex: 1, RaceLaps: 14},
	{ID: 355, Name: "GT4 Falken Challenge", CarIndex: 2, RaceLaps: 16},
	{ID: 178, Name: "Formula 3 Championship", CarIndex: 3, RaceLaps: 20},
}

// League is a private league the synthetic driver races in.
type League struct {
	ID       int
	Name     string
	CarIndex int
	RaceLaps int
	// Weekday is the day of week the league races, using time.Weekday values.
	Weekday int
}

// Leagues are the leagues in the synthetic world. Neither races on Sunday.
var Leagues = []League{
	{ID: 4242, Name: "Thursday Night GT3", CarIndex: 0, RaceLaps: 26, Weekday: 4},
	{ID: 5107, Name: "Tuesday Formula Sprint", CarIndex: 3, RaceLaps: 22, Weekday: 2},
}

// Opponent is another driver in the field.
type Opponent struct {
	Name    string
	UserID  int
	IRating int
	IsAI    bool
}

// opponentNames are used for both human and AI fields. AI entries in real
// iRacing carry ordinary-looking names too, so no attempt is made to make them
// obviously artificial.
var opponentNames = []string{
	"A. Lindqvist", "B. Okonkwo", "C. Ferreira", "D. Nakamura", "E. Vasquez",
	"F. Brennan", "G. Aaltonen", "H. Marchetti", "I. Petrov", "J. Delacroix",
	"K. Sørensen", "L. Anand", "M. Kowalski", "N. Bergström", "O. Castellano",
	"P. Halvorsen", "Q. Ibarra", "R. Tanaka", "S. Whitfield", "T. Novák",
	"U. Steenkamp", "V. Moreau", "W. Kirkpatrick", "X. Zhou", "Y. Abadi",
	"Z. Ravensworth", "A. Thornbury", "B. Castellanos", "C. Lindgren", "D. Osei",
	"E. Fairweather", "F. Mikkelsen", "G. Rasmussen", "H. Oyelaran", "I. Duchamp",
	"J. Baptiste", "K. Wilhelmsen", "L. Marchand", "M. Sandoval", "N. Achterberg",
}

// DriverName is the synthetic local driver.
const DriverName = "Dan Blezek"

// DriverUserID is the local driver's customer ID.
const DriverUserID = 271828
