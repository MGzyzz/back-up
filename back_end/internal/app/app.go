// Package app связывает источник сообщений, разбор и публикацию отчёта.
//
// Про MTProto и Google API пакет не знает: он ходит через интерфейсы Source
// и Sink, объявленные здесь же. Поэтому порядок шагов, дедуп и чистка
// проверяются на фейках, без сети и учётных данных.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"backup-report/internal/gsheets"
	"backup-report/internal/parser"
	"backup-report/internal/report"
)

// Source отдаёт сообщения канала за указанные сутки.
type Source interface {
	FetchDay(ctx context.Context, day time.Time) ([]parser.RawMessage, error)
}

// Sink — папка отчётов: посмотреть, что в ней лежит, положить новое,
// убрать старое.
type Sink interface {
	List(ctx context.Context) ([]gsheets.File, error)
	Publish(ctx context.Context, name string, tabs []gsheets.Sheet) (string, error)
	Trash(ctx context.Context, fileID string) error
}

// Options — то, что сервису нужно из настроек. Не *config.Config целиком:
// от формы конфига пакет зависеть не должен, раскладывает их main.
type Options struct {
	Labels        map[string]string // LABELS из сообщения -> тип бэкапа
	Location      *time.Location    // зона, в которой считаются сутки
	RetentionDays int
	ReportHH      int // час и минута ежедневного запуска; нужны только Daemon
	ReportMM      int
}

type App struct {
	opts Options
	src  Source
	sink Sink
	log  *slog.Logger
}

// New собирает сервис. log может быть nil — тогда берётся slog.Default().
func New(opts Options, src Source, sink Sink, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}
	return &App{opts: opts, src: src, sink: sink, log: log}
}

// Once — вся работа сервиса за один проход: прочитать сутки, разобрать,
// опубликовать, прибрать старое.
func (a *App) Once(ctx context.Context, day time.Time, dryRun bool) error {
	name := report.ReportName(day)
	a.log.Info("строю отчёт", "day", day.Format(report.DateLayout), "file", name)

	raws, err := a.src.FetchDay(ctx, day)
	if err != nil {
		return fmt.Errorf("прочитать канал: %w", err)
	}

	backups, skipped := a.parseAll(raws)
	a.log.Info("сообщения разобраны",
		"всего", len(raws), "бэкапов", len(backups), "пропущено", skipped)

	files, err := a.sink.List(ctx)
	if err != nil {
		return fmt.Errorf("прочитать папку отчётов: %w", err)
	}

	// Отчёт за день должен быть один. Тот же список уходит в чистку —
	// созданный сейчас файл в него не попадает, так что удалить сам себя
	// запуск не может.
	if id, ok := findByName(files, name); ok {
		a.log.Warn("отчёт за этот день уже есть, публикацию пропускаю", "file_id", id)
	} else {
		jobs := report.Aggregate(backups)
		if len(jobs) == 0 {
			// Либо канал молчал, либо не отработал вообще никто.
			a.log.Warn("в отчёте нет ни одной задачи: за этот день в канале не нашлось сообщений о бэкапах")
		}
		a.warnAmbiguousNames(jobs)
		id, err := a.sink.Publish(ctx, name, sheetsOf(jobs, backups))
		if err != nil {
			return err
		}
		a.log.Info("отчёт опубликован",
			"file_id", id, "задач", len(jobs), "успешно", okCount(jobs), "сообщений", len(backups))
	}

	a.cleanup(ctx, files, dryRun)
	return nil
}

// Daemon ждёт schedule.report_at и строит отчёт за наступивший день.
// Нужен там, где нет cron; при наличии cron проще запускать одним проходом.
func (a *App) Daemon(ctx context.Context, dryRun bool) error {
	hh, mm := a.opts.ReportHH, a.opts.ReportMM
	loc := a.opts.Location

	for {
		next := nextRun(time.Now(), hh, mm, loc)
		a.log.Info("следующий отчёт", "at", next.Format(time.RFC3339),
			"через", time.Until(next).Round(time.Minute).String())

		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			a.log.Info("сервис остановлен")
			return nil
		case <-t.C:
		}

		// Отчёт не построился — не повод убивать демон: завтра попробуем снова.
		if err := a.Once(ctx, time.Now().In(loc), dryRun); err != nil {
			a.log.Error("отчёт не построен", "err", err)
		}
	}
}

// parseAll разбирает сообщения, отделяя штатные пропуски от сломанных.
func (a *App) parseAll(raws []parser.RawMessage) ([]parser.Backup, int) {
	var backups []parser.Backup
	skipped := 0

	for _, m := range raws {
		b, err := parser.Parse(m, a.opts.Labels, a.opts.Location)
		switch {
		case err == nil:
			a.warnAnomalies(b, m.Date)
			backups = append(backups, b)
		case errors.Is(err, parser.ErrNotBackupMessage):
			skipped++ // чужое уведомление или восстановление — это норма
		default:
			// Сломанный формат: одно сообщение не должно ронять весь отчёт,
			// но человек обязан об этом узнать.
			skipped++
			a.log.Warn("не разобрал сообщение", "at", m.Date.Format(time.RFC3339), "err", err)
		}
	}
	return backups, skipped
}

