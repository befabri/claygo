package claygo

// goldenScenes maps scene name -> Go layout builder. Each name must match a
// scene compiled into oracle/main.c, and a corresponding
// testdata/<name>.golden.json must exist.
var goldenScenes = map[string]func(*Context){
	"rect_solid":                      sceneRectSolid,
	"padded_rect":                     scenePaddedRect,
	"row_3_fixed":                     sceneRow3Fixed,
	"row_3_grow":                      sceneRow3Grow,
	"col_3_grow":                      sceneCol3Grow,
	"mixed_fixed_grow":                sceneMixedFixedGrow,
	"percent_half":                    scenePercentHalf,
	"min_max_clamp":                   sceneMinMaxClamp,
	"fit_to_children":                 sceneFitToChildren,
	"align_center":                    sceneAlignCenter,
	"align_right_bottom":              sceneAlignRightBottom,
	"text_simple":                     sceneTextSimple,
	"corner_radius":                   sceneCornerRadius,
	"border_basic":                    sceneBorderBasic,
	"border_between_children":         sceneBorderBetweenChildren,
	"border_between_children_col":     sceneBorderBetweenChildrenCol,
	"overlay_color":                   sceneOverlayColor,
	"image_basic":                     sceneImageBasic,
	"image_full_stack":                sceneImageFullStack,
	"text_wrap_words":                 sceneTextWrapWords,
	"text_align_center":               sceneTextAlignCenter,
	"text_align_right":                sceneTextAlignRight,
	"aspect_ratio":                    sceneAspectRatio,
	"nested_3_levels":                 sceneNested3Levels,
	"clip_overflow":                   sceneClipOverflow,
	"clip_scroll_offset":              sceneClipScrollOffset,
	"clip_text_wrap":                  sceneClipTextWrap,
	"floating_parent":                 sceneFloatingParent,
	"floating_z_sort":                 sceneFloatingZSort,
	"floating_in_clip":                sceneFloatingInClip,
	"grow_7_nonint":                   sceneGrow7NonInt,
	"border_between_children_odd_gap": sceneBorderBetweenChildrenOddGap,
}

// A non-nil sentinel image pointer used by sceneImageBasic.
var fakeImageData = struct{ x int }{x: 1}

func sceneRectSolid(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
		},
		BackgroundColor: RGBA(40, 40, 48, 255),
	}, nil)
}

func scenePaddedRect(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding: PaddingAll(16),
		},
		BackgroundColor: RGBA(40, 40, 48, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneRow3Fixed(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(150), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func sceneRow3Grow(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func sceneCol3Grow(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: TopToBottom,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingGrow(0)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingGrow(0)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingGrow(0)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func sceneMixedFixedGrow(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func scenePercentHalf(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding: PaddingAll(16),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingPercent(0.5), Height: SizingPercent(0.5)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneMinMaxClamp(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(50, 200), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
	})
}

func sceneFitToChildren(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFit(0), Height: SizingFit(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(150), Height: SizingFixed(80)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
	})
}

func sceneAlignCenter(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:         Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			ChildAlignment: ChildAlignment{X: AlignXCenter, Y: AlignYCenter},
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneAlignRightBottom(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:         Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			Padding:        PaddingAll(16),
			ChildAlignment: ChildAlignment{X: AlignXRight, Y: AlignYBottom},
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(100)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneTextSimple(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFit(0), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Text(c, "Hello World", TextElementConfig{
			TextColor: RGBA(240, 240, 240, 255),
			FontSize:  16,
		})
	})
}

func sceneCornerRadius(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(100)}},
		BackgroundColor: RGBA(200, 80, 80, 255),
		CornerRadius:    CornerRadius{TopLeft: 12, TopRight: 12, BottomLeft: 12, BottomRight: 12},
	}, nil)
}

func sceneBorderBasic(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(100)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
		CornerRadius:    CornerRadius{TopLeft: 8, TopRight: 8, BottomLeft: 8, BottomRight: 8},
		Border: BorderElementConfig{
			Color: RGBA(240, 200, 80, 255),
			Width: BorderWidth{Left: 2, Right: 2, Top: 2, Bottom: 2},
		},
	}, nil)
}

func sceneBorderBetweenChildren(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFit(0), Height: SizingFixed(60)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Border: BorderElementConfig{
			Color: RGBA(240, 200, 80, 255),
			Width: BorderWidth{BetweenChildren: 2},
		},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

// sceneBorderBetweenChildrenOddGap is border_between_children with gap 7 and
// divider width 3: upstream halves both with integer division (3 and 1), which
// even values never exercise.
func sceneBorderBetweenChildrenOddGap(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFit(0), Height: SizingFixed(60)},
			Padding:         PaddingAll(8),
			ChildGap:        7,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Border: BorderElementConfig{
			Color: RGBA(240, 200, 80, 255),
			Width: BorderWidth{BetweenChildren: 3},
		},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func sceneOverlayColor(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFit(0), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
		OverlayColor:    RGBA(255, 0, 0, 128),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneImageBasic(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(120)}},
		BackgroundColor: RGBA(255, 255, 255, 255),
		CornerRadius:    CornerRadius{TopLeft: 4, TopRight: 4, BottomLeft: 4, BottomRight: 4},
		Image:           ImageElementConfig{ImageData: &fakeImageData},
	}, nil)
}

