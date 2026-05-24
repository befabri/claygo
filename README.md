# claygo

A pure-Go port of [nicbarker/clay](https://github.com/nicbarker/clay): a
high-performance 2D UI layout library with a flexbox-style model and a
**renderer-agnostic** output. Each frame the solver returns a flat, sorted list
of render commands that you draw however you like (SDL, OpenGL, a terminal, a
canvas; claygo never touches a GPU itself).

- **No cgo, no dependencies.** Standard library only.
- **Capacity-bounded layout state.** You provide an arena-sized byte slice up
  front; claygo reserves fixed-capacity internal buffers from that budget and
  reports capacity overruns through your `ErrorHandler`.
- **Immediate-mode API.** Re-declare your whole UI tree every frame; claygo
  diffs nothing and keeps no retained widget graph.
- **Renderer-owned drawing.** claygo emits rectangles, text, images, borders,
  scissor commands, overlays, and custom commands; your renderer interprets
  them.
- **Parity-tested against upstream Clay.** A C oracle (`oracle/`) is compiled
  from the original `clay.h` and its output is compared against claygo's in the
  test suite.

## Install

```sh
go get github.com/befabri/claygo
```

Requires Go 1.25+.

## How it works

claygo is **immediate mode**. There are no widget objects to retain. Every
frame you:

1. feed it input,
2. declare the tree with nested `Box` / `Text` calls,
3. get back a `RenderCommandArray`,
4. draw the commands with your own renderer.

The canonical per-frame call order (input *before* `BeginLayout`, output
*after* `EndLayout`):

```go
ctx.SetPointerState(pos, isDown)              // 1. pointer hits + OnHover, vs last frame's bboxes
ctx.UpdateScrollContainers(true, wheel, dt)   // 2. advance drag / momentum / wheel scroll
ctx.BeginLayout()                             // 3. reset per-frame state, open the root
//    ... claygo.Box / claygo.Text declarations ...   4. declare the tree
cmds := ctx.EndLayout(dt)                     // 5. run the solver → sorted command list
myRenderer.Draw(cmds)                         // 6. paint
```

`SetPointerState` can be skipped if you have no pointer input;
`UpdateScrollContainers` if you have no clip containers.

### Memory model

claygo asks you for a byte slice called the *arena*. In this Go port the arena
is a capacity budget, not raw object storage: internal arrays are backed by
typed Go slices so strings, interfaces, and function pointers remain visible to
the garbage collector. Size the arena before `Initialize` and keep it alive for
the lifetime of the `Context`:

```go
mem := make([]byte, claygo.MinMemorySize())
arena := claygo.CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
ctx := claygo.Initialize(arena,
    claygo.Dimensions{Width: 1280, Height: 720},
    claygo.ErrorHandler{Func: func(e claygo.ErrorData) {
        log.Printf("[clay] type=%d: %s", e.Type, e.Text)
    }})
```

Default capacities are 8192 elements and 16384 measured words. If your UI needs
more, call the package-level setters before `MinMemorySize` and `Initialize`:

```go
claygo.SetMaxElementCount(12000)
claygo.SetMaxMeasureTextCacheWordCount(24000)

mem := make([]byte, claygo.MinMemorySize())
```

Changing those limits after a `Context` has been initialized does not resize
the buffers already reserved for that context.

### Text measurement

claygo doesn't rasterize text — it asks *you* how big a string is. Install a
measure callback once; it's invoked many times per frame (once per word for
wrappable text, plus cache misses):

```go
ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
    width := float32(len(s.Text)) * float32(cfg.FontSize) * 0.55 // stub; use your font/glyph atlas here
    return claygo.Dimensions{
        Width:  width,
        Height: float32(cfg.FontSize + 4),
    }
}, nil)
```

