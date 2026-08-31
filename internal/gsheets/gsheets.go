// Package gsheets публикует отчёты в Google Sheets и убирает старые.
//
// Авторизация — OAuth от живого пользователя со scope drive.file: приложение
// видит только созданные им самим файлы, поэтому чистка не может задеть чужой.
package gsheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// ErrNoToken означает, что OAuth-токена нет или он не читается.
var ErrNoToken = errors.New("нет токена Google: запусти с флагом -login")

const sheetMime = "application/vnd.google-apps.spreadsheet"

// RGB — цвет заливки строки, компоненты от 0 до 1.
type RGB struct{ R, G, B float64 }

// ColorRule красит строку, если в колонке статуса стоит Value.
type ColorRule struct {
	Value string
	Color RGB
}

// Sheet — один лист публикуемой таблицы.
type Sheet struct {
	Title string
	Rows  [][]any
	// StatusCol — индекс колонки, по значению которой красятся строки.
	// Если Colors пуст, лист не красится и StatusCol не используется.
	StatusCol int
	Colors    []ColorRule
	// Widths — ширина колонок в пикселях. Ноль или отсутствие значения
	// означает «подогнать по содержимому».
	Widths []int
}

// File — то немногое, что нужно знать о файле в папке отчётов.
type File struct {
	ID   string
	Name string
}

// Config — параметры подключения. Структура, а не три строковых аргумента
// подряд: перепутанные местами пути компилятор бы не заметил.
type Config struct {
	ClientSecretPath string
	TokenPath        string
	FolderID         string
	Logger           *slog.Logger // nil = slog.Default()
}

type Client struct {
	folderID string
	log      *slog.Logger
	drive    *drive.Service
	sheets   *sheets.Service
}

// Connect читает сохранённый токен и поднимает клиентов Drive и Sheets.
// Браузер не открывает: под cron это привело бы к зависанию.
func Connect(ctx context.Context, c Config) (*Client, error) {
	cfg, err := oauthConfig(c.ClientSecretPath)
	if err != nil {
		return nil, err
	}
	tok, err := readToken(c.TokenPath)
	if err != nil {
		return nil, err
	}

	// TokenSource сам обновляет протухший access-токен по refresh-токену.
	httpClient := cfg.Client(ctx, tok)

	driveSrv, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("клиент Drive: %w", err)
	}
	sheetsSrv, err := sheets.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("клиент Sheets: %w", err)
	}
	log := c.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Client{folderID: c.FolderID, log: log, drive: driveSrv, sheets: sheetsSrv}, nil
}

