// Package historymodel runs the documented future-history prototype with the
// production driver. It is test tooling, never a production migration.
package historymodel

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite"
	"portexa/internal/storage"
)

func Open(path, document string) (*sql.DB, error) {
	data, err := os.ReadFile(document)
	if err != nil {
		return nil, err
	}
	_, sqlText, ok := strings.Cut(string(data), "```sql\n")
	if !ok {
		return nil, errors.New("schema SQL is missing")
	}
	sqlText, _, ok = strings.Cut(sqlText, "\n```")
	if !ok {
		return nil, errors.New("schema SQL is incomplete")
	}
	u := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	for _, value := range []string{"foreign_keys(1)", "synchronous(FULL)", "busy_timeout(1000)", "wal_autocheckpoint(1000)"} {
		q.Add("_pragma", value)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version, mode string
	if err = db.QueryRow("SELECT sqlite_version()").Scan(&version); err == nil && !storage.SupportedVersion(version) {
		err = errors.New("unsupported SQLite")
	}
	if err == nil {
		err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode)
	}
	if err == nil && mode != "wal" {
		err = errors.New("WAL not enabled")
	}
	if err == nil {
		_, err = db.Exec(sqlText)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Seed(db *sql.DB) error {
	_, err := db.Exec(`
INSERT INTO servers(id,protocol,broker_name,server_name,created_at_ms) VALUES('s','mt4','Synthetic','Synthetic-Performance',0);
INSERT INTO server_endpoints(id,server_id,revision,ip_address,port,state,created_at_ms) VALUES('e','s',1,'203.0.113.9',443,'pending',0);
INSERT INTO accounts(id,server_id,login,label,credential_envelope,credential_key_id,credential_version,created_at_ms)
VALUES('a','s',1,'Synthetic',X'00','synthetic',1,0),('b','s',2,'Synthetic',X'00','synthetic',1,0);`)
	return err
}
