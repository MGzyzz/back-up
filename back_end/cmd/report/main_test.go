package main

import (
	"strings"
	"testing"
	"time"
)

var testLoc = time.FixedZone("+05", 5*60*60)

func TestReportDay(t *testing.T) {
	// 31 августа, 14:47 по Алматы — момент, в который день ещё не кончился.
	now := time.Date(2026, 8, 31, 14, 47, 0, 0, testLoc)

	tests := []struct {
		name    string
		date    string
		want    string // "" = ожидаем ошибку
		errWant string
	}{
		{name: "пусто — сегодня", date: "", want: "2026-08-31"},
		{name: "сегодня явно", date: "2026-08-31", want: "2026-08-31"},
		{name: "вчера", date: "2026-08-30", want: "2026-08-30"},
		{name: "прошлый месяц", date: "2026-07-01", want: "2026-07-01"},
		{name: "завтра — отказ", date: "2026-09-01", errWant: "день ещё не наступил"},
		{name: "далёкое будущее — отказ", date: "2027-01-01", errWant: "день ещё не наступил"},
		{name: "битый формат", date: "01.09.2026", errWant: "ожидается формат"},
		{name: "несуществующая дата", date: "2026-02-30", errWant: "ожидается формат"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reportDay(tt.date, now, testLoc)

			if tt.errWant != "" {
				if err == nil {
					t.Fatalf("ожидал ошибку, получил день %v", got)
				}
				if !strings.Contains(err.Error(), tt.errWant) {
					t.Errorf("ошибка %q, ожидал упоминание %q", err, tt.errWant)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got.Format("2006-01-02") != tt.want {
				t.Errorf("день = %s, ожидал %s", got.Format("2006-01-02"), tt.want)
			}
		})
	}
}

// Граница суток: в 23:59 завтрашний день всё ещё будущее,
// а сразу после полуночи — уже сегодня.
func TestReportDayMidnightBoundary(t *testing.T) {
	if _, err := reportDay("2026-09-01", time.Date(2026, 8, 31, 23, 59, 59, 0, testLoc), testLoc); err == nil {
		t.Error("в 23:59:59 31 августа 1 сентября ещё не наступило, ожидал отказ")
	}
	if _, err := reportDay("2026-09-01", time.Date(2026, 9, 1, 0, 0, 1, 0, testLoc), testLoc); err != nil {
		t.Errorf("после полуночи 1 сентября — уже сегодня, отказ не нужен: %v", err)
	}
}

// День считается в зоне из конфига, а не в зоне машины: в 02:00 по Алматы
// по UTC ещё вчера, и отчёт за «сегодня» не должен становиться будущим.
func TestReportDayUsesConfiguredZone(t *testing.T) {
	night := time.Date(2026, 8, 31, 2, 0, 0, 0, testLoc) // = 21:00 30 августа UTC
	if _, err := reportDay("2026-08-31", night.UTC(), testLoc); err != nil {
		t.Errorf("31 августа по Алматы — сегодня, а не будущее: %v", err)
	}
}

// Флаг, который ничего не делает, опаснее отказа: человек уверен, что
// отчёт построен за указанный день, а демон построил его за сегодня.
func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    options
		errWant string
	}{
		{"обычный запуск", options{date: "2026-08-28"}, ""},
		{"демон без даты", options{daemon: true}, ""},
		{"дата вместе с демоном", options{daemon: true, date: "2026-08-28"}, "-date"},
		{"два интерактивных режима", options{login: true, channels: true}, "-login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			switch {
			case tt.errWant == "" && err != nil:
				t.Fatalf("validate() = %v, ожидал nil", err)
			case tt.errWant != "" && err == nil:
				t.Fatalf("validate() = nil, ожидал ошибку про %s", tt.errWant)
			case tt.errWant != "" && !strings.Contains(err.Error(), tt.errWant):
				t.Errorf("ошибка %q не называет %s", err, tt.errWant)
			}
		})
	}
}
