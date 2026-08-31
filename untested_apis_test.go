package claygo

import (
	"slices"
	"strings"
	"testing"
)

// TestGetScrollContainerDataReturnsFields exercises the public scroll
// query: declare a clip container, end layout, look up its data, and
// assert every field of the returned ScrollContainerData is plausible.
func TestGetScrollContainerDataReturnsFields(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Scrollable", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Box(ctx, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(150)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
	ctx.EndLayout(0)

	id := GetElementID("Scrollable")
	sd := ctx.GetScrollContainerData(id)
	if !sd.Found {
		t.Fatalf("expected Found=true for an existing clip element")
	}
	if sd.ScrollContainerDimensions.Width != 120 || sd.ScrollContainerDimensions.Height != 80 {
		t.Errorf("ScrollContainerDimensions = %v, want 120×80", sd.ScrollContainerDimensions)
	}
	if sd.ContentDimensions.Width != 200 || sd.ContentDimensions.Height != 150 {
		t.Errorf("ContentDimensions = %v, want 200×150", sd.ContentDimensions)
	}
	if sd.ScrollPosition == nil {
		t.Error("expected non-nil ScrollPosition pointer")
	}
	if !sd.Config.Horizontal || !sd.Config.Vertical {
		t.Errorf("Config = %+v, want both axes true", sd.Config)
	}

	// An unknown id returns Found=false with zero fields.
	miss := ctx.GetScrollContainerData(GetElementID("Nope"))
	if miss.Found {
		t.Errorf("expected Found=false for unknown id, got %+v", miss)
	}
}

func TestGetScrollOffsetInternalScroll(t *testing.T) {
	ctx := freshContext(t)
	paneID := GetElementID("Pane")
	childID := GetElementID("PaneChild")

	build := func() Vector2 {
		ctx.BeginLayout()
		ctx.OpenElementWithID(paneID)
		offset := ctx.GetScrollOffset()
		ctx.ConfigureOpenElement(Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: offset},
		})
		BoxID(ctx, "PaneChild", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(400)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		ctx.CloseElement()
		ctx.EndLayout(0)
		return offset
	}

	if got := build(); got != (Vector2{}) {
		t.Fatalf("first-frame GetScrollOffset = %v, want zero", got)
	}
	ctx.SetPointerState(Vector2{X: 50, Y: 40}, false)
	ctx.UpdateScrollContainers(false, Vector2{X: 0, Y: -3}, 1.0/60.0)

	offset := build()
	if offset.Y != -30 {
		t.Fatalf("second-frame GetScrollOffset.Y = %v, want -30", offset.Y)
	}
	data := ctx.GetElementData(childID)
	if !data.Found {
		t.Fatalf("child element not found")
	}
	if data.BoundingBox.Y != -30 {
		t.Fatalf("child Y = %v, want -30 from internal scroll offset", data.BoundingBox.Y)
	}
}

func TestExternalScrollHandlingQueriesOffsetWithoutMovingChildren(t *testing.T) {
	ctx := freshContext(t)
	paneID := GetElementID("ExternalPane")
	childID := GetElementID("ExternalChild")
	queryCount := 0
	ctx.SetExternalScrollHandlingEnabled(true)
	ctx.SetQueryScrollOffsetFunction(func(elementID uint32, userData any) Vector2 {
		queryCount++
		if elementID != paneID.ID {
			t.Fatalf("query elementID = %d, want %d", elementID, paneID.ID)
		}
		if userData != "scroll-user-data" {
			t.Fatalf("query userData = %v, want scroll-user-data", userData)
		}
		return Vector2{X: -20, Y: -10}
	}, "scroll-user-data")

	build := func() Vector2 {
		ctx.BeginLayout()
		ctx.OpenElementWithID(paneID)
		offset := ctx.GetScrollOffset()
		ctx.ConfigureOpenElement(Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: offset},
		})
		BoxID(ctx, "ExternalChild", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(400)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		ctx.CloseElement()
		ctx.EndLayout(0)
		return offset
	}

	if got := build(); got != (Vector2{}) {
		t.Fatalf("first-frame external GetScrollOffset = %v, want zero before configure creates scroll data", got)
	}
	if queryCount != 1 {
		t.Fatalf("query count after first frame = %d, want 1", queryCount)
	}
	if got := build(); got != (Vector2{X: -20, Y: -10}) {
		t.Fatalf("second-frame external GetScrollOffset = %v, want {-20 -10}", got)
	}
	if queryCount != 2 {
		t.Fatalf("query count after second frame = %d, want 2", queryCount)
	}
	data := ctx.GetElementData(childID)
	if !data.Found {
		t.Fatalf("external child element not found")
	}
	if data.BoundingBox.X != 0 || data.BoundingBox.Y != 0 {
		t.Fatalf("external child bbox = %v, want origin unchanged by Clip.ChildOffset", data.BoundingBox)
	}
}

