package claygo

import "testing"

// TestRootResizedLastFrameDetectsResize covers the rootResizedLastFrame
// flag: false on the first frame and when SetLayoutDimensions receives the
// current dimensions; true immediately after SetLayoutDimensions changes the
// viewport.
func TestRootResizedLastFrameDetectsResize(t *testing.T) {
	ctx := freshContext(t)

	// Frame 1 — first BeginLayout, no prior frame, so no resize event.
	ctx.BeginLayout()
	if ctx.RootResizedLastFrame() {
		t.Errorf("first frame: expected RootResizedLastFrame=false")
	}
	ctx.EndLayout(0)

	// Frame 2 — same viewport, still no resize.
	ctx.BeginLayout()
	if ctx.RootResizedLastFrame() {
		t.Errorf("same-size frame: expected RootResizedLastFrame=false")
	}
	ctx.EndLayout(0)

	// Frame 3 — viewport shrinks.
	ctx.SetLayoutDimensions(Dimensions{Width: 800, Height: 600})
	ctx.BeginLayout()
	if !ctx.RootResizedLastFrame() {
		t.Errorf("after resize: expected RootResizedLastFrame=true")
	}
	ctx.EndLayout(0)

	// Frame 4 — caller reports the unchanged viewport, so the flag drops.
	ctx.SetLayoutDimensions(Dimensions{Width: 800, Height: 600})
	ctx.BeginLayout()
	if ctx.RootResizedLastFrame() {
		t.Errorf("frame after resize: expected RootResizedLastFrame=false")
	}
	ctx.EndLayout(0)
}

// TestLocalAutoIDIncrementsThenResets covers the per-frame counter used to
// build stable IDs for items inside a loop.
func TestLocalAutoIDIncrementsThenResets(t *testing.T) {
	ctx := freshContext(t)

	ctx.BeginLayout()
	a := ctx.LocalAutoID()
	b := ctx.LocalAutoID()
	c2 := ctx.LocalAutoID()
	ctx.EndLayout(0)
	if a == 0 || b <= a || c2 <= b {
		t.Errorf("expected strictly increasing nonzero ids, got %d %d %d", a, b, c2)
	}

	// New frame → counter resets to zero, so the first call lands at 1 again.
	ctx.BeginLayout()
	a2 := ctx.LocalAutoID()
	ctx.EndLayout(0)
	if a2 != a {
		t.Errorf("expected counter to reset between frames; got %d (frame 1: %d)", a2, a)
	}
}

// TestBoxIDOffsetProducesDistinctElements builds a 3-element row using
// BoxIDOffset and asserts each gets a unique element id derived from
// HashStringWithOffset, so downstream queries can target them individually.
func TestBoxIDOffsetProducesDistinctElements(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFixed(300), Height: SizingFixed(100)},
			LayoutDirection: LeftToRight,
		},
	}, func() {
		for i := uint32(0); i < 3; i++ {
			BoxIDOffset(ctx, "Item", i, Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
			}, nil)
		}
	})
	ctx.EndLayout(0)

	id0 := HashStringWithOffset(String{Text: "Item"}, 0, 0).ID
	id1 := HashStringWithOffset(String{Text: "Item"}, 1, 0).ID
	id2 := HashStringWithOffset(String{Text: "Item"}, 2, 0).ID
	if id0 == id1 || id1 == id2 || id0 == id2 {
		t.Fatalf("expected distinct ids, got %d %d %d", id0, id1, id2)
	}
	for _, id := range []uint32{id0, id1, id2} {
		if got := ctx.getHashMapItem(id); got == nil {
			t.Errorf("expected hashmap entry for id %d (from BoxIDOffset)", id)
		}
	}
}

