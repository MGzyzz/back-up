package report

import (
	"reflect"
	"testing"
	"time"

	"backup-report/internal/parser"
)

// Фиксированная зона вместо time.LoadLocation: тест не зависит от tzdata на машине.
var testLoc = time.FixedZone("+05", 5*60*60)

// at — момент внутри тестовых суток. Дата и зона заданы здесь один раз:
// раньше каждый тест заводил своё замыкание с тем же литералом.
func at(hh, mm, ss int) time.Time {
	return time.Date(2025, 12, 15, hh, mm, ss, 0, testLoc)
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"час с минутами", 83*time.Minute + 19*time.Second, "1h 23m"},
		{"меньше часа", 4*time.Minute + 38*time.Second, "0h 04m"},
		{"ровно два часа", 2 * time.Hour, "2h 00m"},
		{"больше суток", 26*time.Hour + 5*time.Minute, "26h 05m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanDuration(tt.d); got != tt.want {
				t.Errorf("humanDuration(%v) = %q, ожидал %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestDetailRows(t *testing.T) {
	// Намеренно вперемешку; MinIO и MongoDB стартуют в одну секунду.
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-mongo01", Type: "MongoDB", Status: "FAILED",
			Start: at(1, 0, 1), End: at(1, 11, 26)},
		{Environment: "PROD KPO", Node: "", Type: "PostgreSQL", Status: "SUCCESS",
			Start: at(3, 30, 19), End: at(3, 34, 57)},
		{Environment: "KT", Node: "kt-minio01", Type: "MinIO", Status: "SUCCESS",
			Start: at(1, 0, 1), End: at(2, 23, 20)},
	}

	want := [][]any{
		{"Environment", "Node", "Type", "Status", "Start", "End", "Duration"},
		{"KT", "kt-mongo01", "MongoDB", "FAILED", "2025-12-15 01:00:01", "2025-12-15 01:11:26", "0h 11m"},
		{"KT", "kt-minio01", "MinIO", "SUCCESS", "2025-12-15 01:00:01", "2025-12-15 02:23:20", "1h 23m"},
		{"PROD KPO", "—", "PostgreSQL", "SUCCESS", "2025-12-15 03:30:19", "2025-12-15 03:34:57", "0h 04m"},
	}

	got := DetailRows(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DetailRows():\nполучил %v\nожидал  %v", got, want)
	}
}

func TestReportDateFromName(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string // "" = ожидаем ok == false
	}{
		{"наш файл", "backup-report-2025-12-15", "2025-12-15"},
		{"чужой файл", "Отчёт за квартал", ""},
		{"наш префикс, битая дата", "backup-report-ноябрь", ""},
		{"наш префикс, несуществующая дата", "backup-report-2025-13-99", ""},
		{"пустое имя", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := reportDateFromName(tt.file, testLoc)
			if tt.want == "" {
				if ok {
					t.Fatalf("ожидал ok == false, получил дату %v", got)
				}
				return
			}
			if !ok {
				t.Fatal("ожидал ok == true, получил false")
			}
			if got.Format(DateLayout) != tt.want {
				t.Errorf("дата = %q, ожидал %q", got.Format(DateLayout), tt.want)
			}
		})
	}
}

func TestShouldDelete(t *testing.T) {
	cutoff := time.Date(2025, 11, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"свежий отчёт", "backup-report-2025-12-15", false},
		{"в пределах хранения", "backup-report-2025-11-20", false},
		{"ровно на границе — оставляем", "backup-report-2025-11-15", false},
		{"старый отчёт", "backup-report-2025-11-01", true},
		{"посторонний файл не трогаем", "Отчёт за квартал", false},
		{"наш префикс с битой датой не трогаем", "backup-report-ноябрь", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldDelete(tt.file, cutoff); got != tt.want {
				t.Errorf("ShouldDelete(%q) = %v, ожидал %v", tt.file, got, tt.want)
			}
		})
	}
}

