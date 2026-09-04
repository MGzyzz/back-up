package parser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Карта меток из конфига. Пять значений известны от постановщика,
// три добавлены после выгрузки реального канала спайком.
var testLabels = map[string]string{
	"PGBACKREST":                 "PostgreSQL",
	"PGBACKREST_BACKUP":          "PostgreSQL",
	"PGBACKREST_CLOUD_BACKUP":    "PostgreSQL",
	"POSTGRES-LASTBACKUP":        "PostgreSQL",
	"MONGODB_BACKUP":             "MongoDB",
	"MONGODB_BACKUP_REMOTE_REPO": "MongoDB",
	"MINIO_BACKUPS":              "MinIO",
	"MINIO_BACKUPS_AI_BUCKET":    "MinIO",
}

// Фиксированная зона вместо time.LoadLocation: тест не зависит от tzdata на машине.
var testLoc = time.FixedZone("+05", 5*60*60)

// load читает фикстуру — настоящее сообщение, выгруженное из канала.
func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("прочитать фикстуру: %v", err)
	}
	return string(b)
}

func at(hh, mm, ss int) time.Time {
	return time.Date(2026, 8, 28, hh, mm, ss, 0, testLoc)
}

func TestSplitFields(t *testing.T) {
	// Реальное сообщение: обрати внимание на NODE с пустым значением
	// и на время, внутри которого есть свои двоеточия.
	fields := splitFields(load(t, "restore.txt"))

	want := map[string]string{
		"STATUS":                "✅ SUCCESS",
		"ENVIRONMENT":           "❗️❗️KT❗️❗️",
		"ALERT":                 "LAST BACKUP RESTORE COMPLETED",
		"LABELS":                "POSTGRES-LASTBACKUP",
		"NODE":                  "",
		"RESTORE START TIME":    "28-08-26_06:00:02",
		"RESTORE FINISHED TIME": "28-08-26_07:44:08",
	}
	for k, w := range want {
		got, ok := fields[k]
		if !ok {
			t.Errorf("ключ %q не найден", k)
			continue
		}
		if got != w {
			t.Errorf("fields[%q] = %q, ожидал %q", k, got, w)
		}
	}
	if len(fields) != len(want) {
		t.Errorf("ключей %d, ожидал %d: %v", len(fields), len(want), fields)
	}
}

func TestSplitFieldsSkipsLinesWithoutColon(t *testing.T) {
	fields := splitFields(load(t, "foreign_plain.txt"))
	if len(fields) != 0 {
		t.Errorf("ожидал пустую карту, получил %v", fields)
	}
}