// TestOffscreenCullingSkipsEmission places an in-view red square and an
// out-of-view green square (offset far past the right edge of the viewport
// via a floating element), then verifies the green RECTANGLE is NOT
// emitted when culling is on and IS emitted when culling is off.
func TestOffscreenCullingSkipsEmission(t *testing.T) {
	red := RGBA(255, 0, 0, 255)
	green := RGBA(0, 255, 0, 255)

	build := func(c *Context) {
		Box(c, Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		}, func() {
			// On-screen: the red square renders inside the parent.
			Box(c, Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				BackgroundColor: red,
			}, nil)
			// Offscreen via floating: positioned at (5000, 5000), well
			// outside the 1280x720 viewport.
			Box(c, Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				BackgroundColor: green,
				Floating: FloatingElementConfig{
					AttachTo: AttachToRoot,
					AttachPoints: FloatingAttachPoints{
						Parent:  AttachPointLeftTop,
						Element: AttachPointLeftTop,
					},
					Offset: Vector2{X: 5000, Y: 5000},
				},
			}, nil)
		})
	}

	count := func(cmds RenderCommandArray, col Color) int {
		n := 0
		for i := 0; i < cmds.Len(); i++ {
			if cmds.Get(i).CommandType == RenderCommandTypeRectangle &&
				cmds.Get(i).RenderData.Rectangle.BackgroundColor == col {
				n++
			}
		}
		return n
	}

	// Culling ON (default) — green should be culled.
	ctx := freshContext(t)
	ctx.SetCullingEnabled(true)
	ctx.BeginLayout()
	build(ctx)
	cmds := ctx.EndLayout(0)
	if got := count(cmds, red); got != 1 {
		t.Errorf("culling=on: expected 1 red, got %d", got)
	}
	if got := count(cmds, green); got != 0 {
		t.Errorf("culling=on: expected 0 green (offscreen), got %d", got)
	}

	// Culling OFF — green should reappear.
	ctx2 := freshContext(t)
	ctx2.SetCullingEnabled(false)
	ctx2.BeginLayout()
	build(ctx2)
	cmds2 := ctx2.EndLayout(0)
	if got := count(cmds2, green); got != 1 {
		t.Errorf("culling=off: expected 1 green, got %d", got)
	}
}

// TestWrapEmptyLineAtStartEdgeCase exercises the wrap-pass scenario the
// reviewer flagged: text where lineStartOffset+lineLengthChars==0 (the
// first character is a newline). With the C-matching MAX(idx, 0) clamp,
// reading text[0] is safe — the first character is `\n`, not a space,
// so the trim-trailing-space branch correctly stays off.
func TestWrapEmptyLineAtStartEdgeCase(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	// Text starting with a newline, narrow container forces wrap evaluation.
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(80), Height: SizingFit(0)},
			Padding: PaddingAll(4),
		},
	}, func() {
		Text(ctx, "\nhello world", TextElementConfig{
			TextColor: RGBA(240, 240, 240, 255),
			FontSize:  16,
			WrapMode:  TextWrapWords,
		})
	})
	cmds := ctx.EndLayout(0)
	// Must not panic, must produce at least one TEXT command for the second
	// line "hello world" (or words thereof).
	textCmdCount := 0
	for i := 0; i < cmds.Len(); i++ {
		if cmds.Get(i).CommandType == RenderCommandTypeText {
			textCmdCount++
		}
	}
	if textCmdCount == 0 {
		t.Errorf("expected TEXT commands; got %d", textCmdCount)
	}
}

// TestClipAncestorRecordsOnOpen pins layoutElementClipElementIds population:
// an element opened inside a clip container should have its clip-ancestor
// id recorded; elements opened at the top level should have 0.
func TestClipAncestorRecordsOnOpen(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()

	// Element 1 (index 1) — top-level user element, no clip ancestor.
	BoxID(ctx, "TopLevel", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
	}, nil)

	// Element 2 (index 2) — clip container with element 3 inside.
	BoxID(ctx, "Clipper", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(200)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		// Element 3 (index 3) — child of the clip container.
		BoxID(ctx, "Nested", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
		}, nil)
	})
	ctx.EndLayout(0)

	clipperID := GetElementID("Clipper").ID

	// TopLevel (index 1) is NOT inside any clip.
	if got := ctx.layoutElementClipElementIds.Data[1]; got != 0 {
		t.Errorf("TopLevel clip-ancestor = %d, want 0", got)
	}
	// Clipper itself (index 2) is also not inside a clip — it IS the clip.
	if got := ctx.layoutElementClipElementIds.Data[2]; got != 0 {
		t.Errorf("Clipper's own clip-ancestor = %d, want 0", got)
	}
	// Nested (index 3) is inside Clipper.
	if got := ctx.layoutElementClipElementIds.Data[3]; uint32(got) != clipperID {
		t.Errorf("Nested clip-ancestor = %d, want %d", got, clipperID)
	}
}
