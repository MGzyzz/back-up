# Веб-дашборд по бэкапам — план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** экран, где по выбранной дате видно, какие бэкапы в каких средах прошли, а какие нет, по живым данным Telegram.

**Architecture:** к существующему сервису отчётов добавляется второй бинарь — HTTP-сервер на Gin, который переиспользует `telegram → parser → report.Aggregate` и отдаёт JSON. Фронт на Vite + React + TypeScript группирует плоский список задач по средам. Оба поднимаются через `docker compose`; nginx отдаёт статику и проксирует `/api`, поэтому CORS не нужен.

**Tech Stack:** Go 1.27, Gin, gotd/td, Vite, React, TypeScript, Tailwind v4, vitest, Docker, nginx.

**Spec:** `docs/superpowers/specs/2026-09-04-backup-dashboard-design.md`

## Global Constraints

- Модуль остаётся `backup-report`. Импорты вида `backup-report/internal/parser` не меняются никогда.
- Go 1.27.0 (из `go.mod`).
- Весь код, комментарии, логи и сообщения об ошибках — **на русском**. Это стиль проекта.
- **Коммиты без трейлера `Co-Authored-By`.** Требование владельца репозитория.
- `internal/api` **не импортирует** `internal/gsheets` и `internal/app`. Это то, что держит Google API вне образа веб-сервера.
- Существующее поведение `cmd/report` не меняется ни в чём: те же флаги, те же ошибки, те же тексты.
- В JSON: `nodes` — всегда массив, никогда `null`. Времена отсутствующих значений — `null`, никогда `0001-01-01T00:00:00Z`.
- Gin поднимается через `gin.New()`, не `gin.Default()`.
- Рантайм-образ бэка — `alpine` с `ca-certificates` и `tzdata`, `WORKDIR /app`.
- Правило «задача успешна, если хотя бы одна нода отчиталась SUCCESS» живёт в `report.Aggregate` и **не повторяется** ни в `internal/api`, ни на фронте.
- Ветка работы: `feature/backup-dashboard`.

---

### Task 1: Переезд в back_end/ и создание docker/

Чистое перемещение файлов. Поведение не меняется, поэтому тестов не пишем — проверкой служит то, что существующий набор тестов остаётся зелёным.

**Files:**
- Move: `main.go`, `go.mod`, `go.sum`, `internal/`, `configs/`, `data/`, `secrets/`, `config.yaml`, `env.sh` → `back_end/`
- Move: `back_end/main.go` → `back_end/cmd/report/main.go`
- Create: `docker/.gitkeep`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: ничего
- Produces: рабочее дерево `back_end/` с пакетом `main` в `back_end/cmd/report`; все последующие задачи ведут пути от `back_end/`

- [ ] **Step 1: Зафиксировать эталон — прогнать тесты до переезда**

```bash
go test ./... 2>&1 | tail -20
```

Ожидается: `ok` по всем пакетам. Запишите вывод — после переезда он должен совпасть.

