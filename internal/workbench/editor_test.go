package workbench

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestEditor_preserves_inserted_SQL_and_enters_normal_mode_after_escape(t *testing.T) {
	// Given
	editor := newEditor()

	// When
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	// Then
	if editor.insert {
		t.Fatal("editor remained in insert mode")
	}
	if got := editor.textarea.Value(); got != "S" {
		t.Errorf("editor value = %q, want %q", got, "S")
	}
	if got := editor.textarea.Column(); got != 0 {
		t.Errorf("editor column = %d, want 0", got)
	}
}

func TestEditor_startsInNormalModeAndRequiresIToEdit(t *testing.T) {
	// Given
	editor := newEditor()

	// When
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})

	// Then
	if editor.insert {
		t.Fatal("editor started in insert mode")
	}
	if got := editor.textarea.Value(); got != "" {
		t.Errorf("editor value = %q, want empty before insert mode", got)
	}

	// When
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})

	// Then
	if got := editor.textarea.Value(); got != "S" {
		t.Errorf("editor value = %q, want S after entering insert mode", got)
	}
}

func TestEditor_normal_mode_motions(t *testing.T) {
	tests := []struct {
		name, value          string
		width                int
		line, column         int
		keys                 []tea.KeyPressMsg
		wantLine, wantColumn int
	}{
		{
			name:       "horizontal motions stay on a unicode line",
			value:      "a界b",
			line:       0,
			column:     1,
			keys:       []tea.KeyPressMsg{{Code: 'h'}, {Code: 'l'}, {Code: 'l'}},
			wantLine:   0,
			wantColumn: 2,
		},
		{
			name:       "line start and end use existing runes",
			value:      "alpha",
			line:       0,
			column:     2,
			keys:       []tea.KeyPressMsg{{Code: '0'}, {Code: '$'}},
			wantLine:   0,
			wantColumn: 4,
		},
		{
			name:       "vertical motion keeps preferred column across ragged lines",
			value:      "abcdef\nx\nuvwxyz",
			line:       0,
			column:     3,
			keys:       []tea.KeyPressMsg{{Code: 'j'}, {Code: 'j'}},
			wantLine:   2,
			wantColumn: 3,
		},
		{
			name:       "upward vertical motion keeps preferred column across ragged lines",
			value:      "abcdef\nx\nuvwxyz",
			line:       2,
			column:     3,
			keys:       []tea.KeyPressMsg{{Code: 'k'}, {Code: 'k'}},
			wantLine:   0,
			wantColumn: 3,
		},
		{
			name:       "vertical motion crosses visual wraps as one logical line",
			value:      "abcdef\nuvwxyz",
			width:      5,
			line:       0,
			column:     5,
			keys:       []tea.KeyPressMsg{{Code: 'j'}},
			wantLine:   1,
			wantColumn: 5,
		},
		{
			name:       "upward vertical motion crosses visual wraps as one logical line",
			value:      "abcdef\nuvwxyz",
			width:      5,
			line:       1,
			column:     5,
			keys:       []tea.KeyPressMsg{{Code: 'k'}},
			wantLine:   0,
			wantColumn: 5,
		},
		{
			name:       "end of line preference lands on destination ends",
			value:      "abcde\nx\nuvwxyz",
			line:       0,
			column:     0,
			keys:       []tea.KeyPressMsg{{Code: '$'}, {Code: 'j'}, {Code: 'j'}},
			wantLine:   2,
			wantColumn: 5,
		},
		{
			name:       "word motion crosses a newline as whitespace",
			value:      "one\n世界",
			line:       0,
			column:     0,
			keys:       []tea.KeyPressMsg{{Code: 'w'}},
			wantLine:   1,
			wantColumn: 0,
		},
		{
			name:       "go to first and final lines retain desired column",
			value:      "abcd\nxy\nwxyz",
			line:       1,
			column:     1,
			keys:       []tea.KeyPressMsg{{Code: 'G'}, {Code: 'g'}, {Code: 'g'}},
			wantLine:   0,
			wantColumn: 1,
		},
		{
			name:       "word motions classify unicode words punctuation and whitespace",
			value:      "alpha,  世界! z",
			line:       0,
			column:     0,
			keys:       []tea.KeyPressMsg{{Code: 'w'}, {Code: 'w'}, {Code: 'w'}, {Code: 'b'}, {Code: 'b'}},
			wantLine:   0,
			wantColumn: 5,
		},
		{
			name:       "motions are bounded on empty input",
			value:      "",
			line:       0,
			column:     0,
			keys:       []tea.KeyPressMsg{{Code: 'h'}, {Code: 'l'}, {Code: 'j'}, {Code: 'k'}, {Code: 'w'}, {Code: 'b'}, {Code: '0'}, {Code: '$'}},
			wantLine:   0,
			wantColumn: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			editor := newEditor()
			editor.textarea.SetValue(test.value)
			if test.width > 0 {
				editor.textarea.SetWidth(test.width)
			}
			editor.textarea.MoveToBegin()
			for editor.textarea.Line() < test.line {
				editor.moveLogicalLine(true)
			}
			editor.textarea.SetCursorColumn(test.column)
			editor.insert = false
			editor.desiredColumn = test.column

			// When
			for _, key := range test.keys {
				editor, _ = editor.Update(key)
			}

			// Then
			if got := editor.textarea.Line(); got != test.wantLine {
				t.Errorf("editor line = %d, want %d", got, test.wantLine)
			}
			if got := editor.textarea.Column(); got != test.wantColumn {
				t.Errorf("editor column = %d, want %d", got, test.wantColumn)
			}
			if got := editor.textarea.Value(); got != test.value {
				t.Errorf("editor value = %q, want %q", got, test.value)
			}
		})
	}
}

