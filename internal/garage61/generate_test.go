package garage61

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

type fixture struct {
	Car       Entity
	Track     Entity
	Laps      []lap
	CSV       map[string]string
	Scenarios []store.BrakeCatalogScenario
}

func loadFixture(t *testing.T) fixture {
	t.Helper()
	body, err := os.ReadFile("testdata/python_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err = json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	c := New(func(context.Context) (string, error) { return "test-secret", nil })
	c.baseURL = "https://garage61.test"
	c.http.Transport = handlerTransport{handler}
	c.interval = 0
	return c
}

type handlerTransport struct{ handler http.HandlerFunc }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	h.handler(recorder, r)
	return recorder.Result(), nil
}

func TestPythonParity(t *testing.T) {
	f := loadFixture(t)
	events := []event{}
	for _, l := range f.Laps {
		rows, err := loadCSV([]byte(f.CSV[l.ID]))
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, detectEvents(l, rows)...)
	}
	clusters := clusterEvents(events)
	if len(clusters) != len(f.Scenarios) {
		t.Fatalf("zones=%d, Python=%d", len(clusters), len(f.Scenarios))
	}
	for i, c := range clusters {
		got := makeScenario(c, i+1, len(f.Laps), f.Car, f.Track, "2026-10-06T12:00:00Z")
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(f.Scenarios[i])
		var a, b any
		json.Unmarshal(gotJSON, &a)
		json.Unmarshal(wantJSON, &b)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("zone %d differs from Python\ngot: %s\nwant: %s", i+1, gotJSON, wantJSON)
		}
	}
}
func TestGenerateDownloadsOnlyVisibleLapsAndScreensOutput(t *testing.T) {
	f := loadFixture(t)
	downloads := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing authorization")
		}
		if r.URL.Path == "/laps" {
			q := r.URL.Query()
			if q.Get("group") != "driver-car" || q.Get("cars") != "101" || q.Get("tracks") != "202" || q.Get("lapTypes") != "1" {
				t.Errorf("wrong lap query: %v", q)
			}
			laps := append([]lap(nil), f.Laps...)
			laps[0].CanViewTelemetry = false
			json.NewEncoder(w).Encode(map[string]any{"items": laps, "total": len(laps)})
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/laps/"), "/csv")
		downloads++
		fmt.Fprint(w, f.CSV[id])
	})
	got, err := c.Generate(context.Background(), f.Car, f.Track, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if downloads != 5 || len(got.Scenarios) != 2 {
		t.Fatalf("downloads=%d scenarios=%d", downloads, len(got.Scenarios))
	}
	body, _ := json.Marshal(got)
	if _, err := store.DecodeBrakeCatalog(strings.NewReader(string(body))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "test-secret") || strings.Contains(string(body), "driver") {
		t.Fatal("private data reached catalog")
	}
}
func TestLapsScansAtLeastThreeBlocks(t *testing.T) {
	offsets := []int{}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		offsets = append(offsets, offset)
		page := []lap{}
		for i := offset; i < offset+12; i++ {
			page = append(page, lap{ID: fmt.Sprint(i), LapTime: 80 + float64(i), CanViewTelemetry: true})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": page, "total": 100})
	})
	laps, err := c.laps(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(offsets, []int{0, 12, 24}) || len(laps) != 12 || laps[11].ID != "11" {
		t.Fatalf("offsets=%v laps=%+v", offsets, laps)
	}
}
func TestRejectUnsortedAndInsufficientLaps(t *testing.T) {
	f := loadFixture(t)
	for _, mode := range []string{"unordered", "insufficient"} {
		t.Run(mode, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/laps" {
					laps := append([]lap(nil), f.Laps[:2]...)
					if mode == "unordered" {
						laps[0], laps[1] = laps[1], laps[0]
					}
					json.NewEncoder(w).Encode(map[string]any{"items": laps, "total": 2})
					return
				}
				id := strings.Split(r.URL.Path, "/")[2]
				fmt.Fprint(w, f.CSV[id])
			})
			generated, err := c.Generate(context.Background(), f.Car, f.Track, func(string) {})
			want := "fastest-first"
			if mode == "unordered" {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error=%v, want %s", err, want)
				}
				return
			}
			if err != nil || len(generated.Scenarios) != 0 {
				t.Fatalf("insufficient source laps should retire old scenarios: catalog=%+v err=%v", generated, err)
			}
		})
	}
}
func TestRateLimitDelayAndCancellation(t *testing.T) {
	response := &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"0.25"}}}
	delay, ok := retryDelay(response, []byte(`{"details":{"retryAfterSeconds":0.5}}`))
	if !ok || delay != 500*time.Millisecond {
		t.Fatalf("delay=%v ok=%v", delay, ok)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "60"); w.WriteHeader(429) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.get(ctx, "/laps", nil, false)
	if err != context.DeadlineExceeded || time.Since(start) > time.Second {
		t.Fatalf("cancellation error=%v duration=%v", err, time.Since(start))
	}
}
func TestAuthenticationErrorsDoNotEchoResponse(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "test-secret private driver")
	})
	_, err := c.get(context.Background(), "/laps/x/csv", nil, true)
	if err == nil || strings.Contains(err.Error(), "test-secret") || !strings.Contains(err.Error(), "token") {
		t.Fatalf("error=%v", err)
	}
}
func TestCatalogFiltersPlatformAndKeepsLayouts(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":2,"name":"Fixture","variant":"Road","platform":"iracing"},{"id":3,"name":"Fixture","variant":"Oval","platform":"iracing"},{"id":4,"name":"Other","platform":"other"}]`)
	})
	catalog, err := c.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tracks) != 2 || catalog.Tracks[0].Label() != "Fixture Oval" {
		t.Fatalf("catalog=%+v", catalog)
	}
}

func TestCatalogReadsEveryPage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cars":
			if r.URL.Query().Get("offset") == "0" {
				fmt.Fprint(w, `{"items":[{"id":1,"name":"First","platform":"iracing","platform_id":"10"}],"total":2}`)
			} else {
				fmt.Fprint(w, `{"items":[{"id":2,"name":"Second","platform":"iracing","platform_id":"20"}],"total":2}`)
			}
		case "/tracks":
			fmt.Fprint(w, `{"items":[{"id":3,"name":"Circuit","variant":"Road","platform":"iracing","platform_id":"30"}],"total":1}`)
		}
	})
	catalog, err := c.Catalog(context.Background())
	if err != nil || len(catalog.Cars) != 2 || len(catalog.Tracks) != 1 || catalog.Cars[1].PlatformID != "20" {
		t.Fatalf("paginated catalog=%+v err=%v", catalog, err)
	}
}
