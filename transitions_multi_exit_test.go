package claygo

import (
	"testing"
)

// transitions_multi_exit_test.go exercises cloneElementsWithExitTransition:
// when an exit-transitioned parent disappears, its previous-frame subtree is
// cloned back for exit rendering. Nested elements that also own transition
// records follow upstream C: their records are removed when the ancestor exit
// owns the subtree.
//
// The single-element exit path is covered by
// TestTransitionsExitTriggersWhenElementRemoved in transitions_test.go.

// TestTransitionsMultiElementSubtreeExit declares a parent with one child in
// frame 1, then in frame 2 declares neither. The clone pass must surface the
// child even though only the parent owns a transition record.
func TestTransitionsMultiElementSubtreeExit(t *testing.T) {
	ctx := freshContext(t)

	// exitSlideOff sets the exit target's BoundingBox.X off-screen so the
	// linear interpolator has a non-trivial path to animate along.
	exitSlideOff := func(initial TransitionData, _ TransitionProperty) TransitionData {
		initial.BoundingBox.X = -500
		return initial
	}

	declareSubtree := func() {
		BoxID(ctx, "ExitingParent", Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(200), Height: SizingFixed(200)},
				LayoutDirection: TopToBottom,
				Padding:         Padding{Top: 10, Left: 10, Right: 10, Bottom: 10},
			},
			BackgroundColor: RGBA(100, 100, 200, 255),
			Transition: TransitionElementConfig{
				Handler:    linearXInterpolator,
				Duration:   1.0,
				Properties: TransitionPropertyX,
				Exit: TransitionExitConfig{
					SetFinalState: exitSlideOff,
				},
			},
		}, func() {
			BoxID(ctx, "ExitingChild", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(50)}},
				BackgroundColor: RGBA(200, 50, 50, 255),
			}, nil)
		})
	}

	// Frame 1: both elements present. Only the parent registers a transition.
	ctx.BeginLayout()
	declareSubtree()
	ctx.EndLayout(0.1)

	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 1 transitionDatas.Length = %d, want 1", got)
	}

	parentID := HashString(String{Text: "ExitingParent"}, 0).ID
	childID := HashString(String{Text: "ExitingChild"}, 0).ID

	// Sanity check: frame 1 render commands should include both rectangles.
	cmdsF1 := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	if findRectCommand(cmdsF1, parentID) == nil {
		t.Fatalf("frame 1: parent rectangle missing from render commands")
	}
	if findRectCommand(cmdsF1, childID) == nil {
		t.Fatalf("frame 1: child rectangle missing from render commands")
	}

	// Frame 2: neither element declared. The parent must enter EXITING and
	// the clone pass must place both parent and child back into render commands.
	ctx.BeginLayout()
	// (no declareSubtree)
	ctx.EndLayout(0.1)

	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 2 transitionDatas.Length = %d, want 1 (parent still exiting)", got)
	}
	td := ctx.transitionDatas.Get(0)
	if td.State != TransitionStateExiting {
		t.Errorf("frame 2 transition.State = %d, want EXITING", td.State)
	}
	if !td.TransitionOut {
		t.Errorf("frame 2 transition.TransitionOut = false, want true")
	}

	cmdsF2 := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	parentCmd := findRectCommand(cmdsF2, parentID)
	childCmd := findRectCommand(cmdsF2, childID)
	if parentCmd == nil {
		t.Errorf("frame 2: parent rectangle missing from render commands (subtree clone broken)")
	}
	if childCmd == nil {
		t.Errorf("frame 2: child rectangle missing from render commands (subtree clone broken)")
	}

	// The cloned commands should retain valid sizes rather than degenerating
	// when the subtree is recreated for exit rendering.
	if parentCmd != nil && (parentCmd.BoundingBox.Width <= 0 || parentCmd.BoundingBox.Height <= 0) {
		t.Errorf("frame 2: parent clone has degenerate size %v", parentCmd.BoundingBox)
	}
	if childCmd != nil && (childCmd.BoundingBox.Width <= 0 || childCmd.BoundingBox.Height <= 0) {
		t.Errorf("frame 2: child clone has degenerate size %v", childCmd.BoundingBox)
	}
}

func TestTransitionsNestedExitTransitionRemovedWithParent(t *testing.T) {
	ctx := freshContext(t)
	exitSlideOff := func(initial TransitionData, _ TransitionProperty) TransitionData {
		initial.BoundingBox.X = -500
		return initial
	}
	transition := TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   1.0,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: exitSlideOff},
	}
	declare := func() {
		BoxID(ctx, "NestedParent", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
			BackgroundColor: RGBA(100, 100, 200, 255),
			Transition:      transition,
		}, func() {
			BoxID(ctx, "NestedChild", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(40)}},
				BackgroundColor: RGBA(200, 50, 50, 255),
				Transition:      transition,
			}, nil)
		})
	}

	ctx.BeginLayout()
	declare()
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 2 {
		t.Fatalf("after frame 1 transitionDatas.Length = %d, want 2", got)
	}

	ctx.BeginLayout()
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 2 transitionDatas.Length = %d, want 1 (nested transition removed)", got)
	}

	parentID := HashString(String{Text: "NestedParent"}, 0).ID
	childID := HashString(String{Text: "NestedChild"}, 0).ID
	cmds := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	if findRectCommand(cmds, parentID) == nil {
		t.Errorf("frame 2: parent rectangle missing from render commands")
	}
	if findRectCommand(cmds, childID) != nil {
		t.Errorf("frame 2: nested child transition rendered after its record was removed")
	}
}

