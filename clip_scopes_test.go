package claygo

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestClipScopesInspector(t *testing.T) {
	c := freshContext(t)
	c.SetDebugModeEnabled(true)
	c.debugSelectedElementID = GetElementID("front").ID
	viewport := GetElementIDWithIndex("viewport", 2)
	missing := GetElementID("removed")
	for frame := range 2 {
		c.BeginLayout()
		scopes := []ClipScope{
			{ElementID: ElementID{ID: viewport.ID}, Horizontal: true},
			{ElementID: missing, Vertical: true},
			{ElementID: ElementID{ID: 4294967295}, Horizontal: true, Vertical: true},
			{ElementID: GetElementID("disabled")},
		}
		BoxID(c, "front", Decl{
			Layout:   clipTestLayout(100, 100),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes},
		}, nil)
		// The inspector must read the copied configuration, not the caller's
		// reused slice, and resolve names of targets declared after this root.
		clear(scopes)
		BoxIDOffset(c, "viewport", 2, Decl{Layout: clipTestLayout(100, 100)}, nil)
		if frame == 0 {
			BoxID(c, "removed", Decl{Layout: clipTestLayout(100, 100)}, nil)
		}
		c.EndLayout(0)
		texts := clipScopesInspectorText(t, c)
		removed := "Target: removed (" + strconv.FormatUint(uint64(missing.ID), 10) + ")"
		if frame == 1 {
			removed += " [missing]"
		}
		want := []string{
			"Clip Scopes",
			"Target: viewport (" + strconv.FormatUint(uint64(viewport.ID), 10) + ")",
			"Horizontal: true, Vertical: false",
			removed,
			"Horizontal: false, Vertical: true",
			"Target: 4294967295 [missing]",
			"Horizontal: true, Vertical: true",
		}
		start := slices.Index(texts, "Clip Scopes")
		if start < 0 || !slices.Equal(texts[start:], want) {
			t.Fatalf("frame %d: inspector scopes = %q, want %q", frame, texts, want)
		}
	}
}

func TestClipScopesDisabledInspectorIdentity(t *testing.T) {
	c := freshContext(t)
	c.SetDebugModeEnabled(true)
	c.debugSelectedElementID = GetElementID("front").ID
	var baseline []string
	for i, scopes := range [][]ClipScope{nil, {}, {{ElementID: GetElementID("disabled")}}} {
		runDebugFrame(c, func() {
			BoxID(c, "front", Decl{
				Layout:   clipTestLayout(100, 100),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes},
			}, nil)
		})
		texts := clipScopesInspectorText(t, c)
		if i == 0 {
			baseline = texts
		}
		if slices.Contains(texts, "Clip Scopes") || !slices.Equal(texts, baseline) {
			t.Fatalf("disabled scopes changed the inspector: %q", texts)
		}
	}
}

func TestClipScopesInspectorReportsCapacityFailure(t *testing.T) {
	var errors []ErrorData
	c := clipTestLimitedContext(t, 1024, func(e ErrorData) { errors = append(errors, e) })
	c.SetMeasureTextFunction(deterministicMeasureTextForTest, nil)
	c.SetDebugModeEnabled(true)
	c.debugSelectedElementID = GetElementID("front").ID
	scopes := make([]ClipScope, 1025)
	for i := range scopes {
		scopes[i].Horizontal = true
	}
	runDebugFrame(c, func() {
		BoxID(c, "front", Decl{Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes}}, nil)
	})
	if len(errors) != 1 || !slices.Contains(clipScopesInspectorText(t, c), "Hidden: clip-scope capacity exceeded") {
		t.Fatalf("capacity failure was not diagnosed: %v", errors)
	}
}

