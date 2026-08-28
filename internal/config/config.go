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

	loc *time.Location
}

// Location — часовой пояс из schedule.timezone. Заполняется в Validate.
func (c *Config) Location() *time.Location {
	return c.loc
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать конфиг: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("разобрать конфиг: %w", err)
	}

	// Ошибку разбора не теряем: нулевой APIID поймает Validate,
	var apiIDErr error
	if raw := os.Getenv("TELEGRAM_API_ID"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			apiIDErr = fmt.Errorf("TELEGRAM_API_ID %q не число", raw)
		}
		cfg.Telegram.APIID = id
	}
	cfg.Telegram.APIHash = os.Getenv("TELEGRAM_API_HASH")
	cfg.Telegram.Phone = os.Getenv("TELEGRAM_PHONE")
	cfg.Google.OAuthClientPath = os.Getenv("GOOGLE_OAUTH_CLIENT")

	if err := errors.Join(apiIDErr, cfg.Validate()); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate собирает ВСЕ проблемы разом
func (c *Config) Validate() error {
	var errs []error

	if c.Telegram.APIID == 0 {
		errs = append(errs, errors.New("переменная окружения TELEGRAM_API_ID не задана"))
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

	// Разбор в часы/минуты живёт в пакете report; здесь достаточно проверить формат.
	if _, err := time.Parse("15:04", c.Schedule.ReportAt); err != nil {
		errs = append(errs, fmt.Errorf("schedule.report_at %q: %w", c.Schedule.ReportAt, err))
	}

	if c.Schedule.Timezone == "" {
		errs = append(errs, errors.New("schedule.timezone обязателен"))
	} else {
		loc, err := time.LoadLocation(c.Schedule.Timezone)
		if err != nil {
			errs = append(errs, fmt.Errorf("schedule.timezone: %w", err))
		} else {
			c.loc = loc
		}
	}

	return errors.Join(errs...)
}
