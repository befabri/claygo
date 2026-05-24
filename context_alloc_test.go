package claygo

import "testing"

// TestEphemeralMemoryReusedAcrossFrames pins the per-frame allocation contract
// for the layout arena. The ephemeral arrays (layoutElements, renderCommands,
// …) are allocated once in allocateEphemeralMemory and rewound each BeginLayout
// by resetEphemeralMemory; they must NOT be reallocated per frame.
//
// An earlier design re-`make`d all ten arrays every BeginLayout, churning
// ~6.7 MB of garbage per frame at the default 8192-element cap. An empty frame
// touches the full reset path but declares no user elements (so no DSL-closure
// allocations), which isolates the array handling: it must be zero-alloc.
func TestEphemeralMemoryReusedAcrossFrames(t *testing.T) {
	ctx := freshContext(t)

	// A couple of real frames first, to make sure any lazily-grown scratch
	// reaches steady state before measuring.
	for range 3 {
		ctx.BeginLayout()
		BoxID(ctx, "Box", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(50)}},
			BackgroundColor: RGBA(40, 40, 48, 255),
		}, nil)
		ctx.EndLayout(0.016)
	}

	if avg := testing.AllocsPerRun(500, func() {
		ctx.BeginLayout()
		ctx.EndLayout(0.016)
	}); avg != 0 {
		t.Errorf("empty frame allocates %v/frame; the ephemeral arrays should be reused, not reallocated", avg)
	}
}

// TestEphemeralResetClearsPointerBearingSlots guards the memory-hygiene side of
// reusing (rather than reallocating) the ephemeral arrays. Because the backing
// is reused, a slot that held a pointer-bearing element last frame would keep
// that element's UserData / strings / funcs reachable until overwritten unless
// resetEphemeralMemory clears it. This element is gone in frame 2, so its slot
// must be zeroed — not left pinning the frame-1 UserData object.
func TestEphemeralResetClearsPointerBearingSlots(t *testing.T) {
	ctx := freshContext(t)
	marker := new(int) // distinct heap object referenced via UserData

	ctx.BeginLayout()
	BoxID(ctx, "Parent", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}, LayoutDirection: TopToBottom},
	}, func() {
		BoxID(ctx, "Child", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
			BackgroundColor: RGBA(200, 50, 50, 255),
			UserData:        marker,
		}, nil)
	})
	ctx.EndLayout(0.016)

	childID := HashString(String{Text: "Child"}, 0).ID
	slot := int32(-1)
	for i := int32(0); i < ctx.layoutElements.Capacity; i++ {
		if ctx.layoutElements.Data[i].ID == childID {
			slot = i
			break
		}
	}
	if slot < 0 {
		t.Fatal("child element not found in layoutElements after frame 1")
	}
	if ctx.layoutElements.Data[slot].Config.UserData == nil {
		t.Fatalf("frame 1: child slot %d should carry UserData", slot)
	}

	// Frame 2 declares nothing, so the child's slot is not reused. The reset
	// must have cleared it rather than leaving frame-1 data (and its UserData
	// reference) in place.
	ctx.BeginLayout()
	ctx.EndLayout(0.016)

	if got := ctx.layoutElements.Data[slot]; got.ID != 0 || got.Config.UserData != nil {
		t.Errorf("slot %d not cleared after empty frame: ID=%d UserData=%v (stale entry would pin the UserData object)",
			slot, got.ID, got.Config.UserData)
	}
}

// TestEphemeralResetClearsExitCloneRegion guards the scoped-clear optimization
// on the exit-clone path. An exiting element is cloned into the high end of
// layoutElements (and Length is bumped to capacity), so resetEphemeralMemory
// must clear that recorded clone range too — not just the live low-end — or the
// clone's pointer-bearing data would linger. This is the case the full-capacity
// clear used to cover for free; the scoped version has to track it explicitly.
func TestEphemeralResetClearsExitCloneRegion(t *testing.T) {
	ctx := freshContext(t)
	exitSlide := func(s TransitionData, _ TransitionProperty) TransitionData {
		s.BoundingBox.X = -500
		return s
	}
	tr := TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   1.0,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: exitSlide},
	}
	id := HashString(String{Text: "Exiting"}, 0).ID

	// Frame 1: declare the element.
	ctx.BeginLayout()
	BoxID(ctx, "Exiting", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		BackgroundColor: RGBA(80, 80, 80, 255),
		Transition:      tr,
	}, nil)
	ctx.EndLayout(2.0)

	// Frame 2: remove it. It enters EXITING and is cloned into the high end;
	// Length is bumped to capacity and a clone region is recorded.
	ctx.BeginLayout()
	ctx.EndLayout(2.0)
	cloneStart := ctx.prevLayoutElementsCloneStart
	if cloneStart >= ctx.layoutElements.Capacity {
		t.Fatal("frame 2: expected an exit-clone high-end region")
	}
	foundClone := false
	for i := cloneStart; i < ctx.layoutElements.Capacity; i++ {
		if ctx.layoutElements.Data[i].ID == id {
			foundClone = true
			break
		}
	}
	if !foundClone {
		t.Fatal("frame 2: exit clone not found in the high-end region")
	}

	// Frame 3: elapsed (2.0) >= duration (1.0), so the exit completes and the
	// transition record is removed. Frame 4 then has nothing exiting, and its
	// reset must clear the clone region frame 3 left behind.
	ctx.BeginLayout()
	ctx.EndLayout(2.0)
	ctx.BeginLayout()
	ctx.EndLayout(2.0)

	for i := int32(0); i < ctx.layoutElements.Capacity; i++ {
		if ctx.layoutElements.Data[i].ID == id {
			t.Fatalf("exit-clone slot %d still holds element id %d after the exit completed; clone region not cleared", i, id)
		}
	}
}

