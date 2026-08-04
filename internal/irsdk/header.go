package irsdk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Binary sizes of the shared-memory structures, in bytes. See
// irsdk_defines.h: HeaderSize is 48 bytes of scalars plus MaxBufs 16-byte
// varBuf entries; VarHeaderSize is 16 + MaxString + MaxDesc + MaxString.
const (
	HeaderSize    = 48 + MaxBufs*16
	VarHeaderSize = 16 + MaxString + MaxDesc + MaxString
)

// ErrShortBuffer indicates the supplied bytes are too small for the
// structure being parsed.
var ErrShortBuffer = errors.New("irsdk: short buffer")

// VarBuf describes one of the sim's triple-buffered variable rows.
//
// TickCountBegin is written before the sim starts writing the row and
// TickCount after it finishes, which is what makes torn reads detectable.
type VarBuf struct {
	TickCount      int32
	BufOffset      int32
	TickCountBegin int32
}

// Header is the irsdk_header at offset 0 of the shared memory region.
type Header struct {
	Ver               int32
	Status            int32
	TickRate          int32
	SessionInfoUpdate int32
	SessionInfoLen    int32
	SessionInfoOffset int32
	NumVars           int32
	VarHeaderOffset   int32
	NumBuf            int32
	BufLen            int32
	CurBufTickCount   int32
	CurBuf            uint8
	VarBuf            [MaxBufs]VarBuf
}

// Connected reports whether the sim has the irsdk_stConnected bit set.
func (h Header) Connected() bool { return h.Status&StatusConnected != 0 }

// LatestBuf returns the variable buffer with the highest TickCount, and
// false if the header declares no usable buffers.
func (h Header) LatestBuf() (VarBuf, bool) {
	n := int(h.NumBuf)
	if n <= 0 {
		return VarBuf{}, false
	}
	if n > MaxBufs {
		n = MaxBufs
	}
	best := 0
	for i := 1; i < n; i++ {
		if h.VarBuf[i].TickCount > h.VarBuf[best].TickCount {
			best = i
		}
	}
	return h.VarBuf[best], true
}

// ParseHeader decodes an irsdk_header from b.
func ParseHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, fmt.Errorf("%w: header needs %d bytes, got %d", ErrShortBuffer, HeaderSize, len(b))
	}
	i32 := func(off int) int32 { return int32(binary.LittleEndian.Uint32(b[off:])) }
	h := Header{
		Ver:               i32(0),
		Status:            i32(4),
		TickRate:          i32(8),
		SessionInfoUpdate: i32(12),
		SessionInfoLen:    i32(16),
		SessionInfoOffset: i32(20),
		NumVars:           i32(24),
		VarHeaderOffset:   i32(28),
		NumBuf:            i32(32),
		BufLen:            i32(36),
		CurBufTickCount:   i32(40),
		CurBuf:            b[44],
	}
	for i := 0; i < MaxBufs; i++ {
		base := 48 + i*16
		h.VarBuf[i] = VarBuf{
			TickCount:      i32(base),
			BufOffset:      i32(base + 4),
			TickCountBegin: i32(base + 8),
		}
	}
	return h, nil
}

// EncodeHeader writes h into b, which must be at least HeaderSize bytes.
//
// This exists so synthetic mappings can be built for tests and for the
// dataset generator, using the same field offsets ParseHeader reads.
func EncodeHeader(b []byte, h Header) error {
	if len(b) < HeaderSize {
		return fmt.Errorf("%w: header needs %d bytes, got %d", ErrShortBuffer, HeaderSize, len(b))
	}
	put := func(off int, v int32) { binary.LittleEndian.PutUint32(b[off:], uint32(v)) }
	put(0, h.Ver)
	put(4, h.Status)
	put(8, h.TickRate)
	put(12, h.SessionInfoUpdate)
	put(16, h.SessionInfoLen)
	put(20, h.SessionInfoOffset)
	put(24, h.NumVars)
	put(28, h.VarHeaderOffset)
	put(32, h.NumBuf)
	put(36, h.BufLen)
	put(40, h.CurBufTickCount)
	b[44] = h.CurBuf
	for i := 0; i < MaxBufs; i++ {
		base := 48 + i*16
		put(base, h.VarBuf[i].TickCount)
		put(base+4, h.VarBuf[i].BufOffset)
		put(base+8, h.VarBuf[i].TickCountBegin)
	}
	return nil
}

// VarHeader describes one telemetry variable: its type, where in a variable
// row it starts, and how many elements it has.
type VarHeader struct {
	Type        VarType `json:"type"`
	Offset      int32   `json:"offset"`
	Count       int32   `json:"count"`
	CountAsTime bool    `json:"countAsTime"`
	Name        string  `json:"name"`
	Desc        string  `json:"desc"`
	Unit        string  `json:"unit"`
}

// Extent returns the byte offset just past the variable's last element.
func (v VarHeader) Extent() int32 {
	return v.Offset + int32(v.Type.Size())*v.Count
}

// ParseVarHeaders decodes numVars consecutive irsdk_varHeader entries from b.
func ParseVarHeaders(b []byte, numVars int) ([]VarHeader, error) {
	need := numVars * VarHeaderSize
	if numVars < 0 || len(b) < need {
		return nil, fmt.Errorf("%w: %d var headers need %d bytes, got %d", ErrShortBuffer, numVars, need, len(b))
	}
	out := make([]VarHeader, numVars)
	for i := range out {
		base := i * VarHeaderSize
		out[i] = VarHeader{
			Type:        VarType(int32(binary.LittleEndian.Uint32(b[base:]))),
			Offset:      int32(binary.LittleEndian.Uint32(b[base+4:])),
			Count:       int32(binary.LittleEndian.Uint32(b[base+8:])),
			CountAsTime: b[base+12] != 0,
			Name:        cstr(b[base+16 : base+16+MaxString]),
			Desc:        cstr(b[base+48 : base+48+MaxDesc]),
			Unit:        cstr(b[base+112 : base+112+MaxString]),
		}
	}
	return out, nil
}

// EncodeVarHeaders writes vh into b, which must hold len(vh)*VarHeaderSize
// bytes. Strings longer than their fixed field are truncated, matching what
// the sim itself would produce.
func EncodeVarHeaders(b []byte, vh []VarHeader) error {
	need := len(vh) * VarHeaderSize
	if len(b) < need {
		return fmt.Errorf("%w: %d var headers need %d bytes, got %d", ErrShortBuffer, len(vh), need, len(b))
	}
	for i, v := range vh {
		base := i * VarHeaderSize
		put := func(off int, x int32) { binary.LittleEndian.PutUint32(b[base+off:], uint32(x)) }
		put(0, int32(v.Type))
		put(4, v.Offset)
		put(8, v.Count)
		if v.CountAsTime {
			b[base+12] = 1
		}
		putCstr(b[base+16:base+16+MaxString], v.Name)
		putCstr(b[base+48:base+48+MaxDesc], v.Desc)
		putCstr(b[base+112:base+112+MaxString], v.Unit)
	}
	return nil
}

// cstr converts a NUL-padded fixed-width C string to a Go string.
func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// putCstr writes s into a fixed-width field, truncating so at least one NUL
// terminator always remains.
func putCstr(field []byte, s string) {
	n := copy(field[:len(field)-1], s)
	for i := n; i < len(field); i++ {
		field[i] = 0
	}
}