// TestUpdateScrollContainersWheel pins the wheel-scroll path: scrollDelta
// translates the innermost pointer-over scroll container by deltaY*10
// (and similarly on x) when content is bigger than the container.
func TestUpdateScrollContainersWheel(t *testing.T) {
	ctx := freshContext(t)

	// Frame 1: declare a scrollable clip with overflowing content.
	ctx.BeginLayout()
	BoxID(ctx, "Pane", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Box(ctx, Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(400)}},
		}, nil)
	})
	ctx.EndLayout(0)

	// Pointer over the pane.
	ctx.SetPointerState(Vector2{X: 50, Y: 40}, false)
	// Wheel scroll delta = (0, -3). Expected: ScrollPosition.Y += -3 * 10
	// = -30, clamped to [-(400-80), 0] = [-320, 0]. -30 is within range.
	ctx.UpdateScrollContainers(false, Vector2{X: 0, Y: -3}, 1.0/60.0)

	sd := ctx.GetScrollContainerData(GetElementID("Pane"))
	if !sd.Found {
		t.Fatalf("Pane not registered as scroll container")
	}
	if sd.ScrollPosition.Y != -30 {
		t.Errorf("ScrollPosition.Y after wheel = %v, want -30", sd.ScrollPosition.Y)
	}

	// Need to re-declare on the next frame to keep the entry alive
	// (UpdateScrollContainers reaps containers that didn't reopen).
	ctx.BeginLayout()
	BoxID(ctx, "Pane", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(400)}}}, nil)
	})
	ctx.EndLayout(0)
	ctx.SetPointerState(Vector2{X: 50, Y: 40}, false)
	// Another wheel tick of -5: -30 + (-50) = -80.
	ctx.UpdateScrollContainers(false, Vector2{X: 0, Y: -5}, 1.0/60.0)
	sd = ctx.GetScrollContainerData(GetElementID("Pane"))
	if sd.ScrollPosition.Y != -80 {
		t.Errorf("ScrollPosition.Y after second wheel = %v, want -80", sd.ScrollPosition.Y)
	}
}

// TestUpdateScrollContainersDragAndMomentum exercises the press-drag-release
// flow: press inside the container, move the pointer, release, observe
// momentum-fueled scroll position on subsequent frames.
func TestUpdateScrollContainersDragAndMomentum(t *testing.T) {
	ctx := freshContext(t)
	build := func() {
		ctx.BeginLayout()
		BoxID(ctx, "Pane", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
		}, func() {
			Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(400)}}}, nil)
		})
		ctx.EndLayout(0)
	}

	build()
	// Frame A: press the pointer over the pane; first call seeds the drag
	// origin without moving anything.
	ctx.SetPointerState(Vector2{X: 60, Y: 40}, true)
	ctx.UpdateScrollContainers(true, Vector2{}, 1.0/60.0)

	build()
	// Frame B: still pressed, pointer moves up by 40 px. Scroll should
	// follow the drag.
	ctx.SetPointerState(Vector2{X: 60, Y: 0}, true)
	ctx.UpdateScrollContainers(true, Vector2{}, 1.0/60.0)
	sd := ctx.GetScrollContainerData(GetElementID("Pane"))
	if sd.ScrollPosition.Y >= 0 {
		t.Errorf("after drag-up, ScrollPosition.Y = %v; want a negative value", sd.ScrollPosition.Y)
	}
	draggedY := sd.ScrollPosition.Y

	// Release: momentum should pick up.
	build()
	ctx.SetPointerState(Vector2{X: 60, Y: 0}, false)
	ctx.UpdateScrollContainers(true, Vector2{}, 1.0/60.0)
	sd = ctx.GetScrollContainerData(GetElementID("Pane"))
	if sd.ScrollPosition.Y == draggedY {
		t.Fatalf("ScrollPosition.Y unchanged on release; want momentum to advance from draggedY=%v", draggedY)
	}
	if sd.ScrollPosition.Y >= draggedY {
		t.Fatalf("ScrollPosition.Y after release = %v, want less than draggedY=%v from release momentum", sd.ScrollPosition.Y, draggedY)
	}
}

