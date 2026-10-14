package repository

import "strings"

// splitSQLStatements recognizes lexical statement boundaries, not SQL grammar.
// Keep comments and quoted bytes intact for PostgreSQL, and leave malformed SQL
// for the server to reject. Ordinary strings use PostgreSQL's default
// standard_conforming_strings=on; backslash escapes require explicit E'...'.
func splitSQLStatements(content string) []string {
	var statements []string
	start := 0
	for i := 0; i < len(content); {
		if end, _ := migrationSQLLexemeEnd(content, i); end > i {
			i = end
			continue
		}
		if content[i] == ';' {
			if part := content[start:i]; strings.TrimSpace(part) != "" {
				statements = append(statements, part)
			}
			start = i + 1
		}
		i++
	}
	if part := content[start:]; strings.TrimSpace(part) != "" {
		statements = append(statements, part)
	}
	return statements
}

// Validation and comment-only detection must use the same lexical boundaries as
// execution: '--' inside a literal/identifier is data, and block comments nest.
func stripMigrationSQLComments(content string) string {
	var result strings.Builder
	start := 0
	for i := 0; i < len(content); {
		end, comment := migrationSQLLexemeEnd(content, i)
		if end <= i {
			i++
			continue
		}
		if comment {
			_, _ = result.WriteString(content[start:i])
			_ = result.WriteByte(' ') // Comments separate tokens, even without spaces.
			start = end
		}
		i = end
	}
	_, _ = result.WriteString(content[start:])
	return strings.TrimSpace(result.String())
}

// Returns the end of a quoted value/identifier or comment beginning at start.
// Returning start means ordinary SQL. An unterminated block is retained rather
// than treated as a comment-only migration that could be marked applied.
func migrationSQLLexemeEnd(sql string, start int) (end int, comment bool) {
	if strings.HasPrefix(sql[start:], "--") {
		end = start + 2
		for end < len(sql) && sql[end] != '\n' && sql[end] != '\r' {
			end++
		}
		return end, true
	}
	if strings.HasPrefix(sql[start:], "/*") {
		depth := 1
		for end = start + 2; end < len(sql); {
			switch {
			case strings.HasPrefix(sql[end:], "/*"):
				depth++
				end += 2
			case strings.HasPrefix(sql[end:], "*/"):
				depth--
				end += 2
				if depth == 0 {
					return end, true
				}
			default:
				end++
			}
		}
		return len(sql), false
	}
	quote := sql[start]
	if quote == '\'' || quote == '"' {
		escaped := quote == '\'' && start > 0 && (sql[start-1] == 'E' || sql[start-1] == 'e') &&
			(start == 1 || !migrationSQLIdentifierContinue(sql[start-2]))
		for end = start + 1; end < len(sql); end++ {
			if escaped && sql[end] == '\\' {
				if end+1 < len(sql) {
					end++
				}
				continue
			}
			if sql[end] == quote {
				if end+1 < len(sql) && sql[end+1] == quote {
					end++
					continue
				}
				if quote == '\'' {
					if nextQuote := migrationSQLStringContinuation(sql, end+1); nextQuote >= 0 {
						// PostgreSQL concatenates literals separated by a newline.
						// E need only appear on the first segment, so retain its
						// backslash-escape mode throughout the continued literal.
						end = nextQuote
						continue
					}
				}
				return end + 1, false
			}
		}
		return len(sql), false
	}
	if quote == '$' && (start == 0 || !migrationSQLIdentifierContinue(sql[start-1])) {
		end = start + 1
		if end < len(sql) && migrationSQLIdentifierStart(sql[end]) {
			for end++; end < len(sql) && (migrationSQLIdentifierStart(sql[end]) || sql[end] >= '0' && sql[end] <= '9'); end++ {
			}
		}
		if end < len(sql) && sql[end] == '$' {
			tag := sql[start : end+1]
			if closing := strings.Index(sql[end+1:], tag); closing >= 0 {
				return end + 1 + closing + len(tag), false
			}
			return len(sql), false
		}
	}
	return start, false
}

func migrationSQLStringContinuation(sql string, start int) int {
	newline := false
	for i := start; i < len(sql); {
		switch sql[i] {
		case ' ', '\t', '\f', '\v':
			i++
		case '\n', '\r':
			newline = true
			i++
		case '-':
			if !strings.HasPrefix(sql[i:], "--") {
				return -1
			}
			for i += 2; i < len(sql) && sql[i] != '\n' && sql[i] != '\r'; i++ {
			}
		default:
			if sql[i] == '\'' && newline {
				return i
			}
			return -1
		}
	}
	return -1
}

func migrationSQLIdentifierStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func migrationSQLIdentifierContinue(c byte) bool {
	return migrationSQLIdentifierStart(c) || c >= '0' && c <= '9' || c == '$'
}