- [ ] **Step 2: Переместить дерево в back_end/**

```bash
mkdir -p back_end/cmd/report docker
git mv main.go back_end/cmd/report/main.go
git mv main_test.go back_end/cmd/report/main_test.go
git mv go.mod go.sum internal configs env.sh back_end/
mv data secrets config.yaml back_end/   # не в git: лежат под .gitignore
touch docker/.gitkeep
```

`data`, `secrets` и `config.yaml` перемещаются обычным `mv`, а не `git mv`: они под `.gitignore` и git о них не знает.

- [ ] **Step 3: Поправить .gitignore под новые пути**

Замените строки, указывающие на корень, на пути внутри `back_end/`:

```gitignore
.DS_Store

back_end/config.yaml
back_end/env.sh
back_end/.env
back_end/data/
back_end/secrets/

front_end/node_modules/
front_end/dist/
```

- [ ] **Step 4: Убедиться, что тесты зелёные на новом месте**

```bash
cd back_end && go test ./... 2>&1 | tail -20 && go vet ./...
```

Ожидается: тот же список `ok`, что и в шаге 1. `go vet` молчит.

Если `go test` не находит пакеты — проверьте, что `go.mod` лежит в `back_end/`, а не в корне.

- [ ] **Step 5: Убедиться, что бинарь по-прежнему собирается и запускается**

```bash
cd back_end && go build -o /tmp/report ./cmd/report && /tmp/report -h 2>&1 | head -5
```

Ожидается: список флагов `-channels`, `-config`, `-daemon`, `-date`, `-dry-run`, `-login`.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: переезд бэка в back_end/, main.go в cmd/report

Готовим место под фронт и общий docker/. Логика не меняется:
тот же набор тестов зелёный, те же флаги у бинаря."
```

---

### Task 2: config.RequireGoogle() и необязательный блок server

Сейчас `prepare()` требует настройки Google безусловно, и веб-сервер без них не стартует. Выносим их в отдельный метод. Заодно добавляем блок `server` — необязательный, со значениями по умолчанию в коде.

**Files:**
- Modify: `back_end/internal/config/config.go`
- Modify: `back_end/cmd/report/main.go`
- Modify: `back_end/configs/config.example.yaml`
- Test: `back_end/internal/config/config_test.go`

**Interfaces:**
- Consumes: `config.LoadConfig(path string) (*Config, error)` — существует
- Produces:
  - `func (c *Config) RequireGoogle() error`
  - `c.Server.Addr string`, `c.Server.CacheTTLToday time.Duration`, `c.Server.CacheTTLPast time.Duration`

- [ ] **Step 1: Написать падающие тесты**

В `back_end/internal/config/config_test.go` уже есть хелперы `writeConfig(t, body)`
и `setSecrets(t)` — пользуемся ими. Добавьте в конец файла:

```go
// noGoogleYAML — конфиг без блока google. Ровно то, с чем стартует cmd/api-server.
const noGoogleYAML = `
telegram:
  channel_id: -1001234567890
  session_path: ./data/session.json
schedule:
  report_at: "11:00"
  timezone: Asia/Almaty
labels:
  MINIO_BACKUPS: MinIO
`

const serverYAML = `
telegram:
  channel_id: -1001234567890
  session_path: ./data/session.json
schedule:
  report_at: "11:00"
  timezone: Asia/Almaty
labels:
  MINIO_BACKUPS: MinIO
server:
  addr: ":9090"
  cache_ttl_today: 30s
  cache_ttl_past: 2h
`

func TestLoadConfigБезGoogleНеРугается(t *testing.T) {
	setSecrets(t)
	os.Unsetenv("GOOGLE_OAUTH_CLIENT") // t.Setenv из setSecrets вернёт значение после теста

	if _, err := LoadConfig(writeConfig(t, noGoogleYAML)); err != nil {
		t.Fatalf("LoadConfig отверг конфиг без google: %v\nвеб-сервер обязан на нём стартовать", err)
	}
}

func TestRequireGoogleНазываетВсеПропуски(t *testing.T) {
	setSecrets(t)
	os.Unsetenv("GOOGLE_OAUTH_CLIENT")

	cfg, err := LoadConfig(writeConfig(t, noGoogleYAML))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	err = cfg.RequireGoogle()
	if err == nil {
		t.Fatal("RequireGoogle промолчал на конфиге без google")
	}
	for _, want := range []string{
		"GOOGLE_OAUTH_CLIENT",
		"google.folder_id",
		"google.token_path",
		"google.retention_days",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет упоминания %q; получено: %v", want, err)
		}
	}
}

func TestRequireGoogleМолчитНаПолномКонфиге(t *testing.T) {
	setSecrets(t)

	cfg, err := LoadConfig(writeConfig(t, goodYAML))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := cfg.RequireGoogle(); err != nil {
		t.Fatalf("RequireGoogle ругнулся на полный конфиг: %v", err)
	}
}

func TestServerПоУмолчанию(t *testing.T) {
	setSecrets(t)

	cfg, err := LoadConfig(writeConfig(t, goodYAML)) // блока server в нём нет
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Server.Addr; got != ":8080" {
		t.Errorf("Server.Addr = %q, ожидалось \":8080\"", got)
	}
	if got := cfg.Server.CacheTTLToday; got != time.Minute {
		t.Errorf("CacheTTLToday = %v, ожидалась 1m", got)
	}
	if got := cfg.Server.CacheTTLPast; got != time.Hour {
		t.Errorf("CacheTTLPast = %v, ожидался 1h", got)
	}
}

func TestServerИзФайлаПеребиваетУмолчания(t *testing.T) {
	setSecrets(t)

	cfg, err := LoadConfig(writeConfig(t, serverYAML))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("Server.Addr = %q, ожидалось \":9090\"", cfg.Server.Addr)
	}
	if cfg.Server.CacheTTLToday != 30*time.Second {
		t.Errorf("CacheTTLToday = %v, ожидалось 30s", cfg.Server.CacheTTLToday)
	}
	if cfg.Server.CacheTTLPast != 2*time.Hour {
		t.Errorf("CacheTTLPast = %v, ожидалось 2h", cfg.Server.CacheTTLPast)
	}
}
```

Добавьте `"time"` в импорты файла — `os` и `strings` там уже есть.

- [ ] **Step 2: Прогнать тесты и убедиться, что они падают**

```bash
cd back_end && go test ./internal/config/ -run 'RequireGoogle|Server|БезGoogle' -v
```

Ожидается: ошибки компиляции — `cfg.RequireGoogle undefined` и `cfg.Server undefined`.

- [ ] **Step 3: Добавить блок server в структуру Config**

В `back_end/internal/config/config.go`, внутри `type Config struct`, после блока `Google`:

```go
	// Server — настройки веб-сервера дашборда. Блок необязательный:
	// пустые поля добираются значениями по умолчанию в prepare. Требовать
	// его нельзя — это сломало бы cmd/report на существующих конфигах.
	Server struct {
		Addr          string        `yaml:"addr"`
		CacheTTLToday time.Duration `yaml:"cache_ttl_today"`
		CacheTTLPast  time.Duration `yaml:"cache_ttl_past"`
	} `yaml:"server"`
```

`yaml.v3` разбирает `"60s"` прямо в `time.Duration`, отдельного разбора строки не нужно.

- [ ] **Step 4: Вынести проверки Google в RequireGoogle**

Удалите из `prepare()` четыре проверки Google (блок от `if c.Google.OAuthClientPath == ""` до `retention_days должен быть > 0`) и добавьте в файл новый метод:

```go
// RequireGoogle проверяет настройки публикации в Google.
//
// Отдельно от prepare, потому что их требует только cmd/report: веб-серверу
// дашборда Google не нужен, и падать на старте из-за незаданного токена
// он не должен.
func (c *Config) RequireGoogle() error {
	var errs []error

	if c.Google.OAuthClientPath == "" {
		errs = append(errs, errors.New("переменная окружения GOOGLE_OAUTH_CLIENT не задана"))
	}
	if c.Google.FolderID == "" {
		errs = append(errs, errors.New("google.folder_id обязателен"))
	}
	if c.Google.TokenPath == "" {
		errs = append(errs, errors.New("google.token_path обязателен"))
	}
	if c.Google.RetentionDays <= 0 {
		errs = append(errs, errors.New("google.retention_days должен быть > 0"))
	}

	return errors.Join(errs...)
}
```

- [ ] **Step 5: Добрать умолчания server в prepare**

В конце `prepare()`, **перед** `return errors.Join(errs...)`:

```go
	// Умолчания веб-сервера. Не ошибки: блок server необязателен.
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.CacheTTLToday <= 0 {
		c.Server.CacheTTLToday = time.Minute
	}
	if c.Server.CacheTTLPast <= 0 {
		c.Server.CacheTTLPast = time.Hour
	}
```

- [ ] **Step 6: Позвать RequireGoogle из cmd/report**

В `back_end/cmd/report/main.go`, в функции `run`, сразу после успешного `config.LoadConfig`:

```go
	cfg, err := config.LoadConfig(opts.configPath)
	if err != nil {
		return err
	}
	// Публикация в Google — работа именно этой команды, поэтому её настройки
	// проверяем здесь, а не в LoadConfig: веб-сервер обходится без них.
	if err := cfg.RequireGoogle(); err != nil {
		return err
	}
```

- [ ] **Step 7: Прогнать тесты**

```bash
cd back_end && go test ./... && go vet ./...
```

Ожидается: все пакеты `ok`. Если упали старые тесты `config`, ожидавшие ошибок Google от `LoadConfig`, — поправьте их: теперь эти ошибки приходят из `RequireGoogle`.

- [ ] **Step 8: Проверить, что cmd/report ругается как раньше**

```bash
cd back_end && unset GOOGLE_OAUTH_CLIENT && go run ./cmd/report -config configs/config.example.yaml 2>&1 | head -5
```

Ожидается: жалоба на незаданный `GOOGLE_OAUTH_CLIENT` — ровно тем же текстом, что и до правки.

- [ ] **Step 9: Описать блок server в примере конфига**

В конец `back_end/configs/config.example.yaml`:

```yaml
# Веб-сервер дашборда. Блок необязательный: показаны значения по умолчанию.
# Читает его только cmd/api-server, cmd/report на него не смотрит.
server:
  addr: ":8080"
  # Сегодняшний день ещё дописывается, поэтому живёт в кэше недолго.
  cache_ttl_today: 60s
  # Прошедшие сутки закрыты и не меняются — их можно держать дольше.
  cache_ttl_past: 1h
```

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat(config): RequireGoogle и необязательный блок server

Проверки Google уезжают из prepare в отдельный метод: их требует только
cmd/report. Веб-серверу дашборда Google не нужен, и стартовать без
GOOGLE_OAUTH_CLIENT он должен нормально."
```

---

### Task 3: internal/api — DTO и перевод report.Job в JSON

`report.Job` держит нулевой `time.Time`, который ушёл бы в JSON как `0001-01-01T00:00:00Z`. Заводим отдельные типы транспорта, чтобы не вешать json-теги на доменный тип.

**Files:**
- Create: `back_end/internal/api/dto.go`
- Test: `back_end/internal/api/dto_test.go`

**Interfaces:**
- Consumes: `report.Job` с полями `Environment, Label, Name string`, `OK bool`, `Start, End time.Time`, `Node string`, `Nodes []string`; метод `HasTimes() bool`
- Produces:
  - `type JobDTO struct` и `type ReportDTO struct` (поля ниже)
  - `func jobDTOs(jobs []report.Job) []JobDTO`

- [ ] **Step 1: Написать падающий тест**

Создайте `back_end/internal/api/dto_test.go`:

```go
package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backup-report/internal/report"
)

func TestJobDTOСВременами(t *testing.T) {
	start := time.Date(2026, 9, 4, 1, 0, 1, 0, time.UTC)
	end := time.Date(2026, 9, 4, 2, 23, 20, 0, time.UTC)

	got := jobDTOs([]report.Job{{
		Environment: "KT", Label: "MINIO_BACKUPS", Name: "MinIO",
		OK: true, Start: start, End: end, Node: "kt-minio01",
		Nodes: []string{"kt-minio01"},
	}})

	if len(got) != 1 {
		t.Fatalf("получено %d задач, ожидалась 1", len(got))
	}
	j := got[0]
	if j.Start == nil || !j.Start.Equal(start) {
		t.Errorf("Start = %v, ожидалось %v", j.Start, start)
	}
	if j.DurationSeconds == nil || *j.DurationSeconds != 4999 {
		t.Errorf("DurationSeconds = %v, ожидалось 4999", j.DurationSeconds)
	}
}

func TestJobDTOБезВремёнДаётNull(t *testing.T) {
	// Статус ERROR: команда не запускалась, времён нет вовсе.
	got := jobDTOs([]report.Job{{
		Environment: "KT", Label: "MINIO_VK_CLOUD_BACKUP", Name: "MinIO VK Cloud",
		OK: false, Nodes: []string{"kt-backup01"},
	}})

	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	for _, want := range []string{`"start":null`, `"end":null`, `"duration_seconds":null`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("в JSON нет %s; получено: %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "0001-01-01") {
		t.Errorf("нулевое время утекло в JSON: %s", raw)
	}
}

func TestNodesВсегдаМассив(t *testing.T) {
	// У задачи без нод Nodes равен nil. В JSON это обязано быть [],
	// иначе .map на фронте падает.
	got := jobDTOs([]report.Job{{Environment: "KT", Name: "MinIO", Nodes: nil}})

	raw, _ := json.Marshal(got[0])
	if !strings.Contains(string(raw), `"nodes":[]`) {
		t.Errorf("nodes сериализовались не в []; получено: %s", raw)
	}
}

func TestПустойСписокЗадачДаётМассив(t *testing.T) {
	raw, _ := json.Marshal(jobDTOs(nil))
	if string(raw) != "[]" {
		t.Errorf("пустой список задач дал %s, ожидалось []", raw)
	}
}
```

- [ ] **Step 2: Прогнать тест и убедиться, что он падает**

```bash
cd back_end && go test ./internal/api/ -v
```

Ожидается: `undefined: jobDTOs`.

- [ ] **Step 3: Написать dto.go**

```go
// Package api отдаёт отчёт по бэкапам за сутки по HTTP.
//
// Пакет намеренно не знает ни про Google Sheets, ни про internal/app: ему
// нужны только источник сообщений, разбор и свёртка в задачи. Благодаря
// этому бинарь веб-сервера не тянет Google API.
package api

import (
	"time"

	"backup-report/internal/report"
)

// JobDTO — задача бэкапа в том виде, в каком её видит фронт.
//
// Отдельный тип, а не report.Job с тегами: у Job нулевой time.Time означает
// «времени нет», и в JSON он ушёл бы как 0001-01-01T00:00:00Z. Указатель
// даёт честный null. Заодно формат транспорта не попадает в домен.
type JobDTO struct {
	Environment string `json:"environment"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	// Времена и длительность пусты у задач со статусом ERROR:
	// команда бэкапа не запускалась, засекать было нечего.
	Start           *time.Time `json:"start"`
	End             *time.Time `json:"end"`
	DurationSeconds *int       `json:"duration_seconds"`
	// Node — нода, сделавшая бэкап; у проваленной задачи пуста.
	Node string `json:"node"`
	// Nodes — все отчитавшиеся ноды. Всегда массив, никогда null:
	// на null у фронта падает .map.
	Nodes []string `json:"nodes"`
}

// StatsDTO считает сообщения, а не задачи: Messages — сколько сообщений отдал
// канал, Backups — сколько из них разобралось, Skipped — сколько отсеялось
// как не про бэкап. Задач всегда меньше, чем Backups: они сворачиваются
// по нодам.
type StatsDTO struct {
	Messages int `json:"messages"`
	Backups  int `json:"backups"`
	Skipped  int `json:"skipped"`
}

// ReportDTO — ответ ручки /api/report.
type ReportDTO struct {
	Date      string    `json:"date"`
	FetchedAt time.Time `json:"fetched_at"`
	// Cached говорит фронту, что данные пришли из кэша. Без этого кнопка
	// «обновить» выглядит сломанной, когда ответ не менялся.
	Cached bool     `json:"cached"`
	Stats  StatsDTO `json:"stats"`
	Jobs   []JobDTO `json:"jobs"`
}

// ErrorDTO — единая форма ошибки для всех кодов.
type ErrorDTO struct {
	Error string `json:"error"`
}

// jobDTOs переводит задачи в форму транспорта.
func jobDTOs(jobs []report.Job) []JobDTO {
	out := make([]JobDTO, 0, len(jobs)) // не nil: пустой отчёт должен дать [], не null
	for _, j := range jobs {
		d := JobDTO{
			Environment: j.Environment,
			Label:       j.Label,
			Name:        j.Name,
			OK:          j.OK,
			Node:        j.Node,
			Nodes:       j.Nodes,
		}
		if d.Nodes == nil {
			d.Nodes = []string{}
		}
		if j.HasTimes() {
			start, end := j.Start, j.End
			secs := int(end.Sub(start).Seconds())
			d.Start, d.End, d.DurationSeconds = &start, &end, &secs
		}
		out = append(out, d)
	}
	return out
}
```

- [ ] **Step 4: Прогнать тесты**

```bash
cd back_end && go test ./internal/api/ -v && go vet ./internal/api/
```

Ожидается: все четыре теста PASS.

- [ ] **Step 5: Commit**

```bash
git add back_end/internal/api/
git commit -m "feat(api): типы транспорта для отчёта

Отдельные DTO вместо тегов на report.Job: нулевое время уходит в JSON
как null, а не 0001-01-01, и nodes всегда массив."
```

---

### Task 4: internal/api — кэш с мьютексом

Мьютекс держится **на всё время похода в Telegram**. Обычно это дурная практика, но здесь она умышленна: она же не даёт двум MTProto-соединениям писать в один `data/session.json`.

**Files:**
- Create: `back_end/internal/api/cache.go`
- Test: `back_end/internal/api/cache_test.go`

**Interfaces:**
- Consumes: `ReportDTO` из Task 3; `report.DateLayout` (константа `"2006-01-02"`)
- Produces:
  - `type loader struct`
  - `func newLoader(fetch func(context.Context, time.Time) (ReportDTO, error), loc *time.Location, ttlToday, ttlPast time.Duration) *loader`
  - `func (l *loader) get(ctx context.Context, day time.Time) (ReportDTO, error)`
  - подменяемое поле `l.now func() time.Time` для тестов

- [ ] **Step 1: Написать падающие тесты**

Создайте `back_end/internal/api/cache_test.go`:

```go
package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingFetch считает походы за данными.
func countingFetch(calls *atomic.Int32, err error) func(context.Context, time.Time) (ReportDTO, error) {
	return func(_ context.Context, day time.Time) (ReportDTO, error) {
		calls.Add(1)
		if err != nil {
			return ReportDTO{}, err
		}
		return ReportDTO{Date: day.Format("2006-01-02")}, nil
	}
}

func TestВторойЗапросБерётИзКэша(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	first, err := l.get(context.Background(), day)
	if err != nil {
		t.Fatalf("первый запрос: %v", err)
	}
	if first.Cached {
		t.Error("первый ответ помечен как кэшированный")
	}

	second, err := l.get(context.Background(), day)
	if err != nil {
		t.Fatalf("второй запрос: %v", err)
	}
	if !second.Cached {
		t.Error("второй ответ не помечен как кэшированный")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1", got)
	}
}

func TestИстёкшийTTLЗаставляетСходитьЗаново(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	now := day.Add(10 * time.Hour)
	l.now = func() time.Time { return now }

	l.get(context.Background(), day)
	now = now.Add(2 * time.Minute) // TTL сегодняшнего дня — минута
	l.get(context.Background(), day)

	if got := calls.Load(); got != 2 {
		t.Errorf("походов за данными: %d, ожидалось 2", got)
	}
}

func TestПрошедшийДеньЖивётДольше(t *testing.T) {
	var calls atomic.Int32
	l := newLoader(countingFetch(&calls, nil), time.UTC, time.Minute, time.Hour)
	past := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	l.get(context.Background(), past)
	now = now.Add(30 * time.Minute) // больше TTL сегодняшнего, меньше TTL прошедшего
	l.get(context.Background(), past)

	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1: прошедшие сутки не меняются", got)
	}
}

func TestОшибкаНеКэшируется(t *testing.T) {
	var calls atomic.Int32
	boom := errors.New("канал недоступен")
	l := newLoader(countingFetch(&calls, boom), time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	l.get(context.Background(), day)
	l.get(context.Background(), day)

	if got := calls.Load(); got != 2 {
		t.Errorf("походов за данными: %d, ожидалось 2: неудача не должна оседать в кэше", got)
	}
}

func TestДесятьГорутинДаютОдинПоход(t *testing.T) {
	var calls atomic.Int32
	slow := func(_ context.Context, day time.Time) (ReportDTO, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // имитируем поход в сеть
		return ReportDTO{Date: day.Format("2006-01-02")}, nil
	}
	l := newLoader(slow, time.UTC, time.Minute, time.Hour)
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return day.Add(10 * time.Hour) }

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.get(context.Background(), day); err != nil {
				t.Errorf("запрос упал: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("походов за данными: %d, ожидался 1", got)
	}
}
```

- [ ] **Step 2: Прогнать тесты и убедиться, что они падают**

```bash
cd back_end && go test ./internal/api/ -run 'Кэш|TTL|Горутин|Ошибка|Прошедший' -v
```

Ожидается: `undefined: newLoader`.

- [ ] **Step 3: Написать cache.go**

```go
package api

import (
	"context"
	"sync"
	"time"

	"backup-report/internal/dates"
	"backup-report/internal/report"
)

// entry — готовый отчёт и момент, после которого его надо перечитать.
type entry struct {
	rep       ReportDTO
	expiresAt time.Time
}

// loader отдаёт отчёт за сутки, ходя за ним не чаще, чем нужно.
//
// Мьютекс держится на всё время похода в Telegram, и это сделано умышленно.
// Обычно блокировка вокруг сетевого вызова — ошибка, но здесь она заодно
// решает вторую задачу: MTProto-соединение пишет data/session.json, и двух
// одновременных соединений с одной сессией быть не должно. Побочный
// полезный эффект — десять одновременных запросов за одну дату дают один
// поход, остальные читают уже готовое.
type loader struct {
	mu    sync.Mutex
	items map[string]entry

	fetch    func(ctx context.Context, day time.Time) (ReportDTO, error)
	loc      *time.Location
	ttlToday time.Duration
	ttlPast  time.Duration

	// now подменяется в тестах: иначе TTL не проверить, не засыпая.
	now func() time.Time
}

func newLoader(
	fetch func(ctx context.Context, day time.Time) (ReportDTO, error),
	loc *time.Location,
	ttlToday, ttlPast time.Duration,
) *loader {
	return &loader{
		items:    make(map[string]entry),
		fetch:    fetch,
		loc:      loc,
		ttlToday: ttlToday,
		ttlPast:  ttlPast,
		now:      time.Now,
	}
}

// get отдаёт отчёт за сутки — из кэша либо сходив за ним.
func (l *loader) get(ctx context.Context, day time.Time) (ReportDTO, error) {
	key := day.In(l.loc).Format(report.DateLayout)

	l.mu.Lock()
	defer l.mu.Unlock()

	// Проверка под замком, а не до него: пока мы ждали, данные мог положить
	// сосед, и второй поход в Telegram был бы напрасным.
	if e, ok := l.items[key]; ok && l.now().Before(e.expiresAt) {
		e.rep.Cached = true
		return e.rep, nil
	}

	rep, err := l.fetch(ctx, day)
	if err != nil {
		// Неудачу не кэшируем: следующий запрос должен попробовать снова.
		return ReportDTO{}, err
	}
	rep.Cached = false
	l.items[key] = entry{rep: rep, expiresAt: l.now().Add(l.ttl(day))}
	return rep, nil
}

// ttl: сегодняшний день ещё дописывается, прошедшие сутки закрыты навсегда.
func (l *loader) ttl(day time.Time) time.Duration {
	today := dates.StartOfDay(l.now().In(l.loc), l.loc)
	if !dates.StartOfDay(day.In(l.loc), l.loc).Before(today) {
		return l.ttlToday
	}
	return l.ttlPast
}
```

Если сигнатура `dates.StartOfDay` окажется иной — посмотрите `back_end/internal/dates/dates.go` и подставьте фактическую; смысл в том, чтобы сравнивать начала суток, а не моменты.

- [ ] **Step 4: Прогнать тесты, в том числе детектором гонок**

```bash
cd back_end && go test ./internal/api/ -v && go test ./internal/api/ -race -run Горутин
```

Ожидается: все PASS, детектор гонок молчит.

- [ ] **Step 5: Commit**

```bash
git add back_end/internal/api/
git commit -m "feat(api): кэш отчётов с мьютексом вокруг похода в Telegram

Мьютекс держится на всё время запроса умышленно: он же не даёт двум
MTProto-соединениям писать в один session.json. Сегодняшний день живёт
в кэше минуту, прошедшие — час: закрытые сутки не меняются."
```

---

### Task 5: internal/api — сборка отчёта и ручки на Gin

**Files:**
- Create: `back_end/internal/api/handler.go`
- Create: `back_end/internal/api/build.go`
- Modify: `back_end/internal/telegram/telegram.go` (добавить `FloodWaitFor`)
- Test: `back_end/internal/api/handler_test.go`

**Interfaces:**
- Consumes: `jobDTOs` (Task 3), `newLoader`/`get` (Task 4), `parser.Parse(m RawMessage, labels map[string]string, loc *time.Location) (Backup, error)`, `parser.ErrNotBackupMessage`, `report.Aggregate([]parser.Backup) []report.Job`, `telegram.ErrNoSession`
- Produces:
  - `type Source interface { FetchDay(ctx context.Context, day time.Time) ([]parser.RawMessage, error) }`
  - `type Options struct { Labels map[string]string; Location *time.Location; CacheTTLToday, CacheTTLPast time.Duration }`
  - `func New(opts Options, src Source, log *slog.Logger) *Handler`
  - `func (h *Handler) Register(r gin.IRouter)` — вешает `GET /api/report` и `GET /api/health`
  - `func telegram.FloodWaitFor(err error) (time.Duration, bool)`

- [ ] **Step 1: Поставить Gin**

```bash
cd back_end && go get github.com/gin-gonic/gin@latest && go mod tidy
```

- [ ] **Step 2: Написать падающие тесты**

Создайте `back_end/internal/api/handler_test.go`:

```go
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
```

- [ ] **Step 3: Прогнать тесты и убедиться, что они падают**

```bash
cd back_end && go test ./internal/api/ -run 'Отчёт|Дата|Кривая|Будущая|Сессия|Прочая|Health' -v
```

Ожидается: `undefined: New`, `undefined: Options`, `undefined: Source`.

- [ ] **Step 4: Добавить FloodWaitFor в пакет telegram**

В конец `back_end/internal/telegram/telegram.go`:

```go
// FloodWaitFor сообщает, сколько Telegram просит подождать, если ошибка —
// это FLOOD_WAIT. Живёт здесь, а не у вызывающей стороны, чтобы знание
// про gotd не расползалось по пакетам.
func FloodWaitFor(err error) (time.Duration, bool) {
	return tgerr.AsFloodWait(err)
}
```

`tgerr` уже импортирован в этом файле.

- [ ] **Step 5: Написать build.go — сборку отчёта**

```go
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
```

- [ ] **Step 6: Написать handler.go**

```go
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
```

- [ ] **Step 7: Прогнать тесты**

```bash
cd back_end && go test ./internal/api/ -v && go vet ./internal/api/
```

Ожидается: все тесты PASS.

- [ ] **Step 8: Убедиться, что api не тянет Google**

```bash
cd back_end && go list -deps ./internal/api | grep -c "google.golang.org/api\|backup-report/internal/gsheets"
```

Ожидается: `0`. Если больше нуля — где-то пролез импорт `gsheets` или `app`, найдите и уберите.

- [ ] **Step 9: Commit**

```bash
git add back_end/internal/api/ back_end/internal/telegram/telegram.go back_end/go.mod back_end/go.sum
git commit -m "feat(api): ручки /api/report и /api/health на Gin

Сборка отчёта повторяет app.Once минус публикация. Протухшая сессия
и FLOOD_WAIT дают 503 с внятным текстом, а не роняют сервер."
```

---

### Task 6: cmd/api-server

**Files:**
- Create: `back_end/cmd/api-server/main.go`

**Interfaces:**
- Consumes: `config.LoadConfig`, `telegram.New`, `api.New`, `api.Handler.Register`
- Produces: бинарь `server`; флаг `-config` со значением по умолчанию `config.yaml`

- [ ] **Step 1: Написать main.go**

```go
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
```

- [ ] **Step 2: Собрать и проверить, что бинарь есть**

```bash
cd back_end && go build -o /tmp/api-server ./cmd/api-server && go vet ./cmd/api-server
```

Ожидается: сборка проходит, `go vet` молчит.

- [ ] **Step 3: Проверить, что Google не попал в бинарь сервера**

```bash
cd back_end && go list -deps ./cmd/api-server | grep -c "backup-report/internal/gsheets"
```

Ожидается: `0`. Это и есть смысл разделения бинарей.

- [ ] **Step 4: Запустить без переменных Google и убедиться, что сервер стартует**

Секреты Telegram нужны, а `GOOGLE_OAUTH_CLIENT` — намеренно нет.

```bash
cd back_end
export TELEGRAM_API_ID TELEGRAM_API_HASH TELEGRAM_PHONE   # из своего .env или env.sh
unset GOOGLE_OAUTH_CLIENT
go run ./cmd/api-server &
sleep 3
curl -s localhost:8080/api/health; echo
curl -s -o /dev/null -w 'report: %{http_code}\n' 'localhost:8080/api/report?date=2026-09-01'
```

Ожидается: `{"status":"ok"}` от health. От `/api/report` — либо `200`, либо `503`, если сессия Telegram протухла; **не** падение процесса. Именно то, что сервер поднялся без `GOOGLE_OAUTH_CLIENT`, и проверяет Task 2.

Остановите фоновый процесс: `kill %1`.

- [ ] **Step 5: Commit**

```bash
git add back_end/cmd/api-server/
git commit -m "feat: бинарь server — дашборд по HTTP

Gin поверх internal/api, лог запросов через slog, graceful shutdown
своим http.Server. В зависимостях нет ни gsheets, ни Google API."
```

---

### Task 7: кэш access_hash в telegram.Client

`resolvePeer` обходит все диалоги аккаунта постранично по обеим папкам ради `access_hash`. Обход остаётся — он защищает от «канал уехал в архив», — но повторять его на каждом запросе незачем: `access_hash` для аккаунта постоянен.

**Files:**
- Modify: `back_end/internal/telegram/telegram.go`
- Test: `back_end/internal/telegram/telegram_test.go`

**Interfaces:**
- Consumes: `Client.resolvePeer(ctx, api)` — существует
- Produces: `Client.cachedPeer() (tg.InputPeerClass, bool)`, `Client.rememberPeer(tg.InputPeerClass)` — приватные, покрываются тестом

- [ ] **Step 1: Написать падающий тест**

Добавьте в `back_end/internal/telegram/telegram_test.go`:

```go
func TestPeerНеЗапомненСразуПослеСоздания(t *testing.T) {
	c := New(Config{ChannelID: 123, Location: time.UTC})

	if _, ok := c.cachedPeer(); ok {
		t.Error("клиент отдал peer, которого ещё не искал")
	}
}

func TestPeerЗапоминаетсяИОтдаётся(t *testing.T) {
	c := New(Config{ChannelID: 123, Location: time.UTC})
	want := &tg.InputPeerChannel{ChannelID: 123, AccessHash: 999}

	c.rememberPeer(want)

	got, ok := c.cachedPeer()
	if !ok {
		t.Fatal("клиент не отдал запомненный peer")
	}
	if got != want {
		t.Errorf("отдан %#v, ожидался %#v", got, want)
	}
}

func TestPeerБезопасенДляПараллельногоДоступа(t *testing.T) {
	// Веб-сервер раздаёт запросы по горутинам, и клиент один на всех.
	c := New(Config{ChannelID: 123, Location: time.UTC})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.rememberPeer(&tg.InputPeerChannel{ChannelID: 123, AccessHash: 999})
			c.cachedPeer()
		}()
	}
	wg.Wait()
}
```

Тест на сам обход диалогов не пишем: MTProto — двоичный протокол со своей криптографией, `httptest` его не подделает. Это тот же осознанный размен, что описан в §11 дизайна первого задания.

- [ ] **Step 2: Прогнать тесты и убедиться, что они падают**

```bash
cd back_end && go test ./internal/telegram/ -run Peer -v
```

Ожидается: `c.cachedPeer undefined`, `c.rememberPeer undefined`.

- [ ] **Step 3: Добавить поля и методы в Client**

Замените объявление структуры:

```go
type Client struct {
	cfg       Config
	channelID int64 // ID, приведённый к виду MTProto

	// peer запоминается между запросами: access_hash у аккаунта постоянен,
	// а добывается обходом всех диалогов по обеим папкам — дорого повторять
	// это на каждом запросе веб-сервера. Мьютекс нужен потому, что клиент
	// один на все горутины сервера.
	peerMu sync.Mutex
	peer   tg.InputPeerClass
}
```

И добавьте рядом с `resolvePeer`:

```go
// cachedPeer отдаёт запомненный peer канала, если он уже находился.
func (c *Client) cachedPeer() (tg.InputPeerClass, bool) {
	c.peerMu.Lock()
	defer c.peerMu.Unlock()
	return c.peer, c.peer != nil
}

// rememberPeer запоминает найденный peer до конца жизни процесса.
func (c *Client) rememberPeer(p tg.InputPeerClass) {
	c.peerMu.Lock()
	defer c.peerMu.Unlock()
	c.peer = p
}
```

Добавьте `"sync"` в импорты.

- [ ] **Step 4: Научить resolvePeer пользоваться кэшем**

Замените тело `resolvePeer`:

```go
func (c *Client) resolvePeer(ctx context.Context, api *tg.Client) (tg.InputPeerClass, error) {
	if peer, ok := c.cachedPeer(); ok {
		return peer, nil
	}

	var peer tg.InputPeerClass
	err := eachChannel(ctx, api, func(ch *tg.Channel) bool {
		if ch.ID != c.channelID {
			return false
		}
		peer = &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
		return true
	})
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, fmt.Errorf("канал %d не найден среди диалогов аккаунта", c.channelID)
	}

	c.rememberPeer(peer)
	return peer, nil
}
```

- [ ] **Step 5: Прогнать тесты с детектором гонок**

```bash
cd back_end && go test ./internal/telegram/ -race -v && go test ./... && go vet ./...
```

Ожидается: все PASS, детектор гонок молчит.

- [ ] **Step 6: Commit**

```bash
git add back_end/internal/telegram/
git commit -m "perf(telegram): запоминать access_hash канала между запросами

Обход всех диалогов ради access_hash повторялся на каждом запросе, хотя
для аккаунта он постоянен. Обход остался — он защищает от уехавшего
в архив канала, — но выполняется один раз за жизнь процесса."
```

---

### Task 8: front_end — каркас и чистые функции

**Files:**
- Create: `front_end/` (каркас Vite)
- Create: `front_end/src/types.ts`
- Create: `front_end/src/lib/format.ts`
- Create: `front_end/src/lib/group.ts`
- Test: `front_end/src/lib/format.test.ts`, `front_end/src/lib/group.test.ts`

**Interfaces:**
- Consumes: форма JSON из Task 3
- Produces:
  - `type Job`, `type ReportResponse`
  - `formatTime(iso: string | null): string`, `formatDuration(seconds: number | null): string`
  - `type EnvGroup = { environment: string; jobs: Job[]; ok: number; total: number }`
  - `groupByEnvironment(jobs: Job[]): EnvGroup[]`

- [ ] **Step 1: Развернуть каркас**

```bash
cd /Users/madi_gaziz/Desktop/work/practice/test_project/back-up
npm create vite@latest front_end -- --template react-ts
cd front_end && npm install && npm install -D tailwindcss @tailwindcss/vite vitest
```

Версии не фиксируем вручную — берём то, что ставит `create vite` и `npm install`.

- [ ] **Step 2: Подключить Tailwind и прокси на бэк**

`front_end/vite.config.ts`:

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // В деве фронт живёт на 5173, бэк на 8080. Прокси делает то же,
    // что nginx в проде: браузер видит один origin, CORS не нужен.
    proxy: { '/api': 'http://localhost:8080' },
  },
})
```

`front_end/src/index.css` — замените всё содержимое на:

```css
@import "tailwindcss";
```

В `front_end/package.json`, в `scripts`, добавьте: `"test": "vitest run"`.

- [ ] **Step 3: Написать типы**

`front_end/src/types.ts`:

```ts
// Зеркало DTO из back_end/internal/api/dto.go. Меняется только вместе с ним.

export type Job = {
  environment: string
  label: string
  name: string
  ok: boolean
  /** null у задач со статусом ERROR: команда не запускалась. */
  start: string | null
  end: string | null
  duration_seconds: number | null
  /** Нода, сделавшая бэкап. У проваленной задачи пуста. */
  node: string
  /** Все отчитавшиеся ноды. С бэка всегда массив, никогда null. */
  nodes: string[]
}