func TestEditor_insert_and_normal_boundaries(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		column      int
		keys        []tea.KeyPressMsg
		startInsert bool
		wantValue   string
		wantColumn  int
		wantInsert  bool
	}{
		{
			name:       "insert before current rune then escape",
			value:      "ac",
			column:     1,
			keys:       []tea.KeyPressMsg{{Code: 'i'}, {Code: 'b', Text: "b"}, {Code: tea.KeyEscape}},
			wantValue:  "abc",
			wantColumn: 1,
		},
		{
			name:       "append after current rune then escape",
			value:      "ac",
			column:     0,
			keys:       []tea.KeyPressMsg{{Code: 'a'}, {Code: 'b', Text: "b"}, {Code: tea.KeyEscape}},
			wantValue:  "abc",
			wantColumn: 1,
		},
		{
			name:        "control e remains textarea line end in insert mode",
			value:       "abc",
			column:      0,
			keys:        []tea.KeyPressMsg{{Code: 'e', Mod: tea.ModCtrl}},
			startInsert: true,
			wantValue:   "abc",
			wantColumn:  3,
			wantInsert:  true,
		},
		{
			name:       "normal escape unmatched g and unsupported keys are harmless",
			value:      "abc",
			column:     1,
			keys:       []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Code: 'g'}, {Code: 'd'}},
			wantValue:  "abc",
			wantColumn: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			editor := newEditor()
			editor.textarea.SetValue(test.value)
			editor.textarea.SetCursorColumn(test.column)
			editor.insert = test.startInsert
			if !editor.insert {
				editor.desiredColumn = test.column
			}

			// When
			for _, key := range test.keys {
				editor, _ = editor.Update(key)
			}

			// Then
			if got := editor.textarea.Value(); got != test.wantValue {
				t.Errorf("editor value = %q, want %q", got, test.wantValue)
			}
			if got := editor.textarea.Column(); got != test.wantColumn {
				t.Errorf("editor column = %d, want %d", got, test.wantColumn)
			}
			if editor.insert != test.wantInsert {
				t.Errorf("editor insert mode = %t, want %t", editor.insert, test.wantInsert)
			}
		})
	}
}

func TestEditor_manual_key_sequence(t *testing.T) {
	// Given
	editor := newEditor()
	editor.textarea.SetValue("go,\n世界 z\nq")
	editor.textarea.MoveToBegin()

	// When
	editor, _ = editor.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'w'})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'j'})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: 'i'})
	editor, _ = editor.Update(tea.KeyPressMsg{Code: '!', Text: "!"})

	// Then
	if got := editor.textarea.Value(); got != "go,\n世界! z\nq" {
		t.Errorf("editor value = %q, want %q", got, "go,\n世界! z\nq")
	}
	if got := editor.textarea.Line(); got != 1 {
		t.Errorf("editor line = %d, want 1", got)
	}
	if got := editor.textarea.Column(); got != 3 {
		t.Errorf("editor column = %d, want 3", got)
	}
}
