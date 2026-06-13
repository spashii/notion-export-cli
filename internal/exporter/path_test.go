package exporter

import "testing"

func TestSanitizeName(t *testing.T) {
	got := sanitizeName("  Roadmap/Q2: Launch\nPlan  ")
	if got != "Roadmap Q2 Launch Plan" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
}

func TestSanitizeNameFallback(t *testing.T) {
	got := sanitizeName("/::")
	if got != "Untitled" {
		t.Fatalf("unexpected sanitized name: %q", got)
	}
}
