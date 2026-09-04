package api

import (
	"context"
	"errors"
	"time"

	"backup-report/internal/parser"
	"backup-report/internal/report"
)

// buildDay делает ровно то же, что app.Once, минус публикация: читает сутки,
// разбирает сообщения, сворачивает в задачи.
func (h *Handler) buildDay(ctx context.Context, day time.Time) (ReportDTO, error) {
	raws, err := h.src.FetchDay(ctx, day)
	if err != nil {
		return ReportDTO{}, err
	}

	backups, skipped := h.parseAll(raws)
	jobs := report.Aggregate(backups)

	return ReportDTO{
		Date:      day.In(h.opts.Location).Format(report.DateLayout),
		FetchedAt: time.Now().In(h.opts.Location),
		Stats: StatsDTO{
			Messages: len(raws),
			Backups:  len(backups),
			Skipped:  skipped,
		},
		Jobs: jobDTOs(jobs),
	}, nil
}

// parseAll отделяет штатные пропуски от сломанных сообщений. Одно кривое
// сообщение не должно ронять весь день, но в лог оно попасть обязано.
func (h *Handler) parseAll(raws []parser.RawMessage) ([]parser.Backup, int) {
	var backups []parser.Backup
	skipped := 0

	for _, m := range raws {
		b, err := parser.Parse(m, h.opts.Labels, h.opts.Location)
		switch {
		case err == nil:
			backups = append(backups, b)
		case errors.Is(err, parser.ErrNotBackupMessage):
			skipped++ // чужое уведомление или отчёт о восстановлении — норма
		default:
			skipped++
			h.log.Warn("не разобрал сообщение",
				"at", m.Date.Format(time.RFC3339), "err", err)
		}
	}
	return backups, skipped
}
