package troubleshooter

import (
	"testing"
)

func TestRunFullDiagnostics(t *testing.T) {
	report := RunFullDiagnostics()
	if report.Timestamp == "" {
		t.Fatal("Expected timestamp to be populated")
	}
	if len(report.Checks) == 0 {
		t.Fatal("Expected checks to be populated")
	}
	if report.SummaryText == "" {
		t.Fatal("Expected summary text to be generated")
	}
	t.Log("\n" + report.SummaryText)
}

func TestFixAllIssues(t *testing.T) {
	res := FixAllIssues()
	if !res.Success {
		t.Fatal("Expected FixAllIssues to report success")
	}
	if len(res.Actions) == 0 {
		t.Fatal("Expected at least one action to be recorded")
	}
}
