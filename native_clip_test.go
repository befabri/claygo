package claygo

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func clipTestLayout(width, height float32) LayoutConfig {
	return LayoutConfig{Sizing: Sizing{Width: SizingFixed(width), Height: SizingFixed(height)}}
}

func TestNativeOffscreenClipsDoNotSpendRenderCapacity(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(strconv.FormatBool(nested), func(t *testing.T) {
			var errors []ErrorData
			c := clipTestLimitedContext(t, 4096, func(e ErrorData) { errors = append(errors, e) })
			c.BeginLayout()
			BoxID(c, "offscreen", Decl{
				Layout:   clipTestLayout(100, 100),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot, Offset: Vector2{X: 2000}},
			}, func() {
				for range 3000 {
					c.OpenElement()
					c.ConfigureOpenElement(Decl{
						Layout:          clipTestLayout(100, 10),
						Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
						BackgroundColor: RGBA(1, 2, 3, 255),
					})
					if !nested {
						c.CloseElement()
					}
				}
				if nested {
					for range 3000 {
						c.CloseElement()
					}
				}
			})
			BoxID(c, "visible", Decl{
				Layout: clipTestLayout(20, 20), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating: FloatingElementConfig{AttachTo: AttachToRoot},
			}, nil)
			commands := c.EndLayout(0)
			if len(errors) != 0 || len(commands.Commands) != 1 {
				t.Fatalf("culled clips spent render capacity: %d commands, errors %v", len(commands.Commands), errors)
			}
			if !clipPaintsAt(t, commands, GetElementID("visible"), Vector2{10, 10}) {
				t.Fatal("offscreen rows prevented the later visible root from painting")
			}
		})
	}
}

func TestFloatingOwnedClipDoesNotLeakToSibling(t *testing.T) {
	c := freshContext(t)
	c.BeginLayout()
	BoxID(c, "viewport", Decl{Layout: clipTestLayout(100, 30), Clip: ClipElementConfig{Vertical: true}}, func() {
		BoxID(c, "owned", Decl{
			Layout: clipTestLayout(20, 20), Clip: ClipElementConfig{Horizontal: true},
			Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
		}, nil)
		BoxID(c, "sibling", Decl{
			Layout:   clipTestLayout(80, 20),
			Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
		}, nil)
	})
	c.EndLayout(0)
	c.SetPointerState(Vector2{X: 50, Y: 10}, false)
	if !c.PointerOver(GetElementID("sibling")) {
		t.Fatal("a closed floating clip still prevents its sibling from receiving the pointer")
	}
	if c.openClipElementStack.Length != 0 {
		t.Fatalf("clip stack has %d entries after EndLayout", c.openClipElementStack.Length)
	}
}

func TestNativeFloatingExitSkipsDisabledClipAncestors(t *testing.T) {
	for _, test := range []struct {
		name  string
		outer ClipElementConfig
	}{
		{"none", ClipElementConfig{}},
		{"horizontal", ClipElementConfig{Horizontal: true}},
		{"vertical", ClipElementConfig{Vertical: true}},
		{"both", ClipElementConfig{Horizontal: true, Vertical: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := freshContext(t)
			for frame, enabled := range []bool{true, false, false, true} {
				c.BeginLayout()
				BoxID(c, "outer", Decl{Layout: clipTestLayout(60, 60), Clip: test.outer}, func() {
					BoxID(c, "viewport", Decl{
						Layout: clipTestLayout(30, 30),
						Clip:   ClipElementConfig{Horizontal: enabled, Vertical: enabled},
					}, func() {
						if frame == 0 {
							BoxID(c, "exiting", Decl{
								Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
								Floating:   FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
								Transition: clipTestExitConfig(),
							}, nil)
						}
					})
				})
				commands := c.EndLayout(0)
				for _, point := range []Vector2{{10, 10}, {50, 50}, {70, 50}, {50, 70}, {70, 70}} {
					want := (!enabled || (point.X < 30 && point.Y < 30)) &&
						(!test.outer.Horizontal || point.X < 60) && (!test.outer.Vertical || point.Y < 60)
					if got := clipPaintsAt(t, commands, GetElementID("exiting"), point); got != want {
						t.Fatalf("frame %d at %v: paint=%v, want %v; disabled clips must skip to active ancestors", frame, point, got, want)
					}
				}
			}
		})
	}
}