export type Stats = {
  messages: number
  backups: number
  skipped: number
}

export type ReportResponse = {
  date: string
  fetched_at: string
  cached: boolean
  stats: Stats
  jobs: Job[]
}
```

- [ ] **Step 4: Написать падающие тесты для format**

`front_end/src/lib/format.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { formatTime, formatDuration } from './format'

describe('formatTime', () => {
  it('показывает время без даты', () => {
    expect(formatTime('2026-09-04T01:00:01+05:00')).toBe('01:00:01')
  })

  it('на отсутствующем времени даёт прочерк', () => {
    expect(formatTime(null)).toBe('—')
  })
})

describe('formatDuration', () => {
  it('переводит секунды в часы и минуты как в отчёте', () => {
    expect(formatDuration(4999)).toBe('1h 23m')
  })

  it('дополняет минуты нулём', () => {
    expect(formatDuration(300)).toBe('0h 05m')
  })

  it('на отсутствующей длительности даёт прочерк', () => {
    expect(formatDuration(null)).toBe('—')
  })
})
```

- [ ] **Step 5: Прогнать и убедиться, что падает**

```bash
cd front_end && npm test
```

Ожидается: `Failed to resolve import "./format"`.

- [ ] **Step 6: Написать format.ts**

```ts
/** Прочерк вместо пустоты — тот же знак, что и в отчёте Google Sheets. */
const NO_VALUE = '—'

