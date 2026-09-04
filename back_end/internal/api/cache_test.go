package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingFetch считает походы за данными.
func countingFetch(calls *atomic.Int32, err error) func(context.Context, time.Time) (ReportDTO, error) {
	return func(_ context.Context, day time.Time) (ReportDTO, error) {
		calls.Add(1)
		if err != nil {
			return ReportDTO{}, err
		}
		return ReportDTO{Date: day.Format("2006-01-02")}, nil
	}
}

func TestВторойЗапросБерётИзКэша(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	first, err := l.get(context.Background(), day)
	if err != nil {
		t.Fatalf("первый запрос: %v", err)
	}
	if first.Cached {
		t.Error("первый ответ помечен как кэшированный")
	}

	second, err := l.get(context.Background(), day)
	if err != nil {
		t.Fatalf("второй запрос: %v", err)
	}
	if !second.Cached {
		t.Error("второй ответ не помечен как кэшированный")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1", got)
	}
}

func TestИстёкшийTTLЗаставляетСходитьЗаново(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	now := day.Add(10 * time.Hour)
	l.now = func() time.Time { return now }

	l.get(context.Background(), day)
	now = now.Add(2 * time.Minute) // TTL сегодняшнего дня — минута
	l.get(context.Background(), day)

	if got := calls.Load(); got != 2 {
		t.Errorf("походов за данными: %d, ожидалось 2", got)
	}
}

func TestПрошедшийДеньЖивётДольше(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	past := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	l.get(context.Background(), past)
	now = now.Add(30 * time.Minute) // больше TTL сегодняшнего, меньше TTL прошедшего
	l.get(context.Background(), past)

	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1: прошедшие сутки не меняются", got)
	}
}

func TestОшибкаНеКэшируется(t *testing.T) {
	var calls atomic.Int32
	boom := errors.New("канал недоступен")
	l := newLoader(countingFetch(&calls, boom), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	l.get(context.Background(), day)
	l.get(context.Background(), day)

	if got := calls.Load(); got != 2 {
		t.Errorf("походов за данными: %d, ожидалось 2: неудача не должна оседать в кэше", got)
	}
}

func TestДесятьГорутинДаютОдинПоход(t *testing.T) {
	var calls atomic.Int32
	slow := func(_ context.Context, day time.Time) (ReportDTO, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // имитируем поход в сеть
		return ReportDTO{Date: day.Format("2006-01-02")}, nil
	}
	l := newLoader(slow, time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.get(context.Background(), day); err != nil {
				t.Errorf("запрос упал: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1", got)
	}
}
