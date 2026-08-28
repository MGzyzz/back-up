package telegram

import (
	"testing"
	"time"
)

func TestNormalizeChannelID(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{"вид Bot API с префиксом -100", -1001234567890, 1234567890},
		{"уже в виде MTProto", 1234567890, 1234567890},
		{"отрицательный без префикса 100 — старая группа", -987654321, 987654321},
		{"ноль остаётся нулём", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeChannelID(tt.in); got != tt.want {
				t.Errorf("NormalizeChannelID(%d) = %d, ожидал %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestDayBounds(t *testing.T) {
	loc := time.FixedZone("+05", 5*60*60)

	// Полдень 28 августа в зоне +05.
	from, to := dayBounds(time.Date(2026, 8, 28, 12, 30, 0, 0, loc), loc)

	wantFrom := time.Date(2026, 8, 28, 0, 0, 0, 0, loc)
	wantTo := time.Date(2026, 8, 29, 0, 0, 0, 0, loc)
	if !from.Equal(wantFrom) {
		t.Errorf("from = %v, ожидал %v", from, wantFrom)
	}
	if !to.Equal(wantTo) {
		t.Errorf("to = %v, ожидал %v", to, wantTo)
	}
}

// Сообщение, пришедшее ночью по местному времени, по UTC относится
// ко вчерашнему дню. Границы обязаны считаться в зоне из конфига,
// иначе ночные бэкапы уедут во вчерашний отчёт.
func TestDayBoundsUsesConfiguredZone(t *testing.T) {
	loc := time.FixedZone("+05", 5*60*60)

	// 01:30 по Алматы = 20:30 предыдущего дня по UTC.
	night := time.Date(2026, 8, 28, 1, 30, 0, 0, loc)
	from, to := dayBounds(night.UTC(), loc)

	if from.Day() != 28 {
		t.Errorf("from = %v, ожидал 28 августа", from)
	}
	if !night.After(from) || !night.Before(to) {
		t.Errorf("ночное сообщение %v не попало в интервал [%v, %v)", night, from, to)
	}
}
