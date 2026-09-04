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
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"backup-report/internal/dates"
	"backup-report/internal/parser"
)

// ErrNoSession означает, что сохранённой сессии нет или она протухла.
// Под cron спрашивать код в stdin некому, поэтому это ошибка, а не приглашение ко входу.
var ErrNoSession = errors.New("нет сессии Telegram: запусти с флагом -login")

const (
	// pageSize — сколько сообщений просить за один запрос истории.
	pageSize = 100
	// dialogPageSize — сколько диалогов просить за один запрос списка.
	dialogPageSize = 100
)

// errStopDialogs останавливает обход диалогов, когда нужное уже найдено.
var errStopDialogs = errors.New("обход диалогов остановлен")

// Config — всё, что нужно клиенту для работы.
type Config struct {
	APIID       int
	APIHash     string
	Phone       string
	SessionPath string
	ChannelID   int64
	Location    *time.Location
}

type Client struct {
	cfg       Config
	channelID int64 // ID, приведённый к виду MTProto
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg, channelID: NormalizeChannelID(cfg.ChannelID)}
}

// NormalizeChannelID приводит ID канала к виду MTProto.
//
// В Bot API тот же канал выглядит как -1001234567890, в MTProto — как
// 1234567890. Конфиг принимает любой из двух видов.
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
	from = dates.StartOfDay(day, loc)
	return from, from.AddDate(0, 0, 1)
}

// run поднимает соединение и отдаёт управление fn. Всё общение с Telegram
// обязано происходить внутри Run: снаружи соединения ещё/уже нет.
func (c *Client) run(ctx context.Context, fn func(ctx context.Context, tgc *telegram.Client) error) error {
	if err := os.MkdirAll(filepath.Dir(c.cfg.SessionPath), 0o700); err != nil {
		return fmt.Errorf("создать каталог сессии: %w", err)
	}
	tgc := telegram.NewClient(c.cfg.APIID, c.cfg.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: c.cfg.SessionPath},
		NoUpdates:      true, // читаем историю, эфир не слушаем
	})
	return tgc.Run(ctx, func(ctx context.Context) error { return fn(ctx, tgc) })
}

// runAuthed — то же, что run, но перед вызовом fn проверяет сессию.
// Так работает всё, кроме самого входа.
func (c *Client) runAuthed(ctx context.Context, fn func(ctx context.Context, tgc *telegram.Client) error) error {
	return c.run(ctx, func(ctx context.Context, tgc *telegram.Client) error {
		if err := requireSession(ctx, tgc); err != nil {
			return err
		}
		return fn(ctx, tgc)
	})
}

// requireSession проверяет, что сохранённая сессия действительна.
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
			return err // сеть или протокол
		}

		// Ридер один на весь вход: Telegram может спросить код повторно,
		// а новый bufio.Reader на каждый запрос уносил бы с собой всё, что
		// человек успел набрать в буфер, вместе с выброшенным ридером.
		stdin := bufio.NewReader(os.Stdin)
		code := auth.CodeAuthenticatorFunc(func(_ context.Context, sent *tg.AuthSentCode) (string, error) {
			// Способ доставки выбирает Telegram, а не мы: если на номере уже
			// есть активная сессия, код уходит в само приложение.
			fmt.Printf("код отправлен: %s\n", describeCodeType(sent))
			if hint := resendHint(sent); hint != "" {
				fmt.Println(hint)
			}
			fmt.Print("код: ")
			line, err := stdin.ReadString('\n')
			return strings.TrimSpace(line), err
		})
		flow := auth.NewFlow(
			auth.Constant(c.cfg.Phone, os.Getenv("TELEGRAM_2FA_PASSWORD"), code),
			auth.SendCodeOptions{},
		)
		if err := tgc.Auth().IfNecessary(ctx, flow); err != nil {
			// Антифлуд выглядит как обычная ошибка протокола, а чинится
			// только ожиданием. Без явного сообщения человек будет дёргать
			// -login по кругу и продлевать себе запрет.
			if d, ok := tgerr.AsFloodWait(err); ok {
				return fmt.Errorf("Telegram временно отказывает в коде (антифлуд): повтори через %s. "+
					"Так бывает, когда код запрашивали много раз подряд", d.Round(time.Second))
			}
			return fmt.Errorf("вход: %w", err)
		}
		fmt.Println("вход выполнен, сессия сохранена в", c.cfg.SessionPath)
		return nil
	})
}

// describeCodeType переводит способ доставки кода в человеческую фразу.
func describeCodeType(sent *tg.AuthSentCode) string {
	if sent == nil {
		return "способ доставки неизвестен"
	}
	switch sent.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		return "Сообщение было отправлено ваш телеграмм чат. Проверьте его на наличие кода —\n" +
			"  Если код не пришел, то значит у вас активная сессия"
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeFirebaseSMS:
		return "по SMS"
	case *tg.AuthSentCodeTypeFragmentSMS:
		return "по SMS через Fragment"
	case *tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase:
		return "по SMS (слово или фраза, а не цифры)"
	case *tg.AuthSentCodeTypeCall:
		return "звонком — код продиктуют голосом"
	case *tg.AuthSentCodeTypeMissedCall, *tg.AuthSentCodeTypeFlashCall:
		return "сброшенным звонком — код это последние цифры номера, с которого звонят"
	case *tg.AuthSentCodeTypeEmailCode:
		return "на почту, привязанную к аккаунту"
	default:
		return fmt.Sprintf("способ %T — см. приложение Telegram", sent.Type)
	}
}