// warnAnomalies сообщает о странностях, которые не мешают строке попасть
// в отчёт, но человеку о них знать стоит.
func (a *App) warnAnomalies(b parser.Backup, at time.Time) {
	// ERROR без времён — норма: команда не запускалась. SUCCESS без времён —
	// уже нет: источник отчитался об успехе, не сказав когда.
	if b.Status == parser.StatusSuccess && !b.HasTimes() {
		a.log.Warn("успешный бэкап без времён",
			"env", b.Environment, "node", b.Node, "at", at.Format(time.RFC3339))
	}
	if b.Type == parser.TypeUnknown {
		// Метку называем в логе: по ней строку и дописывают в labels.
		a.log.Warn("метки нет в labels — задача попадёт в сводку как UNKNOWN",
			"метка", b.Label, "env", b.Environment, "node", b.Node,
			"at", at.Format(time.RFC3339))
	}
}

// Цвета заливки строк — пастельные, чтобы лист читался.
var (
	green = gsheets.RGB{R: 0.85, G: 0.94, B: 0.85}
	red   = gsheets.RGB{R: 1, G: 0.85, B: 0.85}
	grey  = gsheets.RGB{R: 0.94, G: 0.94, B: 0.94}
)

// sheetsOf собирает листы отчёта. «Сводка» первой: она открывается
// по умолчанию. В «Детали» идут, когда в сводке что-то красное.
func sheetsOf(jobs []report.Job, backups []parser.Backup) []gsheets.Sheet {
	return []gsheets.Sheet{
		{
			Title:     "Сводка",
			Rows:      report.SummaryRows(jobs),
			StatusCol: report.SummaryStatusCol,
			// Явная ширина у двух левых колонок (Environment, Backup):
			// их значения длиннее заголовка ("PostgreSQL Cloud",
			// "AMANAT PROD"), автоподгонка ужимала бы их впритык.
			// Остальным хватает подгонки по содержимому.
			Widths: []int{130, 120},
			Colors: []gsheets.ColorRule{
				{Value: report.MarkOK, Color: green},
				{Value: report.MarkFail, Color: red},
			},
		},
		{
			Title:     "Детали",
			Rows:      report.DetailRows(backups),
			StatusCol: report.DetailStatusCol,
			Colors: []gsheets.ColorRule{
				{Value: parser.StatusSuccess, Color: green},
				{Value: parser.StatusFailed, Color: red},
				// ERROR серым: на кластере это «команда не выполнялась
				// на этой ноде» — штатная работа, а не авария.
				{Value: parser.StatusError, Color: grey},
			},
		},
	}
}

// warnAmbiguousNames ловит ошибку конфига: две метки в одном окружении
// получили одинаковое имя — в сводке это две неразличимые строки.
func (a *App) warnAmbiguousNames(jobs []report.Job) {
	type key struct{ env, name string }
	seen := make(map[key]string, len(jobs))

	for _, j := range jobs {
		k := key{j.Environment, j.Name}
		if prev, dup := seen[k]; dup {
			a.log.Warn("две метки дают одно имя — строки сводки не различить, поправь labels в конфиге",
				"env", j.Environment, "имя", j.Name, "метки", prev+" и "+j.Label)
			continue
		}
		seen[k] = j.Label
	}
}

func okCount(jobs []report.Job) int {
	n := 0
	for _, j := range jobs {
		if j.OK {
			n++
		}
	}
	return n
}

func findByName(files []gsheets.File, name string) (string, bool) {
	for _, f := range files {
		if f.Name == name {
			return f.ID, true
		}
	}
	return "", false
}

// cleanup убирает отчёты старше retention_days. Ошибку не возвращает:
// неудачное удаление логируется и не должно ронять уже готовый отчёт.
func (a *App) cleanup(ctx context.Context, files []gsheets.File, dryRun bool) {
	cutoff := report.Cutoff(time.Now(), a.opts.RetentionDays, a.opts.Location)

	removed := 0
	for _, f := range files {
		if !report.ShouldDelete(f.Name, cutoff) {
			continue
		}
		if dryRun {
			a.log.Info("удалил бы (dry-run)", "file", f.Name)
			continue
		}
		if err := a.sink.Trash(ctx, f.ID); err != nil {
			a.log.Error("не удалил старый отчёт", "file", f.Name, "err", err)
			continue
		}
		removed++
		a.log.Info("отчёт отправлен в корзину", "file", f.Name)
	}
	a.log.Info("чистка завершена",
		"удалено", removed, "граница", cutoff.Format(report.DateLayout))
}
