package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationGateWorktree(t *testing.T) {
	script, err := filepath.Abs("../../scripts/check-migration-gate.sh")
	require.NoError(t, err)
	for _, test := range []struct {
		name, filename, sql                                  string
		stage, committed, allowBreaking, wantError, headOnly bool
		want                                                 string
	}{
		{name: "untracked breaking DDL", filename: "002_new.sql", sql: "ALTER TABLE example ALTER COLUMN label TYPE VARCHAR(128);", wantError: true, want: "ALTER_COLUMN_TYPE"},
		{name: "explicit breaking approval", filename: "002_new.sql", sql: "ALTER TABLE example ALTER COLUMN label TYPE VARCHAR(128);", allowBreaking: true, want: "migration gate: OK"},
		{name: "staged breaking DDL", filename: "002_new.sql", sql: "DROP TABLE example;", stage: true, wantError: true, want: "DROP_TABLE"},
		{name: "committed breaking DDL", filename: "002_new.sql", sql: "DROP TABLE example;", stage: true, committed: true, wantError: true, want: "DROP_TABLE"},
		{name: "CI mode still checks committed DDL", filename: "002_new.sql", sql: "DROP TABLE example;", stage: true, committed: true, headOnly: true, wantError: true, want: "DROP_TABLE"},
		{name: "CI mode ignores local-only files", filename: "002_new.sql", sql: "DROP TABLE example;", headOnly: true, want: "migration gate: OK"},
		{name: "existing migration cannot be changed", filename: "001_initial.sql", sql: "SELECT 2;", allowBreaking: true, wantError: true, want: "status=M"},
		{name: "untracked transactional concurrent index", filename: "002_index.sql", sql: "CREATE INDEX CONCURRENTLY example_idx ON example(id);", allowBreaking: true, wantError: true, want: "CONCURRENTLY"},
		{name: "safe concurrent index", filename: "002_index_notx.sql", sql: "CREATE INDEX CONCURRENTLY example_idx ON example(id);", want: "migration gate: OK"},
		{name: "safe addition with spaces", filename: "002_new column.sql", sql: "ALTER TABLE example ADD COLUMN note TEXT;", want: "migration gate: OK"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				cmd := exec.Command("git", append([]string{"-c", "user.name=Migration Test", "-c", "user.email=migration@example.test", "-c", "commit.gpgsign=false"}, args...)...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
				return string(out)
			}
			git("init", "-q")
			migrationDir := filepath.Join(dir, "backend/migrations")
			require.NoError(t, os.MkdirAll(migrationDir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(migrationDir, "001_initial.sql"), []byte("SELECT 1;\n"), 0644))
			git("add", ".")
			git("commit", "-qm", "baseline")
			git("tag", "baseline")
			require.NoError(t, os.WriteFile(filepath.Join(migrationDir, test.filename), []byte(test.sql+"\n"), 0644))
			if test.stage {
				git("add", ".")
			}
			if test.committed {
				git("commit", "-qm", "new migration")
			}
			args := []string{script, "baseline"}
			if !test.headOnly {
				args = append(args, "--worktree")
			}
			if test.allowBreaking {
				args = append(args, "--allow-breaking")
			}
			cmd := exec.Command("bash", args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if test.wantError {
				require.Error(t, err, string(out))
			} else {
				require.NoError(t, err, string(out))
			}
			require.Contains(t, string(out), test.want)
		})
	}
}
