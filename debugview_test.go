package claygo

import (
	"strings"
	"testing"
)

// runDebugFrame is a small helper that calls BeginLayout, the supplied
// builder, then EndLayout — the standard "render one debug-mode frame"
// dance used by every test in this file.
func runDebugFrame(ctx *Context, build func()) RenderCommandArray {
	ctx.BeginLayout()
	build()
	return ctx.EndLayout(0)
}

// TestDebugViewEmitsCommands confirms the debug overlay actually runs and
// emits render commands when SetDebugModeEnabled(true) is set. We render
// the same trivial scene twice — once with debug off and once with debug
// on — and assert that the on case produces strictly more commands and
// that at least one of those new commands belongs to an element whose
// string id starts with "Clay__Debug_" (proving the elements come from
// the debug panel and not the user UI).
func TestDebugViewEmitsCommands(t *testing.T) {
	build := func(c *Context) {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:  Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
				Padding: PaddingAll(16),
			},
			BackgroundColor: RGBA(40, 40, 48, 255),
		}, func() {
			Text(c, "Hello", TextElementConfig{
				TextColor: RGBA(240, 240, 240, 255),
				FontSize:  16,
			})
		})
	}

	// --- Baseline: debug off ------------------------------------------------
	ctxOff := freshContext(t)
	ctxOff.BeginLayout()
	build(ctxOff)
	cmdsOff := ctxOff.EndLayout(0)
	if len(cmdsOff.Commands) == 0 {
		t.Fatal("baseline scene produced no commands; test setup is wrong")
	}

	// --- Debug on -----------------------------------------------------------
	ctxOn := freshContext(t)
	ctxOn.SetDebugModeEnabled(true)
	ctxOn.BeginLayout()
	build(ctxOn)
	cmdsOn := ctxOn.EndLayout(0)

	if len(cmdsOn.Commands) <= len(cmdsOff.Commands) {
		t.Errorf("debug-on commands=%d, debug-off commands=%d; expected strictly more with debug on",
			len(cmdsOn.Commands), len(cmdsOff.Commands))
	}

	// Cross-check that some of the new commands trace back to debug-view
	// elements (string id prefixed with "Clay__Debug_"). We look the IDs
	// up against the hashmap because RenderCommand.ID is a uint32 hash,
	// not the string itself.
	foundDebugElement := false
	for i := range cmdsOn.Commands {
		item := ctxOn.getHashMapItem(cmdsOn.Commands[i].ID)
		if item == nil {
			continue
		}
		if strings.HasPrefix(item.ElementID.StringID.Text, "Clay__Debug_") {
			foundDebugElement = true
			break
		}
	}
	if !foundDebugElement {
		t.Error("debug mode is on but no render command was emitted for an element with the Clay__Debug_ id prefix")
	}
}

// TestDebugViewShrinksRoot verifies BeginLayout's debug-mode adjustment:
// the root container's width must drop by DebugViewWidth when debugMode
// is enabled so the side panel has somewhere to sit.
func TestDebugViewShrinksRoot(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	ctx.BeginLayout()
	ctx.EndLayout(0)

	if ctx.layoutElements.Length < 1 {
		t.Fatal("no layout elements produced")
	}
	root := ctx.layoutElements.Get(0)
	wantWidth := float32(1280) - float32(DebugViewWidth)
	if root.Dimensions.Width != wantWidth {
		t.Errorf("root width with debug mode on = %v, want %v",
			root.Dimensions.Width, wantWidth)
	}
}

func TestDebugViewGlobalsAreCustomizable(t *testing.T) {
	oldWidth := DebugViewWidth
	oldHighlight := DebugViewHighlightColor
	defer func() {
		DebugViewWidth = oldWidth
		DebugViewHighlightColor = oldHighlight
	}()

	DebugViewWidth = 320
	DebugViewHighlightColor = RGBA(1, 2, 3, 4)
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	ctx.BeginLayout()
	ctx.EndLayout(0)
	root := ctx.layoutElements.Get(0)
	if root.Dimensions.Width != 1280-320 {
		t.Fatalf("root width with custom DebugViewWidth = %v, want %v", root.Dimensions.Width, float32(1280-320))
	}
}

