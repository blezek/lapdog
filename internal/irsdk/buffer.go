package irsdk

// IsTorn reports whether a variable row copied out between two observations
// of its VarBuf may have been overwritten mid-copy.
//
// The sim writes TickCountBegin before starting a row and TickCount after
// finishing it. A caller reads the VarBuf, copies BufLen bytes, then re-reads
// the VarBuf and passes both here. If either counter moved, or if
// TickCountBegin is ahead of TickCount, the copy is not self-consistent and
// must be discarded rather than partially applied.
func IsTorn(before, after VarBuf) bool {
	if before.TickCount != after.TickCount {
		return true
	}
	if before.TickCountBegin != after.TickCountBegin {
		return true
	}
	return after.TickCountBegin != after.TickCount
}
