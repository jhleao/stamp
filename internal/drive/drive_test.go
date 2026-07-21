package drive

import "testing"

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