func TestParse(t *testing.T) {
	// Дата доставки сообщения: нужна там, где времён в тексте нет.
	received := at(9, 28, 3)

	tests := []struct {
		name    string
		file    string    // фикстура из testdata; если пусто — берётся text
		text    string    // синтетический случай
		date    time.Time // дата сообщения в Telegram
		want    Backup
		wantErr error // nil = ошибки быть не должно
		anyErr  bool  // true = ошибка нужна, конкретная не важна
	}{
		{
			name: "minio success с нодой",
			file: "success_minio.txt",
			date: received,
			want: Backup{
				Environment: "KT",
				Node:        "kt-minio03",
				Type:        "MinIO",
				Status:      StatusSuccess,
				Start:       at(1, 0, 2),
				End:         at(7, 31, 4),
			},
		},
		{
			name: "pgbackrest success, лишнее поле MODE не мешает",
			file: "success_pgbackrest.txt",
			date: received,
			want: Backup{
				Environment: "KT",
				Node:        "kt-postgres03",
				Type:        "PostgreSQL",
				Status:      StatusSuccess,
				Start:       at(1, 0, 3),
				End:         at(5, 20, 52),
			},
		},
		{
			// Главный случай, найденный спайком: 40% уведомлений в канале.
			// Времён нет вовсе, день берётся из даты сообщения.
			name: "error без времён",
			file: "error_no_times.txt",
			date: received,
			want: Backup{
				Environment: "KT",
				Node:        "kt-backup01",
				Type:        "PostgreSQL",
				Status:      StatusError,
				Start:       time.Time{},
				End:         time.Time{},
			},
		},
		{
			name:    "проверка восстановления — не бэкап",
			file:    "restore.txt",
			date:    received,
			wantErr: ErrNotBackupMessage,
		},
		{
			name:    "чужое уведомление с двоеточием в тексте",
			file:    "foreign.txt",
			date:    received,
			wantErr: ErrNotBackupMessage,
		},
		{
			name:    "чужое уведомление без двоеточия",
			file:    "foreign_plain.txt",
			date:    received,
			wantErr: ErrNotBackupMessage,
		},
		{
			name: "неизвестный LABELS не роняет разбор",
			text: "STATUS: ✅ SUCCESS\n" +
				"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
				"LABELS: CLICKHOUSE_BACKUP\n" +
				"NODE: kt-ch01\n" +
				"BACKUP START TIME: 28-08-26_01:00:00\n" +
				"BACKUP FINISHED TIME: 28-08-26_01:30:00",
			date: received,
			want: Backup{
				Environment: "KT",
				Node:        "kt-ch01",
				Type:        TypeUnknown,
				Status:      StatusSuccess,
				Start:       at(1, 0, 0),
				End:         at(1, 30, 0),
			},
		},
		{
			name: "окружение из двух слов, нода отсутствует",
			text: "STATUS: ❌ FAILED\n" +
				"ENVIRONMENT: ❗️❗️PROD KEGOC❗️❗️\n" +
				"LABELS: MONGODB_BACKUP\n" +
				"BACKUP START TIME: 28-08-26_02:00:00\n" +
				"BACKUP FINISHED TIME: 28-08-26_02:10:00",
			date: received,
			want: Backup{
				Environment: "PROD KEGOC",
				Node:        "",
				Type:        "MongoDB",
				Status:      StatusFailed,
				Start:       at(2, 0, 0),
				End:         at(2, 10, 0),
			},
		},
		{
			// Найдено на живом прогоне: ключ есть, значения нет.
			// Приравниваем к отсутствию времён, а не к ошибке разбора.
			name: "пустое BACKUP START TIME",
			text: "STATUS: 🔥  BACKUP error\n" +
				"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
				"LABELS: MINIO_BACKUPS\n" +
				"NODE: kt-minio02\n" +
				"BACKUP START TIME:\n" +
				"BACKUP FINISHED TIME:",
			date: received,
			want: Backup{
				Environment: "KT",
				Node:        "kt-minio02",
				Type:        "MinIO",
				Status:      StatusError,
			},
		},
		{
			name: "битое время начала",
			text: "STATUS: ✅ SUCCESS\n" +
				"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
				"LABELS: MINIO_BACKUPS\n" +
				"BACKUP START TIME: вчера ночью\n" +
				"BACKUP FINISHED TIME: 28-08-26_02:23:20",
			date:   received,
			anyErr: true,
		},
		{
			name: "конец раньше начала",
			text: "STATUS: ✅ SUCCESS\n" +
				"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
				"LABELS: MINIO_BACKUPS\n" +
				"BACKUP START TIME: 28-08-26_02:00:00\n" +
				"BACKUP FINISHED TIME: 28-08-26_01:00:00",
			date:   received,
			anyErr: true,
		},
		{
			name: "незнакомый статус — сигнал, что формат изменился",
			text: "STATUS: 🤷 WHO KNOWS\n" +
				"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
				"LABELS: MINIO_BACKUPS\n" +
				"BACKUP START TIME: 28-08-26_01:00:00\n" +
				"BACKUP FINISHED TIME: 28-08-26_01:30:00",
			date:   received,
			anyErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := tt.text
			if tt.file != "" {
				text = load(t, tt.file)
			}

			got, err := Parse(RawMessage{Date: tt.date, Text: text}, testLabels, testLoc)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ожидал ошибку %v, получил %v", tt.wantErr, err)
				}
				return
			case tt.anyErr:
				if err == nil {
					t.Fatal("ожидал ошибку, получил nil")
				}
				return
			case err != nil:
				t.Fatalf("неожиданная ошибка: %v", err)
			}

			if got.Environment != tt.want.Environment {
				t.Errorf("Environment = %q, ожидал %q", got.Environment, tt.want.Environment)
			}
			if got.Node != tt.want.Node {
				t.Errorf("Node = %q, ожидал %q", got.Node, tt.want.Node)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, ожидал %q", got.Type, tt.want.Type)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, ожидал %q", got.Status, tt.want.Status)
			}
			if !got.Start.Equal(tt.want.Start) {
				t.Errorf("Start = %v, ожидал %v", got.Start, tt.want.Start)
			}
			if !got.End.Equal(tt.want.End) {
				t.Errorf("End = %v, ожидал %v", got.End, tt.want.End)
			}
		})
	}
}

func TestBackupDuration(t *testing.T) {
	b := Backup{Start: at(1, 0, 1), End: at(2, 23, 20)}
	if !b.HasTimes() {
		t.Fatal("HasTimes() = false при обоих заданных временах")
	}
	if got, want := b.Duration(), 83*time.Minute+19*time.Second; got != want {
		t.Errorf("Duration() = %v, ожидал %v", got, want)
	}
}

func TestBackupHasTimesFalse(t *testing.T) {
	if (Backup{Status: StatusError}).HasTimes() {
		t.Error("HasTimes() = true при нулевых временах")
	}
}

// Двузначный год разворачивается стандартной библиотекой по правилу
// 69–99 -> 19xx, 00–68 -> 20xx. Фиксируем, чтобы не уехало незамеченным.
func TestTwoDigitYear(t *testing.T) {
	got, err := time.ParseInLocation(tgTimeLayout, "28-08-26_01:00:02", testLoc)
	if err != nil {
		t.Fatalf("разобрать время: %v", err)
	}
	if got.Year() != 2026 {
		t.Errorf("год = %d, ожидал 2026", got.Year())
	}
}

// NODE приходит из того же чужого канала, что и остальные поля, и мусор
// в нём встречается тот же. Нечищеная нода стала бы отдельным элементом
// Job.Nodes и разъехалась в отчёте со своей же строкой без мусора.
func TestParseTrimsNode(t *testing.T) {
	msg := RawMessage{Text: "ENVIRONMENT: PROD\n" +
		"NODE: ❗️kt-minio01❗️\n" +
		"LABELS: MINIO_BACKUPS\n" +
		"STATUS: ✅ SUCCESS\n" +
		"BACKUP START TIME: 28-08-26_01:00:00\n" +
		"BACKUP FINISHED TIME: 28-08-26_01:30:00\n"}

	b, err := Parse(msg, testLabels, testLoc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if b.Node != "kt-minio01" {
		t.Errorf("Node = %q, ожидал %q", b.Node, "kt-minio01")
	}
}
