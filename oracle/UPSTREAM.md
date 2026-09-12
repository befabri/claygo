# Tracking upstream Clay

The oracle is a small C program that runs the same test layouts as claygo.
It writes expected render commands to [`testdata/`](../testdata/), and the Go
tests compare their output with those files, known as goldens.

Use this guide when updating Clay, adding a test scene, or changing a C patch.
The original [clay.h](clay.h) stays untouched; [CLAY_VERSION](CLAY_VERSION)
records its upstream commit and our patches.

## Running the checks

Run these commands from the repository root. You need Go, a C compiler, Make,
`patch`, and Git:

```sh
make -C oracle
go test ./...
```

The build produces three programs so we can check fixes and extensions
separately:

| Program | Includes | Test scenes |
|---|---|---|
| `oracle-upstream` | Original upstream code | Upstream behavior |
| `oracle-native` | Upstream code with our native fixes | Upstream behavior and fixes |
| `oracle` | All fixes and extensions | All scenes |

To run a single scene, use its name from the list:

```sh
./oracle/oracle --list
./oracle/oracle row_3_fixed
```

For a clean rebuild and a check against the committed goldens, run:

```sh
make -C oracle verify
```

This regenerates the expected files and fails if they differ from what is
committed. It also checks that adding extensions leaves the upstream and
native correction scenes unchanged. These comparisons cover the test scenes,
not every possible layout.

A scene must finish without engine errors. The oracle stops at the first error
before writing JSON, and the Go checks also reject unexpected error output.

## Updating Clay

Choose a tag, branch, or full commit ID and use it in place of `TAG_OR_COMMIT`:

```sh
make -C oracle update-clay REF=TAG_OR_COMMIT
```

Or copy from a local Clay checkout without fetching from GitHub:

```sh
make -C oracle update-clay CLAY_SRC=/path/to/clay
```

This updates `clay.h` and `CLAY_VERSION`, applies our patches, and regenerates
the goldens. If a patch no longer applies, it stops before regeneration.
[Rebase that patch](#editing-patches), then run regeneration again.

Review `git diff -- testdata/` to see which layouts changed, and run
`go test ./...` to check the Go port. Resolve any differences before committing
the header, version file, patch changes, Go changes, and goldens together.

## Adding a scene

Write the layout in both C and Go, using the same scene name:

1. Add the C scene function and a `SCENES[]` entry in [main.c](main.c).
2. Add the Go builder to the matching table below.
3. Run `make -C oracle regenerate`, review the generated JSON, then run
   `go test ./...`.

| Scene kind | Name prefix | Go table |
|---|---|---|
| Upstream layout | No reserved prefix | `goldenScenes` in [scenes_test.go](../scenes_test.go) |
| Upstream transition | No reserved prefix | `goldenTransitionScenes` in [scenes_transition_test.go](../scenes_transition_test.go) |
| Native fix | `native_` | `nativeScenes` in [native_clip_test.go](../native_clip_test.go) |
| Extension | `ext_` | `extensionScenes` in [scenes_ext_test.go](../scenes_ext_test.go) |

In `main.c`, put native scenes inside the existing native guard and extension
scenes inside the extension guard. This keeps them in the appropriate builds.
Regeneration uses the matching program for each kind of scene; never edit a
golden by hand. The tests check that the C scenes, Go scenes, and golden files
have matching names.

Keep scenes within both implementations' capacity limits. The pinned C version
has 100 scroll-container slots, and clipping containers use these slots too.
Raising the element limit does not raise that separate limit.

## Editing patches

The C functions for each feature live in `patches/*.h`; edit those files
directly. The accompanying `.patch` files connect them to Clay's structures
and functions. Make generates `clay_native.h` and `clay_ext.h` from these
patches; neither generated header is committed.

To change a patch or adapt it after an upstream update, work on the resulting
C code rather than editing the diff by hand. For example:

```sh
make -C oracle rebase-patch P=patches/0001-child-wrap.patch
```

Edit `oracle/clay_ext.h.work`. Any changes that could not be applied are in
`oracle/clay_ext.h.work.rej`; apply them by hand and remove that file once
resolved. Then save the revised patch and check it:

```sh
make -C oracle refresh-patch P=patches/0001-child-wrap.patch
make -C oracle regenerate
go test ./...
```

Use the same patch path for both commands. If several patches need updating,
work in filename order. The refresh command checks that the patch reproduces
your edited C file. Normal builds require exact patch context (`--fuzz=0`).

## Native corrections

Native corrections are fixes to existing Clay behavior and apply by default.
Their `*-native-*.patch` files are applied before extension patches and must
build without any extension code.

### Clipping

The clipping fixes change these cases compared with the pinned upstream code:

- A floating element's own clipping no longer leaks onto later siblings.
- Nested viewports clip drawing, text, and pointer input along each viewport's
  selected axes. Pointer clipping includes the top and left edges and excludes
  the bottom and right; ordinary element hit boxes still include all edges.
- Clipping follows the final layout bounds, including animations. Missing
  targets never reuse the previous frame's bounds.
- A container outside the screen still clips any visible children. If neither
  the container nor its children are visible, its clipping commands are skipped.
- Exiting panels keep their clipping parents but follow their current settings.
  Turning off an inner clip leaves any active outer clips in place.
- Attachment parents must be present in the current declaration. External
  scroll offsets apply once, at the nearest inherited viewport.

The implementation is in [native_clipping.go](../native_clipping.go) and
[native-clipping.h](patches/native-clipping.h), connected to Clay by
[0000-native-clipping.patch](patches/0000-native-clipping.patch).
[native_clip_test.go](../native_clip_test.go) contains the regression tests
and `native_clip_*` scenes.

When upstream fixes one of these cases, port its implementation. Remove our
matching patch changes once the regression also passes against original Clay.

## Extensions

Extensions add features and are off by default. When disabled, they must leave
output unchanged from the **native baseline**: Clay with the fixes above.

| Extension | Go implementation | C reference |
|---|---|---|
| [Child wrapping](#child-wrap) | [wrapchildren.go](../wrapchildren.go) | [Header](patches/child-wrap.h), [patch](patches/0001-child-wrap.patch) |

### Child wrap

`LayoutConfig.WrapChildren` starts a new row or column when children do not
fit. `WrapLineGap` sets the gap between those lines. If the children fit on one
line, enabling wrapping must leave the layout unchanged.

Regression tests live in [wrapchildren_test.go](../wrapchildren_test.go).
The shared C and Go scenes use the `ext_wrap_*` prefix.

### When upstream adds an equivalent feature

Adopt the upstream API. Keep compatible aliases for one minor release and
remove the extension API in the next major release. Remove the C reference
patch and header, along with any extension scenes covered by upstream tests.
