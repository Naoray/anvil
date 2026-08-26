package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalState_OwnedDatabasesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := LocalState{
		DbSuffix: "top_provider",
		Databases: []OwnedDatabase{
			{Name: "dashboard_top_provider", Engine: "mysql", Role: DbRoleApplication},
			{Name: "dashboard_top_provider_test", Engine: "mysql", Role: DbRoleTesting},
		},
	}
	if err := WriteLocalState(dir, want); err != nil {
		t.Fatalf("WriteLocalState() error = %v", err)
	}

	got, err := ReadLocalState(dir)
	if err != nil {
		t.Fatalf("ReadLocalState() error = %v", err)
	}
	if got.DbSuffix != want.DbSuffix {
		t.Fatalf("DbSuffix = %q, want %q", got.DbSuffix, want.DbSuffix)
	}
	if len(got.Databases) != len(want.Databases) {
		t.Fatalf("Databases length = %d, want %d", len(got.Databases), len(want.Databases))
	}
	for i := range want.Databases {
		if got.Databases[i] != want.Databases[i] {
			t.Fatalf("Databases[%d] = %#v, want %#v", i, got.Databases[i], want.Databases[i])
		}
	}
}

func TestReadLocalState_LegacySuffixOnly(t *testing.T) {
	dir := t.TempDir()
	writeTestLocalState(t, dir, "db_suffix: top_provider\n")

	state, err := ReadLocalState(dir)
	if err != nil {
		t.Fatalf("ReadLocalState() error = %v", err)
	}
	if state.DbSuffix != "top_provider" || len(state.Databases) != 0 {
		t.Fatalf("legacy state = %#v", state)
	}
}

func TestWriteLocalState_CanonicalReplaceByRole(t *testing.T) {
	dir := t.TempDir()
	if err := WriteLocalState(dir, LocalState{
		DbSuffix: "top_provider",
		Databases: []OwnedDatabase{
			{Name: "app_old", Engine: "pgsql", Role: DbRoleApplication},
			{Name: "app_old_test", Engine: "pgsql", Role: DbRoleTesting},
		},
	}); err != nil {
		t.Fatalf("initial WriteLocalState() error = %v", err)
	}
	if err := WriteLocalState(dir, LocalState{Databases: []OwnedDatabase{
		{Name: "app_new", Engine: "pgsql", Role: DbRoleApplication},
		{Name: "app_new_test", Engine: "pgsql", Role: DbRoleTesting},
	}}); err != nil {
		t.Fatalf("replacement WriteLocalState() error = %v", err)
	}

	state, err := ReadLocalState(dir)
	if err != nil {
		t.Fatalf("ReadLocalState() error = %v", err)
	}
	if state.DbSuffix != "top_provider" {
		t.Fatalf("DbSuffix = %q, want top_provider", state.DbSuffix)
	}
	if len(state.Databases) != 2 {
		t.Fatalf("Databases = %#v, want two canonical records", state.Databases)
	}
	if state.Databases[0].Name != "app_new" || state.Databases[1].Name != "app_new_test" {
		t.Fatalf("Databases = %#v, want replacement names", state.Databases)
	}
}

func TestWriteLocalState_PrunesDuplicateCanonicalRoles(t *testing.T) {
	dir := t.TempDir()
	writeTestLocalState(t, dir, "db_suffix: top_provider\ndatabases:\n  - name: first_test\n    engine: mysql\n    role: testing\n  - name: stale_test\n    engine: mysql\n    role: testing\n")

	if err := WriteLocalState(dir, LocalState{Databases: []OwnedDatabase{{
		Name: "canonical_test", Engine: "mysql", Role: DbRoleTesting,
	}}}); err != nil {
		t.Fatalf("WriteLocalState() error = %v", err)
	}
	state, err := ReadLocalState(dir)
	if err != nil {
		t.Fatalf("ReadLocalState() error = %v", err)
	}
	if len(state.Databases) != 1 || state.Databases[0].Name != "canonical_test" {
		t.Fatalf("Databases = %#v, want one canonical testing record", state.Databases)
	}
}

func TestWriteLocalState_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	seed := "db_suffix: top_provider\ncustom_top: keep\ndatabases:\n  - name: app_old\n    engine: pgsql\n    role: application\n    custom_rec: keep-too\n  - name: weird\n    engine: pgsql\n    role: someday-role\n"
	writeTestLocalState(t, dir, seed)

	if err := WriteLocalState(dir, LocalState{Databases: []OwnedDatabase{{
		Name: "app_new", Engine: "pgsql", Role: DbRoleApplication,
	}}}); err != nil {
		t.Fatalf("WriteLocalState() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, LocalStateFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, want := range []string{"custom_top: keep", "custom_rec: keep-too", "someday-role", "app_new"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("rewritten state missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "app_old") {
		t.Fatalf("rewritten state retained replaced name:\n%s", raw)
	}
}

