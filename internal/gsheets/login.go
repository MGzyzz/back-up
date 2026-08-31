package gsheets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

// randomState — одноразовая метка запроса OAuth.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("сгенерировать state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login проводит вход через браузер и сохраняет токен. Запускается руками,
// один раз: обычный запуск браузер не открывает — под cron его некому закрыть.
func Login(ctx context.Context, clientSecretPath, tokenPath string) error {
	cfg, err := oauthConfig(clientSecretPath)
	if err != nil {
		return err
	}

	// Слушаем на произвольном порту localhost: для клиентов типа Desktop app
	// Google разрешает любой loopback-порт без предварительной регистрации.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("занять локальный порт: %w", err)
	}
	defer ln.Close()
	cfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	// state связывает наш запрос с ответом, который придёт на localhost.
	// Константа тут не годится: без сверки любой сайт, открытый в том же
	// браузере, может дёрнуть наш /callback со своим кодом.
	state, err := randomState()
	if err != nil {
		return err
	}

	codeCh := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "чужой state — запрос отклонён", http.StatusForbidden)
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "нет кода: "+q.Get("error"), http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Готово. Возвращайся в терминал.")
		codeCh <- code
	})}
	// Ошибка Serve здесь всегда ErrServerClosed из defer ниже: значимый отказ
	// вылезет таймаутом ожидания кода.
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	// AccessTypeOffline + ApprovalForce гарантируют выдачу refresh-токена:
	// без него сервис не сможет работать без браузера.
	url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Println("\nОткрой в браузере:\n\n" + url)
	fmt.Println("\n(приложение не проверено — «Дополнительные настройки» → «Перейти»)")

	var code string
	select {
	case code = <-codeCh:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Minute):
		return errors.New("не дождался подтверждения в браузере")
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("обменять код на токен: %w", err)
	}
	if tok.RefreshToken == "" {
		return errors.New("Google не выдал refresh-токен: сервис не сможет работать без браузера")
	}

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return fmt.Errorf("создать каталог токена: %w", err)
	}
	b, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	// 0600: токен даёт доступ к Google-аккаунту в рамках drive.file.
	if err := os.WriteFile(tokenPath, b, 0o600); err != nil {
		return fmt.Errorf("сохранить токен: %w", err)
	}
	fmt.Println("токен сохранён в", tokenPath)
	return nil
}