// TestCustomElementEmitsCustomCommand pins emission of CUSTOM render commands
// when an element has CustomElementConfig.CustomData set.
func TestCustomElementEmitsCustomCommand(t *testing.T) {
	ctx := freshContext(t)
	customPayload := &struct{ kind string }{kind: "myCustomThing"}
	ctx.BeginLayout()
	Box(ctx, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(50)}},
		BackgroundColor: RGBA(50, 50, 50, 255),
		Custom:          CustomElementConfig{CustomData: customPayload},
	}, nil)
	cmds := ctx.EndLayout(0)

	foundCustom := false
	for i := range cmds.Len() {
		cmd := cmds.Get(i)
		if cmd.CommandType != RenderCommandTypeCustom {
			continue
		}
		foundCustom = true
		if cmd.RenderData.Custom.CustomData != customPayload {
			t.Errorf("CUSTOM command's CustomData = %v, want passthrough of %v", cmd.RenderData.Custom.CustomData, customPayload)
		}
	}
	if !foundCustom {
		t.Errorf("expected at least one CUSTOM render command; got %d total", cmds.Len())
	}
}

// TestPointerCaptureModePassthrough verifies Clay's root-level pointer capture
// behavior: a top floating root with default Capture blocks lower roots, while
// Passthrough lets hits underneath it register too.
func TestPointerCaptureModePassthrough(t *testing.T) {
	run := func(mode PointerCaptureMode) []ElementID {
		ctx := freshContext(t)
		ctx.BeginLayout()
		BoxID(ctx, "Anchor", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(200)}},
		}, func() {
			BoxID(ctx, "LowerFloat", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
				BackgroundColor: RGBA(0, 200, 0, 255),
				Floating: FloatingElementConfig{
					ZIndex:       1,
					AttachTo:     AttachToParent,
					AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
				},
			}, nil)
			BoxID(ctx, "UpperFloat", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
				BackgroundColor: RGBA(200, 0, 0, 255),
				Floating: FloatingElementConfig{
					ZIndex:             2,
					AttachTo:           AttachToParent,
					AttachPoints:       FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
					PointerCaptureMode: mode,
				},
			}, nil)
		})
		ctx.EndLayout(0)
		ctx.SetPointerState(Vector2{X: 50, Y: 50}, false)
		return slices.Clone(ctx.GetPointerOverIds())
	}

	lowerID := GetElementID("LowerFloat").ID
	upperID := GetElementID("UpperFloat").ID
	captured := run(PointerCaptureModeCapture)
	if !hasElementID(captured, upperID) {
		t.Fatalf("capture mode: expected upper floating root hit, got %v", captured)
	}
	if hasElementID(captured, lowerID) {
		t.Fatalf("capture mode: lower floating root should be blocked by upper root, got %v", captured)
	}

	passthrough := run(PointerCaptureModePassthrough)
	if !hasElementID(passthrough, upperID) || !hasElementID(passthrough, lowerID) {
		t.Fatalf("passthrough mode: expected both upper and lower hits, got %v", passthrough)
	}
}

func TestPointerHitsOverflowingChildButRespectsClipAncestor(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "OverflowParent", Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(50), Height: SizingFixed(50)},
			Padding: Padding{Left: 60},
		},
	}, func() {
		BoxID(ctx, "OverflowChild", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(20), Height: SizingFixed(20)}},
		}, nil)
	})
	ctx.EndLayout(0)
	ctx.SetPointerState(Vector2{X: 65, Y: 10}, false)
	if !ctx.PointerOver(GetElementID("OverflowChild")) {
		t.Fatalf("pointer over overflowing child should hit when no clip ancestor blocks it")
	}

	ctx.BeginLayout()
	BoxID(ctx, "ClipParent", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: Vector2{X: 60}},
	}, func() {
		BoxID(ctx, "ClippedChild", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(20), Height: SizingFixed(20)}},
		}, nil)
	})
	ctx.EndLayout(0)
	ctx.SetPointerState(Vector2{X: 65, Y: 10}, false)
	if ctx.PointerOver(GetElementID("ClippedChild")) {
		t.Fatalf("pointer outside clip ancestor should not hit clipped child")
	}
}

func TestFloatingExpandAffectsBoundingBoxAndPointerHit(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "ExpandAnchor", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
	}, func() {
		BoxID(ctx, "ExpandedFloat", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(30)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
			Floating: FloatingElementConfig{
				AttachTo:     AttachToParent,
				AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
				Expand:       Dimensions{Width: 10, Height: 5},
			},
		}, nil)
	})
	ctx.EndLayout(0)

	data := ctx.GetElementData(GetElementID("ExpandedFloat"))
	if !data.Found {
		t.Fatalf("expanded floating element not found")
	}
	want := BoundingBox{X: -10, Y: -5, Width: 70, Height: 40}
	if data.BoundingBox != want {
		t.Fatalf("expanded floating bbox = %v, want %v", data.BoundingBox, want)
	}
	ctx.SetPointerState(Vector2{X: -5, Y: 0}, false)
	if !ctx.PointerOver(GetElementID("ExpandedFloat")) {
		t.Fatalf("pointer inside expanded area should hit floating element")
	}
}

