package workbench

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
)

func TestCommandPalette_navigationAndSelection(t *testing.T) {
	palette := commandPalette{
		filtered: []commandPaletteItem{
			{id: "first"},
			{id: "second"},
		},
		visible: true,
	}

	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 1},
		{key: tea.KeyPressMsg{Code: tea.KeyUp}, want: 0},
		{key: tea.KeyPressMsg{Code: tea.KeyDown}, want: 1},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		_, _, consumed := palette.handleKey(test.key)
		if !consumed {
			t.Fatal("navigation key was not consumed")
		}
		if palette.cursor != test.want {
			t.Fatalf("cursor = %d, want %d", palette.cursor, test.want)
		}
	}

	_, _, _ = palette.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	selected, closed, consumed := palette.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !consumed || closed {
		t.Fatal("enter was not handled as a palette selection")
	}
	if selected.id != "second" {
		t.Fatalf("selected command = %q, want %q", selected.id, "second")
	}
	if palette.visible {
		t.Fatal("palette remained visible after selection")
	}
}

// TestCommandPalette_drawsTitleInTopBorder guards the border overlay: the
// " Command Palette " badge is gone from the body and the title sits in the
// top border, with the context/filter prompt as the first inner line.
func TestCommandPalette_drawsTitleInTopBorder(t *testing.T) {
	palette := &commandPalette{
		visible:      true,
		filtered:     []commandPaletteItem{{id: "first", scope: scopeGlobal}},
		contextTitle: "Connection",
	}
	canvas := uv.NewScreenBuffer(100, 24)
	screen.Clear(canvas)
	palette.paletteDraw(canvas, 100, 24)

	lines := strings.Split(ansi.Strip(canvas.Render()), "\n")
	rawLines := strings.Split(canvas.Render(), "\n")
	palW, palH, boxX, boxY := palette.layout(100, 24)
	// Render rows keep the full canvas width; the box starts at column boxX
	// and byte-slicing at boxX+palW would cut multi-byte border glyphs.
	top := lines[boxY][boxX:]
	if !strings.HasPrefix(top, "┌ Command Palette ") {
		t.Fatalf("palette top border = %q, want title overlay", top)
	}
	if !strings.HasSuffix(top, "┐") {
		t.Fatalf("palette top border = %q, want closing corner", top)
	}
	if got := ansi.StringWidth(strings.TrimRight(top, " ")); got != palW {
		t.Fatalf("palette top border width = %d, want %d", got, palW)
	}
	// The title uses the pane title color: no background (SGR 48), but bold
	// like the other pane overlays (SGR \x1b[…;1m).
	if rawTop := rawLines[boxY]; strings.Contains(rawTop, "48") || !strings.Contains(rawTop, ";1m") {
		t.Fatalf("palette title row lacks pane title styling: %q", rawTop)
	}
	bottom := lines[boxY+palH-1][boxX:]
	if !strings.HasPrefix(bottom, "└") || !strings.HasSuffix(bottom, "┘") {
		t.Fatalf("palette bottom border = %q, want corners", bottom)
	}
	// The old badge must not render inside the box; the title row is the
	// border itself, so the first inner line carries context + filter.
	if first := lines[boxY+1][boxX:]; !strings.Contains(first, "[Connection]") {
		t.Fatalf("first inner line = %q, want context", first)
	}
	// Scope group headers are plain text, not headerStyle badges: the row
	// must carry no background (SGR 48) styling.
	scopeHeader := lines[boxY+3][boxX:]
	if !strings.Contains(scopeHeader, "Global") {
		t.Fatalf("scope header row = %q, want Global", scopeHeader)
	}
	if strings.Contains(rawLines[boxY+3], "48") {
		t.Fatalf("scope header row carries a background badge: %q", rawLines[boxY+3])
	}
}

