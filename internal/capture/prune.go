package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PruneDir enforces a total size cap over the capture files in dir by deleting
// oldest-first until the total is at or below maxBytes.
//
// maxBytes of 0 means unlimited and makes this a no-op. The file named by keep is
// never deleted, which is how the in-progress capture is protected. A missing
// directory is not an error.
//
// Only files with the capture extension are considered, so unrelated files in the
// directory are never touched.
//
// It returns the number of files removed and the bytes freed.
func PruneDir(dir string, maxBytes int64, keep string) (int, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("capture: read dir %s: %w", dir, err)
	}
	if maxBytes <= 0 {
		return 0, 0, nil
	}

	type entry struct {
		path  string
		size  int64
		mtime int64
	}
	var files []entry
	var total int64
	keepAbs, _ := filepath.Abs(keep)

	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), Ext) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // vanished between ReadDir and Info; nothing to prune
		}
		p := filepath.Join(dir, e.Name())
		total += fi.Size()

		abs, _ := filepath.Abs(p)
		if keep != "" && abs == keepAbs {
			// Counts toward the total but is not a deletion candidate.
			continue
		}
		files = append(files, entry{path: p, size: fi.Size(), mtime: fi.ModTime().UnixNano()})
	}

	if total <= maxBytes {
		return 0, 0, nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })

	var removed int
	var freed int64
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, freed, fmt.Errorf("capture: remove %s: %w", f.path, err)
		}
		total -= f.size
		freed += f.size
		removed++
	}
	return removed, freed, nil
}
