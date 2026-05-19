package claygo

import (
	"testing"
)

// transitions_test.go exercises the cross-frame transition state machine.
// Each test drives BeginLayout / EndLayout multiple times against a single
// Context (mimicking real frame loops). The deterministic measurer from
// measure_test.go::freshContext keeps text measurement reproducible.

// linearXInterpolator is a minimal transition handler that linearly
// interpolates the X axis from initial to target over its full duration.
// Follows the Go port's handler convention (see setters.go::EaseOut):
// returns true while STILL PROGRESSING, false once the transition is done.
func linearXInterpolator(args TransitionCallbackArguments) bool {
	if args.Duration <= 0 {
		return false
	}
	t := args.ElapsedTime / args.Duration
	if t >= 1 {
		t = 1
	}
	if args.Current != nil && args.Properties&TransitionPropertyX != 0 {
		args.Current.BoundingBox.X = args.Initial.BoundingBox.X +
			(args.Target.BoundingBox.X-args.Initial.BoundingBox.X)*t
	}
	return t < 1
}

// findRectCommand returns the (first) RECTANGLE command for the element with
// the given id, or nil if none.
func findRectCommand(commands []RenderCommand, id uint32) *RenderCommand {
	for i := range commands {
		if commands[i].CommandType == RenderCommandTypeRectangle && commands[i].ID == id {
			return &commands[i]
		}
	}
	return nil
}

// TestTransitionsRegisterEntry: declaring an element with a transition
// handler must create exactly one transitionDatas entry, keyed on the
// element's id, with parent recorded correctly.
func TestTransitionsRegisterEntry(t *testing.T) {
	ctx := freshContext(t)

	ctx.BeginLayout()
	BoxID(ctx, "Item", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		Transition: TransitionElementConfig{
			Handler:    linearXInterpolator,
			Duration:   1.0,
			Properties: TransitionPropertyX,
		},
	}, nil)
	ctx.EndLayout(0)

	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("transitionDatas.Length after frame 1 = %d, want 1", got)
	}
	td := ctx.transitionDatas.Get(0)
	wantID := HashString(String{Text: "Item"}, 0).ID
	if td.ElementID != wantID {
		t.Errorf("transition.ElementID = %d, want %d", td.ElementID, wantID)
	}
	rootID := HashString(String{Text: rootElementIDString}, 0).ID
	if td.ParentID != rootID {
		t.Errorf("transition.ParentID = %d, want %d (root)", td.ParentID, rootID)
	}
}

// TestTransitionsInterpolatesXAcrossFrames is the canonical multi-frame test:
// frame 1 declares an Item at X=0, frame 2 declares the same Item at a new
// X position (kicking off the transition; handler runs with elapsed=0, so
// the bbox stays at the start), frame 3 holds the new layout and advances
// time so the handler interpolates partway. The final bbox X must be
// strictly between the frame-1 and frame-2 positions.
//
// This mirrors upstream's "elapsed advanced after handler" ordering at
// oracle/clay.h ~line 4707, which means a single delta-step worth of
// transition progress is only visible on the frame AFTER the target change.
func TestTransitionsInterpolatesXAcrossFrames(t *testing.T) {
	ctx := freshContext(t)

	// frameRender wraps a single layout pass. The "outer" container's
	// padding shifts where "Item" lands on the X axis.
	frameRender := func(outerPaddingLeft uint16, deltaTime float32) {
		ctx.BeginLayout()
		Box(ctx, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(800), Height: SizingFixed(400)},
				Padding:         Padding{Left: outerPaddingLeft},
				LayoutDirection: LeftToRight,
			},
		}, func() {
			BoxID(ctx, "Item", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				BackgroundColor: RGBA(255, 0, 0, 255),
				Transition: TransitionElementConfig{
					Handler:    linearXInterpolator,
					Duration:   1.0,
					Properties: TransitionPropertyX,
				},
			}, nil)
		})
		ctx.EndLayout(deltaTime)
	}

	// Frame 1: outer padding = 0 → Item.X = 0.
	frameRender(0, 0.5)
	td := ctx.transitionDatas.Get(0)
	if td.State != TransitionStateIdle {
		t.Fatalf("after frame 1 state = %d, want IDLE", td.State)
	}

	// Frame 2: outer padding = 100 → Item.X target = 100. This triggers
	// TRANSITIONING. The handler is called once with ElapsedTime=0, so the
	// rendered bbox.X stays at 0 (the initial state).
	frameRender(100, 0.5)
	td = ctx.transitionDatas.Get(0)
	if td.State != TransitionStateTransitioning {
		t.Fatalf("after frame 2 state = %d, want TRANSITIONING", td.State)
	}

	// Frame 3: same target, deltaTime=0 so we don't push past the duration.
	// Handler is called with ElapsedTime=0.5, Duration=1.0 → t=0.5 → bbox.X
	// lands at midpoint 50.
	frameRender(100, 0)

	itemID := HashString(String{Text: "Item"}, 0).ID
	commands := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	cmd := findRectCommand(commands, itemID)
	if cmd == nil {
		t.Fatalf("no RECTANGLE command emitted for Item")
	}
	x := cmd.BoundingBox.X
	if x <= 0.001 || x >= 99.999 {
		t.Errorf("Item bbox.X after frame 3 = %f, want strictly between 0 and 100", x)
	}
	if x < 49.5 || x > 50.5 {
		t.Errorf("Item bbox.X after frame 3 = %f, want ~50 (linear midpoint)", x)
	}
}

