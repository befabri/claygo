package claygo

import (
	"reflect"
	"strings"
	"testing"
)

// Go-only tests for child wrapping. The ext_* goldens in scenes_ext_test.go
// pin C/Go agreement; these pin the semantics by hand-computed numbers.

// wrapStrip declares a LeftToRight wrapping strip with padding 8, the value
// every hand-computed inner width in this file assumes.
func wrapStrip(c *Context, id string, width float32, gap uint16, children func()) {
	BoxID(c, id, Decl{
		Layout: LayoutConfig{
			Sizing:       Sizing{Width: SizingFixed(width), Height: SizingFit()},
			Padding:      PaddingAll(8),
			ChildGap:     gap,
			WrapChildren: true,
		},
	}, children)
}

func fixedChip(c *Context, id string, i int, w, h float32) {
	BoxIDOffset(c, id, uint32(i), Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(w), Height: SizingFixed(h)}},
		BackgroundColor: RGBA(200, 80, 80, 255),
	}, nil)
}

// wrapLinesOf returns the packed lines of the element declared with id, as
// (start, count) pairs.
func wrapLinesOf(t *testing.T, c *Context, id string) [][2]int32 {
	t.Helper()
	item := c.getHashMapItem(GetElementID(id).ID)
	if item == nil || item.LayoutElement == nil {
		t.Fatalf("element %q not found", id)
	}
	lines := item.LayoutElement.WrapLines
	out := make([][2]int32, 0, lines.Length)
	for i := range lines.Length {
		out = append(out, [2]int32{lines.Data[i].Start, lines.Data[i].Count})
	}
	return out
}

// bboxOf returns the bounding box of the element declared with
// BoxIDOffset(id, offset).
func bboxOf(t *testing.T, c *Context, id string, offset uint32) BoundingBox {
	t.Helper()
	return bboxOfID(t, c, HashStringWithOffset(String{Text: id}, offset, 0))
}

// bboxOfName returns the bounding box of the element declared with BoxID(id).
func bboxOfName(t *testing.T, c *Context, id string) BoundingBox {
	t.Helper()
	return bboxOfID(t, c, GetElementID(id))
}

func bboxOfID(t *testing.T, c *Context, id ElementID) BoundingBox {
	t.Helper()
	data := c.GetElementData(id)
	if !data.Found {
		t.Fatalf("element %q not found", id.StringID.Text)
	}
	return data.BoundingBox
}