If no measure function is installed, text leaves measure as 0×0 and the
`ErrorHandler` fires `ErrorTypeTextMeasurementFunctionNotProvided` once.
Call `ctx.ResetMeasureTextCache()` after changing font metrics, DPI, or any
other input that invalidates previously measured text.

## Quick start

```go
package main

import (
    "log"

    "github.com/befabri/claygo"
)

func main() {
    mem := make([]byte, claygo.MinMemorySize())
    arena := claygo.CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
    ctx := claygo.Initialize(arena,
        claygo.Dimensions{Width: 1280, Height: 720},
        claygo.ErrorHandler{Func: func(e claygo.ErrorData) {
            log.Printf("[clay] %s", e.Text)
        }})

    ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
        cw := float32(cfg.FontSize) * 0.55 // stub: ~0.55em per char
        return claygo.Dimensions{Width: float32(len(s.Text)) * cw, Height: float32(cfg.FontSize + 4)}
    }, nil)

    // --- one frame ---
    ctx.SetPointerState(claygo.Vector2{}, false)
    ctx.BeginLayout()
    claygo.Box(ctx, claygo.Decl{
        Layout: claygo.LayoutConfig{
            Sizing:          claygo.Sizing{Width: claygo.SizingGrow(), Height: claygo.SizingGrow()},
            Padding:         claygo.PaddingAll(16),
            ChildGap:        8,
            LayoutDirection: claygo.TopToBottom,
        },
        BackgroundColor: claygo.RGBA(30, 30, 36, 255),
    }, func() {
        claygo.Text(ctx, "Hello, Clay", claygo.TextElementConfig{
            TextColor: claygo.RGBA(240, 240, 240, 255),
            FontSize:  18,
        })
    })
    cmds := ctx.EndLayout(0) // deltaTime in seconds; 0 if not using transitions

    for i := 0; i < cmds.Len(); i++ {
        cmd := cmds.Get(i)
        switch cmd.CommandType {
        case claygo.RenderCommandTypeRectangle:
            // draw cmd.BoundingBox filled with cmd.RenderData.Rectangle.BackgroundColor
        case claygo.RenderCommandTypeText:
            // draw cmd.RenderData.Text.StringContents at cmd.BoundingBox
        }
    }
}
```

## Declaring the tree

| Function | Use |
|----------|-----|
| `Box(c, decl, children)` | An element with an auto-id derived from its position in the parent. |
| `BoxID(c, id, decl, children)` | An element with an explicit string id; needed when you later query it (`PointerOver`, `GetElementData`) or attach a floating element to it. |
| `BoxIDOffset(c, id, i, decl, children)` | `BoxID` with a numeric offset folded in. Use a stable per-item offset/key for loop rows, especially if rows can be inserted or reordered. |
| `Text(c, s, cfg)` | A leaf text element (no children). |

Pass `nil` children for a leaf. A `Decl` carries `Layout` plus optional
`BackgroundColor`, `CornerRadius`, `Border`, `Image`, `Clip`, `Floating`,
`Custom`, `Transition`, and arbitrary `UserData`.

### Sizing helpers

```go
claygo.SizingFixed(200)   // exactly 200px
claygo.SizingGrow()       // expand to fill remaining space
claygo.SizingGrow(50, 300) // grow, clamped to min=50 and max=300
claygo.SizingFit()        // shrink-wrap to children
claygo.SizingFit(20, 500) // fit, clamped to min=20 and max=500
claygo.SizingPercent(0.5) // 50% of the parent axis
```

The zero value of `LayoutDirection` is `LeftToRight`. Set
`LayoutDirection: claygo.TopToBottom` for vertical stacks.

## Render commands

`EndLayout` returns a `RenderCommandArray` already sorted in ascending draw
order — iterate it naively. Switch on `cmd.CommandType` and read the matching
field of `cmd.RenderData`:

| `CommandType` | Payload field |
|---------------|---------------|
| `RenderCommandTypeRectangle` | `RenderData.Rectangle` |
| `RenderCommandTypeText` | `RenderData.Text` |
| `RenderCommandTypeBorder` | `RenderData.Border` |
| `RenderCommandTypeImage` | `RenderData.Image` |
| `RenderCommandTypeScissorStart` / `…End` | `RenderData.Clip` |
| `RenderCommandTypeOverlayColorStart` / `…End` | `RenderData.OverlayColor` |
| `RenderCommandTypeCustom` | `RenderData.Custom` |

Every command carries a `BoundingBox`, a `ZIndex`, the element `ID`, and any
`UserData` you attached.

`RenderCommandArray.Commands` aliases the context's internal command buffer.
Copy it if you need to keep commands after the next `EndLayout` or hand them to
another goroutine.

### Color convention

Components are conventionally 0–255 (`claygo.RGBA(r, g, b, a)`), but the
interpretation is entirely the renderer's. If yours expects 0–1, divide on the
way in.

## Interaction

- `ctx.SetPointerState(pos, isDown)` - feed pointer position/button each frame
  before `BeginLayout`; hit testing uses the previous frame's bounding boxes.
- `ctx.Hovered()` - reports whether the *currently open* element is hovered.
- `ctx.PointerOver(id)` - reports whether the pointer is over the element with
  this id.
- `ctx.OnHover(fn, userData)` - registers a callback for the currently open
  element; callbacks fire from `SetPointerState`.
- `ctx.GetPointerOverIds()` - returns the current pointer-over id snapshot.
- `ctx.GetElementData(id)` - returns the last known bounding box and a `Found`
  flag for an id.

### Scrolling and clipping

Elements with `Clip.Horizontal` or `Clip.Vertical` emit scissor commands and
register scroll-container state. `UpdateScrollContainers` advances that state;
apply it to the next frame's declaration through `Clip.ChildOffset`:

```go
paneID := claygo.GetElementID("Pane")
offset := claygo.Vector2{}
if data := ctx.GetScrollContainerData(paneID); data.Found && data.ScrollPosition != nil {
    offset = *data.ScrollPosition
}

claygo.BoxID(ctx, "Pane", claygo.Decl{
    Layout: claygo.LayoutConfig{
        Sizing: claygo.Sizing{Width: claygo.SizingFixed(320), Height: claygo.SizingFixed(240)},
    },
    Clip: claygo.ClipElementConfig{Vertical: true, ChildOffset: offset},
}, func() {
    // Declare content taller than the pane here.
})
```

For host-managed scroll views, enable external scroll handling with
`ctx.SetExternalScrollHandlingEnabled(true)` and install
`ctx.SetQueryScrollOffsetFunction(...)`.

## Thread safety

A `Context` is **not** safe for concurrent use. Build the layout on one
goroutine. If a renderer runs on another, snapshot the `RenderCommandArray`
before handing it off — the live array is overwritten by the next `EndLayout`.

## Relationship to upstream Clay

claygo mirrors the structure and behaviour of Clay closely enough that source
comments reference upstream line numbers (`oracle/clay.h ~line NNNN`). The
`oracle/` directory builds the real C library and the test suite checks
claygo's layout output against it for parity. See the
[Clay documentation](https://github.com/nicbarker/clay) for conceptual
background — most of it applies directly.

The exact upstream commit is pinned in `oracle/CLAY_VERSION`. When Clay
changes, `make -C oracle update-clay REF=<tag>` re-vendors the header and
regenerates the golden corpus; `oracle/UPSTREAM.md` documents the full
bump workflow.

Full Go API reference: run `go doc github.com/befabri/claygo` or browse it on
pkg.go.dev once published.

## License

[zlib/libpng](LICENSE.md). claygo is a derivative work of Clay
(© 2024 Nic Barker) and is distributed under the same license; the Go port is
© 2026 Benjamin Fabri. The original Clay copyright notice is retained in both
`LICENSE.md` and the bundled `oracle/clay.h`.
