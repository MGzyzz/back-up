// Package api отдаёт отчёт по бэкапам за сутки по HTTP.
//
// Пакет намеренно не знает ни про Google Sheets, ни про internal/app: ему
// нужны только источник сообщений, разбор и свёртка в задачи. Благодаря
// этому бинарь веб-сервера не тянет Google API.
package api

import (
	"time"

	"backup-report/internal/report"
)

// JobDTO — задача бэкапа в том виде, в каком её видит фронт.
//
// Отдельный тип, а не report.Job с тегами: у Job нулевой time.Time означает
// «времени нет», и в JSON он ушёл бы как 0001-01-01T00:00:00Z. Указатель
// даёт честный null. Заодно формат транспорта не попадает в домен.
type JobDTO struct {
	Environment string `json:"environment"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	// Времена и длительность пусты у задач со статусом ERROR:
	// команда бэкапа не запускалась, засекать было нечего.
	Start           *time.Time `json:"start"`
	End             *time.Time `json:"end"`
	DurationSeconds *int       `json:"duration_seconds"`
	// Node — нода, сделавшая бэкап; у проваленной задачи пуста.
	Node string `json:"node"`
	// Nodes — все отчитавшиеся ноды. Всегда массив, никогда null:
	// на null у фронта падает .map.
	Nodes []string `json:"nodes"`
}

// StatsDTO считает сообщения, а не задачи: Messages — сколько сообщений отдал
// канал, Backups — сколько из них разобралось, Skipped — сколько отсеялось
// как не про бэкап. Задач всегда меньше, чем Backups: они сворачиваются
// по нодам.
type StatsDTO struct {
	Messages int `json:"messages"`
	Backups  int `json:"backups"`
	Skipped  int `json:"skipped"`
}

// ReportDTO — ответ ручки /api/report.
type ReportDTO struct {
	Date      string    `json:"date"`
	FetchedAt time.Time `json:"fetched_at"`
	// Cached говорит фронту, что данные пришли из кэша. Без этого кнопка
	// «обновить» выглядит сломанной, когда ответ не менялся.
	Cached bool     `json:"cached"`
	Stats  StatsDTO `json:"stats"`
	Jobs   []JobDTO `json:"jobs"`
}

// ErrorDTO — единая форма ошибки для всех кодов.
type ErrorDTO struct {
	Error string `json:"error"`
}

// jobDTOs переводит задачи в форму транспорта.
func jobDTOs(jobs []report.Job) []JobDTO {
	out := make([]JobDTO, 0, len(jobs)) // не nil: пустой отчёт должен дать [], не null
	for _, j := range jobs {
		d := JobDTO{
			Environment: j.Environment,
			Label:       j.Label,
			Name:        j.Name,
			OK:          j.OK,
			Node:        j.Node,
			Nodes:       j.Nodes,
		}
		if d.Nodes == nil {
			d.Nodes = []string{}
		}
		if j.HasTimes() {
			start, end := j.Start, j.End
			secs := int(end.Sub(start).Seconds())
			d.Start, d.End, d.DurationSeconds = &start, &end, &secs
		}
		out = append(out, d)
	}
	return out
}
