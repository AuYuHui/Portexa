package storage

import (
	"database/sql"
	_ "embed"
	"errors"
	"strings"
)

//go:embed migrations/013_mt5.sql
var mt5Schema string

//go:embed migrations/014_mt5_history.sql
var mt5HistorySchema string

//go:embed migrations/015_mt5_native_only.sql
var mt5NativeOnlySchema string

func rebuildTables(tx *sql.Tx, schema string, tables ...string) error {
	// Preserve explicit indexes and triggers added in earlier migrations.
	params := make([]any, len(tables))
	for i, name := range tables {
		params[i] = name
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tables)), ",")
	rows, err := tx.Query(`SELECT sql FROM sqlite_schema
		WHERE tbl_name IN (`+placeholders+`) AND type IN ('index', 'trigger') AND sql IS NOT NULL
		ORDER BY rowid`, params...)
	if err != nil {
		return err
	}
	var objects []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, statement)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	for _, statement := range objects {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func checkForeignKeys(tx *sql.Tx) error {
	check, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer check.Close()
	if check.Next() {
		return errors.New("database migration violated a foreign key")
	}
	return check.Err()
}