func TestClipScopesOverflowStateLifetime(t *testing.T) {
	for _, redeclare := range []bool{false, true} {
		t.Run(strconv.FormatBool(redeclare), func(t *testing.T) {
			var errors []ErrorData
			c := clipTestLimitedContext(t, 32, func(e ErrorData) { errors = append(errors, e) })
			scopes := make([]ClipScope, 33)
			for i := range scopes {
				scopes[i].Horizontal = true
			}
			for frame := range 3 {
				c.BeginLayout()
				if frame == 0 || redeclare {
					BoxID(c, "front", Decl{
						Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
						Floating:   FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes},
						Transition: clipTestExitConfig(),
					}, nil)
				}
				commands := c.EndLayout(0)
				want := frame > 0 && redeclare
				if clipPaintsAt(t, commands, GetElementID("front"), Vector2{10, 10}) != want {
					t.Fatalf("frame %d: overflow state must survive exit clones and reset on fresh declarations", frame)
				}
				scopes = nil
			}
			if len(errors) != 1 || !strings.Contains(errors[0].Text, "clip-scope") {
				t.Fatalf("unexpected errors: %v", errors)
			}
		})
	}
}

// Inspect the actual selected Floating section, including its scrolled-out
// rows, so this regression does not depend on the inspector's viewport height.
func clipScopesInspectorText(t *testing.T, c *Context) []string {
	t.Helper()
	id := HashStringWithOffset(String{Text: "Clay__Debug_InspFloating"}, GetElementID("front").ID, 0)
	item := c.getHashMapItem(id.ID)
	if item == nil || item.Generation <= c.generation {
		t.Fatal("selected floating inspector section was not declared")
	}
	var texts []string
	var visit func(*LayoutElement)
	visit = func(element *LayoutElement) {
		if element.IsTextElement {
			texts = append(texts, element.TextElementData.Text.Text)
			return
		}
		for _, idx := range element.Children.Data[:element.Children.Length] {
			visit(c.layoutElements.Get(idx))
		}
	}
	visit(item.LayoutElement)
	return texts
}

func clipTestScope(c *Context, name string, box BoundingBox, z int16) {
	BoxID(c, name, Decl{
		Layout: clipTestLayout(box.Width, box.Height),
		Floating: FloatingElementConfig{
			AttachTo: AttachToRoot, Offset: Vector2{box.X, box.Y}, ZIndex: z,
			PointerCaptureMode: PointerCaptureModePassthrough,
		},
	}, nil)
}

func TestClipScopesApplyBeforeHoverAndCapture(t *testing.T) {
	for _, mode := range []struct {
		name    string
		capture PointerCaptureMode
	}{
		{"capture", PointerCaptureModeCapture},
		{"passthrough", PointerCaptureModePassthrough},
	} {
		t.Run(mode.name, func(t *testing.T) {
			c := freshContext(t)
			hovers := 0
			scopes := []ClipScope{
				{ElementID: GetElementID("horizontal"), Horizontal: true},
				{ElementID: GetElementID("vertical"), Vertical: true},
			}
			c.BeginLayout()
			BoxID(c, "behind", Decl{Layout: clipTestLayout(100, 100)}, nil)
			BoxID(c, "front", Decl{
				Layout:   clipTestLayout(100, 100),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ZIndex: 2, PointerCaptureMode: mode.capture, ClipScopes: scopes},
				Custom:   CustomElementConfig{CustomData: "mesh"},
			}, func() {
				c.OnHover(func(ElementID, PointerData, any) { hovers++ }, nil)
				BoxID(c, "child", Decl{Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255)}, nil)
			})
			// Both targets are declared and painted later. Configuration must
			// own its input before a caller changes the slice.
			scopes[0] = ClipScope{}
			scopes[1].ElementID = GetElementID("missing")
			clipTestScope(c, "horizontal", BoundingBox{20, 0, 40, 100}, 10)
			clipTestScope(c, "vertical", BoundingBox{0, 10, 100, 20}, 10)
			commands := c.EndLayout(0)
			for _, test := range []struct {
				point   Vector2
				visible bool
			}{
				{Vector2{10, 20}, false}, {Vector2{60, 20}, false},
				{Vector2{30, 9}, false}, {Vector2{30, 30}, false},
				{Vector2{20, 10}, true}, {Vector2{59, 29}, true},
			} {
				before := hovers
				c.SetPointerState(test.point, true)
				for _, id := range []string{"front", "child"} {
					if got := c.PointerOver(GetElementID(id)); got != test.visible {
						t.Errorf("%s at %v: pointer=%v, want %v", id, test.point, got, test.visible)
					}
					if got := clipPaintsAt(t, commands, GetElementID(id), test.point); got != test.visible {
						t.Errorf("%s at %v: paint=%v, want %v", id, test.point, got, test.visible)
					}
				}
				if (hovers == before+1) != test.visible {
					t.Errorf("hover callback at %v: count changed from %d to %d", test.point, before, hovers)
				}
				wantBehind := !test.visible || mode.capture == PointerCaptureModePassthrough
				if got := c.PointerOver(GetElementID("behind")); got != wantBehind {
					t.Errorf("behind at %v: pointer=%v, want %v", test.point, got, wantBehind)
				}
			}
			if got := c.PointerState().State; got != PointerDataPressed {
				t.Fatalf("clipping changed the button state machine: %v", got)
			}
			c.SetPointerState(Vector2{1, 1}, false)
			if got := c.PointerState().State; got != PointerDataReleasedThisFrame {
				t.Fatalf("clipped pointer did not release: %v", got)
			}
		})
	}
}

