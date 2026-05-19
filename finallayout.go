package claygo

// finallayout.go ports the second half of Clay__CalculateFinalLayout
// (oracle/clay.h ~line 2573): after the sizing solver has decided how big
// each element is, this pass walks the tree depth-first and (a) decides
// each element's bounding-box position based on its parent's padding,
// child gap and child alignment, then (b) emits the per-element render
// commands in z-order.
//
// What is intentionally skipped (deferred to later port waves):
//   - Text wrapping: scenes covered by the current corpus never overflow
//     their parents, so wrappedLines collapse to a single line containing
//     the full text. The wrap pass becomes necessary once we add a scene
//     that needs it.
//   - Aspect-ratio scaling.
//   - Floating containers (no layoutElementTreeRoots yet).
//   - Clip / scroll containers (no openClipElementStack data).
//   - Borders, overlay colors, images, custom elements: the render-command
//     emission below only handles RECTANGLE and TEXT, the two types our
//     committed goldens exercise.
//   - DFS upward-pass: would emit borders / overlay-end / scissor-end.
//     With none of those features active, the upward visit is a no-op so
//     we skip the second visited-marker check entirely.
//   - Tree-root z-index sort (single root).
//   - Transition state tracking.

// layoutTreeNode is one frame of the DFS stack used during final positioning.
// Mirrors Clay__LayoutElementTreeNode (oracle/clay.h ~line 1232).
type layoutTreeNode struct {
	element         *LayoutElement
	position        Vector2
	nextChildOffset Vector2
}

// borderHasAnyWidth mirrors Clay__BorderHasAnyWidth (oracle/clay.h ~line 1420):
// returns true if any of the five Width fields (left/right/top/bottom OR
// betweenChildren) is greater than zero.
func borderHasAnyWidth(b BorderElementConfig) bool {
	w := b.Width
	return w.Left > 0 || w.Right > 0 || w.Top > 0 || w.Bottom > 0 || w.BetweenChildren > 0
}

// elementIsOffscreen reports whether a bounding box is fully outside the
// current viewport. Used to skip render-command emission for elements that
// won't appear on screen. Mirrors Clay__ElementIsOffscreen (oracle/clay.h
// ~line 2561). When Context.cullingEnabled is false, the check is
// short-circuited to always return false (no culling).
func (c *Context) elementIsOffscreen(b BoundingBox) bool {
	if !c.cullingEnabled {
		return false
	}
	// Matches C exactly (oracle/clay.h:2567-2570): strict `<` / `>` so that
	// an element sitting exactly at the viewport edge (x == viewport.W, or
	// x+w == 0) is treated as on-screen, not culled.
	return b.X > c.layoutDimensions.Width ||
		b.Y > c.layoutDimensions.Height ||
		b.X+b.Width < 0 ||
		b.Y+b.Height < 0
}

// calculateFinalLayout sizes (x then y), then positions every element in the
// tree, emits render commands into c.renderCommands, and returns the result.
// Mirrors Clay__CalculateFinalLayout (oracle/clay.h ~line 2573).
func (c *Context) calculateFinalLayout(deltaTime float32) RenderCommandArray {
	c.sizeContainersAlongAxis(true)
	c.wrappedTextLines.Length = 0
	c.wrapTextElements()
	c.propagateTextHeights()
	c.sizeContainersAlongAxis(false)
	// TODO (aspect wave): scale widths off the now-final heights.

	// Sort tree roots by zIndex ascending. Mirrors C's bubble sort at
	// oracle/clay.h ~line 2702. Tiny list (handful of entries in practice)
	// so the O(n²) cost is irrelevant; we keep the stable in-place behavior
	// of the C version so equal-zIndex roots stay in declaration order.
	for sortMax := c.layoutElementTreeRoots.Length - 1; sortMax > 0; sortMax-- {
		for i := int32(0); i < sortMax; i++ {
			a := c.layoutElementTreeRoots.Get(i)
			b := c.layoutElementTreeRoots.Get(i + 1)
			if b.ZIndex < a.ZIndex {
				ta := *a
				tb := *b
				c.layoutElementTreeRoots.Set(i, tb)
				c.layoutElementTreeRoots.Set(i+1, ta)
			}
		}
	}

	c.renderCommands.Length = 0

	// One DFS per tree root. Roots emit into the same renderCommands array,
	// so the final list is naturally in z-order.
	for treeIdx := int32(0); treeIdx < c.layoutElementTreeRoots.Length; treeIdx++ {
		c.emitTreeRoot(c.layoutElementTreeRoots.Get(treeIdx))
	}

	// Construct the public-facing array view. We expose only the live
	// prefix of the arena-backed array so callers don't see stale slots
	// from prior frames.
	commands := c.renderCommands.Data[:c.renderCommands.Length]
	return RenderCommandArray{Commands: commands}
}