// TestTwoPassRenderClearsFirstPassOnlyCommands guards Finding-1: a transition
// frame runs calculateFinalLayout twice, and when an exit completes between the
// passes the second pass emits fewer commands than the first. The first-pass-
// only command slots (carrying ID / UserData / etc.) must be cleared by the
// second pass's clear-before-refill, not left pinning stale data.
func TestTwoPassRenderClearsFirstPassOnlyCommands(t *testing.T) {
	ctx := freshContext(t)
	marker := new(int)
	exitSlide := func(s TransitionData, _ TransitionProperty) TransitionData {
		s.BoundingBox.X = -500
		return s
	}
	// Tiny duration so the exit completes on the second advance (frame 3),
	// producing the pass-1 > pass-2 command-count split that frame. AboveSiblings
	// so the exiting clone reattaches LAST, putting its first-pass command at a
	// slot index the shorter second pass never overwrites — the slot that must be
	// cleared (with the default underneath ordering it renders first and gets
	// harmlessly overwritten, hiding the bug).
	tr := TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   0.01,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: exitSlide, SiblingOrdering: ExitTransitionOrderingAboveSiblings},
	}
	exitingID := HashString(String{Text: "Exiting"}, 0).ID

	declare := func(withExiting bool) RenderCommandArray {
		ctx.BeginLayout()
		BoxID(ctx, "Root", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)}, LayoutDirection: TopToBottom},
		}, func() {
			BoxID(ctx, "Keep", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(20)}},
				BackgroundColor: RGBA(40, 40, 48, 255),
			}, nil)
			if withExiting {
				BoxID(ctx, "Exiting", Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(20)}},
					BackgroundColor: RGBA(200, 50, 50, 255),
					UserData:        marker,
					Transition:      tr,
				}, nil)
			}
		})
		return ctx.EndLayout(0.1)
	}

	declare(true)          // frame 1: both present
	declare(false)         // frame 2: Exiting enters EXITING (renders in both passes)
	cmds := declare(false) // frame 3: exit completes between passes; pass 2 drops it

	// Sanity: the visible output no longer contains the exiting element.
	if findRectCommand(cmds.Commands, exitingID) != nil {
		t.Fatalf("frame 3 still renders the exiting element; test setup is wrong")
	}
	// No render-command slot (including stale ones past Length) may retain the
	// exiting element's command or its UserData.
	for i := int32(0); i < ctx.renderCommands.Capacity; i++ {
		cmd := ctx.renderCommands.Data[i]
		if cmd.ID == exitingID {
			t.Fatalf("renderCommands slot %d retains the exiting element's first-pass command", i)
		}
		if cmd.UserData == marker {
			t.Fatalf("renderCommands slot %d still pins the exiting element's UserData", i)
		}
	}
}

// TestCloneElementsWithExitTransitionNoAllocWhenNothingExits pins the hot path
// for ordinary active transitions. EndLayout calls cloneElementsWithExitTransition
// whenever any transition data exists, but stable enter/move transitions should
// not pay for the nested-exit bookkeeping map when no entry is EXITING.
func TestCloneElementsWithExitTransitionNoAllocWhenNothingExits(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "StableTransition", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		BackgroundColor: RGBA(80, 80, 80, 255),
		Transition: TransitionElementConfig{
			Handler:    linearXInterpolator,
			Duration:   1,
			Properties: TransitionPropertyX,
		},
	}, nil)
	ctx.EndLayout(0.016)
	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("transitionDatas.Length = %d, want 1", got)
	}
	if got := ctx.transitionDatas.Get(0).State; got == TransitionStateExiting {
		t.Fatalf("transition state = %d, want non-exiting", got)
	}

	if avg := testing.AllocsPerRun(500, func() {
		ctx.cloneElementsWithExitTransition()
	}); avg != 0 {
		t.Fatalf("cloneElementsWithExitTransition allocates %v/call with no exiting elements", avg)
	}
}

