package app_test

import (
	"path/filepath"
	"testing"

	"portexa/tests/historymodel"
)

func TestHistoryParentAccountCompositeForeignKey(t *testing.T) {
	db, err := historymodel.Open(filepath.Join(t.TempDir(), "history.sqlite"), "../../docs/product-data-model.md")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = historymodel.Seed(db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO history_coverage(id,account_id,start_wall,end_wall,state,created_at_ms) VALUES('p','a',0,100,'pending',0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO history_coverage(id,account_id,parent_id,start_wall,end_wall,state,created_at_ms) VALUES('c','b','p',0,50,'pending',0)"); err == nil {
		t.Fatal("cross-account coverage parent accepted")
	}
	if _, err = db.Exec("INSERT INTO history_coverage(id,account_id,parent_id,start_wall,end_wall,state,created_at_ms) VALUES('c','a','p',0,50,'pending',0)"); err != nil {
		t.Fatal("same-account coverage rejected", err)
	}
}
