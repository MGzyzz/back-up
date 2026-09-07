package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodYAML = `
telegram:
  channel_id: -1001234567890
  session_path: ./data/session.json
schedule:
  report_at: "11:00"
  timezone: Asia/Almaty
google:
  folder_id: "folder-123"
  token_path: ./data/google_token.json
  retention_days: 30
labels:
  MINIO_BACKUPS: MinIO
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("записать конфиг: %v", err)
	}
	return p
}

// setSecrets кладёт в окружение всё, что конфиг обязан оттуда достать.
func setSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("TELEGRAM_API_ID", "1234567")
	t.Setenv("TELEGRAM_API_HASH", "hash-abc")
	t.Setenv("TELEGRAM_PHONE", "+77001234567")
	t.Setenv("GOOGLE_OAUTH_CLIENT", "/tmp/client_secret.json")
}

func TestLoadConfigOK(t *testing.T) {
	setSecrets(t)

	cfg, err := LoadConfig(writeConfig(t, goodYAML))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Telegram.ChannelID != -1001234567890 {
		t.Errorf("ChannelID = %d", cfg.Telegram.ChannelID)
	}
	if cfg.Telegram.SessionPath != "./data/session.json" {
		t.Errorf("SessionPath = %q", cfg.Telegram.SessionPath)
	}
	if cfg.Google.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d", cfg.Google.RetentionDays)
	}
	if cfg.Google.TokenPath != "./data/google_token.json" {
		t.Errorf("TokenPath = %q", cfg.Google.TokenPath)
	}
	if cfg.Labels["MINIO_BACKUPS"] != "MinIO" {
		t.Errorf("Labels = %v", cfg.Labels)
	}
	if cfg.Location() == nil {
		t.Fatal("Location() = nil, ожидал загруженную зону")
	}
	if cfg.Location().String() != "Asia/Almaty" {
		t.Errorf("Location() = %v, ожидал Asia/Almaty", cfg.Location())
	}
	if cfg.Telegram.APIID != 1234567 {
		t.Errorf("APIID не подтянулся из окружения: %d", cfg.Telegram.APIID)
	}
	if cfg.Telegram.APIHash != "hash-abc" {
		t.Errorf("APIHash не подтянулся из окружения: %q", cfg.Telegram.APIHash)
	}
}

// Секреты в YAML не читаются никогда, даже если кто-то их туда впишет.
func TestSecretsAreNeverReadFromYAML(t *testing.T) {
	setSecrets(t)

	withSecrets := goodYAML + "  api_hash: hash-из-файла\n"
	cfg, err := LoadConfig(writeConfig(t, withSecrets))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Telegram.APIHash != "hash-abc" {
		t.Errorf("APIHash = %q, ожидал значение из окружения", cfg.Telegram.APIHash)
	}
}

// Главный тест файла: пропущенные поля превращаются в нули, и Validate обязан их поймать.
// Google сюда не входит: его проверяет RequireGoogle, см. TestRequireGoogleReportsAllMissingFields.
func TestValidateCatchesZeroValues(t *testing.T) {
	setSecrets(t)

	almostEmpty := "schedule:\n  report_at: \"11:00\"\n"
	_, err := LoadConfig(writeConfig(t, almostEmpty))
	if err == nil {
		t.Fatal("LoadConfig вернул nil на конфиге без обязательных полей")
	}
	msg := err.Error()
	for _, want := range []string{
		"telegram.channel_id",
		"telegram.session_path",
		"labels",
		"schedule.timezone",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("в тексте ошибки нет %q; получено:\n%s", want, msg)
		}
	}
}

// GOOGLE_OAUTH_CLIENT сюда не входит: его проверяет RequireGoogle,
// см. TestRequireGoogleReportsAllMissingFields.
func TestValidateRequiresSecretsFromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_API_ID", "")
	t.Setenv("TELEGRAM_API_HASH", "")
	t.Setenv("TELEGRAM_PHONE", "")
	t.Setenv("GOOGLE_OAUTH_CLIENT", "")

	_, err := LoadConfig(writeConfig(t, goodYAML))
	if err == nil {
		t.Fatal("LoadConfig вернул nil при пустых секретах")
	}
	for _, want := range []string{
		"TELEGRAM_API_ID", "TELEGRAM_API_HASH", "TELEGRAM_PHONE",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в тексте ошибки нет %q; получено:\n%s", want, err.Error())
		}
	}
}

func TestValidateRejectsNonNumericAPIID(t *testing.T) {
	setSecrets(t)
	t.Setenv("TELEGRAM_API_ID", "не-число")

	_, err := LoadConfig(writeConfig(t, goodYAML))
	if err == nil {
		t.Fatal("LoadConfig принял нечисловой TELEGRAM_API_ID")
	}
	if !strings.Contains(err.Error(), "не число") {
		t.Errorf("ожидал жалобу на нечисловой id; получено:\n%s", err.Error())
	}
}

func TestValidateRejectsBadTimezone(t *testing.T) {
	setSecrets(t)

	bad := strings.Replace(goodYAML, "Asia/Almaty", "Mars/Olympus", 1)
	if _, err := LoadConfig(writeConfig(t, bad)); err == nil {
		t.Fatal("LoadConfig принял несуществующую таймзону")
	}
}

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

func TestLoadConfigWithoutGoogleSucceeds(t *testing.T) {
	setSecrets(t)
	os.Unsetenv("GOOGLE_OAUTH_CLIENT") // t.Setenv из setSecrets вернёт значение после теста

	if _, err := LoadConfig(writeConfig(t, noGoogleYAML)); err != nil {
		t.Fatalf("LoadConfig отверг конфиг без google: %v\nвеб-сервер обязан на нём стартовать", err)
	}
}

func TestRequireGoogleReportsAllMissingFields(t *testing.T) {
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

func TestRequireGoogleSucceedsWithCompleteConfig(t *testing.T) {
	setSecrets(t)

	cfg, err := LoadConfig(writeConfig(t, goodYAML))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := cfg.RequireGoogle(); err != nil {
		t.Fatalf("RequireGoogle ругнулся на полный конфиг: %v", err)
	}
}

func TestServerDefaults(t *testing.T) {
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

func TestServerConfigOverridesDefaults(t *testing.T) {
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
		t.Errorf("CacheTTLPast = %v, ожидался 2h", cfg.Server.CacheTTLPast)
	}
}
