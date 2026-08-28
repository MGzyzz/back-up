// Команда backup-report собирает уведомления о бэкапах из Telegram-канала
// за сутки и публикует отчёт отдельным файлом Google Sheets.
//
// Обычный режим — один проход и выход: расписание задаёт cron. Флаг -daemon
// включает ожидание времени из конфига, если внешнего планировщика нет.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backup-report/internal/config"
	"backup-report/internal/gsheets"
	"backup-report/internal/parser"
	"backup-report/internal/report"
	"backup-report/internal/telegram"
)

// options — разобранные флаги командной строки.
type options struct {
	configPath string
	date       string
	login      bool
	channels   bool
	dryRun     bool
	daemon     bool
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.configPath, "config", "config.yaml", "путь к конфигу")
	flag.StringVar(&o.date, "date", "", "день отчёта в виде 2026-08-28; по умолчанию сегодня")
	flag.BoolVar(&o.login, "login", false, "интерактивный вход в Telegram и Google, потом выход")
	flag.BoolVar(&o.channels, "channels", false, "показать каналы аккаунта с их ID и выйти")
	flag.BoolVar(&o.dryRun, "dry-run", false, "показать, что было бы удалено, и ничего не делать")
	flag.BoolVar(&o.daemon, "daemon", false, "не выходить, а ждать schedule.report_at из конфига")
	flag.Parse()
	return o
}

func main() {
	opts := parseFlags()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// SIGTERM шлют systemd и docker stop. Без обработки процесс добивают
	// SIGKILL, и висящий сетевой запрос обрывается посреди работы.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, opts); err != nil {
		slog.Error("сервис остановлен с ошибкой", "err", err)
		// Ненулевой код — единственный способ сказать крону, что отчёт не построен.
		os.Exit(1)
	}
}

// run выбирает режим работы. Каждый режим — отдельная функция ниже.
func run(ctx context.Context, opts options) error {
	cfg, err := config.LoadConfig(opts.configPath)
	if err != nil {
		return err
	}

	tg := telegram.New(
		cfg.Telegram.APIID, cfg.Telegram.APIHash, cfg.Telegram.Phone,
		cfg.Telegram.SessionPath, cfg.Telegram.ChannelID, cfg.Location(),
	)

	switch {
	case opts.login:
		return runLogin(ctx, cfg, tg)
	case opts.channels:
		return runChannels(ctx, tg)
	case opts.daemon:
		return runDaemon(ctx, cfg, tg, opts.dryRun)
	}

	day, err := reportDay(opts.date, cfg.Location())
	if err != nil {
		return err
	}
	return runOnce(ctx, cfg, tg, day, opts.dryRun)
}

// reportDay возвращает день, за который строим отчёт: из флага или сегодняшний.
func reportDay(dateStr string, loc *time.Location) (time.Time, error) {
	if dateStr == "" {
		return time.Now().In(loc), nil
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("-date %q: ожидается формат 2006-01-02: %w", dateStr, err)
	}
	return day, nil
}

// runLogin проводит оба интерактивных входа. Запускается человеком и требует
// терминала: Telegram спросит код, Google откроет браузер.
func runLogin(ctx context.Context, cfg *config.Config, tg *telegram.Client) error {
	fmt.Println("== Telegram ==")
	if err := tg.Login(ctx); err != nil {
		return fmt.Errorf("вход в Telegram: %w", err)
	}
	fmt.Println("\n== Google ==")
	if err := gsheets.Login(ctx, cfg.Google.OAuthClientPath, cfg.Google.TokenPath); err != nil {
		return fmt.Errorf("вход в Google: %w", err)
	}
	return nil
}

