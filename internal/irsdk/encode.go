package irsdk

import (
	"encoding/binary"
	"fmt"
	"math"
)

// RowBuilder writes named telemetry values into a variable row, using the same
// layout the decoder reads.
//
// It exists so synthetic captures can be produced by the dataset generator and
// by hand-authored test fixtures, without a second implementation of the byte
// layout that could drift from Row.
type RowBuilder struct {
	vars map[string]VarHeader
	data []byte
}

// NewRowBuilder returns a builder over a zeroed row of bufLen bytes.
func NewRowBuilder(vars []VarHeader, bufLen int32) *RowBuilder {
	m := make(map[string]VarHeader, len(vars))
	for _, v := range vars {
		m[v.Name] = v
	}
	return &RowBuilder{vars: m, data: make([]byte, bufLen)}
}

// Bytes returns the row. The slice is owned by the builder; callers that retain
// it across a Reset must copy first.
func (b *RowBuilder) Bytes() []byte { return b.data }

// Reset zeroes the row so the builder can be reused for the next frame.
func (b *RowBuilder) Reset() {
	for i := range b.data {
		b.data[i] = 0
	}
}

// field locates element i of the named variable, verifying type and bounds.
func (b *RowBuilder) field(name string, i int, want ...VarType) ([]byte, error) {
	v, ok := b.vars[name]
	if !ok {
		return nil, fmt.Errorf("irsdk: no variable named %q in this layout", name)
	}
	if !typeMatches(v.Type, want) {
		return nil, fmt.Errorf("irsdk: variable %q is %s, not the requested type", name, v.Type)
	}
	sz := v.Type.Size()
	if sz == 0 || i < 0 || int32(i) >= v.Count {
		return nil, fmt.Errorf("irsdk: element %d is outside variable %q (count %d)", i, name, v.Count)
	}
	start := int(v.Offset) + i*sz
	if start < 0 || start+sz > len(b.data) {
		return nil, fmt.Errorf("irsdk: variable %q element %d runs past the %d byte row", name, i, len(b.data))
	}
	return b.data[start : start+sz], nil
}

// SetInt writes an int variable.
func (b *RowBuilder) SetInt(name string, v int32) error {
	f, err := b.field(name, 0, VarInt)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(f, uint32(v))
	return nil
}

// SetBitField writes a bitField variable.
func (b *RowBuilder) SetBitField(name string, v uint32) error {
	f, err := b.field(name, 0, VarBitField)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(f, v)
	return nil
}

// SetBool writes a bool variable.
func (b *RowBuilder) SetBool(name string, v bool) error {
	f, err := b.field(name, 0, VarBool)
	if err != nil {
		return err
	}
	if v {
		f[0] = 1
	} else {
		f[0] = 0
	}
	return nil
}

// SetFloat writes a float or double variable, narrowing as the layout requires.
func (b *RowBuilder) SetFloat(name string, v float64) error {
	if f, err := b.field(name, 0, VarFloat); err == nil {
		binary.LittleEndian.PutUint32(f, math.Float32bits(float32(v)))
		return nil
	}
	f, err := b.field(name, 0, VarDouble)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(f, math.Float64bits(v))
	return nil
}

// SetIntAt writes one element of an int array variable.
func (b *RowBuilder) SetIntAt(name string, i int, v int32) error {
	f, err := b.field(name, i, VarInt)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(f, uint32(v))
	return nil
}

// SetBoolAt writes one element of a bool array variable.
func (b *RowBuilder) SetBoolAt(name string, i int, v bool) error {
	f, err := b.field(name, i, VarBool)
	if err != nil {
		return err
	}
	if v {
		f[0] = 1
	} else {
		f[0] = 0
	}
	return nil
}

// SetFloatAt writes one element of a float or double array variable.
func (b *RowBuilder) SetFloatAt(name string, i int, v float64) error {
	if f, err := b.field(name, i, VarFloat); err == nil {
		binary.LittleEndian.PutUint32(f, math.Float32bits(float32(v)))
		return nil
	}
	f, err := b.field(name, i, VarDouble)
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint64(f, math.Float64bits(v))
	return nil
}
