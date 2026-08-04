package synth

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// PollIntervalS is the rate frames are emitted at, matching the application's
// default poll interval. Captures record what the collector polled, not what
// the simulator wrote, so this is the correct cadence.
const PollIntervalS = 1.0

// writer carries the per-capture state while a weekend is simulated.
type writer struct {
	cap    *capture.Writer
	rb     *irsdk.RowBuilder
	rng    *rand.Rand
	t      float64 // monotonic seconds since capture start
	tick   uint32
	update uint32
}

// emit writes the current row at the current capture time and advances.
func (w *writer) emit() error {
	w.tick += uint32(PollIntervalS * TickRate)
	if err := w.cap.WriteVars(w.t, w.tick, w.rb.Bytes()); err != nil {
		return err
	}
	w.t += PollIntervalS
	return nil
}

// yaml writes a session-info blob, bumping the update counter the way the sim
// does when the document changes.
func (w *writer) yaml(text string) error {
	w.update++
	return w.cap.WriteSession(w.t, w.update, []byte(text))
}

// carState tracks one opponent through a race.
type carState struct {
	carIdx    int
	onPitRoad bool
	retired   bool
	// pitUntil is the capture time pit road occupancy ends.
	pitUntil float64
	lap      int
	distPct  float64
}

// SimulateWeekend writes one weekend to a capture file.
//
// One file covers the whole weekend rather than one file per session segment.
// The application itself rotates per segment, but a multi-session capture is
// strictly more useful as a fixture: it exercises the SessionNum transition, the
// YAML evolving as results populate, and classification of practice inside a
// race weekend — none of which a single-segment file can reach.
func SimulateWeekend(w *Weekend, path string, seed int64) error {
	vh, bufLen := Layout()
	meta := capture.Meta{
		TickRate:   TickRate,
		NumVars:    int32(len(vh)),
		BufLen:     bufLen,
		VarHeaders: vh,
	}
	cw, err := capture.NewWriter(path, meta)
	if err != nil {
		return err
	}
	defer cw.Close()

	sw := &writer{
		cap: cw,
		rb:  irsdk.NewRowBuilder(vh, bufLen),
		rng: rand.New(rand.NewSource(seed)),
	}

	// The opening document has no results anywhere, which is what the sim
	// publishes when a weekend begins.
	if err := sw.yaml(renderYAML(w, -1)); err != nil {
		return err
	}

	for i := range w.Sessions {
		if err := simulateSession(sw, w, i); err != nil {
			return fmt.Errorf("synth: session %d: %w", i, err)
		}
		// Results appear only once the session concludes. Publishing them here,
		// then emitting a few more frames while SessionNum is unchanged, is what
		// lets the collector attribute them before it closes the segment.
		if err := sw.yaml(renderYAML(w, i)); err != nil {
			return err
		}
		if err := coolDown(sw, w, i, 8+sw.rng.Intn(12)); err != nil {
			return err
		}
	}
	return cw.Close()
}

// coolDown emits garage frames after a session's results are published.
//
// The incident counter is carried through these frames. The real simulator does
// not zero it when the driver returns to the garage, and emitting zero here would
// invite a consumer to treat the drop as incidents being forgiven.
func coolDown(sw *writer, w *Weekend, sessionIdx, frames int) error {
	s := &w.Sessions[sessionIdx]

	incidents := 0
	for _, r := range s.Results {
		if r.CarIdx == w.DriverCarIdx {
			incidents = r.Incidents
			break
		}
	}

	for i := 0; i < frames; i++ {
		sw.rb.Reset()
		if err := setBase(sw, w, s, 0); err != nil {
			return err
		}
		if err := sw.rb.SetBool("IsInGarage", true); err != nil {
			return err
		}
		if err := sw.rb.SetInt("SessionState", int32(irsdk.StateCoolDown)); err != nil {
			return err
		}
		if err := sw.rb.SetInt("PlayerCarMyIncidentCount", int32(incidents)); err != nil {
			return err
		}
		if err := setAllCars(sw, w, nil, nil, irsdk.InPitStall); err != nil {
			return err
		}
		if err := sw.emit(); err != nil {
			return err
		}
	}
	return nil
}