func TestClipScopesUseCurrentGeometryAndForgetMissingTargets(t *testing.T) {
	c := freshContext(t)
	for _, frame := range []struct {
		width   float32
		present bool
	}{
		{80, true}, {35, true}, {80, false}, {60, true},
	} {
		c.BeginLayout()
		BoxID(c, "front", Decl{
			Layout: clipTestLayout(frame.width, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{
				{ElementID: GetElementID("front"), Horizontal: true},
				{ElementID: GetElementID("later"), Vertical: true},
			}},
		}, nil)
		if frame.present {
			clipTestScope(c, "later", BoundingBox{0, 0, 100, frame.width / 2}, 10)
		}
		commands := c.EndLayout(0)
		for _, point := range []Vector2{{10, 10}, {10, 20}, {50, 25}, {75, 39}} {
			want := frame.present && point.X < frame.width && point.Y < frame.width/2
			c.SetPointerState(point, false)
			if got := c.PointerOver(GetElementID("front")); got != want {
				t.Errorf("frame %+v, pointer %v: hit=%v, want %v", frame, point, got, want)
			}
			if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != want {
				t.Errorf("frame %+v, pointer %v: paint=%v, want %v", frame, point, got, want)
			}
		}
	}
}

func TestClipScopesRespectAxesForOffscreenAndEmptyTargets(t *testing.T) {
	for _, test := range []struct {
		name                          string
		box                           BoundingBox
		horizontal, vertical, visible bool
	}{
		{"offscreen-restricted-axis", BoundingBox{2000, 0, 100, 100}, true, false, false},
		{"offscreen-unrestricted-axis", BoundingBox{2000, 0, 100, 100}, false, true, true},
		{"empty-restricted-axis", BoundingBox{0, 0, 0, 100}, true, false, false},
		{"empty-unrestricted-axis", BoundingBox{0, 0, 0, 100}, false, true, true},
		{"zero-id", BoundingBox{}, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := freshContext(t)
			for _, culling := range []bool{true, false} {
				c.SetCullingEnabled(culling)
				c.BeginLayout()
				id := GetElementID("scope")
				if test.name == "zero-id" {
					id = ElementID{}
				} else {
					clipTestScope(c, "scope", test.box, 0)
				}
				BoxID(c, "front", Decl{
					Layout: clipTestLayout(100, 100), Image: ImageElementConfig{ImageData: "image"},
					Floating: FloatingElementConfig{AttachTo: AttachToRoot, ZIndex: 2, ClipScopes: []ClipScope{{id, test.horizontal, test.vertical}}},
					Clip:     ClipElementConfig{Vertical: true},
				}, nil)
				commands := c.EndLayout(0)
				point := Vector2{10, 10}
				c.SetPointerState(point, false)
				if got := c.PointerOver(GetElementID("front")); got != test.visible {
					t.Errorf("culling=%v: hit=%v, want %v", culling, got, test.visible)
				}
				if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != test.visible {
					t.Errorf("culling=%v: paint=%v, want %v", culling, got, test.visible)
				}
			}
		})
	}
}