func TestWrapPacking(t *testing.T) {
	cases := []struct {
		name   string
		width  float32 // strip width; inner = width - 16
		gap    uint16
		widths []float32
		want   [][2]int32
	}{
		// Three chips of 100 with gap 8 need exactly 316 of inner width.
		{"exact fit", 332, 8, []float32{100, 100, 100}, [][2]int32{{0, 3}}},
		{"fit plus one", 332, 8, []float32{100, 100, 101}, [][2]int32{{0, 2}, {2, 1}}},
		{"gap forces a break", 316, 8, []float32{100, 100, 100}, [][2]int32{{0, 2}, {2, 1}}},
		{"no gap fits", 316, 0, []float32{100, 100, 100}, [][2]int32{{0, 3}}},
		// Padding alone fills the strip: inner size is 0, one child per line.
		{"zero inner size", 16, 8, []float32{10, 10, 10}, [][2]int32{{0, 1}, {1, 1}, {2, 1}}},
		{"lone too-wide child", 200, 8, []float32{60, 500, 60, 60}, [][2]int32{{0, 1}, {1, 1}, {2, 2}}},
		{"single child", 50, 8, []float32{500}, [][2]int32{{0, 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := freshContext(t)
			ctx.BeginLayout()
			wrapStrip(ctx, "Strip", tc.width, tc.gap, func() {
				for i, w := range tc.widths {
					fixedChip(ctx, "Chip", i, w, 20)
				}
			})
			ctx.EndLayout(0)
			if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("lines = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWrapPackingSkipsExitingChildren declares five chips, then drops the
// second one so its exit clone sits in the children list: packing must skip
// it (A, C, D share the first line; E alone on the second) while the clone
// stays inside the first line's range.
func TestWrapPackingSkipsExitingChildren(t *testing.T) {
	ctx := freshContext(t)
	extDeclareWrapExit(ctx, true)
	ctx.EndLayout(oracleTransitionDelta)
	if got := wrapLinesOf(t, ctx, "WrapExitStrip"); !reflect.DeepEqual(got, [][2]int32{{0, 3}, {3, 2}}) {
		t.Fatalf("frame 1 lines = %v, want [[0 3] [3 2]]", got)
	}
	extDeclareWrapExit(ctx, false)
	ctx.EndLayout(oracleTransitionDelta)
	if got := wrapLinesOf(t, ctx, "WrapExitStrip"); !reflect.DeepEqual(got, [][2]int32{{0, 4}, {4, 1}}) {
		t.Errorf("frame 2 lines = %v, want [[0 4] [4 1]] (exiting clone inside line 0, not counted)", got)
	}
	// The live chips take the slots a four-chip line gives them.
	if got := bboxOfName(t, ctx, "WrapExitC").X; got != 96 {
		t.Errorf("C.x = %v, want 96 (packed right after A, the exiting B is skipped)", got)
	}
	if got := bboxOfName(t, ctx, "WrapExitE").Y; got != 46 {
		t.Errorf("E.y = %v, want 46 (second line)", got)
	}
}

// TestWrapPerLineGrow checks that a line without GROW children keeps its
// content while the next line's GROW children share only that line's slack,
// and that Max caps hold.
func TestWrapPerLineGrow(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	wrapStrip(ctx, "Strip", 400, 8, func() { // inner 384
		fixedChip(ctx, "Fixed", 0, 180, 20)
		fixedChip(ctx, "Fixed", 1, 180, 20) // 368 with the gap: line 0, no grow children
		BoxIDOffset(ctx, "Grow", 0, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(100, 150), Height: SizingFixed(20)}}}, nil)
		BoxIDOffset(ctx, "Grow", 1, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(100), Height: SizingFixed(20)}}}, nil)
	})
	ctx.EndLayout(0)
	if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, [][2]int32{{0, 2}, {2, 2}}) {
		t.Fatalf("lines = %v", got)
	}
	if w := bboxOf(t, ctx, "Fixed", 1).Width; w != 180 {
		t.Errorf("fixed chip width = %v, want 180 (line without grow children keeps its content)", w)
	}
	// Line 1: 100 + 8 + 100 = 208, slack 176; the capped chip stops at 150,
	// the other takes the rest: 384 - 8 - 150 = 226.
	if w := bboxOf(t, ctx, "Grow", 0).Width; w != 150 {
		t.Errorf("capped grow chip width = %v, want 150", w)
	}
	if w := bboxOf(t, ctx, "Grow", 1).Width; w != 226 {
		t.Errorf("grow chip width = %v, want 226", w)
	}
}

// TestWrapCrossSizing checks the parent's stacked height and minimum, and that
// a fixed-height parent keeps its height.
func TestWrapCrossSizing(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	// The 300-wide column clamps the FIT strip to 300 on its cross axis: the
	// strip's own minimum (widest chip plus padding) is what lets that happen.
	BoxID(ctx, "Column", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFit()}, LayoutDirection: TopToBottom, ChildGap: 4}}, func() {
		BoxID(ctx, "Fit", Decl{Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, Padding: PaddingAll(8), ChildGap: 8, WrapChildren: true,
		}}, func() { // inner 284: [100,100] [100,100] [100]
			for i := range 5 {
				fixedChip(ctx, "Chip", i, 100, 10+float32(i)*10) // heights 10..50
			}
		})
		BoxID(ctx, "Fixed", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFixed(500)}, Padding: PaddingAll(8), ChildGap: 8, WrapChildren: true},
		}, func() {
			for i := range 5 {
				fixedChip(ctx, "Other", i, 100, 20)
			}
		})
	})
	ctx.EndLayout(0)
	// Line extents 20, 40, 50 plus two gaps and padding.
	if h := bboxOfName(t, ctx, "Fit").Height; h != 16+20+40+50+16 {
		t.Errorf("fit strip height = %v, want 142 (stacked lines)", h)
	}
	fit := ctx.getHashMapItem(GetElementID("Fit").ID).LayoutElement
	if got := fit.MinDimensions.Height; got != 142 {
		t.Errorf("fit strip MinDimensions.Height = %v, want 142 (line minimums stack)", got)
	}
	if got := fit.MinDimensions.Width; got != 16+100 {
		t.Errorf("fit strip MinDimensions.Width = %v, want 116 (widest child plus padding)", got)
	}
	if h := bboxOfName(t, ctx, "Fixed").Height; h != 500 {
		t.Errorf("fixed strip height = %v, want 500", h)
	}
}

