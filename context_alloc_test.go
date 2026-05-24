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
