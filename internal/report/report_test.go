package report

import (
	"reflect"
	"testing"
	"time"

	"backup-report/internal/parser"
)

// Фиксированная зона вместо time.LoadLocation: тест не зависит от tzdata на машине.
var testLoc = time.FixedZone("+05", 5*60*60)

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

func TestBuildRows(t *testing.T) {
	d := func(hh, mm, ss int) time.Time {
		return time.Date(2025, 12, 15, hh, mm, ss, 0, testLoc)
	}
	// Намеренно вперемешку; MinIO и MongoDB стартуют в одну секунду.
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-mongo01", Type: "MongoDB", Status: "FAILED",
			Start: d(1, 0, 1), End: d(1, 11, 26)},
		{Environment: "PROD KPO", Node: "", Type: "PostgreSQL", Status: "SUCCESS",
			Start: d(3, 30, 19), End: d(3, 34, 57)},
		{Environment: "KT", Node: "kt-minio01", Type: "MinIO", Status: "SUCCESS",
			Start: d(1, 0, 1), End: d(2, 23, 20)},
	}

	want := [][]any{
		{"Environment", "Node", "Type", "Status", "Start", "End", "Duration"},
		{"KT", "kt-mongo01", "MongoDB", "FAILED", "2025-12-15 01:00:01", "2025-12-15 01:11:26", "0h 11m"},
		{"KT", "kt-minio01", "MinIO", "SUCCESS", "2025-12-15 01:00:01", "2025-12-15 02:23:20", "1h 23m"},
		{"PROD KPO", "—", "PostgreSQL", "SUCCESS", "2025-12-15 03:30:19", "2025-12-15 03:34:57", "0h 04m"},
	}

	got := BuildRows(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildRows():\nполучил %v\nожидал  %v", got, want)
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
			got, ok := reportDateFromName(tt.file)
			if tt.want == "" {
				if ok {
					t.Fatalf("ожидал ok == false, получил дату %v", got)
				}
				return
			}
			if !ok {
				t.Fatal("ожидал ok == true, получил false")
			}
			if got.Format(dateLayout) != tt.want {
				t.Errorf("дата = %q, ожидал %q", got.Format(dateLayout), tt.want)
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

func TestParseReportAt(t *testing.T) {
	hh, mm, err := ParseReportAt("11:00")
	if err != nil || hh != 11 || mm != 0 {
		t.Errorf(`ParseReportAt("11:00") = %d, %d, %v; ожидал 11, 0, nil`, hh, mm, err)
	}
	if _, _, err := ParseReportAt("25:99"); err == nil {
		t.Error(`ParseReportAt("25:99") = nil error; ожидал ошибку`)
	}
	if _, _, err := ParseReportAt(""); err == nil {
		t.Error(`ParseReportAt("") = nil error; ожидал ошибку`)
	}
}

func TestNextRun(t *testing.T) {
	loc := testLoc
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "до 11:00 — сегодня",
			now:  time.Date(2025, 12, 15, 9, 30, 0, 0, loc),
			want: time.Date(2025, 12, 15, 11, 0, 0, 0, loc),
		},
		{
			name: "после 11:00 — завтра",
			now:  time.Date(2025, 12, 15, 15, 40, 0, 0, loc),
			want: time.Date(2025, 12, 16, 11, 0, 0, 0, loc),
		},
		{
			name: "ровно 11:00 — завтра, чтобы не сработать дважды",
			now:  time.Date(2025, 12, 15, 11, 0, 0, 0, loc),
			want: time.Date(2025, 12, 16, 11, 0, 0, 0, loc),
		},
		{
			name: "переход через конец месяца",
			now:  time.Date(2025, 12, 31, 23, 59, 0, 0, loc),
			want: time.Date(2026, 1, 1, 11, 0, 0, 0, loc),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRun(tt.now, 11, 0, loc); !got.Equal(tt.want) {
				t.Errorf("NextRun(%v) = %v, ожидал %v", tt.now, got, tt.want)
			}
		})
	}
}

// Строка со статусом ERROR: времён нет, три последние колонки пустые,
// и в сортировке она уходит в конец.
func TestBuildRowsErrorWithoutTimes(t *testing.T) {
	d := func(hh, mm, ss int) time.Time {
		return time.Date(2025, 12, 15, hh, mm, ss, 0, testLoc)
	}
	in := []parser.Backup{
		{Environment: "KT", Node: "kt-backup01", Type: "PostgreSQL", Status: parser.StatusError},
		{Environment: "KT", Node: "kt-minio01", Type: "MinIO", Status: parser.StatusSuccess,
			Start: d(1, 0, 1), End: d(2, 23, 20)},
	}

	got := BuildRows(in)
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