/** Время без даты: дата одна на весь экран и стоит в поле выбора. */
export function formatTime(iso: string | null): string {
  if (!iso) return NO_VALUE
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return NO_VALUE
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/**
 * Длительность в том же виде, что и в Google Sheets: "1h 23m".
 * Формат повторяет humanDuration из internal/report — чтобы отчёт и дашборд
 * читались одинаково.
 */
export function formatDuration(seconds: number | null): string {
  if (seconds === null || seconds < 0) return NO_VALUE
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor(seconds / 60) % 60
  return `${hours}h ${String(minutes).padStart(2, '0')}m`
}
```

- [ ] **Step 7: Написать падающие тесты для group**

`front_end/src/lib/group.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { groupByEnvironment } from './group'
import type { Job } from '../types'

function job(environment: string, name: string, ok: boolean): Job {
  return {
    environment, name, ok,
    label: name.toUpperCase(),
    start: null, end: null, duration_seconds: null,
    node: '', nodes: [],
  }
}

describe('groupByEnvironment', () => {
  it('на пустом списке даёт пустой список', () => {
    expect(groupByEnvironment([])).toEqual([])
  })

  it('собирает задачи одной среды, даже если они не подряд', () => {
    // Бэк кладёт все провалы в начало, поэтому задачи одной среды
    // приходят двумя кусками. Группировка обязана это пережить.
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('PROD KPO', 'MongoDB', false),
      job('KT', 'MinIO', true),
      job('KT', 'PostgreSQL', true),
    ]

    const groups = groupByEnvironment(jobs)

    expect(groups.map((g) => g.environment)).toEqual(['KT', 'PROD KPO'])
    expect(groups[0].jobs).toHaveLength(3)
  })

  it('ставит среду с провалом первой, наследуя порядок бэка', () => {
    const jobs = [
      job('PROD KPO', 'MongoDB', false),
      job('KT', 'MinIO', true),
    ]

    expect(groupByEnvironment(jobs)[0].environment).toBe('PROD KPO')
  })

  it('считает успешные и все задачи среды', () => {
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('KT', 'MinIO', true),
      job('KT', 'PostgreSQL', true),
    ]

    const [kt] = groupByEnvironment(jobs)
    expect(kt.ok).toBe(2)
    expect(kt.total).toBe(3)
  })

  it('сохраняет порядок задач внутри среды', () => {
    const jobs = [
      job('KT', 'MinIO VK Cloud', false),
      job('KT', 'MinIO', true),
    ]

    expect(groupByEnvironment(jobs)[0].jobs.map((j) => j.name))
      .toEqual(['MinIO VK Cloud', 'MinIO'])
  })
})
```

- [ ] **Step 8: Прогнать и убедиться, что падает**

```bash
cd front_end && npm test
```

Ожидается: `Failed to resolve import "./group"`.

- [ ] **Step 9: Написать group.ts**

```ts
import type { Job } from '../types'

