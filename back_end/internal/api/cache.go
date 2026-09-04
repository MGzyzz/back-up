package api

import (
	"context"
	"sync"
	"time"

	"backup-report/internal/dates"
	"backup-report/internal/report"
)

// entry — готовый отчёт и момент, после которого его надо перечитать.
type entry struct {
	rep       ReportDTO
	expiresAt time.Time
}

// loader отдаёт отчёт за сутки, ходя за ним не чаще, чем нужно.
//
// Мьютекс держится на всё время похода в Telegram, и это сделано умышленно.
// Обычно блокировка вокруг сетевого вызова — ошибка, но здесь она заодно
// решает вторую задачу: MTProto-соединение пишет data/session.json, и двух
// одновременных соединений с одной сессией быть не должно. Побочный
// полезный эффект — десять одновременных запросов за одну дату дают один
// поход, остальные читают уже готовое.
type loader struct {
	mu    sync.Mutex
	items map[string]entry

	fetch    func(ctx context.Context, day time.Time) (ReportDTO, error)
	loc      *time.Location
	ttlToday time.Duration
	ttlPast  time.Duration

	// now подменяется в тестах: иначе TTL не проверить, не засыпая.
	now func() time.Time
}

func newLoader(
	fetch func(ctx context.Context, day time.Time) (ReportDTO, error),
	loc *time.Location,
	ttlToday, ttlPast time.Duration,
) *loader {
	return &loader{
		items:    make(map[string]entry),
		fetch:    fetch,
		loc:      loc,
		ttlToday: ttlToday,
		ttlPast:  ttlPast,
		now:      time.Now,
	}
}

// get отдаёт отчёт за сутки — из кэша либо сходив за ним.
func (l *loader) get(ctx context.Context, day time.Time) (ReportDTO, error) {
	key := day.In(l.loc).Format(report.DateLayout)

	l.mu.Lock()
	defer l.mu.Unlock()

	// Проверка под замком, а не до него: пока мы ждали, данные мог положить
	// сосед, и второй поход в Telegram был бы напрасным.
	if e, ok := l.items[key]; ok && l.now().Before(e.expiresAt) {
		e.rep.Cached = true
		return e.rep, nil
	}

	rep, err := l.fetch(ctx, day)
	if err != nil {
		// Неудачу не кэшируем: следующий запрос должен попробовать снова.
		return ReportDTO{}, err
	}
	rep.Cached = false
	l.items[key] = entry{rep: rep, expiresAt: l.now().Add(l.ttl(day))}
	return rep, nil
}

// ttl: сегодняшний день ещё дописывается, прошедшие сутки закрыты навсегда.
func (l *loader) ttl(day time.Time) time.Duration {
	today := dates.StartOfDay(l.now().In(l.loc), l.loc)
	if !dates.StartOfDay(day.In(l.loc), l.loc).Before(today) {
		return l.ttlToday
	}
	return l.ttlPast
}
