package claygo

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSceneParity guards against drift between three lists of scene names:
//
//  1. The committed testdata/*.golden.json files (the C oracle's regression
//     net).
//  2. The Go-side scene builders: goldenScenes (single-frame) in scenes_test.go
//     plus goldenTransitionScenes (multi-frame) in scenes_transition_test.go.
//  3. The oracle binary's --list output, if the binary has been built.
//
// Any mismatch means somebody added a scene on one side without the other.
// The Go and golden-file check is mandatory; the oracle-binary check is
// best-effort and is skipped if oracle/oracle has not been compiled.
func TestSceneParity(t *testing.T) {
	goldenNames := listGoldenFiles(t)
	sceneNames := append(keys(goldenScenes), transitionKeys(goldenTransitionScenes)...)
	sort.Strings(goldenNames)
	sort.Strings(sceneNames)

	checkParity(t, "goldenScenes", sceneNames, "testdata", goldenNames)

	oraclePath := filepath.Join("oracle", "oracle")
	if _, err := os.Stat(oraclePath); err != nil {
		t.Skipf("skipping oracle --list cross-check: %v", err)
		return
	}
	out, err := exec.Command("./"+oraclePath, "--list").Output()
	if err != nil {
		t.Fatalf("run %s --list: %v", oraclePath, err)
	}
	oracleNames := splitNonEmpty(string(out))
	sort.Strings(oracleNames)

	checkParity(t, "oracle --list", oracleNames, "testdata", goldenNames)
	checkParity(t, "oracle --list", oracleNames, "goldenScenes", sceneNames)
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
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func transitionKeys(m map[string]func(*Context) RenderCommandArray) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func splitNonEmpty(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
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
