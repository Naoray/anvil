package config

import (
	"fmt"
	"strings"
)

// databaseEngineAliases is the single table of database names accepted from a
// db.create/db.destroy `type` or a .env DB_CONNECTION value.
var databaseEngineAliases = map[string]DatabaseEngine{
	"mysql":      DBEngineMySQL,
	"mariadb":    DBEngineMySQL,
	"pgsql":      DBEnginePgSQL,
	"postgres":   DBEnginePgSQL,
	"postgresql": DBEnginePgSQL,
	"sqlite":     DBEngineSQLite,
}

// DatabaseConnectionEngine resolves a Laravel DB_CONNECTION value. Matching is
// exact: an unrecognised connection means no engine is configured.
func DatabaseConnectionEngine(connection string) (DatabaseEngine, bool) {
	engine, ok := databaseEngineAliases[connection]
	return engine, ok
}

// ParseDatabaseType resolves an explicit step `type` case-insensitively.
// Unknown values are configuration errors.
func ParseDatabaseType(value string) (DatabaseEngine, error) {
	if engine, ok := databaseEngineAliases[strings.ToLower(value)]; ok {
		return engine, nil
	}
	return "", fmt.Errorf("unsupported database type: %s", value)
}

// IsMariaDBType reports whether a step `type` or DB_CONNECTION selects the
// MariaDB service rather than MySQL.
func IsMariaDBType(value string) bool {
	return strings.EqualFold(value, string(DBEngineMariaDB))
}
