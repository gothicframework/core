package config

import "testing"

// TestStaticFilesModeEnumOrder pins the append-only invariant of StaticFilesMode.
// These ordinals are baked into already-compiled user gothic.config.go files, so
// the existing values MUST NOT be reordered — new modes are appended only.
func TestStaticFilesModeEnumOrder(t *testing.T) {
	cases := []struct {
		name string
		got  StaticFilesMode
		want int
	}{
		{"CDN", CDN, 0},
		{"DISK", DISK, 1},
		{"EMBEDDED", EMBEDDED, 2},
	}
	for _, c := range cases {
		if int(c.got) != c.want {
			t.Errorf("%s ordinal = %d, want %d (StaticFilesMode is append-only; do not reorder)", c.name, int(c.got), c.want)
		}
	}
}

// TestStaticFilesModeZeroValueIsCDN ensures the zero value keeps the
// documented default so a project omitting ServeStaticFiles gets CDN.
func TestStaticFilesModeZeroValueIsCDN(t *testing.T) {
	var m StaticFilesMode
	if m != CDN {
		t.Errorf("zero value of StaticFilesMode = %d, want CDN (0)", m)
	}
}