// Publish создаёт таблицу с именем name в папке отчётов и заливает в неё листы.
//
// Создание файла и заливка данных — разные вызовы API, между ними возможен
// сбой. Пустой файл с правильным именем хуже, чем никакого: следующий запуск
// счёл бы отчёт за этот день готовым. Поэтому недописанный файл убирается.
func (c *Client) Publish(ctx context.Context, name string, tabs []Sheet) (id string, err error) {
	if len(tabs) == 0 {
		return "", errors.New("нечего публиковать: ни одного листа")
	}
	for _, t := range tabs {
		if len(t.Rows) == 0 {
			return "", fmt.Errorf("лист %q пуст: нет даже заголовка", t.Title)
		}
	}

	file, err := c.drive.Files.Create(&drive.File{
		Name:     name,
		MimeType: sheetMime,
		Parents:  []string{c.folderID},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("создать файл %q: %w", name, err)
	}
	defer func() {
		if err != nil {
			c.discard(ctx, file.Id, name)
		}
	}()

	if err = c.layout(ctx, file.Id, tabs); err != nil {
		return "", err
	}

	data := make([]*sheets.ValueRange, 0, len(tabs))
	for _, t := range tabs {
		data = append(data, &sheets.ValueRange{
			Range:  sheetRange(t.Title),
			Values: escapeFormulas(t.Rows),
		})
	}
	_, err = c.sheets.Spreadsheets.Values.BatchUpdate(file.Id, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "USER_ENTERED", // чтобы даты стали датами, а не текстом
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("записать строки: %w", err)
	}

	// Оформление — поверх готовых данных. Если Sheets его отверг, отчёт
	// всё равно опубликован: терять его из-за цвета заливки незачем.
	if ferr := c.format(ctx, file.Id, tabs); ferr != nil {
		c.log.Warn("таблица опубликована, но не оформлена", "file_id", file.Id, "err", ferr)
	}
	return file.Id, nil
}

// layout переименовывает лист, созданный по умолчанию, и добавляет остальные.
//
// Имя задаём сами, чтобы дальше адресовать диапазоны по нему: у нового файла
// первый лист называется "Sheet1" или "Лист1" — смотря какая локаль аккаунта.
func (c *Client) layout(ctx context.Context, fileID string, tabs []Sheet) error {
	ss, err := c.sheets.Spreadsheets.Get(fileID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("прочитать свойства таблицы: %w", err)
	}
	if len(ss.Sheets) == 0 {
		return errors.New("в созданной таблице нет ни одного листа")
	}

	reqs := []*sheets.Request{{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
		Properties: &sheets.SheetProperties{
			SheetId: ss.Sheets[0].Properties.SheetId,
			Title:   tabs[0].Title,
		},
		Fields: "title",
	}}}
	for _, t := range tabs[1:] {
		reqs = append(reqs, &sheets.Request{AddSheet: &sheets.AddSheetRequest{
			Properties: &sheets.SheetProperties{Title: t.Title},
		}})
	}

	if _, err := c.sheets.Spreadsheets.BatchUpdate(fileID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: reqs,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("создать листы: %w", err)
	}
	return nil
}

// discard убирает файл, который не удалось дописать. Ошибку только логируем,
// чтобы не подменять ею исходную причину сбоя.
//
// WithoutCancel: типичная причина попасть сюда — отмена контекста по SIGTERM,
// а убрать за собой надо и в этом случае.
func (c *Client) discard(ctx context.Context, fileID, name string) {
	if err := c.Trash(context.WithoutCancel(ctx), fileID); err != nil {
		c.log.Error("не убрал недописанный файл — удали его руками, иначе он заблокирует отчёт за этот день",
			"file", name, "file_id", fileID, "err", err)
		return
	}
	c.log.Warn("недописанный файл убран в корзину", "file", name, "file_id", fileID)
}

// escapeFormulas обезвреживает ячейки, которые Sheets принял бы за формулу.
//
// USER_ENTERED нужен ради дат: без него они легли бы строками. Но значения
// приходят из чужого канала, поэтому всё, что начинается с = + - @,
// получает апостроф и остаётся текстом.
func escapeFormulas(rows [][]any) [][]any {
	out := make([][]any, len(rows))
	for i, row := range rows {
		cells := make([]any, len(row))
		for j, cell := range row {
			s, ok := cell.(string)
			if ok && strings.ContainsAny(first(s), "=+-@") {
				cell = "'" + s
			}
			cells[j] = cell
		}
		out[i] = cells
	}
	return out
}

// first возвращает первый символ строки или пустую строку.
func first(s string) string {
	if s == "" {
		return ""
	}
	return s[:1]
}

// format оформляет каждый лист: жирный заголовок, закреплённая первая строка,
// автофильтр и заливка строк по значению в колонке статуса.
func (c *Client) format(ctx context.Context, fileID string, tabs []Sheet) error {
	ss, err := c.sheets.Spreadsheets.Get(fileID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("прочитать свойства листов: %w", err)
	}
	byTitle := make(map[string]int64, len(ss.Sheets))
	for _, sh := range ss.Sheets {
		byTitle[sh.Properties.Title] = sh.Properties.SheetId
	}

	var reqs []*sheets.Request
	for _, t := range tabs {
		sheetID, ok := byTitle[t.Title]
		if !ok {
			return fmt.Errorf("лист %q не найден в созданной таблице", t.Title)
		}
		reqs = append(reqs, sheetFormat(sheetID, t)...)
	}

	if _, err := c.sheets.Spreadsheets.BatchUpdate(fileID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: reqs,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("оформить таблицу: %w", err)
	}
	return nil
}

