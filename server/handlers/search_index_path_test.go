package handlers

import "testing"

func TestSearchIndexExpressionPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"filebeat-*", "filebeat-*"},
		{".ds-filebeat-*", ".ds-filebeat-*"},
		{"filebeat-*,.ds-filebeat-*", "filebeat-*,.ds-filebeat-*"},
		{"logs-2026.05.18", "logs-2026.05.18"},
		{"  filebeat-* , logs-*  ", "filebeat-*,logs-*"},
	}
	for _, tc := range tests {
		if got := searchIndexExpressionPath(tc.in); got != tc.want {
			t.Errorf("searchIndexExpressionPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
