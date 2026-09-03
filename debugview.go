package claygo

import "strconv"

// debugview.go ports Clay's debug overlay panel (Clay__RenderDebugView at
// oracle/clay.h ~line 3523): a right-hand side panel with a "Clay Debug Tools"
// header, the live element tree as a one-row-per-element list (name,
// dimensions, Offscreen / Duplicate-ID markers, config chips, and a +/-
// collapse button on non-leaf rows), and a scrollable inspector pane that
// shows the selected element's bbox / layout / sizing / floating / clip /
// border breakdown.
//
// Interactivity: hovering a row highlights the matching scene element with a
// translucent overlay; clicking selects it (Context.debugSelectedElementID,
// persisted across frames); the +/- button toggles a row's children
// (Context.debugCollapsed). Every element the panel declares uses a
// "Clay__Debug_" id prefix so it can't collide with user elements.

// DebugViewWidth is the fixed pixel width of the debug side panel.
// Mirrors Clay__debugViewWidth (oracle/clay.h:3952).
var DebugViewWidth uint32 = 400

// debugViewRowHeight is the per-row height used in the element list and
// header. Mirrors CLAY__DEBUGVIEW_ROW_HEIGHT (oracle/clay.h:3206).
const debugViewRowHeight float32 = 30

// debugViewOuterPadding is the side padding applied to the panel body.
// Mirrors CLAY__DEBUGVIEW_OUTER_PADDING.
const debugViewOuterPadding uint16 = 10

// debugViewIndentWidth controls per-depth indentation of the tree.
// Mirrors CLAY__DEBUGVIEW_INDENT_WIDTH.
const debugViewIndentWidth uint16 = 16

// debugViewInspectorHeight is the fixed height of the detail-inspector
// pane that sits below the element list. Mirrors the 300-pixel value
// upstream uses for the inspector / warnings pane (oracle/clay.h:3610).
const debugViewInspectorHeight float32 = 300

// debugViewPanelZ is the z-index for the debug panel itself. Picked just
// below int16 max so the panel sits above all normal floating UI (which
// typically uses small z values), and the hover-highlight overlay (which
// uses the next-higher value) sits above the panel.
const debugViewPanelZ int16 = 32765

// debugViewHighlightZ is the z-index for the hover-highlight overlay. One
// above debugViewPanelZ so the translucent rectangle painted over the
// hovered scene element appears on top of any other UI.
const debugViewHighlightZ int16 = 32767

// Debug palette. Mirrors the CLAY__DEBUGVIEW_COLOR_* constants.
var (
	debugColor1        = RGBA(58, 56, 52, 255)    // dark row
	debugColor2        = RGBA(62, 60, 58, 255)    // alt row / header bar
	debugColor3        = RGBA(141, 133, 135, 255) // muted text / dividers
	debugColor4        = RGBA(238, 226, 231, 255) // primary text
	debugColorSelected = RGBA(102, 80, 78, 255)   // selected row bg
	debugColorDup      = RGBA(177, 147, 8, 255)   // duplicate-id border

	// debugColorInspectorHeader fills the title strip on each inspector
	// section ("BBox", "Layout", "Sizing", ...).
	debugColorInspectorHeader       = Color{R: 200, G: 200, B: 200, A: 120}
	debugColorInspectorHeaderBorder = Color{R: 200, G: 200, B: 200, A: 255}
)

// DebugViewHighlightColor is the translucent color used to highlight the live
// scene element corresponding to the hovered debug row. Mirrors
// Clay__debugViewHighlightColor (oracle/clay.h:3953).
var DebugViewHighlightColor = RGBA(168, 66, 28, 100)

// debugConfigChip describes one of the colored type labels that appears
// next to an element row (Background, Border, Floating, Clip, ...).
// Mirrors Clay__DebugElementConfigTypeLabelConfig + the
// Clay__DebugGetElementConfigTypeLabel switch (oracle/clay.h:3230).
type debugConfigChip struct {
	label string
	color Color
}