// Граница хранения не должна зависеть от знака смещения зоны. Пока дата
// из имени разбиралась в UTC, а граница строилась в зоне конфига, отчёт
// ровно retention_days возраста западнее UTC удалялся на сутки раньше.
func TestShouldDeleteAcrossZones(t *testing.T) {
	zones := []struct {
		name   string
		offset int // часы от UTC
	}{
		{"восточнее UTC", +5},
		{"UTC", 0},
		{"западнее UTC", -4},
	}
	for _, z := range zones {
		t.Run(z.name, func(t *testing.T) {
			loc := time.FixedZone(z.name, z.offset*60*60)
			now := time.Date(2025, 12, 15, 11, 0, 0, 0, loc)
			cutoff := Cutoff(now, 30, loc)

			onEdge := ReportName(now.AddDate(0, 0, -30))
			if ShouldDelete(onEdge, cutoff) {
				t.Errorf("%s: отчёт ровно на границе хранения удалён, ожидал оставить", onEdge)
			}
			older := ReportName(now.AddDate(0, 0, -31))
			if !ShouldDelete(older, cutoff) {
				t.Errorf("%s: отчёт старше границы оставлен, ожидал удалить", older)
			}
		})
	}
}

// Строка со статусом ERROR: времён нет, три последние колонки пустые,
// и в сортировке она уходит в конец.
func TestDetailRowsErrorWithoutTimes(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-backup01", Type: "PostgreSQL", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-minio01", Type: "MinIO", Status: parser.StatusSuccess,
			Start: at(1, 0, 1), End: at(2, 23, 20)},
	}

	got := DetailRows(in)
	if len(got) != 3 {
		t.Fatalf("строк %d, ожидал 3 (заголовок + две)", len(got))
	}
	if got[1][1] != "kt-minio01" {
		t.Errorf("первой строкой ожидал бэкап со временем, получил %v", got[1])
	}
	last := got[2]
	if last[3] != parser.StatusError {
		t.Errorf("Status = %v, ожидал ERROR", last[3])
	}
	for i, name := range map[int]string{4: "Start", 5: "End", 6: "Duration"} {
		if last[i] != "" {
			t.Errorf("%s = %q, ожидал пустую строку", name, last[i])
		}
	}
}

func TestCutoff(t *testing.T) {
	now := time.Date(2025, 12, 15, 11, 30, 0, 0, testLoc)
	got := Cutoff(now, 30, testLoc)
	want := time.Date(2025, 11, 15, 0, 0, 0, 0, testLoc)
	if !got.Equal(want) {
		t.Errorf("Cutoff = %v, ожидал %v", got, want)
	}
	// Момент внутри суток не должен влиять на границу: она всегда полночь.
	late := time.Date(2025, 12, 15, 23, 59, 59, 0, testLoc)
	if !Cutoff(late, 30, testLoc).Equal(want) {
		t.Errorf("граница уехала от времени суток: %v", Cutoff(late, 30, testLoc))
	}
}

// BuildRows не имеет права переупорядочивать срез вызывающей стороны.
func TestDetailRowsDoesNotMutateInput(t *testing.T) {
	in := []parser.Backup{
		{Node: "поздний", Status: parser.StatusSuccess, Start: at(5, 0, 0), End: at(6, 0, 0)},
		{Node: "ранний", Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)},
	}

	DetailRows(in)

	if in[0].Node != "поздний" || in[1].Node != "ранний" {
		t.Errorf("DetailRows переставила элементы аргумента: %v", in)
	}
}

// Правило кластера: бэкап сделан хотя бы одной нодой — задача успешна,
// а остальные «команда не выполнялась» отказом не считаются.
func TestAggregateClusterSuccess(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-minio01", Label: "MINIO_BACKUPS", Type: "MinIO", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-minio02", Label: "MINIO_BACKUPS", Type: "MinIO", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-minio03", Label: "MINIO_BACKUPS", Type: "MinIO",
			Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 23, 0)},
	}

	jobs := Aggregate(in)

	if len(jobs) != 1 {
		t.Fatalf("задач %d, ожидал 1: три ноды одного кластера — одна задача", len(jobs))
	}
	j := jobs[0]
	if !j.OK {
		t.Error("задача не успешна, хотя одна нода отчиталась SUCCESS")
	}
	if j.Node != "kt-minio03" {
		t.Errorf("Node = %q, ожидал ноду, которая отработала", j.Node)
	}
	if !reflect.DeepEqual(j.Nodes, []string{"kt-minio01", "kt-minio02", "kt-minio03"}) {
		t.Errorf("Nodes = %v, ожидал все три ноды по алфавиту", j.Nodes)
	}
	if !j.Start.Equal(at(1, 0, 0)) || !j.End.Equal(at(2, 23, 0)) {
		t.Errorf("времена %v–%v, ожидал времена успешной ноды", j.Start, j.End)
	}
}