// emitTreeRoot runs the position+emission DFS for one tree root. For the
// auto-root the starting position is (0,0); for a floating root it's
// derived from the floating attach-point config against the parent's
// already-computed bounding box.
func (c *Context) emitTreeRoot(treeRoot *layoutElementTreeRoot) {
	root := c.layoutElements.Get(treeRoot.LayoutElementIndex)

	// Compute the root's starting position. Auto-root sits at (0,0);
	// floating roots project onto their parent via attach points.
	rootPosition := Vector2{}
	if root.Config.Floating.AttachTo != AttachToNone {
		parentItem := c.getHashMapItem(treeRoot.ParentID)
		if parentItem != nil {
			rootPosition = computeFloatingPosition(
				parentItem.BoundingBox, root.Dimensions,
				root.Config.Floating.AttachPoints, root.Config.Floating.Offset)
		}
	}

	// Floating roots nested inside a clip ancestor emit a SCISSOR_START
	// scoped to the clip's bbox so their content is masked by the same
	// scroll viewport as the anchor. Matches C 2779-2802.
	emitClipBound := false
	var clipScissorBBox BoundingBox
	if treeRoot.ClipElementID != 0 {
		if clipItem := c.getHashMapItem(treeRoot.ClipElementID); clipItem != nil &&
			!c.elementIsOffscreen(clipItem.BoundingBox) {
			clipScissorBBox = clipItem.BoundingBox
			emitClipBound = true
			c.renderCommands.Add(RenderCommand{
				BoundingBox: clipScissorBBox,
				UserData:    root.Config.UserData,
				ID:          HashNumber(root.ID, uint32(root.Children.Length)+10).ID,
				ZIndex:      treeRoot.ZIndex,
				CommandType: RenderCommandTypeScissorStart,
			})
		}
	}

	dfs := []layoutTreeNode{{
		element:  root,
		position: rootPosition,
		nextChildOffset: Vector2{
			X: float32(root.Config.Layout.Padding.Left),
			Y: float32(root.Config.Layout.Padding.Top),
		},
	}}
	visited := []bool{false}

	rootChildCount := root.Children.Length
	// Every command emitted under this tree root carries the root's
	// ZIndex (matching C, which writes .zIndex = root->zIndex on every
	// RenderCommand). Floating roots can have non-zero ZIndex; the
	// auto-root uses zero.
	treeZ := treeRoot.ZIndex

	for len(dfs) > 0 {
		idx := len(dfs) - 1
		node := &dfs[idx]
		cur := node.element

		if visited[idx] {
			// Upward DFS visit. Order matches Clay__CalculateFinalLayout
			// (oracle/clay.h ~line 2837-2899): BORDER (with between-children
			// dividers) → OVERLAY_COLOR_END → SCISSOR_END.
			if !cur.IsTextElement {
				upBBox := BoundingBox{X: node.position.X, Y: node.position.Y, Width: cur.Dimensions.Width, Height: cur.Dimensions.Height}
				layoutCfg := &cur.Config.Layout

				if borderHasAnyWidth(cur.Config.Border) {
					c.renderCommands.Add(RenderCommand{
						BoundingBox: upBBox,
						RenderData: RenderData{
							Border: BorderRenderData{
								Color:        cur.Config.Border.Color,
								CornerRadius: cur.Config.CornerRadius,
								Width:        cur.Config.Border.Width,
							},
						},
						UserData:    cur.Config.UserData,
						ID:          HashNumber(uint32(cur.Children.Length), cur.ID).ID,
						ZIndex:      treeZ,

						CommandType: RenderCommandTypeBorder,
					})

					// Between-children divider rectangles. Iterates the layout
					// axis, advancing a cursor by (child.dim + childGap) and
					// emitting a divider at each gap.
					if cur.Config.Border.Width.BetweenChildren > 0 && cur.Config.Border.Color.A > 0 {
						halfGap := float32(layoutCfg.ChildGap) / 2
						halfW := float32(cur.Config.Border.Width.BetweenChildren) / 2
						borderOffset := Vector2{
							X: float32(layoutCfg.Padding.Left) - halfGap,
							Y: float32(layoutCfg.Padding.Top) - halfGap,
						}
						if layoutCfg.LayoutDirection == LeftToRight {
							for i := int32(0); i < cur.Children.Length; i++ {
								child := c.layoutElements.Get(cur.Children.Data[i])
								if i > 0 {
									c.renderCommands.Add(RenderCommand{
										BoundingBox: BoundingBox{
											X:      upBBox.X + borderOffset.X - halfW,
											Y:      upBBox.Y,
											Width:  float32(cur.Config.Border.Width.BetweenChildren),
											Height: cur.Dimensions.Height,
										},
										RenderData: RenderData{
											Rectangle: RectangleRenderData{BackgroundColor: cur.Config.Border.Color},
										},
										UserData:    cur.Config.UserData,
										ID:          HashNumber(uint32(cur.Children.Length+1+i), cur.ID).ID,
										ZIndex:      treeZ,

										CommandType: RenderCommandTypeRectangle,
									})
								}
								borderOffset.X += child.Dimensions.Width + float32(layoutCfg.ChildGap)
							}
						} else {
							for i := int32(0); i < cur.Children.Length; i++ {
								child := c.layoutElements.Get(cur.Children.Data[i])
								if i > 0 {
									c.renderCommands.Add(RenderCommand{
										BoundingBox: BoundingBox{
											X:      upBBox.X,
											Y:      upBBox.Y + borderOffset.Y - halfW,
											Width:  cur.Dimensions.Width,
											Height: float32(cur.Config.Border.Width.BetweenChildren),
										},
										RenderData: RenderData{
											Rectangle: RectangleRenderData{BackgroundColor: cur.Config.Border.Color},
										},
										UserData:    cur.Config.UserData,
										ID:          HashNumber(uint32(cur.Children.Length+1+i), cur.ID).ID,
										ZIndex:      treeZ,

										CommandType: RenderCommandTypeRectangle,
									})
								}
								borderOffset.Y += child.Dimensions.Height + float32(layoutCfg.ChildGap)
							}
						}
					}
				}

				if cur.Config.OverlayColor.A > 0 {
					c.renderCommands.Add(RenderCommand{
						RenderData:  RenderData{OverlayColor: OverlayColorRenderData{}}, // C emits zero color here; the END marker doesn't carry color
						UserData:    cur.Config.UserData,
						ID:          cur.ID,
						ZIndex:      treeZ,

						CommandType: RenderCommandTypeOverlayColorEnd,
					})
				}

				if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
					c.renderCommands.Add(RenderCommand{
						ID:          HashNumber(cur.ID, uint32(rootChildCount)+11).ID,
						ZIndex:      treeZ,

						CommandType: RenderCommandTypeScissorEnd,
					})
				}
			}
			dfs = dfs[:idx]
			visited = visited[:idx]
			continue
		}
		visited[idx] = true

		bbox := BoundingBox{
			X:      node.position.X,
			Y:      node.position.Y,
			Width:  cur.Dimensions.Width,
			Height: cur.Dimensions.Height,
		}

		// Transition override (mirrors Clay__CalculateFinalLayout's
		// useStoredBoundingBoxes branch at oracle/clay.h ~line 2918-2938).
		// During the second EndLayout pass, applyStoredTransitionToBoundingBox
		// replaces the per-axis values with whatever the state machine
		// interpolated to this frame. Returns true when the element is an
		// exiting transition that already completed (skip emitting its subtree).
		if c.useStoredBoundingBoxes && !cur.IsTextElement && cur.Config.Transition.Handler != nil {
			if c.applyStoredTransitionToBoundingBox(cur, &bbox) {
				dfs = dfs[:idx]
				visited = visited[:idx]
				continue
			}
		}

		// Record the final bbox on the hashmap item so caller queries
		// (Hovered, GetElementData) see correct values.
		if item := c.getHashMapItem(cur.ID); item != nil {
			item.BoundingBox = bbox
		}

		// Offscreen elements still descend (their floating children might be
		// in view, their bbox is still recorded above) but skip the actual
		// render-command emission. Matches the C `if (generateRenderCommands
		// && !offscreen)` gate at oracle/clay.h ~2933.
		offscreen := c.elementIsOffscreen(bbox)
		if offscreen {
			if cur.IsTextElement {
				dfs = dfs[:idx]
				visited = visited[:idx]
				continue
			}
			// Non-text: fall through to child positioning so the descent
			// happens, but tag-skip the emit-blocks via the wrap below.
			goto skipEmit
		}

		// Emit render commands for the current element.
		if cur.IsTextElement {
			// Iterate wrappedLines (built earlier by wrapTextElements). One
			// TEXT command per line, positioned via lineHeight stride and
			// horizontal alignment offset within the leaf's bbox.
			textCfg := cur.TextConfig
			naturalLineHeight := cur.TextElementData.PreferredDimensions.Height
			finalLineHeight := naturalLineHeight
			if textCfg.LineHeight > 0 {
				finalLineHeight = float32(textCfg.LineHeight)
			}
			lineHeightOffset := (finalLineHeight - naturalLineHeight) / 2
			yPosition := lineHeightOffset
			textBase := cur.TextElementData.Text.Text
			for lineIdx := int32(0); lineIdx < cur.TextElementData.WrappedLines.Length; lineIdx++ {
				line := &cur.TextElementData.WrappedLines.Data[lineIdx]
				if len(line.Line.Text) == 0 {
					yPosition += finalLineHeight
					continue
				}
				offsetX := bbox.Width - line.Dimensions.Width
				switch textCfg.TextAlignment {
				case TextAlignLeft:
					offsetX = 0
				case TextAlignCenter:
					offsetX /= 2
				}
				c.renderCommands.Add(RenderCommand{
					BoundingBox: BoundingBox{
						X: bbox.X + offsetX, Y: bbox.Y + yPosition,
						Width: line.Dimensions.Width, Height: line.Dimensions.Height,
					},
					RenderData: RenderData{
						Text: TextRenderData{
							StringContents: StringSlice{Text: line.Line.Text, Base: textBase},
							TextColor:      textCfg.TextColor,
							FontID:         textCfg.FontID,
							FontSize:       textCfg.FontSize,
							LetterSpacing:  textCfg.LetterSpacing,
							LineHeight:     textCfg.LineHeight,
						},
					},
					UserData:    textCfg.UserData,
					ID:          HashNumber(uint32(lineIdx), cur.ID).ID,
					ZIndex:      treeZ,

					CommandType: RenderCommandTypeText,
				})
				yPosition += finalLineHeight
			}
			// Text leaves have no children; pop now (no upward visit needed).
			dfs = dfs[:idx]
			visited = visited[:idx]
			continue
		}

		// Order of downward emission matches Clay__CalculateFinalLayout
		// (oracle/clay.h ~line 3010-3083): OVERLAY_COLOR_START → IMAGE →
		// CUSTOM → SCISSOR_START → RECTANGLE.

		if cur.Config.OverlayColor.A > 0 {
			c.renderCommands.Add(RenderCommand{
				RenderData: RenderData{
					OverlayColor: OverlayColorRenderData{Color: cur.Config.OverlayColor},
				},
				UserData:    cur.Config.UserData,
				ID:          cur.ID,
				ZIndex:      treeZ,

				CommandType: RenderCommandTypeOverlayColorStart,
			})
		}

		if cur.Config.Image.ImageData != nil {
			c.renderCommands.Add(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Image: ImageRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
						ImageData:       cur.Config.Image.ImageData,
					},
				},
				UserData:    cur.Config.UserData,
				ID:          cur.ID,
				ZIndex:      treeZ,

				CommandType: RenderCommandTypeImage,
			})
		}

		if cur.Config.Custom.CustomData != nil {
			c.renderCommands.Add(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Custom: CustomRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
						CustomData:      cur.Config.Custom.CustomData,
					},
				},
				UserData:    cur.Config.UserData,
				ID:          cur.ID,
				ZIndex:      treeZ,

				CommandType: RenderCommandTypeCustom,
			})
		}

		if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
			c.renderCommands.Add(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Clip: ClipRenderData{
						Horizontal: cur.Config.Clip.Horizontal,
						Vertical:   cur.Config.Clip.Vertical,
					},
				},
				UserData:    cur.Config.UserData,
				ID:          cur.ID,
				ZIndex:      treeZ,

				CommandType: RenderCommandTypeScissorStart,
			})
		}

		// Background rectangle. Matches C: emitted whenever
		// BackgroundColor.A > 0, regardless of image/custom/clip — those
		// emit their own commands before this and the renderer composites
		// them in declaration order.
		if cur.Config.BackgroundColor.A > 0 {
			c.renderCommands.Add(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Rectangle: RectangleRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
					},
				},
				UserData:    cur.Config.UserData,
				ID:          cur.ID,
				ZIndex:      treeZ,

				CommandType: RenderCommandTypeRectangle,
			})
		}

	skipEmit:
		// No children → nothing more to do for this node. Mark for pop on
		// next iteration via the upward-visit path.
		if cur.Children.Length == 0 {
			continue
		}

		layoutCfg := &cur.Config.Layout

		// On-axis ChildAlignment: compute the bounding box of all children
		// along the layout axis, then shift the starting nextChildOffset to
		// honor center/right/bottom alignment.
		var contentSize Dimensions
		if layoutCfg.LayoutDirection == LeftToRight {
			for i := int32(0); i < cur.Children.Length; i++ {
				child := c.layoutElements.Get(cur.Children.Data[i])
				contentSize.Width += child.Dimensions.Width
				if child.Dimensions.Height > contentSize.Height {
					contentSize.Height = child.Dimensions.Height
				}
			}
			contentSize.Width += float32(maxInt32(cur.Children.Length-1, 0)) * float32(layoutCfg.ChildGap)
			extraSpace := cur.Dimensions.Width -
				float32(layoutCfg.Padding.Left+layoutCfg.Padding.Right) - contentSize.Width
			switch layoutCfg.ChildAlignment.X {
			case AlignXLeft:
				extraSpace = 0
			case AlignXCenter:
				extraSpace /= 2
			}
			if extraSpace < 0 {
				extraSpace = 0
			}
			node.nextChildOffset.X += extraSpace
		} else if layoutCfg.LayoutDirection == TopToBottom {
			for i := int32(0); i < cur.Children.Length; i++ {
				child := c.layoutElements.Get(cur.Children.Data[i])
				if child.Dimensions.Width > contentSize.Width {
					contentSize.Width = child.Dimensions.Width
				}
				contentSize.Height += child.Dimensions.Height
			}
			contentSize.Height += float32(maxInt32(cur.Children.Length-1, 0)) * float32(layoutCfg.ChildGap)
			extraSpace := cur.Dimensions.Height -
				float32(layoutCfg.Padding.Top+layoutCfg.Padding.Bottom) - contentSize.Height
			switch layoutCfg.ChildAlignment.Y {
			case AlignYTop:
				extraSpace = 0
			case AlignYCenter:
				extraSpace /= 2
			}
			if extraSpace < 0 {
				extraSpace = 0
			}
			node.nextChildOffset.Y += extraSpace
		}

		// If this element is a clip container, record the inner content
		// size on its scrollContainerData entry (used by GetScrollContainerData
		// and the drag-scroll math in UpdateScrollContainers). Also remember
		// the scroll offset so children are translated correctly below.
		var scrollOffset Vector2
		if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
			for sci := int32(0); sci < c.scrollContainerDatas.Length; sci++ {
				sd := c.scrollContainerDatas.Get(sci)
				if sd.ElementID != cur.ID {
					continue
				}
				sd.BoundingBox = bbox
				sd.ContentSize = Dimensions{
					Width:  contentSize.Width + float32(layoutCfg.Padding.Left+layoutCfg.Padding.Right),
					Height: contentSize.Height + float32(layoutCfg.Padding.Top+layoutCfg.Padding.Bottom),
				}
				scrollOffset = cur.Config.Clip.ChildOffset
				break
			}
		}

		// Push children onto the DFS stack in REVERSE order so popping
		// processes them in declaration order.
		childCount := int(cur.Children.Length)
		startIdx := len(dfs)
		for j := 0; j < childCount; j++ {
			dfs = append(dfs, layoutTreeNode{})
			visited = append(visited, false)
		}
		// `node` was taken before the append above and may now be a dangling
		// pointer if the slice grew. Re-derive it so the rest of the loop is
		// safe regardless of backing-array reallocation.
		node = &dfs[idx]

		for i := int32(0); i < cur.Children.Length; i++ {
			child := c.layoutElements.Get(cur.Children.Data[i])

			// Cross-axis ChildAlignment: for each individual child, set the
			// non-layout-axis offset to align it within the parent's content
			// area according to ChildAlignment.X/Y.
			if layoutCfg.LayoutDirection == LeftToRight {
				node.nextChildOffset.Y = float32(layoutCfg.Padding.Top)
				whiteSpace := cur.Dimensions.Height -
					float32(layoutCfg.Padding.Top+layoutCfg.Padding.Bottom) - child.Dimensions.Height
				switch layoutCfg.ChildAlignment.Y {
				case AlignYTop:
					// no-op
				case AlignYCenter:
					node.nextChildOffset.Y += whiteSpace / 2
				case AlignYBottom:
					node.nextChildOffset.Y += whiteSpace
				}
			} else {
				node.nextChildOffset.X = float32(layoutCfg.Padding.Left)
				whiteSpace := cur.Dimensions.Width -
					float32(layoutCfg.Padding.Left+layoutCfg.Padding.Right) - child.Dimensions.Width
				switch layoutCfg.ChildAlignment.X {
				case AlignXLeft:
					// no-op
				case AlignXCenter:
					node.nextChildOffset.X += whiteSpace / 2
				case AlignXRight:
					node.nextChildOffset.X += whiteSpace
				}
			}

			childPos := Vector2{
				X: bbox.X + node.nextChildOffset.X + scrollOffset.X,
				Y: bbox.Y + node.nextChildOffset.Y + scrollOffset.Y,
			}

			newNodeIdx := startIdx + childCount - 1 - int(i)
			dfs[newNodeIdx] = layoutTreeNode{
				element:  child,
				position: childPos,
				nextChildOffset: Vector2{
					X: float32(child.Config.Layout.Padding.Left),
					Y: float32(child.Config.Layout.Padding.Top),
				},
			}
			visited[newNodeIdx] = false

			// Advance the parent's cursor along the layout axis for the next
			// sibling.
			if layoutCfg.LayoutDirection == LeftToRight {
				node.nextChildOffset.X += child.Dimensions.Width + float32(layoutCfg.ChildGap)
			} else {
				node.nextChildOffset.Y += child.Dimensions.Height + float32(layoutCfg.ChildGap)
			}
		}
	}

	// Close the floating-tree-root scissor opened above, if any.
	if emitClipBound {
		c.renderCommands.Add(RenderCommand{
			ID:          HashNumber(root.ID, uint32(root.Children.Length)+11).ID,
			ZIndex:      treeRoot.ZIndex,
			CommandType: RenderCommandTypeScissorEnd,
		})
	}
}

