package drive

import "testing"

func TestExactPickerRequiresTheCompleteIdentitySet(t *testing.T) {
	want := []string{"current", "folder-a", "folder-b", "file-a", "file-b"}
	for _, test := range []struct {
		name string
		got  []string
		ok   bool
	}{
		{name: "same order", got: append([]string(nil), want...), ok: true},
		{name: "picker order", got: []string{"file-b", "folder-a", "current", "file-a", "folder-b"}, ok: true},
		{name: "missing nested file", got: []string{"current", "folder-a", "folder-b", "file-a"}},
		{name: "unrelated replacement", got: []string{"current", "folder-a", "folder-b", "file-a", "other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sameIDs(want, test.got); got != test.ok {
				t.Fatalf("sameIDs() = %v, want %v", got, test.ok)
			}
		})
	}
}
