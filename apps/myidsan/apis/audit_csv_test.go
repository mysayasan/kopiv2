package apis

import "testing"

// The audit CSV carries attacker-influenced text: a failed-login "username" and a
// User-Agent are whatever the caller sent. Excel and Sheets evaluate a cell beginning with
// = + - @ (or a leading tab/CR) as a formula, so exporting one verbatim turns a compliance
// report into code execution on the reviewer's machine.
func TestCsvSafeDefusesFormulaInjection(t *testing.T) {
	hostile := []string{
		`=cmd|'/c calc'!A1`,
		`+1+1`,
		`-1+1`,
		`@SUM(1:9)`,
		"\tleading-tab",
		"\rleading-cr",
	}
	for _, raw := range hostile {
		got := csvSafe(raw)
		if got == raw {
			t.Errorf("csvSafe(%q) returned it unchanged — a spreadsheet would evaluate it", raw)
			continue
		}
		if got[0] != '\'' {
			t.Errorf("csvSafe(%q) = %q, want a leading apostrophe to force a literal cell", raw, got)
		}
	}
}

// Ordinary values must pass through untouched: quoting everything would corrupt the
// export for the humans reading it.
func TestCsvSafeLeavesOrdinaryValuesAlone(t *testing.T) {
	safe := []string{
		"", "alice@corp.local", "login.success", "192.168.1.10",
		"Mozilla/5.0 (Windows NT 10.0)", `{"method":"local"}`, "user 1",
	}
	for _, raw := range safe {
		if got := csvSafe(raw); got != raw {
			t.Errorf("csvSafe(%q) = %q, want it unchanged", raw, got)
		}
	}
}
