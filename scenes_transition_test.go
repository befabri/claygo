package claygo

import (
	"reflect"
	"testing"
)

// goldenTransitionScenes maps scene name -> a MULTI-frame Go layout builder that
// drives its own BeginLayout/EndLayout cycles and returns the final frame's
// render commands. Unlike goldenScenes (single frame, EndLayout(0)), exit
// transitions only manifest across two frames, so these scenes can't use the
// single-frame runScene helper.
//
// Each name must match a scene compiled into oracle/main.c and a corresponding
// testdata/<name>.golden.json must exist. TestSceneParity cross-checks the
// union of goldenScenes + goldenTransitionScenes against both.
var goldenTransitionScenes = map[string]func(*Context) RenderCommandArray{
	"exit_nested_child_with_exit": sceneExitNestedChildWithExit,
	"exit_nested_child_plain":     sceneExitNestedChildPlain,
}

// oracleTransitionDelta matches ORACLE_TRANSITION_DELTA in oracle/main.c. On
// the dumped (first exit) frame elapsedTime is still 0, so the actual value
// doesn't affect output — it's kept identical for clarity.
const oracleTransitionDelta = float32(0.1)

// goldenExitSlideOff is the byte-identical port of oracle/main.c::exit_slide_off.
func goldenExitSlideOff(initial TransitionData, _ TransitionProperty) TransitionData {
	initial.BoundingBox.X = -500
	return initial
}

func goldenTransitionConfig() TransitionElementConfig {
	return TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   1.0,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: goldenExitSlideOff},
	}
}

// sceneExitNestedChildWithExit mirrors oracle scene_exit_nested_child_with_exit:
// parent and child BOTH have exit transitions. Frame 2 emits only the parent —
// the nested child's transition record is removed and the child is skipped by
// the render pass (clay.h:2933-2937 / 4527-4531).
func sceneExitNestedChildWithExit(c *Context) RenderCommandArray {
	transition := goldenTransitionConfig()
	c.BeginLayout()
	BoxID(c, "NEParent", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
		BackgroundColor: RGBA(100, 100, 200, 255),
		Transition:      transition,
	}, func() {
		BoxID(c, "NEChild", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 50, 50, 255),
			Transition:      transition,
		}, nil)
	})
	c.EndLayout(oracleTransitionDelta)

	c.BeginLayout()
	return c.EndLayout(oracleTransitionDelta)
}

// sceneExitNestedChildPlain mirrors oracle scene_exit_nested_child_plain: the
// parent has an exit transition, the child has none. Frame 2 emits BOTH — the
// plain child is cloned as part of the parent's exiting subtree and rendered.
func sceneExitNestedChildPlain(c *Context) RenderCommandArray {
	transition := goldenTransitionConfig()
	c.BeginLayout()
	BoxID(c, "NPParent", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(120)}, LayoutDirection: TopToBottom},
		BackgroundColor: RGBA(100, 100, 200, 255),
		Transition:      transition,
	}, func() {
		BoxID(c, "NPChild", Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 50, 50, 255),
		}, nil)
	})
	c.EndLayout(oracleTransitionDelta)

	c.BeginLayout()
	return c.EndLayout(oracleTransitionDelta)
}

// runTransitionScene wires up a fresh Context and lets the multi-frame builder
// drive the frames. Mirrors runScene but without the single BeginLayout/
// EndLayout assumption.
func runTransitionScene(t *testing.T, frames func(*Context) RenderCommandArray) RenderCommandArray {
	t.Helper()
	mem := make([]byte, MinMemorySize())
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := Initialize(arena, goldenViewport, ErrorHandler{
		Func: func(err ErrorData) {
			t.Errorf("clay error: type=%d text=%q", err.Type, err.Text)
		},
	})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	return frames(ctx)
}

// TestTransitionGoldens asserts the Go port reproduces the C oracle's frame-2
// render commands for each multi-frame exit-transition scene, byte-for-byte.
func TestTransitionGoldens(t *testing.T) {
	for name, frames := range goldenTransitionScenes {
		t.Run(name, func(t *testing.T) {
			cmds := runTransitionScene(t, frames)
			got := toGoldenJSON(cmds)
			want, err := loadGolden(name)
			if err != nil {
				t.Fatalf("load golden: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("scene %q diverges from oracle\n--- got  (%d commands) ---\n%s\n--- want (%d commands) ---\n%s",
					name, len(got.Commands), prettyJSON(got), len(want.Commands), prettyJSON(want))
			}
		})
	}
}
