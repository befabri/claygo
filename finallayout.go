package claygo

// finallayout.go ports the second half of Clay__CalculateFinalLayout
// (oracle/clay.h ~line 2573): after the sizing solver has decided how big
// each element is, this pass walks the tree depth-first and (a) decides
// each element's bounding-box position based on its parent's padding,
// child gap and child alignment, then (b) emits the per-element render
// commands in z-order.
//
// This file also handles the final aspect-ratio corrections, root z-sort,
// clip scissor emission, floating-root positioning, and render-command
// generation for every command type Clay exposes.

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

// emitCommand appends a render command, firing ErrorTypeElementsCapacityExceeded
// once if the renderCommands Array is full. Mirrors Clay__AddRenderCommand's
// capacity-1 guard (oracle/clay.h ~line 2546). Used by every render-command
// emission site in this file so the user sees an actionable error instead of
// silent truncation when the layout has more commands than the arena holds.
func (c *Context) emitCommand(cmd RenderCommand) {
	if c.renderCommands.Length >= c.renderCommands.Capacity-1 {
		if !c.warnMaxRenderCommandsExceeded {
			c.reportError(ErrorTypeElementsCapacityExceeded,
				"Clay ran out of room in its render-command array. Raise SetMaxElementCount() and re-Initialize with a larger arena.")
			c.warnMaxRenderCommandsExceeded = true
		}
		return
	}
	c.renderCommands.Add(cmd)
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
	aspectRatioElements := c.collectAspectRatioElements()
	c.wrappedTextLines.Length = 0
	c.wrapTextElements()
	c.scaleAspectRatioHeights(aspectRatioElements)
	c.propagateTextHeights()
	c.sizeContainersAlongAxis(false)
	c.scaleAspectRatioWidths(aspectRatioElements)

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
	// prefix of the fixed-capacity array so callers don't see stale slots
	// from prior frames.
	commands := c.renderCommands.Data[:c.renderCommands.Length]
	return RenderCommandArray{Commands: commands}
}

func (c *Context) collectAspectRatioElements() []int32 {
	var out []int32
	var stack []int32
	for rootIdx := int32(0); rootIdx < c.layoutElementTreeRoots.Length; rootIdx++ {
		root := c.layoutElements.Get(c.layoutElementTreeRoots.Get(rootIdx).LayoutElementIndex)
		for i := int32(0); i < root.Children.Length; i++ {
			stack = append(stack, root.Children.Data[i])
		}
	}
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		element := c.layoutElements.GetCheckCapacity(idx)
		if element.IsTextElement {
			continue
		}
		if element.Config.AspectRatio.AspectRatio != 0 {
			out = append(out, idx)
		}
		for i := int32(0); i < element.Children.Length; i++ {
			stack = append(stack, element.Children.Data[i])
		}
	}
	return out
}

func (c *Context) scaleAspectRatioHeights(indices []int32) {
	for _, idx := range indices {
		element := c.layoutElements.GetCheckCapacity(idx)
		ar := element.Config.AspectRatio.AspectRatio
		if ar == 0 {
			continue
		}
		element.Dimensions.Height = element.Dimensions.Width * (1 / ar)
		element.Config.Layout.Sizing.Height.MinMax.Max = element.Dimensions.Height
	}
}

