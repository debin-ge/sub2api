package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestSplitSQLStatementsPostgresLexicalBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"line comment", "-- A comment; contains ' and $$\nSELECT 1; SELECT 2;", []string{"-- A comment; contains ' and $$\nSELECT 1", " SELECT 2"}},
		{"CRLF comment", "-- ignore;\r\nSELECT 1;", []string{"-- ignore;\r\nSELECT 1"}},
		{"nested block comments", "/* outer; /* nested; */ ' ; */ SELECT 1; SELECT 2;", []string{"/* outer; /* nested; */ ' ; */ SELECT 1", " SELECT 2"}},
		{"ordinary string and doubled quote", "SELECT 'a;b'';c'; SELECT 2;", []string{"SELECT 'a;b'';c'", " SELECT 2"}},
		{"standard string backslash", `SELECT 'backslash\'; SELECT 2;`, []string{`SELECT 'backslash\'`, " SELECT 2"}},
		{"quoted identifier", `SELECT "a;"";b"; SELECT 2;`, []string{`SELECT "a;"";b"`, " SELECT 2"}},
		{"escape string", `SELECT E'quote\';tail'; SELECT 2;`, []string{`SELECT E'quote\';tail'`, " SELECT 2"}},
		{"lowercase escape string", `SELECT e'quote\';tail'; SELECT 2;`, []string{`SELECT e'quote\';tail'`, " SELECT 2"}},
		{"escaped backslash", `SELECT E'tail\\'; SELECT 2;`, []string{`SELECT E'tail\\'`, " SELECT 2"}},
		{"continued escape string", "SELECT E'first'\n'quote\\';tail'; SELECT 2;", []string{"SELECT E'first'\n'quote\\';tail'", " SELECT 2"}},
		{"continued escape string with line comment", "SELECT E'first' -- comment;\r\n'quote\\';tail'; SELECT 2;", []string{"SELECT E'first' -- comment;\r\n'quote\\';tail'", " SELECT 2"}},
		{"escape prefix must be token boundary", `SELECT nameE'backslash\'; SELECT 2;`, []string{`SELECT nameE'backslash\'`, " SELECT 2"}},
		{"untagged dollar quote", `SELECT $$one; ' -- /* ; */$$; SELECT 2;`, []string{`SELECT $$one; ' -- /* ; */$$`, " SELECT 2"}},
		{"tagged dollar quote", `DO $body$ BEGIN PERFORM 'a;'; END $body$; SELECT 2;`, []string{`DO $body$ BEGIN PERFORM 'a;'; END $body$`, " SELECT 2"}},
		{"case sensitive dollar tags", `SELECT $Q$one; $q$ two; $Q$; SELECT 2;`, []string{`SELECT $Q$one; $q$ two; $Q$`, " SELECT 2"}},
		{"dollar in unquoted identifier", `SELECT foo$tag$; SELECT 2;`, []string{`SELECT foo$tag$`, " SELECT 2"}},
		{"parameter is not dollar quote", `SELECT $1; SELECT $2;`, []string{`SELECT $1`, " SELECT $2"}},
		{"unicode dollar tag", `SELECT $标签$one;two$标签$; SELECT 2;`, []string{`SELECT $标签$one;two$标签$`, " SELECT 2"}},
		{"unterminated string stays intact", `SELECT 'one; SELECT 2;`, []string{`SELECT 'one; SELECT 2;`}},
		{"unterminated block stays intact", `/* one; SELECT 2;`, []string{`/* one; SELECT 2;`}},
		{"empty statements", " ; \n ; SELECT 1;; ", []string{" SELECT 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, splitSQLStatements(tc.input))
		})
	}
}

func TestStripMigrationSQLCommentsPreservesQuotedData(t *testing.T) {
	input := `/* outer; /* nested */ */ SELECT '--;/*x*/', "--column;", $$--dollar;$$, E'quote\';--value'; -- trailing;`
	want := `SELECT '--;/*x*/', "--column;", $$--dollar;$$, E'quote\';--value';`
	require.Equal(t, want, stripMigrationSQLComments(input))
	require.Equal(t, "CREATE INDEX", stripMigrationSQLComments("CREATE/* separator; */INDEX"))
	require.Empty(t, stripMigrationSQLComments("-- line;\n/* block; /* nested; */ */"))
	require.Equal(t, "/* unterminated;", stripMigrationSQLComments("/* unterminated;"), "malformed SQL must not disappear as a comment-only migration")
}

func TestMigrationProviderCostIndexesSplitIntoTwoCompleteStatements(t *testing.T) {
	raw, err := migrations.FS.ReadFile(videoProviderCostIndexesMigration)
	require.NoError(t, err)
	statements := splitSQLStatements(string(raw))
	require.Len(t, statements, 2)
	for i, statement := range statements {
		require.True(t, strings.HasPrefix(stripMigrationSQLComments(statement),
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS "+videoProviderCostIndexNames[i]), statement)
		require.True(t, strings.HasSuffix(strings.TrimSpace(statement), "'true'))"), "the full predicate must reach PostgreSQL")
	}
	nonTx, err := validateMigrationExecutionMode(videoProviderCostIndexesMigration, string(raw))
	require.NoError(t, err)
	require.True(t, nonTx)
}

func TestApplyMigrationsFS_ProviderCostIndexesPreserveCommentAndChecksum(t *testing.T) {
	raw, err := migrations.FS.ReadFile(videoProviderCostIndexesMigration)
	require.NoError(t, err)
	// These expected statements come from the two named CREATE boundaries, not
	// the splitter under test. The first must keep the semicolon in its comment.
	second := strings.Index(string(raw), "\nCREATE INDEX CONCURRENTLY IF NOT EXISTS idx_video_tasks_unknown_cost_account")
	require.Positive(t, second)
	firstSQL := strings.TrimSuffix(strings.TrimSpace(string(raw[:second])), ";")
	secondSQL := strings.TrimSuffix(strings.TrimSpace(string(raw[second:])), ";")
	require.Contains(t, firstSQL, "large; build admission")
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(raw))))

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(videoProviderCostIndexesMigration).WillReturnError(sql.ErrNoRows)
	for _, name := range videoProviderCostIndexNames {
		mock.ExpectQuery("(?s)SELECT EXISTS.*AND NOT i.indisvalid").WithArgs(name).
			WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(false))
	}
	mock.ExpectExec("^" + regexp.QuoteMeta(firstSQL) + "$").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("^" + regexp.QuoteMeta(secondSQL) + "$").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations \\(filename, checksum\\) VALUES \\(\\$1, \\$2\\)").
		WithArgs(videoProviderCostIndexesMigration, hex.EncodeToString(sum[:])).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock\\(\\$1\\)").WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	fsys := fstest.MapFS{videoProviderCostIndexesMigration: &fstest.MapFile{Data: raw}}
	require.NoError(t, applyMigrationsFS(context.Background(), db, fsys))
	require.NoError(t, mock.ExpectationsWereMet())
}
