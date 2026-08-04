// Package irsdk reads telemetry from the iRacing simulator's shared
// memory-mapped file. Constants and layouts mirror
// documentation/irsdk_1_20/irsdk_defines.h.
package irsdk

// Shared memory and event object names published by the sim.
const (
	MemMapFileName     = `Local\IRSDKMemMapFileName`
	DataValidEventName = `Local\IRSDKDataValidEvent`
)

// Layout limits from irsdk_defines.h.
const (
	MaxBufs   = 4
	MaxString = 32
	MaxDesc   = 64

	// ExpectedVer is IRSDK_VER. A higher value in the header is logged as a
	// warning but is not fatal, since the layout has been stable.
	ExpectedVer = 2

	// UnlimitedLaps and UnlimitedTime are the sim's sentinels for a session
	// with no lap or time limit.
	UnlimitedLaps = 32767
	UnlimitedTime = 604800.0
)

// StatusConnected is the irsdk_stConnected bit in Header.Status.
const StatusConnected = 1

// VarType is the storage type of a telemetry variable.
type VarType int32

// VarType values. These are wire format; do not reorder.
const (
	VarChar VarType = iota
	VarBool
	VarInt
	VarBitField
	VarFloat
	VarDouble
)

// Size returns the width in bytes of a single element of this type, or 0 if
// the type is unknown.
func (v VarType) Size() int {
	switch v {
	case VarChar, VarBool:
		return 1
	case VarInt, VarBitField, VarFloat:
		return 4
	case VarDouble:
		return 8
	default:
		return 0
	}
}

// String implements fmt.Stringer.
func (v VarType) String() string {
	switch v {
	case VarChar:
		return "char"
	case VarBool:
		return "bool"
	case VarInt:
		return "int"
	case VarBitField:
		return "bitField"
	case VarFloat:
		return "float"
	case VarDouble:
		return "double"
	default:
		return "unknown"
	}
}

// TrkLoc describes where a car is relative to the track surface. It is the
// element type of the CarIdxTrackSurface array.
type TrkLoc int32

// TrkLoc values. These are wire format; do not reorder.
const (
	NotInWorld      TrkLoc = -1
	OffTrack        TrkLoc = 0
	InPitStall      TrkLoc = 1
	ApproachingPits TrkLoc = 2
	OnTrack         TrkLoc = 3
)

// SessionState is the value of the SessionState telemetry variable.
type SessionState int32

// SessionState values. These are wire format; do not reorder.
const (
	StateInvalid SessionState = iota
	StateGetInCar
	StateWarmup
	StateParadeLaps
	StateRacing
	StateCheckered
	StateCoolDown
)
