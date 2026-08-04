package irsdk

import (
	"encoding/binary"
	"math"
	"sort"
)

// Row decodes telemetry values out of a single variable buffer row, looked up
// by variable name.
//
// Every accessor returns (value, ok). ok is false when the variable is absent,
// is of a different type than requested, or when its declared extent runs past
// the row. A malformed row must never panic, because the variable set the sim
// publishes depends on the car and session and is not fully known ahead of
// time.
type Row struct {
	vars map[string]VarHeader
	data []byte
}

// NewRow builds a decoder over data using vars as the layout. It does not copy
// data, so callers must not mutate it afterwards.
func NewRow(vars []VarHeader, data []byte) Row {
	m := make(map[string]VarHeader, len(vars))
	for _, v := range vars {
		m[v.Name] = v
	}
	return Row{vars: m, data: data}
}

// Raw returns the row's backing bytes, for callers that need to store the row
// verbatim rather than decode it. The slice is not copied.
func (r Row) Raw() []byte { return r.data }

// Names returns the sorted names of every variable in the row's layout.
func (r Row) Names() []string {
	out := make([]string, 0, len(r.vars))
	for n := range r.vars {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Has reports whether the named variable is present in the layout.
func (r Row) Has(name string) bool {
	_, ok := r.vars[name]
	return ok
}

// slice returns the bytes for element i of the named variable, verifying the
// type matches one of want and that the read is in bounds.
func (r Row) slice(name string, i int, want ...VarType) ([]byte, bool) {
	v, ok := r.vars[name]
	if !ok {
		return nil, false
	}
	if !typeMatches(v.Type, want) {
		return nil, false
	}
	sz := v.Type.Size()
	if sz == 0 || i < 0 || int32(i) >= v.Count {
		return nil, false
	}
	start := int(v.Offset) + i*sz
	if start < 0 || start+sz > len(r.data) {
		return nil, false
	}
	return r.data[start : start+sz], true
}

// count returns the declared element count of the named variable, after
// verifying the type matches and the whole extent is in bounds.
func (r Row) count(name string, want ...VarType) (VarHeader, bool) {
	v, ok := r.vars[name]
	if !ok {
		return VarHeader{}, false
	}
	if !typeMatches(v.Type, want) {
		return VarHeader{}, false
	}
	if v.Type.Size() == 0 || v.Count <= 0 {
		return VarHeader{}, false
	}
	if int(v.Offset) < 0 || int(v.Extent()) > len(r.data) {
		return VarHeader{}, false
	}
	return v, true
}

// typeMatches reports whether got is one of want.
func typeMatches(got VarType, want []VarType) bool {
	for _, w := range want {
		if got == w {
			return true
		}
	}
	return false
}

// Int returns an int variable.
func (r Row) Int(name string) (int32, bool) {
	b, ok := r.slice(name, 0, VarInt)
	if !ok {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(b)), true
}

// BitField returns a bitField variable.
func (r Row) BitField(name string) (uint32, bool) {
	b, ok := r.slice(name, 0, VarBitField)
	if !ok {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}

// Bool returns a bool variable.
func (r Row) Bool(name string) (bool, bool) {
	b, ok := r.slice(name, 0, VarBool)
	if !ok {
		return false, false
	}
	return b[0] != 0, true
}

// Float returns a float or double variable, widened to float64.
func (r Row) Float(name string) (float64, bool) {
	if b, ok := r.slice(name, 0, VarFloat); ok {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), true
	}
	if b, ok := r.slice(name, 0, VarDouble); ok {
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), true
	}
	return 0, false
}

// IntArray returns every element of an int array variable.
func (r Row) IntArray(name string) ([]int32, bool) {
	v, ok := r.count(name, VarInt)
	if !ok {
		return nil, false
	}
	out := make([]int32, v.Count)
	for i := range out {
		b, ok := r.slice(name, i, VarInt)
		if !ok {
			return nil, false
		}
		out[i] = int32(binary.LittleEndian.Uint32(b))
	}
	return out, true
}

// BoolArray returns every element of a bool array variable.
func (r Row) BoolArray(name string) ([]bool, bool) {
	v, ok := r.count(name, VarBool)
	if !ok {
		return nil, false
	}
	out := make([]bool, v.Count)
	for i := range out {
		b, ok := r.slice(name, i, VarBool)
		if !ok {
			return nil, false
		}
		out[i] = b[0] != 0
	}
	return out, true
}

// FloatArray returns every element of a float or double array variable,
// widened to float64.
func (r Row) FloatArray(name string) ([]float64, bool) {
	v, ok := r.count(name, VarFloat, VarDouble)
	if !ok {
		return nil, false
	}
	out := make([]float64, v.Count)
	for i := range out {
		if v.Type == VarFloat {
			b, ok := r.slice(name, i, VarFloat)
			if !ok {
				return nil, false
			}
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
			continue
		}
		b, ok := r.slice(name, i, VarDouble)
		if !ok {
			return nil, false
		}
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return out, true
}