export type EnvGroup = {
  environment: string
  jobs: Job[]
  /** Сколько задач среды прошло. */
  ok: number
  total: number
}

/**
 * Группирует задачи по средам в порядке первого появления.
 *
 * Своей сортировки здесь нет и быть не должно. Бэк уже отдаёт задачи
 * отсортированными — сначала все провалы, потом всё успешное, — поэтому
 * порядок первого появления сам поднимает среду с провалом наверх,
 * а внутри среды провал оказывается первой строкой.
 *
 * Задачи одной среды приходят несколькими кусками именно из-за этой
 * сортировки, поэтому собираем по всему списку, а не подряд идущими
 * участками.
 */
export function groupByEnvironment(jobs: Job[]): EnvGroup[] {
  const byEnv = new Map<string, EnvGroup>()

  for (const job of jobs) {
    let group = byEnv.get(job.environment)
    if (!group) {
      group = { environment: job.environment, jobs: [], ok: 0, total: 0 }
      byEnv.set(job.environment, group)
    }
    group.jobs.push(job)
    group.total++
    if (job.ok) group.ok++
  }

  return [...byEnv.values()] // Map сохраняет порядок вставки
}
```

- [ ] **Step 10: Прогнать все тесты фронта**

```bash
cd front_end && npm test && npx tsc --noEmit
```

Ожидается: все тесты PASS, `tsc` молчит.

- [ ] **Step 11: Commit**

```bash
git add front_end/
git commit -m "feat(front): каркас Vite + типы, форматирование и группировка

