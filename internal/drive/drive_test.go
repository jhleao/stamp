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

func TestCredentialsUseBundledDefaultWithoutOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("STAMP_GOOGLE_OAUTH_CONFIG", "")
	info, err := Credentials()
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != CredentialDefault || info.ClientID != defaultClientID {
		t.Fatalf("unexpected credentials: %+v", info)
	}
}

func TestCredentialsPreferEnvironmentThenInstalledOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	environment := filepath.Join(t.TempDir(), "environment.json")
	installed := OverridePath()
	for path, id := range map[string]string{environment: "environment-client", installed: "installed-client"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"installed":{"client_id":"`+id+`"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("STAMP_GOOGLE_OAUTH_CONFIG", environment)
	info, err := Credentials()
	if err != nil || info.Source != CredentialEnvironment || info.ClientID != "environment-client" {
		t.Fatalf("environment credentials = %+v, %v", info, err)
	}
	t.Setenv("STAMP_GOOGLE_OAUTH_CONFIG", "")
	info, err = Credentials()
	if err != nil || info.Source != CredentialInstalled || info.ClientID != "installed-client" {
		t.Fatalf("installed credentials = %+v, %v", info, err)
	}
	if _, err := ResetConfig(); err != nil {
		t.Fatal(err)
	}
	info, err = Credentials()
	if err != nil || info.Source != CredentialDefault {
		t.Fatalf("reset credentials = %+v, %v", info, err)
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
