package claygo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Golden tests: for each scene name registered in goldenScenes, we run the
// pure-Go layout, marshal the result into the same JSON shape that
// oracle/main.c emits, parse the committed testdata/<name>.golden.json
// produced by the C upstream, and compare the two as Go structs.

// goldenViewport is the layout dimensions every scene runs against. Matches
// the C oracle's viewport so positions and grow-shares match byte-for-byte.
var goldenViewport = Dimensions{Width: 1280, Height: 720}

// deterministicMeasureText is the byte-identical port of the C oracle's fake
// text measurement function (oracle/main.c::measure_text).
func deterministicMeasureText(text StringSlice, cfg *TextElementConfig, _ any) Dimensions {
	charW := float32(int(float32(cfg.FontSize) * 0.55))
	chars := len(text.Text)
	gaps := float32(0)
	if chars > 0 {
		gaps = float32(chars - 1)
	}
	w := float32(chars)*charW + gaps*float32(cfg.LetterSpacing)
	var h float32
	if cfg.LineHeight > 0 {
		h = float32(cfg.LineHeight)
	} else {
		h = float32(cfg.FontSize + 4)
	}
	return Dimensions{Width: w, Height: h}
}

// runScene wires up a fresh Context for one scene and returns the resulting
// render commands.
func runScene(t *testing.T, build func(*Context)) RenderCommandArray {
	t.Helper()
	mem := make([]byte, MinMemorySize())
	arena := CreateArenaWithCapacityAndMemory(uint(len(mem)), mem)
	ctx := Initialize(arena, goldenViewport, ErrorHandler{
		Func: func(err ErrorData) {
			t.Errorf("clay error: type=%d text=%q", err.Type, err.Text)
		},
	})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	ctx.BeginLayout()
	build(ctx)
	return ctx.EndLayout(0)
}

// TestGoldens iterates every registered scene and asserts the Go-produced
// command list matches the committed C-oracle JSON.
func TestGoldens(t *testing.T) {
	for name, build := range goldenScenes {
		t.Run(name, func(t *testing.T) {
			cmds := runScene(t, build)
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

// ---------------------------------------------------------------------------
// Golden JSON shape — mirrors oracle/main.c::dump_command exactly.
// ---------------------------------------------------------------------------

type goldenArray struct {
	Commands []goldenCommand `json:"commands"`
}

type goldenCommand struct {
	Type            string      `json:"type"`
	BBox            goldenBBox  `json:"bbox"`
	ZIndex          int16       `json:"zIndex"`
	ID              uint32      `json:"id"`
	BackgroundColor *[4]float32 `json:"backgroundColor,omitempty"`
	Color           *[4]float32 `json:"color,omitempty"`
	CornerRadius    *[4]float32 `json:"cornerRadius,omitempty"`
	Width           *[5]uint16  `json:"width,omitempty"`
	Text            *string     `json:"text,omitempty"`
	FontID          uint16      `json:"fontId,omitempty"`
	FontSize        uint16      `json:"fontSize,omitempty"`
	LetterSpacing   uint16      `json:"letterSpacing,omitempty"`
	LineHeight      uint16      `json:"lineHeight,omitempty"`
	Horizontal      *bool       `json:"horizontal,omitempty"`
	Vertical        *bool       `json:"vertical,omitempty"`
}

type goldenBBox struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

func toGoldenJSON(arr RenderCommandArray) goldenArray {
	out := goldenArray{Commands: make([]goldenCommand, 0, len(arr.Commands))}
	for i := range arr.Commands {
		out.Commands = append(out.Commands, toGoldenCommand(&arr.Commands[i]))
	}
	return out
}

func toGoldenCommand(cmd *RenderCommand) goldenCommand {
	out := goldenCommand{
		Type:   renderCommandTypeName(cmd.CommandType),
		BBox:   goldenBBox{cmd.BoundingBox.X, cmd.BoundingBox.Y, cmd.BoundingBox.Width, cmd.BoundingBox.Height},
		ZIndex: cmd.ZIndex,
		ID:     cmd.ID,
	}
	switch cmd.CommandType {
	case RenderCommandTypeRectangle:
		c := colorArr(cmd.RenderData.Rectangle.BackgroundColor)
		r := cornerArr(cmd.RenderData.Rectangle.CornerRadius)
		out.BackgroundColor = &c
		out.CornerRadius = &r
	case RenderCommandTypeBorder:
		c := colorArr(cmd.RenderData.Border.Color)
		r := cornerArr(cmd.RenderData.Border.CornerRadius)
		w := [5]uint16{
			cmd.RenderData.Border.Width.Left,
			cmd.RenderData.Border.Width.Right,
			cmd.RenderData.Border.Width.Top,
			cmd.RenderData.Border.Width.Bottom,
			cmd.RenderData.Border.Width.BetweenChildren,
		}
		out.Color = &c
		out.CornerRadius = &r
		out.Width = &w
	case RenderCommandTypeText:
		s := cmd.RenderData.Text.StringContents.Text
		c := colorArr(cmd.RenderData.Text.TextColor)
		out.Text = &s
		out.Color = &c
		out.FontID = cmd.RenderData.Text.FontID
		out.FontSize = cmd.RenderData.Text.FontSize
		out.LetterSpacing = cmd.RenderData.Text.LetterSpacing
		out.LineHeight = cmd.RenderData.Text.LineHeight
	case RenderCommandTypeImage:
		c := colorArr(cmd.RenderData.Image.BackgroundColor)
		r := cornerArr(cmd.RenderData.Image.CornerRadius)
		out.BackgroundColor = &c
		out.CornerRadius = &r
	case RenderCommandTypeScissorStart, RenderCommandTypeScissorEnd:
		h := cmd.RenderData.Clip.Horizontal
		v := cmd.RenderData.Clip.Vertical
		out.Horizontal = &h
		out.Vertical = &v
	case RenderCommandTypeOverlayColorStart, RenderCommandTypeOverlayColorEnd:
		c := colorArr(cmd.RenderData.OverlayColor.Color)
		out.Color = &c
	}
	return out
}

func renderCommandTypeName(t RenderCommandType) string {
	switch t {
	case RenderCommandTypeNone:
		return "NONE"
	case RenderCommandTypeRectangle:
		return "RECTANGLE"
	case RenderCommandTypeBorder:
		return "BORDER"
	case RenderCommandTypeText:
		return "TEXT"
	case RenderCommandTypeImage:
		return "IMAGE"
	case RenderCommandTypeScissorStart:
		return "SCISSOR_START"
	case RenderCommandTypeScissorEnd:
		return "SCISSOR_END"
	case RenderCommandTypeOverlayColorStart:
		return "OVERLAY_COLOR_START"
	case RenderCommandTypeOverlayColorEnd:
		return "OVERLAY_COLOR_END"
	case RenderCommandTypeCustom:
		return "CUSTOM"
	default:
		return "UNKNOWN"
	}
}

func colorArr(c Color) [4]float32 { return [4]float32{c.R, c.G, c.B, c.A} }
func cornerArr(c CornerRadius) [4]float32 {
	return [4]float32{c.TopLeft, c.TopRight, c.BottomLeft, c.BottomRight}
}

func loadGolden(name string) (goldenArray, error) {
	path := filepath.Join("testdata", name+".golden.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenArray{}, err
	}
	var arr goldenArray
	if err := json.Unmarshal(data, &arr); err != nil {
		return goldenArray{}, err
	}
	return arr, nil
}

func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "<marshal error: " + err.Error() + ">"
	}
	var s strings.Builder
	s.Write(b)
	return s.String()
}
