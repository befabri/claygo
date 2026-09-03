package claygo

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// extensionScenes maps scene name -> Go layout builder for the claygo
// extensions upstream Clay does not have. Each name carries the ext_ prefix,
// must match a scene compiled into the patched oracle binary (oracle/main.c,
// inside #ifndef CLAY_ORACLE_UPSTREAM), and has a testdata/<name>.golden.json
// regenerated from that binary. Builders drive their own frames so the one
// multi-frame transition scene fits the same table.
var extensionScenes = map[string]func(*Context) RenderCommandArray{
	"ext_wrap_rows_basic":              sceneExtWrapRowsBasic,
	"ext_wrap_rows_single_line":        sceneExtWrapRowsSingleLine,
	"ext_wrap_rows_grow":               sceneExtWrapRowsGrow,
	"ext_wrap_rows_percent":            sceneExtWrapRowsPercent,
	"ext_wrap_rows_align_center":       sceneExtWrapRowsAlignCenter,
	"ext_wrap_rows_align_right_bottom": sceneExtWrapRowsAlignRightBottom,
	"ext_wrap_rows_gap_borders":        sceneExtWrapRowsGapBorders,
	"ext_wrap_rows_lone_wide":          sceneExtWrapRowsLoneWide,
	"ext_wrap_rows_text_children":      sceneExtWrapRowsTextChildren,
	"ext_wrap_rows_fit_in_grow":        sceneExtWrapRowsFitInGrow,
	"ext_wrap_rows_clip_scroll":        sceneExtWrapRowsClipScroll,
	"ext_wrap_rows_nested":             sceneExtWrapRowsNested,
	"ext_wrap_exit_transition":         sceneExtWrapExitTransition,
	"ext_wrap_cols_fixed_height":       sceneExtWrapColsFixedHeight,
	"ext_wrap_cols_grow_height":        sceneExtWrapColsGrowHeight,
	"ext_wrap_clip_shrink":             sceneExtWrapClipShrink,
	"ext_wrap_rows_short_parent":       sceneExtWrapRowsShortParent,
	"ext_wrap_cols_clip_grow":          sceneExtWrapColsClipGrow,
	"ext_wrap_rows_line_gap":           sceneExtWrapRowsLineGap,
	"ext_wrap_rows_line_gap_slack":     sceneExtWrapRowsLineGapSlack,
	"ext_wrap_rows_line_gap_borders":   sceneExtWrapRowsLineGapBorders,
	"ext_wrap_cols_line_gap":           sceneExtWrapColsLineGap,
	"ext_wrap_rows_line_gap_scroll":    sceneExtWrapRowsLineGapScroll,
}

// extensionScenePrefix separates extension scenes from the upstream corpus in
// every list the parity test compares.
const extensionScenePrefix = "ext_"

func extensionKeys(m map[string]func(*Context) RenderCommandArray) []string {
	return slices.Collect(maps.Keys(m))
}

// TestExtensionSceneNaming pins the naming contract the oracle Makefile and
// parity test rely on: extension scenes and only extension scenes start with
// ext_.
func TestExtensionSceneNaming(t *testing.T) {
	for name := range extensionScenes {
		if !strings.HasPrefix(name, extensionScenePrefix) {
			t.Errorf("extensionScenes has %q, which lacks the %q prefix", name, extensionScenePrefix)
		}
	}
	for _, name := range append(keys(goldenScenes), transitionKeys(goldenTransitionScenes)...) {
		if strings.HasPrefix(name, extensionScenePrefix) {
			t.Errorf("upstream scene %q carries the %q prefix reserved for extensions", name, extensionScenePrefix)
		}
	}
}

// TestExtensionGoldens asserts the Go port reproduces the patched oracle's
// output for every extension scene, byte for byte, the same way TestGoldens
// does for the upstream corpus.
func TestExtensionGoldens(t *testing.T) {
	for name, frames := range extensionScenes {
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

// extChipColor mirrors oracle/main.c::ext_chip_color.
func extChipColor(i int) Color {
	palette := [3]Color{RGBA(200, 80, 80, 255), RGBA(80, 200, 80, 255), RGBA(80, 80, 200, 255)}
	return palette[i%3]
}

// extChip mirrors oracle/main.c::ext_chip: a fixed-size chip with the i-th
// palette color.
func extChip(c *Context, i int, w, h float32) {
	Box(c, Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(w), Height: SizingFixed(h)}},
		BackgroundColor: extChipColor(i),
	}, nil)
}

