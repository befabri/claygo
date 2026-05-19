package claygo

import (
	"testing"
)

// transitions_multi_exit_test.go exercises cloneElementsWithExitTransition:
// when a parent element with exit-transitioned children disappears, the
// entire subtree must still appear in the next frame's render commands
// (driving each element's exit handler one more time before they vanish).
//
// The single-element exit path is covered by
// TestTransitionsExitTriggersWhenElementRemoved in transitions_test.go.

// TestTransitionsMultiElementSubtreeExit declares a parent with one child in
// frame 1 (both registered for exit transitions), then in frame 2 declares
// neither. The clone pass must surface both as render commands.
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
				Transition: TransitionElementConfig{
					Handler:    linearXInterpolator,
					Duration:   1.0,
					Properties: TransitionPropertyX,
					Exit: TransitionExitConfig{
						SetFinalState: exitSlideOff,
					},
				},
			}, nil)
		})
	}

	// Frame 1: both elements present. Two transitionData entries register.
	ctx.BeginLayout()
	declareSubtree()
	ctx.EndLayout(0.1)

	if got := ctx.transitionDatas.Length; got != 2 {
		t.Fatalf("after frame 1 transitionDatas.Length = %d, want 2", got)
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

	// Frame 2: neither element declared. Both must enter EXITING state and
	// the clone pass must place both back into render commands.
	ctx.BeginLayout()
	// (no declareSubtree)
	ctx.EndLayout(0.1)

	if got := ctx.transitionDatas.Length; got != 2 {
		t.Fatalf("after frame 2 transitionDatas.Length = %d, want 2 (both still exiting)", got)
	}
	for i := int32(0); i < ctx.transitionDatas.Length; i++ {
		td := ctx.transitionDatas.Get(i)
		if td.State != TransitionStateExiting {
			t.Errorf("frame 2 transitionData[%d].State = %d, want EXITING", i, td.State)
		}
		if !td.TransitionOut {
			t.Errorf("frame 2 transitionData[%d].TransitionOut = false, want true", i)
		}
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

	// Both clones should be reachable as floating-attached tree roots
	// anchored to the auto-root, so they preserve their previous on-screen
	// positions instead of stacking at (0,0). The parent in frame 1 sits at
	// X=0; with the exit animation having just started (ElapsedTime = 0 for
	// the first interpolator call) the rendered X should still be near 0.
	if parentCmd != nil && (parentCmd.BoundingBox.Width <= 0 || parentCmd.BoundingBox.Height <= 0) {
		t.Errorf("frame 2: parent clone has degenerate size %v", parentCmd.BoundingBox)
	}
	if childCmd != nil && (childCmd.BoundingBox.Width <= 0 || childCmd.BoundingBox.Height <= 0) {
		t.Errorf("frame 2: child clone has degenerate size %v", childCmd.BoundingBox)
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
	for i := 0; i < 10; i++ {
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
