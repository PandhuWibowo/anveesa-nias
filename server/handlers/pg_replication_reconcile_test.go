package handlers

// Always-on unit tests for the pure (no-database) pieces of the Check & Sync
// safety logic: the row-fingerprint expression, the zero-overlap detector that
// drives the format-mismatch guard, and the error→HTTP-status classifier. The
// database-backed end-to-end scenarios live in
// pg_replication_reconcile_local_test.go (gated behind PGREPL_TEST=1).

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRowContentHashExpr(t *testing.T) {
	// The columns must appear in the exact order given (the caller sorts them
	// via matchedColumns) and each must be identifier-quoted, so the same
	// expression is emitted to both servers regardless of physical layout.
	got := rowContentHashExpr([]string{"a", "b", "id"})
	want := `md5(ROW("a", "b", "id")::text)`
	if got != want {
		t.Errorf("rowContentHashExpr = %q, want %q", got, want)
	}

	// A column name containing a double quote must be escaped, not allowed to
	// break out of the identifier.
	got = rowContentHashExpr([]string{`we"ird`})
	want = `md5(ROW("we""ird")::text)`
	if got != want {
		t.Errorf("rowContentHashExpr(quoted) = %q, want %q", got, want)
	}
}

func TestCountHashOverlap(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]int
		want int
	}{
		{"identical", map[string]int{"x": 1, "y": 2}, map[string]int{"x": 5, "y": 1}, 2},
		{"partial", map[string]int{"x": 1, "y": 1}, map[string]int{"y": 1, "z": 1}, 1},
		{"disjoint", map[string]int{"x": 1}, map[string]int{"y": 1}, 0},
		{"empty a", map[string]int{}, map[string]int{"y": 1}, 0},
		{"empty b", map[string]int{"x": 1}, map[string]int{}, 0},
		// A key present in b but with a zero count is not real overlap.
		{"zero count in b", map[string]int{"x": 1}, map[string]int{"x": 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countHashOverlap(c.a, c.b); got != c.want {
				t.Errorf("countHashOverlap = %d, want %d", got, c.want)
			}
		})
	}
}

func TestHttpStatusForCompareErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"column mismatch", fmt.Errorf("%w: details", errColumnMismatch), http.StatusBadRequest},
		{"format mismatch", fmt.Errorf("%w: details", errFormatMismatch), http.StatusConflict},
		{"plain error", fmt.Errorf("connection refused"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := httpStatusForCompareErr(c.err); got != c.want {
				t.Errorf("httpStatusForCompareErr = %d, want %d", got, c.want)
			}
		})
	}
}
