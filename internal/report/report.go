// Package report превращает разобранные бэкапы в таблицы отчёта и отвечает
// за всё, что связано с его именем: как файл назвать и когда его удалять.
//
// Отчёт состоит из двух таблиц. Сводка отвечает на вопрос «всё ли прошло»:
// одна строка на задачу бэкапа, свёрнутая по нодам кластера. Детали — то же
// сырьё построчно, по одной строке на сообщение; туда идут, когда в сводке
// что-то красное.
//
// Пакет чистый: ни сети, ни времени «сейчас» изнутри — момент отсчёта
// всегда приходит параметром, поэтому решения проверяются тестами.
package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"backup-report/internal/parser"
)

// DateLayout — формат даты в имени файла и в логах. Экспортирован, чтобы
// вызывающая сторона не переписывала тот же литерал у себя.
const DateLayout = "2006-01-02"

// Отметки статуса в сводке. Короткое слово, а не эмодзи: сигнал несёт заливка
// строки, а по тексту можно отфильтровать лист штатными средствами Sheets.
const (
	MarkOK   = "OK"
	MarkFail = "FAIL"
)

const (
	namePrefix = "backup-report-"
	// Детали держат полную дату: так её называет ТЗ («Start date & time»),
	// и там же с ней разбираются построчно.
	detailTimeLayout = "2006-01-02 15:04:05"
	// В сводке дата не нужна: она одна на весь файл и уже стоит в его имени.
	// Продублированная в каждой строке дважды, она добавляет 22 символа
	// и ноль информации — ровно то, против чего «минимум букв».
	summaryTimeLayout = "15:04:05"
	noValue           = "—"
)

// Схемы таблиц. Ширину отчёта берут отсюда, а не считают руками.
var (
	summaryHeader = []string{"Environment", "Backup", "Status", "Start", "End", "Duration", "Node"}
	detailHeader  = []string{"Environment", "Node", "Type", "Status", "Start", "End", "Duration"}
)

// SummaryStatusCol и DetailStatusCol — индексы колонки статуса. Нужны тому,
// кто красит строки, чтобы не пересчитывать схему у себя.
const (
	SummaryStatusCol = 2
	DetailStatusCol  = 3
)

func humanDuration(d time.Duration) string {
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}

// Job — итог одной задачи бэкапа за сутки, сведённый по всем нодам.
type Job struct {
	Environment string
	Label       string // исходная метка LABELS — ключ задачи
	Name        string // как показать человеку: тип из карты labels
	OK          bool
	Start, End  time.Time // времена ноды, которая отработала
	// Node — нода, сделавшая бэкап. У проваленной задачи пуста: делать
	// его было некому.
	Node string
	// Nodes — все ноды, приславшие отчёт, без повторов и по алфавиту.
	// У проваленной задачи это единственный след, с которого начинают
	// разбираться, поэтому в сводку идут именно они.
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
			j = &Job{Environment: b.Environment, Label: b.Label, Name: b.Type}
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
	// Порядок предсказуемый и не зависящий от порядка сообщений: человек
	// ищет строку глазами на том же месте, что и вчера.
	slices.SortFunc(jobs, func(a, b Job) int {
		return cmp.Or(
			// Провалы наверх: тогда первая строка отчёта сама отвечает
			// на главный вопрос. Зелёная сверху — прошло всё, читать
			// остальное незачем.
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
		// У успеха показываем ту ноду, что отработала; у провала — все,
		// что отчитались: с них и начинают разбираться.
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

// DetailRows — сырьё построчно, по одной строке на разобранное сообщение.
//
// Сортировка стабильная: бэкапы стартуют по крону пачкой в одну и ту же
// секунду, и нестабильная сортировка переставляла бы их произвольно от
// запуска к запуску. Строки без времён уходят в конец: сортировать их не по чему.
//
// Аргумент не меняется: переупорядочивать чужой срез втихую функция не должна.
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
			b.Environment, cmp.Or(b.Node, noValue), b.Type, b.Status, start, end, dur,
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

func reportDateFromName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, namePrefix) {
		return time.Time{}, false
	}
	day, err := time.Parse(DateLayout, strings.TrimPrefix(name, namePrefix))
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

// Cutoff — граница хранения: отчёты за дни строго раньше неё подлежат удалению.
// now приходит параметром, а не берётся из time.Now(), чтобы правило ретенции
// можно было проверить тестом.
func Cutoff(now time.Time, retentionDays int, loc *time.Location) time.Time {
	n := now.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc).
		AddDate(0, 0, -retentionDays)
}

// ShouldDelete решает судьбу файла по его имени, а не по modifiedTime:
// последний меняется, когда кто-то просто открыл отчёт. Чужие файлы и наши
// с испорченной датой не трогаем.
func ShouldDelete(name string, cutoff time.Time) bool {
	day, ok := reportDateFromName(name)
	if !ok {
		return false
	}
	return day.Before(cutoff)
}
