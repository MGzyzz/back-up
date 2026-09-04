package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"backup-report/internal/gsheets"
	"backup-report/internal/parser"
	"backup-report/internal/report"
)

var testLoc = time.FixedZone("+05", 5*60*60)

// fakeSource и fakeSink подменяют Telegram и Google Drive
type fakeSource struct {
	msgs []parser.RawMessage
	err  error
	days []time.Time // за какие сутки спрашивали
}

func (f *fakeSource) FetchDay(_ context.Context, day time.Time) ([]parser.RawMessage, error) {
	f.days = append(f.days, day)
	return f.msgs, f.err
}

type fakeSink struct {
	files []gsheets.File

	listErr    error
	publishErr error
	trashErr   error

	published []string          // имена опубликованных файлов
	tabs      [][]gsheets.Sheet // листы каждой публикации
	trashed   []string          // id отправленных в корзину
}

func (f *fakeSink) List(context.Context) ([]gsheets.File, error) {
	return f.files, f.listErr
}

func (f *fakeSink) Publish(_ context.Context, name string, tabs []gsheets.Sheet) (string, error) {
	if f.publishErr != nil {
		return "", f.publishErr
	}
	f.published = append(f.published, name)
	f.tabs = append(f.tabs, tabs)
	return "new-id", nil
}

