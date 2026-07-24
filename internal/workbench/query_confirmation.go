package workbench

import "charm.land/huh/v2"

type queryConfirmation struct {
	form      *huh.Form
	statement string
	confirmed bool
}

func newQueryConfirmation(statement string, width int) *queryConfirmation {
	confirmation := &queryConfirmation{statement: statement}
	confirmation.form = huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Run destructive SQL?").Description(statement).Height(8),
		huh.NewConfirm().Key("confirm").Affirmative("Yes").Negative("No").Value(&confirmation.confirmed),
	)).WithShowHelp(width >= 40).WithWidth(max(width, 1))
	return confirmation
}

func requiresQueryConfirmation(statement string) bool {
	switch statementKeyword(statement) {
	case "ALTER", "ANALYZE", "BEGIN", "COMMIT", "CREATE", "DELETE", "DROP", "END", "REINDEX", "RELEASE", "RENAME", "ROLLBACK", "SAVEPOINT", "START", "TRUNCATE", "UPDATE", "VACUUM":
		return true
	default:
		return false
	}
}
