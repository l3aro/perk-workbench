package workbench

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/l3aro/perk-workbench/internal/chrome"
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
	palW, palH, boxX, boxY := palette.layout(100, 24)
	// Render rows keep the full canvas width; the box starts at column boxX
	// and byte-slicing at boxX+palW would cut multi-byte border glyphs. The
	// backdrop dim fills the row tail, so trim before corner checks.
	top := strings.TrimRight(lines[boxY][boxX:], " ")
	if !strings.HasPrefix(top, "╭ Command Palette ") {
		t.Fatalf("palette top border = %q, want title overlay", top)
	}
	if !strings.HasSuffix(top, "╮") {
		t.Fatalf("palette top border = %q, want closing corner", top)
	}
	if got := ansi.StringWidth(top); got != palW {
		t.Fatalf("palette top border width = %d, want %d", got, palW)
	}
	// The title uses the pane title color: no background (SGR 48), but bold
	// like the other pane overlays. The backdrop dim adds backgrounds to
	// cells outside the box, so check the title cell directly.
	titleCell := canvas.CellAt(boxX+1, boxY)
	if titleCell == nil || titleCell.Style.Bg != nil || titleCell.Style.Attrs&uv.AttrBold == 0 {
		t.Fatalf("palette title cell = %+v, want no background and bold", titleCell)
	}
	bottom := strings.TrimRight(lines[boxY+palH-1][boxX:], " ")
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("palette bottom border = %q, want corners", bottom)
	}
	// The old badge must not render inside the box; the title row is the
	// border itself, so the first inner line carries context + filter.
	if first := lines[boxY+1][boxX:]; !strings.Contains(first, "[Connection]") {
		t.Fatalf("first inner line = %q, want context", first)
	}
	// Scope group headers are plain text, not headerStyle badges: the cell
	// must carry no background (SGR 48) styling.
	scopeHeader := lines[boxY+3][boxX:]
	if !strings.Contains(scopeHeader, "Global") {
		t.Fatalf("scope header row = %q, want Global", scopeHeader)
	}
	if scopeCell := canvas.CellAt(boxX+1, boxY+3); scopeCell == nil || scopeCell.Style.Bg != nil {
		t.Fatalf("scope header cell = %+v, want no background", scopeCell)
	}
}

// TestCommandPalette_dimBackdrop guards the backdrop dim: content cells
// outside the palette box keep their glyphs with colors blended toward
// black, cells without a background get a dim canvas fill, and the box
// interior is drawn over the dim untouched.
func TestCommandPalette_dimBackdrop(t *testing.T) {
	palette := &commandPalette{
		visible:  true,
		filtered: []commandPaletteItem{{id: "first", scope: scopeGlobal}},
	}
	canvas := uv.NewScreenBuffer(100, 24)
	screen.Clear(canvas)
	ink := chrome.ParseHex(colorInk)
	stripe := chrome.ParseHex(colorStripe)
	canvas.SetCell(2, 2, &uv.Cell{Content: "X", Width: 1, Style: uv.Style{Fg: ink, Bg: stripe}})
	canvas.SetCell(2, 3, &uv.Cell{Content: "Y", Width: 1, Style: uv.Style{Fg: ink}})

	palette.paletteDraw(canvas, 100, 24)

	_, _, boxX, boxY := palette.layout(100, 24)
	if boxX <= 3 || boxY <= 3 {
		t.Fatalf("palette box at %d,%d too small to keep sample cells outside", boxX, boxY)
	}
	// Content cell: the glyph survives, both colors blend toward black.
	cell := canvas.CellAt(2, 2)
	if cell == nil || cell.Content != "X" {
		t.Fatalf("backdrop cell = %v, want glyph X preserved", cell)
	}
	if got, want := rgbaOf(cell.Style.Fg), expectDim(ink); got != want {
		t.Fatalf("backdrop fg = %v, want dimmed %v", got, want)
	}
	if got, want := rgbaOf(cell.Style.Bg), expectDim(stripe); got != want {
		t.Fatalf("backdrop bg = %v, want dimmed %v", got, want)
	}
	// Bare text cell: gets the dim canvas fill so the scrim is uniform.
	bare := canvas.CellAt(2, 3)
	if bare == nil || bare.Content != "Y" {
		t.Fatalf("bare backdrop cell = %v, want glyph Y preserved", bare)
	}
	if got, want := rgbaOf(bare.Style.Bg), expectDim(chrome.ParseHex(colorCanvas)); got != want {
		t.Fatalf("bare backdrop bg = %v, want dim canvas %v", got, want)
	}
	// Box interior: text cells are drawn fresh over the dim, keeping their
	// undimmed style (no background).
	inner := canvas.CellAt(boxX+1, boxY+1)
	if inner == nil || inner.Style.Bg != nil {
		t.Fatalf("box interior cell = %+v, want undimmed text cell", inner)
	}
}

