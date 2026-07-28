package browser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The raw Chrome output a user actually sees when the X server rejects the
// container. The wrapped error must name the fix, not just repeat the noise.
func TestExplainDisplayError_AuthorizationRefused(t *testing.T) {
	raw := errors.New(`chrome failed to start: Authorization required, but no authorization ` +
		`protocol specified [ERROR:ozone_platform_x11.cc:257] Missing X server or $DISPLAY`)

	got := explainDisplayError(raw).Error()
	if !strings.Contains(got, "allow-x11.sh") {
		t.Fatalf("error does not name the fix: %s", got)
	}
	// The original text must survive for debugging.
	if !errors.Is(explainDisplayError(raw), raw) {
		t.Fatal("original error was not wrapped")
	}
}

func TestExplainDisplayError_MissingDisplay(t *testing.T) {
	raw := errors.New("chrome failed to start: Missing X server or $DISPLAY")
	got := explainDisplayError(raw).Error()
	if !strings.Contains(got, "/tmp/.X11-unix") {
		t.Fatalf("error does not mention the socket mount: %s", got)
	}
}

// An unrelated failure must not be mislabelled as a display problem.
func TestExplainDisplayError_PassesOtherErrorsThrough(t *testing.T) {
	raw := errors.New("net::ERR_NAME_NOT_RESOLVED")
	got := explainDisplayError(raw).Error()
	if strings.Contains(got, "allow-x11.sh") {
		t.Fatalf("unrelated error was misdiagnosed: %s", got)
	}
}

// withSocketDir points the display check at a temp dir, so the result does not
// depend on whatever X sockets happen to exist on the machine running the tests.
func withSocketDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := x11SocketDir
	x11SocketDir = dir
	t.Cleanup(func() { x11SocketDir = orig })
	return dir
}

func TestCheckDisplay(t *testing.T) {
	withSocketDir(t)

	t.Setenv("DISPLAY", "")
	if msg := CheckDisplay(); !strings.Contains(msg, "DISPLAY is not set") {
		t.Fatalf("msg=%q, want a DISPLAY-is-unset warning", msg)
	}

	// A local DISPLAY whose socket is absent is the Docker misconfiguration.
	t.Setenv("DISPLAY", ":99")
	if msg := CheckDisplay(); !strings.Contains(msg, "X11-unix") {
		t.Fatalf("msg=%q, want a missing-socket warning", msg)
	}

	// A remote (TCP) DISPLAY has no local socket and must not warn.
	t.Setenv("DISPLAY", "192.168.1.5:0")
	if msg := CheckDisplay(); msg != "" {
		t.Fatalf("remote display warned unnecessarily: %s", msg)
	}
}

// With the socket present and no XAUTHORITY, a local display is usable.
func TestCheckDisplay_SocketPresent(t *testing.T) {
	dir := withSocketDir(t)
	if err := os.WriteFile(filepath.Join(dir, "X77"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XAUTHORITY", "")
	t.Setenv("DISPLAY", ":77")
	if msg := CheckDisplay(); msg != "" {
		t.Fatalf("usable display warned: %s", msg)
	}
}

// The X cookie is what the container actually lacks, and docker turns a missing
// bind-mount source into a directory — both must be caught at startup.
func TestCheckDisplay_BadXauthority(t *testing.T) {
	dir := withSocketDir(t)
	if err := os.WriteFile(filepath.Join(dir, "X77"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":77")

	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{"missing", func(t *testing.T) string { return filepath.Join(t.TempDir(), "nope.xauth") }},
		{"directory", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "as-dir")
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"empty", func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "empty.xauth")
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XAUTHORITY", c.setup(t))
			msg := CheckDisplay()
			if !strings.Contains(msg, "allow-x11.sh") {
				t.Fatalf("msg=%q, want it to name the fix", msg)
			}
		})
	}

	// A populated cookie file is accepted.
	good := filepath.Join(t.TempDir(), "ok.xauth")
	if err := os.WriteFile(good, []byte("cookie"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XAUTHORITY", good)
	if msg := CheckDisplay(); msg != "" {
		t.Fatalf("valid cookie warned: %s", msg)
	}
}