func TestNativeClipPointerRespectsEachAxis(t *testing.T) {
	for _, axes := range []struct {
		name            string
		clip            ClipElementConfig
		inside, outside Vector2
	}{
		{"vertical", ClipElementConfig{Vertical: true}, Vector2{50, 10}, Vector2{10, 30}},
		{"horizontal", ClipElementConfig{Horizontal: true}, Vector2{10, 50}, Vector2{30, 10}},
	} {
		t.Run(axes.name, func(t *testing.T) {
			c := freshContext(t)
			c.BeginLayout()
			BoxID(c, "viewport", Decl{Layout: clipTestLayout(30, 30), Clip: axes.clip}, func() {
				BoxID(c, "child", Decl{Layout: clipTestLayout(80, 80)}, nil)
			})
			c.EndLayout(0)
			c.SetPointerState(axes.inside, false)
			if !c.PointerOver(GetElementID("child")) {
				t.Error("visible overflow on an unrestricted axis lost its pointer hit")
			}
			c.SetPointerState(axes.outside, false)
			if c.PointerOver(GetElementID("child")) {
				t.Error("content beyond the selected clip boundary received a pointer hit")
			}
		})
	}
}

func TestNativeClipPointerChecksOuterAncestors(t *testing.T) {
	c := freshContext(t)
	c.BeginLayout()
	BoxID(c, "outer", Decl{Layout: clipTestLayout(30, 30), Clip: ClipElementConfig{Horizontal: true}}, func() {
		BoxID(c, "inner", Decl{Layout: clipTestLayout(80, 80), Clip: ClipElementConfig{Vertical: true}}, func() {
			BoxID(c, "child", Decl{Layout: clipTestLayout(80, 80)}, nil)
		})
	})
	c.EndLayout(0)
	c.SetPointerState(Vector2{50, 10}, false)
	if c.PointerOver(GetElementID("child")) {
		t.Fatal("the nearest viewport allowed a hit outside the outer viewport")
	}
}

func TestNativeClipPointerIncludesText(t *testing.T) {
	c := freshContext(t)
	c.SetMeasureTextFunction(deterministicMeasureText, nil)
	c.BeginLayout()
	BoxID(c, "viewport", Decl{Layout: clipTestLayout(200, 5), Clip: ClipElementConfig{Vertical: true}}, func() {
		Text(c, "clipped text", TextElementConfig{FontSize: 20})
	})
	c.EndLayout(0)
	textID := ElementID{ID: HashNumber(0, GetElementID("viewport").ID).ID}
	c.SetPointerState(Vector2{5, 10}, false)
	if c.PointerOver(textID) {
		t.Fatal("text outside the viewport received a pointer hit")
	}
	c.SetPointerState(Vector2{5, 2}, false)
	if !c.PointerOver(textID) {
		t.Fatal("visible text lost its pointer hit")
	}
}

func TestNativeFloatingClipsCombineAncestorsOnTheirOwnAxes(t *testing.T) {
	for _, attach := range []FloatingAttachToElement{AttachToParent, AttachToElementWithID} {
		c := freshContext(t)
		c.BeginLayout()
		floating := func() {
			BoxID(c, "front", Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
				Floating: FloatingElementConfig{AttachTo: attach, ParentID: GetElementID("anchor").ID, ClipTo: ClipToAttachedParent},
			}, nil)
		}
		BoxID(c, "outer", Decl{Layout: clipTestLayout(30, 100), Clip: ClipElementConfig{Horizontal: true}}, func() {
			BoxID(c, "inner", Decl{Layout: clipTestLayout(100, 20), Clip: ClipElementConfig{Vertical: true}}, func() {
				BoxID(c, "anchor", Decl{Layout: clipTestLayout(100, 100)}, func() {
					if attach == AttachToParent {
						floating()
					}
				})
			})
		})
		if attach == AttachToElementWithID {
			floating()
		}
		commands := c.EndLayout(0)
		for _, point := range []Vector2{{10, 10}, {50, 10}, {10, 50}} {
			want := point.X < 30 && point.Y < 20
			c.SetPointerState(point, false)
			if got := c.PointerOver(GetElementID("front")); got != want {
				t.Errorf("attach=%v at %v: hit=%v, want %v", attach, point, got, want)
			}
			if got := clipPaintsAt(t, commands, GetElementID("front"), point); got != want {
				t.Errorf("attach=%v at %v: paint=%v, want %v", attach, point, got, want)
			}
		}
	}
}

