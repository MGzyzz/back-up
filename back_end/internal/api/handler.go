package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"backup-report/internal/dates"
	"backup-report/internal/parser"
	"backup-report/internal/report"
	"backup-report/internal/telegram"
)

// Source отдаёт сообщения канала за сутки. Объявлен здесь, а не берётся
// из internal/app: тот пакет тянет за собой Google, а нам он не нужен.
type Source interface {
	FetchDay(ctx context.Context, day time.Time) ([]parser.RawMessage, error)
}

// Options — то, что ручкам нужно из настроек.
type Options struct {
	Labels        map[string]string
	Location      *time.Location
	CacheTTLToday time.Duration
	CacheTTLPast  time.Duration
}

type Handler struct {
	opts   Options
	src    Source
	loader *loader
	log    *slog.Logger
}

func New(opts Options, src Source, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{opts: opts, src: src, log: log}
	h.loader = newLoader(h.buildDay, opts.Location, opts.CacheTTLToday, opts.CacheTTLPast)
	return h
}

// Register вешает ручки. Принимает gin.IRouter, а не *gin.Engine, чтобы
// маршруты можно было повесить и в группу.
func (h *Handler) Register(r gin.IRouter) {
	r.GET("/api/report", h.report)
	r.GET("/api/health", h.health)
}

func (h *Handler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) report(c *gin.Context) {
	day, err := h.day(c.Query("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorDTO{Error: err.Error()})
		return
	}

	rep, err := h.loader.get(c.Request.Context(), day)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, rep)
}

// day разбирает параметр date. Пустой — сегодня.
func (h *Handler) day(s string) (time.Time, error) {
	loc := h.opts.Location
	today := dates.StartOfDay(time.Now().In(loc), loc)

	if s == "" {
		return today, nil
	}

	day, err := time.ParseInLocation(report.DateLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q: ожидается формат %s", s, report.DateLayout)
	}
	// День ещё не наступил: сообщений за него нет и быть не может.
	if day.After(today) {
		return time.Time{}, fmt.Errorf(
			"date %s: день ещё не наступил, сегодня %s", s, today.Format(report.DateLayout))
	}
	return day, nil
}

// fail переводит ошибку источника в код ответа.
//
// Ни один из случаев не роняет сервер: протухшую сессию чинят руками
// на хосте, и до тех пор дашборд должен внятно объяснять, что случилось.
func (h *Handler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, telegram.ErrNoSession):
		h.log.Error("нет сессии Telegram", "err", err)
		c.JSON(http.StatusServiceUnavailable, ErrorDTO{
			Error: "сервер не может войти в Telegram: нужен повторный вход через -login",
		})

	case isFloodWait(err):
		d, _ := telegram.FloodWaitFor(err)
		h.log.Warn("Telegram просит подождать", "через", d.String())
		c.Header("Retry-After", strconv.Itoa(int(d.Seconds())+1))
		c.JSON(http.StatusServiceUnavailable, ErrorDTO{
			Error: fmt.Sprintf("Telegram просит подождать %s и повторить", d),
		})

	default:
		h.log.Error("не смог построить отчёт", "err", err)
		c.JSON(http.StatusBadGateway, ErrorDTO{Error: err.Error()})
	}
}

func isFloodWait(err error) bool {
	_, ok := telegram.FloodWaitFor(err)
	return ok
}
