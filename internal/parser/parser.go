// Package parser разбирает тексты уведомлений о бэкапах из Telegram-канала.
//
// Пакет не ходит в сеть и не знает ни о конфиге, ни об отчётах: на входе текст,
// на выходе структура или ошибка.
package parser

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// ErrNotBackupMessage означает, что сообщение вообще не про бэкап: чужое
// уведомление в том же канале или отчёт о восстановлении. Это штатный случай,
// вызывающая сторона пропускает такие молча.
var ErrNotBackupMessage = errors.New("сообщение не про бэкап")

// tgTimeLayout соответствует "28-08-26_01:00:02" — день-месяц-год_часы:минуты:секунды.
const tgTimeLayout = "02-01-06_15:04:05"

// Возможные значения Backup.Status и Backup.Type.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
	StatusError   = "ERROR" // "🔥 BACKUP error" — команда бэкапа не выполнилась
	TypeUnknown   = "UNKNOWN"
)

// RawMessage — сообщение как его отдаёт источник. Date нужна как запасной
// способ определить день: у сообщений со статусом ERROR времён внутри текста нет.
type RawMessage struct {
	Date time.Time
	Text string
}

type Backup struct {
	Environment string
	Node        string    // может быть пустым
	Type        string    // PostgreSQL | MongoDB | MinIO | UNKNOWN
	Status      string    // SUCCESS | FAILED | ERROR
	Start       time.Time // нулевое время = не указано (бывает у ERROR)
	End         time.Time // нулевое время = не указано (бывает у ERROR)
	Day         time.Time // начало суток, к которым отнесён бэкап
}

// HasTimes сообщает, есть ли у бэкапа измеренная длительность.
func (b Backup) HasTimes() bool { return !b.Start.IsZero() && !b.End.IsZero() }

// Duration осмысленна только когда HasTimes() == true.
func (b Backup) Duration() time.Duration { return b.End.Sub(b.Start) }

// trimNoise срезает с обоих концов всё, что не буква и не цифра:
// "✅ SUCCESS" -> "SUCCESS", "❗️❗️PROD KPO❗️❗️" -> "PROD KPO" (пробел внутри цел).
func trimNoise(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// splitFields разбирает сообщение в карту "КЛЮЧ" -> "значение".
//
// Разбор именно в карту, а не по порядку строк: в реальном канале встречаются
// сообщения, где NODE стоит перед LABELS. strings.Cut режет по первому
// двоеточию, поэтому время "01:00:02" не разваливается.
func splitFields(msg string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(msg, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return fields
}

// Parse разбирает сообщение в Backup.
func Parse(m RawMessage, labels map[string]string, loc *time.Location) (Backup, error) {
	fields := splitFields(m.Text)

	if _, ok := fields["STATUS"]; !ok {
		return Backup{}, ErrNotBackupMessage
	}
	if _, ok := fields["RESTORE START TIME"]; ok {
		return Backup{}, ErrNotBackupMessage
	}

	raw := trimNoise(fields["STATUS"])

	var status string
	switch {
	case raw == "SUCCESS":
		status = StatusSuccess
	case raw == "FAILED":
		status = StatusFailed
	case strings.Contains(strings.ToLower(raw), "error"):
		status = StatusError
	default:
		return Backup{}, fmt.Errorf("неизвестный статус %q", raw)
	}

	// У статуса ERROR времён нет вовсе: команда бэкапа не выполнялась.
	// Пустое значение приравниваем к отсутствию: в канале встречается
	// "BACKUP START TIME:" вообще без времени.
	var start, end time.Time
	if fields["BACKUP START TIME"] != "" {
		var err error
		start, err = time.ParseInLocation(tgTimeLayout, fields["BACKUP START TIME"], loc)
		if err != nil {
			return Backup{}, fmt.Errorf("BACKUP START TIME: %w", err)
		}
		end, err = time.ParseInLocation(tgTimeLayout, fields["BACKUP FINISHED TIME"], loc)
		if err != nil {
			return Backup{}, fmt.Errorf("BACKUP FINISHED TIME: %w", err)
		}
		if end.Before(start) {
			return Backup{}, fmt.Errorf("конец %v раньше начала %v", end, start)
		}
	}

	typ, ok := labels[trimNoise(fields["LABELS"])]
	if !ok {
		typ = TypeUnknown
	}

	// День берём из начала бэкапа, а если его нет — из даты самого сообщения.
	d := m.Date
	if !start.IsZero() {
		d = start
	}
	d = d.In(loc)

	return Backup{
		Environment: trimNoise(fields["ENVIRONMENT"]),
		Node:        fields["NODE"],
		Type:        typ,
		Status:      status,
		Start:       start,
		End:         end,
		Day:         time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc),
	}, nil
}
