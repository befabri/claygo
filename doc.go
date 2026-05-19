// Package claygo is a pure-Go port of nicbarker/clay, a high-performance 2D UI
// layout library with a flexbox-style model and a renderer-agnostic output:
// a sorted list of render commands that the caller draws however it likes.
//
// Quick start:
//
//	mem := make([]byte, claygo.MinMemorySize())
//	arena := claygo.CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
//	ctx := claygo.Initialize(arena, claygo.Dimensions{Width: 1280, Height: 720},
//	    claygo.ErrorHandler{Func: func(e claygo.ErrorData) { log.Println(e.Text) }})
//
//	ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
//	    return measureWithMyFont(s.Text, cfg.FontID, cfg.FontSize)
//	}, nil)
//
//	for {
//	    ctx.SetPointerState(mouseXY, mouseDown)
//	    ctx.UpdateScrollContainers(true, scrollDelta, dt)
//	    ctx.BeginLayout()
//	    claygo.Box(ctx, claygo.Decl{
//	        Layout: claygo.LayoutConfig{
//	            Sizing: claygo.Sizing{Width: claygo.SizingGrow(0), Height: claygo.SizingGrow(0)},
//	            Padding: claygo.PaddingAll(16),
//	            ChildGap: 8,
//	        },
//	        BackgroundColor: claygo.Color{R: 30, G: 30, B: 36, A: 255},
//	    }, func() {
//	        claygo.Text(ctx, "Hello, Clay", claygo.TextElementConfig{
//	            TextColor: claygo.Color{R: 240, G: 240, B: 240, A: 255},
//	            FontSize: 18,
//	        })
//	    })
//	    cmds := ctx.EndLayout(dt)
//	    myRenderer.Draw(cmds)
//	}
//
// The port is intentionally allocation-free at steady state: all internal state
// lives inside a caller-supplied arena, matching the C original.
package claygo