Группировка по средам наследует порядок бэка: сортировки на фронте нет,
и правило «что считать провалом» не дублируется."
```

---

### Task 9: front_end — экран

**Files:**
- Create: `front_end/src/api/client.ts`
- Create: `front_end/src/components/StatusBadge.tsx`
- Create: `front_end/src/components/JobRow.tsx`
- Create: `front_end/src/components/EnvSection.tsx`
- Create: `front_end/src/components/DateBar.tsx`
- Modify: `front_end/src/App.tsx`

**Interfaces:**
- Consumes: `ReportResponse`, `Job` (Task 8); `groupByEnvironment`, `formatTime`, `formatDuration` (Task 8)
- Produces: работающий экран со всеми шестью состояниями

- [ ] **Step 1: Написать клиент API**

`front_end/src/api/client.ts`:

```ts
import type { ReportResponse } from '../types'

/** Ошибка с текстом от сервера и, если он просил подождать, — со сколькими секундами. */
export class ApiError extends Error {
  constructor(message: string, readonly retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
  }
}

export async function fetchReport(date: string): Promise<ReportResponse> {
  const res = await fetch(`/api/report?date=${encodeURIComponent(date)}`)

  if (!res.ok) {
    // Тело ошибки всегда {"error": "..."} — но сеть могла оборваться
    // на полуслове, поэтому разбор защищаем.
    let message = `сервер ответил ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      // оставляем сообщение по коду ответа
    }
    const retryAfter = Number(res.headers.get('Retry-After'))
    throw new ApiError(message, Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined)
  }

  return res.json()
}
```

- [ ] **Step 2: Написать мелкие компоненты**

`front_end/src/components/StatusBadge.tsx`:

```tsx
export function StatusBadge({ ok }: { ok: boolean }) {
  const cls = ok
    ? 'bg-green-100 text-green-800'
    : 'bg-red-100 text-red-800'
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-semibold ${cls}`}>
      {ok ? 'OK' : 'FAIL'}
    </span>
  )
}
```

`front_end/src/components/JobRow.tsx`:

```tsx
import type { Job } from '../types'
import { formatTime, formatDuration } from '../lib/format'
import { StatusBadge } from './StatusBadge'

