package claygo

import "testing"

// Benchmarks for the per-frame layout path. These aren't run by CI (go test
// doesn't run benchmarks by default); run with:
//
//	go test -run '^$' -bench BenchmarkEndLayout -benchmem ./...
//
// They exist mainly to make per-frame allocation visible. The ephemeral
// arrays are allocated once (allocateEphemeralMemory) and only rewound each
// BeginLayout (resetEphemeralMemory), so a steady-state frame allocates almost
// nothing here — the residual comes from the DSL closures these builders pass.
// The with/without-transitions pair isolates the transition cost from baseline.

// benchTree builds a moderate tree: 12 rows, each with one cell. When
// withTransition is true every row also carries a transition handler, so
// snapshotTransitionElements runs for all of them each EndLayout.
func benchTree(c *Context, withTransition bool) {
	var transition TransitionElementConfig
	if withTransition {
		transition = TransitionElementConfig{
			Handler:    linearXInterpolator,
			Duration:   1.0,
			Properties: TransitionPropertyX,
		}
	}
	BoxID(c, "Root", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)}, LayoutDirection: TopToBottom, ChildGap: 4},
	}, func() {
		for i := range 12 {
			BoxIDOffset(c, "Row", uint32(i), Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(40)}},
				BackgroundColor: RGBA(60, 60, 80, 255),
				Transition:      transition,
			}, func() {
				BoxIDOffset(c, "Cell", uint32(i), Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(30)}},
					BackgroundColor: RGBA(200, 50, 50, 255),
				}, nil)
			})
		}
	})
}

func benchEndLayout(b *testing.B, withTransition bool) {
	b.Helper()
	ctx := freshContextB(b)
	// Warm up so any reusable buffers reach steady-state capacity.
	for range 5 {
		ctx.BeginLayout()
		benchTree(ctx, withTransition)
		ctx.EndLayout(0.016)
	}
	b.ReportAllocs()
	for b.Loop() {
		ctx.BeginLayout()
		benchTree(ctx, withTransition)
		ctx.EndLayout(0.016)
	}
}

func BenchmarkEndLayoutWithTransitions(b *testing.B) { benchEndLayout(b, true) }
func BenchmarkEndLayoutNoTransitions(b *testing.B)   { benchEndLayout(b, false) }

// benchWrapTree declares a row-wrapping strip and a column-wrapping strip, so
// the second sizing sweep that column wrap forces is on the measured path.
func benchWrapTree(c *Context) {
	BoxID(c, "Root", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)}, LayoutDirection: TopToBottom, ChildGap: 8, Padding: PaddingAll(8)},
	}, func() {
		BoxID(c, "Rows", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(640), Height: SizingFit()}, ChildGap: 8, WrapChildren: true},
		}, func() {
			for i := range 40 {
				BoxIDOffset(c, "Chip", uint32(i), Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(90 + float32(i%4)*10), Height: SizingFixed(28)}},
					BackgroundColor: RGBA(60, 60, 80, 255),
				}, nil)
			}
		})
		BoxID(c, "Cols", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFixed(200)}, ChildGap: 8, LayoutDirection: TopToBottom, WrapChildren: true},
		}, func() {
			for i := range 24 {
				BoxIDOffset(c, "Cell", uint32(i), Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(30)}},
					BackgroundColor: RGBA(200, 50, 50, 255),
				}, nil)
			}
		})
	})
}

func BenchmarkEndLayoutWrapChildren(b *testing.B) {
	ctx := freshContextB(b)
	for range 5 {
		ctx.BeginLayout()
		benchWrapTree(ctx)
		ctx.EndLayout(0.016)
	}
	b.ReportAllocs()
	for b.Loop() {
		ctx.BeginLayout()
		benchWrapTree(ctx)
		ctx.EndLayout(0.016)
	}
}

func freshContextB(b *testing.B) *Context {
	b.Helper()
	arena := CreateArenaWithCapacity(MinMemorySize())
	ctx := Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) { b.Fatalf("clay error: type=%d %q", err.Type, err.Text) },
	})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	return ctx
}