// sheetFormat собирает запросы оформления одного листа.
func sheetFormat(sheetID int64, t Sheet) []*sheets.Request {
	rowCount := int64(len(t.Rows))
	colCount := int64(len(t.Rows[0]))

	// Заливка идёт только по строкам с данными; фильтр, в отличие от неё,
	// захватывает и заголовок — именно он становится строкой с выпадашками.
	dataRange := gridRange(sheetID, 1, rowCount, colCount)  // без заголовка
	tableRange := gridRange(sheetID, 0, rowCount, colCount) // с заголовком

	reqs := []*sheets.Request{
		{RepeatCell: &sheets.RepeatCellRequest{
			Range:  &sheets.GridRange{SheetId: sheetID, StartRowIndex: 0, EndRowIndex: 1},
			Cell:   &sheets.CellData{UserEnteredFormat: &sheets.CellFormat{TextFormat: &sheets.TextFormat{Bold: true}}},
			Fields: "userEnteredFormat.textFormat.bold",
		}},
		{UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
			Properties: &sheets.SheetProperties{
				SheetId:        sheetID,
				GridProperties: &sheets.GridProperties{FrozenRowCount: 1},
			},
			Fields: "gridProperties.frozenRowCount",
		}},
		// Автофильтр: сортировку и отбор отдаём читателю.
		{SetBasicFilter: &sheets.SetBasicFilterRequest{
			Filter: &sheets.BasicFilter{Range: tableRange},
		}},
	}
	reqs = append(reqs, columnWidths(sheetID, colCount, t.Widths)...)

	// Правила заливки перечислены явно, а не выведены из «не успех»:
	// на кластере ERROR — штатная работа, и красным быть не должен.
	col := columnLetter(t.StatusCol)
	for _, rule := range t.Colors {
		reqs = append(reqs, &sheets.Request{
			AddConditionalFormatRule: &sheets.AddConditionalFormatRuleRequest{
				Rule: &sheets.ConditionalFormatRule{
					Ranges: []*sheets.GridRange{dataRange},
					BooleanRule: &sheets.BooleanRule{
						Condition: &sheets.BooleanCondition{
							Type: "CUSTOM_FORMULA",
							// Без функций: локаль листа разделяет аргументы
							// точкой с запятой, и формула с ними бы сломалась.
							Values: []*sheets.ConditionValue{
								{UserEnteredValue: fmt.Sprintf(`=$%s2=%q`, col, rule.Value)},
							},
						},
						Format: &sheets.CellFormat{
							BackgroundColor: &sheets.Color{
								Red: rule.Color.R, Green: rule.Color.G, Blue: rule.Color.B,
							},
						},
					},
				},
			},
		})
	}
	return reqs
}

// gridRange — прямоугольник листа от строки fromRow до rowCount
// по всем колонкам.
func gridRange(sheetID int64, fromRow, rowCount, colCount int64) *sheets.GridRange {
	return &sheets.GridRange{
		SheetId: sheetID, StartRowIndex: fromRow, EndRowIndex: rowCount,
		StartColumnIndex: 0, EndColumnIndex: colCount,
	}
}