var (
	chipBackground = debugConfigChip{"Background", RGBA(243, 134, 48, 255)}
	chipOverlay    = debugConfigChip{"Overlay", RGBA(142, 129, 206, 255)}
	chipRadius     = debugConfigChip{"Radius", RGBA(239, 148, 157, 255)}
	chipText       = debugConfigChip{"Text", RGBA(105, 210, 231, 255)}
	chipAspect     = debugConfigChip{"Aspect", RGBA(101, 149, 194, 255)}
	chipImage      = debugConfigChip{"Image", RGBA(121, 189, 154, 255)}
	chipFloating   = debugConfigChip{"Floating", RGBA(250, 105, 0, 255)}
	chipClip       = debugConfigChip{"Scroll", RGBA(242, 196, 90, 255)}
	chipBorder     = debugConfigChip{"Border", RGBA(108, 91, 123, 255)}
	chipCustom     = debugConfigChip{"Custom", RGBA(11, 72, 107, 255)}
)

// debugTextConfig is the default text style used for element-name rows
// and inline labels. Mirrors Clay__DebugView_TextNameConfig.
func debugTextConfig() TextElementConfig {
	return TextElementConfig{
		TextColor: debugColor4,
		FontSize:  16,
		WrapMode:  TextWrapNone,
	}
}

// debugTitleConfig is the muted "field label" style (e.g. "Bounding Box").
func debugTitleConfig() TextElementConfig {
	return TextElementConfig{
		TextColor: debugColor3,
		FontSize:  16,
		WrapMode:  TextWrapNone,
	}
}

// debugRowEntry records the mapping from a panel-row index to the scene
// element it describes. We collect these during the DFS that builds the
// element list so that downstream code (hover-highlight, click-select,
// collapse-button hit detection) can answer "what element does row N
// correspond to?" without re-walking the tree.
type debugRowEntry struct {
	// elementID is the hashed id of the scene element this row represents.
	elementID uint32
	// hasChildren is true when the row owns a collapse button (non-text
	// element with at least one child).
	hasChildren bool
}

