package workbench

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const endOfLineColumn = -1

type editor struct {
	textarea      textarea.Model
	insert        bool
	desiredColumn int
	pendingG      bool
}

func newEditor() editor {
	input := textarea.New()
	input.Focus()
	return editor{textarea: input}
}

func (e editor) Update(message tea.Msg) (editor, tea.Cmd) {
	keyPress, ok := message.(tea.KeyPressMsg)
	if !ok {
		var command tea.Cmd
		e.textarea, command = e.textarea.Update(message)
		return e, command
	}

	if e.insert {
		if keyPress.Key().Code == tea.KeyEscape {
			e.enterNormalMode()
			return e, nil
		}
		var command tea.Cmd
		e.textarea, command = e.textarea.Update(message)
		return e, command
	}

	e.updateNormalMode(keyPress.String())
	return e, nil
}

func (e *editor) enterNormalMode() {
	e.insert = false
	e.pendingG = false
	if line := e.currentLine(); len(line) > 0 {
		e.textarea.SetCursorColumn(e.textarea.Column() - 1)
	}
	e.desiredColumn = e.textarea.Column()
}

func (e *editor) updateNormalMode(key string) {
	if e.pendingG {
		e.pendingG = false
		if key == "g" {
			e.textarea.MoveToBegin()
			e.restoreDesiredColumn()
		}
		return
	}

	switch key {
	case "i":
		e.insert = true
	case "a":
		if line := e.currentLine(); len(line) > 0 {
			e.textarea.SetCursorColumn(e.textarea.Column() + 1)
		}
		e.insert = true
	case "h":
		if e.textarea.Column() > 0 {
			e.textarea.SetCursorColumn(e.textarea.Column() - 1)
		}
		e.desiredColumn = e.textarea.Column()
	case "l":
		if column, line := e.textarea.Column(), e.currentLine(); column+1 < len(line) {
			e.textarea.SetCursorColumn(column + 1)
		}
		e.desiredColumn = e.textarea.Column()
	case "j":
		e.moveLogicalLine(true)
	case "k":
		e.moveLogicalLine(false)
	case "w":
		e.moveWord(true)
	case "b":
		e.moveWord(false)
	case "0":
		e.textarea.CursorStart()
		e.desiredColumn = 0
	case "$":
		e.textarea.CursorEnd()
		if len(e.currentLine()) > 0 {
			e.textarea.SetCursorColumn(e.textarea.Column() - 1)
		}
		e.desiredColumn = endOfLineColumn
	case "g":
		e.pendingG = true
	case "G":
		e.textarea.MoveToEnd()
		e.restoreDesiredColumn()
	}
}

func (e *editor) moveLogicalLine(down bool) {
	line := e.textarea.Line()
	if down {
		if line+1 >= e.textarea.LineCount() {
			return
		}
		for range e.textarea.Length() + e.textarea.LineCount() {
			if e.textarea.Line() != line {
				break
			}
			column := e.textarea.Column()
			e.textarea.CursorDown()
			if e.textarea.Line() == line && e.textarea.Column() == column {
				e.textarea.CursorEnd()
				e.textarea.CursorDown()
			}
		}
	} else {
		if line == 0 {
			return
		}
		for range e.textarea.Length() + e.textarea.LineCount() {
			if e.textarea.Line() != line {
				break
			}
			column := e.textarea.Column()
			e.textarea.CursorUp()
			if e.textarea.Line() == line && e.textarea.Column() == column {
				e.textarea.CursorStart()
				e.textarea.CursorUp()
			}
		}
	}
	e.restoreDesiredColumn()
}

func (e *editor) restoreDesiredColumn() {
	line := e.currentLine()
	if len(line) == 0 {
		e.textarea.SetCursorColumn(0)
		return
	}
	if e.desiredColumn == endOfLineColumn {
		e.textarea.SetCursorColumn(len(line) - 1)
		return
	}
	e.textarea.SetCursorColumn(min(e.desiredColumn, len(line)-1))
}

func (e *editor) moveWord(forward bool) {
	runes := []rune(e.textarea.Value())
	position := e.cursorOffset(runes)
	if forward {
		if position >= len(runes) {
			return
		}
		class := runeClass(runes[position])
		for position < len(runes) && runeClass(runes[position]) == class {
			position++
		}
		for position < len(runes) && runeClass(runes[position]) == whitespaceRune {
			position++
		}
		if position == len(runes) {
			return
		}
	} else {
		if position == 0 {
			return
		}
		position--
		for position >= 0 && runeClass(runes[position]) == whitespaceRune {
			position--
		}
		if position < 0 {
			return
		}
		class := runeClass(runes[position])
		for position > 0 && runeClass(runes[position-1]) == class {
			position--
		}
	}
	e.moveToOffset(position)
	e.desiredColumn = e.textarea.Column()
}

const (
	whitespaceRune = iota
	wordRune
	punctuationRune
)

func runeClass(r rune) int {
	if unicode.IsSpace(r) {
		return whitespaceRune
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
		return wordRune
	}
	return punctuationRune
}

func (e editor) currentLine() []rune {
	return []rune(strings.Split(e.textarea.Value(), "\n")[e.textarea.Line()])
}

func (e editor) cursorOffset(runes []rune) int {
	offset := 0
	for line := 0; line < e.textarea.Line(); line++ {
		for runes[offset] != '\n' {
			offset++
		}
		offset++
	}
	return min(offset+e.textarea.Column(), len(runes))
}

func (e *editor) moveToOffset(target int) {
	runes := []rune(e.textarea.Value())
	line, column := 0, 0
	for index, r := range runes {
		if index == target {
			break
		}
		if r == '\n' {
			line, column = line+1, 0
		} else {
			column++
		}
	}
	e.textarea.MoveToBegin()
	for e.textarea.Line() < line {
		current := e.textarea.Line()
		for e.textarea.Line() == current {
			e.textarea.CursorDown()
		}
	}
	e.textarea.SetCursorColumn(column)
}
