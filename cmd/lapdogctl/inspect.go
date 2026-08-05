package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// inspectUsage is printed for a malformed invocation.
const inspectUsage = `lapdogctl inspect [flags] <file.lpd>

  -yaml           print the session YAML and nothing else (default)
  -grep <substr>  print only YAML lines containing substr, with line numbers
  -ndjson         print every record as one JSON object per line
  -vars           with -ndjson, decode variable rows instead of listing their names
  -limit <n>      stop after n records (0 means all)

The default prints the session YAML, because the reason to open a capture is almost
always to read what the simulator actually reported. Use -ndjson for the frames.`

// inspectCapture dumps a capture file for a human to read.
//
// This exists to answer questions the database cannot, the first being whether the
// field the AI classifier looks for is named what the SDK headers suggest. The
// database stores the classification's inputs but not the raw YAML, so confirming a
// field name means going back to the capture.
func inspectCapture(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		yamlOnly = fs.Bool("yaml", false, "print the session YAML only")
		grep     = fs.String("grep", "", "print only YAML lines containing this substring")
		ndjson   = fs.Bool("ndjson", false, "print one JSON object per record")
		decode   = fs.Bool("vars", false, "decode variable rows")
		limit    = fs.Int("limit", 0, "stop after n records")
	)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w\n\n%s", err, inspectUsage)
	}
	if fs.NArg() != 1 {
		return errors.New(inspectUsage)
	}
	path := fs.Arg(0)

	r, err := capture.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	if *ndjson {
		return inspectNDJSON(out, r, *decode, *limit)
	}
	// -yaml is the default, so an explicit -yaml or -grep needs no special case.
	_ = *yamlOnly
	return inspectYAML(out, r, *grep)
}

// inspectYAML prints the session YAML, optionally filtered to matching lines.
//
// The last session record wins. The simulator rewrites the whole document whenever
// anything in it changes, so the final copy is the most complete — an early one can
// predate the driver list being populated.
func inspectYAML(out *bufio.Writer, r *capture.Reader, grep string) error {
	var latest []byte
	var count int
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if rec.Kind == capture.KindSession && len(rec.YAML) > 0 {
			latest = rec.YAML
			count++
		}
	}
	if latest == nil {
		return errors.New("inspect: this capture holds no session YAML")
	}

	if grep == "" {
		fmt.Fprintf(out, "# %d session record(s); showing the last, %d bytes\n",
			count, len(latest))
		out.Write(latest)
		if !strings.HasSuffix(string(latest), "\n") {
			out.WriteString("\n")
		}
		return nil
	}

	// Line numbers are printed because a match's position in the document says which
	// section it belongs to, and the sections are what disambiguate a field name.
	var hits int
	for i, line := range strings.Split(string(latest), "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(grep)) {
			fmt.Fprintf(out, "%6d: %s\n", i+1, line)
			hits++
		}
	}
	if hits == 0 {
		fmt.Fprintf(out, "# no line contains %q, in %d bytes of YAML\n", grep, len(latest))
	}
	return nil
}

// inspectNDJSON prints one JSON object per record.
//
// Variable rows carry raw bytes, which are useless on screen, so they are summarised
// by default and decoded only on request — a decoded row is one object per variable
// and there are several hundred of them per frame.
func inspectNDJSON(out *bufio.Writer, r *capture.Reader, decode bool, limit int) error {
	meta := r.Meta()
	names := make([]string, 0, len(meta.VarHeaders))
	for _, vh := range meta.VarHeaders {
		names = append(names, vh.Name)
	}

	enc := json.NewEncoder(out)
	if err := enc.Encode(map[string]any{
		"kind": "header", "tickRate": meta.TickRate,
		"numVars": meta.NumVars, "bufLen": meta.BufLen, "varNames": names,
	}); err != nil {
		return err
	}

	var n int
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		obj := map[string]any{"t": rec.T}
		switch rec.Kind {
		case capture.KindSession:
			obj["kind"] = "session"
			obj["update"] = rec.Update
			obj["yamlBytes"] = len(rec.YAML)
		case capture.KindVars:
			obj["kind"] = "vars"
			obj["tickCount"] = rec.TickCount
			if decode {
				obj["values"] = decodeRow(meta.VarHeaders, rec.Vars)
			} else {
				obj["varBytes"] = len(rec.Vars)
			}
		default:
			obj["kind"] = fmt.Sprintf("kind-%d", rec.Kind)
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}

		n++
		if limit > 0 && n >= limit {
			return nil
		}
	}
}

// decodeRow renders every scalar variable in a row.
//
// Arrays are reported by length rather than expanded: the per-car arrays are sixty-four
// entries wide and would bury the scalars that identify the frame.
func decodeRow(vh []irsdk.VarHeader, data []byte) map[string]any {
	row := irsdk.NewRow(vh, data)
	out := make(map[string]any, len(vh))
	for _, v := range vh {
		if v.Count > 1 {
			out[v.Name] = fmt.Sprintf("[%d values]", v.Count)
			continue
		}
		switch v.Type {
		case irsdk.VarFloat, irsdk.VarDouble:
			if f, ok := row.Float(v.Name); ok {
				out[v.Name] = f
			}
		case irsdk.VarBool:
			if b, ok := row.Bool(v.Name); ok {
				out[v.Name] = b
			}
		default:
			if i, ok := row.Int(v.Name); ok {
				out[v.Name] = i
			}
		}
	}
	return out
}