func TestClipScopesDisabledIdentityAndFrameReset(t *testing.T) {
	c := freshContext(t)
	var baseline goldenArray
	for i, scopes := range [][]ClipScope{nil, {{ElementID: GetElementID("missing")}}, {{ElementID: GetElementID("missing"), Vertical: true}}, nil} {
		c.BeginLayout()
		BoxID(c, "front", Decl{
			Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes},
		}, nil)
		commands := c.EndLayout(0)
		if i == 0 {
			baseline = toGoldenJSON(commands)
		} else if i != 2 && !reflect.DeepEqual(toGoldenJSON(commands), baseline) {
			t.Errorf("frame %d with no active scopes changed the render stream", i)
		}
		point := Vector2{10, 10}
		c.SetPointerState(point, false)
		if got := c.PointerOver(GetElementID("front")); got != (i != 2) {
			t.Errorf("frame %d: pointer=%v", i, got)
		}
		if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != (i != 2) {
			t.Errorf("frame %d: paint=%v", i, got)
		}
	}
	// A floating-only field must not affect an ordinary box, or consume the
	// scope budget just because the caller reuses a declaration.
	c.BeginLayout()
	BoxID(c, "ordinary", Decl{
		Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
		Floating: FloatingElementConfig{ClipScopes: []ClipScope{{ElementID: GetElementID("missing"), Vertical: true}}},
	}, nil)
	commands := c.EndLayout(0)
	if !clipPaintsAt(t, commands, GetElementID("ordinary"), Vector2{10, 10}) || c.clipScopes.Length != 0 {
		t.Fatal("non-floating configuration applied scopes")
	}
}

func TestClipScopeContains(t *testing.T) {
	for _, axes := range []ClipScope{{}, {Horizontal: true}, {Vertical: true}, {Horizontal: true, Vertical: true}} {
		for _, test := range []struct {
			point Vector2
			x, y  bool
		}{
			{Vector2{10, 20}, true, true}, {Vector2{39, 59}, true, true},
			{Vector2{40, 30}, false, true}, {Vector2{20, 60}, true, false},
			{Vector2{9, 30}, false, true}, {Vector2{20, 19}, true, false},
		} {
			want := (!axes.Horizontal || test.x) && (!axes.Vertical || test.y)
			if got := axes.Contains(BoundingBox{10, 20, 30, 40}, test.point); got != want {
				t.Errorf("axes %+v, point %v: %v, want %v", axes, test.point, got, want)
			}
		}
	}
}

func TestClipScopesRemainActiveWithExternalScrollingAndClipToNone(t *testing.T) {
	for _, inherit := range []FloatingClipToElement{ClipToNone, ClipToAttachedParent} {
		c := freshContext(t)
		c.SetExternalScrollHandlingEnabled(true)
		c.SetQueryScrollOffsetFunction(func(uint32, any) Vector2 { return Vector2{} }, nil)
		c.BeginLayout()
		BoxID(c, "viewport", Decl{
			Layout: clipTestLayout(30, 30),
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: Vector2{10, 15}},
		}, func() {
			BoxID(c, "front", Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: inherit, ClipScopes: []ClipScope{
					{ElementID: GetElementID("scope"), Horizontal: true, Vertical: true},
				}},
			}, nil)
		})
		clipTestScope(c, "scope", BoundingBox{0, 0, 80, 80}, 10)
		commands := c.EndLayout(0)
		box := c.GetElementData(GetElementID("front")).BoundingBox
		wantPosition := Vector2{}
		if inherit == ClipToAttachedParent {
			wantPosition = Vector2{10, 15}
		}
		if box.X != wantPosition.X || box.Y != wantPosition.Y {
			t.Errorf("inherit=%v: host offset applied incorrectly: %v", inherit, box)
		}
		// The host owns native pointer clipping, but explicit scopes are still
		// enforced before capture. Rendering keeps both native and extra clips.
		for _, point := range []Vector2{{20, 20}, {50, 50}, {90, 90}} {
			c.SetPointerState(point, false)
			wantHit := point.X < 80 && point.Y < 80
			if got := c.PointerOver(GetElementID("front")); got != wantHit {
				t.Errorf("inherit=%v at %v: hit=%v, want %v", inherit, point, got, wantHit)
			}
			wantPaint := wantHit && (inherit == ClipToNone || point.X < 30 && point.Y < 30)
			if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != wantPaint {
				t.Errorf("inherit=%v at %v: paint=%v, want %v", inherit, point, got, wantPaint)
			}
		}
	}
}