// TestTransitionExitSiblingOrdering ensures SiblingOrdering affects the render
// order of an exiting child relative to siblings that remain under the same
// live parent.
func TestTransitionExitSiblingOrdering(t *testing.T) {
	tests := []struct {
		name     string
		ordering ExitTransitionSiblingOrdering
		want     []uint32
	}{
		{"underneath", ExitTransitionOrderingUnderneathSiblings, []uint32{GetElementID("B").ID, GetElementID("A").ID, GetElementID("C").ID}},
		{"natural", ExitTransitionOrderingNaturalOrder, []uint32{GetElementID("A").ID, GetElementID("B").ID, GetElementID("C").ID}},
		{"above", ExitTransitionOrderingAboveSiblings, []uint32{GetElementID("A").ID, GetElementID("C").ID, GetElementID("B").ID}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := freshContext(t)
			build := func(includeB bool) {
				ctx.BeginLayout()
				BoxID(ctx, "Parent", Decl{
					Layout: LayoutConfig{
						Sizing:          Sizing{Width: SizingFit(0), Height: SizingFixed(40)},
						LayoutDirection: LeftToRight,
					},
					BackgroundColor: RGBA(10, 10, 10, 255),
				}, func() {
					BoxID(ctx, "A", Decl{
						Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
						BackgroundColor: RGBA(255, 0, 0, 255),
					}, nil)
					if includeB {
						BoxID(ctx, "B", Decl{
							Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
							BackgroundColor: RGBA(0, 255, 0, 255),
							Transition: TransitionElementConfig{
								Handler:    keepTransitionRunning,
								Duration:   1,
								Properties: TransitionPropertyX,
								Exit: TransitionExitConfig{
									SetFinalState:   func(initial TransitionData, _ TransitionProperty) TransitionData { return initial },
									SiblingOrdering: tc.ordering,
								},
							},
						}, nil)
					}
					BoxID(ctx, "C", Decl{
						Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
						BackgroundColor: RGBA(0, 0, 255, 255),
					}, nil)
				})
				ctx.EndLayout(0)
			}

			build(true)
			build(false)

			got := rectOrder(ctx.renderCommands.Data[:ctx.renderCommands.Length], map[uint32]bool{
				GetElementID("A").ID: true,
				GetElementID("B").ID: true,
				GetElementID("C").ID: true,
			})
			if !sameUint32s(got, tc.want) {
				t.Fatalf("render order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExitingChildDoesNotSizeOrShiftLiveSiblings(t *testing.T) {
	ctx := freshContext(t)
	build := func(includeB bool) {
		ctx.BeginLayout()
		BoxID(ctx, "ExitSizingParent", Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFit(0), Height: SizingFixed(20)},
				LayoutDirection: LeftToRight,
				ChildGap:        3,
			},
			BackgroundColor: RGBA(20, 20, 20, 255),
		}, func() {
			BoxID(ctx, "ExitSiblingA", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
				BackgroundColor: RGBA(255, 0, 0, 255),
			}, nil)
			if includeB {
				BoxID(ctx, "ExitSiblingB", Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
					BackgroundColor: RGBA(0, 255, 0, 255),
					Transition: TransitionElementConfig{
						Handler:    keepTransitionRunning,
						Duration:   1,
						Properties: TransitionPropertyX,
						Exit: TransitionExitConfig{
							SetFinalState: func(initial TransitionData, _ TransitionProperty) TransitionData { return initial },
						},
					},
				}, nil)
			}
			BoxID(ctx, "ExitSiblingC", Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
				BackgroundColor: RGBA(0, 0, 255, 255),
			}, nil)
		})
		ctx.EndLayout(0)
	}

	build(true)
	build(false)

	parent := ctx.GetElementData(GetElementID("ExitSizingParent"))
	if !parent.Found {
		t.Fatalf("parent element not found")
	}
	if parent.BoundingBox.Width != 23 {
		t.Fatalf("parent width = %v, want 23 from A+C plus one gap", parent.BoundingBox.Width)
	}
	c := ctx.GetElementData(GetElementID("ExitSiblingC"))
	if !c.Found {
		t.Fatalf("live sibling C not found")
	}
	if c.BoundingBox.X != 13 {
		t.Fatalf("live sibling C X = %v, want 13 after A plus one gap", c.BoundingBox.X)
	}
}