func extStripDecl(width float32, gap uint16) Decl {
	return Decl{
		Layout: LayoutConfig{
			Sizing:       Sizing{Width: SizingFixed(width), Height: SizingFit()},
			Padding:      PaddingAll(8),
			ChildGap:     gap,
			WrapChildren: true,
			WrapLineGap:  gap,
		},
		BackgroundColor: RGBA(30, 30, 36, 255),
	}
}

func singleFrame(c *Context, build func()) RenderCommandArray {
	c.BeginLayout()
	build()
	return c.EndLayout(0)
}

func sceneExtWrapRowsBasic(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, extStripDecl(650, 8), func() {
			for i := range 14 {
				extChip(c, i, 118, 28)
			}
		})
	})
}

func sceneExtWrapRowsSingleLine(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, ChildGap: 16, LayoutDirection: TopToBottom}}, func() {
			for strip := range 2 {
				Box(c, Decl{
					Layout: LayoutConfig{
						Sizing:         Sizing{Width: SizingFixed(650), Height: SizingFit()},
						Padding:        PaddingAll(8),
						ChildGap:       8,
						ChildAlignment: ChildAlignment{X: AlignXCenter, Y: AlignYCenter},
						WrapChildren:   strip == 0,
						WrapLineGap:    8,
					},
					BackgroundColor: RGBA(30, 30, 36, 255),
				}, func() {
					extChip(c, 0, 118, 28)
					extChip(c, 1, 118, 40)
					extChip(c, 2, 118, 28)
				})
			}
		})
	})
}

func sceneExtWrapRowsGrow(c *Context) RenderCommandArray {
	grow := func(i int, w SizingAxis) {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: w, Height: SizingFixed(30)}}, BackgroundColor: extChipColor(i)}, nil)
	}
	return singleFrame(c, func() {
		Box(c, extStripDecl(400, 8), func() {
			grow(0, SizingGrow(100))
			grow(1, SizingGrow(150))
			grow(2, SizingGrow(120))
			grow(0, SizingGrow(200))
			grow(1, SizingGrow(90))
			grow(2, SizingGrow(80, 100))
		})
	})
}

func sceneExtWrapRowsPercent(c *Context) RenderCommandArray {
	percent := func(p float32) {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingPercent(p), Height: SizingFixed(40)}}, BackgroundColor: extChipColor(0)}, nil)
	}
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:       Sizing{Width: SizingFixed(500), Height: SizingFit()},
				Padding:      PaddingAll(10),
				ChildGap:     10,
				WrapChildren: true,
				WrapLineGap:  10,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			percent(0.5)
			extChip(c, 1, 100, 40)
			extChip(c, 2, 100, 40)
			percent(0.25)
			extChip(c, 1, 200, 40)
		})
	})
}

func sceneExtWrapRowsAligned(c *Context, alignment ChildAlignment) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:         Sizing{Width: SizingFixed(300), Height: SizingFixed(200)},
				Padding:        PaddingAll(8),
				ChildGap:       6,
				ChildAlignment: alignment,
				WrapChildren:   true,
				WrapLineGap:    6,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			extChip(c, 0, 90, 20)
			extChip(c, 1, 120, 30)
			extChip(c, 2, 60, 24)
			extChip(c, 3, 100, 20)
			extChip(c, 4, 80, 40)
			extChip(c, 5, 110, 28)
		})
	})
}

func sceneExtWrapRowsAlignCenter(c *Context) RenderCommandArray {
	return sceneExtWrapRowsAligned(c, ChildAlignment{X: AlignXCenter, Y: AlignYCenter})
}

func sceneExtWrapRowsAlignRightBottom(c *Context) RenderCommandArray {
	return sceneExtWrapRowsAligned(c, ChildAlignment{X: AlignXRight, Y: AlignYBottom})
}

func sceneExtWrapRowsGapBorders(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		decl := extStripDecl(300, 9)
		decl.Border = BorderElementConfig{Color: RGBA(240, 200, 80, 255), Width: BorderWidth{BetweenChildren: 2}}
		Box(c, decl, func() {
			for i := range 7 {
				extChip(c, i, 80, 30)
			}
		})
	})
}

