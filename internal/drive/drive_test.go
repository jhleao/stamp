package drive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestID(t *testing.T) {
	for input, want := range map[string]string{
		"abc123": "abc123",
		"https://drive.google.com/drive/folders/abc": "abc",
		"https://drive.google.com/file/d/xyz/view":   "xyz",
		"https://drive.google.com/open?id=old":       "old",
	} {
		if got := ID(input); got != want {
			t.Errorf("ID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInstallConfig(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "download.json")
	destination := filepath.Join(dir, "Stamp", "google-oauth.json")
	data := []byte(`{"installed":{"client_id":"client","client_secret":"secret"}}`)
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STAMP_GOOGLE_OAUTH_CONFIG", destination)
	got, err := InstallConfig(source)
	if err != nil {
		t.Fatal(err)
	}
	if got != destination {
		t.Fatalf("destination = %q, want %q", got, destination)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
}
