package main

import (
	"testing"
	"time"
)

func TestIsPastStopDate(t *testing.T) {
	cases := []struct {
		name      string
		now       time.Time
		stopAfter string
		tz        string
		want      bool
	}{
		{
			name:      "empty disables cutoff",
			now:       time.Date(2027, 1, 2, 8, 0, 0, 0, time.UTC),
			stopAfter: "",
			tz:        "Europe/Dublin",
			want:      false,
		},
		{
			name:      "on stop date still runs",
			now:       time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC),
			stopAfter: "2026-12-31",
			tz:        "Europe/Dublin",
			want:      false,
		},
		{
			name:      "day after stop date skips",
			now:       time.Date(2027, 1, 1, 0, 0, 1, 0, time.UTC),
			stopAfter: "2026-12-31",
			tz:        "Europe/Dublin",
			want:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isPastStopDate(tc.now, tc.stopAfter, tc.tz)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("isPastStopDate() = %v, want %v", got, tc.want)
			}
		})
	}
}