// simulateSession emits every frame of one session segment.
func simulateSession(sw *writer, w *Weekend, sessionIdx int) error {
	s := &w.Sessions[sessionIdx]
	isRace := isRaceType(s.RawType)
	lapBase := w.LapSeconds()

	// Session-local clock, which the sim resets between sessions.
	sessionT := 0.0

	// Garage: connected but not in the car. Only connected time accrues.
	garageFrames := int(s.GarageSeconds / PollIntervalS)
	for i := 0; i < garageFrames; i++ {
		sw.rb.Reset()
		if err := setBase(sw, w, s, sessionT); err != nil {
			return err
		}
		if err := sw.rb.SetBool("IsInGarage", true); err != nil {
			return err
		}
		if err := sw.rb.SetInt("SessionState", int32(irsdk.StateGetInCar)); err != nil {
			return err
		}
		if err := setAllCars(sw, w, nil, nil, irsdk.InPitStall); err != nil {
			return err
		}
		if err := sw.emit(); err != nil {
			return err
		}
		sessionT += PollIntervalS
	}

	// Field state for the race, and the driver's running position.
	cars := make([]carState, 0, len(w.Opponents))
	for i := range w.Opponents {
		cars = append(cars, carState{carIdx: w.carIdxFor(i)})
	}
	order := buildStartOrder(w, sessionIdx)
	driverPos := indexOf(order, w.DriverCarIdx) + 1
	// The grid slot is the position the driver's pace justifies, and is the target
	// the in-race drift model pulls toward.
	startPos := driverPos

	// Pit box before the green: in the car, physics live, but not driving.
	pitFrames := int(s.PitSeconds / PollIntervalS)
	fuel := w.Car.FuelCapacityL * (0.55 + sw.rng.Float64()*0.4)
	incidents := 0
	bestLap := 0.0
	bestLapNum := 0
	lapNum := int32(1)

	for i := 0; i < pitFrames; i++ {
		sw.rb.Reset()
		if err := setBase(sw, w, s, sessionT); err != nil {
			return err
		}
		if err := setInCar(sw, w, s, fuel, lapNum, 0, 0, bestLap, bestLapNum, incidents, driverPos, true); err != nil {
			return err
		}
		if err := setAllCars(sw, w, cars, order, irsdk.InPitStall); err != nil {
			return err
		}
		if err := sw.emit(); err != nil {
			return err
		}
		sessionT += PollIntervalS
	}

	// Timed laps.
	lapTimes := make([]float64, 0, s.Laps)
	pitEvery := 0
	if isRace && s.Laps > 14 {
		pitEvery = s.Laps / 2
	}

	for lap := 0; lap < s.Laps; lap++ {
		isInLap := pitEvery > 0 && (lap+1)%pitEvery == 0 && lap+1 < s.Laps

		// Lap time: reference pace, small scatter, plus a pit-loss penalty on
		// the lap the driver enters the pits.
		lapTime := lapBase * (1 + (sw.rng.Float64()-0.5)*0.018)
		if lap == 0 {
			lapTime *= 1.06 // out lap on cold tyres
		}
		if isInLap {
			lapTime += w.Track.PitLossS
		}

		frames := int(math.Ceil(lapTime / PollIntervalS))
		for f := 0; f < frames; f++ {
			pct := float64(f) / float64(frames)
			onPit := isInLap && pct > 0.93

			// Incidents accrue at the weekend's rate while driving.
			if sw.rng.Float64() < w.IncidentRatePerHour/3600.0*PollIntervalS {
				incidents += 1 + sw.rng.Intn(2)
			}

			fuel -= w.Car.FuelPerLapL / float64(frames)
			if fuel < 1 {
				fuel = w.Car.FuelCapacityL * 0.9 // refuelled in the pits
			}

			state := irsdk.StateRacing
			if isRace && lap == 0 && pct < 0.4 {
				state = irsdk.StateParadeLaps
			}
			if !isRace {
				state = irsdk.StateWarmup
			}

			sw.rb.Reset()
			if err := setBase(sw, w, s, sessionT); err != nil {
				return err
			}
			if err := sw.rb.SetInt("SessionState", int32(state)); err != nil {
				return err
			}
			if err := setInCar(sw, w, s, fuel, lapNum, pct, lapTime*pct, bestLap, bestLapNum, incidents, driverPos, onPit); err != nil {
				return err
			}

			surface := irsdk.OnTrack
			if onPit {
				surface = irsdk.ApproachingPits
			}
			if err := sw.rb.SetIntAt("CarIdxTrackSurface", w.DriverCarIdx, int32(surface)); err != nil {
				return err
			}

			// Field movement, and the position swaps that produce the pass/passed
			// record.
			//
			// The driver's position is re-derived from the running order rather than
			// tracked alongside it. Both advanceField and maybeSwap may reorder the
			// field, and keeping a separate counter in step with them proved to be a
			// source of silent misattribution.
			if isRace {
				advanceField(sw, w, cars, order, lap, pct)
				maybeSwap(sw, w, cars, order, driverPos, startPos)
				driverPos = indexOf(order, w.DriverCarIdx) + 1
			}
			if err := setAllCars(sw, w, cars, order, surface); err != nil {
				return err
			}
			if err := sw.rb.SetInt("PlayerCarPosition", int32(driverPos)); err != nil {
				return err
			}
			if err := sw.rb.SetInt("PlayerCarClassPosition", int32(driverPos)); err != nil {
				return err
			}

			if err := sw.emit(); err != nil {
				return err
			}
			sessionT += PollIntervalS
		}

		// Cross the line: the counter advances and the completed lap's time
		// becomes LapLastLapTime on the following frames.
		lapTimes = append(lapTimes, lapTime)
		if bestLap == 0 || lapTime < bestLap {
			bestLap = lapTime
			bestLapNum = int(lapNum)
		}
		lapNum++

		// One frame carrying the completed lap time, which is the signal the
		// lap detector keys on.
		sw.rb.Reset()
		if err := setBase(sw, w, s, sessionT); err != nil {
			return err
		}
		if err := setInCar(sw, w, s, fuel, lapNum, 0.01, 0, bestLap, bestLapNum, incidents, driverPos, false); err != nil {
			return err
		}
		if err := sw.rb.SetFloat("LapLastLapTime", lapTime); err != nil {
			return err
		}
		if err := setAllCars(sw, w, cars, order, irsdk.OnTrack); err != nil {
			return err
		}
		if err := sw.emit(); err != nil {
			return err
		}
		sessionT += PollIntervalS

		// Pit box occupancy after an in lap: in the car, not driving.
		if isInLap {
			stop := 18 + sw.rng.Float64()*14
			for i := 0; i < int(stop/PollIntervalS); i++ {
				sw.rb.Reset()
				if err := setBase(sw, w, s, sessionT); err != nil {
					return err
				}
				if err := setInCar(sw, w, s, fuel, lapNum, 0.02, 0, bestLap, bestLapNum, incidents, driverPos, true); err != nil {
					return err
				}
				if err := sw.rb.SetIntAt("CarIdxTrackSurface", w.DriverCarIdx, int32(irsdk.InPitStall)); err != nil {
					return err
				}
				if err := setAllCars(sw, w, cars, order, irsdk.InPitStall); err != nil {
					return err
				}
				if err := sw.emit(); err != nil {
					return err
				}
				sessionT += PollIntervalS
			}
		}
	}

	// Replay viewing, which must contribute to no counter at all.
	for i := 0; i < int(s.ReplaySeconds/PollIntervalS); i++ {
		sw.rb.Reset()
		if err := setBase(sw, w, s, sessionT); err != nil {
			return err
		}
		if err := sw.rb.SetBool("IsReplayPlaying", true); err != nil {
			return err
		}
		if err := sw.rb.SetBool("IsOnTrackCar", true); err != nil {
			return err
		}
		if err := setAllCars(sw, w, cars, order, irsdk.OnTrack); err != nil {
			return err
		}
		if err := sw.emit(); err != nil {
			return err
		}
		sessionT += PollIntervalS
	}

	recordResults(w, sessionIdx, order, driverPos, lapTimes, incidents, bestLap, bestLapNum)
	return nil
}

