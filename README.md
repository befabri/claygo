# claygo

A Go port of [nicbarker/clay](https://github.com/nicbarker/clay) for laying out
2D user interfaces with a flexbox-style model. Describe your UI each frame;
claygo calculates sizes and positions, then returns drawing commands for your
renderer.

- **No cgo, no dependencies.** Standard library only.
- **Set layout limits.** Choose the capacity when you create a context;
  claygo reports overflows through your `ErrorHandler`.
- **Immediate-mode API.** Declare the UI each frame, without keeping widget
  objects between frames.
- **Use your own renderer.** Draw with SDL, OpenGL, a terminal, a canvas, or
  whatever fits your application.
- **Tested against Clay's C implementation.** Shared layout scenes check that
  claygo produces the same render commands. See
  [Relationship to upstream Clay](#relationship-to-upstream-clay) for the
  features and fixes that differ from upstream.

## Which Go port of Clay should I use?

claygo is written by hand in Go and provides layout only. Choose it if you
want to work directly with the layout code and use your own renderer.

[TotallyGamerJet/clay](https://github.com/TotallyGamerJet/clay) generates its
layout code from Clay's C source and includes renderers for SDL, Ebitengine,
and software drawing.

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

Feed input before `BeginLayout`, then draw the commands returned by `EndLayout`:

```go
ctx.SetPointerState(pos, isDown)
ctx.UpdateScrollContainers(true, wheel, dt)
ctx.BeginLayout()
// Declare the UI with claygo.Box and claygo.Text.
cmds := ctx.EndLayout(dt)
myRenderer.Draw(cmds)
```

`SetPointerState` can be skipped if you have no pointer input;
`UpdateScrollContainers` if you have no clip containers.

### Memory model

The arena sets a memory budget for claygo's internal buffers. The buffers are
Go slices managed by the garbage collector. Choose the budget before creating
the context:

```go
arena := claygo.CreateArenaWithCapacity(claygo.MinMemorySize())
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

arena := claygo.CreateArenaWithCapacity(claygo.MinMemorySize())
```

Changing those limits after a `Context` has been initialized does not resize
the buffers already reserved for that context.

### Text measurement

claygo asks your renderer how big the text is. Install a measurement callback
when you create the context. claygo measures individual words for wrapping and
caches the results:

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
    arena := claygo.CreateArenaWithCapacity(claygo.MinMemorySize())
    ctx := claygo.Initialize(arena,
        claygo.Dimensions{Width: 1280, Height: 720},
        claygo.ErrorHandler{Func: func(e claygo.ErrorData) {
            log.Printf("[clay] %s", e.Text)
        }})

    ctx.SetMeasureTextFunction(func(s claygo.StringSlice, cfg *claygo.TextElementConfig, _ any) claygo.Dimensions {
        cw := float32(cfg.FontSize) * 0.55 // stub: ~0.55em per char
        return claygo.Dimensions{Width: float32(len(s.Text)) * cw, Height: float32(cfg.FontSize + 4)}
    }, nil)

    // Draw one frame.
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

`EndLayout` returns a `RenderCommandArray` in draw order. Process the commands
from first to last, switching on `cmd.CommandType` to read the matching field
of `cmd.RenderData`:

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

## Extensions beyond upstream Clay

claygo adds child wrapping and clip scopes for floating panels. Both are off
by default.

### Child wrapping (`LayoutConfig.WrapChildren`)

Set `WrapChildren` to start a new row when the next child does not fit.
With `TopToBottom` layout, it starts a new column instead:

```go
claygo.Box(ctx, claygo.Decl{
    Layout: claygo.LayoutConfig{
        Sizing:       claygo.Sizing{Width: claygo.SizingGrow(), Height: claygo.SizingFit()},
        ChildGap:     8,    // between chips on a row
        WrapChildren: true, // as many chips per row as fit, then a new row
        WrapLineGap:  12,   // between the rows
    },
}, func() {
    for _, label := range labels {
        chip(ctx, label) // a FIT-width box; no column arithmetic needed
    }
})
```

Children keep their declaration order. Line breaks are chosen before `Grow`
children share the space left on their line. `ChildGap` controls the space
between children; `WrapLineGap` controls the space between rows or columns.

Alignment, borders, and scroll content size follow the wrapped lines. If all
children fit on one line, enabling wrapping leaves the layout unchanged.

### Floating clip scopes (`FloatingElementConfig.ClipScopes`)

Use `ClipScopes` to keep a floating panel's drawing and pointer input within
one or more viewports. The panel can be declared anywhere in the layout.
This example clips it to the top and bottom edges of `Pane`:

```go
claygo.BoxID(ctx, "Panel", claygo.Decl{
    Layout: claygo.LayoutConfig{
        Sizing: claygo.Sizing{Width: claygo.SizingFixed(200), Height: claygo.SizingFixed(120)},
    },
    Floating: claygo.FloatingElementConfig{
        AttachTo: claygo.AttachToRoot,
        ClipScopes: []claygo.ClipScope{
            {ElementID: claygo.GetElementID("Pane"), Vertical: true},
        },
    },
}, func() {
    // Declare the panel's contents here.
})
```

Set `Horizontal` to clip the left and right edges, or set both flags to clip
all four. This only affects drawing and pointer input; it does not move or
resize the panel.

claygo copies the active entries when you declare the panel, so you can reuse
the slice afterward. Targets can be declared later in the frame, and clipping
follows their bounds during animations. If an active target is missing, the
panel is hidden and receives no pointer input. Empty slices and entries with
both flags off add no restriction.

`ClipToAttachedParent` still applies the parent's clipping as well.
`ClipToNone` turns off that inheritance but keeps explicit scopes active.
See the [clip scopes specification](docs/clip-scopes-spec.md) for details.

## Thread safety

A `Context` is **not** safe for concurrent use. Build the layout on one
goroutine. If a renderer runs on another, copy the render commands before
handing them off; the next `EndLayout` overwrites the context's command buffer.

## Relationship to upstream Clay

claygo follows Clay's layout model. The
[Clay documentation](https://github.com/nicbarker/clay) is a useful guide to
the concepts; for the Go API, run `go doc github.com/befabri/claygo`.

The port tracks the commit recorded in [CLAY_VERSION](oracle/CLAY_VERSION).
Shared test scenes compare its output with the original C library. claygo also
includes [clipping fixes](oracle/UPSTREAM.md#native-corrections) that apply by
default, independently of the optional extensions above.

For instructions on updating the C reference or adding test scenes, see
[Tracking upstream Clay](oracle/UPSTREAM.md). To add an extension, follow the
[extension guide](docs/extensions.md).

## License

[zlib/libpng](LICENSE.md). claygo is a derivative work of Clay
(© 2024 Nic Barker) and is distributed under the same license; the Go port is
© 2026 Benjamin Fabri. The original Clay copyright notice is retained in both
`LICENSE.md` and the bundled `oracle/clay.h`.