// resendHint пересказывает, что Telegram обещает, если код не дошёл: через
// сколько можно повторить запрос и каким способом придёт следующий.
func resendHint(sent *tg.AuthSentCode) string {
	if sent == nil {
		return ""
	}
	var parts []string
	if t, ok := sent.GetTimeout(); ok && t > 0 {
		parts = append(parts, fmt.Sprintf("повторный запрос возможен через %s",
			(time.Duration(t)*time.Second).String()))
	}
	if next, ok := sent.GetNextType(); ok {
		parts = append(parts, "следующий код придёт "+describeNextType(next))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  (" + strings.Join(parts, "; ") + ")"
}

// describeNextType — способ доставки следующей попытки.
func describeNextType(t tg.AuthCodeTypeClass) string {
	switch t.(type) {
	case *tg.AuthCodeTypeSMS:
		return "по SMS"
	case *tg.AuthCodeTypeFragmentSMS:
		return "по SMS через Fragment"
	case *tg.AuthCodeTypeCall:
		return "звонком"
	case *tg.AuthCodeTypeFlashCall, *tg.AuthCodeTypeMissedCall:
		return "сброшенным звонком"
	default:
		return fmt.Sprintf("способом %T", t)
	}
}

// FetchDay выгружает сообщения канала за указанные сутки.
func (c *Client) FetchDay(ctx context.Context, day time.Time) ([]parser.RawMessage, error) {
	from, to := dayBounds(day, c.cfg.Location)

	var out []parser.RawMessage
	err := c.runAuthed(ctx, func(ctx context.Context, tgc *telegram.Client) error {
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
	err := c.runAuthed(ctx, func(ctx context.Context, tgc *telegram.Client) error {
		return eachChannel(ctx, tgc.API(), func(ch *tg.Channel) bool {
			if !ch.Megagroup {
				out = append(out, Channel{ID: ch.ID, Title: ch.Title})
			}
			return false
		})
	})
	return out, err
}

// eachChannel обходит каналы аккаунта и зовёт fn на каждом; fn возвращает
// true, когда обход пора прекратить.
//
// Обход постраничный и по обеим папкам. Один запрос со срезом в 200 диалогов
// тут не годится: канал уведомлений обычно заглушен, легко уезжает в архив
// (это отдельная папка, в основном списке его тогда нет) и вытесняется вниз
// живой перепиской. Любого из этих случаев хватило бы, чтобы ежедневный
// отчёт перестал строиться с «канал не найден».
func eachChannel(ctx context.Context, api *tg.Client, fn func(ch *tg.Channel) bool) error {
	// 0 — основной список диалогов, 1 — архив.
	for _, folder := range []int{0, 1} {
		err := query.GetDialogs(api).
			BatchSize(dialogPageSize).
			FolderID(folder).
			ForEach(ctx, func(_ context.Context, e dialogs.Elem) error {
				p, ok := e.Peer.(*tg.InputPeerChannel)
				if !ok {
					return nil
				}
				// Канал берём из сущностей ответа: там же лежит access_hash,
				// без которого к каналу не обратиться.
				ch, ok := e.Entities.Channels()[p.ChannelID]
				if !ok {
					return nil
				}
				if fn(ch) {
					return errStopDialogs
				}
				return nil
			})
		switch {
		case errors.Is(err, errStopDialogs):
			return nil
		case err != nil:
			return fmt.Errorf("получить список диалогов (папка %d): %w", folder, err)
		}
	}
	return nil
}

// resolvePeer ищет канал среди диалогов аккаунта.
//
// Обращение к каналу требует не только ID, но и access_hash, которого в конфиге
// нет и быть не может: он свой у каждого аккаунта. Список диалогов — способ
// его получить, работающий и для приватного канала без username.
func (c *Client) resolvePeer(ctx context.Context, api *tg.Client) (tg.InputPeerClass, error) {
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
	return peer, nil
}

// fetchRange листает историю назад от to, пока не дойдёт до from.
//
// История отдаётся страницами по убыванию даты, поэтому идём от конца суток
// к началу и останавливаемся, как только сообщения стали старше from.
func fetchRange(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, from, to time.Time) ([]parser.RawMessage, error) {
	// Зона границ — зона из конфига: в ней же должно быть время сообщения,
	// иначе предупреждения в логе указывают на час, которого нет в отчёте.
	loc := from.Location()

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
			// Сдвигаем offset до разбора типа и без условий: если целая
			// страница окажется служебными сообщениями, а offset двигать
			// только на обычных, следующий запрос уйдёт тем же самым
			// и цикл станет бесконечным.
			offsetID = m.GetID()

			msg, ok := m.(*tg.Message)
			if !ok {
				continue // служебное сообщение
			}
			at := time.Unix(int64(msg.Date), 0).In(loc)
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
