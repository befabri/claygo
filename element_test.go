package claygo

import "testing"

// TestElementMachineryBuildsRoot exercises BeginLayout + EndLayout with no
// user elements at all. The auto-root container must be opened, configured to
// span the viewport, and closed cleanly. After EndLayout the open stack must
// be empty.
func TestElementMachineryBuildsRoot(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	ctx.EndLayout(0)

	if ctx.openLayoutElementStack.Length != 0 {
		t.Errorf("openLayoutElementStack.Length after EndLayout = %d, want 0",
			ctx.openLayoutElementStack.Length)
	}
	if ctx.layoutElements.Length != 1 {
		t.Fatalf("layoutElements.Length = %d, want 1 (just the root)",
			ctx.layoutElements.Length)
	}
	root := ctx.layoutElements.Get(0)
	wantRootID := HashString(String{Text: rootElementIDString}, 0).ID
	if root.ID != wantRootID {
		t.Errorf("root.ID = %d, want %d", root.ID, wantRootID)
	}
}

// TestElementMachineryAutoIDDerivesFromRoot verifies the auto-id formula:
// the first user-opened element gets HashNumber(0, root.id), which is the
// id we expect in every committed scene golden (id=1806997583).
func TestElementMachineryAutoIDDerivesFromRoot(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)}},
	}, nil)
	ctx.EndLayout(0)

	if ctx.layoutElements.Length != 2 {
		t.Fatalf("layoutElements.Length = %d, want 2", ctx.layoutElements.Length)
	}
	userElement := ctx.layoutElements.Get(1)
	rootID := HashString(String{Text: rootElementIDString}, 0).ID
	wantID := HashNumber(0, rootID).ID
	if userElement.ID != wantID {
		t.Errorf("user element id = %d, want %d (HashNumber(0, root.id))",
			userElement.ID, wantID)
	}
	const goldenFirstChildID uint32 = 1806997583
	if userElement.ID != goldenFirstChildID {
		t.Errorf("user element id = %d, want golden %d", userElement.ID, goldenFirstChildID)
	}
}

// TestElementMachinerySiblingIDsIncrement verifies that sibling auto-ids use
// monotonically increasing offsets (offset=0,1,2,...) derived from the
// parent. The IDs must match what row_3_fixed.golden.json pins.
func TestElementMachinerySiblingIDsIncrement(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			LayoutDirection: LeftToRight,
		},
	}, func() {
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
	})
	ctx.EndLayout(0)

	if ctx.layoutElements.Length != 5 {
		t.Fatalf("layoutElements.Length = %d, want 5 (root + outer + 3)",
			ctx.layoutElements.Length)
	}
	parent := ctx.layoutElements.Get(1)
	// Pins from row_3_fixed.golden.json:
	wantSibling := [3]uint32{805335866, 1091999082, 1332490773}
	for i, want := range wantSibling {
		got := ctx.layoutElements.Get(int32(2 + i)).ID
		if got != want {
			t.Errorf("sibling[%d].ID = %d, want %d", i, got, want)
		}
	}
	if parent.Children.Length != 3 {
		t.Errorf("parent.Children.Length = %d, want 3", parent.Children.Length)
	}
}

// TestElementMachineryFitToChildrenDimensions exercises the FIT-axis branch
// of closeElement against the fit_to_children scene: parent FIT, two FIXED
// children (100x60 and 150x80), childGap=8, padding=8. Expected parent size
// after close: 274x96.
func TestElementMachineryFitToChildrenDimensions(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFit(0), Height: SizingFit(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
	}, func() {
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}}}, nil)
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(150), Height: SizingFixed(80)}}}, nil)
	})
	ctx.EndLayout(0)

	parent := ctx.layoutElements.Get(1)
	if parent.Dimensions.Width != 274 {
		t.Errorf("FIT parent.Dimensions.Width = %v, want 274", parent.Dimensions.Width)
	}
	if parent.Dimensions.Height != 96 {
		t.Errorf("FIT parent.Dimensions.Height = %v, want 96", parent.Dimensions.Height)
	}
}

// TestElementMachineryTextLeaf exercises openTextElement: the parent FIT
// container should fit around a "Hello World" text leaf measured with the
// deterministic measurer. Expected: 104 wide x 36 tall, matching text_simple.
func TestElementMachineryTextLeaf(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFit(0), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
	}, func() {
		Text(ctx, "Hello World", TextElementConfig{
			TextColor: RGBA(240, 240, 240, 255),
			FontSize:  16,
		})
	})
	ctx.EndLayout(0)

	if ctx.layoutElements.Length != 3 {
		t.Fatalf("layoutElements.Length = %d, want 3 (root + outer + text)",
			ctx.layoutElements.Length)
	}
	parent := ctx.layoutElements.Get(1)
	if parent.Dimensions.Width != 104 {
		t.Errorf("text parent width = %v, want 104", parent.Dimensions.Width)
	}
	if parent.Dimensions.Height != 36 {
		t.Errorf("text parent height = %v, want 36", parent.Dimensions.Height)
	}

	textLeaf := ctx.layoutElements.Get(2)
	if !textLeaf.IsTextElement {
		t.Error("expected text leaf IsTextElement = true")
	}
	if textLeaf.Dimensions.Width != 88 {
		t.Errorf("text leaf width = %v, want 88", textLeaf.Dimensions.Width)
	}
	if textLeaf.Dimensions.Height != 20 {
		t.Errorf("text leaf height = %v, want 20", textLeaf.Dimensions.Height)
	}
}

// TestElementMachineryPercentOverOne verifies the percent-over-1 error path:
// declaring SizingPercent(1.5) must call the error handler with
// ErrorTypePercentageOver1 — once per offending element, but no panic.
func TestElementMachineryPercentOverOne(t *testing.T) {
	ctx, errs := contextCapturingErrors(t)
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingPercent(1.5), Height: SizingPercent(0.5)},
		},
	}, nil)
	ctx.EndLayout(0)

	gotPercentErr := false
	for _, e := range *errs {
		if e.Type == ErrorTypePercentageOver1 {
			gotPercentErr = true
			break
		}
	}
	if !gotPercentErr {
		t.Errorf("expected ErrorTypePercentageOver1 in %+v", *errs)
	}
}