func TestTransitionInteractionHandlingPointerHits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handling TransitionInteractionHandlingType
		wantHit  bool
	}{
		{"disable", TransitionDisableInteractionsWhileTransitioningPosition, false},
		{"allow", TransitionAllowInteractionsWhileTransitioningPosition, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := freshContext(t)
			build := func(paddingLeft uint16) {
				ctx.BeginLayout()
				BoxID(ctx, "TransitionHitParent", Decl{
					Layout: LayoutConfig{
						Sizing:  Sizing{Width: SizingFixed(200), Height: SizingFixed(50)},
						Padding: Padding{Left: paddingLeft},
					},
				}, func() {
					BoxID(ctx, "TransitionHitChild", Decl{
						Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(30), Height: SizingFixed(30)}},
						BackgroundColor: RGBA(100, 100, 255, 255),
						Transition: TransitionElementConfig{
							Handler:             keepTransitionRunning,
							Duration:            1,
							Properties:          TransitionPropertyX,
							InteractionHandling: tc.handling,
						},
					}, nil)
				})
				ctx.EndLayout(0)
			}

			build(0)
			build(100)
			if got := ctx.transitionDatas.Get(0).State; got != TransitionStateTransitioning {
				t.Fatalf("transition state = %d, want transitioning", got)
			}
			ctx.SetPointerState(Vector2{X: 5, Y: 5}, false)
			if got := ctx.PointerOver(GetElementID("TransitionHitChild")); got != tc.wantHit {
				t.Fatalf("PointerOver transitioning child = %v, want %v", got, tc.wantHit)
			}
		})
	}
}

// TestTransitionEnterTriggerVariants pins the difference between skipping and
// triggering a child's enter transition when its parent also first appears.
func TestTransitionEnterTriggerVariants(t *testing.T) {
	for _, tc := range []struct {
		trigger   TransitionEnterTriggerType
		wantState TransitionState
		wantCalls int
	}{
		{TransitionEnterSkipOnFirstParentFrame, TransitionStateIdle, 0},
		{TransitionEnterTriggerOnFirstParentFrame, TransitionStateEntering, 1},
	} {
		ctx := freshContext(t)
		calls := 0
		ctx.BeginLayout()
		BoxID(ctx, "Parent", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		}, func() {
			BoxID(ctx, "Child", Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				Transition: TransitionElementConfig{
					Handler: func(TransitionCallbackArguments) bool {
						calls++
						return false
					},
					Duration:   0.5,
					Properties: TransitionPropertyX,
					Enter: TransitionEnterConfig{
						Trigger: tc.trigger,
						SetInitialState: func(target TransitionData, _ TransitionProperty) TransitionData {
							target.BoundingBox.X -= 10
							return target
						},
					},
				},
			}, nil)
		})
		ctx.EndLayout(1.0 / 60.0)
		if ctx.transitionDatas.Length != 1 {
			t.Fatalf("trigger=%d: expected 1 transitionDatas entry, got %d", tc.trigger, ctx.transitionDatas.Length)
		}
		if got := ctx.transitionDatas.Get(0).State; got != tc.wantState {
			t.Fatalf("trigger=%d: transition state = %d, want %d", tc.trigger, got, tc.wantState)
		}
		if calls != tc.wantCalls {
			t.Fatalf("trigger=%d: handler calls = %d, want %d", tc.trigger, calls, tc.wantCalls)
		}
	}
}

func TestTransitionPositionDoesNotRetriggerOnRootResize(t *testing.T) {
	ctx := freshContext(t)
	calls := 0
	build := func(width float32) {
		ctx.SetLayoutDimensions(Dimensions{Width: width, Height: 100})
		ctx.BeginLayout()
		BoxID(ctx, "ResizeParent", Decl{
			Layout: LayoutConfig{
				Sizing:         Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
				ChildAlignment: ChildAlignment{X: AlignXRight},
			},
		}, func() {
			BoxID(ctx, "ResizeChild", Decl{
				Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(50), Height: SizingFixed(50)}},
				Transition: TransitionElementConfig{
					Handler: func(TransitionCallbackArguments) bool {
						calls++
						return false
					},
					Duration:   1,
					Properties: TransitionPropertyX,
				},
			}, nil)
		})
		ctx.EndLayout(1.0 / 60.0)
	}

	build(100)
	if calls != 0 {
		t.Fatalf("initial frame calls = %d, want 0", calls)
	}
	build(200)
	if calls != 0 {
		t.Fatalf("resize frame calls = %d, want 0 because root resize suppresses position transitions", calls)
	}
	if got := ctx.transitionDatas.Get(0).State; got != TransitionStateIdle {
		t.Fatalf("resize frame transition state = %d, want idle", got)
	}
}

func TestAspectRatioWidthScalesAfterYAxisShrink(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "AspectParent", Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFixed(100), Height: SizingFixed(40)},
			LayoutDirection: TopToBottom,
		},
	}, func() {
		BoxID(ctx, "AspectChild", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFit(0)}},
			AspectRatio: AspectRatioElementConfig{
				AspectRatio: 2,
			},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
	ctx.EndLayout(0)

	data := ctx.GetElementData(GetElementID("AspectChild"))
	if !data.Found {
		t.Fatalf("aspect child not found")
	}
	if data.BoundingBox.Width != 80 || data.BoundingBox.Height != 40 {
		t.Fatalf("aspect child bbox = %v, want width=80 height=40", data.BoundingBox)
	}
}

