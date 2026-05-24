package claygo

import "testing"

// TestSnapshotElementSubtreeZeroAlloc pins the steady-state allocation contract
// of snapshotElementSubtree. Subtree snapshots run every frame for every
// registered transition; an earlier version allocated a fresh []LayoutElement
// plus a []int32 per node each call. The current version reuses the caller's
// backing slice and a shared identity index buffer, so once those buffers reach
// their working size it must not allocate. If this regresses, transitions add
// per-frame GC pressure that scales with the animated subtree size.
func TestSnapshotElementSubtreeZeroAlloc(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "P", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
		BackgroundColor: RGBA(100, 100, 200, 255),
	}, func() {
		for i := 0; i < 5; i++ {
			BoxIDOffset(ctx, "C", uint32(i), Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
				BackgroundColor: RGBA(200, 50, 50, 255),
			}, nil)
		}
	})
	ctx.EndLayout(0.016)

	root := ctx.getHashMapItem(HashString(String{Text: "P"}, 0).ID).LayoutElement

	// Warm the reuse + identity buffers to their steady-state capacity.
	var reuse []LayoutElement
	for i := 0; i < 5; i++ {
		reuse = ctx.snapshotElementSubtree(root, reuse)
	}
	if len(reuse) != 6 {
		t.Fatalf("snapshot len = %d, want 6 (parent + 5 children)", len(reuse))
	}
	// Children of the root must be relative indices 1..5 into the snapshot.
	if got := reuse[0].Children.Length; got != 5 {
		t.Fatalf("root snapshot children = %d, want 5", got)
	}
	for j := int32(0); j < 5; j++ {
		if got := reuse[0].Children.Data[j]; got != j+1 {
			t.Fatalf("root child[%d] index = %d, want %d", j, got, j+1)
		}
	}

	if avg := testing.AllocsPerRun(500, func() {
		reuse = ctx.snapshotElementSubtree(root, reuse)
	}); avg != 0 {
		t.Errorf("snapshotElementSubtree allocates %v/call in steady state, want 0", avg)
	}
}

// TestSnapshotElementSubtreeClearsShrunkReuseTail guards the memory-hygiene
// side of reusing transition snapshots. If a subtree snapshot shrinks, the
// returned slice is shorter but the backing array is still retained by
// ElementSnapshotSubtree; stale tail slots must be cleared so removed elements'
// UserData / strings / funcs are not kept reachable by the old backing array.
func TestSnapshotElementSubtreeClearsShrunkReuseTail(t *testing.T) {
	ctx := freshContext(t)
	marker := new(int)

	ctx.BeginLayout()
	BoxID(ctx, "P", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
	}, func() {
		BoxID(ctx, "Kept", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
		}, nil)
		BoxID(ctx, "Removed", Decl{
			Layout:   LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
			UserData: marker,
		}, nil)
	})
	ctx.EndLayout(0.016)

	root := ctx.getHashMapItem(HashString(String{Text: "P"}, 0).ID).LayoutElement
	reuse := ctx.snapshotElementSubtree(root, nil)
	if len(reuse) != 3 {
		t.Fatalf("initial snapshot len = %d, want 3", len(reuse))
	}
	if reuse[2].Config.UserData != marker {
		t.Fatalf("initial removed-child snapshot lost marker: %v", reuse[2].Config.UserData)
	}

	ctx.BeginLayout()
	BoxID(ctx, "P", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
	}, func() {
		BoxID(ctx, "Kept", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(20)}},
		}, nil)
	})
	ctx.EndLayout(0.016)

	root = ctx.getHashMapItem(HashString(String{Text: "P"}, 0).ID).LayoutElement
	oldBacking := reuse
	reuse = ctx.snapshotElementSubtree(root, reuse)
	if len(reuse) != 2 {
		t.Fatalf("shrunk snapshot len = %d, want 2", len(reuse))
	}
	if got := oldBacking[2]; got.ID != 0 || got.Config.UserData != nil {
		t.Fatalf("shrunk snapshot tail not cleared: ID=%d UserData=%v", got.ID, got.Config.UserData)
	}
}