// rgbaOf normalizes a color to its RGBA channels for comparison.
func rgbaOf(c color.Color) color.RGBA {
	return color.RGBAModel.Convert(c).(color.RGBA)
}

// expectDim is the blend the backdrop dim applies: keep 55% of each channel.
func expectDim(c color.Color) color.RGBA {
	rgba := color.RGBAModel.Convert(c).(color.RGBA)
	return color.RGBA{
		R: uint8(float64(rgba.R) * 0.55),
		G: uint8(float64(rgba.G) * 0.55),
		B: uint8(float64(rgba.B) * 0.55),
		A: 255,
	}
}

// TestVimMode_paletteToggleTransitionsAndPersists drives the palette toggle
// in place: the flag flips, the active SQL editor enters insert mode so
// typing works without a re-click, and config.json is rewritten.
func TestVimMode_paletteToggleTransitionsAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{})

	model := readyModel(t)
	model.configPath = filepath.Join(dir, "perk-workbench", "config.json")
	model.Focus, model.Tab = focusWorkspace, tabSQL
	model = resizeModel(model, 100, 24)
	if !model.vimMode {
		t.Fatal("test setup: vim mode should default on")
	}
	if model.overlay.formMode.editing() {
		t.Fatal("test setup: SQL editor should start in normal mode")
	}
	for _, item := range newCommandPalette(model).items {
		if item.id == "vim.toggle" && item.label != "vim mode: on" {
			t.Fatalf("palette label = %q, want vim mode: on", item.label)
		}
	}

	// Normal mode swallows typing.
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if model.queryLog.editor.value != "" {
		t.Fatalf("editor = %q, want empty before toggle", model.queryLog.editor.value)
	}

	// Toggle off via the palette command.
	updated, _ = model.handlePaletteCommand("vim.toggle")
	model = updated.(Model)
	if model.vimMode {
		t.Fatal("vim mode still on after toggle")
	}
	if !model.overlay.formMode.editing() {
		t.Fatal("SQL editor did not enter insert mode when vim mode switched off")
	}

	// Typing now lands in the editor.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	model = updated.(Model)
	if model.queryLog.editor.value != "j" {
		t.Fatalf("editor = %q, want j after toggle", model.queryLog.editor.value)
	}

	// Persisted for the next launch.
	contents, err := os.ReadFile(model.configPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(contents), `"vim_mode": false`) {
		t.Fatalf("config = %q, want vim_mode false", contents)
	}

	// Toggle back on.
	updated, _ = model.handlePaletteCommand("vim.toggle")
	model = updated.(Model)
	if !model.vimMode {
		t.Fatal("vim mode still off after second toggle")
	}
}

func TestCommandPalette_connectionShowsExecutableCommands(t *testing.T) {
	model := New("", context.Background(), testOpen, false)

	assertCommandIDs(t, newCommandPalette(model), "app.quit", "connection.add", "connection.delete", "connection.edit", "connection.switch_to_form", "notifications.show", "table.open_target", "theme.select", "vim.toggle")

	model.connection.form.focus = connectionFocusForm
	assertCommandIDs(t, newCommandPalette(model), "app.quit", "connection.edit_field", "connection.execute", "connection.field_next", "connection.field_prev", "connection.switch_to_list", "editor.external", "notifications.show", "table.open_target", "theme.select", "vim.toggle")
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
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.overlay.commandPalette.visible {
		t.Fatal("palette did not open")
	}

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	model = updated.(Model)
	if model.overlay.commandPalette.cursor != 1 {
		t.Fatalf("wheel down cursor = %d, want 1", model.overlay.commandPalette.cursor)
	}
	if !model.overlay.commandPalette.visible {
		t.Fatal("wheel closed the palette")
	}

	updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	model = updated.(Model)
	if model.overlay.commandPalette.cursor != 0 {
		t.Fatalf("wheel up cursor = %d, want 0", model.overlay.commandPalette.cursor)
	}
}

