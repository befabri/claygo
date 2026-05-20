package claygo_test

import (
	"fmt"

	"github.com/befabri/claygo"
)

// Example demonstrates the canonical per-frame loop for a claygo user.
// It builds a single frame of a panel with a header text, dumps the
// resulting command types, and is wired through `go test` so any future
// API drift in the public surface that the example uses will fail to
// compile, catching documentation rot.
func Example() {
	// One-time setup ----------------------------------------------------
	mem := make([]byte, claygo.MinMemorySize())
	arena := claygo.CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := claygo.Initialize(arena,
		claygo.Dimensions{Width: 320, Height: 240},
		claygo.ErrorHandler{Func: func(e claygo.ErrorData) {
			fmt.Printf("clay error: %s\n", e.Text)
		}})
	ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
		cw := float32(cfg.FontSize) * 0.55
		return claygo.Dimensions{
			Width:  float32(len(s.Text)) * cw,
			Height: float32(cfg.FontSize + 4),
		}
	}, nil)

	// One frame ---------------------------------------------------------
	ctx.SetPointerState(claygo.Vector2{}, false)
	ctx.BeginLayout()
	claygo.Box(ctx, claygo.Decl{
		Layout: claygo.LayoutConfig{
			Sizing:  claygo.Sizing{Width: claygo.SizingFit(0), Height: claygo.SizingFit(0)},
			Padding: claygo.PaddingAll(8),
		},
		BackgroundColor: claygo.RGBA(30, 30, 36, 255),
	}, func() {
		claygo.Text(ctx, "Hello, Clay", claygo.TextElementConfig{
			TextColor: claygo.RGBA(240, 240, 240, 255),
			FontSize:  16,
		})
	})
	cmds := ctx.EndLayout(0)

	// Render commands ---------------------------------------------------
	for i := 0; i < cmds.Len(); i++ {
		cmd := cmds.Get(i)
		switch cmd.CommandType {
		case claygo.RenderCommandTypeRectangle:
			fmt.Println("RECT")
		case claygo.RenderCommandTypeText:
			fmt.Println("TEXT")
		}
	}
	// Output:
	// RECT
	// TEXT
}
