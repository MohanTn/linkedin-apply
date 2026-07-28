package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "Chromium runs but no window appears" is normally the container drawing on a
// display the monitor is not showing, so the summary must name both the selected
// display and every reachable one.
func TestDisplayInfo_ListsReachableDisplays(t *testing.T) {
	dir := withSocketDir(t)
	for _, s := range []string{"X0", "X1"} {
		if err := os.WriteFile(filepath.Join(dir, s), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DISPLAY", ":0")

	got := DisplayInfo()
	for _, want := range []string{"DISPLAY=:0", ":0", ":1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("info=%q, missing %q", got, want)
		}
	}
	// More than one display is exactly the ambiguous case worth calling out.
	if !strings.Contains(got, "does not appear") {
		t.Fatalf("info=%q, want a hint about picking the wrong display", got)
	}
}

func TestDisplayInfo_SingleDisplayHasNoWarning(t *testing.T) {
	dir := withSocketDir(t)
	if err := os.WriteFile(filepath.Join(dir, "X0"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":0")

	if got := DisplayInfo(); strings.Contains(got, "does not appear") {
		t.Fatalf("info=%q, should not warn when only one display exists", got)
	}
}

func TestDisplayInfo_NoDisplays(t *testing.T) {
	withSocketDir(t)
	t.Setenv("DISPLAY", ":0")

	if got := DisplayInfo(); !strings.Contains(got, "no X displays") {
		t.Fatalf("info=%q, want a no-displays message", got)
	}
}