// Ключ — LABELS, а не тип: иначе провал одного MinIO-бэкапа спрятался бы
// за успехом другого.
func TestAggregateKeepsLabelsApart(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-minio01", Label: "MINIO_BACKUPS", Type: "MinIO",
			Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)},
		{Environment: "KT", Node: "kt-minio01", Label: "MINIO_BACKUPS_AI_BUCKET", Type: "MinIO AI",
			Status: parser.StatusError},
	}

	jobs := Aggregate(in)

	if len(jobs) != 2 {
		t.Fatalf("задач %d, ожидал 2: разные LABELS — разные бэкапы", len(jobs))
	}
	byName := map[string]bool{}
	for _, j := range jobs {
		byName[j.Name] = j.OK
	}
	if !byName["MinIO"] {
		t.Error("MinIO должен быть успешен")
	}
	if byName["MinIO AI"] {
		t.Error("MinIO AI провалился, но помечен успешным — провал спрятался за соседом")
	}
}

// Никто не отчитался успехом — задача провалена.
func TestAggregateAllErrorsIsFailure(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-pg01", Label: "PGBACKREST_BACKUP", Type: "PostgreSQL", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-pg02", Label: "PGBACKREST_BACKUP", Type: "PostgreSQL", Status: parser.StatusFailed},
	}

	jobs := Aggregate(in)

	if len(jobs) != 1 || jobs[0].OK {
		t.Fatalf("ожидал одну проваленную задачу, получил %+v", jobs)
	}
	if jobs[0].HasTimes() {
		t.Error("у задачи без успеха не должно быть времён")
	}
}

// Порядок строк не должен зависеть от порядка сообщений в канале:
// человек ищет строку глазами на том же месте, что и вчера.
func TestAggregateOrderIsStable(t *testing.T) {
	in := []parser.Backup{
		{Environment: "PROD KEGOC", Label: "MONGODB_BACKUP", Type: "MongoDB", Status: parser.StatusSuccess},
		{Environment: "KT", Label: "PGBACKREST_BACKUP", Type: "PostgreSQL", Status: parser.StatusSuccess},
		{Environment: "KT", Label: "MINIO_BACKUPS", Type: "MinIO", Status: parser.StatusSuccess},
	}

	var got []string
	for _, j := range Aggregate(in) {
		got = append(got, j.Environment+"/"+j.Name)
	}

	want := []string{"KT/MinIO", "KT/PostgreSQL", "PROD KEGOC/MongoDB"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("порядок %v, ожидал %v", got, want)
	}
}

func TestSummaryRows(t *testing.T) {
	jobs := []Job{
		{Environment: "KT", Name: "MinIO", OK: true, Start: at(1, 0, 0), End: at(2, 23, 0), Node: "kt-minio03"},
		{Environment: "KT", Name: "PostgreSQL Cloud", OK: false},
	}

	got := SummaryRows(jobs)

	want := [][]any{
		{"Environment", "Backup", "Status", "Start", "End", "Duration", "Node"},
		// Порядок как на входе: сортировка — забота Aggregate, не SummaryRows.
		{"KT", "MinIO", MarkOK, "01:00:00", "02:23:00", "1h 23m", "kt-minio03"},
		{"KT", "PostgreSQL Cloud", MarkFail, "", "", "", "—"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SummaryRows():\nполучил %v\nожидал  %v", got, want)
	}
	// Колонка статуса должна стоять там, где её ищет покраска.
	if got[0][SummaryStatusCol] != "Status" {
		t.Errorf("SummaryStatusCol = %d указывает на %v, а не на Status", SummaryStatusCol, got[0][SummaryStatusCol])
	}
}

func TestDetailStatusColMatchesHeader(t *testing.T) {
	rows := DetailRows(nil)
	if rows[0][DetailStatusCol] != "Status" {
		t.Errorf("DetailStatusCol = %d указывает на %v, а не на Status", DetailStatusCol, rows[0][DetailStatusCol])
	}
}

// У проваленной задачи в сводке обязаны стоять ноды, которые отчитались:
// именно с них человек начинает разбираться. Пустая клетка бесполезна.
func TestSummaryShowsNodesOnFailure(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KPO PROD", Node: "kpo-postgres03", Label: "PGBACKREST_BACKUP",
			Type: "PostgreSQL", Status: parser.StatusError},
		{Environment: "KPO PROD", Node: "kpo-postgres01", Label: "PGBACKREST_BACKUP",
			Type: "PostgreSQL", Status: parser.StatusError},
		// Повтор с той же ноды не должен дублироваться в списке.
		{Environment: "KPO PROD", Node: "kpo-postgres01", Label: "PGBACKREST_BACKUP",
			Type: "PostgreSQL", Status: parser.StatusError},
	}

	rows := SummaryRows(Aggregate(in))

	if len(rows) != 2 {
		t.Fatalf("строк %d, ожидал 2", len(rows))
	}
	if got, want := rows[1][2], MarkFail; got != want {
		t.Errorf("статус = %v, ожидал %v", got, want)
	}
	if got, want := rows[1][6], "kpo-postgres01, kpo-postgres03"; got != want {
		t.Errorf("Node = %q, ожидал %q", got, want)
	}
}

