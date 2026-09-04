// Команда server отдаёт по HTTP отчёт по бэкапам за сутки — то же, что
// cmd/report кладёт в Google Sheets, но по живым данным и в JSON.
//
// Google этой команде не нужен: config.RequireGoogle она не зовёт,
// и пакет internal/gsheets в её зависимостях не появляется.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"backup-report/internal/api"
	"backup-report/internal/config"
	"backup-report/internal/telegram"
)

func main() {
	configPath := flag.String("config", "config.yaml", "путь к конфигу")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *configPath); err != nil {
		slog.Error("сервер остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string) error {
	cfg, err := config.LoadConfig(configPath)
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

	h := api.New(api.Options{
		Labels:        cfg.Labels,
		Location:      cfg.Location(),
		CacheTTLToday: cfg.Server.CacheTTLToday,
		CacheTTLPast:  cfg.Server.CacheTTLPast,
	}, tg, slog.Default())

	gin.SetMode(gin.ReleaseMode)
	// gin.New, а не gin.Default: свой лог поверх slog, чтобы записи сервера
	// выглядели как записи остального сервиса, а не как строки Gin.
	r := gin.New()
	r.Use(gin.Recovery(), logRequests(slog.Default()))
	h.Register(r)

	srv := &http.Server{
		Addr:    cfg.Server.Addr,
		Handler: r,
		// Первый запрос за день может занять секунды: обход диалогов плюс
		// пагинация истории. Читающий таймаут должен это пережить.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      2 * time.Minute,
	}

	// engine.Run() не умеет graceful shutdown, поэтому сервер поднимаем сами.
	go func() {
		<-ctx.Done()
		slog.Info("останавливаю сервер")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("остановка сервера", "err", err)
		}
	}()

	slog.Info("слушаю", "addr", cfg.Server.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// logRequests пишет запросы через slog — тем же форматом, что и остальной сервис.
func logRequests(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("запрос",
			"method", c.Request.Method,
			"path", c.Request.URL.RequestURI(),
			"status", c.Writer.Status(),
			"за", time.Since(start).Round(time.Millisecond).String(),
		)
	}
}
