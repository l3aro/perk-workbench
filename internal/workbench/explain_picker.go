package workbench

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	sharedsql "github.com/l3aro/perk/internal/sql"
)

type explainPicker struct {
	form      *huh.Form
	selection string
	statement string
}

func newExplainPicker(product, version, statement string, width int) *explainPicker {
	if sharedsql.ValidateStatement(statement) != nil {
		return nil
	}
	options := explainOptions(product, version, statement)
	if len(options) == 0 {
		return nil
	}

	picker := &explainPicker{statement: statement, selection: options[0]}
	choices := make([]huh.Option[string], len(options))
	for index, option := range options {
		choices[index] = huh.NewOption(option, option)
	}
	picker.form = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Key("explain").Title("Explain query with").Options(choices...).Value(&picker.selection),
	)).WithShowHelp(width >= 40).WithWidth(max(width, 1))
	return picker
}

func (p *explainPicker) Update(message tea.Msg) tea.Cmd {
	form, command := p.form.Update(message)
	p.form = form.(*huh.Form)
	return command
}

func (p *explainPicker) completed() bool { return p.form.State == huh.StateCompleted }

func (p *explainPicker) setWidth(width int) {
	p.form.WithWidth(max(width, 1)).WithShowHelp(width >= 40)
}

func (p *explainPicker) query() string { return p.selection + "\n" + p.statement }

func explainOptions(product, version, statement string) []string {
	switch product {
	case "SQLite":
		if sqliteExplainable(statement) {
			return []string{"EXPLAIN", "EXPLAIN QUERY PLAN"}
		}
	case "MySQL":
		if !mysqlExplainable(statement) {
			return nil
		}
		options := []string{"EXPLAIN"}
		if mysqlAnalyzeSupported(version) && mysqlAnalyzeable(statement) {
			options = append(options, "EXPLAIN ANALYZE")
		}
		return options
	}
	return nil
}

func sqliteExplainable(statement string) bool {
	keyword := explainStatementKeyword(statement)
	return keyword != "" && keyword != "EXPLAIN"
}

func mysqlExplainable(statement string) bool {
	switch explainStatementKeyword(statement) {
	case "SELECT", "TABLE", "INSERT", "DELETE", "UPDATE", "REPLACE", "WITH":
		return true
	default:
		return false
	}
}

func mysqlAnalyzeable(statement string) bool {
	switch explainStatementKeyword(statement) {
	case "SELECT", "TABLE", "WITH":
		return true
	default:
		return false
	}
}

func explainStatementKeyword(statement string) string {
	for {
		statement = strings.TrimSpace(statement)
		switch {
		case strings.HasPrefix(statement, "--"):
			if index := strings.IndexByte(statement, '\n'); index >= 0 {
				statement = statement[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(statement, "/*"):
			if index := strings.Index(statement[2:], "*/"); index >= 0 {
				statement = statement[index+4:]
				continue
			}
			return ""
		}
		break
	}

	for index, runeValue := range statement {
		if !((runeValue >= 'a' && runeValue <= 'z') || (runeValue >= 'A' && runeValue <= 'Z')) {
			return strings.ToUpper(statement[:index])
		}
	}
	return strings.ToUpper(statement)
}

func mysqlAnalyzeSupported(version string) bool {
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return false
	}
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patchText := parts[2]
	for index, runeValue := range patchText {
		if runeValue < '0' || runeValue > '9' {
			patchText = patchText[:index]
			break
		}
	}
	patch, patchErr := strconv.Atoi(patchText)
	if majorErr != nil || minorErr != nil || patchErr != nil {
		return false
	}
	return major > 8 || (major == 8 && (minor > 0 || (minor == 0 && patch >= 18)))
}
