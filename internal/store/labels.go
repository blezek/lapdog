package store

// qualifyDuplicateLabels appends an identity only when different entities have
// the same visible label. Rows for different categories of one combo share an
// identity and therefore keep the same label.
func qualifyDuplicateLabels[T any](rows []T, label func(*T) *string, identity func(*T) string) {
	idsByLabel := make(map[string]map[string]struct{})
	for i := range rows {
		name, id := *label(&rows[i]), identity(&rows[i])
		ids := idsByLabel[name]
		if ids == nil {
			ids = make(map[string]struct{})
			idsByLabel[name] = ids
		}
		ids[id] = struct{}{}
	}
	for i := range rows {
		name := label(&rows[i])
		if len(idsByLabel[*name]) > 1 {
			*name += " · #" + identity(&rows[i])
		}
	}
}