// TestExitCloneCapacityErrorClearsWrittenClones guards Finding-2: when the
// exit-clone pass overflows partway (capacity error), the clones it already
// wrote to the high end must still be cleared next frame. Recording the clone
// region at each write (recordExitCloneSlot) makes this hold for every error
// path, not just the ones someone remembered to annotate. Uses a small element
// cap plus floating exiting elements (which become independent tree roots, so
// they hit the root-level capacity check rather than reattachment).
func TestExitCloneCapacityErrorClearsWrittenClones(t *testing.T) {
	prev := GetCurrentContext()
	SetCurrentContext(nil)
	oldMax := GetMaxElementCount()
	defer func() {
		SetCurrentContext(nil)
		SetMaxElementCount(oldMax)
		SetCurrentContext(prev)
	}()
	SetMaxElementCount(16)

	mem := make([]byte, MinMemorySize())
	var sawCapacityError bool
	ctx := Initialize(CreateArenaWithCapacityAndMemory(uint(len(mem)), mem), Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) {
			if err.Type == ErrorTypeElementsCapacityExceeded {
				sawCapacityError = true
			}
		},
	})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)

	marker := new(int)
	exitSlide := func(s TransitionData, _ TransitionProperty) TransitionData {
		s.BoundingBox.X = -500
		return s
	}
	tr := TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   1.0,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: exitSlide},
	}

	const n = 8
	// Frame 1: floating boxes (independent tree roots on exit) carrying UserData.
	ctx.BeginLayout()
	for i := range n {
		BoxIDOffset(ctx, "Float", uint32(i), Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
			Floating:        FloatingElementConfig{AttachTo: AttachToRoot},
			BackgroundColor: RGBA(200, 50, 50, 255),
			UserData:        marker,
			Transition:      tr,
		}, nil)
	}
	ctx.EndLayout(0.1)

	// Frame 2: those boxes are gone (all exit + clone to the high end) while live
	// boxes occupy the low end, so the clone pass overflows partway.
	ctx.BeginLayout()
	for i := range n {
		BoxIDOffset(ctx, "Live", uint32(i), Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
			BackgroundColor: RGBA(40, 40, 48, 255),
		}, nil)
	}
	ctx.EndLayout(0.1)

	if !sawCapacityError {
		t.Fatal("expected an ElementsCapacityExceeded error from the overflowing exit-clone pass")
	}

	// Frame 3: a clean frame. Its reset must clear every clone slot the
	// overflowing frame-2 pass wrote — none may still pin the UserData marker.
	ctx.BeginLayout()
	ctx.EndLayout(0.1)
	for i := int32(0); i < ctx.layoutElements.Capacity; i++ {
		if ctx.layoutElements.Data[i].Config.UserData == marker {
			t.Fatalf("layoutElements slot %d still pins exit-clone UserData after a clean frame; clone region not fully cleared", i)
		}
	}
}

// TestWrappedTextLinesClearedOnShrink guards the wrappedTextLines half of
// Finding-1's clear-before-refill: when a frame produces fewer wrapped lines
// than the previous one, the leftover high slots (each holding a Line string)
// must be cleared, not left pinning the old text.
func TestWrappedTextLinesClearedOnShrink(t *testing.T) {
	ctx := freshContext(t)
	build := func(s string) {
		ctx.BeginLayout()
		BoxID(ctx, "Box", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(200)}},
		}, func() {
			Text(ctx, s, TextElementConfig{FontSize: 16})
		})
		ctx.EndLayout(0.016)
	}

	build("AAAA\nBBBB\nCCCC\nDDDD")
	n1 := ctx.wrappedTextLines.Length
	if n1 < 2 {
		t.Fatalf("frame 1 wrapped lines = %d, want >= 2 (multi-line text)", n1)
	}

	build("AAAA")
	n2 := ctx.wrappedTextLines.Length
	if n2 >= n1 {
		t.Fatalf("frame 2 wrapped lines = %d, want < frame 1 (%d)", n2, n1)
	}
	for i := n2; i < n1; i++ {
		if got := ctx.wrappedTextLines.Data[i].Line.Text; got != "" {
			t.Errorf("stale wrapped-line slot %d retains text %q after a shorter frame", i, got)
		}
	}
}