// columnWidths расставляет ширины: заданные — явно, остальные — автоподгонкой.
//
// Автоподгонка идёт одним запросом на непрерывный диапазон, чтобы не плодить
// по запросу на колонку.
func columnWidths(sheetID int64, colCount int64, widths []int) []*sheets.Request {
	var reqs []*sheets.Request
	autoFrom := int64(-1)

	flushAuto := func(to int64) {
		if autoFrom < 0 {
			return
		}
		reqs = append(reqs, &sheets.Request{AutoResizeDimensions: &sheets.AutoResizeDimensionsRequest{
			Dimensions: &sheets.DimensionRange{
				SheetId: sheetID, Dimension: "COLUMNS",
				StartIndex: autoFrom, EndIndex: to,
			},
		}})
		autoFrom = -1
	}

	for i := int64(0); i < colCount; i++ {
		px := 0
		if int(i) < len(widths) {
			px = widths[i]
		}
		if px <= 0 {
			if autoFrom < 0 {
				autoFrom = i
			}
			continue
		}
		flushAuto(i)
		reqs = append(reqs, &sheets.Request{UpdateDimensionProperties: &sheets.UpdateDimensionPropertiesRequest{
			Range: &sheets.DimensionRange{
				SheetId: sheetID, Dimension: "COLUMNS",
				StartIndex: i, EndIndex: i + 1,
			},
			Properties: &sheets.DimensionProperties{PixelSize: int64(px)},
			Fields:     "pixelSize",
		}})
	}
	flushAuto(colCount)
	return reqs
}

// columnLetter переводит индекс колонки в её букву: 0 -> A, 26 -> AA.
func columnLetter(i int) string {
	var b []byte
	for {
		b = append([]byte{byte('A' + i%26)}, b...)
		i = i/26 - 1
		if i < 0 {
			return string(b)
		}
	}
}

// sheetRange адресует лист целиком, начиная с A1. Имя в апострофах:
// без них лист с пробелом в названии сломал бы разбор диапазона.
func sheetRange(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'!A1"
}

// List возвращает файлы папки отчётов. Из-за scope drive.file сюда попадает
// только созданное этим приложением — чужие файлы в папке невидимы.
func (c *Client) List(ctx context.Context) ([]File, error) {
	var out []File
	token := ""
	for {
		// Листинг постраничный: без цикла через год отчёты дальше первой
		// страницы перестали бы удаляться.
		res, err := c.drive.Files.List().
			Q(fmt.Sprintf("%s in parents and trashed = false", quote(c.folderID))).
			Fields("nextPageToken, files(id, name)").
			PageToken(token).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("листинг папки: %w", err)
		}
		for _, f := range res.Files {
			out = append(out, File{ID: f.Id, Name: f.Name})
		}
		if res.NextPageToken == "" {
			return out, nil
		}
		token = res.NextPageToken
	}
}

// Trash отправляет файл в корзину, а не удаляет насовсем: в Drive он пролежит
// там ещё 30 дней, и ошибка в folder_id останется исправимой.
func (c *Client) Trash(ctx context.Context, fileID string) error {
	_, err := c.drive.Files.Update(fileID, &drive.File{Trashed: true}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("отправить в корзину %s: %w", fileID, err)
	}
	return nil
}

// quote оформляет значение как строковый литерал запроса Drive. ID папки
// кавычек не содержит, но подставлять в чужой язык запросов без
// экранирования не стоит.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}

func oauthConfig(clientSecretPath string) (*oauth2.Config, error) {
	raw, err := os.ReadFile(clientSecretPath)
	if err != nil {
		return nil, fmt.Errorf("прочитать клиентский файл OAuth: %w", err)
	}
	// drive.file — доступ только к файлам приложения. Scope несенситивный,
	// поэтому приложение можно перевести в Production без верификации,
	// а без Production Google протухает refresh-токен через 7 дней.
	cfg, err := google.ConfigFromJSON(raw, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("разобрать клиентский файл OAuth: %w", err)
	}
	return cfg, nil
}

func readToken(path string) (*oauth2.Token, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		// Исходную ошибку сохраняем: «нет прав на файл» и «файла нет» чинятся
		// по-разному, а совет «запусти -login» помогает только во втором случае.
		return nil, fmt.Errorf("%w (%s): %w", ErrNoToken, path, err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, fmt.Errorf("%w: файл повреждён (%s): %w", ErrNoToken, path, err)
	}
	return &tok, nil
}
