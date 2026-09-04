package app

import "time"

// nextRun возвращает ближайший момент hh:mm в зоне loc строго после now.
//
// Ровно в hh:mm срабатывание переносится на завтра: иначе запуск, начавшийся
// секунда в секунду, сразу увидел бы «время пришло» и сработал дважды.
func nextRun(now time.Time, hh, mm int, loc *time.Location) time.Time {
	now = now.In(loc)
	run := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	if !run.After(now) {
		run = run.AddDate(0, 0, 1)
	}
	return run
}
