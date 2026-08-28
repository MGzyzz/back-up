// Package telegram читает историю канала через MTProto от имени юзер-аккаунта.
//
// Пакет отвечает только за транспорт: авторизация, поиск канала, выгрузка
// сообщений за сутки. Разбор текста живёт в пакете parser.
package telegram

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"backup-report/internal/parser"
)

// ErrNoSession означает, что сохранённой сессии нет или она протухла.
// Под cron спрашивать код в stdin некому, поэтому это ошибка, а не приглашение ко входу.
var ErrNoSession = errors.New("нет сессии Telegram: запусти с флагом -login")

// pageSize — сколько сообщений просить за один запрос истории.
const pageSize = 100

type Client struct {
	apiID       int
	apiHash     string
	phone       string
	sessionPath string
	channelID   int64
	loc         *time.Location
}

func New(apiID int, apiHash, phone, sessionPath string, channelID int64, loc *time.Location) *Client {
	return &Client{
		apiID:       apiID,
		apiHash:     apiHash,
		phone:       phone,
		sessionPath: sessionPath,
		channelID:   NormalizeChannelID(channelID),
		loc:         loc,
	}
}

// NormalizeChannelID приводит ID канала к виду MTProto.
//
// В Bot API тот же канал выглядит как -1001234567890, в MTProto — как 1234567890.
// Конфиг может содержать любой из двух
func NormalizeChannelID(id int64) int64 {
	if id >= 0 {
		return id
	}
	digits := strconv.FormatInt(-id, 10)
	if rest, ok := strings.CutPrefix(digits, "100"); ok {
		if v, err := strconv.ParseInt(rest, 10, 64); err == nil {
			return v
		}
	}
	return -id
}

// dayBounds возвращает полуинтервал [начало суток, начало следующих) в зоне loc.
func dayBounds(day time.Time, loc *time.Location) (from, to time.Time) {
	d := day.In(loc)
	from = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	return from, from.AddDate(0, 0, 1)
}

// run поднимает соединение и отдаёт управление fn. Всё общение с Telegram
// обязано происходить внутри Run: снаружи соединения ещё/уже нет.
func (c *Client) run(ctx context.Context, fn func(ctx context.Context, tgc *telegram.Client) error) error {
	if err := os.MkdirAll(filepath.Dir(c.sessionPath), 0o700); err != nil {
		return fmt.Errorf("создать каталог сессии: %w", err)
	}
	tgc := telegram.NewClient(c.apiID, c.apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.sessionPath},
		NoUpdates:      true, // читаем историю, эфир не слушаем
	})
	return tgc.Run(ctx, func(ctx context.Context) error { return fn(ctx, tgc) })
}

// requireSession проверяет, что сохранённая сессия действительна.
// Под cron спросить код некому, поэтому это проверка, а не вход.
func requireSession(ctx context.Context, tgc *telegram.Client) error {
	status, err := tgc.Auth().Status(ctx)
	if err != nil {
		return fmt.Errorf("статус авторизации: %w", err)
	}
	if !status.Authorized {
		return ErrNoSession
	}
	return nil
}

// Login проводит интерактивный вход и сохраняет сессию. Запускается руками,
// один раз: спрашивает код из Telegram и, если включена, двухфакторный пароль.
func (c *Client) Login(ctx context.Context) error {
	return c.run(ctx, func(ctx context.Context, tgc *telegram.Client) error {
		switch err := requireSession(ctx, tgc); {
		case err == nil:
			fmt.Println("сессия уже действительна, вход не требуется")
			return nil
		case !errors.Is(err, ErrNoSession):
			return err // сеть или протокол — входить бессмысленно
		}

		code := auth.CodeAuthenticatorFunc(func(context.Context, *tg.AuthSentCode) (string, error) {
			fmt.Print("код из Telegram: ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			return strings.TrimSpace(line), err
		})
		flow := auth.NewFlow(
			auth.Constant(c.phone, os.Getenv("TELEGRAM_2FA_PASSWORD"), code),
			auth.SendCodeOptions{},
		)
		if err := tgc.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("вход: %w", err)
		}
		fmt.Println("вход выполнен, сессия сохранена в", c.sessionPath)
		return nil
	})
}

