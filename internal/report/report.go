// Package report превращает разобранные бэкапы в таблицы отчёта и отвечает
// за его имя: как файл назвать и когда его удалять.
//
// Таблиц две: сводка — строка на задачу, свёрнутая по нодам кластера;
// детали — то же сырьё построчно, по строке на сообщение.
//
// Момент «сейчас» приходит параметром, а не берётся изнутри: иначе
// ретенцию не проверить тестом.
package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"backup-report/internal/dates"
	"backup-report/internal/parser"
)

// DateLayout — формат даты в имени файла и в логах. Экспортирован, чтобы
// вызывающая сторона не переписывала тот же литерал у себя.
const DateLayout = "2006-01-02"

// Отметки статуса в сводке. Слово, а не эмодзи: по нему работает автофильтр.
const (
	MarkOK   = "OK"
	MarkFail = "FAIL"
)

const (
	namePrefix = "backup-report-"
	// В деталях дата полная: бэкап мог начаться накануне.
	detailTimeLayout = "2006-01-02 15:04:05"
	// В сводке дата не нужна: она одна на весь файл и стоит в его имени.
	summaryTimeLayout = "15:04:05"
	noValue           = "—"
)

// Схемы таблиц. Ширину отчёта берут отсюда, а не считают руками.
var (
	summaryHeader = []string{"Environment", "Backup", "Status", "Start", "End", "Duration", "Node"}
	detailHeader  = []string{"Environment", "Node", "Type", "Status", "Start", "End", "Duration"}
)

// Индексы колонки статуса — по ней красятся строки.
const (
	SummaryStatusCol = 2
	DetailStatusCol  = 3
)

func humanDuration(d time.Duration) string {
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// displayName — как задача называется в отчёте.
//
// У метки, которой нет в labels, тип один на всех — UNKNOWN. Саму метку
// сервис пишет в лог, но в файл она не попадала: открывший отчёт видел
// «UNKNOWN» и не мог понять, какой бэкап не прошёл, а несколько незнакомых
// меток в одном окружении давали неразличимые строки.
func displayName(b parser.Backup) string {
	if b.Type == parser.TypeUnknown && b.Label != "" {
		return b.Type + " (" + b.Label + ")"
	}
	return b.Type
}

// Job — итог одной задачи бэкапа за сутки, сведённый по всем нодам.
type Job struct {
	Environment string
	Label       string // исходная метка LABELS — ключ задачи
	Name        string // как показать человеку: тип из карты labels
	OK          bool
	Start, End  time.Time // времена ноды, которая отработала
	Node        string    // нода, сделавшая бэкап; у проваленной задачи пуста
	// Nodes — все ноды, приславшие отчёт, без повторов и по алфавиту.
	// У проваленной задачи это единственный след для разбора.
	Nodes []string
}

func (j Job) HasTimes() bool { return !j.Start.IsZero() && !j.End.IsZero() }

// Aggregate сворачивает сообщения в задачи.
//
// Ключ — пара (окружение, LABELS), а не (окружение, тип): под одним типом
// живут разные бэкапы (MINIO_BACKUPS и MINIO_BACKUPS_AI_BUCKET оба MinIO),
// и склеивание их в одну строку спрятало бы провал одного за успехом другого.
//
// Правило статуса: задача успешна, если хотя бы одна нода отчиталась SUCCESS.
// Бэкапы стоят на кластере — команда выполняется на одной ноде, остальные
// присылают «command was not executed». Считать это отказом значит красить
// в красный штатную работу кластера.
func Aggregate(bs []parser.Backup) []Job {
	type key struct{ env, label string }

	index := make(map[key]*Job, len(bs))
	var order []key

	for _, b := range bs {
		k := key{b.Environment, b.Label}
		j, seen := index[k]
		if !seen {
			j = &Job{Environment: b.Environment, Label: b.Label, Name: displayName(b)}
			index[k] = j
			order = append(order, k)
		}
		// Ноду запоминаем до проверки статуса: у проваленной задачи успеха
		// не будет вовсе, а знать, кто отчитался, всё равно надо.
		if b.Node != "" && !slices.Contains(j.Nodes, b.Node) {
			j.Nodes = append(j.Nodes, b.Node)
		}

		if b.Status != parser.StatusSuccess {
			continue
		}
		switch {
		case !j.OK:
			j.OK = true
			j.Start, j.End, j.Node = b.Start, b.End, b.Node
		case b.HasTimes() && (!j.HasTimes() || b.Start.Before(j.Start)):
			// Отчитались несколько нод — оставляем ту, что начала раньше.
			j.Start, j.End, j.Node = b.Start, b.End, b.Node
		}
	}

	jobs := make([]Job, 0, len(order))
	for _, k := range order {
		j := index[k]
		slices.Sort(j.Nodes) // порядок сообщений в канале не должен влиять на отчёт
		jobs = append(jobs, *j)
	}
	// Порядок не зависит от порядка сообщений: строка на том же месте, что и вчера.
	slices.SortFunc(jobs, func(a, b Job) int {
		return cmp.Or(
			// Провалы наверх: первая строка сразу отвечает, всё ли прошло.
			cmp.Compare(sortRank(a.OK), sortRank(b.OK)),
			cmp.Compare(a.Environment, b.Environment),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Label, b.Label),
		)
	})
	return jobs
}

