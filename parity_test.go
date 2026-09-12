package claygo

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestSceneParity guards against drift between the lists of scene names:
//
//  1. The committed testdata/*.golden.json files (the C oracle's regression
//     net).
//  2. The Go-side scene builders: goldenScenes (single-frame) in scenes_test.go
//     plus goldenTransitionScenes (multi-frame) in scenes_transition_test.go
//     for the upstream corpus, nativeScenes in native_clip_test.go for native
//     corrections, and extensionScenes in scenes_ext_test.go for extensions.
//  3. The oracle binaries' --list output, if they have been built: oracle
//     (patched header) lists everything, oracle-upstream (verbatim header)
//     lists upstream only, and oracle-native lists upstream plus corrections.
//
// Any mismatch means somebody added a scene on one side without the other.
// The Go and golden-file check is mandatory; the oracle-binary checks are
// best-effort and are skipped if the binaries have not been compiled.
func TestSceneParity(t *testing.T) {
	goldenNames := listGoldenFiles(t)
	upstreamNames := append(keys(goldenScenes), transitionKeys(goldenTransitionScenes)...)
	nativeNames := append(slices.Clone(upstreamNames), transitionKeys(nativeScenes)...)
	sceneNames := append(slices.Clone(nativeNames), extensionKeys(extensionScenes)...)
	slices.Sort(goldenNames)
	slices.Sort(upstreamNames)
	slices.Sort(sceneNames)

	checkParity(t, "Go scene tables", sceneNames, "testdata", goldenNames)

	if oracleNames, ok := oracleList(t, "oracle"); ok {
		checkParity(t, "oracle --list", oracleNames, "testdata", goldenNames)
		checkParity(t, "oracle --list", oracleNames, "Go scene tables", sceneNames)
	}
	if upstreamListed, ok := oracleList(t, "oracle-upstream"); ok {
		checkParity(t, "oracle-upstream --list", upstreamListed, "upstream Go scene tables", upstreamNames)
		for _, name := range upstreamListed {
			if strings.HasPrefix(name, extensionScenePrefix) {
				t.Errorf("oracle-upstream --list has extension scene %q; it must only know the upstream corpus", name)
			}
		}
	}
	if nativeListed, ok := oracleList(t, "oracle-native"); ok {
		checkParity(t, "oracle-native --list", nativeListed, "native and upstream Go scenes", nativeNames)
	}
}

// This compares builds with and without any extension implementation, including
// scenes that actually exercise native corrections.
func TestExtensionsPreserveNativeBaseline(t *testing.T) {
	_, nativeAvailable := oracleList(t, "oracle-native")
	_, extendedAvailable := oracleList(t, "oracle")
	if !nativeAvailable || !extendedAvailable {
		return
	}
	names := append(keys(goldenScenes), transitionKeys(goldenTransitionScenes)...)
	names = append(names, transitionKeys(nativeScenes)...)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			want := runOracleScene(t, "oracle-native", name)
			got := runOracleScene(t, "oracle", name)
			if !bytes.Equal(got, want) {
				t.Fatalf("extensions change native baseline for %s", name)
			}
		})
	}
}

// Compare render JSON only after checking process status and diagnostics.
// Matching error output from two broken builds is never evidence of parity.
func runOracleScene(t *testing.T, binary string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("./"+filepath.Join("oracle", binary), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", binary, args, err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("%s %v wrote unexpected diagnostics:\n%s", binary, args, stderr.String())
	}
	if !json.Valid(out) {
		t.Fatalf("%s %v did not produce valid render JSON", binary, args)
	}
	return out
}

func TestOracleEngineErrorsAreFatal(t *testing.T) {
	for _, binary := range []string{"oracle-upstream", "oracle-native", "oracle"} {
		t.Run(binary, func(t *testing.T) {
			if _, available := oracleList(t, binary); !available {
				return
			}
			// The root, row and three children cannot fit this declaration
			// budget. Exercise the engine's actual capacity error callback.
			cmd := exec.Command("./"+filepath.Join("oracle", binary), "--max-elements", "4", "row_3_fixed")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if _, exited := err.(*exec.ExitError); !exited || !strings.Contains(stderr.String(), "[clay error]") {
				t.Fatalf("capacity failure did not fail the oracle: %v\n%s", err, stderr.String())
			}
			if len(out) != 0 {
				t.Fatal("failed oracle emitted render JSON that could be mistaken for a golden")
			}
		})
	}
}

// oracleList runs oracle/<binary> --list, or reports false (after logging a
// skip reason) when the binary has not been built.
func oracleList(t *testing.T, binary string) ([]string, bool) {
	t.Helper()
	path := filepath.Join("oracle", binary)
	if _, err := os.Stat(path); err != nil {
		// A missing binary is fine on a developer machine, but CI builds all three
		// before running go test (see .github/workflows/ci.yml); skipping
		// there would let a scene dropped from main.c hide behind its stale
		// golden, since regenerate never deletes files.
		if os.Getenv("CI") != "" {
			t.Fatalf("%s --list cross-check needs the binary under CI: %v (run make -C oracle all first)", binary, err)
		}
		t.Logf("skipping %s --list cross-check: %v", binary, err)
		return nil, false
	}
	out, err := exec.Command("./"+path, "--list").Output()
	if err != nil {
		t.Fatalf("run %s --list: %v", path, err)
	}
	names := splitNonEmpty(string(out))
	slices.Sort(names)
	return names, true
}

func listGoldenFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("testdata", "*.golden.json"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		base := filepath.Base(m)
		name := strings.TrimSuffix(base, ".golden.json")
		out = append(out, name)
	}
	return out
}

func keys(m map[string]func(*Context)) []string {
	return slices.Collect(maps.Keys(m))
}

func transitionKeys(m map[string]func(*Context) RenderCommandArray) []string {
	return slices.Collect(maps.Keys(m))
}

func splitNonEmpty(s string) []string {
	out := []string{}
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func checkParity(t *testing.T, aName string, a []string, bName string, b []string) {
	t.Helper()
	aSet := setOf(a)
	bSet := setOf(b)
	for _, name := range a {
		if !bSet[name] {
			t.Errorf("%s has %q but %s does not", aName, name, bName)
		}
	}
	for _, name := range b {
		if !aSet[name] {
			t.Errorf("%s has %q but %s does not", bName, name, aName)
		}
	}
}

func setOf(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