// FetchDay выгружает сообщения канала за указанные сутки.
func (c *Client) FetchDay(ctx context.Context, day time.Time) ([]parser.RawMessage, error) {
	from, to := dayBounds(day, c.loc)

	var out []parser.RawMessage
	err := c.run(ctx, func(ctx context.Context, tgc *telegram.Client) error {
		if err := requireSession(ctx, tgc); err != nil {
			return err
		}
		api := tgc.API()
		peer, err := c.resolvePeer(ctx, api)
		if err != nil {
			return err
		}
		out, err = fetchRange(ctx, api, peer, from, to)
		return err
	})
	return out, err
}

// Channel — канал аккаунта в том виде, в каком его показывает -channels.
type Channel struct {
	ID    int64
	Title string
}

// ListChannels возвращает каналы аккаунта. Нужна на настройке: ID канала
// неоткуда взять, а без него сервис не знает, что читать.
func (c *Client) ListChannels(ctx context.Context) ([]Channel, error) {
	var out []Channel
	err := c.run(ctx, func(ctx context.Context, tgc *telegram.Client) error {
		if err := requireSession(ctx, tgc); err != nil {
			return err
		}
		chats, err := dialogChats(ctx, tgc.API())
		if err != nil {
			return err
		}
		for _, chat := range chats {
			if ch, ok := chat.(*tg.Channel); ok && !ch.Megagroup {
				out = append(out, Channel{ID: ch.ID, Title: ch.Title})
			}
		}
		return nil
	})
	return out, err
}

// dialogChats снимает список чатов аккаунта — общий шаг для поиска канала
// по ID и для показа списка на настройке.
func dialogChats(ctx context.Context, api *tg.Client) ([]tg.ChatClass, error) {
	res, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      200,
	})
	if err != nil {
		return nil, fmt.Errorf("получить список диалогов: %w", err)
	}
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		return d.Chats, nil
	case *tg.MessagesDialogsSlice:
		return d.Chats, nil
	default:
		return nil, fmt.Errorf("неожиданный ответ на getDialogs: %T", res)
	}
}

// resolvePeer ищет канал среди диалогов аккаунта.
//
// Обращение к каналу требует не только ID, но и access_hash, которого в конфиге
// нет и быть не может: он свой у каждого аккаунта. Список диалогов — способ
// его получить, работающий и для приватного канала без username.
func (c *Client) resolvePeer(ctx context.Context, api *tg.Client) (tg.InputPeerClass, error) {
	chats, err := dialogChats(ctx, api)
	if err != nil {
		return nil, err
	}

	for _, chat := range chats {
		ch, ok := chat.(*tg.Channel)
		if !ok || ch.ID != c.channelID {
			continue
		}
		return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, nil
	}
	return nil, fmt.Errorf("канал %d не найден среди диалогов аккаунта", c.channelID)
}

// fetchRange листает историю назад от to, пока не дойдёт до from.
//
// История отдаётся страницами по убыванию даты, поэтому идём от конца суток
// к началу и останавливаемся, как только сообщения стали старше from.
func fetchRange(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, from, to time.Time) ([]parser.RawMessage, error) {
	var out []parser.RawMessage
	offsetID := 0

	for {
		req := &tg.MessagesGetHistoryRequest{
			Peer:     peer,
			Limit:    pageSize,
			OffsetID: offsetID,
		}
		if offsetID == 0 {
			// Первая страница: просим всё, что старше конца суток.
			req.OffsetDate = int(to.Unix())
		}

		res, err := api.MessagesGetHistory(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("получить историю: %w", err)
		}

		msgs, err := messagesOf(res)
		if err != nil {
			return nil, err
		}
		if len(msgs) == 0 {
			return out, nil // история кончилась
		}

		for _, m := range msgs {
			msg, ok := m.(*tg.Message)
			if !ok {
				continue // служебное сообщение
			}
			offsetID = msg.ID
			at := time.Unix(int64(msg.Date), 0)
			if at.Before(from) {
				return out, nil // ушли за начало суток
			}
			if !at.Before(to) || msg.Message == "" {
				continue // ещё не вошли в сутки, либо медиа без подписи
			}
			out = append(out, parser.RawMessage{Date: at, Text: msg.Message})
		}
	}
}

// messagesOf достаёт срез сообщений из любого из вариантов ответа.
func messagesOf(res tg.MessagesMessagesClass) ([]tg.MessageClass, error) {
	switch m := res.(type) {
	case *tg.MessagesChannelMessages:
		return m.Messages, nil
	case *tg.MessagesMessages:
		return m.Messages, nil
	case *tg.MessagesMessagesSlice:
		return m.Messages, nil
	default:
		return nil, fmt.Errorf("неожиданный ответ на getHistory: %T", res)
	}
}
