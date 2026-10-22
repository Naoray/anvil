package config

import (
	"strings"
	"testing"
)

func TestParseDatabaseType(t *testing.T) {
	cases := map[string]DatabaseEngine{
		"mysql": DBEngineMySQL, "MySQL": DBEngineMySQL, "mariadb": DBEngineMySQL, "MariaDB": DBEngineMySQL,
		"pgsql": DBEnginePgSQL, "postgres": DBEnginePgSQL, "PostgreSQL": DBEnginePgSQL,
		"sqlite": DBEngineSQLite, "SQLite": DBEngineSQLite,
	}
	for value, want := range cases {
		got, err := ParseDatabaseType(value)
		if err != nil || got != want {
			t.Errorf("ParseDatabaseType(%q) = %q, %v; want %q", value, got, err, want)
		}
	}
	for _, value := range []string{"mssql", "sqlsrv", "my sql"} {
		if _, err := ParseDatabaseType(value); err == nil || !strings.Contains(err.Error(), "unsupported database type") {
			t.Errorf("ParseDatabaseType(%q) error = %v, want unsupported database type", value, err)
		}
	}
}

func TestDatabaseConnectionEngineIsExact(t *testing.T) {
	if engine, ok := DatabaseConnectionEngine("postgresql"); !ok || engine != DBEnginePgSQL {
		t.Fatalf("postgresql = %q, %v", engine, ok)
	}
	if _, ok := DatabaseConnectionEngine("sqlsrv"); ok {
		t.Fatal("sqlsrv must not resolve to an engine")
	}
}

func TestValidateDatabaseStepType(t *testing.T) {
	for _, step := range []string{StepDbCreate, StepDbDestroy} {
		for _, value := range []string{"", "postgres", "MySQL", "mariadb", "sqlite"} {
			if err := ValidateStepConfig(step, StepConfig{Type: value}); err != nil {
				t.Errorf("%s type %q: unexpected error %v", step, value, err)
			}
		}
		err := ValidateStepConfig(step, StepConfig{Type: "mssql"})
		if err == nil || !strings.Contains(err.Error(), step+": unsupported database type: mssql") {
			t.Errorf("%s type mssql: error = %v", step, err)
		}
	}
}