// debugTestScene builds a small scene used by the interactivity tests below,
// giving them stable element ids to query against the debug-panel state.
func debugTestScene(ctx *Context) {
	BoxID(ctx, "Outer", Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding: PaddingAll(16),
		},
		BackgroundColor: RGBA(40, 40, 48, 255),
	}, func() {
		BoxID(ctx, "ChildA", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
		}, nil)
		BoxID(ctx, "ChildB", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
		}, nil)
	})
}

// pointerInPanelAtRow returns a Vector2 that lands inside the debug
// panel's list area on the given row. Row 0 is the first element-list
// entry below the header; row 1 the next, and so on. Matches the
// highlightedRow formula in debugview.go (pointerY/rowHeight - 1).
func pointerInPanelAtRow(row int) Vector2 {
	// Panel sits flush against the right edge: x range
	// [1280-DebugViewWidth, 1280]. Pick the middle.
	panelMidX := 1280 - float32(DebugViewWidth)/2
	// Pick a Y inside the row's band. The formula is
	// (Y / rowHeight) - 1 == row, so Y must be in
	// [(row+1)*rowHeight, (row+2)*rowHeight).
	y := float32(row+1)*debugViewRowHeight + debugViewRowHeight/2
	return Vector2{X: panelMidX, Y: y}
}

// TestDebugViewClickSelectsElement exercises click-to-select: with debug
// mode on, a press inside the panel on row N must store the element id
// shown on that row into Context.debugSelectedElementID. The test first
// lays out one frame (debug list now has stable row positions), then
// presses on row 0 (the auto-root) and re-renders to drive the selection
// state machine.
func TestDebugViewClickSelectsElement(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	// Frame 1: lay out so the debug panel has bboxes / row mapping for
	// the next frame's hover/press queries.
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	// Frame 2: pointer pressed on row 0. Row 0 corresponds to the
	// auto-root (the first DFS visit), which is the rootElementIDString
	// element.
	ctx.SetPointerState(pointerInPanelAtRow(0), true)
	if ctx.debugSelectedElementID != 0 {
		t.Fatalf("pre-frame selection should still be 0; got %d", ctx.debugSelectedElementID)
	}
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	if ctx.debugSelectedElementID == 0 {
		t.Fatal("expected a non-zero selection after pressing on row 0")
	}
	// Row 0 is the auto-root; its hashmap entry exists.
	item := ctx.getHashMapItem(ctx.debugSelectedElementID)
	if item == nil {
		t.Fatalf("selected id %d has no hashmap entry", ctx.debugSelectedElementID)
	}

	// Frame 3: release.
	ctx.SetPointerState(pointerInPanelAtRow(0), false)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	// Frame 4: press on row 1 (the "Outer" element — first non-root row).
	ctx.SetPointerState(pointerInPanelAtRow(1), true)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })
	outerID := GetElementID("Outer").ID
	if ctx.debugSelectedElementID != outerID {
		t.Errorf("expected debugSelectedElementID == GetElementID(\"Outer\") (%d), got %d",
			outerID, ctx.debugSelectedElementID)
	}
}

// TestDebugViewCollapseHidesChildren verifies that flipping the
// collapsed bit on a parent removes its children from the rendered row
// list. We don't simulate the actual button click (the click would
// require a hashmap query that's only stable after a layout pass); we
// poke the collapsed-map field directly and confirm the panel
// re-renders with fewer "ChildA" / "ChildB" elements.
func TestDebugViewCollapseHidesChildren(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	// Count Clay__Debug_Row_<id> entries that point at ChildA / ChildB.
	childAID := GetElementID("ChildA").ID
	childBID := GetElementID("ChildB").ID
	rowAID := HashStringWithOffset(String{Text: "Clay__Debug_Row"}, childAID, 0).ID
	rowBID := HashStringWithOffset(String{Text: "Clay__Debug_Row"}, childBID, 0).ID

	// Before collapse: both child rows are in the hashmap (they were
	// just declared this frame).
	if ctx.getHashMapItem(rowAID) == nil {
		t.Fatal("expected ChildA row to exist before collapse")
	}
	if ctx.getHashMapItem(rowBID) == nil {
		t.Fatal("expected ChildB row to exist before collapse")
	}

	// Collapse the Outer element.
	if ctx.debugCollapsed == nil {
		ctx.debugCollapsed = map[uint32]bool{}
	}
	ctx.debugCollapsed[GetElementID("Outer").ID] = true

	// Re-render; the child rows should not be declared this frame.
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	if it := ctx.getHashMapItem(rowAID); it != nil && it.Generation > ctx.generation {
		// The hashmap entry might still exist as a stale slot, but it
		// must not have been refreshed this frame.
		t.Errorf("ChildA row was refreshed this frame despite Outer being collapsed (gen=%d, ctx.gen=%d)",
			it.Generation, ctx.generation)
	}
	if it := ctx.getHashMapItem(rowBID); it != nil && it.Generation > ctx.generation {
		t.Errorf("ChildB row was refreshed this frame despite Outer being collapsed (gen=%d, ctx.gen=%d)",
			it.Generation, ctx.generation)
	}
}

