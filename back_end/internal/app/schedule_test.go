package app

import (
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "до 11:00 — сегодня",
			now:  time.Date(2025, 12, 15, 9, 30, 0, 0, testLoc),
			want: time.Date(2025, 12, 15, 11, 0, 0, 0, testLoc),
		},
		{
			name: "после 11:00 — завтра",
			now:  time.Date(2025, 12, 15, 15, 40, 0, 0, testLoc),
			want: time.Date(2025, 12, 16, 11, 0, 0, 0, testLoc),
		},
		{
			name: "ровно 11:00 — завтра, чтобы не сработать дважды",
			now:  time.Date(2025, 12, 15, 11, 0, 0, 0, testLoc),
			want: time.Date(2025, 12, 16, 11, 0, 0, 0, testLoc),
		},
		{
			name: "переход через конец месяца",
			now:  time.Date(2025, 12, 31, 23, 59, 0, 0, testLoc),
			want: time.Date(2026, 1, 1, 11, 0, 0, 0, testLoc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextRun(tt.now, 11, 0, testLoc); !got.Equal(tt.want) {
				t.Errorf("nextRun(%v) = %v, ожидал %v", tt.now, got, tt.want)
			}
		})
	}
}
