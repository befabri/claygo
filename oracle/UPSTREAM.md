# Tracking upstream Clay

The `oracle/` harness compiles a **pinned snapshot** of upstream
[clay.h](https://github.com/nicbarker/clay) and dumps a corpus of layout scenes
to `../testdata/*.golden.json`; the Go tests prove the port reproduces that
output byte-for-byte.

- `clay.h` — verbatim upstream header. Never edit by hand.
- `CLAY_VERSION` — provenance: the exact upstream commit `clay.h` came from
  (the header's own `VERSION: 0.14` spans ~100 commits, so this is what pins it).
- `main.c` — hand-written scene corpus + JSON dumper, mirrored on the Go side by
  `goldenScenes` in `scenes_test.go`. Two independent implementations is what
  makes the parity test meaningful, so neither is generated.

## Bumping Clay

```sh
make -C oracle update-clay REF=v0.15              # tag, branch, or full SHA
make -C oracle update-clay CLAY_SRC=../.reference/clay   # offline, from a local clone
```

This re-vendors `clay.h`, rewrites `CLAY_VERSION`, and regenerates the goldens.
Then `git diff testdata/` shows exactly which scenes upstream changed, and
`go test ./...` says whether the port still matches. If the diff is non-empty,
commit `clay.h` + `CLAY_VERSION` + `testdata/` together; if `go test` fails, the
failing scenes pin what to fix in the Go port first.

## Adding a scene

Touch three places, then `make -C oracle regenerate && go test ./...`:

1. `main.c` — the C scene function + a `SCENES[]` entry.
2. `scenes_test.go` — the Go builder + a `goldenScenes` entry.
3. `testdata/<name>.golden.json` — regenerated, never hand-written.

`parity_test.go` fails if these lists fall out of sync.