// renderDebugView is the entry point invoked from EndLayout when
// debugMode is true. It snapshots the count of elements/tree roots from
// the user's UI, then declares a floating-attached side panel that
// participates in the same sizing/positioning pass as the user UI.
//
// The work happens between the user's closeElement (which pops the
// auto-root) and calculateFinalLayout: we must be inside the same
// frame's element-build phase so the floating root we declare gets
// processed by the solver.
func (c *Context) renderDebugView() {
	// Snapshot the user element/root counts so the element list iterates
	// only the user UI, not the debug panel itself. Mirrors the
	// initialRootsLength / initialElementsLength capture at the top of
	// Clay__RenderDebugView (oracle/clay.h:3536).
	userRoots := c.layoutElementTreeRoots.Length

	// Re-prime the open-element stack so the [root, root, ...] sentinel
	// the openElement helpers expect is valid again. EndLayout popped the
	// duplicate-root sentinel and then closed the root, leaving the stack
	// empty. We push the root index back twice so configureOpenElement on
	// a floating root sees a well-formed parent chain.
	c.openLayoutElementStack.Length = 0
	c.openLayoutElementStack.Add(0)
	c.openLayoutElementStack.Add(0)

	// Compute which list row (if any) the pointer is currently over. The
	// upstream version derives this geometrically from the pointer Y
	// (oracle/clay.h:3554) — same trick here. The "-1" offset accounts
	// for the header row above the list.
	debugViewWidth := float32(DebugViewWidth)
	pointerInPanel := c.pointerPosition.X >= c.layoutDimensions.Width-debugViewWidth &&
		c.pointerPosition.X <= c.layoutDimensions.Width &&
		c.pointerPosition.Y >= debugViewRowHeight &&
		c.pointerPosition.Y < c.layoutDimensions.Height-debugViewInspectorHeight
	highlightedRow := int32(-1)
	if pointerInPanel {
		// Header occupies row 0; list rows begin at y = rowHeight (after
		// header) inside the panel. So pointerY / rowHeight - 1 is the
		// row index within the list (matches C 3554-3556).
		highlightedRow = int32((c.pointerPosition.Y)/debugViewRowHeight) - 1
	}

	// rowEntries is built during DFS and consumed afterward to (a) drive
	// click-to-select and (b) figure out which scene element corresponds
	// to the hovered row for the floating highlight rectangle.
	var rowEntries []debugRowEntry
	highlightedElementID := uint32(0)

	BoxID(c, "Clay__Debug_Panel", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingFixed(debugViewWidth),
				Height: SizingFixed(c.layoutDimensions.Height),
			},
			LayoutDirection: TopToBottom,
		},
		BackgroundColor: debugColor1,
		Floating: FloatingElementConfig{
			ZIndex: debugViewPanelZ,
			AttachPoints: FloatingAttachPoints{
				Element: AttachPointLeftTop,
				Parent:  AttachPointRightTop,
			},
			AttachTo:           AttachToRoot,
			PointerCaptureMode: PointerCaptureModePassthrough,
		},
		Border: BorderElementConfig{
			Color: debugColor3,
			Width: BorderWidth{Left: 1},
		},
	}, func() {
		// Header bar.
		c.debugHeader()
		// One-pixel divider.
		c.debugDivider()
		// Element tree list (scrollable). The clip container handles
		// overflow so trees taller than the panel can be scrolled with
		// the wheel / scroll API.
		rowEntries, highlightedElementID = c.debugElementList(userRoots, highlightedRow)
		// Second divider between list and inspector.
		c.debugDivider()
		// Detail inspector pane (below the list).
		c.debugInspectorPane()
	})

	// Apply click-to-select using the row-to-element mapping. We can do
	// this AFTER the panel was declared because the click affects state
	// read on the NEXT frame: this frame just paints with whatever
	// debugSelectedElementID currently holds, and a click switches it
	// for next frame.
	if c.pointerData.State == PointerDataPressedThisFrame && highlightedRow >= 0 &&
		int(highlightedRow) < len(rowEntries) {
		c.debugSelectedElementID = rowEntries[highlightedRow].elementID
	}

	// Collapse-button toggle: if the pointer is currently over any
	// element whose id starts with "Clay__Debug_Collapse_<elementID>",
	// and we just pressed, flip the collapsed bit. We iterate the
	// pointerOverIds (built last frame) and match against the well-known
	// per-row collapse-button ids stored as offsets. Mirrors C 3425-3434.
	if c.pointerData.State == PointerDataPressedThisFrame {
		collapseBase := HashString(String{Text: "Clay__Debug_Collapse"}, 0).BaseID
		for i := c.pointerOverIds.Length - 1; i >= 0; i-- {
			id := c.pointerOverIds.GetValue(i)
			if id.BaseID == collapseBase {
				if c.debugCollapsed == nil {
					c.debugCollapsed = map[uint32]bool{}
				}
				c.debugCollapsed[id.Offset] = !c.debugCollapsed[id.Offset]
				break
			}
		}
	}

	// Hover-to-highlight: paint a translucent rectangle on the actual
	// scene element so the user can see which element the hovered row
	// corresponds to. Floating-attaches to the scene element by id;
	// pointer-capture is passthrough so the highlight doesn't intercept
	// further pointer events. Mirrors C 3437-3440.
	if highlightedElementID != 0 {
		BoxID(c, "Clay__Debug_Highlight", Decl{
			Layout: LayoutConfig{
				Sizing: Sizing{Width: SizingGrow(0), Height: SizingGrow(0)},
			},
			BackgroundColor: DebugViewHighlightColor,
			Floating: FloatingElementConfig{
				ZIndex:             debugViewHighlightZ,
				ParentID:           highlightedElementID,
				AttachTo:           AttachToElementWithID,
				PointerCaptureMode: PointerCaptureModePassthrough,
				AttachPoints: FloatingAttachPoints{
					Element: AttachPointLeftTop,
					Parent:  AttachPointLeftTop,
				},
			},
		}, nil)
	}

	// closeElement was called by Box; the open stack is now back to the
	// [root, root] sentinel. Clear it so the layout machinery is in the
	// same state EndLayout expected (empty stack after its own
	// closeElement call).
	c.openLayoutElementStack.Length = 0
}

// debugHeader emits the "Clay Debug Tools" title bar at the top of the
// panel. Mirrors the first auto-id branch inside Clay__RenderDebugView
// (oracle/clay.h ~line 3566).
func (c *Context) debugHeader() {
	BoxID(c, "Clay__Debug_Header", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingGrow(0),
				Height: SizingFixed(debugViewRowHeight),
			},
			Padding:        Padding{Left: debugViewOuterPadding, Right: debugViewOuterPadding},
			ChildAlignment: ChildAlignment{Y: AlignYCenter},
		},
		BackgroundColor: debugColor2,
	}, func() {
		Text(c, "Clay Debug Tools", debugTextConfig())
	})
}

// debugDivider emits a 1px-tall horizontal line in the muted color. The
// C version uses this between the header and the scroll pane, and also
// between the element list and the configuration panel.
func (c *Context) debugDivider() {
	BoxIDOffset(c, "Clay__Debug_Divider", c.dynamicElementIndex, Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingGrow(0),
				Height: SizingFixed(1),
			},
		},
		BackgroundColor: debugColor3,
	}, nil)
	c.dynamicElementIndex++
}

