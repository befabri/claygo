package claygo

import "testing"

// TestPointerStatePressTransitions exercises the press/release state machine
// in SetPointerState. Mirrors the four PointerDataInteractionState values:
//   - first frame down → PRESSED_THIS_FRAME
//   - subsequent frames still down → PRESSED
//   - first frame up after press → RELEASED_THIS_FRAME
//   - subsequent frames up → RELEASED
func TestPointerStatePressTransitions(t *testing.T) {
	ctx := freshContext(t)
	// Frame 1: pointer just pressed.
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, true)
	if got := ctx.PointerState().State; got != PointerDataPressedThisFrame {
		t.Errorf("frame1 state = %d, want PressedThisFrame", got)
	}
	// Frame 2: still down.
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, true)
	if got := ctx.PointerState().State; got != PointerDataPressed {
		t.Errorf("frame2 state = %d, want Pressed", got)
	}
	// Frame 3: released.
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, false)
	if got := ctx.PointerState().State; got != PointerDataReleasedThisFrame {
		t.Errorf("frame3 state = %d, want ReleasedThisFrame", got)
	}
	// Frame 4: still up.
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, false)
	if got := ctx.PointerState().State; got != PointerDataReleased {
		t.Errorf("frame4 state = %d, want Released", got)
	}
}

// TestPointerOverHitsCorrectElement runs a simple layout, sets the pointer
// inside an inner element's bbox, and asserts PointerOver returns true for
// that element's id and false for an unrelated id.
func TestPointerOverHitsCorrectElement(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Outer", Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(300), Height: SizingFixed(200)},
			Padding: PaddingAll(10),
		},
	}, func() {
		BoxID(ctx, "Inner", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(50)}},
		}, nil)
	})
	ctx.EndLayout(0)

	// Inner bbox is (10, 10, 100, 50). Pointer at (50, 30) is inside.
	ctx.SetPointerState(Vector2{X: 50, Y: 30}, false)

	innerID := GetElementID("Inner")
	outerID := GetElementID("Outer")
	if !ctx.PointerOver(innerID) {
		t.Errorf("expected PointerOver(Inner) = true")
	}
	if !ctx.PointerOver(outerID) {
		t.Errorf("expected PointerOver(Outer) = true (pointer is inside both)")
	}

	// A point outside Outer entirely.
	ctx.SetPointerState(Vector2{X: 500, Y: 500}, false)
	if ctx.PointerOver(innerID) {
		t.Errorf("expected PointerOver(Inner) = false when far outside")
	}
}

// TestGetElementData returns bbox + Found from the layout-element hashmap.
func TestGetElementData(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Tile", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(64), Height: SizingFixed(64)}},
	}, nil)
	ctx.EndLayout(0)

	id := GetElementID("Tile")
	got := ctx.GetElementData(id)
	if !got.Found {
		t.Fatalf("expected Found = true for known id")
	}
	if got.BoundingBox.Width != 64 || got.BoundingBox.Height != 64 {
		t.Errorf("expected 64×64, got %vx%v", got.BoundingBox.Width, got.BoundingBox.Height)
	}

	// Unknown id returns Found = false.
	missing := ctx.GetElementData(ElementID{ID: 0xDEADBEEF, StringID: String{Text: "nope"}})
	if missing.Found {
		t.Errorf("expected Found = false for unknown id")
	}
}

// TestOnHoverCallback exercises the OnHover registration: a callback set on
// an element during BeginLayout must fire on a SetPointerState call where
// the pointer lands inside that element's bounding box.
func TestOnHoverCallback(t *testing.T) {
	ctx := freshContext(t)

	// Frame 1: declare layout, register OnHover. No firing yet (pointer not
	// updated this frame).
	ctx.BeginLayout()
	called := 0
	var capturedID ElementID
	BoxID(ctx, "Button", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
	}, func() {
		ctx.OnHover(func(id ElementID, _ PointerData, _ any) {
			called++
			capturedID = id
		}, nil)
	})
	ctx.EndLayout(0)

	// Frame-start pointer update: now the callback should fire.
	ctx.SetPointerState(Vector2{X: 30, Y: 10}, false)
	if called != 1 {
		t.Errorf("OnHover callback fired %d times, want 1", called)
	}
	if capturedID.ID != GetElementID("Button").ID {
		t.Errorf("OnHover callback got id=%d, want %d", capturedID.ID, GetElementID("Button").ID)
	}
}

func TestOnHoverReceivesPreTransitionPointerState(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	var captured PointerDataInteractionState
	called := false
	BoxID(ctx, "PressTarget", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
	}, func() {
		ctx.OnHover(func(_ ElementID, pointer PointerData, _ any) {
			called = true
			captured = pointer.State
		}, nil)
	})
	ctx.EndLayout(0)

	ctx.SetPointerState(Vector2{X: 200, Y: 200}, false)
	ctx.SetPointerState(Vector2{X: 200, Y: 200}, false)
	if got := ctx.PointerState().State; got != PointerDataReleased {
		t.Fatalf("setup pointer state = %d, want released", got)
	}
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, true)
	if !called {
		t.Fatalf("OnHover callback was not called")
	}
	if captured != PointerDataReleased {
		t.Fatalf("OnHover pointer state = %d, want previous released state", captured)
	}
	if got := ctx.PointerState().State; got != PointerDataPressedThisFrame {
		t.Fatalf("post-call pointer state = %d, want pressed-this-frame", got)
	}
}

