package dates

import (
	"testing"
	"time"
)

func TestStartOfDay(t *testing.T) {
	loc := time.FixedZone("+05", 5*60*60)
	want := time.Date(2026, 8, 28, 0, 0, 0, 0, loc)

	for _, in := range []time.Time{
		time.Date(2026, 8, 28, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 28, 12, 30, 15, 0, loc),
		time.Date(2026, 8, 28, 23, 59, 59, 0, loc),
	} {
		if got := StartOfDay(in, loc); !got.Equal(want) {
			t.Errorf("StartOfDay(%v) = %v, ожидал %v", in, got, want)
		}
	}
}

// Момент приводится к зоне loc, а не к зоне машины: 01:30 по +05 — это
// ещё вчерашний день по UTC, и сутки обязаны считаться по конфигу.
func TestStartOfDayConvertsZone(t *testing.T) {
	loc := time.FixedZone("+05", 5*60*60)
	night := time.Date(2026, 8, 28, 1, 30, 0, 0, loc)

	got := StartOfDay(night.UTC(), loc)

	if want := time.Date(2026, 8, 28, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("StartOfDay = %v, ожидал %v", got, want)
	}
}
