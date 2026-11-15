package handlers

import (
	"errors"
	"strings"
	"testing"
)

// fakeRows is the smallest thing that satisfies rowsScanner, so the
// row-reading path can be driven without a database. `scanErr` fires on
// the 1-based row number `scanAt`; `err` is what Err() reports after the
// loop.
type fakeRows struct {
	columns []string
	rows    [][]interface{}
	scanErr error
	scanAt  int
	err     error
	i       int
	current []interface{}
}

func (f *fakeRows) Columns() ([]string, error) { return f.columns, nil }

func (f *fakeRows) Next() bool {
	if f.i >= len(f.rows) {
		return false
	}
	f.current = f.rows[f.i]
	f.i++
	return true
}

func (f *fakeRows) Scan(dest ...interface{}) error {
	if f.scanErr != nil && f.i == f.scanAt {
		return f.scanErr
	}
	for i := range dest {
		if i >= len(f.current) {
			break
		}
		spot, ok := dest[i].(*interface{})
		if !ok {
			continue
		}
		*spot = f.current[i]
	}
	return nil
}

func (f *fakeRows) Err() error   { return f.err }
func (f *fakeRows) Close() error { return nil }

func TestScanRowsConvertsBytesAndKeepsOrder(t *testing.T) {
	rows := &fakeRows{
		columns: []string{"id", "name"},
		rows: [][]interface{}{
			{[]byte("1"), "ana"},
			{[]byte("2"), "bo"},
		},
	}

	data, err := scanRows(rows, rows.columns)
	if err != nil {
		t.Fatalf("scanRows returned %v, want nil", err)
	}
	if len(data) != 2 {
		t.Fatalf("got %d rows, want 2", len(data))
	}
	if data[0][0] != "1" || data[0][1] != "ana" {
		t.Fatalf("first row = %#v, want [\"1\" \"ana\"]", data[0])
	}
	if data[1][0] != "2" || data[1][1] != "bo" {
		t.Fatalf("second row = %#v, want [\"2\" \"bo\"]", data[1])
	}
}

func TestScanRowsReportsARowThatWillNotScan(t *testing.T) {
	rows := &fakeRows{
		columns: []string{"id"},
		rows:    [][]interface{}{{1}, {2}},
		scanErr: errors.New("sql: Scan error on column index 0"),
		scanAt:  1,
	}

	data, err := scanRows(rows, rows.columns)
	if err == nil {
		t.Fatalf("scanRows returned rows %#v and no error, want the scan error", data)
	}
	if !strings.Contains(err.Error(), "read row 1") {
		t.Fatalf("error = %q, want it to name the failing row", err.Error())
	}
	if data != nil {
		t.Fatalf("scanRows returned %#v alongside the error, want nil so a partial result cannot be served", data)
	}
}

func TestScanRowsReportsAnErrorAfterTheLoop(t *testing.T) {
	rows := &fakeRows{
		columns: []string{"id"},
		rows:    [][]interface{}{{1}},
		err:     errors.New("driver: bad connection"),
	}

	data, err := scanRows(rows, rows.columns)
	if err == nil {
		t.Fatalf("scanRows returned rows %#v and no error, want the driver error", data)
	}
	if !strings.Contains(err.Error(), "read result set") {
		t.Fatalf("error = %q, want it to say the result set failed", err.Error())
	}
	if data != nil {
		t.Fatalf("scanRows returned %#v alongside the error, want nil", data)
	}
}