func sceneTextWrapWords(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(200), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Text(c, "The quick brown fox jumps over the lazy dog", TextElementConfig{
			TextColor: RGBA(240, 240, 240, 255),
			FontSize:  16,
			WrapMode:  TextWrapWords,
		})
	})
}

func sceneAspectRatio(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingFixed(200),
				Height: SizingFit(0),
			},
		},
		BackgroundColor: RGBA(200, 80, 80, 255),
		AspectRatio:     AspectRatioElementConfig{AspectRatio: 2.0},
	}, nil)
}

func sceneNested3Levels(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:  Sizing{Width: SizingFit(0), Height: SizingFit(0)},
				Padding: PaddingAll(16),
			},
			BackgroundColor: RGBA(80, 80, 120, 255),
		}, func() {
			Box(c, Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}},
				BackgroundColor: RGBA(200, 80, 80, 255),
			}, nil)
		})
	})
}

func sceneClipOverflow(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(150)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneFloatingParent(c *Context) {
	BoxID(c, "Anchor", Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(300), Height: SizingFixed(200)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
			Floating: FloatingElementConfig{
				AttachTo: AttachToParent,
				AttachPoints: FloatingAttachPoints{
					Parent:  AttachPointLeftBottom,
					Element: AttachPointLeftTop,
				},
			},
		}, nil)
	})
}

func sceneBorderBetweenChildrenCol(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingFixed(80), Height: SizingFit(0)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: TopToBottom,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Border: BorderElementConfig{
			Color: RGBA(240, 200, 80, 255),
			Width: BorderWidth{BetweenChildren: 2},
		},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(40)}},
			BackgroundColor: RGBA(80, 80, 200, 255),
		}, nil)
	})
}

func sceneImageFullStack(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(120)}},
		BackgroundColor: RGBA(255, 255, 255, 255),
		OverlayColor:    RGBA(0, 0, 255, 80),
		CornerRadius:    UniformCornerRadius(8),
		Border: BorderElementConfig{
			Color: RGBA(240, 200, 80, 255),
			Width: BorderOutside(2),
		},
		Image: ImageElementConfig{ImageData: &fakeImageData},
	}, nil)
}

func sceneTextAlignCenter(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(200), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Text(c, "The quick brown fox jumps over the lazy dog", TextElementConfig{
			TextColor:     RGBA(240, 240, 240, 255),
			FontSize:      16,
			WrapMode:      TextWrapWords,
			TextAlignment: TextAlignCenter,
		})
	})
}

func sceneTextAlignRight(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(200), Height: SizingFit(0)},
			Padding: PaddingAll(8),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Text(c, "The quick brown fox jumps over the lazy dog", TextElementConfig{
			TextColor:     RGBA(240, 240, 240, 255),
			FontSize:      16,
			WrapMode:      TextWrapWords,
			TextAlignment: TextAlignRight,
		})
	})
}

func sceneFloatingZSort(c *Context) {
	BoxID(c, "AnchorZ", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(300), Height: SizingFixed(200)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
			Floating: FloatingElementConfig{
				ZIndex:       2,
				AttachTo:     AttachToParent,
				AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
			},
		}, nil)
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(80, 200, 80, 255),
			Floating: FloatingElementConfig{
				ZIndex:       1,
				AttachTo:     AttachToParent,
				AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
				Offset:       Vector2{X: 100, Y: 0},
			},
		}, nil)
	})
}

func sceneClipScrollOffset(c *Context) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(120), Height: SizingFixed(80)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Clip: ClipElementConfig{
			Horizontal:  true,
			Vertical:    true,
			ChildOffset: Vector2{X: -20, Y: -10},
		},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(150)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
		}, nil)
	})
}

func sceneClipTextWrap(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:  Sizing{Width: SizingFixed(160), Height: SizingFixed(40)},
			Padding: PaddingAll(4),
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Text(c, "The quick brown fox jumps over the lazy dog", TextElementConfig{
			TextColor: RGBA(240, 240, 240, 255),
			FontSize:  16,
			WrapMode:  TextWrapWords,
		})
	})
}

func sceneFloatingInClip(c *Context) {
	BoxID(c, "ClipParent", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(200), Height: SizingFixed(150)}},
		BackgroundColor: RGBA(30, 30, 36, 255),
		Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
	}, func() {
		Box(c, Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(60)}},
			BackgroundColor: RGBA(200, 80, 80, 255),
			Floating: FloatingElementConfig{
				AttachTo:     AttachToParent,
				AttachPoints: FloatingAttachPoints{Parent: AttachPointLeftTop, Element: AttachPointLeftTop},
				ClipTo:       ClipToAttachedParent,
				Offset:       Vector2{X: 10, Y: 10},
			},
		}, nil)
	})
}

func sceneGrow7NonInt(c *Context) {
	Box(c, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0), Height: SizingFixed(80)},
			Padding:         PaddingAll(8),
			ChildGap:        8,
			LayoutDirection: LeftToRight,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}, func() {
		for range 7 {
			Box(c, Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(60)}},
				BackgroundColor: RGBA(200, 80, 80, 255),
			}, nil)
		}
	})
}
