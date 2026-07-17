package steps

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestEscapeLikePattern(t *testing.T) {
	if got, want := EscapeLikePattern("dashboard_top_provider_test"), `dashboard\_top\_provider\_test`; got != want {
		t.Fatalf("EscapeLikePattern() = %q, want %q", got, want)
	}
	if got, want := EscapeLikePattern(`100%_a\b`), `100\%\_a\\b`; got != want {
		t.Fatalf("EscapeLikePattern() = %q, want %q", got, want)
	}
}

func TestListDatabases_RowsCloseErrorSurfaces(t *testing.T) {
	closeErr := errors.New("close rows")
	rows := &fakeDatabaseRows{names: []string{"b", "a"}, closeErr: closeErr}
	names, err := collectDatabaseNames(rows)
	if !errors.Is(err, closeErr) {
		t.Fatalf("collectDatabaseNames() error = %v, want close error", err)
	}
	if strings.Join(names, ",") != "a,b" {
		t.Fatalf("collectDatabaseNames() names = %v, want sorted names", names)
	}
}

func TestListDatabases_RowErrAndCloseErrorBothReported(t *testing.T) {
	rowErr := errors.New("iterate rows")
	closeErr := errors.New("close rows")
	rows := &fakeDatabaseRows{rowErr: rowErr, closeErr: closeErr}
	_, err := collectDatabaseNames(rows)
	if !errors.Is(err, rowErr) || !errors.Is(err, closeErr) {
		t.Fatalf("collectDatabaseNames() error = %v, want both row and close errors", err)
	}
	message := err.Error()
	if strings.Index(message, rowErr.Error()) > strings.Index(message, closeErr.Error()) {
		t.Fatalf("row error must precede close error: %s", message)
	}
}

func TestDatabaseListingsUseBoundPatternParameters(t *testing.T) {
	for _, query := range []string{mysqlListDatabasesQuery, postgresListDatabasesQuery} {
		t.Run(query, func(t *testing.T) {
			queryErr := errors.New("stop after recording query")
			recorder := &recordingDatabaseQueryer{err: queryErr}
			_, err := queryDatabaseNames(recorder, query, `unsafe%'_pattern`)
			if !errors.Is(err, queryErr) {
				t.Fatalf("queryDatabaseNames() error = %v, want recorder error", err)
			}
			if recorder.query != query {
				t.Fatalf("query = %q, want %q", recorder.query, query)
			}
			if len(recorder.args) != 1 || recorder.args[0] != `unsafe%'_pattern` {
				t.Fatalf("query args = %#v, want one bound pattern", recorder.args)
			}
			if strings.Contains(recorder.query, `unsafe%'_pattern`) {
				t.Fatalf("pattern was interpolated into query: %s", recorder.query)
			}
		})
	}
}

type fakeDatabaseRows struct {
	names    []string
	index    int
	rowErr   error
	closeErr error
}

func (r *fakeDatabaseRows) Next() bool {
	if r.index >= len(r.names) {
		return false
	}
	r.index++
	return true
}

func (r *fakeDatabaseRows) Scan(dest ...any) error {
	value, ok := dest[0].(*string)
	if !ok {
		return errors.New("unexpected scan destination")
	}
	*value = r.names[r.index-1]
	return nil
}

func (r *fakeDatabaseRows) Err() error {
	return r.rowErr
}

func (r *fakeDatabaseRows) Close() error {
	return r.closeErr
}

type recordingDatabaseQueryer struct {
	query string
	args  []any
	err   error
}

func (q *recordingDatabaseQueryer) Query(query string, args ...any) (*sql.Rows, error) {
	q.query = query
	q.args = append([]any(nil), args...)
	return nil, q.err
}