func TestNativeOffscreenClipStillConfinesVisibleDescendants(t *testing.T) {
	for _, horizontal := range []bool{true, false} {
		c := freshContext(t)
		c.BeginLayout()
		BoxID(c, "viewport", Decl{
			Layout:   clipTestLayout(100, 30),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, Offset: Vector2{X: 2000}},
			Clip:     ClipElementConfig{Horizontal: horizontal, Vertical: !horizontal, ChildOffset: Vector2{X: -2000}},
		}, func() {
			BoxID(c, "child", Decl{Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255)}, nil)
		})
		commands := c.EndLayout(0)
		for _, point := range []Vector2{{10, 10}, {10, 50}} {
			want := !horizontal && point.Y < 30
			c.SetPointerState(point, false)
			if got := c.PointerOver(GetElementID("child")); got != want {
				t.Errorf("horizontal=%v at %v: hit=%v, want %v", horizontal, point, got, want)
			}
			if got := clipPaintsAt(t, commands, GetElementID("child"), point); got != want {
				t.Errorf("horizontal=%v at %v: paint=%v, want %v", horizontal, point, got, want)
			}
		}
	}
}

func TestFloatingClipInheritanceRejectsStaleAttachment(t *testing.T) {
	var errors []ErrorData
	c := Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 320, Height: 200},
		ErrorHandler{Func: func(e ErrorData) { errors = append(errors, e) }})
	c.BeginLayout()
	BoxID(c, "old-parent", Decl{Layout: clipTestLayout(100, 100)}, nil)
	c.EndLayout(0)
	c.BeginLayout()
	BoxID(c, "unrelated", Decl{Layout: clipTestLayout(100, 100), Clip: ClipElementConfig{Vertical: true}}, nil)
	BoxID(c, "floating", Decl{
		Layout:   clipTestLayout(100, 100),
		Floating: FloatingElementConfig{AttachTo: AttachToElementWithID, ParentID: GetElementID("old-parent").ID, ClipTo: ClipToAttachedParent},
	}, nil)
	c.EndLayout(0)
	if len(errors) != 1 || errors[0].Type != ErrorTypeFloatingContainerParentNotFound {
		t.Fatalf("a previous-frame parent was accepted as a current attachment: %v", errors)
	}
}

func TestNativeFloatingClipSurvivesExit(t *testing.T) {
	c := freshContext(t)
	for frame := range 3 {
		c.BeginLayout()
		BoxID(c, "viewport", Decl{Layout: clipTestLayout(30, 100), Clip: ClipElementConfig{Horizontal: true}}, func() {
			if frame == 0 {
				BoxID(c, "exiting", Decl{
					Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(1, 2, 3, 255),
					Floating:   FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
					Transition: clipTestExitConfig(),
				}, nil)
			}
		})
		commands := c.EndLayout(0)
		if !clipPaintsAt(t, commands, GetElementID("exiting"), Vector2{10, 10}) ||
			clipPaintsAt(t, commands, GetElementID("exiting"), Vector2{50, 10}) {
			t.Errorf("frame %d: exit lost its native viewport", frame)
		}
	}
}

func clipTestExitConfig() TransitionElementConfig {
	return TransitionElementConfig{
		Handler: keepTransitionRunning, Duration: 1, Properties: TransitionPropertyX,
		Exit: TransitionExitConfig{SetFinalState: func(state TransitionData, _ TransitionProperty) TransitionData { return state }},
	}
}

