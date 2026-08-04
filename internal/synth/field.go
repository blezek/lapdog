package synth

import "math/rand"

// buildStartOrder returns the car indices in starting-position order.
//
// For a race, qualifying decides the grid, so the order is taken from the
// qualifying result when one exists. Otherwise cars are ordered by rating, which
// is a reasonable stand-in for a practice or hosted session's classification.
func buildStartOrder(w *Weekend, sessionIdx int) []int {
	order := make([]int, 0, w.FieldSize())

	qi := w.qualifyIndex()
	if isRaceType(w.Sessions[sessionIdx].RawType) && qi >= 0 && len(w.QualifyResults) > 0 {
		for _, q := range w.QualifyResults {
			order = append(order, q.CarIdx)
		}
		return order
	}

	order = append(order, w.DriverCarIdx)
	for i := range w.Opponents {
		order = append(order, w.carIdxFor(i))
	}
	return order
}

// advanceField moves the opponents around the lap and decides when one pits or
// retires.
//
// Pit stops and retirements matter because they are what make a position change
// something other than an overtake. Without them the dataset would contain only
// OnTrack causes and the pass/passed attribution would never be tested.
func advanceField(sw *writer, w *Weekend, cars []carState, order []int, lap int, pct float64) {
	for i := range cars {
		c := &cars[i]
		c.lap = lap + 1
		c.distPct = pct

		if c.retired {
			continue
		}
		if c.onPitRoad && sw.t >= c.pitUntil {
			c.onPitRoad = false
		}
		if c.onPitRoad {
			continue
		}

		// A mid-race pit stop. Restricted to the middle of the race so the window
		// does not overlap the start or the finish.
		if lap > 3 && sw.rng.Float64() < 0.0016 {
			c.onPitRoad = true
			c.pitUntil = sw.t + 20 + sw.rng.Float64()*18
			continue
		}
		// A retirement. Rare, and permanent for the rest of the session.
		if lap > 2 && sw.rng.Float64() < 0.00035 {
			c.retired = true
		}
	}

	// A retired car keeps its slot in the running order rather than being moved to
	// the back.
	//
	// That is deliberate, and it is what generates the OpponentOffWorld cause: the
	// driver passes the stricken car, so at the moment of the swap that car is the
	// one holding the position just vacated, and the ingestion path can see it is
	// out of the world. Dropping it to the rear instead would leave a different,
	// perfectly healthy car in that slot, and the gain would be indistinguishable
	// from a real overtake.
}

// stateFor returns the tracked state of a car by index, or nil if it is not in
// the field.
func stateFor(cars []carState, carIdx int) *carState {
	for i := range cars {
		if cars[i].carIdx == carIdx {
			return &cars[i]
		}
	}
	return nil
}

// maybeSwap occasionally exchanges the driver with an adjacent car, returning
// the driver's new position.
//
// The swap partner's state at that moment is what the ingestion path reads to
// decide the cause, so no cause is assigned here — the generator only makes the
// position change happen and leaves attribution to the collector, exactly as it
// would with real telemetry.
func maybeSwap(sw *writer, w *Weekend, cars []carState, order []int, driverPos int) (int, bool) {
	di := driverPos - 1
	if di < 0 || di >= len(order) {
		return driverPos, false
	}

	swapWith := func(other int) (int, bool) {
		order[di], order[other] = order[other], order[di]
		return other + 1, true
	}

	// stopped reports whether a car cannot defend its position.
	stopped := func(carIdx int) bool {
		c := stateFor(cars, carIdx)
		return c != nil && (c.onPitRoad || c.retired)
	}

	// A car directly ahead that is on pit road or out of the world is passed
	// immediately. This is what produces the OpponentPit and OpponentOffWorld
	// causes: without it every swap would look like an on-track pass, and
	// attributing a cause — separating real overtakes from other people's
	// misfortune — would go untested.
	if di > 0 && stopped(order[di-1]) {
		return swapWith(di - 1)
	}

	if sw.rng.Float64() > 0.004 {
		return driverPos, false
	}

	// Gaining is somewhat more likely than losing as the driver improves, but both
	// must occur or the pass/passed ratio would be degenerate.
	//
	// A car that is stopped never takes a place from the driver: you do not lose a
	// position to someone sitting in the pit lane. Excluding that case is what
	// keeps a loss from being attributed to OpponentPit or OpponentOffWorld, which
	// would be nonsense.
	forward := sw.rng.Float64() < 0.56
	canGain := di > 0
	canLose := di < len(order)-1 && !stopped(order[di+1])

	switch {
	case forward && canGain:
		return swapWith(di - 1)
	case !forward && canLose:
		return swapWith(di + 1)
	case canGain:
		return swapWith(di - 1)
	case canLose:
		return swapWith(di + 1)
	}
	return driverPos, false
}