export function JobRow({ job }: { job: Job }) {
  // У успеха показываем ноду, что отработала; у провала — всех, кто отчитался:
  // у проваленной задачи это единственный след для разбора.
  const nodes = job.ok ? job.node : job.nodes.join(', ')

  return (
    <tr className="border-t border-gray-100">
      <td className="py-2 pr-4"><StatusBadge ok={job.ok} /></td>
      <td className="py-2 pr-4 font-medium">{job.name}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatTime(job.start)}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatTime(job.end)}</td>
      <td className="py-2 pr-4 tabular-nums text-gray-600">{formatDuration(job.duration_seconds)}</td>
      <td className="py-2 text-gray-500">{nodes || '—'}</td>
    </tr>
  )
}
```

`front_end/src/components/EnvSection.tsx`:

```tsx
import type { EnvGroup } from '../lib/group'
import { JobRow } from './JobRow'

export function EnvSection({ group }: { group: EnvGroup }) {
  const allOk = group.ok === group.total

  return (
    <section className="mb-8">
      <header className="mb-2 flex items-baseline justify-between border-b border-gray-200 pb-1">
        <h2 className="text-lg font-semibold">{group.environment}</h2>
        <span className={allOk ? 'text-sm text-gray-500' : 'text-sm font-semibold text-red-700'}>
          {group.ok} / {group.total}
        </span>
      </header>

      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-xs uppercase tracking-wide text-gray-400">
            <th className="pb-1 pr-4 font-normal">Статус</th>
            <th className="pb-1 pr-4 font-normal">Бэкап</th>
            <th className="pb-1 pr-4 font-normal">Начало</th>
            <th className="pb-1 pr-4 font-normal">Конец</th>
            <th className="pb-1 pr-4 font-normal">Длительность</th>
            <th className="pb-1 font-normal">Ноды</th>
          </tr>
        </thead>
        <tbody>
          {group.jobs.map((job) => (
            <JobRow key={`${job.environment}/${job.label}`} job={job} />
          ))}
        </tbody>
      </table>
    </section>
  )
}
```

Ключ строки — пара (среда, метка): именно она отличает задачи друг от друга в `report.Aggregate`, а имена могут совпадать.

- [ ] **Step 3: Написать DateBar**

`front_end/src/components/DateBar.tsx`:

```tsx
type Props = {
  date: string
  onDateChange: (date: string) => void
  onRefresh: () => void
  busy: boolean
  /** Когда сервер получил эти данные; null, пока данных нет. */
  fetchedAt: string | null
  cached: boolean
}

export function DateBar({ date, onDateChange, onRefresh, busy, fetchedAt, cached }: Props) {
  const today = new Date().toISOString().slice(0, 10)

  return (
    <div className="mb-6 flex flex-wrap items-center gap-3">
      <input
        type="date"
        value={date}
        max={today}
        onChange={(e) => onDateChange(e.target.value)}
        className="rounded border border-gray-300 px-3 py-1.5"
      />
      <button
        onClick={onRefresh}
        disabled={busy}
        className="rounded border border-gray-300 px-3 py-1.5 hover:bg-gray-50 disabled:opacity-50"
      >
        Обновить
      </button>
      {fetchedAt && (
        // Без этой подписи «обновить» выглядит сломанной, когда ответ
        // пришёл из кэша и ничего на экране не изменилось.
        <span className="text-sm text-gray-500">
          данные на {new Date(fetchedAt).toLocaleTimeString()}
          {cached && ' (из кэша)'}
        </span>
      )}
    </div>
  )
}
```

`max={today}` не даёт выбрать будущий день — тот самый случай, на который бэк отвечает 400.

- [ ] **Step 4: Написать App.tsx**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { fetchReport, ApiError } from './api/client'
import { groupByEnvironment } from './lib/group'
import type { ReportResponse } from './types'
import { DateBar } from './components/DateBar'
import { EnvSection } from './components/EnvSection'

/**
 * refreshing отделён от loading намеренно: при смене даты экран пустеет
 * и показывает скелет, при нажатии «обновить» данные остаются на месте.
 * Иначе каждое обновление мигает пустотой.
 */
type State =
  | { kind: 'loading' }
  | { kind: 'refreshing'; data: ReportResponse }
  | { kind: 'ok'; data: ReportResponse }
  | { kind: 'error'; message: string; retryAfter?: number }

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

/** Дата берётся из адреса, чтобы ссылку можно было переслать. */
function dateFromLocation(): string {
  const fromUrl = new URLSearchParams(window.location.search).get('date')
  return fromUrl && /^\d{4}-\d{2}-\d{2}$/.test(fromUrl) ? fromUrl : todayISO()
}

export default function App() {
  const [date, setDate] = useState(dateFromLocation)
  const [state, setState] = useState<State>({ kind: 'loading' })

  const load = useCallback(
    async (day: string, keepData: boolean) => {
      setState((prev) =>
        keepData && (prev.kind === 'ok' || prev.kind === 'refreshing')
          ? { kind: 'refreshing', data: prev.data }
          : { kind: 'loading' },
      )
      try {
        setState({ kind: 'ok', data: await fetchReport(day) })
      } catch (err) {
        const message = err instanceof Error ? err.message : 'неизвестная ошибка'
        const retryAfter = err instanceof ApiError ? err.retryAfter : undefined
        setState({ kind: 'error', message, retryAfter })
      }
    },
    [],
  )

  useEffect(() => {
    const url = new URL(window.location.href)
    url.searchParams.set('date', date)
    window.history.replaceState(null, '', url)
    load(date, false)
  }, [date, load])

  const data = state.kind === 'ok' || state.kind === 'refreshing' ? state.data : null
  const groups = data ? groupByEnvironment(data.jobs) : []

  return (
    <main className="mx-auto max-w-4xl p-6">
      <h1 className="mb-6 text-2xl font-bold">Бэкапы</h1>

      <DateBar
        date={date}
        onDateChange={setDate}
        onRefresh={() => load(date, true)}
        busy={state.kind === 'loading' || state.kind === 'refreshing'}
        fetchedAt={data?.fetched_at ?? null}
        cached={data?.cached ?? false}
      />

      {state.kind === 'refreshing' && (
        <div className="mb-4 h-0.5 animate-pulse rounded bg-blue-400" />
      )}

      {state.kind === 'loading' && (
        <div className="space-y-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-24 animate-pulse rounded bg-gray-100" />
          ))}
        </div>
      )}

      {state.kind === 'error' && (
        <div className="rounded border border-red-200 bg-red-50 p-4">
          <p className="mb-3 text-red-800">{state.message}</p>
          {state.retryAfter && (
            <p className="mb-3 text-sm text-red-700">
              Повторить можно через {state.retryAfter} с.
            </p>
          )}
          <button
            onClick={() => load(date, false)}
            className="rounded border border-red-300 px-3 py-1.5 text-sm hover:bg-red-100"
          >
            Повторить
          </button>
        </div>
      )}

      {data && groups.length === 0 && (
        // Пустой день — не ошибка: канал мог молчать или день ещё не начался.
        // Отдельно от сломанной сессии, иначе её молча спрячет «нет данных».
        <p className="text-gray-500">
          За {data.date} в канале нет сообщений о бэкапах.
        </p>
      )}

      {groups.map((group) => (
        <EnvSection key={group.environment} group={group} />
      ))}
    </main>
  )
}
```

- [ ] **Step 5: Проверить типы и сборку**

```bash
cd front_end && npx tsc --noEmit && npm run build && npm test
```

Ожидается: сборка проходит, тесты PASS.

- [ ] **Step 6: Проверить экран живьём**

В одном терминале:

```bash
cd back_end && set -a && . ./.env && set +a && go run ./cmd/api-server
```

В другом:

```bash
cd front_end && npm run dev
```

Откройте `http://localhost:5173`. Проверьте по очереди:

