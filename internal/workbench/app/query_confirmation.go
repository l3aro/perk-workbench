package app

type queryConfirmation struct {
	dialog    *confirmationDialog
	statement string
}

func newQueryConfirmation(statement string) *queryConfirmation {
	return &queryConfirmation{dialog: yesNoConfirmation("Run destructive SQL?", statement, "run"), statement: statement}
}

func requiresQueryConfirmation(statement string) bool {
	switch statementKeyword(statement) {
	case "ALTER", "ANALYZE", "BEGIN", "COMMIT", "CREATE", "DELETE", "DROP", "END", "REINDEX", "RELEASE", "RENAME", "ROLLBACK", "SAVEPOINT", "START", "TRUNCATE", "UPDATE", "VACUUM":
		return true
	default:
		return false
	}
}