func TestTransitionsNestedExitChildCheckedFirstDoesNotBecomeRoot(t *testing.T) {
	ctx := freshContext(t)
	exitSlideOff := func(initial TransitionData, _ TransitionProperty) TransitionData {
		initial.BoundingBox.X = -500
		return initial
	}
	transition := TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   1.0,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: exitSlideOff},
	}
	parentID := HashString(String{Text: "OrderedParent"}, 0).ID
	childID := HashString(String{Text: "OrderedChild"}, 0).ID

	ctx.BeginLayout()
	BoxID(ctx, "OrderedParent", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
		BackgroundColor: RGBA(100, 100, 200, 255),
		Transition:      transition,
	}, func() {
		BoxID(ctx, "OrderedChild", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 50, 50, 255),
			Transition:      transition,
		}, nil)
	})
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 2 {
		t.Fatalf("after frame 1 transitionDatas.Length = %d, want 2", got)
	}

	if ctx.transitionDatas.Data[0].ElementID != childID {
		if ctx.transitionDatas.Data[1].ElementID != childID {
			t.Fatalf("child transition not found")
		}
		ctx.transitionDatas.Data[0], ctx.transitionDatas.Data[1] = ctx.transitionDatas.Data[1], ctx.transitionDatas.Data[0]
	}

	ctx.BeginLayout()
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 2 transitionDatas.Length = %d, want 1", got)
	}
	if got := ctx.transitionDatas.Get(0).ElementID; got != parentID {
		t.Fatalf("remaining transition id = %d, want parent id %d", got, parentID)
	}
	if got := ctx.layoutElementTreeRoots.Length; got != 1 {
		t.Fatalf("layoutElementTreeRoots.Length = %d, want 1 (auto-root only; parent is reattached)", got)
	}
}

// TestTransitionsMultiElementExitDoesNotLeakAcrossFrames pins the cleanup
// contract: after the exit transitions complete, no stale transitionData
// entries remain and the clone-only render commands stop appearing.
func TestTransitionsMultiElementExitDoesNotLeakAcrossFrames(t *testing.T) {
	ctx := freshContext(t)

	exitSlideOff := func(initial TransitionData, _ TransitionProperty) TransitionData {
		initial.BoundingBox.X = -500
		return initial
	}
	declare := func() {
		BoxID(ctx, "P", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(80)}, LayoutDirection: TopToBottom},
			BackgroundColor: RGBA(10, 10, 10, 255),
			Transition: TransitionElementConfig{
				Handler:    linearXInterpolator,
				Duration:   0.1,
				Properties: TransitionPropertyX,
				Exit:       TransitionExitConfig{SetFinalState: exitSlideOff},
			},
		}, func() {
			BoxID(ctx, "C", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(40), Height: SizingFixed(40)}},
				BackgroundColor: RGBA(20, 20, 20, 255),
				Transition: TransitionElementConfig{
					Handler:    linearXInterpolator,
					Duration:   0.1,
					Properties: TransitionPropertyX,
					Exit:       TransitionExitConfig{SetFinalState: exitSlideOff},
				},
			}, nil)
		})
	}

	// Frame 1: declare both.
	ctx.BeginLayout()
	declare()
	ctx.EndLayout(0.05)

	// Subsequent frames: never declare again. With duration 0.1 and ~0.05
	// deltaTime per frame, the exit animations should retire within a few
	// frames.
	for range 10 {
		ctx.BeginLayout()
		ctx.EndLayout(0.05)
		if ctx.transitionDatas.Length == 0 {
			break
		}
	}
	if got := ctx.transitionDatas.Length; got != 0 {
		t.Errorf("transitionDatas.Length after many exit-only frames = %d, want 0", got)
	}

	// One more empty frame: no leftover render commands for the cloned
	// subtree should appear (the auto-root rectangle has no background, so
	// the only rectangle commands would be from clones).
	ctx.BeginLayout()
	ctx.EndLayout(0.05)
	parentID := HashString(String{Text: "P"}, 0).ID
	childID := HashString(String{Text: "C"}, 0).ID
	cmds := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	if findRectCommand(cmds, parentID) != nil {
		t.Errorf("parent rectangle still emitted after exit completed")
	}
	if findRectCommand(cmds, childID) != nil {
		t.Errorf("child rectangle still emitted after exit completed")
	}
}