// recordResults fills in a session's classified results once it has run.
func recordResults(
	w *Weekend, sessionIdx int, order []int, driverPos int,
	lapTimes []float64, incidents int, bestLap float64, bestLapNum int,
) {
	s := &w.Sessions[sessionIdx]

	total := 0.0
	for _, t := range lapTimes {
		total += t
	}
	avg := 0.0
	if len(lapTimes) > 0 {
		avg = total / float64(len(lapTimes))
	}

	s.LapsComplete = len(lapTimes)
	s.AverageLapS = avg
	s.CautionFlags = 0
	s.LeadChanges = 0
	if isRaceType(s.RawType) {
		s.CautionFlags = rndIntFrom(w, 0, 3)
		s.LeadChanges = rndIntFrom(w, 0, 5)
	}

	// The driver's own result.
	s.Results = append(s.Results, SessionResult{
		CarIdx:        w.DriverCarIdx,
		Position:      driverPos,
		ClassPosition: driverPos,
		LapsComplete:  len(lapTimes),
		LapsLed:       0,
		Incidents:     incidents,
		FastestLap:    bestLapNum,
		FastestTimeS:  bestLap,
		TotalTimeS:    total,
		ReasonOutID:   0,
	})

	// Opponent results, ordered by the finishing order and paced relative to
	// the driver so the classification reads consistently.
	for pos, carIdx := range order {
		if carIdx == w.DriverCarIdx {
			continue
		}
		offset := float64(pos-(driverPos-1)) * 0.35
		opponentBest := bestLap + offset
		if opponentBest <= 0 {
			opponentBest = bestLap
		}
		laps := len(lapTimes)
		s.Results = append(s.Results, SessionResult{
			CarIdx:        carIdx,
			Position:      pos + 1,
			ClassPosition: pos + 1,
			LapsComplete:  laps,
			Incidents:     rndIntFrom(w, 0, 12),
			FastestLap:    bestLapNum,
			FastestTimeS:  opponentBest,
			TotalTimeS:    opponentBest * float64(laps),
			ReasonOutID:   0,
		})
	}

	// Sort the driver's entry into position order so the document reads the way
	// the sim's does: ResultsPositions is in position order.
	sortResultsByPosition(s.Results)

	// Qualifying additionally populates QualifyResultsInfo, which only appears
	// once qualifying has run. This is the field .ibt cannot represent.
	if isQualifyType(s.RawType) {
		w.QualifyResults = w.QualifyResults[:0]
		for _, r := range s.Results {
			w.QualifyResults = append(w.QualifyResults, QualifyResult{
				CarIdx:        r.CarIdx,
				Position:      r.Position,
				ClassPosition: r.ClassPosition,
				FastestLap:    r.FastestLap,
				FastestTimeS:  r.FastestTimeS,
			})
		}
	}
}

// sortResultsByPosition orders results ascending by finishing position.
func sortResultsByPosition(rs []SessionResult) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].Position < rs[j-1].Position; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// rndIntFrom returns a value in [lo, hi], derived from the weekend so it stays
// deterministic without threading another generator through every call.
func rndIntFrom(w *Weekend, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	// Deriving from stable weekend fields keeps this reproducible for a given
	// schedule without needing the simulator's generator here.
	seed := int64(w.SubSessionID*31 + w.Track.ID*17 + w.Car.ID*7 + len(w.Sessions))
	r := rand.New(rand.NewSource(seed))
	return lo + r.Intn(hi-lo+1)
}