func sceneExtWrapRowsLoneWide(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, extStripDecl(200, 8), func() {
			extChip(c, 0, 60, 30)
			Box(c, Decl{
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, Padding: PaddingAll(4)},
				BackgroundColor: RGBA(80, 80, 120, 255),
			}, func() {
				Text(c, "The quick brown fox jumps over", TextElementConfig{
					TextColor: RGBA(240, 240, 240, 255),
					FontSize:  16,
					WrapMode:  TextWrapWords,
				})
			})
			extChip(c, 2, 300, 30)
		})
	})
}

func sceneExtWrapRowsTextChildren(c *Context) RenderCommandArray {
	labels := [8]string{"Workspaces", "Layout", "Window", "Status", "Alerts", "System", "Clock", "Battery"}
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:         Sizing{Width: SizingFixed(320), Height: SizingFit()},
				Padding:        PaddingAll(8),
				ChildGap:       8,
				ChildAlignment: ChildAlignment{X: AlignXLeft, Y: AlignYCenter},
				WrapChildren:   true,
				WrapLineGap:    8,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			for i, label := range labels {
				Box(c, Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, Padding: PaddingAll(6)},
					BackgroundColor: extChipColor(i),
				}, func() {
					Text(c, label, TextElementConfig{
						TextColor: RGBA(240, 240, 240, 255),
						FontSize:  14,
						WrapMode:  TextWrapWords,
					})
				})
			}
		})
	})
}

func sceneExtWrapRowsFitInGrow(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
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
				Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(900), Height: SizingFixed(200)}},
				BackgroundColor: RGBA(60, 60, 80, 255),
			}, nil)
			Box(c, Decl{
				Layout: LayoutConfig{
					Sizing:       Sizing{Width: SizingFit(), Height: SizingFit()},
					Padding:      PaddingAll(6),
					ChildGap:     6,
					WrapChildren: true,
					WrapLineGap:  6,
				},
				BackgroundColor: RGBA(80, 80, 120, 255),
			}, func() {
				for i := range 8 {
					extChip(c, i, 100, 30)
				}
			})
		})
	})
}

func sceneExtWrapRowsClipScroll(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:       Sizing{Width: SizingFixed(300), Height: SizingFixed(80)},
				Padding:      PaddingAll(8),
				ChildGap:     8,
				WrapChildren: true,
				WrapLineGap:  8,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
			Clip:            ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: Vector2{X: 0, Y: -30}},
		}, func() {
			for i := range 9 {
				extChip(c, i, 80, 30)
			}
		})
	})
}

func sceneExtWrapRowsNested(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, extStripDecl(400, 8), func() {
			extChip(c, 0, 120, 30)
			extChip(c, 1, 120, 30)
			Box(c, Decl{
				Layout: LayoutConfig{
					Sizing:       Sizing{Width: SizingFit(), Height: SizingFit()},
					Padding:      PaddingAll(4),
					ChildGap:     4,
					WrapChildren: true,
					WrapLineGap:  4,
				},
				BackgroundColor: RGBA(80, 80, 120, 255),
			}, func() {
				for i := range 8 {
					extChip(c, i, 50, 20)
				}
			})
			extChip(c, 2, 120, 30)
			extChip(c, 0, 120, 30)
		})
	})
}

// extDeclareWrapExit mirrors oracle/main.c::ext_declare_wrap_exit.
func extDeclareWrapExit(c *Context, withB bool) {
	transition := goldenTransitionConfig()
	chip := func(id string, i int, withTransition bool) {
		decl := Decl{
			Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(80), Height: SizingFixed(30)}},
			BackgroundColor: extChipColor(i),
		}
		if withTransition {
			decl.Transition = transition
		}
		BoxID(c, id, decl, nil)
	}
	c.BeginLayout()
	BoxID(c, "WrapExitStrip", extStripDecl(300, 8), func() {
		chip("WrapExitA", 0, false)
		if withB {
			chip("WrapExitB", 1, true)
		}
		chip("WrapExitC", 2, false)
		chip("WrapExitD", 0, false)
		chip("WrapExitE", 1, false)
	})
}

