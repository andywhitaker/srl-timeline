package srlclient

import (
	"testing"
)

func TestParseCommitLogLine(t *testing.T) {
	// 1. Real SR Linux sr_mgmt_server EV|commitSucceeded log format
	srlLine := `2026-10-22T18:00:05.644205+00:00 srl sr_mgmt_server: mgmt|3107|3107|00033|EV|commitSucceeded|I: All changes have been committed successfully by user alice session 84.`
	entry, ok := ParseCommitLogLine(srlLine)
	if !ok {
		t.Fatalf("expected ParseCommitLogLine to match real SR Linux sr_mgmt_server log")
	}
	if entry.Username != "alice" || entry.Session != "84" {
		t.Fatalf("expected user=alice session=84, got user=%q session=%q", entry.Username, entry.Session)
	}

	// 2. Legacy/synthetic commit log format
	legacyLine := `2026-10-22T12:35:00.000Z srl1 sr_mgmt: commitSucceeded username='netops' session='77'`
	entry2, ok := ParseCommitLogLine(legacyLine)
	if !ok {
		t.Fatalf("expected ParseCommitLogLine to match legacy commit log")
	}
	if entry2.Username != "netops" || entry2.Session != "77" {
		t.Fatalf("expected user=netops session=77, got user=%q session=%q", entry2.Username, entry2.Session)
	}

	// 3. Unrelated log line
	if _, ok := ParseCommitLogLine("2026-10-22T12:36:00.000Z srl1 sshd[123]: Accepted publickey for admin"); ok {
		t.Fatalf("did not expect non-commit log line to match")
	}
}

func TestUnescapeCLIMultilineCommand(t *testing.T) {
	in := `set / system tls profile p1 certificate "-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n"`
	got := unescapeCLIMultilineCommand(in)
	want := "set / system tls profile p1 certificate \"-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n\""
	if got != want {
		t.Fatalf("unescapeCLIMultilineCommand mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestParseLatestCommitState(t *testing.T) {
	raw := map[string]interface{}{
		"commit": []interface{}{
			map[string]interface{}{
				"id":       float64(1),
				"username": "__internal__",
				"ended":    "2026-10-22T20:38:54.941Z (16 minutes ago)",
			},
			map[string]interface{}{
				"id":       float64(5),
				"username": "admin",
				"comment":  "configure uplink port",
				"ended":    "2026-10-22T20:45:12.100Z (10 minutes ago)",
			},
		},
	}
	logEntry, ok := parseLatestCommitState(raw)
	if !ok {
		t.Fatalf("expected parseLatestCommitState to succeed")
	}
	if logEntry.CommitID != "5" || logEntry.Username != "admin" || logEntry.Comment != "configure uplink port" || logEntry.Timestamp != "2026-10-22T20:45:12.100Z" {
		t.Fatalf("unexpected parsed commit state: %+v", logEntry)
	}
}