// TestDebugViewInspectorRendersWhenSelected confirms the bottom
// inspector pane emits its "Element Configuration" header (and the
// selected element's id text) once a selection is active. We seed the
// selection directly to keep the test independent of click-routing
// geometry.
func TestDebugViewInspectorRendersWhenSelected(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	ctx.debugSelectedElementID = GetElementID("Outer").ID

	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	// The inspector container must have been declared.
	insp := ctx.getHashMapItem(GetElementID("Clay__Debug_Inspector").ID)
	if insp == nil {
		t.Fatal("inspector container was not declared")
	}
	// The "InspectorHeader" sub-element must also be present.
	header := ctx.getHashMapItem(GetElementID("Clay__Debug_InspectorHeader").ID)
	if header == nil {
		t.Fatal("inspector header was not declared with a selection active")
	}
	// And the layout-section header (a per-element id) must be there.
	layoutHeader := ctx.getHashMapItem(
		HashStringWithOffset(String{Text: "Clay__Debug_InspHeader_Layout"}, GetElementID("Outer").ID, 0).ID,
	)
	if layoutHeader == nil {
		t.Fatal("inspector Layout section header was not declared")
	}
}

// TestDebugViewHighlightAppearsOnHover confirms that hovering over a
// list row causes a translucent highlight rectangle (parented to the
// scene element) to be declared. We position the pointer over row 1
// (the Outer element) and look up the well-known highlight id.
func TestDebugViewHighlightAppearsOnHover(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	ctx.SetPointerState(pointerInPanelAtRow(1), false)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })

	hl := ctx.getHashMapItem(GetElementID("Clay__Debug_Highlight").ID)
	if hl == nil {
		t.Fatal("expected highlight rectangle to be declared when hovering a row")
	}

	// And no highlight when the pointer is way outside the panel.
	ctx.SetPointerState(Vector2{X: 50, Y: 50}, false)
	runDebugFrame(ctx, func() { debugTestScene(ctx) })
	if hl2 := ctx.getHashMapItem(GetElementID("Clay__Debug_Highlight").ID); hl2 != nil && hl2.Generation > ctx.generation {
		t.Errorf("highlight refreshed this frame when pointer was off-panel (gen=%d, ctx.gen=%d)",
			hl2.Generation, ctx.generation)
	}
}

// TestDebugViewOffscreenMarker confirms an element whose bbox is
// outside the viewport has an "Offscreen" pill rendered next to its
// row. We force-offscreen an element via floating offset and check the
// debug list declared the "Clay__Debug_Off_<id>" marker box.
func TestDebugViewOffscreenMarker(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	build := func() {
		BoxID(ctx, "Visible", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(40), Height: SizingFixed(20)}},
		}, nil)
		BoxID(ctx, "OffStage", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(40), Height: SizingFixed(20)}},
			Floating: FloatingElementConfig{
				AttachTo: AttachToRoot,
				Offset:   Vector2{X: 5000, Y: 5000},
				AttachPoints: FloatingAttachPoints{
					Element: AttachPointLeftTop,
					Parent:  AttachPointLeftTop,
				},
			},
		}, nil)
	}
	// Two frames: first sets the bbox, second sees it offscreen.
	runDebugFrame(ctx, build)
	runDebugFrame(ctx, build)

	offBoxID := HashStringWithOffset(
		String{Text: "Clay__Debug_Off"},
		GetElementID("OffStage").ID, 0,
	).ID
	if ctx.getHashMapItem(offBoxID) == nil {
		t.Errorf("expected Offscreen marker for OffStage element")
	}
}