// У успеха колонка остаётся про ту ноду, что реально отработала,
// а не про весь кластер.
func TestSummaryShowsWinningNodeOnSuccess(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-minio01", Label: "MINIO_BACKUPS", Type: "MinIO", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-minio03", Label: "MINIO_BACKUPS", Type: "MinIO",
			Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)},
	}

	rows := SummaryRows(Aggregate(in))

	if got, want := rows[1][6], "kt-minio03"; got != want {
		t.Errorf("Node = %q, ожидал %q — только отработавшую ноду", got, want)
	}
}

// Ноды нет ни в одном сообщении — честное «—», а не пустая клетка.
func TestSummaryNoNodeAtAll(t *testing.T) {
	in := []parser.Backup{
		{Environment: "KT", Label: "MONGODB_BACKUP", Type: "MongoDB", Status: parser.StatusError},
	}
	if got := SummaryRows(Aggregate(in))[1][6]; got != "—" {
		t.Errorf("Node = %q, ожидал «—»", got)
	}
}

// Провалы идут первыми: первая строка отчёта сама отвечает на вопрос
// «всё ли прошло», читать остальное не требуется.
func TestAggregatePutsFailuresFirst(t *testing.T) {
	in := []parser.Backup{
		{Environment: "AMANAT PROD", Label: "MINIO_BACKUPS", Type: "MinIO",
			Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)},
		{Environment: "KT", Label: "PGBACKREST_BACKUP", Type: "PostgreSQL", Status: parser.StatusError},
		{Environment: "KT", Label: "MINIO_BACKUPS", Type: "MinIO",
			Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)},
		{Environment: "AMANAT PROD", Label: "MONGODB_BACKUP", Type: "MongoDB", Status: parser.StatusFailed},
	}

	var got []string
	for _, j := range Aggregate(in) {
		got = append(got, j.Environment+"/"+j.Name)
	}

	// Сначала оба провала по алфавиту, потом оба успеха по алфавиту.
	want := []string{
		"AMANAT PROD/MongoDB", "KT/PostgreSQL",
		"AMANAT PROD/MinIO", "KT/MinIO",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("порядок %v, ожидал %v", got, want)
	}
}

// В сводке дата не пишется — она одна на файл и стоит в его имени.
// На деталях, наоборот, остаётся: так требует ТЗ.
func TestTimeLayoutDiffersBetweenSheets(t *testing.T) {
	in := []parser.Backup{{Environment: "KT", Node: "n", Label: "MINIO_BACKUPS", Type: "MinIO",
		Status: parser.StatusSuccess, Start: at(1, 0, 0), End: at(2, 0, 0)}}

	if got := SummaryRows(Aggregate(in))[1][3]; got != "01:00:00" {
		t.Errorf("сводка: Start = %v, ожидал 01:00:00 без даты", got)
	}
	if got := DetailRows(in)[1][4]; got != "2025-12-15 01:00:00" {
		t.Errorf("детали: Start = %v, ожидал полную дату", got)
	}
}
