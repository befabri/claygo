package claygo

import (
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
//     for the upstream corpus, and extensionScenes (ext_ prefix) in
//     scenes_ext_test.go for claygo's extensions.
//  3. The oracle binaries' --list output, if they have been built: oracle
//     (patched header) lists everything, oracle-upstream (verbatim header)
//     lists the upstream corpus only.
//
// Any mismatch means somebody added a scene on one side without the other.
// The Go and golden-file check is mandatory; the oracle-binary checks are
// best-effort and are skipped if the binaries have not been compiled.
func TestSceneParity(t *testing.T) {
	goldenNames := listGoldenFiles(t)
	upstreamNames := append(keys(goldenScenes), transitionKeys(goldenTransitionScenes)...)
	sceneNames := append(slices.Clone(upstreamNames), extensionKeys(extensionScenes)...)
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
}

// oracleList runs oracle/<binary> --list, or reports false (after logging a
// skip reason) when the binary has not been built.
func oracleList(t *testing.T, binary string) ([]string, bool) {
	t.Helper()
	path := filepath.Join("oracle", binary)
	if _, err := os.Stat(path); err != nil {
		// A missing binary is fine on a developer machine, but CI builds both
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