func TestClippedCrossAxisGrowExpandsToInnerContent(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "ClipCrossAxis", Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFixed(100), Height: SizingFixed(100)},
			LayoutDirection: TopToBottom,
		},
		Clip: ClipElementConfig{Horizontal: true},
	}, func() {
		BoxID(ctx, "WideContent", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(10)}},
		}, nil)
		BoxID(ctx, "CrossAxisGrow", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(10)}},
		}, nil)
	})
	ctx.EndLayout(0)

	grow := ctx.GetElementData(GetElementID("CrossAxisGrow"))
	if !grow.Found {
		t.Fatalf("grow child not found")
	}
	if grow.BoundingBox.Width != 200 {
		t.Fatalf("cross-axis grow width = %v, want 200 from clipped inner content", grow.BoundingBox.Width)
	}
}

func TestCurrentContextLowLevelAPIs(t *testing.T) {
	previous := GetCurrentContext()
	defer SetCurrentContext(previous)

	ctx := freshContext(t)
	if GetCurrentContext() != ctx {
		t.Fatalf("Initialize should set current context")
	}
	ctx.BeginLayout()
	OpenElementWithID(GetElementID("LowLevelBox"))
	ConfigureOpenElement(Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(40), Height: SizingFixed(20)}},
		BackgroundColor: RGBA(20, 40, 60, 255),
	})
	CloseElement()
	cmds := ctx.EndLayout(0)

	if findRectCommand(cmds.Commands, GetElementID("LowLevelBox").ID) == nil {
		t.Fatalf("low-level current-context API did not emit rectangle command")
	}
	if RenderCommandArray_Get(&cmds, 0) == nil {
		t.Fatalf("RenderCommandArray_Get returned nil for first command")
	}
	SetPointerState(Vector2{X: 1, Y: 2}, false)
	if got := GetPointerState().Position; got != (Vector2{X: 1, Y: 2}) {
		t.Fatalf("GetPointerState position = %v, want {1 2}", got)
	}
	SetCurrentContext(nil)
	if got := GetPointerState(); got != (PointerData{}) {
		t.Fatalf("nil-current GetPointerState = %+v, want zero", got)
	}
}

func TestPackageMaxDefaultsAffectMinMemorySizeAndInitialize(t *testing.T) {
	previous := GetCurrentContext()
	SetCurrentContext(nil)
	oldMaxElements := GetMaxElementCount()
	oldMaxWords := GetMaxMeasureTextCacheWordCount()
	defer func() {
		SetCurrentContext(nil)
		SetMaxElementCount(oldMaxElements)
		SetMaxMeasureTextCacheWordCount(oldMaxWords)
		SetCurrentContext(previous)
	}()

	base := MinMemorySize()
	SetMaxElementCount(128)
	if got := GetMaxElementCount(); got != 128 {
		t.Fatalf("default max elements = %d, want 128", got)
	}
	if got := GetMaxMeasureTextCacheWordCount(); got != 256 {
		t.Fatalf("default max measure words = %d, want 256", got)
	}
	resized := MinMemorySize()
	if resized >= base {
		t.Fatalf("MinMemorySize after shrinking defaults = %d, want < original %d", resized, base)
	}

	ctx := Initialize(CreateArenaWithCapacity(resized), Dimensions{Width: 100, Height: 100}, ErrorHandler{})
	if ctx.MaxElementCount() != 128 {
		t.Fatalf("initialized max elements = %d, want 128", ctx.MaxElementCount())
	}
	if ctx.MaxMeasureTextCacheWordCount() != 256 {
		t.Fatalf("initialized max measure words = %d, want 256", ctx.MaxMeasureTextCacheWordCount())
	}
}

func TestLocalElementIDHelpersUseOpenElementSeed(t *testing.T) {
	previous := GetCurrentContext()
	defer SetCurrentContext(previous)

	ctx := freshContext(t)
	parentID := GetElementID("LocalParent")
	var methodID ElementID
	var methodIndexedID ElementID
	var packageID ElementID
	var packageIndexedID ElementID
	ctx.BeginLayout()
	BoxID(ctx, "LocalParent", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
	}, func() {
		methodID = ctx.GetElementIDLocal("Child")
		methodIndexedID = ctx.GetElementIDWithIndexLocal("Child", 7)
		packageID = GetElementIDLocal("Child")
		packageIndexedID = GetElementIDWithIndexLocal("Child", 7)
	})
	ctx.EndLayout(0)

	want := HashString(String{Text: "Child"}, parentID.ID)
	wantIndexed := HashStringWithOffset(String{Text: "Child"}, 7, parentID.ID)
	if methodID.ID != want.ID || packageID.ID != want.ID {
		t.Fatalf("local IDs = method %d package %d, want %d", methodID.ID, packageID.ID, want.ID)
	}
	if methodIndexedID.ID != wantIndexed.ID || packageIndexedID.ID != wantIndexed.ID {
		t.Fatalf("local indexed IDs = method %d package %d, want %d", methodIndexedID.ID, packageIndexedID.ID, wantIndexed.ID)
	}
}

