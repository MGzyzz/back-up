// Package config читает YAML-конфиг, подставляет секреты из окружения
// и проверяет, что заполнено всё обязательное.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Config — параметры сервиса. Поля с тегом `yaml:"-"` в файл не пишутся
// никогда: это секреты, они приходят только из окружения.
type Config struct {
	Telegram struct {
		APIID       int    `yaml:"-"` // TELEGRAM_API_ID
		APIHash     string `yaml:"-"` // TELEGRAM_API_HASH
		Phone       string `yaml:"-"` // TELEGRAM_PHONE
		ChannelID   int64  `yaml:"channel_id"`
		SessionPath string `yaml:"session_path"`
	} `yaml:"telegram"`
	Schedule struct {
		ReportAt string `yaml:"report_at"`
		Timezone string `yaml:"timezone"`
	} `yaml:"schedule"`
	Google struct {
		OAuthClientPath string `yaml:"-"` // GOOGLE_OAUTH_CLIENT
		FolderID        string `yaml:"folder_id"`
		TokenPath       string `yaml:"token_path"`
		RetentionDays   int    `yaml:"retention_days"`
	} `yaml:"google"`
	Labels map[string]string `yaml:"labels"`

	rawAPIID string // TELEGRAM_API_ID как пришёл из окружения, до разбора

	// Производные значения: их заполняет prepare, а его зовёт только
	// LoadConfig — поэтому Location() никогда не вернёт nil.
	loc      *time.Location
	reportHH int
	reportMM int
}

// Location — часовой пояс из schedule.timezone.
func (c *Config) Location() *time.Location { return c.loc }

// ReportTime — schedule.report_at, разобранное на часы и минуты.
func (c *Config) ReportTime() (hh, mm int) { return c.reportHH, c.reportMM }

// LoadConfig читает файл, добирает секреты из окружения и проверяет,
// что заполнено всё обязательное.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать конфиг: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разобрать конфиг: %w", err)
	}

	cfg.readSecrets()
	if err := cfg.prepare(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// readSecrets забирает секреты из окружения. Разбор TELEGRAM_API_ID отложен
// до prepare: там про него ровно одна жалоба вместо двух противоречивых.
func (c *Config) readSecrets() {
	c.rawAPIID = os.Getenv("TELEGRAM_API_ID")
	c.Telegram.APIHash = os.Getenv("TELEGRAM_API_HASH")
	c.Telegram.Phone = os.Getenv("TELEGRAM_PHONE")
	c.Google.OAuthClientPath = os.Getenv("GOOGLE_OAUTH_CLIENT")
}

// prepare собирает ВСЕ проблемы разом и заполняет производные поля.
func (c *Config) prepare() error {
	var errs []error

	switch id, err := strconv.Atoi(c.rawAPIID); {
	case c.rawAPIID == "":
		errs = append(errs, errors.New("переменная окружения TELEGRAM_API_ID не задана"))
	case err != nil:
		errs = append(errs, fmt.Errorf("TELEGRAM_API_ID %q не число", c.rawAPIID))
	case id <= 0:
		errs = append(errs, fmt.Errorf("TELEGRAM_API_ID %d: ожидается положительное число", id))
	default:
		c.Telegram.APIID = id
	}

	if c.Telegram.APIHash == "" {
		errs = append(errs, errors.New("переменная окружения TELEGRAM_API_HASH не задана"))
	}
	if c.Telegram.Phone == "" {
		errs = append(errs, errors.New("переменная окружения TELEGRAM_PHONE не задана"))
	}
	if c.Telegram.ChannelID == 0 {
		errs = append(errs, errors.New("telegram.channel_id обязателен"))
	}
	if c.Telegram.SessionPath == "" {
		errs = append(errs, errors.New("telegram.session_path обязателен"))
	}

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

	if len(c.Labels) == 0 {
		errs = append(errs, errors.New("labels не может быть пустым"))
	}

	// Разбираем один раз: дальше время берут готовым через ReportTime.
	if t, err := time.Parse("15:04", c.Schedule.ReportAt); err != nil {
		errs = append(errs, fmt.Errorf("schedule.report_at %q: %w", c.Schedule.ReportAt, err))
	} else {
		c.reportHH, c.reportMM = t.Hour(), t.Minute()
	}

	if c.Schedule.Timezone == "" {
		errs = append(errs, errors.New("schedule.timezone обязателен"))
	} else if loc, err := time.LoadLocation(c.Schedule.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("schedule.timezone: %w", err))
	} else {
		c.loc = loc
	}

	return errors.Join(errs...)
}
