package claygo

import (
	"reflect"
	"testing"
)

// goldenTransitionScenes maps scene name -> a MULTI-frame Go layout builder that
// drives its own BeginLayout/EndLayout cycles and returns the final frame's
// render commands. Unlike goldenScenes (single frame, EndLayout(0)), exit
// transitions only manifest across multiple frames, so these scenes can't use
// the single-frame runScene helper.
//
// Each name must match a scene compiled into oracle/main.c and a corresponding
// testdata/<name>.golden.json must exist. TestSceneParity cross-checks the
// union of goldenScenes + goldenTransitionScenes against both.
var goldenTransitionScenes = map[string]func(*Context) RenderCommandArray{
	"exit_nested_child_with_exit": sceneExitNestedChildWithExit,
	"exit_nested_child_plain":     sceneExitNestedChildPlain,
	"exit_single_mid":             sceneExitSingleMid,
	"exit_single_completed":       sceneExitSingleCompleted,
}

// oracleTransitionDelta matches ORACLE_TRANSITION_DELTA in oracle/main.c.
const oracleTransitionDelta = float32(0.1)

// goldenExitSlideOff is the byte-identical port of oracle/main.c::exit_slide_off.
func goldenExitSlideOff(initial TransitionData, _ TransitionProperty) TransitionData {
	initial.BoundingBox.X = -500
	return initial
}

func goldenTransitionConfig() TransitionElementConfig {
	return goldenTransitionConfigWithDuration(1.0)
}

func goldenTransitionConfigWithDuration(duration float32) TransitionElementConfig {
	return TransitionElementConfig{
		Handler:    linearXInterpolator,
		Duration:   duration,
		Properties: TransitionPropertyX,
		Exit:       TransitionExitConfig{SetFinalState: goldenExitSlideOff},
	}
}

func sceneExitSingle(c *Context, duration float32, exitFrames int) RenderCommandArray {
	transition := goldenTransitionConfigWithDuration(duration)
	c.BeginLayout()
	BoxID(c, "ExitSingle", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(100)}},
		BackgroundColor: RGBA(80, 80, 80, 255),
		Transition:      transition,
	}, nil)
	c.EndLayout(oracleTransitionDelta)

	var cmds RenderCommandArray
	for range exitFrames {
		c.BeginLayout()
		cmds = c.EndLayout(oracleTransitionDelta)
	}
	return cmds
}

// sceneExitSingleMid returns the second exit frame. Frame 2 starts EXITING with
// elapsedTime=0 and still renders at x=0; frame 3 uses elapsedTime=0.1s over a
// 1s duration, so the oracle and Go port must both render at x=-50.
func sceneExitSingleMid(c *Context) RenderCommandArray {
	return sceneExitSingle(c, 1.0, 2)
}

// sceneExitSingleCompleted returns the frame where the exit handler reports
// complete. The clone still exists in the first pass, but the second visible
// pass must skip it after the transition record is removed.
func sceneExitSingleCompleted(c *Context) RenderCommandArray {
	return sceneExitSingle(c, oracleTransitionDelta, 2)
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

// TestTransitionGoldens asserts the Go port reproduces the C oracle's selected
// output frame for each multi-frame exit-transition scene, byte-for-byte.
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
