package source

import (
	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// replay plays a capture file back through the Source interface.
//
// Time advances from the frame timestamps in the file, never from the wall clock,
// so tests can run a ninety-minute race through the collector in milliseconds.
type replay struct {
	r        *capture.Reader
	meta     capture.Meta
	yaml     []byte
	update   uint32
	pendYAML bool
}

// NewReplay opens a capture file as a Source.
func NewReplay(path string) (Source, error) {
	r, err := capture.OpenReader(path)
	if err != nil {
		return nil, err
	}
	return &replay{r: r, meta: r.Meta()}, nil
}

// Meta returns the variable layout the capture was recorded against.
func (s *replay) Meta() capture.Meta { return s.meta }

// Next returns the next frame.
//
// Session records are folded into the following variable record, since the
// collector consumes one frame per poll and a YAML change is only actionable
// alongside the telemetry it describes.
func (s *replay) Next() (Frame, error) {
	for {
		rec, err := s.r.Next()
		if err != nil {
			return Frame{}, err
		}
		switch rec.Kind {
		case capture.KindSession:
			s.yaml = rec.YAML
			s.update = rec.Update
			s.pendYAML = true
		case capture.KindVars:
			f := Frame{
				T:             rec.T,
				TickCount:     rec.TickCount,
				Row:           irsdk.NewRow(s.meta.VarHeaders, rec.Vars),
				SessionYAML:   s.yaml,
				SessionUpdate: s.update,
				YAMLChanged:   s.pendYAML,
			}
			s.pendYAML = false
			return f, nil
		default:
			// A stray header record mid-file is ignored rather than fatal.
			continue
		}
	}
}

// Close releases the underlying capture file.
func (s *replay) Close() error { return s.r.Close() }

// compile-time assertion that replay satisfies Source.
var _ Source = (*replay)(nil)