func TestSetLayoutDimensionsUpdatesRootResizedImmediately(t *testing.T) {
	ctx := freshContext(t)
	if ctx.RootResizedLastFrame() {
		t.Fatalf("initial root resize flag = true, want false")
	}
	ctx.SetLayoutDimensions(Dimensions{Width: 640, Height: 480})
	if !ctx.RootResizedLastFrame() {
		t.Fatalf("root resize flag was not updated immediately")
	}
	ctx.SetLayoutDimensions(Dimensions{Width: 640, Height: 480})
	if ctx.RootResizedLastFrame() {
		t.Fatalf("root resize flag stayed true after setting same dimensions")
	}
}

func TestMeasureTextEntryCapacityReportsElementsCapacityError(t *testing.T) {
	var errs []ErrorData
	ctx := Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 100, Height: 100}, ErrorHandler{
		Func: func(err ErrorData) { errs = append(errs, err) },
	})
	ctx.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	ctx.measureTextHashMapInternal.Length = ctx.measureTextHashMapInternal.Capacity - 1
	ctx.measureTextCached("hello", &TextElementConfig{FontSize: 12})
	if len(errs) != 1 {
		t.Fatalf("error count = %d, want 1", len(errs))
	}
	if errs[0].Type != ErrorTypeElementsCapacityExceeded {
		t.Fatalf("error type = %d, want elements capacity", errs[0].Type)
	}
}

func TestMeasuredWordsCapacityReportsTextMeasurementError(t *testing.T) {
	var errs []ErrorData
	ctx := Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 100, Height: 100}, ErrorHandler{
		Func: func(err ErrorData) { errs = append(errs, err) },
	})
	ctx.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	ctx.measuredWords.Length = ctx.measuredWords.Capacity - 1
	ctx.measureTextCached("hello world", &TextElementConfig{FontSize: 12})
	if len(errs) != 1 {
		t.Fatalf("error count = %d, want 1", len(errs))
	}
	if errs[0].Type != ErrorTypeTextMeasurementCapacityExceeded {
		t.Fatalf("error type = %d, want text measurement capacity", errs[0].Type)
	}
}

func TestRenderCommandCapacityUsesCapacityMinusOne(t *testing.T) {
	var errs []ErrorData
	ctx := Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 100, Height: 100}, ErrorHandler{
		Func: func(err ErrorData) { errs = append(errs, err) },
	})
	ctx.renderCommands.Length = ctx.renderCommands.Capacity - 1
	ctx.emitCommand(RenderCommand{CommandType: RenderCommandTypeRectangle})
	if ctx.renderCommands.Length != ctx.renderCommands.Capacity-1 {
		t.Fatalf("render command length changed to %d at capacity-1", ctx.renderCommands.Length)
	}
	if len(errs) != 1 || errs[0].Type != ErrorTypeElementsCapacityExceeded {
		t.Fatalf("errors = %+v, want one elements-capacity error", errs)
	}
	if !ctx.warnMaxRenderCommandsExceeded {
		t.Fatalf("render-command warning flag not set")
	}
	if ctx.warnMaxElementsExceeded {
		t.Fatalf("element warning flag should remain false for render-command overflow")
	}
}