func sceneExtWrapExitTransition(c *Context) RenderCommandArray {
	extDeclareWrapExit(c, true)
	c.EndLayout(oracleTransitionDelta)
	extDeclareWrapExit(c, false)
	c.EndLayout(oracleTransitionDelta)
	extDeclareWrapExit(c, false)
	return c.EndLayout(oracleTransitionDelta)
}

func sceneExtWrapColsFixedHeight(c *Context) RenderCommandArray {
	sizes := [9][2]float32{{100, 40}, {80, 60}, {120, 50}, {60, 30}, {90, 70}, {110, 40}, {70, 40}, {100, 60}, {80, 20}}
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFit(), Height: SizingFixed(200)},
				Padding:         PaddingAll(8),
				ChildGap:        8,
				LayoutDirection: TopToBottom,
				WrapChildren:    true,
				WrapLineGap:     8,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			for i, s := range sizes {
				extChip(c, i, s[0], s[1])
			}
		})
	})
}

func sceneExtWrapColsGrowHeight(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(400), Height: SizingFixed(160)},
				Padding:         PaddingAll(8),
				ChildGap:        8,
				LayoutDirection: TopToBottom,
				WrapChildren:    true,
				WrapLineGap:     8,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			for i := range 6 {
				if i%2 == 0 {
					Box(c, Decl{
						Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFit()}, Padding: PaddingAll(4)},
						BackgroundColor: extChipColor(i),
					}, func() {
						Text(c, "Quick brown fox", TextElementConfig{
							TextColor: RGBA(240, 240, 240, 255),
							FontSize:  16,
							WrapMode:  TextWrapWords,
						})
					})
				} else {
					Box(c, Decl{
						Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingGrow(20)}},
						BackgroundColor: extChipColor(i),
					}, nil)
				}
			}
		})
	})
}

func sceneExtWrapClipShrink(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, ChildGap: 8, LayoutDirection: TopToBottom}}, func() {
			Box(c, Decl{
				Layout: LayoutConfig{
					Sizing:          Sizing{Width: SizingFixed(300), Height: SizingFixed(120)},
					Padding:         PaddingAll(8),
					ChildGap:        8,
					LayoutDirection: TopToBottom,
				},
				BackgroundColor: RGBA(30, 30, 36, 255),
			}, func() {
				Box(c, Decl{
					Layout: LayoutConfig{
						Sizing:       Sizing{Width: SizingGrow(0), Height: SizingFit()},
						Padding:      PaddingAll(8),
						ChildGap:     8,
						WrapChildren: true,
						WrapLineGap:  8,
					},
					BackgroundColor: RGBA(80, 80, 120, 255),
					Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
				}, func() {
					for i := range 9 {
						extChip(c, i, 80, 30)
					}
				})
				Box(c, Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(100), Height: SizingFixed(30)}},
					BackgroundColor: RGBA(60, 60, 80, 255),
				}, nil)
			})
			Box(c, Decl{
				Layout: LayoutConfig{
					Sizing:          Sizing{Width: SizingFixed(200), Height: SizingFixed(150)},
					Padding:         PaddingAll(8),
					ChildGap:        8,
					LayoutDirection: LeftToRight,
				},
				BackgroundColor: RGBA(30, 30, 36, 255),
			}, func() {
				Box(c, Decl{
					Layout: LayoutConfig{
						Sizing:          Sizing{Width: SizingFit(), Height: SizingGrow(0)},
						Padding:         PaddingAll(8),
						ChildGap:        8,
						LayoutDirection: TopToBottom,
						WrapChildren:    true,
						WrapLineGap:     8,
					},
					BackgroundColor: RGBA(80, 80, 120, 255),
					Clip:            ClipElementConfig{Horizontal: true, Vertical: true},
				}, func() {
					for i := range 8 {
						extChip(c, i, 60, 40)
					}
				})
				Box(c, Decl{
					Layout:          LayoutConfig{Sizing: Sizing{Width: SizingFixed(60), Height: SizingFixed(100)}},
					BackgroundColor: RGBA(60, 60, 80, 255),
				}, nil)
			})
		})
	})
}

func sceneExtWrapRowsShortParent(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:       Sizing{Width: SizingFixed(120), Height: SizingFixed(40)},
				ChildGap:     10,
				WrapChildren: true,
				WrapLineGap:  10,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
			Border:          BorderElementConfig{Color: RGBA(240, 200, 80, 255), Width: BorderWidth{BetweenChildren: 2}},
		}, func() {
			for i := range 6 {
				extChip(c, i, 50, 30)
			}
		})
	})
}