func (c *Context) scaleAspectRatioWidths(indices []int32) {
	for _, idx := range indices {
		element := c.layoutElements.GetCheckCapacity(idx)
		ar := element.Config.AspectRatio.AspectRatio
		if ar == 0 {
			continue
		}
		element.Dimensions.Width = ar * element.Dimensions.Height
	}
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
			if c.externalScrollHandlingEnabled && clipItem.LayoutElement != nil {
				clipCfg := clipItem.LayoutElement.Config.Clip
				if clipCfg.Horizontal {
					rootPosition.X += clipCfg.ChildOffset.X
				}
				if clipCfg.Vertical {
					rootPosition.Y += clipCfg.ChildOffset.Y
				}
			}
			clipScissorBBox = clipItem.BoundingBox
			emitClipBound = true
			c.emitCommand(RenderCommand{
				BoundingBox: clipScissorBBox,
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
	// Downward render commands emitted under this tree root carry the root's
	// ZIndex. Some upward/end commands intentionally keep the zero value to match
	// oracle initializers.
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
				// Source the bbox from the hashmap (set during the downward
				// visit, post-transition-application) rather than from
				// node.position+cur.Dimensions, which capture the pre-
				// transition position. Otherwise BORDER/dividers draw at the
				// un-interpolated position while RECTANGLE/IMAGE drew at the
				// interpolated one.
				upBBox := BoundingBox{X: node.position.X, Y: node.position.Y, Width: cur.Dimensions.Width, Height: cur.Dimensions.Height}
				if item := c.getHashMapItem(cur.ID); item != nil {
					upBBox = item.BoundingBox
				}
				// Offscreen elements skip the upward emit (BORDER, dividers,
				// OVERLAY_END, SCISSOR_END). Matches C `if (generateRenderCommands
				// && !Clay__ElementIsOffscreen(&currentElementData->boundingBox))`
				// gate around the entire upward-emit block (oracle/clay.h:2820).
				if c.elementIsOffscreen(upBBox) {
					dfs = dfs[:idx]
					visited = visited[:idx]
					continue
				}
				layoutCfg := &cur.Config.Layout
				// Clip containers offset their dividers by the clip's
				// childOffset so the dividers scroll with their siblings.
				// Matches C 2823-2835.
				var dividerScrollOffset Vector2
				if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
					dividerScrollOffset = cur.Config.Clip.ChildOffset
					if c.externalScrollHandlingEnabled {
						dividerScrollOffset = Vector2{}
					}
				}

				if borderHasAnyWidth(cur.Config.Border) {
					c.emitCommand(RenderCommand{
						BoundingBox: upBBox,
						RenderData: RenderData{
							Border: BorderRenderData{
								Color:        cur.Config.Border.Color,
								CornerRadius: cur.Config.CornerRadius,
								Width:        cur.Config.Border.Width,
							},
						},
						UserData: cur.Config.UserData,
						ID:       HashNumber(uint32(cur.Children.Length), cur.ID).ID,

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
									c.emitCommand(RenderCommand{
										BoundingBox: BoundingBox{
											X:      upBBox.X + borderOffset.X + dividerScrollOffset.X - halfW,
											Y:      upBBox.Y + dividerScrollOffset.Y,
											Width:  float32(cur.Config.Border.Width.BetweenChildren),
											Height: cur.Dimensions.Height,
										},
										RenderData: RenderData{
											Rectangle: RectangleRenderData{BackgroundColor: cur.Config.Border.Color},
										},
										UserData: cur.Config.UserData,
										ID:       HashNumber(uint32(cur.Children.Length+1+i), cur.ID).ID,

										CommandType: RenderCommandTypeRectangle,
									})
								}
								borderOffset.X += child.Dimensions.Width + float32(layoutCfg.ChildGap)
							}
						} else {
							for i := int32(0); i < cur.Children.Length; i++ {
								child := c.layoutElements.Get(cur.Children.Data[i])
								if i > 0 {
									c.emitCommand(RenderCommand{
										BoundingBox: BoundingBox{
											X:      upBBox.X + dividerScrollOffset.X,
											Y:      upBBox.Y + borderOffset.Y + dividerScrollOffset.Y - halfW,
											Width:  cur.Dimensions.Width,
											Height: float32(cur.Config.Border.Width.BetweenChildren),
										},
										RenderData: RenderData{
											Rectangle: RectangleRenderData{BackgroundColor: cur.Config.Border.Color},
										},
										UserData: cur.Config.UserData,
										ID:       HashNumber(uint32(cur.Children.Length+1+i), cur.ID).ID,

										CommandType: RenderCommandTypeRectangle,
									})
								}
								borderOffset.Y += child.Dimensions.Height + float32(layoutCfg.ChildGap)
							}
						}
					}
				}

				if cur.Config.OverlayColor.A > 0 {
					c.emitCommand(RenderCommand{
						RenderData: RenderData{OverlayColor: OverlayColorRenderData{}}, // C emits zero color here; the END marker doesn't carry color
						UserData:   cur.Config.UserData,
						ID:         cur.ID,
						ZIndex:     treeZ,

						CommandType: RenderCommandTypeOverlayColorEnd,
					})
				}

				if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
					c.emitCommand(RenderCommand{
						ID: HashNumber(cur.ID, uint32(rootChildCount)+11).ID,

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
		if cur.Config.Floating.AttachTo != AttachToNone {
			expand := cur.Config.Floating.Expand
			bbox.X -= expand.Width
			bbox.Width += expand.Width * 2
			bbox.Y -= expand.Height
			bbox.Height += expand.Height * 2
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
				c.emitCommand(RenderCommand{
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
					UserData: textCfg.UserData,
					ID:       HashNumber(uint32(lineIdx), cur.ID).ID,
					ZIndex:   treeZ,

					CommandType: RenderCommandTypeText,
				})
				yPosition += finalLineHeight
				// Halt the per-line loop once we've stepped past the viewport
				// bottom; further lines would be off-screen. Matches C
				// `if (!disableCulling && currentElementBoundingBox.y + yPosition
				// > context->layoutDimensions.height) break;` at oracle/clay.h:3006.
				if c.cullingEnabled && bbox.Y+yPosition > c.layoutDimensions.Height {
					break
				}
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
			c.emitCommand(RenderCommand{
				RenderData: RenderData{
					OverlayColor: OverlayColorRenderData{Color: cur.Config.OverlayColor},
				},
				UserData: cur.Config.UserData,
				ID:       cur.ID,
				ZIndex:   treeZ,

				CommandType: RenderCommandTypeOverlayColorStart,
			})
		}

		if cur.Config.Image.ImageData != nil {
			c.emitCommand(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Image: ImageRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
						ImageData:       cur.Config.Image.ImageData,
					},
				},
				UserData: cur.Config.UserData,
				ID:       cur.ID,
				ZIndex:   treeZ,

				CommandType: RenderCommandTypeImage,
			})
		}

		if cur.Config.Custom.CustomData != nil {
			c.emitCommand(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Custom: CustomRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
						CustomData:      cur.Config.Custom.CustomData,
					},
				},
				UserData: cur.Config.UserData,
				ID:       cur.ID,
				ZIndex:   treeZ,

				CommandType: RenderCommandTypeCustom,
			})
		}

		if cur.Config.Clip.Horizontal || cur.Config.Clip.Vertical {
			c.emitCommand(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Clip: ClipRenderData{
						Horizontal: cur.Config.Clip.Horizontal,
						Vertical:   cur.Config.Clip.Vertical,
					},
				},
				UserData: cur.Config.UserData,
				ID:       cur.ID,
				ZIndex:   treeZ,

				CommandType: RenderCommandTypeScissorStart,
			})
		}

		// Background rectangle. Matches C: emitted whenever
		// BackgroundColor.A > 0, regardless of image/custom/clip — those
		// emit their own commands before this and the renderer composites
		// them in declaration order.
		if cur.Config.BackgroundColor.A > 0 {
			c.emitCommand(RenderCommand{
				BoundingBox: bbox,
				RenderData: RenderData{
					Rectangle: RectangleRenderData{
						BackgroundColor: cur.Config.BackgroundColor,
						CornerRadius:    cur.Config.CornerRadius,
					},
				},
				UserData: cur.Config.UserData,
				ID:       cur.ID,
				ZIndex:   treeZ,

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
				if child.Exiting {
					continue
				}
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
				if child.Exiting {
					continue
				}
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
				if c.externalScrollHandlingEnabled {
					scrollOffset = Vector2{}
				}
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
			if !child.Exiting {
				if layoutCfg.LayoutDirection == LeftToRight {
					node.nextChildOffset.X += child.Dimensions.Width + float32(layoutCfg.ChildGap)
				} else {
					node.nextChildOffset.Y += child.Dimensions.Height + float32(layoutCfg.ChildGap)
				}
			}
		}
	}

	// Close the floating-tree-root scissor opened above, if any.
	if emitClipBound {
		c.emitCommand(RenderCommand{
			ID:          HashNumber(root.ID, uint32(root.Children.Length)+11).ID,
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