func FuzzClipScopesPaintMatchesPointer(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint8(30), uint8(40), uint8(3), uint8(30), uint8(10))
	f.Add(uint8(20), uint8(10), uint8(40), uint8(20), uint8(1), uint8(20), uint8(90))
	f.Add(uint8(0), uint8(0), uint8(0), uint8(100), uint8(2), uint8(20), uint8(20))
	f.Add(uint8(10), uint8(10), uint8(20), uint8(20), uint8(0), uint8(99), uint8(99))
	f.Fuzz(func(t *testing.T, x, y, width, height, axes, px, py uint8) {
		c := clipTestLimitedContext(t, 32, func(e ErrorData) { t.Fatalf("layout error: %v", e) })
		box := BoundingBox{float32(x), float32(y), float32(width), float32(height)}
		point := Vector2{float32(px % 100), float32(py % 100)}
		c.BeginLayout()
		BoxID(c, "front", Decl{
			Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{
				{ElementID: GetElementID("target"), Horizontal: axes&1 != 0, Vertical: axes&2 != 0},
			}},
		}, nil)
		clipTestScope(c, "target", box, 10)
		commands := c.EndLayout(0)
		c.SetPointerState(point, false)
		paint := clipPaintsAt(t, commands, GetElementID("front"), point)
		if hit := c.PointerOver(GetElementID("front")); hit != paint {
			t.Fatalf("axes=%d, box=%v, point=%v: paint=%v, hit=%v", axes, box, point, paint, hit)
		}
	})
}

func TestClipScopesUseTransitionedSelfAndLaterTarget(t *testing.T) {
	c := freshContext(t)
	for _, width := range []float32{40, 100} {
		c.BeginLayout()
		BoxID(c, "front", Decl{
			Layout: clipTestLayout(width, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{
				{ElementID: GetElementID("front"), Horizontal: true},
				{ElementID: GetElementID("target"), Vertical: true},
			}},
			Transition: TransitionElementConfig{
				Handler: keepTransitionRunning, Duration: 1, Properties: TransitionPropertyWidth,
				InteractionHandling: TransitionAllowInteractionsWhileTransitioningPosition,
			},
		}, nil)
		BoxID(c, "target", Decl{
			Layout:   clipTestLayout(100, width),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ZIndex: 10, PointerCaptureMode: PointerCaptureModePassthrough},
			Transition: TransitionElementConfig{
				Handler: keepTransitionRunning, Duration: 1, Properties: TransitionPropertyHeight,
			},
		}, nil)
		commands := c.EndLayout(0)
		if got := c.GetElementData(GetElementID("front")).BoundingBox.Width; got != 40 {
			t.Fatalf("fixture width=%v; wanted the initial/intermediate width 40", got)
		}
		for _, test := range []struct {
			point   Vector2
			visible bool
		}{
			{Vector2{10, 10}, true}, {Vector2{50, 10}, false}, {Vector2{10, 50}, false},
		} {
			c.SetPointerState(test.point, false)
			if got := c.PointerOver(GetElementID("front")); got != test.visible {
				t.Errorf("target width %v at %v: hit=%v", width, test.point, got)
			}
			if got := clipPaintsAt(t, commands, GetElementID("front"), test.point); got != test.visible {
				t.Errorf("target width %v at %v: paint=%v", width, test.point, got)
			}
		}
	}
}

