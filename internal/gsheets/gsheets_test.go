package gsheets

import (
	"reflect"
	"testing"
)

// Значения приходят текстом из чужого канала, а записываются с USER_ENTERED.
// Всё, что Sheets принял бы за формулу, обязано остаться текстом.
func TestEscapeFormulas(t *testing.T) {
	in := [][]any{
		{"Environment", "Node", "Start"},
		{"KT", "kt-minio01", "2025-12-15 01:00:01"},
		{`=IMPORTXML("http://evil/","//x")`, "+7", "@here"},
		{"-PROD", 42, ""},
	}

	got := escapeFormulas(in)

	want := [][]any{
		{"Environment", "Node", "Start"},
		{"KT", "kt-minio01", "2025-12-15 01:00:01"},
		{`'=IMPORTXML("http://evil/","//x")`, "'+7", "'@here"},
		{"'-PROD", 42, ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("escapeFormulas():\nполучил %v\nожидал  %v", got, want)
	}

	// Аргумент менять нельзя: он же уходит в подсчёт строк и в логи.
	if in[2][0] != `=IMPORTXML("http://evil/","//x")` {
		t.Errorf("escapeFormulas изменила аргумент: %v", in[2][0])
	}
}

func TestQuote(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"1a2b3c", "'1a2b3c'"},
		{"с'кавычкой", `'с\'кавычкой'`},
		{`со\слешем`, `'со\\слешем'`},
	}
	for _, tt := range tests {
		if got := quote(tt.in); got != tt.want {
			t.Errorf("quote(%q) = %s, ожидал %s", tt.in, got, tt.want)
		}
	}
}

func TestColumnLetter(t *testing.T) {
	tests := map[int]string{0: "A", 2: "C", 6: "G", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"}
	for i, want := range tests {
		if got := columnLetter(i); got != want {
			t.Errorf("columnLetter(%d) = %q, ожидал %q", i, got, want)
		}
	}
}

func TestSheetRange(t *testing.T) {
	if got, want := sheetRange("Сводка"), "'Сводка'!A1"; got != want {
		t.Errorf("sheetRange = %q, ожидал %q", got, want)
	}
	// Апостроф в имени листа обязан удваиваться, иначе диапазон не разберётся.
	if got, want := sheetRange("Ad'hoc"), "'Ad''hoc'!A1"; got != want {
		t.Errorf("sheetRange = %q, ожидал %q", got, want)
	}
}

// Формула правила покраски строится вокруг колонки статуса и должна
// закреплять колонку ($C), но не строку — правило считается построчно.
func TestSheetFormatBuildsStatusFormula(t *testing.T) {
	tab := Sheet{
		Title:     "Сводка",
		Rows:      [][]any{{"a", "b", "Status"}, {"1", "2", "OK"}},
		StatusCol: 2,
		Colors:    []ColorRule{{Value: "OK", Color: RGB{G: 1}}},
	}

	var formulas []string
	for _, r := range sheetFormat(7, tab) {
		if r.AddConditionalFormatRule == nil {
			continue
		}
		formulas = append(formulas, r.AddConditionalFormatRule.Rule.BooleanRule.Condition.Values[0].UserEnteredValue)
	}

	if len(formulas) != 1 || formulas[0] != `=$C2="OK"` {
		t.Errorf("формулы %q, ожидал одну =$C2=\"OK\"", formulas)
	}
}
