package steps

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLClient_CreateDatabaseExistingReturnsDatabaseExistsError(t *testing.T) {
	connector := &execErrorConnector{
		err: &mysql.MySQLError{Number: 1007, Message: "create rejected"},
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	client := &MySQLClient{db: db}

	err := client.CreateDatabase("dashboard_collision")
	var existsErr *DatabaseExistsError
	if !errors.As(err, &existsErr) {
		t.Fatalf("CreateDatabase() error = %T %v, want DatabaseExistsError", err, err)
	}
	if existsErr.Name != "dashboard_collision" {
		t.Fatalf("DatabaseExistsError.Name = %q, want %q", existsErr.Name, "dashboard_collision")
	}
	if got, want := connector.query, "CREATE DATABASE `dashboard_collision`"; got != want {
		t.Fatalf("CreateDatabase() query = %q, want %q", got, want)
	}
}

func TestMySQLClient_CreateDatabaseSuccess(t *testing.T) {
	connector := &execErrorConnector{}
	db := sql.OpenDB(connector)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	client := &MySQLClient{db: db}

	if err := client.CreateDatabase("dashboard_created"); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	if got, want := connector.query, "CREATE DATABASE `dashboard_created`"; got != want {
		t.Fatalf("CreateDatabase() query = %q, want %q", got, want)
	}
}

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

type execErrorConnector struct {
	err     error
	errors  []error
	query   string
	queries []string
}

func (c *execErrorConnector) Connect(context.Context) (driver.Conn, error) {
	return &execErrorConn{connector: c}, nil
}

func (c *execErrorConnector) Driver() driver.Driver {
	return execErrorDriver{}
}

type execErrorDriver struct{}

func (execErrorDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type execErrorConn struct {
	connector *execErrorConnector
}

func (c *execErrorConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *execErrorConn) Close() error {
	return nil
}

func (c *execErrorConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

func (c *execErrorConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.connector.query = query
	c.connector.queries = append(c.connector.queries, query)
	if len(c.connector.errors) == 0 {
		return nil, c.connector.err
	}
	err := c.connector.errors[0]
	c.connector.errors = c.connector.errors[1:]
	return nil, err
}