func TestClipScopesSurviveExitAndFrameBufferReuse(t *testing.T) {
	c := freshContext(t)
	for frame := range 6 {
		c.BeginLayout()
		clipTestScope(c, "original", BoundingBox{0, 0, 30, 100}, 0)
		clipTestScope(c, "unrelated", BoundingBox{0, 0, 100, 100}, 0)
		if frame == 0 {
			BoxID(c, "exiting", Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating:   FloatingElementConfig{AttachTo: AttachToRoot, ZIndex: 2, ClipScopes: []ClipScope{{ElementID: GetElementID("original"), Horizontal: true}}},
				Transition: clipTestExitConfig(),
			}, nil)
		} else {
			// Reuse both frame pools with different scope IDs before exits are
			// cloned. Retaining a caller slice or a rewound pool leaks here.
			for i := range 3 {
				BoxIDOffset(c, "new", uint32(i), Decl{
					Layout:   clipTestLayout(100, 100),
					Floating: FloatingElementConfig{AttachTo: AttachToRoot, Offset: Vector2{Y: 200}, ClipScopes: []ClipScope{{ElementID: GetElementID("unrelated"), Vertical: true}}},
				}, nil)
			}
		}
		commands := c.EndLayout(0)
		if !clipPaintsAt(t, commands, GetElementID("exiting"), Vector2{10, 10}) {
			t.Errorf("frame %d: exit lost its visible content", frame)
		}
		if clipPaintsAt(t, commands, GetElementID("exiting"), Vector2{50, 10}) {
			t.Errorf("frame %d: exit used an unrelated scope", frame)
		}
	}
}

func TestClipScopesConcealTreeWhenTargetExitCompletes(t *testing.T) {
	c := freshContext(t)
	finished := false
	transition := clipTestExitConfig()
	transition.Handler = func(TransitionCallbackArguments) bool { return finished }
	for frame := range 4 {
		finished = frame >= 2
		c.BeginLayout()
		BoxID(c, "front", Decl{
			Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{{ElementID: GetElementID("target"), Horizontal: true}}},
		}, nil)
		if frame == 0 {
			BoxID(c, "target", Decl{
				Layout: clipTestLayout(30, 100), Transition: transition,
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ZIndex: 10, PointerCaptureMode: PointerCaptureModePassthrough},
			}, nil)
		}
		commands := c.EndLayout(0)
		point := Vector2{10, 10}
		c.SetPointerState(point, false)
		if got := c.PointerOver(GetElementID("front")); got != !finished {
			t.Errorf("frame %d: hit=%v, want %v", frame, got, !finished)
		}
		if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != !finished {
			t.Errorf("frame %d: paint=%v, want %v", frame, got, !finished)
		}
	}
}

func TestClipScopesExitPreservesTextDescendants(t *testing.T) {
	c := freshContext(t)
	for frame := range 4 {
		c.BeginLayout()
		clipTestScope(c, "scope", BoundingBox{0, 0, 30, 100}, 0)
		if frame == 0 {
			BoxID(c, "exiting", Decl{
				Layout: clipTestLayout(100, 100), Transition: clipTestExitConfig(),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{{ElementID: GetElementID("scope"), Horizontal: true}}},
			}, func() { Text(c, "retained text", TextElementConfig{FontSize: 16, WrapMode: TextWrapNone}) })
		} else {
			Text(c, "new text", TextElementConfig{FontSize: 16})
		}
		commands := c.EndLayout(0)
		found := false
		for _, command := range commands.Commands {
			if command.CommandType != RenderCommandTypeText || command.RenderData.Text.StringContents.Text != "retained text" {
				continue
			}
			found = true
			if !clipPaintsAt(t, commands, ElementID{ID: command.ID}, Vector2{5, 5}) ||
				clipPaintsAt(t, commands, ElementID{ID: command.ID}, Vector2{50, 5}) {
				t.Errorf("frame %d: retained text lost its clipping", frame)
			}
		}
		if !found {
			t.Fatalf("frame %d: exit snapshot lost its text", frame)
		}
	}
}