// TestWrapAlignment checks all nine ChildAlignment combinations on a
// three-line strip against hand-computed positions. Strip 300x160, padding 10,
// gap 10: lines [100x20, 100x40] (content 210, extent 40), [150x30, 60x30]
// (220, 30) and [200x50] (200, 50); the stacked lines fill the inner height
// exactly, so line tops are 10, 60 and 100.
func TestWrapAlignment(t *testing.T) {
	xs := []LayoutAlignmentX{AlignXLeft, AlignXCenter, AlignXRight}
	ys := []LayoutAlignmentY{AlignYTop, AlignYCenter, AlignYBottom}
	// Along-axis slack per line: 70, 60, 80.
	extraX := [3][3]float32{{0, 0, 0}, {35, 30, 40}, {70, 60, 80}}
	// Only the first chip (20 tall in a 40 line) has cross-axis whitespace.
	firstChipY := [3]float32{10, 20, 30}
	for xi, ax := range xs {
		for yi, ay := range ys {
			ctx := freshContext(t)
			ctx.BeginLayout()
			BoxID(ctx, "Strip", Decl{Layout: LayoutConfig{
				Sizing:         Sizing{Width: SizingFixed(300), Height: SizingFixed(160)},
				Padding:        PaddingAll(10),
				ChildGap:       10,
				ChildAlignment: ChildAlignment{X: ax, Y: ay},
				WrapChildren:   true,
			}}, func() {
				fixedChip(ctx, "Chip", 0, 100, 20)
				fixedChip(ctx, "Chip", 1, 100, 40)
				fixedChip(ctx, "Chip", 2, 150, 30)
				fixedChip(ctx, "Chip", 3, 60, 30)
				fixedChip(ctx, "Chip", 4, 200, 50)
			})
			ctx.EndLayout(0)
			want := []Vector2{
				{X: 10 + extraX[xi][0], Y: firstChipY[yi]},
				{X: 10 + extraX[xi][0] + 110, Y: 10},
				{X: 10 + extraX[xi][1], Y: 60},
				{X: 10 + extraX[xi][1] + 160, Y: 60},
				{X: 10 + extraX[xi][2], Y: 100},
			}
			for i, w := range want {
				b := bboxOf(t, ctx, "Chip", uint32(i))
				if b.X != w.X || b.Y != w.Y {
					t.Errorf("align (%d,%d) chip %d at (%v,%v), want (%v,%v)", ax, ay, i, b.X, b.Y, w.X, w.Y)
				}
			}
		}
	}
}

// TestWrapDividers checks that within-line dividers tile the parent's height
// band by band, that between-line dividers sit in the middle of the gap, and
// that every divider gets a distinct id.
func TestWrapDividers(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Strip", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFit()}, Padding: PaddingAll(8), ChildGap: 10, WrapChildren: true},
		Border: BorderElementConfig{Color: RGBA(240, 200, 80, 255), Width: BorderWidth{BetweenChildren: 2}},
	}, func() {
		for i := range 7 { // inner 284: three per line, lines of height 30 at y 8, 48, 88
			fixedChip(ctx, "Chip", i, 80, 30)
		}
	})
	cmds := ctx.EndLayout(0)
	var vertical, horizontal []RenderCommand
	// Every drawn rectangle and border in the frame (children, the strip's
	// border, within-line and between-line dividers) must carry its own id:
	// dividers hash offsets past the child range, between-line ones past the
	// within-line range.
	ids := map[uint32]bool{}
	for _, cmd := range cmds.Commands {
		if cmd.CommandType != RenderCommandTypeRectangle && cmd.CommandType != RenderCommandTypeBorder {
			continue
		}
		if ids[cmd.ID] {
			t.Errorf("render command id %d emitted twice", cmd.ID)
		}
		ids[cmd.ID] = true
		if cmd.CommandType != RenderCommandTypeRectangle || cmd.RenderData.Rectangle.BackgroundColor != RGBA(240, 200, 80, 255) {
			continue
		}
		if cmd.BoundingBox.Width == 2 {
			vertical = append(vertical, cmd)
		} else {
			horizontal = append(horizontal, cmd)
		}
	}
	if len(vertical) != 4 || len(horizontal) != 2 {
		t.Fatalf("got %d within-line and %d between-line dividers, want 4 and 2", len(vertical), len(horizontal))
	}
	// Bands: line 0 [0, 43], line 1 [43, 83], line 2 [83, 124]; only lines 0
	// and 1 have a second child, so their bands must meet at 43.
	wantBands := [][2]float32{{0, 43}, {0, 43}, {43, 83}, {43, 83}}
	for i, cmd := range vertical {
		top, bottom := cmd.BoundingBox.Y, cmd.BoundingBox.Y+cmd.BoundingBox.Height
		if top != wantBands[i][0] || bottom != wantBands[i][1] {
			t.Errorf("within-line divider %d spans [%v, %v], want %v", i, top, bottom, wantBands[i])
		}
	}
	// Gap midpoints between lines: 43 and 83, minus half the divider width.
	for i, wantY := range []float32{42, 82} {
		if got := horizontal[i].BoundingBox.Y; got != wantY {
			t.Errorf("between-line divider %d at y=%v, want %v", i, got, wantY)
		}
		if got := horizontal[i].BoundingBox.Width; got != 300 {
			t.Errorf("between-line divider %d width = %v, want the parent's 300", i, got)
		}
	}
}

