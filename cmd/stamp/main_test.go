package main

import (
	"strings"
	"testing"
)

func TestParseArgsRejectsUnknownOptions(t *testing.T) {
	_, _, _, err := parseArgs([]string{"--mesage", "typo"}, []string{"message"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown option --mesage") {
		t.Fatalf("expected a useful unknown-option error, got %v", err)
	}
}

func TestCommandKeepsUsefulOptionError(t *testing.T) {
	err := run([]string{"project", "create", "somewhere", "--mesage", "typo"})
	if err == nil || err.Error() != "unknown option --mesage" {
		t.Fatalf("error = %v", err)
	}
}

func TestParseArgsSeparatesValuesFlagsAndPositionals(t *testing.T) {
	pos, values, flags, err := parseArgs(
		[]string{"report", "--dir", "work", "--replace"},
		[]string{"dir"}, []string{"replace"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 || pos[0] != "report" || values["dir"] != "work" || !flags["replace"] {
		t.Fatalf("unexpected parse result: pos=%v values=%v flags=%v", pos, values, flags)
	}
}