// debugElementList walks the user-declared tree roots in DFS order and
// emits one row per element. Mirrors Clay__RenderDebugLayoutElementsList
// (oracle/clay.h:3261).
//
// Returns the row-to-element mapping that was built during the walk
// (so the caller can resolve highlightedRow -> element id) and the
// element id corresponding to highlightedRow, or 0 if the highlight is
// out of range.
func (c *Context) debugElementList(userRoots int32, highlightedRow int32) ([]debugRowEntry, uint32) {
	var rowEntries []debugRowEntry
	highlightedElementID := uint32(0)

	BoxID(c, "Clay__Debug_ListClip", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width: SizingGrow(0),
				// The list region grows to fill whatever space remains
				// after the header and the inspector pane. The clip
				// makes it scrollable.
				Height: SizingGrow(0),
			},
		},
		// Clip the list so a deep tree doesn't bleed into the inspector
		// below. Vertical clip + childOffset lets scroll handling work
		// without further bookkeeping here (scroll position is updated
		// out-of-band by UpdateScrollContainers).
		Clip: ClipElementConfig{
			Vertical:    true,
			ChildOffset: c.GetScrollContainerData(GetElementID("Clay__Debug_ListClip")).childOffsetOrZero(),
		},
	}, func() {
		BoxID(c, "Clay__Debug_ListOuter", Decl{
			Layout: LayoutConfig{
				Sizing: Sizing{
					Width:  SizingGrow(0),
					Height: SizingFit(),
				},
				Padding: Padding{
					Left:  debugViewOuterPadding,
					Right: debugViewOuterPadding,
					Top:   debugViewOuterPadding / 2,
				},
				LayoutDirection: TopToBottom,
			},
		}, func() {
			for rootIdx := range userRoots {
				root := c.layoutElementTreeRoots.Get(rootIdx)
				if root == nil {
					continue
				}
				c.debugElementRow(root.LayoutElementIndex, 0, &rowEntries)
			}
		})
	})

	// Resolve the hovered scene element using the just-built mapping.
	if highlightedRow >= 0 && int(highlightedRow) < len(rowEntries) {
		highlightedElementID = rowEntries[highlightedRow].elementID
	}
	return rowEntries, highlightedElementID
}