// TestWrapScrollContentSize checks the scroll content size a clipping wrap
// parent reports: widest line plus padding by stacked lines plus padding.
func TestWrapScrollContentSize(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Pane", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFixed(80)}, Padding: PaddingAll(8), ChildGap: 8, WrapChildren: true},
		Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		for i := range 9 { // inner 284: three per line, 3 lines of 30
			fixedChip(ctx, "Chip", i, 80, 30)
		}
	})
	ctx.EndLayout(0)
	data := ctx.GetScrollContainerData(GetElementID("Pane"))
	if !data.Found {
		t.Fatal("scroll container not found")
	}
	want := Dimensions{Width: 3*80 + 2*8 + 16, Height: 3*30 + 2*8 + 16}
	if data.ContentDimensions != want {
		t.Errorf("content size = %+v, want %+v", data.ContentDimensions, want)
	}
	// Lines keep their natural height inside the short clip: the chips on the
	// third line sit at 8 + 2*(30+8).
	if y := bboxOf(t, ctx, "Chip", 8).Y; y != 84 {
		t.Errorf("last line y = %v, want 84", y)
	}
}

// TestWrapIdentityProperty runs every upstream scene with WrapChildren forced
// on for every LeftToRight element and asserts the committed golden still
// matches. Scenes where any element actually wrapped onto more than one line
// are skipped; everything else must be byte-identical (docs/child-wrap-spec.md,
// section 10).
func TestWrapIdentityProperty(t *testing.T) {
	compared := 0
	for name, build := range goldenScenes {
		t.Run(name, func(t *testing.T) {
			arena := CreateArenaWithCapacity(MinMemorySize())
			ctx := Initialize(arena, goldenViewport, ErrorHandler{
				Func: func(err ErrorData) { t.Errorf("clay error: type=%d text=%q", err.Type, err.Text) },
			})
			ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
			ctx.BeginLayout()
			build(ctx)
			// Every non-text row, the root included, now wraps. Closed elements
			// already computed their minimum, so redo the wrap-axis one; the
			// root closes inside EndLayout and picks up the flag there.
			for i := range ctx.layoutElements.Length {
				le := ctx.layoutElements.Get(i)
				if le.IsTextElement || le.Config.Layout.LayoutDirection != LeftToRight {
					continue
				}
				le.Config.Layout.WrapChildren = true
				if i != 0 {
					ctx.wrapCloseElement(le)
				}
			}
			cmds := ctx.EndLayout(0)
			for i := range ctx.layoutElements.Length {
				if ctx.layoutElements.Get(i).WrapLines.Length > 1 {
					t.Skipf("element %d wrapped onto %d lines", i, ctx.layoutElements.Get(i).WrapLines.Length)
				}
			}
			want, err := loadGolden(name)
			if err != nil {
				t.Fatalf("load golden: %v", err)
			}
			if got := toGoldenJSON(cmds); !reflect.DeepEqual(got, want) {
				t.Errorf("scene %q with WrapChildren forced on diverges from its golden\n--- got ---\n%s\n--- want ---\n%s",
					name, prettyJSON(got), prettyJSON(want))
			}
			// Second pass over the same tree, as a transition frame runs: lines
			// re-pack from grown float32 sizes and must still match what the
			// plain tree produces on its own second pass.
			wrappedAgain := toGoldenJSON(ctx.calculateFinalLayout())
			plain := freshContext(t)
			plain.BeginLayout()
			build(plain)
			plain.EndLayout(0)
			plainAgain := toGoldenJSON(plain.calculateFinalLayout())
			if !reflect.DeepEqual(wrappedAgain, plainAgain) {
				t.Errorf("scene %q second pass with WrapChildren forced on diverges from the plain second pass\n--- wrapped ---\n%s\n--- plain ---\n%s",
					name, prettyJSON(wrappedAgain), prettyJSON(plainAgain))
			}
			compared++
		})
	}
	if compared == 0 {
		t.Fatal("no scene was compared")
	}
}