func TestClipScopesCapacityFailureConcealsOnlyAffectedRoots(t *testing.T) {
	var errors []ErrorData
	c := clipTestLimitedContext(t, 64, func(e ErrorData) { errors = append(errors, e) })
	if len(errors) != 0 {
		t.Fatalf("MinMemorySize does not cover the clip pools: %v", errors)
	}
	for frame := range 2 {
		c.BeginLayout()
		errors = nil
		scopes := make([]ClipScope, 65)
		for i := range scopes {
			scopes[i] = ClipScope{ElementID: GetElementID("scope"), Vertical: true}
		}
		clipTestScope(c, "scope", BoundingBox{0, 0, 100, 100}, 0)
		for _, name := range []string{"overflow-a", "overflow-b"} {
			BoxID(c, name, Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes},
			}, nil)
		}
		BoxID(c, "valid", Decl{
			Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes[:1], PointerCaptureMode: PointerCaptureModePassthrough},
		}, nil)
		commands := c.EndLayout(0)
		if len(errors) != 1 || errors[0].Type != ErrorTypeElementsCapacityExceeded || !strings.Contains(errors[0].Text, "clip-scope") {
			t.Fatalf("frame %d: want one actionable capacity error, got %v", frame, errors)
		}
		point := Vector2{10, 10}
		c.SetPointerState(point, false)
		for _, name := range []string{"overflow-a", "overflow-b", "valid"} {
			want := name == "valid"
			if got := c.PointerOver(GetElementID(name)); got != want {
				t.Errorf("frame %d, %s: hit=%v, want %v", frame, name, got, want)
			}
			if got := clipPaintsAt(t, commands, GetElementID(name), point); got != want {
				t.Errorf("frame %d, %s: paint=%v, want %v", frame, name, got, want)
			}
		}
	}
}

func TestClipScopesExactCapacityAndPoolReuse(t *testing.T) {
	var errors []ErrorData
	c := clipTestLimitedContext(t, 32, func(e ErrorData) { errors = append(errors, e) })
	if len(errors) != 0 {
		t.Fatalf("initialization: %v", errors)
	}
	scopes := make([]ClipScope, 32)
	for i := range scopes {
		scopes[i] = ClipScope{ElementID: GetElementID("scope"), Horizontal: true}
	}
	for frame := range 4 {
		errors = nil
		c.BeginLayout()
		BoxID(c, "front", Decl{Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: scopes}}, nil)
		// Check the declaration budget separately from the render-command
		// budget: emitting 32 scope pairs needs more than 32 commands.
		if c.clipScopes.Length != 32 || c.getHashMapItem(GetElementID("front").ID).LayoutElement.clipScopesInvalid {
			t.Fatalf("frame %d: exact-capacity declaration was rejected", frame)
		}
		c.EndLayout(0)
		if len(errors) != 1 || !strings.Contains(errors[0].Text, "render-command") {
			t.Fatalf("frame %d: expected only the independent render capacity error, got %v", frame, errors)
		}
	}
}

func TestClipScopesFrameStorageDoesNotRetainOldNames(t *testing.T) {
	c := freshContext(t)
	c.BeginLayout()
	BoxID(c, "front", Decl{Floating: FloatingElementConfig{AttachTo: AttachToRoot,
		ClipScopes: []ClipScope{{ElementID: GetElementID("temporary-scope-name"), Horizontal: true}},
	}}, nil)
	c.EndLayout(0)
	for range 2 {
		c.BeginLayout()
		c.EndLayout(0)
	}
	for _, pool := range []Array[ClipScope]{c.clipScopes, c.previousClipScopes} {
		for _, scope := range pool.Data {
			if scope.ElementID.StringID.Text != "" {
				t.Fatal("an unused scope pool retains a previous frame's string")
			}
		}
	}
}

func TestClipScopesSteadyFramesAllocateNothing(t *testing.T) {
	for _, transition := range []bool{false, true} {
		name := "static"
		if transition {
			name = "transition"
		}
		t.Run(name, func(t *testing.T) {
			c := freshContext(t)
			declaration := Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, ClipScopes: []ClipScope{{ElementID: GetElementID("scope"), Vertical: true}}},
			}
			if transition {
				declaration.Transition = clipTestExitConfig()
			}
			frame := func() {
				c.BeginLayout()
				clipTestScope(c, "scope", BoundingBox{0, 0, 100, 100}, 0)
				BoxID(c, "front", declaration, nil)
				c.EndLayout(0)
				c.SetPointerState(Vector2{10, 10}, false)
			}
			for range 5 {
				frame()
			}
			if allocations := testing.AllocsPerRun(100, frame); allocations != 0 {
				t.Fatalf("stable scoped frame allocates %v times", allocations)
			}
		})
	}
}