// computeFloatingPosition resolves a floating element's top-left corner
// from its attach-point configuration. The parent attach-point picks an
// anchor on the parent's bounding box; the element attach-point picks the
// corresponding anchor on the element (so e.g. parent=LEFT_BOTTOM +
// element=LEFT_TOP places the element's top-left at the parent's bottom-
// left). The configured Offset is added unconditionally.
//
// Mirrors the switch ladder at oracle/clay.h ~line 2728-2776.
func computeFloatingPosition(parentBBox BoundingBox, elementDim Dimensions, ap FloatingAttachPoints, offset Vector2) Vector2 {
	var p Vector2

	switch ap.Parent {
	case AttachPointLeftTop, AttachPointLeftCenter, AttachPointLeftBottom:
		p.X = parentBBox.X
	case AttachPointCenterTop, AttachPointCenterCenter, AttachPointCenterBottom:
		p.X = parentBBox.X + parentBBox.Width/2
	case AttachPointRightTop, AttachPointRightCenter, AttachPointRightBottom:
		p.X = parentBBox.X + parentBBox.Width
	}
	switch ap.Element {
	case AttachPointLeftTop, AttachPointLeftCenter, AttachPointLeftBottom:
		// no-op
	case AttachPointCenterTop, AttachPointCenterCenter, AttachPointCenterBottom:
		p.X -= elementDim.Width / 2
	case AttachPointRightTop, AttachPointRightCenter, AttachPointRightBottom:
		p.X -= elementDim.Width
	}
	switch ap.Parent {
	case AttachPointLeftTop, AttachPointCenterTop, AttachPointRightTop:
		p.Y = parentBBox.Y
	case AttachPointLeftCenter, AttachPointCenterCenter, AttachPointRightCenter:
		p.Y = parentBBox.Y + parentBBox.Height/2
	case AttachPointLeftBottom, AttachPointCenterBottom, AttachPointRightBottom:
		p.Y = parentBBox.Y + parentBBox.Height
	}
	switch ap.Element {
	case AttachPointLeftTop, AttachPointCenterTop, AttachPointRightTop:
		// no-op
	case AttachPointLeftCenter, AttachPointCenterCenter, AttachPointRightCenter:
		p.Y -= elementDim.Height / 2
	case AttachPointLeftBottom, AttachPointCenterBottom, AttachPointRightBottom:
		p.Y -= elementDim.Height
	}

	p.X += offset.X
	p.Y += offset.Y
	return p
}