func sceneExtWrapColsClipGrow(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, ChildGap: 8, LayoutDirection: TopToBottom}}, func() {
			for pane := range 2 {
				Box(c, Decl{
					Layout: LayoutConfig{
						Sizing:          Sizing{Width: SizingFixed(400), Height: SizingFixed(100)},
						Padding:         PaddingAll(8),
						ChildGap:        8,
						LayoutDirection: TopToBottom,
						WrapChildren:    true,
						WrapLineGap:     8,
					},
					BackgroundColor: RGBA(30, 30, 36, 255),
					Clip:            ClipElementConfig{Horizontal: pane == 1, Vertical: pane == 1},
				}, func() {
					for i := range 4 {
						Box(c, Decl{
							Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(60), Height: SizingFixed(30)}},
							BackgroundColor: extChipColor(i),
						}, nil)
					}
				})
			}
		})
	})
}

func sceneExtWrapRowsLineGap(c *Context) RenderCommandArray {
	lineGaps := [2]uint16{24, 0}
	return singleFrame(c, func() {
		Box(c, Decl{Layout: LayoutConfig{Sizing: Sizing{Width: SizingFit(), Height: SizingFit()}, ChildGap: 16, LayoutDirection: TopToBottom}}, func() {
			for strip := range 2 {
				Box(c, Decl{
					Layout: LayoutConfig{
						Sizing:       Sizing{Width: SizingFixed(300), Height: SizingFit()},
						Padding:      PaddingAll(8),
						ChildGap:     8,
						WrapChildren: true,
						WrapLineGap:  lineGaps[strip],
					},
					BackgroundColor: RGBA(30, 30, 36, 255),
				}, func() {
					for i := range 7 {
						extChip(c, i, 85, 30)
					}
				})
			}
		})
	})
}

func sceneExtWrapRowsLineGapSlack(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:         Sizing{Width: SizingFixed(300), Height: SizingFixed(220)},
				Padding:        PaddingAll(8),
				ChildGap:       8,
				ChildAlignment: ChildAlignment{X: AlignXCenter, Y: AlignYCenter},
				WrapChildren:   true,
				WrapLineGap:    20,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			extChip(c, 0, 90, 20)
			extChip(c, 1, 120, 30)
			extChip(c, 2, 60, 24)
			extChip(c, 3, 100, 20)
			extChip(c, 4, 80, 40)
			extChip(c, 5, 110, 28)
		})
	})
}

func sceneExtWrapRowsLineGapBorders(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:       Sizing{Width: SizingFixed(300), Height: SizingFit()},
				Padding:      PaddingAll(8),
				ChildGap:     7,
				WrapChildren: true,
				WrapLineGap:  13,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
			Border:          BorderElementConfig{Color: RGBA(240, 200, 80, 255), Width: BorderWidth{BetweenChildren: 2}},
		}, func() {
			for i := range 7 {
				extChip(c, i, 80, 30)
			}
		})
	})
}

func sceneExtWrapColsLineGap(c *Context) RenderCommandArray {
	widths := [7]float32{60, 70, 50, 80, 55, 65, 45}
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingFixed(320), Height: SizingFixed(200)},
				Padding:         PaddingAll(8),
				ChildGap:        6,
				LayoutDirection: TopToBottom,
				WrapChildren:    true,
				WrapLineGap:     20,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
		}, func() {
			for i := range 7 {
				extChip(c, i, widths[i], 50)
			}
		})
	})
}

func sceneExtWrapRowsLineGapScroll(c *Context) RenderCommandArray {
	return singleFrame(c, func() {
		Box(c, Decl{
			Layout: LayoutConfig{
				Sizing:       Sizing{Width: SizingFixed(300), Height: SizingFixed(80)},
				Padding:      PaddingAll(8),
				ChildGap:     8,
				WrapChildren: true,
				WrapLineGap:  22,
			},
			BackgroundColor: RGBA(30, 30, 36, 255),
			Clip:            ClipElementConfig{Horizontal: true, Vertical: true, ChildOffset: Vector2{X: 0, Y: -30}},
		}, func() {
			for i := range 9 {
				extChip(c, i, 80, 30)
			}
		})
	})
}