func TestFloatingClipOwnersFitDeclarationCapacity(t *testing.T) {
	var errors []ErrorData
	c := clipTestLimitedContext(t, 64, func(e ErrorData) { errors = append(errors, e) })
	c.BeginLayout()
	for range 40 {
		c.OpenElement()
		c.ConfigureOpenElement(Decl{
			Floating: FloatingElementConfig{AttachTo: AttachToParent},
			Clip:     ClipElementConfig{Vertical: true},
		})
	}
	for range 40 {
		c.CloseElement()
	}
	if len(errors) != 0 || c.openClipElementStack.Length != 0 {
		t.Fatalf("a valid 40-element tree exhausted/leaked its declaration stack: %v", errors)
	}
	// This many scissors exceed the separate 64-command render budget, which
	// should be the only capacity error in a completed frame.
	c.EndLayout(0)
	if len(errors) != 1 || !strings.Contains(errors[0].Text, "render-command") {
		t.Fatalf("expected a render budget error, got %v", errors)
	}
}

func clipTestLimitedContext(t *testing.T, capacity int32, handler func(ErrorData)) *Context {
	t.Helper()
	previous, previousLimit, previousWords := GetCurrentContext(), defaultMaxElementCount, defaultMaxMeasureTextWordCacheSize
	SetCurrentContext(nil)
	SetMaxElementCount(capacity)
	t.Cleanup(func() {
		defaultMaxElementCount, defaultMaxMeasureTextWordCacheSize = previousLimit, previousWords
		SetCurrentContext(previous)
	})
	return Initialize(CreateArenaWithCapacity(MinMemorySize()), Dimensions{Width: 320, Height: 200}, ErrorHandler{Func: handler})
}

// clipPaintsAt interprets the public scissor stream, independently of Clay's
// pointer helpers. It also checks nesting even if the requested item is culled.
func clipPaintsAt(t *testing.T, commands RenderCommandArray, id ElementID, point Vector2) bool {
	t.Helper()
	var stack []RenderCommand
	painted := false
	for _, command := range commands.Commands {
		switch command.CommandType {
		case RenderCommandTypeScissorStart:
			stack = append(stack, command)
		case RenderCommandTypeScissorEnd:
			if len(stack) == 0 {
				t.Fatal("scissor end has no matching start")
			}
			stack = stack[:len(stack)-1]
		case RenderCommandTypeRectangle, RenderCommandTypeImage, RenderCommandTypeCustom, RenderCommandTypeText:
			if command.ID != id.ID {
				continue
			}
			box := command.BoundingBox
			visible := point.X >= box.X && point.X < box.X+box.Width && point.Y >= box.Y && point.Y < box.Y+box.Height
			for _, clip := range stack {
				x, y := clip.RenderData.Clip.Horizontal, clip.RenderData.Clip.Vertical
				if !x && !y { // Clay's legacy inherited scissor clips both axes.
					x, y = true, true
				}
				box = clip.BoundingBox
				if x && (point.X < box.X || point.X >= box.X+box.Width) ||
					y && (point.Y < box.Y || point.Y >= box.Y+box.Height) {
					visible = false
				}
			}
			painted = painted || visible
		}
	}
	if len(stack) != 0 {
		t.Fatalf("%d scissors were left open", len(stack))
	}
	return painted
}

func sceneNativeClipAncestors(c *Context) RenderCommandArray {
	c.BeginLayout()
	BoxID(c, "outer", Decl{Layout: clipTestLayout(30, 100), Clip: ClipElementConfig{Horizontal: true}}, func() {
		BoxID(c, "inner", Decl{Layout: clipTestLayout(100, 20), Clip: ClipElementConfig{Vertical: true}}, func() {
			BoxID(c, "front", Decl{
				Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(200, 80, 80, 255),
				Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
			}, nil)
		})
	})
	return c.EndLayout(0)
}

