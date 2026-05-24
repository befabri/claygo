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

func freshContextB(b *testing.B) *Context {
	b.Helper()
	mem := make([]byte, MinMemorySize())
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := Initialize(arena, Dimensions{Width: 1280, Height: 720}, ErrorHandler{
		Func: func(err ErrorData) { b.Fatalf("clay error: type=%d %q", err.Type, err.Text) },
	})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	return ctx
}
