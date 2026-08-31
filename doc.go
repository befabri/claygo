// Package claygo is a pure-Go port of nicbarker/clay, a high-performance 2D
// UI layout library with a flexbox-style model and a renderer-agnostic
// output: a sorted list of render commands that the caller draws however it
// likes.
//
// # Per-frame call order
//
// All input methods are called BEFORE BeginLayout so the layout pass sees a
// consistent snapshot, and the render-command output is consumed AFTER
// EndLayout. The canonical order is:
//
//  1. ctx.SetPointerState(pos, isDown)       — gathers pointer-over hits
//     against the previous frame's
//     bboxes, fires OnHover callbacks.
//  2. ctx.UpdateScrollContainers(...)        — advances drag / momentum /
//     wheel scroll on every clip
//     container.
//  3. ctx.BeginLayout()                      — resets per-frame state, opens
//     the auto-root.
//  4. claygo.Box / Text / BoxID / BoxIDOffset — declare the UI tree.
//  5. cmds := ctx.EndLayout(deltaTime)       — runs the solver, returns the
//     flat render-command list.
//  6. myRenderer.Draw(cmds)                  — paint.
//
// Skipping SetPointerState is fine if the app has no pointer input;
// UpdateScrollContainers can be skipped if the app has no clip containers
// or wants to drive scroll programmatically.
//
// # Thread safety
//
// A Context is not safe for concurrent use. Build the layout on one
// goroutine; if a renderer runs on another, snapshot the RenderCommandArray
// before handing it off (the live array is overwritten by the next EndLayout).
//
// # Memory model
//
// Internal arrays use fixed-capacity typed Go slices so their pointer-bearing
// values remain visible to the garbage collector and the solver is
// allocation-free at steady state. Size their logical capacity budget with
// MinMemorySize and CreateArenaWithCapacity.
//
// # Quick start
//
//	package main
//
//	import (
//	    "log"
//	    "github.com/befabri/claygo"
//	)
//
//	func main() {
//	    arena := claygo.CreateArenaWithCapacity(claygo.MinMemorySize())
//	    ctx := claygo.Initialize(arena,
//	        claygo.Dimensions{Width: 1280, Height: 720},
//	        claygo.ErrorHandler{Func: func(e claygo.ErrorData) {
//	            log.Printf("[clay] type=%d: %s", e.Type, e.Text)
//	        }})
//
//	    // The measure callback receives a text slice + config and returns
//	    // pixel dimensions: Width is the rendered advance, Height is the
//	    // line height. Called many times per frame (once per word for
//	    // wrappable text, plus cache misses).
//	    ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
//	        // Stub measurer: ~7px per char, line height = fontSize+4. A
//	        // real app would query its glyph atlas here.
//	        cw := float32(cfg.FontSize) * 0.55
//	        return claygo.Dimensions{
//	            Width:  float32(len(s.Text)) * cw,
//	            Height: float32(cfg.FontSize + 4),
//	        }
//	    }, nil)
//
//	    // Single frame:
//	    ctx.SetPointerState(claygo.Vector2{X: 0, Y: 0}, false)
//	    ctx.BeginLayout()
//	    claygo.Box(ctx, claygo.Decl{
//	        Layout: claygo.LayoutConfig{
//	            Sizing:   claygo.Sizing{Width: claygo.SizingGrow(0), Height: claygo.SizingGrow(0)},
//	            Padding:  claygo.PaddingAll(16),
//	            ChildGap: 8,
//	        },
//	        BackgroundColor: claygo.RGBA(30, 30, 36, 255),
//	    }, func() {
//	        claygo.Text(ctx, "Hello, Clay", claygo.TextElementConfig{
//	            TextColor: claygo.RGBA(240, 240, 240, 255),
//	            FontSize:  18,
//	        })
//	    })
//	    cmds := ctx.EndLayout(0) // deltaTime in seconds, 0 if not using transitions
//
//	    // Drain commands and paint. For each cmd, switch on cmd.CommandType
//	    // and read the matching field of cmd.RenderData (Rectangle, Text,
//	    // Border, Image, Clip, OverlayColor, Custom).
//	    for i := range cmds.Len() {
//	        cmd := cmds.Get(i)
//	        _ = cmd // myRenderer.Submit(cmd)
//	    }
//	}
//
// # Color convention
//
// Color components are conventionally 0-255, but interpretation is up to
// the renderer. Use RGBA(r, g, b, a float32) to construct a Color from the
// 0-255 range; if your renderer expects 0-1, divide on the way in.
package claygo