// sortRank ставит провал (false) перед успехом (true).
func sortRank(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

// SummaryRows — таблица «всё ли прошло»: одна строка на задачу.
func SummaryRows(jobs []Job) [][]any {
	rows := newTable(summaryHeader, len(jobs))
	for _, j := range jobs {
		mark := MarkFail
		if j.OK {
			mark = MarkOK
		}
		// У успеха — нода, что отработала; у провала — все, кто отчитался.
		node := j.Node
		if !j.OK {
			node = strings.Join(j.Nodes, ", ")
		}
		start, end, dur := timeCells(j.Start, j.End, j.HasTimes(), summaryTimeLayout)
		rows = append(rows, []any{
			j.Environment, j.Name, mark, start, end, dur, cmp.Or(node, noValue),
		})
	}
	return rows
}

// DetailRows — сырьё построчно, по строке на разобранное сообщение.
//
// Сортировка стабильная: бэкапы стартуют по крону пачкой в одну секунду,
// и нестабильная переставляла бы их от запуска к запуску. Строки без времён
// уходят в конец. Входной срез не меняется — отсюда Clone.
func DetailRows(bs []parser.Backup) [][]any {
	sorted := slices.Clone(bs)
	slices.SortStableFunc(sorted, func(a, b parser.Backup) int {
		switch {
		case !a.HasTimes() && !b.HasTimes():
			return 0
		case !a.HasTimes():
			return 1
		case !b.HasTimes():
			return -1
		default:
			return a.Start.Compare(b.Start)
		}
	})

	rows := newTable(detailHeader, len(sorted))
	for _, b := range sorted {
		start, end, dur := timeCells(b.Start, b.End, b.HasTimes(), detailTimeLayout)
		rows = append(rows, []any{
			b.Environment, cmp.Or(b.Node, noValue), displayName(b), b.Status, start, end, dur,
		})
	}
	return rows
}

// newTable заводит таблицу с заголовком и местом под rowCount строк.
func newTable(header []string, rowCount int) [][]any {
	head := make([]any, len(header))
	for i, h := range header {
		head[i] = h
	}
	rows := make([][]any, 0, rowCount+1)
	return append(rows, head)
}

// timeCells форматирует тройку Start/End/Duration. Без времён все три пустые:
// у задачи, которую никто не выполнил, длительности не существует.
func timeCells(start, end time.Time, has bool, layout string) (string, string, string) {
	if !has {
		return "", "", ""
	}
	return start.Format(layout), end.Format(layout), humanDuration(end.Sub(start))
}

func ReportName(day time.Time) string {
	return namePrefix + day.Format(DateLayout)
}

// reportDateFromName достаёт дату из имени файла. Зона обязательна: time.Parse
// вернул бы полночь UTC, а сравнивают дату с границей хранения, которая
// считается в зоне из конфига. Западнее UTC разница в сутки удаления.
func reportDateFromName(name string, loc *time.Location) (time.Time, bool) {
	if !strings.HasPrefix(name, namePrefix) {
		return time.Time{}, false
	}
	day, err := time.ParseInLocation(DateLayout, strings.TrimPrefix(name, namePrefix), loc)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

// Cutoff — граница хранения: отчёты за дни строго раньше неё подлежат удалению.
func Cutoff(now time.Time, retentionDays int, loc *time.Location) time.Time {
	return dates.StartOfDay(now, loc).AddDate(0, 0, -retentionDays)
}

// ShouldDelete решает судьбу файла по его имени, а не по modifiedTime:
// последний меняется, когда кто-то просто открыл отчёт. Чужие файлы и наши
// с испорченной датой не трогаем.
func ShouldDelete(name string, cutoff time.Time) bool {
	// Зону берём у границы: Cutoff строит её в зоне из конфига.
	day, ok := reportDateFromName(name, cutoff.Location())
	if !ok {
		return false
	}
	return day.Before(cutoff)
}
