# Adding a feature upstream Clay does not have

claygo is a hand-written port of upstream Clay, proven byte-for-byte against
a C oracle built from a pinned, verbatim `clay.h`. That proof is the whole
value of the port, so a feature upstream lacks must not blur it. This
document is the process for adding one. Child wrapping
(`docs/child-wrap-spec.md`) is the worked example; every path below exists
because of it.

## 1. The contract

An extension is accepted when all of the following hold.

1. **Off by default.** The switch is a field whose zero value disables the
   feature. No context-level or build-time gate, no behavior change for
   trees that do not set it.
2. **The default path stays byte-identical to the native baseline.** The
   baseline is verbatim Clay plus separately documented native corrections
   (below), compiled without extension fields or code. An unset extension
   must not change that baseline. Every upstream golden still reproduces
   unchanged. Define the extension's trivial case so it produces exactly the
   baseline commands, write that identity down, and test it.
3. **The oracle stays honest.** `oracle/clay.h` remains verbatim upstream.
   The extension gets a C reference implementation under `oracle/patches/`:
   a header of new functions, plain C, plus a small unified diff that splices
   the header and the hook lines into `clay.h` at build time to produce
   `clay_ext.h`. The Go code mirrors that C, not the other way round.
4. **Two implementations, one corpus.** Extension scenes exist in both
   `oracle/main.c` and the Go scene tables, carry the `ext_` prefix, and
   their goldens are regenerated from the patched oracle. The patched oracle
   must also reproduce every upstream and native-correction golden;
   `make -C oracle verify` checks both on every run.
5. **Physically separated, tagged where a reader needs it.** Extension logic
   lives in its own Go file and its own C header. Hook sites in the ported
   Go files are a few lines that call into that file and are named after it
   (`wrap...`), which already says whose code they are. The C patch is
   different: its hook lines sit inside upstream functions, so every one of
   them carries the tag `claygo extension: <name>`. A future upstream bump
   is ported by reading `clay.h` diffs by hand, and small, tagged hooks are
   what keep that possible.
6. **Documented as a divergence.** A spec under `docs/`, a row in the
   "Extensions" table of `oracle/UPSTREAM.md`, a section in `README.md`, a
   doc comment on the field saying it is a claygo extension, and a
   migration policy for the day upstream ships an equivalent.

## 2. Oracle mechanics

Native fixes are separate changes, not part of an extension. They have their
own Go implementation, C header and `*-native-*.patch`, named affected cases in
`oracle/UPSTREAM.md`, and `native_` regression scenes. Native patches sort before
extension patches and must compile alone. An extension cannot use a native fix
as an exception to rule 1: it must preserve the corrected baseline when unset.
The upstream corpus passing is evidence for those scenes, not proof that a
native correction changes no other input.

The build has three binaries and two generated headers:

- `clay_native.h` = verbatim `clay.h` + `patches/*-native-*.patch` in name
  order. `oracle-native` uses this header with `-DCLAY_ORACLE_NATIVE` and
  lists upstream and `native_` scenes. It has no extension fields or code.

- `clay_ext.h` = `clay.h` + every `patches/*.patch` in name order. Each
  patch is dry-run first with `--fuzz=0`; a rejected hunk fails the build
  with its number and the message that upstream touched code the extension
  patches. Fuzz is off on purpose: a patch that only applies loosely needs a
  rebase, not luck. A patch `#include`s its functions from a sibling
  `patches/<name>.h`, so `oracle` depends on those headers directly.
- `oracle` is built from `clay_ext.h` and lists every scene.
- `oracle-upstream` is built from the verbatim `clay.h` with
  `-DCLAY_ORACLE_UPSTREAM` and lists only upstream scenes. `main.c` picks the
  header with that define. Native scenes are excluded from this build;
  extension scenes and their `SCENES[]` entries are excluded from both
  baseline builds.
- `make regenerate` takes upstream goldens from `oracle-upstream`, `native_`
  goldens from `oracle-native`, and `ext_` goldens from `oracle`. `make verify`
  cleans, builds all three, regenerates, and fails on changed goldens. It
  also compares the extended oracle against every upstream and native
  golden. Its `--disabled-clip-scopes` variant supplies zero-axis entries
  through actual configuration copying in every scene declaration and must
  reproduce the same output. C scenes also execute their input assertions.
- `update-clay.sh` re-vendors `clay.h`, records the applied patches in
  `CLAY_VERSION`, re-applies them, and stops before regenerating if one no
  longer applies.
- `rebase-patch.sh`, behind `make rebase-patch` and `make refresh-patch`, is
  the only way a patch is edited (below).
- CI builds all three binaries before `go test` (the parity test refuses to skip
  its `--list` cross-checks under `CI`) and runs `make -C oracle verify` last.

### Writing the C

Split the extension's C by where it has to live.

- `patches/<name>.h` holds every new function, as ordinary C that is
  highlighted, formatted, compiled and reviewed like any other file. The
  patch `#include`s it once, at the point in the implementation section of
  `clay.h` where everything it needs is already defined and everything that
  calls it comes later; for child wrap that is directly in front of
  `Clay__SizeContainersAlongAxis`. Forward-declare at the top of the header
  the few upstream functions defined after that point, and open the header
  with `#ifndef CLAY__ARRAY_DEFINE` / `#error` so compiling it on its own
  fails with one message instead of a page of errors.