func TestFloatingClipSyntheticCommandsMatchOracleFields(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "ClipOwner", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		BoxID(ctx, "FloatingRenderParity", Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(60), Height: SizingFixed(30)},
				LayoutDirection: LeftToRight,
				ChildGap:        4,
			},
			Border: BorderElementConfig{
				Color: RGBA(255, 0, 0, 255),
				Width: BorderAll(2),
			},
			Clip:     ClipElementConfig{Horizontal: true},
			UserData: "float-user-data",
			Floating: FloatingElementConfig{
				AttachTo:     AttachToParent,
				AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
				ClipTo:       ClipToAttachedParent,
				ZIndex:       7,
			},
		}, func() {
			Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
			Box(ctx, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
		})
	})
	cmds := ctx.EndLayout(0).Commands

	floatingID := GetElementID("FloatingRenderParity").ID
	syntheticStartID := HashNumber(floatingID, 12).ID
	syntheticEndID := HashNumber(floatingID, 13).ID
	borderID := HashNumber(2, floatingID).ID
	dividerID := HashNumber(4, floatingID).ID
	foundSyntheticScissorStart := false
	foundBorder := false
	foundDivider := false
	syntheticScissorEndCount := 0
	for _, cmd := range cmds {
		switch {
		case cmd.CommandType == RenderCommandTypeScissorStart && cmd.ID == syntheticStartID:
			foundSyntheticScissorStart = true
			if cmd.BoundingBox != (BoundingBox{X: 0, Y: 0, Width: 100, Height: 100}) {
				t.Fatalf("synthetic floating clip scissor bbox = %v, want clip owner bbox", cmd.BoundingBox)
			}
			if cmd.ZIndex != 7 {
				t.Fatalf("synthetic floating clip scissor zIndex = %d, want 7", cmd.ZIndex)
			}
			if cmd.UserData != nil {
				t.Fatalf("synthetic floating clip scissor userData = %v, want nil", cmd.UserData)
			}
		case cmd.CommandType == RenderCommandTypeBorder && cmd.ID == borderID:
			foundBorder = true
			if cmd.ZIndex != 0 {
				t.Fatalf("border zIndex = %d, want 0", cmd.ZIndex)
			}
		case cmd.CommandType == RenderCommandTypeRectangle && cmd.ID == dividerID:
			foundDivider = true
			if cmd.ZIndex != 0 {
				t.Fatalf("between-child divider zIndex = %d, want 0", cmd.ZIndex)
			}
		case cmd.CommandType == RenderCommandTypeScissorEnd && cmd.ID == syntheticEndID:
			syntheticScissorEndCount++
			if cmd.ZIndex != 0 {
				t.Fatalf("scissor end zIndex = %d, want 0", cmd.ZIndex)
			}
		}
	}
	if !foundSyntheticScissorStart || !foundBorder || !foundDivider || syntheticScissorEndCount != 2 {
		t.Fatalf("missing expected commands: syntheticScissor=%v border=%v divider=%v scissorEndCount=%d", foundSyntheticScissorStart, foundBorder, foundDivider, syntheticScissorEndCount)
	}
}

func TestDebugWarningsPaneRendersWarnings(t *testing.T) {
	ctx := Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 640, Height: 480}, ErrorHandler{Func: func(ErrorData) {}})
	ctx.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	ctx.SetDebugModeEnabled(true)

	ctx.BeginLayout()
	BoxID(ctx, "Duplicate", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
	BoxID(ctx, "Duplicate", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}}}, nil)
	cmds := ctx.EndLayout(0)

	foundHeader := false
	foundWarning := false
	for _, cmd := range cmds.Commands {
		if cmd.CommandType != RenderCommandTypeText {
			continue
		}
		text := cmd.RenderData.Text.StringContents.Text
		if text == "Warnings" {
			foundHeader = true
		}
		if strings.Contains(text, "already declared") {
			foundWarning = true
		}
	}
	if !foundHeader || !foundWarning {
		t.Fatalf("debug warnings pane missing header/warning: header=%v warning=%v", foundHeader, foundWarning)
	}
}

func hasElementID(ids []ElementID, id uint32) bool {
	return slices.IndexFunc(ids, func(got ElementID) bool { return got.ID == id }) >= 0
}

func keepTransitionRunning(TransitionCallbackArguments) bool { return false }

func rectOrder(commands []RenderCommand, include map[uint32]bool) []uint32 {
	out := []uint32{}
	for i := range commands {
		cmd := commands[i]
		if cmd.CommandType == RenderCommandTypeRectangle && include[cmd.ID] {
			out = append(out, cmd.ID)
		}
	}
	return out
}

func sameUint32s(a, b []uint32) bool {
	return slices.Equal(a, b)
}

// TestGetOpenElementIDOutsideFrame pins the documented "zero ElementID
// outside of a frame" guard: between EndLayout and the next BeginLayout,
// GetOpenElementID must return a zero ElementID.
func TestGetOpenElementIDOutsideFrame(t *testing.T) {
	ctx := freshContext(t)
	// Before the first frame.
	if got := ctx.GetOpenElementID(); got.ID != 0 {
		t.Errorf("before first frame: GetOpenElementID = %+v, want zero", got)
	}
	ctx.BeginLayout()
	BoxID(ctx, "Foo", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(10), Height: SizingFixed(10)}},
	}, func() {
		if got := ctx.GetOpenElementID(); got.ID != GetElementID("Foo").ID {
			t.Errorf("inside Foo closure: GetOpenElementID = %d, want %d", got.ID, GetElementID("Foo").ID)
		}
	})
	ctx.EndLayout(0)
	// After EndLayout, no element should be open.
	if got := ctx.GetOpenElementID(); got.ID != 0 {
		t.Errorf("after EndLayout: GetOpenElementID = %+v, want zero", got)
	}
}
