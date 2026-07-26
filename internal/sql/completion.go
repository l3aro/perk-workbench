package sql

var Keywords = []string{
	"ALTER", "AND", "AS", "ASC", "BETWEEN", "BY", "CASE", "CREATE", "DELETE", "DESC", "DISTINCT", "DROP",
	"FROM", "GROUP", "HAVING", "INSERT", "INTO", "JOIN", "LEFT", "LIMIT", "NOT", "NULL", "ON", "OR",
	"ORDER", "RIGHT", "SELECT", "SET", "UPDATE", "VALUES", "WHERE", "WITH",
}

func CompletionPrefix(value string) string {
	for index := len(value); index > 0; index-- {
		character := value[index-1]
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '_' && character != '.' {
			return value[index:]
		}
	}
	return value
}
