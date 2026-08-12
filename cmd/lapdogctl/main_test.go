package main

import (
	"testing"
	"time"
)

func TestTimeFromName(t *testing.T) {
	cases := []struct {
		name string
		want time.Time
	}{
		{
			name: "20260812T014837Z-87900660-1.lpd",
			want: time.Date(2026, 8, 12, 1, 48, 37, 0, time.UTC),
		},
		{
			name: "20260812-014837-public-practice.lpd",
			want: time.Date(2026, 8, 12, 1, 48, 37, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := timeFromName(tc.name); !got.Equal(tc.want) {
				t.Errorf("timeFromName(%q) = %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}
