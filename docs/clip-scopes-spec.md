# Floating clip scopes

`FloatingElementConfig.ClipScopes` is a ClayGo extension. Each `ClipScope`
references an element's completed bounding box and selects the horizontal,
vertical, or both axes. The intersection confines the floating root's entire
render stream (including images and custom commands) and its pointer hits.
It does not change sizing, positioning, scrolling, or transition targets.

## Declaration and lifetime

- A nil or empty slice adds no restriction. Entries with neither axis selected
  are ignored, including references to missing elements. On a non-floating
  element the field is ignored.
- Active entries are copied during configuration; callers may immediately
  reuse their slice. `ClipToNone` disables native clip inheritance but does
  not disable explicit scopes.
- Scopes refer to rectangles, not to other scopes. Self references and references
  to later declarations or later-painted roots are supported, without recursive
  scope resolution. Nested floating roots are independent declarations: each
  specifies its own extra scopes, alongside `ClipToAttachedParent` inheritance.
- References resolve after all roots have completed each positioning pass,
  including the second transition pass. A missing reference conceals the tree;
  last frame's geometry is never substituted. An exiting element remains a
  valid reference while its exit clone participates in layout.
- Completed exit transitions do not supply clipping geometry. Exit clones keep
  their copied scopes even while other declarations reuse the frame buffers.

## Rendering and input

Every active scope emits an ordinary scissor start before the floating root
and an end after it, in reverse order, inside any native inherited clips.
Scopes remain balanced even when their rectangle is missing or offscreen.
Renderers intersect nested scissors and leave an unselected axis unrestricted.
The existing convention
that an inherited scissor with neither render flag set clips both axes remains
valid; explicit two-axis scopes use that same representation.

Pointer tests check the same boundaries before hover callbacks or root capture.
Clip rectangles are half-open: left/top are included, right/bottom excluded;
a selected axis with zero extent admits no hits. `ClipScope.Contains` exposes
that rule without performing element lookup. Native element hit boxes retain
their existing closed-rectangle rule. With external scroll handling, native
pointer clipping remains the host's responsibility; explicit scopes still
apply. Native rendering continues to use the host-provided scroll offsets.

## Capacity and compatibility

At most `maxElementCount` active explicit references fit in a frame, including
exit clones. Two fixed-capacity pools preserve the previous frame until its
exit clones are copied. There are no per-declaration allocations and steady
frames allocate nothing for scopes. Overflow reports
`ErrorTypeElementsCapacityExceeded` once per frame and conceals affected roots;
it never silently drops restrictions. Raising the element limit before
`MinMemorySize` / `Initialize` grows these pools and the render-command budget.
The render-command budget is independent: each active scope consumes two
commands. A capacity error invalidates the truncated render stream.

The identity invariant is that nil, empty, or entirely disabled extra scopes
leave both drawing and pointer behavior unchanged from the native baseline.
`oracle-native` builds that baseline without the extension's fields or code.
`TestExtensionsPreserveNativeBaseline` compares it with the extended oracle on
every upstream and native-correction scene, both with unset scopes and with
zero-axis entries injected through configuration copying. Existing upstream
and child-wrap fixtures remain unchanged.

Native clipping corrections live separately in `native_clipping.go` and
`oracle/patches/0000-native-clipping.patch`; their affected cases and compatibility
changes are documented in [Native corrections](../oracle/UPSTREAM.md#native-corrections).
They are not semantics of `ClipScopes`. The extension must preserve their
behavior when disabled, as required by [the extension contract](extensions.md).

The floating inspector lists each active scope's target name and numeric ID,
its horizontal and vertical flags, missing declarations, and capacity failures.
Ignored entries do not appear. Overflow remains fail-closed through exit
snapshots; a fresh declaration does not inherit an earlier declaration's
overflow flag.

The C reference lives in `oracle/patches/clip-scopes.h`; `oracle/clay.h` remains
verbatim upstream. If upstream adds equivalent functionality, port it under
its upstream name, retain compatible aliases for one minor release, and remove
the extension in the next major release. The maintenance workflow is documented
in `oracle/UPSTREAM.md`.