func TestCommandPalette_connectionShowsExecutableCommands(t *testing.T) {
	model := New("", context.Background(), testOpen, false)

	assertCommandIDs(t, newCommandPalette(model), "app.quit", "connection.add", "connection.delete", "connection.edit", "connection.switch_to_form", "theme.select")

	model.connection.focus = connectionFocusForm
	assertCommandIDs(t, newCommandPalette(model), "app.quit", "connection.edit_field", "connection.execute", "connection.field_next", "connection.field_prev", "connection.switch_to_list", "editor.external", "theme.select")
}

func assertCommandIDs(t *testing.T, palette *commandPalette, want ...CommandID) {
	t.Helper()
	if len(palette.items) != len(want) {
		t.Fatalf("palette command count = %d, want %d", len(palette.items), len(want))
	}
	available := make(map[CommandID]bool, len(palette.items))
	for _, item := range palette.items {
		available[item.id] = true
	}
	for _, id := range want {
		if !available[id] {
			t.Fatalf("palette lacks %q", id)
		}
	}
}

func TestCommandPalette_wheelMovesCursor(t *testing.T) {
	palette := &commandPalette{
		filtered: []commandPaletteItem{{id: "first"}, {id: "second"}, {id: "third"}},
	}

	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if palette.cursor != 1 {
		t.Fatalf("wheel down cursor = %d, want 1", palette.cursor)
	}
	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if palette.cursor != 2 {
		t.Fatalf("wheel down past bottom cursor = %d, want 2", palette.cursor)
	}
	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if palette.cursor != 1 {
		t.Fatalf("wheel up cursor = %d, want 1", palette.cursor)
	}
	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	palette.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if palette.cursor != 0 {
		t.Fatalf("wheel up past top cursor = %d, want 0", palette.cursor)
	}

	empty := &commandPalette{}
	empty.handleWheel(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if empty.cursor != 0 {
		t.Fatalf("wheel on empty palette cursor = %d, want 0", empty.cursor)
	}
}

func TestCommandPalette_clickSelectsItem(t *testing.T) {
	palette := &commandPalette{
		visible:  true,
		filtered: []commandPaletteItem{{id: "first", scope: scopeGlobal}, {id: "second", scope: scopeGlobal}, {id: "third", scope: scopeGlobal}},
	}
	// 100x24: palW=60, palH=min(14, 9)=9, boxX=20, boxY=7. Inner area starts
	// at boxY+1: title, blank, then list from boxY+3: line 0 = scope header,
	// line 1 = first item, line 2 = second item.
	_, _, boxX, boxY := palette.layout(100, 24)

	// Click outside the box — dismisses the palette, no dispatch.
	selected, consumed := palette.handleClick(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft}, 100, 24)
	if !consumed || selected.id != "" {
		t.Fatalf("outside click: consumed=%t selected=%q", consumed, selected.id)
	}
	if palette.visible {
		t.Fatal("outside click did not close the palette")
	}
	if !palette.swallowRelease {
		t.Fatal("outside click did not arm the release swallow")
	}
	// Reopen for the in-box assertions.
	palette.visible = true
	palette.swallowRelease = false
	// Click on the scope header — consumed, nothing selected.
	selected, consumed = palette.handleClick(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 3, Button: tea.MouseLeft}, 100, 24)
	if !consumed || selected.id != "" {
		t.Fatalf("header click: consumed=%t selected=%q", consumed, selected.id)
	}
	if palette.swallowRelease {
		t.Fatal("header click armed the release swallow")
	}
	// Click on the first item row — selects it and closes.
	selected, consumed = palette.handleClick(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 4, Button: tea.MouseLeft}, 100, 24)
	if !consumed || selected.id != "first" {
		t.Fatalf("item click: consumed=%t selected=%q, want first", consumed, selected.id)
	}
	if palette.visible {
		t.Fatal("palette remained visible after item click")
	}
	if !palette.swallowRelease {
		t.Fatal("item click did not arm the release swallow")
	}
	// Click on the second item row — selects it.
	palette.visible = true
	selected, consumed = palette.handleClick(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 5, Button: tea.MouseLeft}, 100, 24)
	if !consumed || selected.id != "second" {
		t.Fatalf("second item click: consumed=%t selected=%q, want second", consumed, selected.id)
	}
}