1. Дата по умолчанию — сегодня, и она появилась в адресе как `?date=`.
2. Смена даты на прошлую перезагружает экран через скелет.
3. «Обновить» не мигает пустотой, подпись «данные на …» меняется, при попадании в кэш появляется «(из кэша)».
4. Будущую дату выбрать нельзя (`max` у поля).
5. Если сессия Telegram протухла — видно сообщение про `-login`, а не «нет данных».

- [ ] **Step 7: Commit**

```bash
git add front_end/
git commit -m "feat(front): экран дашборда с секцией на каждую среду

Шесть состояний экрана, включая раздельные «пустой день» и «нет сессии»:
свалить их в одно «нет данных» значит спрятать протухшую сессию."
```

---

### Task 10: Docker, compose и README

**Files:**
- Create: `back_end/Dockerfile`
- Create: `front_end/Dockerfile`
- Create: `front_end/nginx.conf`
- Create: `docker-compose.yml`
- Create: `back_end/.env.example`
- Modify: `back_end/env.sh`
- Modify: `README.md`

**Interfaces:**
- Consumes: бинари `api-server` и `report` (Tasks 6, 1); сборка фронта (Task 9)
- Produces: `docker compose up` поднимает дашборд на `http://localhost:8080`

- [ ] **Step 1: Написать Dockerfile бэка**

`back_end/Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
# Зависимости отдельным слоем: они меняются реже кода.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/api-server ./cmd/api-server && \
    go build -trimpath -ldflags="-s -w" -o /out/report ./cmd/report

FROM alpine:3.21
# ca-certificates — для TLS к Telegram и Google.
# tzdata — обязательно: config зовёт time.LoadLocation, и без базы поясов
# это падает в рантайме, а не на сборке.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app

# WORKDIR /app, и сюда же монтируются config.yaml и data/. Так относительные
# пути из конфига (data/session.json) одинаково работают на хосте
# и в контейнере, и второй конфиг не нужен.
WORKDIR /app
COPY --from=build /out/api-server /out/report /app/
USER app
EXPOSE 8080
CMD ["/app/api-server"]
```

- [ ] **Step 2: Написать Dockerfile фронта и конфиг nginx**

`front_end/Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

FROM node:22-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

`nginx.conf` копируется в образ из контекста `front_end/`, поэтому образ
самодостаточен и не зависит от bind mount при запуске.

`front_end/nginx.conf`:

```nginx
server {
    listen 80;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    # Прокси на бэк. Благодаря ему браузер видит один origin,
    # и CORS-заголовки не нужны ни здесь, ни в Go.
    location /api/ {
        proxy_pass http://back_end:8080;
        proxy_set_header Host $host;
        # Первый запрос за день идёт в Telegram: обход диалогов плюс
        # выгрузка истории. Умолчание в 60 секунд оставляем с запасом.
        proxy_read_timeout 120s;
    }

    # SPA: любой путь отдаёт index.html, маршрутизация живёт в браузере.
    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

- [ ] **Step 3: Написать docker-compose.yml**

`docker-compose.yml` в корне:

```yaml
services:
  back_end:
    build: ./back_end
    env_file:
      - ./back_end/.env
    volumes:
      # session.json пишется контейнером — том на запись.
      - ./back_end/data:/app/data
      - ./back_end/config.yaml:/app/config.yaml:ro
    # secrets/ не монтируется намеренно: client_secret.json нужен только
    # cmd/report, а сервер про Google не знает.
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/health"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 5s
    restart: unless-stopped

  front_end:
    build: ./front_end
    ports:
      - "8080:80"
    depends_on:
      back_end:
        condition: service_healthy
    restart: unless-stopped
```

Наружу торчит только фронт: бэк живёт во внутренней сети compose.

- [ ] **Step 4: Развести секреты через .env**

Создайте `back_end/.env.example`:

```sh
# Секреты сервиса. Скопируйте в back_end/.env и заполните.
# Файл .env под .gitignore и в репозиторий не попадает.
#
# API_ID и API_HASH берутся на https://my.telegram.org
TELEGRAM_API_ID=
TELEGRAM_API_HASH=
TELEGRAM_PHONE=

# Нужен только cmd/report — веб-сервер дашборда без него работает.
GOOGLE_OAUTH_CLIENT=secrets/client_secret.json
```

Замените содержимое `back_end/env.sh` на:

```sh
# Загружает секреты из .env в окружение для локального запуска.
# Единый источник правды с docker compose: тот читает тот же .env напрямую.
set -a
. "$(dirname "$0")/.env"
set +a
```

- [ ] **Step 5: Проверить, что образы собираются**

```bash
cd /Users/madi_gaziz/Desktop/work/practice/test_project/back-up
cp back_end/.env.example back_end/.env   # если .env ещё нет — заполните значения
docker compose build 2>&1 | tail -20
```

Ожидается: оба образа собрались без ошибок.

- [ ] **Step 6: Поднять и проверить живьём**

```bash
docker compose up -d && sleep 15 && docker compose ps
curl -s localhost:8080/api/health && echo
curl -s -o /dev/null -w 'report: %{http_code}\n' "localhost:8080/api/report?date=$(date +%F)"
```

Ожидается: оба сервиса `running`, `back_end` — `healthy`. Health отдаёт `{"status":"ok"}`. `/api/report` — `200` при живой сессии либо `503` при протухшей.

Откройте `http://localhost:8080` — должен открыться дашборд.

- [ ] **Step 7: Проверить, что часовые пояса в контейнере работают**

```bash
docker compose logs back_end | head -20
```

Ожидается: строка `слушаю addr=:8080` и **никакой** жалобы вида `unknown time zone`. Если такая жалоба есть — в образ не попал `tzdata`.

- [ ] **Step 8: Проверить логин внутри контейнера**

```bash
docker compose run --rm -it back_end /app/api-server -login
```

Ожидается: приглашение ввести код из Telegram. Прерывать можно по Ctrl+C — проверяем, что интерактивный режим доступен, а не проходим вход заново.

- [ ] **Step 9: Дописать README**

Добавьте в `README.md` раздел:

````markdown
## Дашборд

Веб-интерфейс по тем же данным: выбираешь дату — видишь, какие бэкапы
в каких средах прошли. Данные живые, из Telegram, а не из вчерашнего файла.

### Запуск

```sh
cp back_end/.env.example back_end/.env   # заполнить TELEGRAM_*
docker compose up -d
```

Дашборд — на http://localhost:8080

### Вход в Telegram

Сессия нужна одна и на сервер, и на отчёты. Если `data/session.json` ещё нет
или он протух (дашборд отвечает «сервер не может войти в Telegram»):

```sh
docker compose run --rm -it back_end /app/api-server -login
```

На Linux файл сессии окажется под UID пользователя контейнера (10001).
Если после этого локальный `go run` перестанет читать сессию — поправьте
владельца: `sudo chown $(id -u) back_end/data/session.json`.

### Разработка без Docker

```sh
cd back_end && ./env.sh && go run ./cmd/api-server   # бэк на :8080
cd front_end && npm run dev                       # фронт на :5173
```

Vite проксирует `/api` на бэк, поэтому CORS не нужен и в деве.

### Два бинаря

| Бинарь | Что делает | Нужен ли Google |
|---|---|---|
| `cmd/report` | отчёт в Google Sheets, cron, `-daemon`, `-login` | да |
| `cmd/api-server` | HTTP-API дашборда | нет |

Сервер не импортирует `internal/gsheets` — в его образе нет ни кода Google API,
ни OAuth-токена.
````

- [ ] **Step 10: Прогнать всё и закоммитить**

```bash
cd back_end && go test ./... && go vet ./...
cd ../front_end && npm test && npx tsc --noEmit
cd .. && git add -A
git commit -m "feat: docker-compose для бэка и фронта

Образы в общей папке docker/, наружу торчит только nginx с фронтом.
В рантайме alpine с tzdata: без базы поясов time.LoadLocation падает
в рантайме, а не на сборке. secrets/ бэку не монтируется — Google
нужен только cmd/report."
```

---

## Итог

После Task 10 работает: `docker compose up` поднимает дашборд на `http://localhost:8080`, отчёты в Google Sheets продолжают строиться `cmd/report` как раньше, `go test ./...` и `npm test` зелёные.