// setBase writes the variables present on every frame regardless of state.
func setBase(sw *writer, w *Weekend, s *Session, sessionT float64) error {
	rb := sw.rb
	for _, err := range []error{
		rb.SetInt("SessionNum", int32(s.Num)),
		rb.SetFloat("SessionTime", sessionT),
		rb.SetFloat("SessionTimeRemain", remaining(s, sessionT)),
		rb.SetInt("SessionLapsRemain", int32(lapsRemaining(s))),
		rb.SetInt("SessionUniqueID", int32(w.SubSessionID%1000000)),
		rb.SetBitField("SessionFlags", 0x00000004), // green
		rb.SetFloat("AirTemp", w.AirTempC),
		rb.SetFloat("TrackTempCrew", w.TrackTempC),
		rb.SetInt("SessionState", int32(irsdk.StateWarmup)),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// setInCar writes the driver-in-car channels.
func setInCar(
	sw *writer, w *Weekend, s *Session,
	fuel float64, lapNum int32, pct, lapElapsed, bestLap float64, bestLapNum, incidents, pos int,
	onPitRoad bool,
) error {
	rb := sw.rb
	speed := 0.0
	rpm := w.Car.RedlineRPM * 0.12
	gear := int32(0)
	throttle := 0.0
	brake := 1.0
	if !onPitRoad {
		// A crude but plausible speed trace: fast on the straights, slower
		// through the middle of the lap.
		speed = 38 + 26*math.Abs(math.Sin(pct*math.Pi*3))
		rpm = w.Car.RedlineRPM * (0.55 + 0.4*math.Abs(math.Sin(pct*math.Pi*3)))
		gear = int32(2 + int(4*math.Abs(math.Sin(pct*math.Pi*3))))
		throttle = 0.35 + 0.65*math.Abs(math.Sin(pct*math.Pi*3))
		brake = 0
	}
	for _, err := range []error{
		rb.SetBool("IsOnTrack", !onPitRoad),
		rb.SetBool("IsOnTrackCar", true),
		rb.SetBool("OnPitRoad", onPitRoad),
		rb.SetInt("Lap", lapNum),
		rb.SetInt("LapCompleted", lapNum-1),
		rb.SetFloat("LapCurrentLapTime", lapElapsed),
		rb.SetFloat("LapBestLapTime", bestLap),
		rb.SetInt("LapBestLap", int32(bestLapNum)),
		rb.SetFloat("LapDist", pct*w.Track.LengthKm*1000),
		rb.SetFloat("LapDistPct", pct),
		rb.SetFloat("FuelLevel", fuel),
		rb.SetFloat("FuelLevelPct", fuel/w.Car.FuelCapacityL),
		rb.SetFloat("FuelUsePerHour", w.Car.FuelPerLapL*3600/w.LapSeconds()),
		rb.SetFloat("Speed", speed),
		rb.SetFloat("RPM", rpm),
		rb.SetInt("Gear", gear),
		rb.SetFloat("Throttle", throttle),
		rb.SetFloat("Brake", brake),
		rb.SetFloat("SteeringWheelAngle", math.Sin(pct*math.Pi*7)*0.6),
		rb.SetInt("PlayerCarPosition", int32(pos)),
		rb.SetInt("PlayerCarClassPosition", int32(pos)),
		rb.SetInt("PlayerCarMyIncidentCount", int32(incidents)),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// setAllCars writes the per-car arrays, including the driver's own slot.
//
// order carries the field in position order, which is what makes the per-car
// position array meaningful. Without it every CarIdxPosition would be zero and no
// position change could ever be attributed to an opponent — the ingestion path
// would record every swap as cause Unknown.
func setAllCars(sw *writer, w *Weekend, cars []carState, order []int, driverSurface irsdk.TrkLoc) error {
	rb := sw.rb
	// Everything not in the field is out of the world, which is what the sim
	// reports for unused car indices.
	for i := 0; i < MaxCars; i++ {
		if err := rb.SetIntAt("CarIdxTrackSurface", i, int32(irsdk.NotInWorld)); err != nil {
			return err
		}
		if err := rb.SetIntAt("CarIdxPosition", i, 0); err != nil {
			return err
		}
		if err := rb.SetIntAt("CarIdxClassPosition", i, 0); err != nil {
			return err
		}
	}
	// Position is one-based and follows the running order.
	for pos, carIdx := range order {
		if carIdx < 0 || carIdx >= MaxCars {
			continue
		}
		if err := rb.SetIntAt("CarIdxPosition", carIdx, int32(pos+1)); err != nil {
			return err
		}
		if err := rb.SetIntAt("CarIdxClassPosition", carIdx, int32(pos+1)); err != nil {
			return err
		}
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", w.DriverCarIdx, int32(driverSurface)); err != nil {
		return err
	}
	for _, c := range cars {
		surface := irsdk.OnTrack
		switch {
		case c.retired:
			surface = irsdk.NotInWorld
		case c.onPitRoad:
			surface = irsdk.InPitStall
		}
		if err := rb.SetIntAt("CarIdxTrackSurface", c.carIdx, int32(surface)); err != nil {
			return err
		}
		if err := rb.SetBoolAt("CarIdxOnPitRoad", c.carIdx, c.onPitRoad); err != nil {
			return err
		}
		if err := rb.SetIntAt("CarIdxLap", c.carIdx, int32(c.lap)); err != nil {
			return err
		}
		if err := rb.SetFloatAt("CarIdxLapDistPct", c.carIdx, c.distPct); err != nil {
			return err
		}
	}
	return nil
}

// remaining returns SessionTimeRemain, using the sim's unlimited sentinel when
// the session has no time limit.
func remaining(s *Session, sessionT float64) float64 {
	if s.TimeLimitS <= 0 {
		return irsdk.UnlimitedTime
	}
	r := s.TimeLimitS - sessionT
	if r < 0 {
		return 0
	}
	return r
}

// lapsRemaining returns SessionLapsRemain, using the unlimited sentinel when
// the session has no lap limit.
func lapsRemaining(s *Session) int {
	if s.LapLimit <= 0 {
		return irsdk.UnlimitedLaps
	}
	return s.LapLimit
}

// indexOf returns the index of v in xs, or -1.
func indexOf(xs []int, v int) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}