// TestWrapEqualWidthGrid builds clayshell's fourteen-chip strip the hand-rolled
// way (rows of a computed column count) and with WrapChildren, and compares
// the chips' bounding boxes.
func TestWrapEqualWidthGrid(t *testing.T) {
	const chipWidth, chipHeight, gap, stripWidth = 118, 28, 8, 650
	columns := int((stripWidth - 16 + gap) / (chipWidth + gap))
	manual := freshContext(t)
	manual.BeginLayout()
	BoxID(manual, "Strip", Decl{Layout: LayoutConfig{
		Sizing: Sizing{Width: SizingFixed(stripWidth), Height: SizingFit()}, Padding: PaddingAll(8), ChildGap: gap, LayoutDirection: TopToBottom,
	}}, func() {
		for start := 0; start < 14; start += columns {
			Box(manual, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(), Height: SizingFit()}, ChildGap: gap}}, func() {
				for i := start; i < min(start+columns, 14); i++ {
					fixedChip(manual, "Chip", i, chipWidth, chipHeight)
				}
			})
		}
	})
	manual.EndLayout(0)

	wrapped := freshContext(t)
	wrapped.BeginLayout()
	wrapStrip(wrapped, "Strip", stripWidth, gap, func() {
		for i := range 14 {
			fixedChip(wrapped, "Chip", i, chipWidth, chipHeight)
		}
	})
	wrapped.EndLayout(0)

	for i := range 14 {
		a, b := bboxOf(t, manual, "Chip", uint32(i)), bboxOf(t, wrapped, "Chip", uint32(i))
		if a != b {
			t.Errorf("chip %d: hand-rolled %+v, wrapped %+v", i, a, b)
		}
	}
	if a, b := bboxOfName(t, manual, "Strip"), bboxOfName(t, wrapped, "Strip"); a != b {
		t.Errorf("strip: hand-rolled %+v, wrapped %+v", a, b)
	}
}

// TestWrapLinePoolCapacity fills the element budget with a strip whose
// children each land on their own line: the line pool must hold them without
// an error.
func TestWrapLinePoolCapacity(t *testing.T) {
	var errs []ErrorData
	arena := CreateArenaWithCapacity(MinMemorySize())
	ctx := Initialize(arena, goldenViewport, ErrorHandler{Func: func(err ErrorData) { errs = append(errs, err) }})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	chips := int(ctx.maxElementCount) - 3 // root, strip, and the capacity-1 guard
	ctx.BeginLayout()
	wrapStrip(ctx, "Strip", 20, 8, func() { // inner 4: every 10-wide chip is a line
		for i := range chips {
			fixedChip(ctx, "Chip", i, 10, 10)
		}
	})
	ctx.EndLayout(0)
	if len(errs) != 0 {
		t.Fatalf("errors: %+v", errs)
	}
	if got := ctx.getHashMapItem(GetElementID("Strip").ID).LayoutElement.WrapLines.Length; got != int32(chips) {
		t.Errorf("lines = %d, want %d", got, chips)
	}
}

// TestWrapLayoutIsAllocationFree pins the steady-state allocation contract
// with wrapping in use (rows and columns, so the second sizing sweep runs).
func TestWrapLayoutIsAllocationFree(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Root", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(), Height: SizingGrow()}, LayoutDirection: TopToBottom, ChildGap: 8}}, func() {
		wrapStrip(ctx, "Rows", 300, 8, func() {
			for i := range 12 {
				fixedChip(ctx, "Chip", i, 80, 30)
			}
		})
		BoxID(ctx, "Cols", Decl{Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingFit(), Height: SizingFixed(120)}, Padding: PaddingAll(8), ChildGap: 8, LayoutDirection: TopToBottom, WrapChildren: true,
		}}, func() {
			for i := range 12 {
				fixedChip(ctx, "Cell", i, 40, 30)
			}
		})
	})
	ctx.EndLayout(0)
	for range 3 {
		ctx.calculateFinalLayout()
	}
	if avg := testing.AllocsPerRun(200, func() { ctx.calculateFinalLayout() }); avg != 0 {
		t.Errorf("calculateFinalLayout with wrapping allocates %v/call after warmup, want 0", avg)
	}
}