// sheet достаёт лист по имени из последней публикации.
func (f *fakeSink) sheet(t *testing.T, title string) gsheets.Sheet {
	t.Helper()
	if len(f.tabs) == 0 {
		t.Fatal("не было ни одной публикации")
	}
	for _, s := range f.tabs[len(f.tabs)-1] {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("лист %q не опубликован", title)
	return gsheets.Sheet{}
}

func (f *fakeSink) Trash(_ context.Context, id string) error {
	if f.trashErr != nil {
		return f.trashErr
	}
	f.trashed = append(f.trashed, id)
	return nil
}

func newApp(src Source, sink Sink) *App {
	return New(Options{
		Labels:        map[string]string{"MINIO_BACKUPS": "MinIO"},
		Location:      testLoc,
		RetentionDays: 30,
	}, src, sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// reportNameDaysAgo — имя отчёта за день, отстоящий от сегодня на days назад.
// Считается от time.Now(), потому что чистку сравнивают с настоящим «сейчас».
func reportNameDaysAgo(days int) string {
	return "backup-report-" + time.Now().In(testLoc).AddDate(0, 0, -days).Format("2006-01-02")
}

func msg(text string) parser.RawMessage {
	return parser.RawMessage{Date: time.Date(2026, 8, 28, 9, 0, 0, 0, testLoc), Text: text}
}

const okMessage = "STATUS: ✅ SUCCESS\n" +
	"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
	"LABELS: MINIO_BACKUPS\n" +
	"NODE: kt-minio01\n" +
	"BACKUP START TIME: 28-08-26_01:00:01\n" +
	"BACKUP FINISHED TIME: 28-08-26_02:23:20"

func day() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, testLoc) }

func TestOncePublishesReport(t *testing.T) {
	src := &fakeSource{msgs: []parser.RawMessage{msg(okMessage), msg("что-то чужое без полей")}}
	sink := &fakeSink{}

	if err := newApp(src, sink).Once(context.Background(), day(), false); err != nil {
		t.Fatalf("Once: %v", err)
	}

	if len(sink.published) != 1 || sink.published[0] != "backup-report-2026-08-28" {
		t.Fatalf("опубликовано %v, ожидал один backup-report-2026-08-28", sink.published)
	}

	// Оба листа на месте, «Сводка» первой: её открывают по умолчанию.
	tabs := sink.tabs[0]
	if len(tabs) != 2 || tabs[0].Title != "Сводка" || tabs[1].Title != "Детали" {
		t.Fatalf("листы %v, ожидал [Сводка Детали]", tabs)
	}
	// Заголовок + одна задача; чужое сообщение в отчёт не попало.
	if got := len(sink.sheet(t, "Сводка").Rows); got != 2 {
		t.Errorf("строк в сводке %d, ожидал 2 (заголовок + задача)", got)
	}
}

// Главное следствие фидбека: кластер шлёт один SUCCESS и несколько
// «command was not executed». Задача при этом успешна, строка в сводке одна.
func TestOnceCollapsesClusterIntoOneSuccess(t *testing.T) {
	node := func(name, status string) parser.RawMessage {
		return msg("STATUS: " + status + "\n" +
			"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
			"LABELS: MINIO_BACKUPS\n" +
			"NODE: " + name + "\n" +
			"BACKUP START TIME: 28-08-26_01:00:01\n" +
			"BACKUP FINISHED TIME: 28-08-26_02:23:20")
	}
	src := &fakeSource{msgs: []parser.RawMessage{
		{Date: node("kt-minio01", "🔥 BACKUP error").Date,
			Text: "STATUS: 🔥 BACKUP error\nENVIRONMENT: ❗️❗️KT❗️❗️\nLABELS: MINIO_BACKUPS\nNODE: kt-minio01"},
		{Date: node("kt-minio02", "🔥 BACKUP error").Date,
			Text: "STATUS: 🔥 BACKUP error\nENVIRONMENT: ❗️❗️KT❗️❗️\nLABELS: MINIO_BACKUPS\nNODE: kt-minio02"},
		node("kt-minio03", "✅ SUCCESS"),
	}}
	sink := &fakeSink{}

	if err := newApp(src, sink).Once(context.Background(), day(), false); err != nil {
		t.Fatalf("Once: %v", err)
	}

	summary := sink.sheet(t, "Сводка").Rows
	if len(summary) != 2 {
		t.Fatalf("строк в сводке %d, ожидал 2 (заголовок + одна задача): %v", len(summary), summary)
	}
	if got := summary[1][report.SummaryStatusCol]; got != report.MarkOK {
		t.Errorf("статус задачи = %v, ожидал %v: бэкап сделан одной нодой — это успех", got, report.MarkOK)
	}
	if got := summary[1][6]; got != "kt-minio03" {
		t.Errorf("нода = %v, ожидал kt-minio03 — ту, что отработала", got)
	}
	// Все три сообщения обязаны остаться в деталях.
	if got := len(sink.sheet(t, "Детали").Rows); got != 4 {
		t.Errorf("строк в деталях %d, ожидал 4 (заголовок + три ноды)", got)
	}
}

// Ключевая защита от дубликатов: отчёт за день уже лежит в папке.
func TestOnceSkipsPublishWhenReportExists(t *testing.T) {
	sink := &fakeSink{files: []gsheets.File{{ID: "old", Name: "backup-report-2026-08-28"}}}

	if err := newApp(&fakeSource{msgs: []parser.RawMessage{msg(okMessage)}}, sink).
		Once(context.Background(), day(), false); err != nil {
		t.Fatalf("Once: %v", err)
	}

	if len(sink.published) != 0 {
		t.Errorf("опубликован дубликат: %v", sink.published)
	}
}

func TestOnceRemovesOnlyExpiredReports(t *testing.T) {
	// Граница при retention 30 и «сегодня» = дата запуска теста; берём
	// заведомо старый и заведомо свежий файлы.
	old, fresh := reportNameDaysAgo(40), reportNameDaysAgo(2)
	sink := &fakeSink{files: []gsheets.File{
		{ID: "old", Name: old},
		{ID: "fresh", Name: fresh},
		{ID: "alien", Name: "Отчёт за квартал"},
	}}

	if err := newApp(&fakeSource{}, sink).Once(context.Background(), day(), false); err != nil {
		t.Fatalf("Once: %v", err)
	}

	if len(sink.trashed) != 1 || sink.trashed[0] != "old" {
		t.Errorf("в корзину ушли %v, ожидал только [old]", sink.trashed)
	}
}

func TestOnceDryRunKeepsFiles(t *testing.T) {
	old := reportNameDaysAgo(40)
	sink := &fakeSink{files: []gsheets.File{{ID: "old", Name: old}}}

	if err := newApp(&fakeSource{}, sink).Once(context.Background(), day(), true); err != nil {
		t.Fatalf("Once: %v", err)
	}

	if len(sink.trashed) != 0 {
		t.Errorf("dry-run удалил файлы: %v", sink.trashed)
	}
	if len(sink.published) != 1 {
		t.Errorf("dry-run обязан публиковать отчёт, опубликовано %v", sink.published)
	}
}

// Неудачное удаление одного файла не должно обесценивать построенный отчёт.
func TestOnceSurvivesTrashFailure(t *testing.T) {
	old := reportNameDaysAgo(40)
	sink := &fakeSink{
		files:    []gsheets.File{{ID: "old", Name: old}},
		trashErr: errors.New("Drive недоступен"),
	}

	if err := newApp(&fakeSource{}, sink).Once(context.Background(), day(), false); err != nil {
		t.Errorf("Once вернула ошибку из-за неудачной чистки: %v", err)
	}
	if len(sink.published) != 1 {
		t.Errorf("отчёт не опубликован: %v", sink.published)
	}
}

func TestOnceFailsOnFetchError(t *testing.T) {
	boom := errors.New("канал недоступен")
	err := newApp(&fakeSource{err: boom}, &fakeSink{}).Once(context.Background(), day(), false)

	if !errors.Is(err, boom) {
		t.Fatalf("ошибка = %v, ожидал обёртку над %v", err, boom)
	}
}

// Публикация не удалась — чистку не запускаем: сначала отчёт, потом уборка.
func TestOnceDoesNotCleanupAfterPublishFailure(t *testing.T) {
	old := reportNameDaysAgo(40)
	boom := errors.New("Sheets недоступен")
	sink := &fakeSink{
		files:      []gsheets.File{{ID: "old", Name: old}},
		publishErr: boom,
	}

	if err := newApp(&fakeSource{}, sink).Once(context.Background(), day(), false); !errors.Is(err, boom) {
		t.Fatalf("ошибка = %v, ожидал %v", err, boom)
	}
	if len(sink.trashed) != 0 {
		t.Errorf("чистка пошла после провала публикации: %v", sink.trashed)
	}
}

func TestOnceFailsOnListError(t *testing.T) {
	boom := errors.New("листинг не удался")
	err := newApp(&fakeSource{}, &fakeSink{listErr: boom}).Once(context.Background(), day(), false)

	if !errors.Is(err, boom) {
		t.Fatalf("ошибка = %v, ожидал %v", err, boom)
	}
}

func TestParseAllSeparatesBrokenFromForeign(t *testing.T) {
	a := newApp(&fakeSource{}, &fakeSink{})
	backups, skipped := a.parseAll([]parser.RawMessage{
		msg(okMessage),
		msg("просто текст без полей"),                     // чужое: штатный пропуск
		msg("STATUS: 🤷 НЕПОНЯТНО\nLABELS: MINIO_BACKUPS"), // сломанный формат
	})

	if len(backups) != 1 {
		t.Errorf("разобрано %d бэкапов, ожидал 1", len(backups))
	}
	if skipped != 2 {
		t.Errorf("пропущено %d, ожидал 2", skipped)
	}
}

// Две метки с одинаковым именем — ошибка конфига, а не данных.
// Отчёт строится, но человек обязан об этом узнать.
func TestWarnsOnAmbiguousNames(t *testing.T) {
	var buf strings.Builder
	a := New(Options{Location: testLoc, RetentionDays: 30},
		&fakeSource{}, &fakeSink{}, slog.New(slog.NewTextHandler(&buf, nil)))

	a.warnAmbiguousNames([]report.Job{
		{Environment: "KT", Label: "PGBACKREST", Name: "PostgreSQL"},
		{Environment: "KT", Label: "PGBACKREST_BACKUP", Name: "PostgreSQL"},
		{Environment: "KT", Label: "MINIO_BACKUPS", Name: "MinIO"},
	})

	log := buf.String()
	if !strings.Contains(log, "PGBACKREST_BACKUP") {
		t.Errorf("предупреждения о совпавших именах нет в логе:\n%s", log)
	}
	if strings.Contains(log, "MinIO") {
		t.Errorf("MinIO уникален, предупреждать о нём не за что:\n%s", log)
	}
}

// Предупреждение о неизвестной метке обязано её называть: иначе непонятно,
// что дописывать в labels.
func TestWarnsUnknownLabelByName(t *testing.T) {
	var buf strings.Builder
	a := New(Options{Labels: map[string]string{}, Location: testLoc, RetentionDays: 30},
		&fakeSource{}, &fakeSink{}, slog.New(slog.NewTextHandler(&buf, nil)))

	a.parseAll([]parser.RawMessage{msg("STATUS: ✅ SUCCESS\n" +
		"ENVIRONMENT: ❗️❗️KT❗️❗️\n" +
		"LABELS: TELEPORT ETCD BACKUP\n" +
		"NODE: kt-teleport01\n" +
		"BACKUP START TIME: 28-08-26_01:00:00\n" +
		"BACKUP FINISHED TIME: 28-08-26_01:30:00")})

	if log := buf.String(); !strings.Contains(log, "TELEPORT ETCD BACKUP") {
		t.Errorf("в предупреждении нет самой метки:\n%s", log)
	}
}
