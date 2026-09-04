package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"google.folder_id",
		"google.token_path",
		"google.retention_days",
		"labels",
		"schedule.timezone",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("в тексте ошибки нет %q; получено:\n%s", want, msg)
		}
	}
}

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
		"TELEGRAM_API_ID", "TELEGRAM_API_HASH", "TELEGRAM_PHONE", "GOOGLE_OAUTH_CLIENT",
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