// TestWrapColumnSecondSweep checks the column-wrap path end to end: a fit
// width parent grows to its stacked columns, and GROW-width children take
// their column's width after the second sizing sweep.
func TestWrapColumnSecondSweep(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Cols", Decl{Layout: LayoutConfig{
		Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(100)}, Padding: PaddingAll(8), ChildGap: 8, LayoutDirection: TopToBottom, WrapChildren: true,
	}}, func() {
		for i := range 4 { // inner height 84: two 30-tall cells per column
			BoxIDOffset(ctx, "Cell", uint32(i), Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(), Height: SizingFixed(30)}}}, nil)
		}
	})
	ctx.EndLayout(0)
	if got := wrapLinesOf(t, ctx, "Cols"); !reflect.DeepEqual(got, [][2]int32{{0, 2}, {2, 2}}) {
		t.Fatalf("columns = %v, want [[0 2] [2 2]]", got)
	}
	// Two columns share the 384 px of inner width: 188 each.
	for i := range 4 {
		b := bboxOf(t, ctx, "Cell", uint32(i))
		wantX := float32(8)
		if i >= 2 {
			wantX = 8 + 188 + 8
		}
		if b.Width != 188 || b.X != wantX {
			t.Errorf("cell %d: %+v, want width 188 at x=%v", i, b, wantX)
		}
	}
	if !ctx.wrapHasColumn {
		t.Error("wrapHasColumn not set; the second sweep did not run")
	}
}

// TestDebugViewShowsWrapChildren checks the inspector's layout section reports
// the flag.
func TestDebugViewShowsWrapChildren(t *testing.T) {
	ctx := freshContext(t)
	ctx.SetDebugModeEnabled(true)
	ctx.SetCullingEnabled(false) // the layout section runs past the 720 px viewport
	ctx.debugSelectedElementID = GetElementID("Strip").ID
	cmds := runDebugFrame(ctx, func() {
		wrapStrip(ctx, "Strip", 300, 8, func() {
			fixedChip(ctx, "Chip", 0, 80, 30)
		})
	})
	var sawTitle, sawValue bool
	for _, cmd := range cmds.Commands {
		if cmd.CommandType != RenderCommandTypeText {
			continue
		}
		text := cmd.RenderData.Text.StringContents.Text
		sawTitle = sawTitle || text == "Wrap Children"
		sawValue = sawValue || (sawTitle && strings.EqualFold(text, "true"))
	}
	if !sawTitle || !sawValue {
		t.Errorf("inspector did not show \"Wrap Children\" = true (title %v, value %v)", sawTitle, sawValue)
	}
}