func TestWriteLocalState_MalformedDatabasesIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LocalStateFile)
	before := []byte("db_suffix: top_provider\ndatabases: not-a-list\n")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := WriteLocalState(dir, LocalState{Databases: []OwnedDatabase{{
		Name: "app_new", Engine: "mysql", Role: DbRoleApplication,
	}}})
	if err == nil {
		t.Fatal("WriteLocalState() error = nil, want malformed databases error")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(after) != string(before) {
		t.Fatalf("malformed file changed:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestIsValidDatabaseIdentifier(t *testing.T) {
	for _, valid := range []string{"a", "a_B9", "DATABASE_123"} {
		if !IsValidDatabaseIdentifier(valid) {
			t.Errorf("IsValidDatabaseIdentifier(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{"", "a-b", "a;drop", "a b", "a%b"} {
		if IsValidDatabaseIdentifier(invalid) {
			t.Errorf("IsValidDatabaseIdentifier(%q) = true, want false", invalid)
		}
	}
}

func TestValidateOwnedDatabases_ValidSingleEngine(t *testing.T) {
	err := ValidateOwnedDatabases([]OwnedDatabase{
		{Name: "app", Engine: "mysql", Role: DbRoleApplication},
		{Name: "app_test", Engine: "mysql", Role: DbRoleTesting},
	})
	if err != nil {
		t.Fatalf("ValidateOwnedDatabases() error = %v", err)
	}
}

func TestValidateOwnedDatabases_RejectsDuplicateCardinalityInStateOrder(t *testing.T) {
	err := ValidateOwnedDatabases([]OwnedDatabase{
		{Name: "bad;first", Engine: "mysql", Role: DbRoleApplication},
		{Name: "app_second", Engine: "mysql", Role: DbRoleApplication},
		{Name: "bad;first", Engine: "oracle", Role: DbRoleTesting},
		{Name: "test_second", Engine: "pgsql", Role: DbRoleTesting},
	})
	if err == nil {
		t.Fatal("ValidateOwnedDatabases() error = nil")
	}
	want := strings.Join([]string{
		`invalid database name "bad;first" in record 0`,
		`duplicate database role "application" in record 1; first seen in record 0`,
		`invalid database name "bad;first" in record 2`,
		`duplicate database name "bad;first" in record 2; first seen in record 0`,
		`unsupported database engine "oracle" in record "bad;first"; supported engines: mysql, pgsql`,
		`duplicate database role "testing" in record 3; first seen in record 2`,
		`database records must use exactly one supported engine; found mysql, pgsql`,
	}, "\n")
	if got := err.Error(); got != want {
		t.Fatalf("ValidateOwnedDatabases() error:\n%s\nwant:\n%s", got, want)
	}
}

func TestValidateOwnedDatabases_RejectsUnsafeRecords(t *testing.T) {
	tests := []struct {
		name string
		dbs  []OwnedDatabase
		want string
	}{
		{"unknown role", []OwnedDatabase{{Name: "app", Engine: "mysql", Role: "someday-role"}}, "unsupported database record role"},
		{"unknown engine", []OwnedDatabase{{Name: "app", Engine: "oracle", Role: DbRoleApplication}}, "unsupported database engine"},
		{"sqlite engine", []OwnedDatabase{{Name: "app", Engine: "sqlite", Role: DbRoleApplication}}, "unsupported database engine"},
		{"mixed supported engines", []OwnedDatabase{
			{Name: "app", Engine: "mysql", Role: DbRoleApplication},
			{Name: "app_test", Engine: "pgsql", Role: DbRoleTesting},
		}, "exactly one supported engine"},
		{"invalid name", []OwnedDatabase{{Name: "bad;drop", Engine: "mysql", Role: DbRoleApplication}}, "invalid database name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOwnedDatabases(tt.dbs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateOwnedDatabases() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateOwnedDatabases_AggregatesInStateOrder(t *testing.T) {
	err := ValidateOwnedDatabases([]OwnedDatabase{
		{Name: "bad;first", Engine: "mysql", Role: "someday-role"},
		{Name: "valid_second", Engine: "oracle", Role: DbRoleTesting},
		{Name: "valid_third", Engine: "pgsql", Role: DbRoleApplication},
	})
	if err == nil {
		t.Fatal("ValidateOwnedDatabases() error = nil")
	}
	message := err.Error()
	for _, want := range []string{"someday-role", "oracle"} {
		if !strings.Contains(message, want) {
			t.Fatalf("aggregate missing %q: %s", want, message)
		}
	}
	wants := []string{"bad;first", "valid_second", "exactly one supported engine"}
	last := -1
	for _, want := range wants {
		idx := strings.Index(message, want)
		if idx < 0 {
			t.Fatalf("aggregate missing %q: %s", want, message)
		}
		if idx < last {
			t.Fatalf("aggregate order is not deterministic for %q: %s", want, message)
		}
		last = idx
	}
}

func TestDbCreateConfig_RoleValidation(t *testing.T) {
	for _, role := range []string{"", DbRoleApplication, DbRoleTesting} {
		if err := ValidateStepConfig(StepDbCreate, StepConfig{Role: role}); err != nil {
			t.Errorf("role %q rejected: %v", role, err)
		}
	}
	if err := ValidateStepConfig(StepDbCreate, StepConfig{Role: "staging"}); err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("invalid role error = %v", err)
	}
}

func TestLoadProject_DbCreateTestingRole(t *testing.T) {
	dir := t.TempDir()
	content := "scaffold:\n  steps:\n    - name: db.create\n      role: testing\n"
	if err := os.WriteFile(filepath.Join(dir, ProjectConfigFile), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}
	if len(cfg.Scaffold.Steps) != 1 || cfg.Scaffold.Steps[0].Role != DbRoleTesting {
		t.Fatalf("loaded steps = %#v", cfg.Scaffold.Steps)
	}
	if err := ValidateStepConfig(cfg.Scaffold.Steps[0].Name, cfg.Scaffold.Steps[0]); err != nil {
		t.Fatalf("loaded testing step failed validation: %v", err)
	}
}

func writeTestLocalState(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, LocalStateFile), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
