package storage

import (
	"database/sql"
	_ "embed"
	"errors"
)

//go:embed migrations/013_mt5.sql
var mt5Schema string

//go:embed migrations/014_mt5_history.sql
var mt5HistorySchema string

func migrateMT5(tx *sql.Tx) error {
	// Preserve indexes and triggers on the two tables being rebuilt, including
	// application invariants added in earlier migrations.
	rows, err := tx.Query(`SELECT sql FROM sqlite_schema
		WHERE tbl_name IN ('accounts', 'servers') AND type IN ('index', 'trigger') AND sql IS NOT NULL
		ORDER BY rowid`)
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
	if _, err := tx.Exec(mt5Schema); err != nil {
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
		return errors.New("MT5 database migration violated a foreign key")
	}
	return check.Err()
}