- `patches/NNNN-<name>.patch` holds only what must sit inside upstream's own
  structs and functions: the config field, new types that upstream structs
  refer to, per-element and per-context fields, forward declarations for
  hooks that run before the `#include`, the `#include` itself, and the hook
  lines. Keep it small enough to read on one screen; the child wrap patch is
  sixteen hunks and about fifty added lines.

Never edit the hunks in place. `make rebase-patch` applies the patch, with
fuzz, onto its base (`clay.h` plus the patches that sort before it) and
writes the result to `clay_ext.h.work`, leaving any hunk that still fails in
`clay_ext.h.work.rej`. Edit `clay_ext.h.work` as C, delete the `.rej` once
its hunks are folded in, then `make refresh-patch` re-diffs the work file
into the patch with current line numbers and context, keeps the description
above the first hunk, and proves the result applies with `--fuzz=0` and
reproduces the work file before writing anything. Adding or moving a hook is
the same two commands. With several patches under `patches/`, name the one
you mean: `P=patches/NNNN-<name>.patch`. Editing the functions themselves is
an edit to the header and needs no patch work at all.

Minimize what the hooks touch. Prefer inserting a call and a `continue` or an
`else` over re-indenting upstream code, so an upstream change inside a block
you merely wrapped still applies. Duplicating an upstream loop inside the
extension's own helper is better than moving upstream's loop.

New per-element or per-context state goes outside upstream's unions and at
the end of upstream's structs; a new config field goes last so positional
initializers in upstream examples keep working. New arenas are allocated in
`Clay__InitializeEphemeralMemory` or `Clay__InitializePersistentMemory` so
`Clay_MinMemorySize` picks them up, and the Go `minMemorySizeFor` gets the
matching line.

## 3. Order of work

Do the phases in this order; each ends with `go test ./...` and
`make -C oracle verify` green.

1. **Freeze semantics.** Write the spec first, including the identity
   invariant, float-ordering rules where the trivial case must match
   upstream (accumulation order, where padding is added, integer versus
   float halving), and the behavior for exiting children, clip containers,
   percent children, and aspect-ratio children. Every rule that is decided
   after two implementations exist costs a C edit, a Go edit, and golden
   churn.
2. **Infrastructure first.** Add the scene table and the `ext_` plumbing
   with no scenes, and a patch whose only hunk `#include`s a header with no
   functions yet. Everything must stay green.
3. **C reference.** Write the header and the hooks, add the `ext_` scenes,
   regenerate, and read the JSON of the first few scenes by hand before
   trusting them. The C is the reference; get the numbers right here.
4. **Go mirror.** Mirror the patch function for function. The goldens must
   match byte for byte; when they do not, the C is presumed right until
   proven otherwise.
5. **Go-only tests.** Hand-computed cases for every rule in the spec, the
   identity invariant over the whole upstream corpus (force the switch on in
   every applicable element of every upstream scene and compare to the
   golden, skipping scenes where the feature actually engaged), a repeated
   pass over the same tree, capacity and overflow of any new pool, and
   zero-allocation steady state.
6. **Peripherals.** Exit transition clones, the debug inspector, a benchmark
   scene, `MinMemorySize`.
7. **Documentation and version.** Spec, `UPSTREAM.md` table, `README.md`
   section, field doc comment; tag a minor version. Consumers pin the tag.

## 4. Things that went wrong once

Each of these was found by review after the goldens were green, because a
bug mirrored line for line in C and Go still matches its own golden. Check
for them explicitly.

- **Float idempotency.** A comparison that upstream never repeats can be
  repeated by your feature (a second layout pass, a second sizing sweep).
  Grown float32 sizes re-sum a hair over their target; use the engine's
  epsilon where a strict comparison would flip a decision.
- **Pipeline order.** If your feature needs a quantity before the pass that
  produces it, do not substitute the value a pass produced for a different
  purpose. A grown width is not a content width.
- **Clip containers.** Upstream lets them shrink to their padding and never
  squashes their children; every minimum or bound you publish must honor
  that.
- **Rigid children.** `Fixed` and min-clamped children do not follow a
  shrunk extent; anything that stacks or tiles must advance by the larger of
  the planned size and the actual content so nothing overlaps and no render
  command gets a negative size.
- **Ancestors have the last word.** If your feature publishes a size in one
  pass and an ancestor re-decides it in a later pass, do not publish again
  afterwards.
- **Integer halving.** Upstream halves `uint16` fields with integer division
  before converting; mirror the conversion order, not the arithmetic you
  would have written.

## 5. When upstream ships the feature

Port upstream's implementation under upstream's name. If the semantics match,
keep the extension's field for one minor version as an alias and delete it in
the next major; otherwise remove it in the next major with a note. Drop the
patch, its header, and every `ext_` scene that upstream's own scenes now
cover, and remove the row from `UPSTREAM.md`.
