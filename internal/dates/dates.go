// Package dates содержит операции над сутками. Полночь нужна и транспорту
// (границы выборки), и отчёту (граница хранения), и разбору флагов.
package dates

import "time"

// StartOfDay — полночь тех суток, в которые попадает t, в зоне loc.
//
// Зона приходит параметром: сутки считаются в зоне из конфига, а не в зоне
// машины, иначе ночные бэкапы уезжают в соседний день.
func StartOfDay(t time.Time, loc *time.Location) time.Time {
	d := t.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}
