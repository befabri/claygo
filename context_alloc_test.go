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
