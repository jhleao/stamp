package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSkillPrintsCanonicalProjectGuide(t *testing.T) {
	output, err := captureStdout(func() error { return run([]string{"skill"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "# Working with this Stamp project") ||
		!strings.Contains(output, "inspect theme/README.md, theme/components/") ||
		!strings.Contains(output, "Reuse existing components") || strings.Contains(output, "MCP") {
		t.Fatalf("unexpected skill output: %q", output)
	}
}

func TestTutorialStartsAtZeroAndEndsWithCollaboration(t *testing.T) {
	output, err := captureStdout(func() error { return run([]string{"tutorial"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Stamp quickstart", "stamp doctor", "stamp login", "stamp new", "stamp studio", "robot button", "top right", "stamp skill", "stamp pull", "stamp push", "Add a colleague", "stamp clone"} {
		if !strings.Contains(output, want) {
			t.Fatalf("tutorial missing %q: %s", want, output)
		}
	}
}

func captureStdout(run func() error) (string, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return "", err
	}
	old := os.Stdout
	os.Stdout = write
	err = run()
	_ = write.Close()
	os.Stdout = old
	var output bytes.Buffer
	_, readErr := io.Copy(&output, read)
	_ = read.Close()
	if err != nil || readErr != nil {
		if err != nil {
			return "", err
		}
		return "", readErr
	}
	return output.String(), nil
}

func TestParseArgsRejectsUnknownOptions(t *testing.T) {
	_, _, _, err := parseArgs([]string{"--mesage", "typo"}, []string{"message"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown option --mesage") {
		t.Fatalf("expected a useful unknown-option error, got %v", err)
	}
}

func TestCommandKeepsUsefulOptionError(t *testing.T) {
	err := run([]string{"new", "somewhere", "--mesage", "typo"})
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
