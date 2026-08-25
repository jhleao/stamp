package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifiedUpdateReplacesExecutable(t *testing.T) {
	archive := testArchive(t, "#!/bin/sh\necho 9.8.7\n")
	digest := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksums.txt" {
			fmt.Fprintf(w, "%s  ./stamp_9.8.7_darwin_arm64.tar.gz\n", hex.EncodeToString(digest[:]))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	directory := t.TempDir()
	executable := filepath.Join(directory, "stamp")
	if err := os.WriteFile(executable, []byte("old version"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := Release{
		Version:   "9.8.7",
		AssetName: "stamp_9.8.7_darwin_arm64.tar.gz",
		AssetURL:  server.URL + "/archive",
		Checksums: server.URL + "/checksums.txt",
	}
	if err := install(context.Background(), server.Client(), release, executable); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "#!/bin/sh\necho 9.8.7\n" {
		t.Fatalf("installed unexpected binary: %q", installed)
	}
	if _, err := os.Stat(executable + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("previous binary was not cleaned up: %v", err)
	}
}

func TestChecksumFailurePreservesExecutable(t *testing.T) {
	archive := testArchive(t, "#!/bin/sh\necho 9.8.7\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksums.txt" {
			fmt.Fprintf(w, "%064x  stamp_9.8.7_darwin_arm64.tar.gz\n", 0)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	executable := filepath.Join(t.TempDir(), "stamp")
	if err := os.WriteFile(executable, []byte("old version"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := Release{
		Version:   "9.8.7",
		AssetName: "stamp_9.8.7_darwin_arm64.tar.gz",
		AssetURL:  server.URL + "/archive",
		Checksums: server.URL + "/checksums.txt",
	}
	if err := install(context.Background(), server.Client(), release, executable); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
	installed, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != "old version" {
		t.Fatalf("existing executable changed after failed verification: %q", installed)
	}
}

func TestReleaseVersionComparisonRejectsNonReleaseBuilds(t *testing.T) {
	for _, version := range []string{"dev", "2.1.0-dirty", "2.1", "2.1.-"} {
		if isReleaseVersion(version) {
			t.Fatalf("accepted non-release version %q", version)
		}
	}
	if compareVersions("2.10.0", "2.9.9") <= 0 || compareVersions("2.1.0", "2.1.0") != 0 {
		t.Fatal("semantic version ordering is incorrect")
	}
}

func testArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var archive bytes.Buffer
	compressed := gzip.NewWriter(&archive)
	writer := tar.NewWriter(compressed)
	data := []byte(binary)
	if err := writer.WriteHeader(&tar.Header{Name: "stamp_9.8.7_darwin_arm64/stamp", Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}