func sceneNativeClipOwnedSibling(c *Context) RenderCommandArray {
	c.BeginLayout()
	BoxID(c, "viewport", Decl{Layout: clipTestLayout(100, 30), Clip: ClipElementConfig{Vertical: true}}, func() {
		BoxID(c, "owned", Decl{
			Layout: clipTestLayout(20, 20), Clip: ClipElementConfig{Horizontal: true},
			Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
		}, nil)
		BoxID(c, "sibling", Decl{
			Layout: clipTestLayout(80, 20), BackgroundColor: RGBA(200, 80, 80, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
		}, nil)
	})
	return c.EndLayout(0)
}

func sceneNativeClipOffscreen(c *Context) RenderCommandArray {
	c.BeginLayout()
	BoxID(c, "viewport", Decl{
		Layout:   clipTestLayout(100, 30),
		Floating: FloatingElementConfig{AttachTo: AttachToRoot, Offset: Vector2{X: 2000}},
		Clip:     ClipElementConfig{Vertical: true, ChildOffset: Vector2{X: -2000}},
	}, func() {
		BoxID(c, "child", Decl{Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(200, 80, 80, 255)}, nil)
	})
	return c.EndLayout(0)
}

func sceneNativeClipExit(c *Context) RenderCommandArray {
	var commands RenderCommandArray
	for frame := range 3 {
		c.BeginLayout()
		BoxID(c, "outer", Decl{Layout: clipTestLayout(100, 60), Clip: ClipElementConfig{Vertical: true}}, func() {
			BoxID(c, "viewport", Decl{Layout: clipTestLayout(30, 100), Clip: ClipElementConfig{Horizontal: frame < 2}}, func() {
				if frame == 0 {
					BoxID(c, "exiting", Decl{
						Layout: clipTestLayout(100, 100), BackgroundColor: RGBA(200, 80, 80, 255),
						Floating:   FloatingElementConfig{AttachTo: AttachToParent, ClipTo: ClipToAttachedParent},
						Transition: clipTestExitConfig(),
					}, nil)
				}
			})
		})
		commands = c.EndLayout(0)
	}
	return commands
}

// nativeScenes exercises intentional corrections to existing Clay behavior.
// Its goldens come from oracle-native, which has no extension configuration.
var nativeScenes = map[string]func(*Context) RenderCommandArray{
	"native_clip_ancestors":     sceneNativeClipAncestors,
	"native_clip_owned_sibling": sceneNativeClipOwnedSibling,
	"native_clip_offscreen":     sceneNativeClipOffscreen,
	"native_clip_exit":          sceneNativeClipExit,
	"native_clip_culled":        sceneNativeClipCulled,
}

func sceneNativeClipCulled(c *Context) RenderCommandArray {
	// Mirror the C fixture's 63 retained scroll IDs across both shapes. The
	// Go-only capacity regression above still exercises 3,000 containers.
	const clipCount = 32
	var commands RenderCommandArray
	for _, nested := range []bool{false, true} {
		c.BeginLayout()
		BoxID(c, "offscreen", Decl{
			Layout:   clipTestLayout(100, 100),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot, Offset: Vector2{X: 2000}},
		}, func() {
			for range clipCount {
				c.OpenElement()
				c.ConfigureOpenElement(Decl{Layout: clipTestLayout(100, 10), Clip: ClipElementConfig{Horizontal: true, Vertical: true}})
				if !nested {
					c.CloseElement()
				}
			}
			if nested {
				for range clipCount {
					c.CloseElement()
				}
			}
		})
		BoxID(c, "visible", Decl{
			Layout: clipTestLayout(20, 20), BackgroundColor: RGBA(1, 2, 3, 255),
			Floating: FloatingElementConfig{AttachTo: AttachToRoot},
		}, nil)
		commands = c.EndLayout(0)
	}
	return commands
}

func TestNativeClippingGoldens(t *testing.T) {
	for name, frames := range nativeScenes {
		t.Run(name, func(t *testing.T) {
			if !strings.HasPrefix(name, "native_") {
				t.Fatalf("native scene %q must use the native_ prefix", name)
			}
			got := toGoldenJSON(runTransitionScene(t, frames))
			want, err := loadGolden(name)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("native correction %s diverges from C\ngot: %s\nwant: %s", name, prettyJSON(got), prettyJSON(want))
			}
		})
	}
}
