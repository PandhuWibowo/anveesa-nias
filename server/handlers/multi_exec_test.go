package handlers

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "two plain statements",
			in:   "SELECT 1; SELECT 2;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "trailing statement without semicolon",
			in:   "SELECT 1; SELECT 2",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "semicolon inside a single-quoted string is preserved",
			in:   "SELECT ';'; SELECT 2;",
			want: []string{"SELECT ';'", "SELECT 2"},
		},
		{
			name: "doubled single-quote escape stays inside the string",
			in:   "SELECT 'it''s a test'; SELECT 2;",
			want: []string{"SELECT 'it''s a test'", "SELECT 2"},
		},
		{
			name: "backslash-escaped quote (MySQL style) stays inside the string",
			in:   `SELECT 'it\'s a test'; SELECT 2;`,
			want: []string{`SELECT 'it\'s a test'`, "SELECT 2"},
		},
		{
			name: "semicolon inside a line comment is ignored, real semicolon still splits",
			in:   "-- note: uses a ; semicolon\nSELECT 1;\nSELECT 2;",
			want: []string{"-- note: uses a ; semicolon\nSELECT 1", "SELECT 2"},
		},
		{
			name: "semicolon inside a block comment is ignored",
			in:   "/* comment ; here */ SELECT 1; SELECT 2;",
			want: []string{"/* comment ; here */ SELECT 1", "SELECT 2"},
		},
		{
			name: "semicolons inside a bare $$ dollar-quoted block do not split it",
			in:   "DO $$ BEGIN INSERT INTO t VALUES (1); INSERT INTO t VALUES (2); END $$; SELECT 1;",
			want: []string{
				"DO $$ BEGIN INSERT INTO t VALUES (1); INSERT INTO t VALUES (2); END $$",
				"SELECT 1",
			},
		},
		{
			name: "semicolons inside a tagged $tag$ dollar-quoted block do not split it",
			in:   "CREATE FUNCTION f() RETURNS void AS $body$ BEGIN PERFORM 1; END; $body$ LANGUAGE plpgsql; SELECT 2;",
			want: []string{
				"CREATE FUNCTION f() RETURNS void AS $body$ BEGIN PERFORM 1; END; $body$ LANGUAGE plpgsql",
				"SELECT 2",
			},
		},
		{
			name: "backtick-quoted identifier containing a semicolon-like column name",
			in:   "SELECT `a;b` FROM t; SELECT 2;",
			want: []string{"SELECT `a;b` FROM t", "SELECT 2"},
		},
		{
			name: "empty statements between semicolons are dropped",
			in:   "SELECT 1;;; SELECT 2;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "whitespace-only input yields no statements",
			in:   "   \n\t  ",
			want: nil,
		},
		{
			name: "a trailing comment after the last statement is not counted as its own statement",
			in:   "SELECT 1; -- trailing note",
			want: []string{"SELECT 1"},
		},
		{
			name: "a block comment attached to a following real statement stays with it, rather than splitting off as its own phantom statement",
			in:   "SELECT 1; /* just a note */ SELECT 2;",
			want: []string{"SELECT 1", "/* just a note */ SELECT 2"},
		},
		{
			name: "a standalone comment-only segment between two real statements is dropped entirely",
			in:   "SELECT 1; /* just a note */; SELECT 2;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitStatements(%q)\n  got:  %#v\n  want: %#v", tc.in, got, tc.want)
			}
		})
	}
}