// debugElementRow emits one row for the element at layoutElements[idx],
// then recurses into its children at the next indent level (unless the
// row is currently collapsed). Mirrors the per-element body of
// Clay__RenderDebugLayoutElementsList's DFS loop (oracle/clay.h:3280-3422).
//
// rowEntries records the element-id <-> row-index mapping so the caller
// can resolve hover/click hits.
func (c *Context) debugElementRow(idx int32, depth int32, rowEntries *[]debugRowEntry) {
	if idx < 0 || idx >= c.layoutElements.Length {
		return
	}
	el := c.layoutElements.Get(idx)
	if el == nil {
		return
	}

	thisRow := int32(len(*rowEntries))
	hasChildren := !el.IsTextElement && el.Children.Length > 0
	*rowEntries = append(*rowEntries, debugRowEntry{elementID: el.ID, hasChildren: hasChildren})

	// Alternating background rows mirror the C list's striping behaviour.
	// A selected row gets the dedicated selected color regardless of
	// parity (mirrors C 3593-3596).
	var rowBg Color
	if thisRow&1 == 0 {
		rowBg = debugColor2
	} else {
		rowBg = debugColor1
	}
	if c.debugSelectedElementID == el.ID && c.debugSelectedElementID != 0 {
		rowBg = debugColorSelected
	}

	// indent: left-padding scaled by depth so children appear nested
	// under their parent.
	leftPad := debugViewOuterPadding + uint16(depth)*debugViewIndentWidth

	// Look up the hashmap entry once — used for duplicate-id marker,
	// offscreen detection, and to fetch the displayed string id.
	item := c.getHashMapItem(el.ID)

	// offscreen is computed from the most recent bbox recorded on the hashmap
	// item. Before the first final-layout pass, bboxes may still be zero-value,
	// so only show the marker after a non-zero bbox has been recorded.
	offscreen := false
	if item != nil && (item.BoundingBox.Width > 0 || item.BoundingBox.Height > 0) {
		offscreen = c.elementIsOffscreen(item.BoundingBox)
	}

	BoxIDOffset(c, "Clay__Debug_Row", el.ID, Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingGrow(0),
				Height: SizingFixed(debugViewRowHeight),
			},
			Padding:        Padding{Left: leftPad, Right: debugViewOuterPadding},
			ChildGap:       6,
			ChildAlignment: ChildAlignment{Y: AlignYCenter},
		},
		BackgroundColor: rowBg,
	}, func() {
		// Collapse +/- button for non-leaf rows, or a dim square dot for
		// leaves. Both occupy the same 16x16 slot so siblings line up.
		if hasChildren {
			collapsed := c.debugCollapsed != nil && c.debugCollapsed[el.ID]
			label := "-"
			if collapsed {
				label = "+"
			}
			BoxIDOffset(c, "Clay__Debug_Collapse", el.ID, Decl{
				Layout: LayoutConfig{
					Sizing: Sizing{
						Width:  SizingFixed(16),
						Height: SizingFixed(16),
					},
					ChildAlignment: ChildAlignment{X: AlignXCenter, Y: AlignYCenter},
				},
				CornerRadius: UniformCornerRadius(4),
				Border: BorderElementConfig{
					Color: debugColor3,
					Width: BorderWidth{Left: 1, Right: 1, Top: 1, Bottom: 1},
				},
			}, func() {
				Text(c, label, debugTextConfig())
			})
		} else {
			BoxIDOffset(c, "Clay__Debug_LeafDot", el.ID, Decl{
				Layout: LayoutConfig{
					Sizing: Sizing{
						Width:  SizingFixed(16),
						Height: SizingFixed(16),
					},
					ChildAlignment: ChildAlignment{X: AlignXCenter, Y: AlignYCenter},
				},
			}, func() {
				BoxIDOffset(c, "Clay__Debug_LeafDotInner", el.ID, Decl{
					Layout: LayoutConfig{
						Sizing: Sizing{
							Width:  SizingFixed(8),
							Height: SizingFixed(8),
						},
					},
					BackgroundColor: debugColor3,
					CornerRadius:    UniformCornerRadius(2),
				}, nil)
			})
		}

		// Duplicate-ID marker (C 3328-3331): a thin yellow-bordered
		// "Duplicate ID" pill, only emitted when the hashmap recorded a
		// collision on this element this frame.
		if item != nil && item.DebugData.Collision {
			BoxIDOffset(c, "Clay__Debug_Dup", el.ID, Decl{
				Layout: LayoutConfig{
					Padding: Padding{Left: 8, Right: 8, Top: 2, Bottom: 2},
				},
				Border: BorderElementConfig{
					Color: debugColorDup,
					Width: BorderWidth{Left: 1, Right: 1, Top: 1, Bottom: 1},
				},
			}, func() {
				Text(c, "Duplicate ID", debugTitleConfig())
			})
		}
		// Offscreen marker (C 3333-3337).
		if offscreen {
			BoxIDOffset(c, "Clay__Debug_Off", el.ID, Decl{
				Layout: LayoutConfig{
					Padding: Padding{Left: 8, Right: 8, Top: 2, Bottom: 2},
				},
				Border: BorderElementConfig{
					Color: debugColor3,
					Width: BorderWidth{Left: 1, Right: 1, Top: 1, Bottom: 1},
				},
			}, func() {
				Text(c, "Offscreen", debugTitleConfig())
			})
		}

		// Name: the element's string id if it has one, else "(auto)".
		name := "(auto)"
		dims := el.Dimensions
		if item != nil && item.ElementID.StringID.Text != "" {
			name = item.ElementID.StringID.Text
			if item.ElementID.Offset != 0 {
				name = name + " (" + strconv.FormatUint(uint64(item.ElementID.Offset), 10) + ")"
			}
		}
		// Use the muted (offscreen) text style if applicable.
		nameCfg := debugTextConfig()
		if offscreen {
			nameCfg.TextColor = debugColor3
		}
		Text(c, name, nameCfg)

		// Dimensions readout: "WxH" in muted text.
		dimText := strconv.Itoa(int(dims.Width)) + "x" + strconv.Itoa(int(dims.Height))
		Text(c, dimText, debugTitleConfig())

		// Type chips for the configs this element opted into.
		if el.IsTextElement {
			c.debugChip(chipText, el.ID, 0)
		} else {
			chipCount := int32(0)
			emit := func(chip debugConfigChip) {
				c.debugChip(chip, el.ID, chipCount)
				chipCount++
			}
			cfg := &el.Config
			if cfg.BackgroundColor.A > 0 {
				emit(chipBackground)
			}
			if cfg.OverlayColor.A > 0 {
				emit(chipOverlay)
			}
			if cfg.CornerRadius != (CornerRadius{}) {
				emit(chipRadius)
			}
			if cfg.AspectRatio.AspectRatio != 0 {
				emit(chipAspect)
			}
			if cfg.Image.ImageData != nil {
				emit(chipImage)
			}
			if cfg.Floating.AttachTo != AttachToNone {
				emit(chipFloating)
			}
			if cfg.Clip.Horizontal || cfg.Clip.Vertical {
				emit(chipClip)
			}
			if borderHasAnyWidth(cfg.Border) {
				emit(chipBorder)
			}
			if cfg.Custom.CustomData != nil {
				emit(chipCustom)
			}
		}
	})

	// Recurse into children unless we're at a text leaf or the row is
	// currently collapsed.
	collapsed := c.debugCollapsed != nil && c.debugCollapsed[el.ID]
	if !el.IsTextElement && !collapsed {
		for i := range el.Children.Length {
			childIdx := el.Children.Data[i]
			c.debugElementRow(childIdx, depth+1, rowEntries)
		}
	}
}