// runChannels печатает каналы аккаунта. Нужен один раз, на настройке:
// ID канала неоткуда взять, а без него сервис не знает, что читать.
func runChannels(ctx context.Context, tg *telegram.Client) error {
	list, err := tg.ListChannels(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%-14s %s\n", "CHANNEL_ID", "НАЗВАНИЕ")
	for _, ch := range list {
		fmt.Printf("%-14d %s\n", ch.ID, ch.Title)
	}
	return nil
}

// runOnce — вся работа сервиса: прочитать сутки, разобрать, опубликовать, прибрать.
func runOnce(ctx context.Context, cfg *config.Config, tg *telegram.Client, day time.Time, dryRun bool) error {
	name := report.ReportName(day)
	slog.Info("строю отчёт", "day", day.Format("2006-01-02"), "file", name)

	raws, err := tg.FetchDay(ctx, day)
	if err != nil {
		return fmt.Errorf("прочитать канал: %w", err)
	}

	backups, skipped := parseAll(raws, cfg.Labels, cfg.Location())
	slog.Info("сообщения разобраны",
		"всего", len(raws), "бэкапов", len(backups), "пропущено", skipped)

	gc, err := gsheets.Connect(ctx, cfg.Google.OAuthClientPath, cfg.Google.TokenPath, cfg.Google.FolderID)
	if err != nil {
		return err
	}

	files, err := gc.List(ctx)
	if err != nil {
		return err
	}

	// Отчёт за день должен быть один. Проверка листингом дешевле,
	// чем разбираться потом с двумя файлами на одну дату. Тот же список
	// уходит в чистку — созданный сейчас файл в него не попадает,
	// поэтому удалить сам себя запуск не может.
	if id, ok := findByName(files, name); ok {
		slog.Warn("отчёт за этот день уже есть, публикацию пропускаю", "file_id", id)
	} else {
		rows := report.BuildRows(backups)
		id, err := gc.Publish(ctx, name, rows)
		if err != nil {
			return err
		}
		slog.Info("отчёт опубликован", "file_id", id, "строк", len(rows)-1)
	}

	cleanup(ctx, gc, files, cfg.Google.RetentionDays, cfg.Location(), dryRun)
	return nil
}

// parseAll разбирает сообщения, отделяя штатные пропуски от сломанных.
func parseAll(raws []parser.RawMessage, labels map[string]string, loc *time.Location) ([]parser.Backup, int) {
	var backups []parser.Backup
	skipped := 0

	for _, m := range raws {
		b, err := parser.Parse(m, labels, loc)
		switch {
		case err == nil:
			warnAnomalies(b, m.Date)
			backups = append(backups, b)
		case errors.Is(err, parser.ErrNotBackupMessage):
			skipped++ // чужое уведомление или восстановление — это норма
		default:
			// Сломанный формат: одно сообщение не должно ронять весь отчёт,
			// но человек обязан об этом узнать.
			skipped++
			slog.Warn("не разобрал сообщение", "at", m.Date.Format(time.RFC3339), "err", err)
		}
	}
	return backups, skipped
}

// warnAnomalies сообщает о странностях, которые не мешают строке попасть
// в отчёт, но человеку о них знать стоит.
func warnAnomalies(b parser.Backup, at time.Time) {
	// ERROR без времён — норма: команда не запускалась. А вот SUCCESS
	// без времён редкость: источник отчитался об успехе, не сказав, когда.
	if b.Status == parser.StatusSuccess && !b.HasTimes() {
		slog.Warn("успешный бэкап без времён",
			"env", b.Environment, "node", b.Node, "at", at.Format(time.RFC3339))
	}
	if b.Type == parser.TypeUnknown {
		slog.Warn("неизвестная метка LABELS, тип UNKNOWN",
			"node", b.Node, "at", at.Format(time.RFC3339))
	}
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
// неудачное удаление логируется и не должно обесценивать построенный отчёт. Дата берётся из имени файла,
// а не из modifiedTime: последний меняется, когда кто-то просто открыл отчёт.
func cleanup(ctx context.Context, gc *gsheets.Client, files []gsheets.File, retentionDays int, loc *time.Location, dryRun bool) {
	now := time.Now().In(loc)
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).
		AddDate(0, 0, -retentionDays)

	removed := 0
	for _, f := range files {
		if !report.ShouldDelete(f.Name, cutoff) {
			continue
		}
		if dryRun {
			slog.Info("удалил бы (dry-run)", "file", f.Name)
			continue
		}
		if err := gc.Trash(ctx, f.ID); err != nil {
			// Неудачное удаление не повод терять уже построенный отчёт.
			slog.Error("не удалил старый отчёт", "file", f.Name, "err", err)
			continue
		}
		removed++
		slog.Info("отчёт отправлен в корзину", "file", f.Name)
	}
	slog.Info("чистка завершена", "удалено", removed, "граница", cutoff.Format("2006-01-02"))
}

// runDaemon ждёт schedule.report_at и строит отчёт за наступивший день.
// Нужен там, где нет cron; при наличии cron проще запускать без флага.
func runDaemon(ctx context.Context, cfg *config.Config, tg *telegram.Client, dryRun bool) error {
	hh, mm, err := report.ParseReportAt(cfg.Schedule.ReportAt)
	if err != nil {
		return err
	}
	for {
		next := report.NextRun(time.Now(), hh, mm, cfg.Location())
		slog.Info("следующий отчёт", "at", next.Format(time.RFC3339),
			"через", time.Until(next).Round(time.Minute).String())

		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			slog.Info("сервис остановлен")
			return nil
		case <-t.C:
		}

		// Отчёт не построился — не повод убивать демон: завтра попробуем снова.
		if err := runOnce(ctx, cfg, tg, time.Now().In(cfg.Location()), dryRun); err != nil {
			slog.Error("отчёт не построен", "err", err)
		}
	}
}