// TestGetPointerOverIdsContainsHits verifies that the snapshot returned by
// GetPointerOverIds includes every element the pointer is currently over.
func TestGetPointerOverIdsContainsHits(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "A", Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(200), Height: SizingFixed(200)},
			Padding: PaddingAll(20),
		},
	}, func() {
		BoxID(ctx, "B", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(80)}},
		}, nil)
	})
	ctx.EndLayout(0)

	// Point inside both A (0..200) and B (20..100). Expect both ids.
	ctx.SetPointerState(Vector2{X: 50, Y: 50}, false)

	ids := ctx.GetPointerOverIds()
	// Expected hits: auto-root (full viewport), A, B. Pointer (50,50) is
	// inside all three. Order varies; just check that A and B are present.
	aFound, bFound := false, false
	for _, id := range ids {
		if id.ID == GetElementID("A").ID {
			aFound = true
		}
		if id.ID == GetElementID("B").ID {
			bFound = true
		}
	}
	if !aFound || !bFound {
		t.Errorf("expected both A and B in hits; aFound=%v bFound=%v ids=%v", aFound, bFound, ids)
	}
}

// TestHoveredQueriesPreviousFrameBBox confirms that Hovered() inside a
// Box closure reads the bounding box from the PREVIOUS frame (since the
// current frame's bbox is computed during EndLayout). A two-frame test:
// frame 1 lays out an element; frame 2 sets pointer over its remembered
// location and the inner closure reads Hovered() = true.
func TestHoveredQueriesPreviousFrameBBox(t *testing.T) {
	ctx := freshContext(t)

	// Frame 1: layout an element so its bbox is recorded.
	ctx.BeginLayout()
	BoxID(ctx, "Tile", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)},
		},
	}, nil)
	ctx.EndLayout(0)

	// Frame 2: pointer over Tile's previous bbox.
	ctx.SetPointerState(Vector2{X: 25, Y: 25}, false)

	gotHovered := false
	ctx.BeginLayout()
	BoxID(ctx, "Tile", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)},
		},
	}, func() {
		gotHovered = ctx.Hovered()
	})
	ctx.EndLayout(0)

	if !gotHovered {
		t.Errorf("Hovered() returned false; want true for pointer inside the element's bbox")
	}
}

func TestHoveredUsesPointerOverIdsAndRespectsClip(t *testing.T) {
	ctx := freshContext(t)
	build := func(capture *bool) {
		ctx.BeginLayout()
		BoxID(ctx, "HoverClipParent", Decl{
			Layout: LayoutConfig{
				Sizing:  Sizing{Width: SizingFixed(50), Height: SizingFixed(50)},
				Padding: Padding{Left: 60},
			},
			Clip: ClipElementConfig{Horizontal: true, Vertical: true},
		}, func() {
			BoxID(ctx, "HoverClippedChild", Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(20), Height: SizingFixed(20)}},
			}, func() {
				if capture != nil {
					*capture = ctx.Hovered()
				}
			})
		})
		ctx.EndLayout(0)
	}

	build(nil)
	ctx.SetPointerState(Vector2{X: 65, Y: 10}, false)
	var hovered bool
	build(&hovered)
	if hovered {
		t.Fatalf("Hovered returned true for child outside its clip ancestor")
	}
}

func TestScrollPriorityUsesDeclarationOrderAtSharedEdge(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "ScrollPriorityParent", Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFixed(200), Height: SizingFixed(50)},
			LayoutDirection: LeftToRight,
		},
	}, func() {
		for _, name := range []string{"ScrollA", "ScrollB"} {
			BoxID(ctx, name, Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(50)}},
				Clip:   ClipElementConfig{Vertical: true},
			}, func() {
				Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(200)}}}, nil)
			})
		}
	})
	ctx.EndLayout(0)

	ctx.SetPointerState(Vector2{X: 100, Y: 10}, false)
	ctx.UpdateScrollContainers(false, Vector2{Y: -1}, 1.0/60.0)
	a := ctx.GetScrollContainerData(GetElementID("ScrollA"))
	b := ctx.GetScrollContainerData(GetElementID("ScrollB"))
	if a.ScrollPosition.Y != 0 {
		t.Fatalf("earlier scroll container moved to %v, want 0", a.ScrollPosition.Y)
	}
	if b.ScrollPosition.Y != -10 {
		t.Fatalf("later scroll container moved to %v, want -10", b.ScrollPosition.Y)
	}
}

func TestSetPointerStateReturnsWhenMaxElementsExceeded(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	called := false
	BoxID(ctx, "OverflowGuardTarget", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
	}, func() {
		ctx.OnHover(func(ElementID, PointerData, any) { called = true }, nil)
	})
	ctx.EndLayout(0)

	ctx.warnMaxElementsExceeded = true
	ctx.SetPointerState(Vector2{X: 10, Y: 10}, false)
	if called {
		t.Fatalf("OnHover callback fired despite max-elements warning")
	}
	if ctx.PointerOver(GetElementID("OverflowGuardTarget")) {
		t.Fatalf("PointerOver updated despite max-elements warning")
	}
}