// debugChip emits one of the colored type-label chips next to an element
// row. Mirrors Clay__RenderElementConfigTypeLabel (oracle/clay.h:3247).
//
// elementID and slot together produce a unique id for the chip so
// siblings don't hash-collide. We fold them into a 32-bit offset by
// mixing.
func (c *Context) debugChip(chip debugConfigChip, elementID uint32, slot int32) {
	bg := chip.color
	bg.A = 90
	BoxIDOffset(c, "Clay__Debug_Chip_"+chip.label, elementID^uint32(slot), Decl{
		Layout: LayoutConfig{
			Padding: Padding{Left: 8, Right: 8, Top: 2, Bottom: 2},
		},
		BackgroundColor: bg,
		CornerRadius:    UniformCornerRadius(4),
		Border: BorderElementConfig{
			Color: chip.color,
			Width: BorderWidth{Left: 1, Right: 1, Top: 1, Bottom: 1},
		},
	}, func() {
		Text(c, chip.label, TextElementConfig{
			TextColor: debugColor4,
			FontSize:  16,
			WrapMode:  TextWrapNone,
		})
	})
}

// debugInspectorPane is the bottom section of the debug panel: a fixed-
// height scrollable region that, when an element is selected, shows the
// element's bbox / layout / sizing / padding / floating / clip / border
// breakdowns. When no element is selected the pane shows queued warnings.
// Mirrors the lower half of Clay__RenderDebugView (oracle/clay.h:3607-3945).
func (c *Context) debugInspectorPane() {
	BoxID(c, "Clay__Debug_Inspector", Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingGrow(0),
				Height: SizingFixed(debugViewInspectorHeight),
			},
			LayoutDirection: TopToBottom,
		},
		BackgroundColor: debugColor2,
		Clip: ClipElementConfig{
			Vertical:    true,
			ChildOffset: c.GetScrollContainerData(GetElementID("Clay__Debug_Inspector")).childOffsetOrZero(),
		},
	}, func() {
		// Inspector header strip ("Element Configuration" + selected id).
		var selected *LayoutElementHashMapItem
		if c.debugSelectedElementID != 0 {
			selected = c.getHashMapItem(c.debugSelectedElementID)
		}
		BoxID(c, "Clay__Debug_InspectorHeader", Decl{
			Layout: LayoutConfig{
				Sizing: Sizing{
					Width:  SizingGrow(0),
					Height: SizingFixed(debugViewRowHeight + 8),
				},
				Padding:        Padding{Left: debugViewOuterPadding, Right: debugViewOuterPadding},
				ChildGap:       8,
				ChildAlignment: ChildAlignment{Y: AlignYCenter},
			},
		}, func() {
			Text(c, "Element Configuration", debugTextConfig())
			if selected != nil && selected.ElementID.StringID.Text != "" {
				Text(c, selected.ElementID.StringID.Text, debugTitleConfig())
				if selected.ElementID.Offset != 0 {
					Text(c, "("+strconv.FormatUint(uint64(selected.ElementID.Offset), 10)+")", debugTitleConfig())
				}
			}
		})

		if selected == nil || selected.LayoutElement == nil {
			c.debugWarningsBody()
			return
		}
		c.debugInspectorBody(selected)
	})
}