func TestModelCommandPalette_wheelNavigatesSelection(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 99, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("palette did not open")
	}

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.commandPalette.cursor != 1 {
		t.Fatalf("wheel down cursor = %d, want 1", model.commandPalette.cursor)
	}
	if !model.commandPalette.visible {
		t.Fatal("wheel closed the palette")
	}

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.commandPalette.cursor != 0 {
		t.Fatalf("wheel up cursor = %d, want 0", model.commandPalette.cursor)
	}
}

func TestModelCommandPalette_outsideClickClosesWithoutLeakingRelease(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 99, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("palette did not open")
	}
	want := model.Focus

	// Click outside the palette box — closes it.
	updated, _ = model.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.commandPalette.visible {
		t.Fatal("outside click did not close the palette")
	}

	// Trailing release must not click the pane underneath.
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != want {
		t.Fatalf("release after outside click changed focus: %v, want %v", model.Focus, want)
	}

	// Swallow is one-shot: a later real release still clicks panes.
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != focusSchema {
		t.Fatalf("second release focus = %v, want focusSchema", model.Focus)
	}
}

func TestModelCommandPalette_clickSelectDoesNotLeakReleaseToPane(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 99, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	p := model.commandPalette

	// Locate the rendered row of the focus.query_log item.
	logIdx := -1
	for i, item := range p.filtered {
		if item.id == "focus.query_log" {
			logIdx = i
			break
		}
	}
	if logIdx < 0 {
		t.Fatal("palette lacks focus.query_log")
	}
	_, palH, boxX, boxY := p.layout(100, 24)
	_, itemAtLine := p.listContent(palH)
	lineIdx := -1
	for i, idx := range itemAtLine {
		if idx == logIdx {
			lineIdx = i
			break
		}
	}
	if lineIdx < 0 {
		t.Fatal("focus.query_log not visible in list")
	}

	// Click the item row — focuses the query log and closes the palette.
	updated, _ = model.Update(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 3 + lineIdx, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != focusQueryLog {
		t.Fatalf("focus = %v, want focusQueryLog", model.Focus)
	}
	if model.commandPalette.visible {
		t.Fatal("palette still visible after click")
	}

	// The trailing release must not click the pane underneath the palette.
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 2, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != focusQueryLog {
		t.Fatalf("release re-focused the pane under the palette: focus = %v, want focusQueryLog", model.Focus)
	}

	// The swallow is one-shot: a later real release still clicks panes.
	updated, _ = model.Update(tea.MouseReleaseMsg{X: 2, Y: 10, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.Focus != focusSchema {
		t.Fatalf("second release focus = %v, want focusSchema", model.Focus)
	}
}

func TestModelCommandPalette_clickSelectsRenderedItem(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 99, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("palette did not open")
	}

	_, _, boxX, boxY := model.commandPalette.layout(100, 24)
	// Header row click — consumed, palette stays open (off-by-one guard).
	updated, _ = model.Update(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 3, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("click on scope header closed the palette")
	}
	// First item sits at inner row 3 (title, blank, header, item).
	updated, _ = model.Update(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.commandPalette.visible {
		t.Fatal("click on first item did not select and close the palette")
	}
}

func TestCommandPalette_filterRequiresSlashAndExitKeepsQuery(t *testing.T) {
	palette := &commandPalette{
		items:    []commandPaletteItem{{id: "first", label: "first"}, {id: "second", label: "second"}},
		filtered: []commandPaletteItem{{id: "first", label: "first"}, {id: "second", label: "second"}},
		visible:  true,
	}

	palette.handleKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if palette.filtering || len(palette.query) != 0 {
		t.Fatal("ordinary key entered filtering")
	}
	palette.handleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !palette.filtering {
		t.Fatal("slash did not enter filtering")
	}
	for _, char := range []rune{'s', 'e', 'c'} {
		palette.handleKey(tea.KeyPressMsg{Code: char, Text: string(char)})
	}
	palette.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if palette.filtering || string(palette.query) != "sec" {
		t.Fatalf("after escape: filtering=%t query=%q", palette.filtering, string(palette.query))
	}
	if len(palette.filtered) != 1 || palette.filtered[0].id != "second" {
		t.Fatalf("filtered items = %#v, want second", palette.filtered)
	}

	palette.handleKey(tea.KeyPressMsg{Code: '/'})
	palette.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if palette.filtering || !palette.visible || string(palette.query) != "sec" {
		t.Fatalf("after enter: filtering=%t visible=%t query=%q", palette.filtering, palette.visible, string(palette.query))
	}
}

func TestCommandPalette_filterNoDuplicates(t *testing.T) {
	// Regression: filtered and items must not share a backing array.
	// When they do, each applyFilter call corrupts p.items while ranging,
	// producing duplicate entries as the query grows.
	items := []commandPaletteItem{
		{id: "app.quit", label: "quit with confirm"},
		{id: "workspace.tab_next", label: "next tab"},
		{id: "theme.select", label: "theme"},
		{id: "focus.schema", label: "schema"},
	}
	palette := &commandPalette{
		items:    items,
		filtered: append([]commandPaletteItem{}, items...),
	}

	// Type "th" one character at a time, checking no duplicates after each.
	for _, char := range []rune{'t', 'h'} {
		palette.query = append(palette.query, char)
		palette.applyFilter()
		seen := map[string]bool{}
		for _, item := range palette.filtered {
			if seen[item.label] {
				t.Fatalf("after typing %q: duplicate label %q in filtered list",
					string(palette.query), item.label)
			}
			seen[item.label] = true
			if !strings.Contains(strings.ToLower(item.label), strings.ToLower(string(palette.query))) {
				t.Fatalf("after typing %q: item %q does not match query",
					string(palette.query), item.label)
			}
		}
	}

	// After "th": "quit with confirm" (contains "th" in "with"), "theme" — exactly 2 items.
	if len(palette.filtered) != 2 {
		t.Fatalf("after typing \"th\": got %d items, want 2", len(palette.filtered))
	}
}

func TestModelCommandPalette_opensThemePicker(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep theme commit off the real config dir

	model := New("", context.Background(), testOpen, false)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl, Text: "p"})
	model = updated.(Model)
	if !model.commandPalette.visible {
		t.Fatal("ctrl+p did not open the palette")
	}

	for _, test := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 1},
		{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 0},
	} {
		updated, _ = model.Update(test.key)
		model = updated.(Model)
		if model.commandPalette.cursor != test.want {
			t.Fatalf("cursor = %d, want %d", model.commandPalette.cursor, test.want)
		}
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	model = updated.(Model)
	for _, character := range "theme" {
		updated, _ = model.Update(tea.KeyPressMsg{Code: character, Text: string(character)})
		model = updated.(Model)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.commandPalette.visible || model.themePicker == nil {
		t.Fatal("theme selection did not open the theme picker")
	}
	if activeTheme != original {
		t.Fatalf("theme = %q, want %q", activeTheme, original)
	}
}

func TestThemePicker_previewsAndCancelsTheme(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })

	model := New("", context.Background(), testOpen, false)
	model.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if activeTheme != themeNord {
		t.Fatalf("previewed theme = %q, want %q", activeTheme, themeNord)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.themePicker != nil {
		t.Fatal("theme picker remained visible after cancel")
	}
	if activeTheme != original {
		t.Fatalf("restored theme = %q, want %q", activeTheme, original)
	}
}

func TestThemePicker_commitsPreviewedTheme(t *testing.T) {
	original := activeTheme
	t.Cleanup(func() { setTheme(original) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep the commit off the real config dir

	model := New("", context.Background(), testOpen, false)
	model.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.themePicker != nil {
		t.Fatal("theme picker remained visible after selection")
	}
	if activeTheme != themeNord {
		t.Fatalf("committed theme = %q, want %q", activeTheme, themeNord)
	}
	config, err := LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.Theme != "nord" {
		t.Fatalf("persisted theme = %q, want %q", config.Theme, "nord")
	}
}