// TestWrapClipParentKeepsPaddingMinimum pins the two ways a clipping wrap
// parent must stay shrinkable, as upstream's clip containers are: a wrap pane
// that clips vertically shrinks below its stacked lines inside a short column,
// and a column-wrap pane that clips horizontally keeps the width its ancestor
// gave it in the second sizing sweep instead of growing back to its columns.
func TestWrapClipParentKeepsPaddingMinimum(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Column", Decl{Layout: LayoutConfig{
		Sizing: Sizing{Width: SizingFixed(300), Height: SizingFixed(120)}, Padding: PaddingAll(8), ChildGap: 8, LayoutDirection: TopToBottom,
	}}, func() {
		BoxID(ctx, "Pane", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFit()}, Padding: PaddingAll(8), ChildGap: 8, WrapChildren: true},
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
		}, func() {
			for i := range 9 { // three lines of 30: 122 tall unclipped
				fixedChip(ctx, "Chip", i, 80, 30)
			}
		})
		BoxID(ctx, "Footer", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(30)}}}, nil)
	})
	BoxID(ctx, "Row", Decl{Layout: LayoutConfig{
		Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(150)}, Padding: PaddingAll(8), ChildGap: 8,
	}}, func() {
		BoxID(ctx, "Cols", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingGrow(0)}, Padding: PaddingAll(8), ChildGap: 8, LayoutDirection: TopToBottom, WrapChildren: true},
			Clip:   ClipElementConfig{Horizontal: true, Vertical: true},
		}, func() {
			for i := range 8 { // two 40-tall cells per column: four columns, 280 wide
				fixedChip(ctx, "Cell", i, 60, 40)
			}
		})
		BoxID(ctx, "Side", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(100)}}}, nil)
	})
	ctx.EndLayout(0)
	// Column inner 104 = pane + 8 + footer 30, so the pane gets 66.
	if h := bboxOfName(t, ctx, "Pane").Height; h != 66 {
		t.Errorf("clipping wrap pane height = %v, want 66 (shrinks toward its padding-only minimum)", h)
	}
	if y := bboxOfName(t, ctx, "Footer").Y; y != 8+66+8 {
		t.Errorf("footer y = %v, want 82", y)
	}
	// Row inner 184 = cols + 8 + side 60, so the column pane gets 116.
	if w := bboxOfName(t, ctx, "Cols").Width; w != 116 {
		t.Errorf("clipping column-wrap pane width = %v, want 116 (the ancestor's decision survives the re-pack)", w)
	}
	if got := wrapLinesOf(t, ctx, "Cols"); len(got) != 4 {
		t.Errorf("columns = %v, want four", got)
	}
}

// TestWrapLinesNeverOverlapRigidChildren: a parent shorter than its stacked
// lines shrinks the line extents, but Fixed children cannot follow, so the
// next line must start below them (the parent overflows, lines do not
// overlap). 120x40 with gap 10 and three 50x30 chips: two lines of natural
// height 30, extents squashed to 15, second line still at y = 30 + 10.
func TestWrapLinesNeverOverlapRigidChildren(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Strip", Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(40)}, ChildGap: 10, WrapChildren: true}}, func() {
		for i := range 3 {
			fixedChip(ctx, "Chip", i, 50, 30)
		}
	})
	ctx.EndLayout(0)
	if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, [][2]int32{{0, 2}, {2, 1}}) {
		t.Fatalf("lines = %v", got)
	}
	if y := bboxOf(t, ctx, "Chip", 2).Y; y != 40 {
		t.Errorf("second line y = %v, want 40 (first line's 30-tall chips plus the gap)", y)
	}
}

// TestWrapLinePoolOverflowDegrades pins the soft-fail path when the line pool
// is exhausted (unreachable at the real 2x-elements capacity, so the pool is
// shrunk by hand): the error handler hears about it once, the children that
// could not start a line join the last one, and every child is still laid out.
func TestWrapLinePoolOverflowDegrades(t *testing.T) {
	var errs []ErrorData
	arena := CreateArenaWithCapacity(MinMemorySize())
	ctx := Initialize(arena, goldenViewport, ErrorHandler{Func: func(err ErrorData) { errs = append(errs, err) }})
	ctx.SetMeasureTextFunction(deterministicMeasureText, nil)
	ctx.wrapLines = NewArray[WrapLine](2, nil)
	ctx.BeginLayout()
	wrapStrip(ctx, "Strip", 20, 8, func() { // inner 4: every 10-wide chip wants its own line
		for i := range 5 {
			fixedChip(ctx, "Chip", i, 10, 10)
		}
	})
	ctx.EndLayout(0)
	if len(errs) != 1 || errs[0].Type != ErrorTypeInternalError {
		t.Fatalf("errors = %+v, want one ErrorTypeInternalError", errs)
	}
	if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, [][2]int32{{0, 1}, {1, 4}}) {
		t.Fatalf("lines = %v, want [[0 1] [1 4]] (chips 2..4 join the last line)", got)
	}
	// Line 1 sits below line 0 and lays its four chips out along x.
	for i := 1; i < 5; i++ {
		b := bboxOf(t, ctx, "Chip", uint32(i))
		if b.Y != 26 || b.X != 8+float32(i-1)*18 {
			t.Errorf("chip %d at (%v,%v), want (%v,26)", i, b.X, b.Y, 8+float32(i-1)*18)
		}
	}
}