func (c *Context) debugWarningsBody() {
	warningConfig := TextElementConfig{TextColor: debugColor4, FontSize: 16, WrapMode: TextWrapNone}
	BoxID(c, "Clay__DebugViewWarningItemHeader", Decl{
		Layout: LayoutConfig{
			Sizing:         Sizing{Height: SizingFixed(debugViewRowHeight)},
			Padding:        Padding{Left: debugViewOuterPadding, Right: debugViewOuterPadding},
			ChildGap:       8,
			ChildAlignment: ChildAlignment{Y: AlignYCenter},
		},
	}, func() {
		Text(c, "Warnings", warningConfig)
	})
	BoxID(c, "Clay__DebugViewWarningsTopBorder", Decl{
		Layout:          LayoutConfig{Sizing: Sizing{Width: SizingGrow(0), Height: SizingFixed(1)}},
		BackgroundColor: RGBA(200, 200, 200, 255),
	}, nil)
	for i, warning := range c.warnings {
		BoxIDOffset(c, "Clay__DebugViewWarningItem", uint32(i), Decl{
			Layout: LayoutConfig{
				Sizing:         Sizing{Height: SizingFixed(debugViewRowHeight)},
				Padding:        Padding{Left: debugViewOuterPadding, Right: debugViewOuterPadding},
				ChildGap:       8,
				ChildAlignment: ChildAlignment{Y: AlignYCenter},
			},
		}, func() {
			Text(c, warning.Text, warningConfig)
		})
	}
}

// debugInspectorBody emits the actual breakdown for the selected
// element: bounding box, layout, padding, floating/clip/border configs,
// text config when applicable. Mirrors C 3627-3927.
func (c *Context) debugInspectorBody(item *LayoutElementHashMapItem) {
	le := item.LayoutElement
	pad := Padding{
		Left:   debugViewOuterPadding,
		Right:  debugViewOuterPadding,
		Top:    8,
		Bottom: 8,
	}
	// Layout section.
	BoxIDOffset(c, "Clay__Debug_InspLayout", item.ElementID.ID, Decl{
		Layout: LayoutConfig{
			Sizing:          Sizing{Width: SizingGrow(0)},
			Padding:         pad,
			ChildGap:        8,
			LayoutDirection: TopToBottom,
		},
	}, func() {
		c.inspectorSectionHeader("Layout", item.ElementID.ID)
		// Bounding box.
		Text(c, "Bounding Box", debugTitleConfig())
		Text(c, "{ x: "+ftoi(item.BoundingBox.X)+", y: "+ftoi(item.BoundingBox.Y)+
			", width: "+ftoi(item.BoundingBox.Width)+", height: "+ftoi(item.BoundingBox.Height)+" }",
			debugTextConfig())
		if !le.IsTextElement {
			lc := &le.Config.Layout
			Text(c, "Layout Direction", debugTitleConfig())
			if lc.LayoutDirection == TopToBottom {
				Text(c, "TOP_TO_BOTTOM", debugTextConfig())
			} else {
				Text(c, "LEFT_TO_RIGHT", debugTextConfig())
			}
			Text(c, "Sizing", debugTitleConfig())
			Text(c, "width: "+sizingString(lc.Sizing.Width), debugTextConfig())
			Text(c, "height: "+sizingString(lc.Sizing.Height), debugTextConfig())
			Text(c, "Padding", debugTitleConfig())
			Text(c, "{ left: "+strconv.Itoa(int(lc.Padding.Left))+
				", right: "+strconv.Itoa(int(lc.Padding.Right))+
				", top: "+strconv.Itoa(int(lc.Padding.Top))+
				", bottom: "+strconv.Itoa(int(lc.Padding.Bottom))+" }", debugTextConfig())
			Text(c, "Child Gap", debugTitleConfig())
			Text(c, strconv.Itoa(int(lc.ChildGap)), debugTextConfig())
			Text(c, "Wrap Children", debugTitleConfig())
			Text(c, boolStr(lc.WrapChildren), debugTextConfig())
		}
	})
	// Floating section.
	if !le.IsTextElement && le.Config.Floating.AttachTo != AttachToNone {
		BoxIDOffset(c, "Clay__Debug_InspFloating", item.ElementID.ID, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingGrow(0)},
				Padding:         pad,
				ChildGap:        8,
				LayoutDirection: TopToBottom,
			},
		}, func() {
			c.inspectorSectionHeader("Floating", item.ElementID.ID)
			f := &le.Config.Floating
			Text(c, "Offset", debugTitleConfig())
			Text(c, "{ x: "+ftoi(f.Offset.X)+", y: "+ftoi(f.Offset.Y)+" }", debugTextConfig())
			Text(c, "Z-Index", debugTitleConfig())
			Text(c, strconv.Itoa(int(f.ZIndex)), debugTextConfig())
		})
	}
	// Clip section.
	if !le.IsTextElement && (le.Config.Clip.Horizontal || le.Config.Clip.Vertical) {
		BoxIDOffset(c, "Clay__Debug_InspClip", item.ElementID.ID, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingGrow(0)},
				Padding:         pad,
				ChildGap:        8,
				LayoutDirection: TopToBottom,
			},
		}, func() {
			c.inspectorSectionHeader("Clip", item.ElementID.ID)
			Text(c, "Vertical", debugTitleConfig())
			Text(c, boolStr(le.Config.Clip.Vertical), debugTextConfig())
			Text(c, "Horizontal", debugTitleConfig())
			Text(c, boolStr(le.Config.Clip.Horizontal), debugTextConfig())
		})
	}
	// Border section.
	if !le.IsTextElement && borderHasAnyWidth(le.Config.Border) {
		BoxIDOffset(c, "Clay__Debug_InspBorder", item.ElementID.ID, Decl{
			Layout: LayoutConfig{
				Sizing:          Sizing{Width: SizingGrow(0)},
				Padding:         pad,
				ChildGap:        8,
				LayoutDirection: TopToBottom,
			},
		}, func() {
			c.inspectorSectionHeader("Border", item.ElementID.ID)
			w := le.Config.Border.Width
			Text(c, "Border Widths", debugTitleConfig())
			Text(c, "{ left: "+strconv.Itoa(int(w.Left))+
				", right: "+strconv.Itoa(int(w.Right))+
				", top: "+strconv.Itoa(int(w.Top))+
				", bottom: "+strconv.Itoa(int(w.Bottom))+" }", debugTextConfig())
		})
	}
}

