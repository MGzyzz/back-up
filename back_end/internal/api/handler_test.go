package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"backup-report/internal/parser"
	"backup-report/internal/telegram"
)

// fakeSource отдаёт заранее заготовленные сообщения либо ошибку.
type fakeSource struct {
	msgs []parser.RawMessage
	err  error
}

func (f fakeSource) FetchDay(context.Context, time.Time) ([]parser.RawMessage, error) {
	return f.msgs, f.err
}

const successMsg = `STATUS: ✅ SUCCESS
ENVIRONMENT: ❗️❗️KT❗️❗️
ALERT: BACKUP COMPLETED
LABELS: MINIO_BACKUPS
NODE: kt-minio01
BACKUP START TIME: 04-09-26_01:00:01
BACKUP FINISHED TIME: 04-09-26_02:23:20`

const foreignMsg = `Backup job 'minio3' has been started.`

func newTestServer(t *testing.T, src Source) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	h := New(Options{
		Labels:        map[string]string{"MINIO_BACKUPS": "MinIO"},
		Location:      time.UTC,
		CacheTTLToday: time.Minute,
		CacheTTLPast:  time.Hour,
	}, src, slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := gin.New()
	h.Register(r)
	return httptest.NewServer(r)
}

func TestОтчётЗаДень(t *testing.T) {
	at := time.Date(2026, 9, 4, 2, 30, 0, 0, time.UTC)
	srv := newTestServer(t, fakeSource{msgs: []parser.RawMessage{
		{Date: at, Text: successMsg},
		{Date: at, Text: foreignMsg},
	}})
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/report?date=2026-09-04")
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", res.StatusCode)
	}

	var got ReportDTO
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if got.Date != "2026-09-04" {
		t.Errorf("date = %q", got.Date)
	}
	if got.Stats.Messages != 2 || got.Stats.Backups != 1 || got.Stats.Skipped != 1 {
		t.Errorf("stats = %+v, ожидалось messages=2 backups=1 skipped=1", got.Stats)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("задач %d, ожидалась 1", len(got.Jobs))
	}
	if got.Jobs[0].Environment != "KT" || got.Jobs[0].Name != "MinIO" || !got.Jobs[0].OK {
		t.Errorf("задача = %+v", got.Jobs[0])
	}
}

func TestДатаБезПараметраЭтоСегодня(t *testing.T) {
	srv := newTestServer(t, fakeSource{})
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/report")
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", res.StatusCode)
	}
	var got ReportDTO
	json.NewDecoder(res.Body).Decode(&got)
	if want := time.Now().In(time.UTC).Format("2006-01-02"); got.Date != want {
		t.Errorf("date = %q, ожидалось %q", got.Date, want)
	}
}

func TestКриваяДатаЭто400(t *testing.T) {
	srv := newTestServer(t, fakeSource{})
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/report?date=04.09.2026")
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", res.StatusCode)
	}
	var e ErrorDTO
	json.NewDecoder(res.Body).Decode(&e)
	if e.Error == "" {
		t.Error("в теле нет объяснения ошибки")
	}
}

func TestБудущаяДатаЭто400(t *testing.T) {
	srv := newTestServer(t, fakeSource{})
	defer srv.Close()

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	res, _ := http.Get(srv.URL + "/api/report?date=" + tomorrow)
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400: день ещё не наступил", res.StatusCode)
	}
}

func TestПротухшаяСессияЭто503(t *testing.T) {
	srv := newTestServer(t, fakeSource{err: telegram.ErrNoSession})
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/report?date=2026-09-04")
	defer res.Body.Close()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("код %d, ожидался 503", res.StatusCode)
	}
	var e ErrorDTO
	json.NewDecoder(res.Body).Decode(&e)
	if !strings.Contains(e.Error, "login") {
		t.Errorf("в ошибке нет подсказки про -login: %q", e.Error)
	}
}

func TestПрочаяОшибкаЭто502(t *testing.T) {
	srv := newTestServer(t, fakeSource{err: errors.New("канал не найден среди диалогов")})
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/report?date=2026-09-04")
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("код %d, ожидался 502", res.StatusCode)
	}
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t, fakeSource{})
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/health")
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", res.StatusCode)
	}
}
