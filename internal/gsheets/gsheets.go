// Package gsheets публикует отчёты в Google Sheets и убирает старые.
//
// Авторизация — OAuth от живого пользователя со scope drive.file: приложение
// видит только созданные им самим файлы. Чужое оно не тронет физически,
// поэтому чистка безопасна по построению, а не по аккуратности кода.
package gsheets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// ErrNoToken означает, что OAuth-токена нет или он не читается.
var ErrNoToken = errors.New("нет токена Google: запусти с флагом -login")

const sheetMime = "application/vnd.google-apps.spreadsheet"

// File — то немногое, что нужно знать о файле в папке отчётов.
type File struct {
	ID   string
	Name string
}

type Client struct {
	folderID string
	drive    *drive.Service
	sheets   *sheets.Service
}

// Connect читает сохранённый токен и поднимает клиентов Drive и Sheets.
// Браузер не открывает: под cron это привело бы к зависанию.
func Connect(ctx context.Context, clientSecretPath, tokenPath, folderID string) (*Client, error) {
	cfg, err := oauthConfig(clientSecretPath)
	if err != nil {
		return nil, err
	}
	tok, err := readToken(tokenPath)
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
	return &Client{folderID: folderID, drive: driveSrv, sheets: sheetsSrv}, nil
}

// Publish создаёт лист с именем name в папке отчётов и заливает в него строки.
func (c *Client) Publish(ctx context.Context, name string, rows [][]any) (string, error) {
	file, err := c.drive.Files.Create(&drive.File{
		Name:     name,
		MimeType: sheetMime,
		Parents:  []string{c.folderID},
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("создать файл %q: %w", name, err)
	}

	// Диапазон без имени листа: при русской локали первый лист называется
	// "Лист1", и "Sheet1!A1" упал бы с Unable to parse range.
	_, err = c.sheets.Spreadsheets.Values.
		Update(file.Id, "A1", &sheets.ValueRange{Values: rows}).
		ValueInputOption("USER_ENTERED"). // чтобы даты стали датами, а не текстом
		Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("записать строки: %w", err)
	}

	// Оформление — украшение поверх готовых данных. Если Sheets его отверг,
	// отчёт всё равно опубликован, и терять его из-за жирного шрифта незачем.
	if err := c.format(ctx, file.Id, len(rows)); err != nil {
		slog.Warn("таблица опубликована, но не оформлена", "file_id", file.Id, "err", err)
	}
	return file.Id, nil
}

// format делает заголовок жирным, закрепляет первую строку, вешает на неё
// автофильтр и подсвечивает красным всё, что не SUCCESS.
func (c *Client) format(ctx context.Context, fileID string, rowCount int) error {
	ss, err := c.sheets.Spreadsheets.Get(fileID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("прочитать свойства листа: %w", err)
	}
	if len(ss.Sheets) == 0 {
		return errors.New("в созданной таблице нет ни одного листа")
	}
	sheetID := ss.Sheets[0].Properties.SheetId

	// Правило по "не SUCCESS", а не по перечислению FAILED/ERROR:
	// новый статус в канале не должен молча остаться без подсветки.
	// Диапазон ограничен строками с данными, поэтому проверять на пустоту не нужно.
	dataRange := &sheets.GridRange{
		SheetId: sheetID, StartRowIndex: 1, EndRowIndex: int64(rowCount),
		StartColumnIndex: 0, EndColumnIndex: 7,
	}

	// Фильтр, в отличие от подсветки, захватывает и строку заголовка:
	// именно она становится строкой с выпадающими списками.
	tableRange := &sheets.GridRange{
		SheetId: sheetID, StartRowIndex: 0, EndRowIndex: int64(rowCount),
		StartColumnIndex: 0, EndColumnIndex: 7,
	}

	_, err = c.sheets.Spreadsheets.BatchUpdate(fileID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
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
			// Отчёт читают с разными вопросами: «что упало ночью», «как там
			// Postgres», «все ли окружения закрылись». Вместо того чтобы
			// угадывать один порядок, отдаём сортировку читателю.
			{SetBasicFilter: &sheets.SetBasicFilterRequest{
				Filter: &sheets.BasicFilter{Range: tableRange},
			}},
			{AddConditionalFormatRule: &sheets.AddConditionalFormatRuleRequest{
				Rule: &sheets.ConditionalFormatRule{
					Ranges: []*sheets.GridRange{dataRange},
					BooleanRule: &sheets.BooleanRule{
						Condition: &sheets.BooleanCondition{
							Type:   "CUSTOM_FORMULA",
							Values: []*sheets.ConditionValue{{UserEnteredValue: `=$D2<>"SUCCESS"`}},
						},
						Format: &sheets.CellFormat{
							BackgroundColor: &sheets.Color{Red: 1, Green: 0.85, Blue: 0.85},
						},
					},
				},
			}},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("оформить таблицу: %w", err)
	}
	return nil
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
			Q(fmt.Sprintf("'%s' in parents and trashed = false", c.folderID)).
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
		return nil, fmt.Errorf("%w (%s)", ErrNoToken, path)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, fmt.Errorf("%w: файл повреждён (%s)", ErrNoToken, path)
	}
	return &tok, nil
}