// inspectorSectionHeader emits a colored pill-style heading (e.g.
// "Layout", "Floating") at the top of an inspector subsection. Mirrors
// Clay__DebugViewRenderElementConfigHeader (oracle/clay.h:3476-3484).
func (c *Context) inspectorSectionHeader(label string, elementID uint32) {
	BoxIDOffset(c, "Clay__Debug_InspHeader_"+label, elementID, Decl{
		Layout: LayoutConfig{
			Padding: Padding{Left: 8, Right: 8, Top: 2, Bottom: 2},
		},
		BackgroundColor: debugColorInspectorHeader,
		CornerRadius:    UniformCornerRadius(4),
		Border: BorderElementConfig{
			Color: debugColorInspectorHeaderBorder,
			Width: BorderWidth{Left: 1, Right: 1, Top: 1, Bottom: 1},
		},
	}, func() {
		Text(c, label, debugTextConfig())
	})
}

// sizingString stringifies a SizingAxis for the inspector pane. Mirrors
// Clay__RenderDebugLayoutSizing (oracle/clay.h:3445-3473).
func sizingString(s SizingAxis) string {
	switch s.Type {
	case SizingTypeFixed:
		return "FIXED(" + ftoi(s.MinMax.Min) + ")"
	case SizingTypeGrow:
		return "GROW(" + ftoi(s.MinMax.Min) + ", " + ftoi(s.MinMax.Max) + ")"
	case SizingTypeFit:
		return "FIT(" + ftoi(s.MinMax.Min) + ", " + ftoi(s.MinMax.Max) + ")"
	case SizingTypePercent:
		return "PERCENT(" + ftoi(s.Percent*100) + "%)"
	}
	return "?"
}

// ftoi formats a float as an int (no fractional part). The debug view
// only ever wants the integer pixel value.
func ftoi(f float32) string {
	return strconv.Itoa(int(f))
}

// boolStr formats a bool as "true" / "false".
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// childOffsetOrZero returns the recorded scroll offset for a clip
// container, or the zero vector if no scroll data has been recorded
// yet. The inspector and the list both use this to feed
// ClipElementConfig.ChildOffset on first declaration without crashing
// when there's no scroll state yet.
func (d ScrollContainerData) childOffsetOrZero() Vector2 {
	if !d.Found || d.ScrollPosition == nil {
		return Vector2{}
	}
	return *d.ScrollPosition
}