// TestWrapPackingIsIdempotent pins the epsilon in the break rule: three GROW
// chips that share 245 px grow to fractional widths whose float32 re-sum
// lands a hair over the inner width, and a second layout pass over the same
// tree (what every transition frame does) must keep them on one line.
func TestWrapPackingIsIdempotent(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	wrapStrip(ctx, "Strip", 261, 0, func() {
		for i := range 3 {
			BoxIDOffset(ctx, "Grow", uint32(i), Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(20), Height: SizingFixed(20)}}}, nil)
		}
	})
	ctx.EndLayout(0)
	if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, [][2]int32{{0, 3}}) {
		t.Fatalf("first pass lines = %v, want one line", got)
	}
	first := bboxOf(t, ctx, "Grow", 2)
	ctx.calculateFinalLayout()
	if got := wrapLinesOf(t, ctx, "Strip"); !reflect.DeepEqual(got, [][2]int32{{0, 3}}) {
		t.Fatalf("second pass lines = %v, want the same single line", got)
	}
	if second := bboxOf(t, ctx, "Grow", 2); second != first {
		t.Errorf("second pass moved the last chip: %+v -> %+v", first, second)
	}
}

// TestWrapColumnClipDoesNotChangeGrowSizing: GROW-width cells of a column-wrap
// pane take their column's width whether or not the pane clips. Before the
// first y pass there are no columns, so the first x pass must leave the cells
// at their content width instead of growing them to the pane; otherwise a
// clipping pane (which never squashes children) reads the pane width as each
// column's content and pushes the second column outside.
func TestWrapColumnClipDoesNotChangeGrowSizing(t *testing.T) {
	for _, clip := range []bool{false, true} {
		ctx := freshContext(t)
		ctx.BeginLayout()
		BoxID(ctx, "Cols", Decl{
			Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(400), Height: SizingFixed(100)}, Padding: PaddingAll(8), ChildGap: 8, LayoutDirection: TopToBottom, WrapChildren: true},
			Clip:   ClipElementConfig{Horizontal: clip, Vertical: clip},
		}, func() {
			for i := range 4 { // 84 px inner height: two 30-tall cells per column
				BoxIDOffset(ctx, "Cell", uint32(i), Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingGrow(60), Height: SizingFixed(30)}}}, nil)
			}
		})
		ctx.EndLayout(0)
		for i := range 4 {
			b := bboxOf(t, ctx, "Cell", uint32(i))
			wantX := float32(8)
			if i >= 2 {
				wantX = 8 + 188 + 8
			}
			if b.Width != 188 || b.X != wantX {
				t.Errorf("clip=%v cell %d: %+v, want width 188 at x=%v", clip, i, b, wantX)
			}
		}
	}
}

// TestWrapDividersNeverNegative: lines pushed past a short parent's edge by
// rigid children keep their dividers inside their own band, so no render
// command has a negative size and the last band ends at its line's end.
func TestWrapDividersNeverNegative(t *testing.T) {
	ctx := freshContext(t)
	ctx.BeginLayout()
	BoxID(ctx, "Strip", Decl{
		Layout: LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(40)}, ChildGap: 10, WrapChildren: true},
		Border: BorderElementConfig{Color: RGBA(240, 200, 80, 255), Width: BorderWidth{BetweenChildren: 2}},
	}, func() {
		for i := range 6 { // three lines of two 50x30 chips at y 0, 40, 80
			fixedChip(ctx, "Chip", i, 50, 30)
		}
	})
	cmds := ctx.EndLayout(0)
	var dividers []BoundingBox
	for _, cmd := range cmds.Commands {
		if cmd.BoundingBox.Width < 0 || cmd.BoundingBox.Height < 0 {
			t.Errorf("%s has a negative size: %+v", renderCommandTypeName(cmd.CommandType), cmd.BoundingBox)
		}
		if cmd.CommandType == RenderCommandTypeRectangle && cmd.RenderData.Rectangle.BackgroundColor == RGBA(240, 200, 80, 255) && cmd.BoundingBox.Width == 2 {
			dividers = append(dividers, cmd.BoundingBox)
		}
	}
	if len(dividers) != 3 {
		t.Fatalf("got %d within-line dividers, want 3", len(dividers))
	}
	// Bands: [0, 35], [35, 75], and the last line's [75, 110]: its own end,
	// not the parent's 40.
	want := [][2]float32{{0, 35}, {35, 75}, {75, 110}}
	for i, d := range dividers {
		if d.Y != want[i][0] || d.Y+d.Height != want[i][1] {
			t.Errorf("divider %d spans [%v, %v], want %v", i, d.Y, d.Y+d.Height, want[i])
		}
	}
}