// TestTransitionsEaseOutProgressesToTarget drives several frames using the
// production EaseOut handler from setters.go. After enough elapsed time the
// state machine must retire to IDLE and the bbox must reach the target.
func TestTransitionsEaseOutProgressesToTarget(t *testing.T) {
	ctx := freshContext(t)

	frameRender := func(outerPaddingLeft uint16, deltaTime float32) {
		ctx.BeginLayout()
		Box(ctx, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(800), Height: SizingFixed(400)},
				Padding:         Padding{Left: outerPaddingLeft},
				LayoutDirection: LeftToRight,
			},
		}, func() {
			BoxID(ctx, "Item", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				BackgroundColor: RGBA(0, 200, 0, 255),
				Transition: TransitionElementConfig{
					Handler:    EaseOut,
					Duration:   0.5,
					Properties: TransitionPropertyX,
				},
			}, nil)
		})
		ctx.EndLayout(deltaTime)
	}

	// Frame 1: target X = 0; sets up baseline.
	frameRender(0, 0)
	// Frame 2: target moves to X = 200. Handler runs with elapsed=0 (bbox
	// still at 0), state enters TRANSITIONING. Elapsed will be advanced to 0.1.
	frameRender(200, 0.1)
	// Frame 3: same target, advances time so we land mid-transition.
	frameRender(200, 0.1)
	itemID := HashString(String{Text: "Item"}, 0).ID
	commands := ctx.renderCommands.Data[:ctx.renderCommands.Length]
	cmd := findRectCommand(commands, itemID)
	if cmd == nil {
		t.Fatalf("no RECTANGLE command emitted for Item mid-transition")
	}
	mid := cmd.BoundingBox.X
	if mid <= 0 || mid >= 200 {
		t.Errorf("Item bbox.X mid-transition = %f, want 0 < X < 200", mid)
	}

	// Continue advancing time without changing layout until the transition
	// completes. We give it many frames to ensure even ease-out tail reaches
	// IDLE within numeric tolerance.
	for i := 0; i < 30; i++ {
		frameRender(200, 0.05)
		td := ctx.transitionDatas.Get(0)
		if td.State == TransitionStateIdle {
			break
		}
	}
	commands = ctx.renderCommands.Data[:ctx.renderCommands.Length]
	cmd = findRectCommand(commands, itemID)
	if cmd == nil {
		t.Fatalf("no RECTANGLE command emitted for Item after transition")
	}
	if cmd.BoundingBox.X < 199.5 || cmd.BoundingBox.X > 200.5 {
		t.Errorf("Item bbox.X after transition completes = %f, want ~200", cmd.BoundingBox.X)
	}
	td := ctx.transitionDatas.Get(0)
	if td.State != TransitionStateIdle {
		t.Errorf("transition.State after completion = %d, want IDLE", td.State)
	}
}

// TestTransitionsExitTriggersWhenElementRemoved verifies that an element
// declared with transition.exit.setFinalState in frame 1 — but absent from
// frame 2 — runs at least one frame of exit transition (i.e. its
// transitionData entry survives past the removal, in state EXITING).
func TestTransitionsExitTriggersWhenElementRemoved(t *testing.T) {
	ctx := freshContext(t)

	declareItem := func() {
		BoxID(ctx, "Disappearing", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(50, 50, 200, 255),
			Transition: TransitionElementConfig{
				Handler:    linearXInterpolator,
				Duration:   1.0,
				Properties: TransitionPropertyX,
				Exit: TransitionExitConfig{
					SetFinalState: func(initial TransitionData, _ TransitionProperty) TransitionData {
						// Slide off-screen to the left.
						initial.BoundingBox.X = -200
						return initial
					},
				},
			},
		}, nil)
	}

	// Frame 1: element present.
	ctx.BeginLayout()
	declareItem()
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 1 transitionDatas.Length = %d, want 1", got)
	}

	// Frame 2: element absent. The exit transition must have fired and
	// retained the entry in EXITING state for this frame.
	ctx.BeginLayout()
	// no declareItem here
	ctx.EndLayout(0.1)
	if got := ctx.transitionDatas.Length; got != 1 {
		t.Fatalf("after frame 2 transitionDatas.Length = %d, want 1 (still exiting)", got)
	}
	td := ctx.transitionDatas.Get(0)
	if td.State != TransitionStateExiting {
		t.Errorf("after frame 2 transition.State = %d, want EXITING", td.State)
	}
	if !td.TransitionOut {
		t.Errorf("after frame 2 transition.TransitionOut = false, want true")
	}
}

// TestTransitionsScenesWithoutHandlerUnchanged exercises the
// non-regression guarantee: an element WITHOUT a Transition.Handler must
// never produce a transitionDatas entry, regardless of how many frames it
// runs for. This is the cheap end of the "existing scenes don't change"
// constraint.
func TestTransitionsScenesWithoutHandlerUnchanged(t *testing.T) {
	ctx := freshContext(t)
	for i := 0; i < 3; i++ {
		ctx.BeginLayout()
		Box(ctx, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(80, 80, 80, 255),
		}, nil)
		ctx.EndLayout(0.016)
	}
	if got := ctx.transitionDatas.Length; got != 0 {
		t.Errorf("transitionDatas.Length after non-transition scene = %d, want 0", got)
	}
}
