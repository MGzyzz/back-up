package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"backup-report/internal/parser"
)

const (
	namePrefix = "backup-report-"
	dateLayout = "2006-01-02"
	cellLayout = "2006-01-02 15:04:05"
	noNode     = "—"
)

func humanDuration(d time.Duration) string {
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// BuildRows превращает разобранные бэкапы в строки таблицы.
//
// Сортировка стабильная: бэкапы стартуют по крону пачкой в одну и ту же секунду,
// и обычная sort.Slice переставляла бы их произвольно от запуска к запуску.
// Строки без времён (статус ERROR) уходят в конец: сортировать их не по чему.
func BuildRows(bs []parser.Backup) [][]any {
	sort.SliceStable(bs, func(i, j int) bool {
		switch {
		case !bs[i].HasTimes():
			return false
		case !bs[j].HasTimes():
			return true
		default:
			return bs[i].Start.Before(bs[j].Start)
		}
	})

	rows := [][]any{
		{"Environment", "Node", "Type", "Status", "Start", "End", "Duration"},
	}
	for _, b := range bs {
		node := b.Node
		if node == "" {
			node = noNode
		}
		// У статуса ERROR бэкап не запускался: времён нет, длительности тоже.
		start, end, dur := "", "", ""
		if b.HasTimes() {
			start = b.Start.Format(cellLayout)
			end = b.End.Format(cellLayout)
			dur = humanDuration(b.Duration())
		}
		rows = append(rows, []any{b.Environment, node, b.Type, b.Status, start, end, dur})
	}
	return rows
}

func ReportName(day time.Time) string {
	return namePrefix + day.Format(dateLayout)
}

func reportDateFromName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, namePrefix) {
		return time.Time{}, false
	}
	day, err := time.Parse(dateLayout, strings.TrimPrefix(name, namePrefix))
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

func ShouldDelete(name string, cutoff time.Time) bool {
	day, ok := reportDateFromName(name)
	if !ok {
		return false
	}
	return day.Before(cutoff)
}

func ParseReportAt(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("report_at %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}

func NextRun(now time.Time, hh, mm int, loc *time.Location) time.Time {
	now = now.In(loc)
	run := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, loc)
	if !run.After(now) {
		run = run.AddDate(0, 0, 1)
	}
	return run
}
