// Команда backup-report собирает уведомления о бэкапах из Telegram-канала
// за сутки и публикует отчёт отдельным файлом Google Sheets.
//
// Обычный режим — один проход и выход: расписание задаёт cron. Флаг -daemon
// включает ожидание времени из конфига, если внешнего планировщика нет.
//
// Здесь только разбор флагов, интерактивные режимы настройки и сборка
// зависимостей; сама работа сервиса живёт в internal/app.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backup-report/internal/app"
	"backup-report/internal/config"
	"backup-report/internal/gsheets"
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
	flag.BoolVar(&o.dryRun, "dry-run", false, "старые отчёты не удалять, только показать их в логе; отчёт при этом публикуется")
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

// run выбирает режим работы и собирает зависимости.
func run(ctx context.Context, opts options) error {
	cfg, err := config.LoadConfig(opts.configPath)
	if err != nil {
		return err
	}

	tg := telegram.New(telegram.Config{
		APIID:       cfg.Telegram.APIID,
		APIHash:     cfg.Telegram.APIHash,
		Phone:       cfg.Telegram.Phone,
		SessionPath: cfg.Telegram.SessionPath,
		ChannelID:   cfg.Telegram.ChannelID,
		Location:    cfg.Location(),
	})

	// Интерактивные режимы настройки: Google-клиент им не нужен.
	switch {
	case opts.login:
		return runLogin(ctx, cfg, tg)
	case opts.channels:
		return runChannels(ctx, tg)
	}

	// Google поднимаем до выгрузки истории: если токен протух, дешевле
	// узнать об этом сразу, чем после нескольких минут чтения канала.
	sink, err := gsheets.Connect(ctx, gsheets.Config{
		ClientSecretPath: cfg.Google.OAuthClientPath,
		TokenPath:        cfg.Google.TokenPath,
		FolderID:         cfg.Google.FolderID,
		Logger:           slog.Default(),
	})
	if err != nil {
		return err
	}

	hh, mm := cfg.ReportTime()
	svc := app.New(app.Options{
		Labels:        cfg.Labels,
		Location:      cfg.Location(),
		RetentionDays: cfg.Google.RetentionDays,
		ReportHH:      hh,
		ReportMM:      mm,
	}, tg, sink, slog.Default())
	if opts.daemon {
		return svc.Daemon(ctx, opts.dryRun)
	}

	day, err := reportDay(opts.date, time.Now(), cfg.Location())
	if err != nil {
		return err
	}
	return svc.Once(ctx, day, opts.dryRun)
}

// reportDay возвращает день, за который строим отчёт: из флага или сегодняшний.
func reportDay(dateStr string, now time.Time, loc *time.Location) (time.Time, error) {
	now = now.In(loc)
	if dateStr == "" {
		return now, nil
	}
	day, err := time.ParseInLocation(report.DateLayout, dateStr, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("-date %q: ожидается формат %s: %w", dateStr, report.DateLayout, err)
	}

	// День ещё не наступил: сообщений за него быть не может, а пустой отчёт
	// займёт имя и заблокирует настоящий, когда день придёт, — сервис увидит
	// готовый файл и молча пропустит публикацию.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if day.After(today) {
		return time.Time{}, fmt.Errorf(
			"-date %s: день ещё не наступил (сегодня %s); пустой отчёт занял бы имя и заблокировал настоящий",
			dateStr, today.Format(report.DateLayout))
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