func TestModelCommandPalette_outsideClickClosesWithoutLeakingRelease(t *testing.T) {
	model := resizeModel(readyModel(t), 100, 24)
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.overlay.commandPalette.visible {
		t.Fatal("palette did not open")
	}
	want := model.Focus

	// Click outside the palette box — closes it.
	updated, _ = model.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.overlay.commandPalette.visible {
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
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	p := model.overlay.commandPalette

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
	if model.overlay.commandPalette.visible {
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
	updated, _ := model.Update(tea.MouseClickMsg{X: 100 - headerRightMargin - headerButtonWidth() - (headerButtonWidth()+1)/2, Y: 0, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.overlay.commandPalette.visible {
		t.Fatal("palette did not open")
	}

	_, _, boxX, boxY := model.overlay.commandPalette.layout(100, 24)
	// Header row click — consumed, palette stays open (off-by-one guard).
	updated, _ = model.Update(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 3, Button: tea.MouseLeft})
	model = updated.(Model)
	if !model.overlay.commandPalette.visible {
		t.Fatal("click on scope header closed the palette")
	}
	// First item sits at inner row 3 (title, blank, header, item).
	updated, _ = model.Update(tea.MouseClickMsg{X: boxX + 2, Y: boxY + 4, Button: tea.MouseLeft})
	model = updated.(Model)
	if model.overlay.commandPalette.visible {
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
	if !model.overlay.commandPalette.visible {
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
		if model.overlay.commandPalette.cursor != test.want {
			t.Fatalf("cursor = %d, want %d", model.overlay.commandPalette.cursor, test.want)
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

	if model.overlay.commandPalette.visible || model.overlay.themePicker == nil {
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
	model.overlay.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	if activeTheme != themeNord {
		t.Fatalf("previewed theme = %q, want %q", activeTheme, themeNord)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.themePicker != nil {
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
	model.overlay.themePicker = newThemePicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.themePicker != nil {
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

func TestModelCommandPalette_tableOpenTargetPickerCommitsAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{})

	model := readyModel(t)
	model.configPath = filepath.Join(dir, "perk-workbench", "config.json")
	for _, item := range newCommandPalette(model).items {
		if item.id == "table.open_target" && item.label != "open table → Columns" {
			t.Fatalf("palette label = %q, want open table → Columns", item.label)
		}
	}

	updated, _ := model.handlePaletteCommand("table.open_target")
	model = updated.(Model)
	if model.overlay.tableTargetPicker == nil {
		t.Fatal("table.open_target did not open the table target picker")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.overlay.tableTargetPicker != nil {
		t.Fatal("table target picker remained visible after selection")
	}
	if got := tableOpenTargetTab(); got != tabBrowse {
		t.Fatalf("tableOpenTargetTab = %v, want Browse", got)
	}
	for _, item := range newCommandPalette(model).items {
		if item.id == "table.open_target" && item.label != "open table → Browse" {
			t.Fatalf("palette label = %q, want open table → Browse", item.label)
		}
	}
	config, err := LoadConfig(model.configPath)
	if err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
	if config.TableOpenTarget != "browse" {
		t.Fatalf("persisted table_open_target = %q, want %q", config.TableOpenTarget, "browse")
	}
}

func TestTableTargetPicker_cancelsWithoutChange(t *testing.T) {
	previous := appConfig
	t.Cleanup(func() { appConfig = previous })
	SetAppConfig(Config{})

	model := readyModel(t)
	model.overlay.tableTargetPicker = newTableTargetPicker()
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = updated.(Model)
	if model.overlay.tableTargetPicker != nil {
		t.Fatal("table target picker remained visible after cancel")
	}
	if got := tableOpenTargetTab(); got != tabStructure {
		t.Fatalf("tableOpenTargetTab = %v, want unchanged Structure", got)
	}
}
