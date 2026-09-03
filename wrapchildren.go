package claygo

// Child wrapping (LayoutConfig.WrapChildren) is a claygo extension: upstream
// clay.h has no element wrapping, so unlike the rest of the port nothing here
// mirrors it. The C reference is oracle/patches/0001-child-wrap.patch, applied
// to the verbatim header by the oracle build; changing a layout rule here means
// changing the patch too, or the ext_ goldens diverge.

// WrapLine is one run of children that share a line (row wrap, LeftToRight)
// or a column (column wrap, TopToBottom) of a parent declared with
// WrapChildren. Lines are contiguous child ranges that together cover every
// child; exiting children never break a line and add nothing to it, but stay
// inside the range they fall in. Mirrors Clay__WrapLine.
type WrapLine struct {
	// Start is the child offset of the first child in the line.
	Start int32
	// Count is the number of children in [Start, Start+Count), exiting ones
	// included.
	Count int32
	// ContentSize is the wrap-axis content at pack time: live child sizes
	// plus gaps.
	ContentSize float32
	// NaturalExtent is the cross-axis size of the largest live child.
	NaturalExtent float32
	// Extent is NaturalExtent plus this line's share of the parent's
	// cross-axis slack; children size and align within it.
	Extent float32
}

// isResizableChild reports whether the solver may resize child along the given
// axis. Text qualifies only when it wraps by words, because the wrapping pass
// may need to flow it into a smaller width.
func isResizableChild(child *LayoutElement, xAxis bool) bool {
	childSizing := getElementSizing(child, xAxis)
	return childSizing.Type != SizingTypePercent && childSizing.Type != SizingTypeFixed &&
		(!child.IsTextElement || child.TextConfig.WrapMode == TextWrapWords)
}

func wrapPadding(cfg *LayoutConfig, xAxis bool) float32 {
	if xAxis {
		return float32(cfg.Padding.Left + cfg.Padding.Right)
	}
	return float32(cfg.Padding.Top + cfg.Padding.Bottom)
}

func wrapAxisSize(le *LayoutElement, xAxis bool) float32 {
	if xAxis {
		return le.Dimensions.Width
	}
	return le.Dimensions.Height
}

func wrapAxisMin(le *LayoutElement, xAxis bool) float32 {
	if xAxis {
		return le.MinDimensions.Width
	}
	return le.MinDimensions.Height
}

func wrapAxisSizePtr(le *LayoutElement, xAxis bool) *float32 {
	if xAxis {
		return &le.Dimensions.Width
	}
	return &le.Dimensions.Height
}

// wrapChild returns the child at offset in parent's children list.
func (c *Context) wrapChild(parent *LayoutElement, offset int32) *LayoutElement {
	return c.layoutElements.Get(parent.Children.Data[offset])
}

// wrapCloseElement sets a wrapping parent's wrap-axis minimum to its widest
// child (plus padding) instead of the sum, so an ancestor can shrink the
// parent below its single-line width and make it wrap. Mirrors
// Clay__WrapCloseElement; runs from closeElement once Children.Data is set.
func (c *Context) wrapCloseElement(openLE *LayoutElement) {
	layoutCfg := &openLE.Config.Layout
	xAxis := layoutCfg.LayoutDirection == LeftToRight
	// Clip containers keep the padding-only minimum upstream gave them.
	if (xAxis && openLE.Config.Clip.Horizontal) || (!xAxis && openLE.Config.Clip.Vertical) {
		return
	}
	var largestMin float32
	for i := range openLE.Children.Length {
		largestMin = max(largestMin, wrapAxisMin(c.wrapChild(openLE, i), xAxis))
	}
	minSize := wrapPadding(layoutCfg, xAxis) + largestMin
	if xAxis {
		openLE.MinDimensions.Width = minSize
	} else {
		openLE.MinDimensions.Height = minSize
	}
}

// wrapLineContent is the wrap-axis content of one line, accumulated in the
// order sizeOneRoot uses for a whole row (live non-percent sizes and gaps
// first, percent sizes last) so a single line distributes exactly like a
// non-wrapping row. Mirrors Clay__WrapLineContent.
func (c *Context) wrapLineContent(parent *LayoutElement, line *WrapLine, xAxis bool) float32 {
	childGap := float32(parent.Config.Layout.ChildGap)
	var content float32
	isFirstChild := true
	for offset := line.Start; offset < line.Start+line.Count; offset++ {
		child := c.wrapChild(parent, offset)
		if child.Exiting {
			continue
		}
		if getElementSizing(child, xAxis).Type != SizingTypePercent {
			content += wrapAxisSize(child, xAxis)
		}
		if !isFirstChild {
			content += childGap
		}
		isFirstChild = false
	}
	for offset := line.Start; offset < line.Start+line.Count; offset++ {
		child := c.wrapChild(parent, offset)
		if getElementSizing(child, xAxis).Type == SizingTypePercent {
			content += wrapAxisSize(child, xAxis)
		}
	}
	return content
}

// packWrapLines appends the parent's lines to the c.wrapLines pool, packed
// greedy first-fit along the wrap axis at the sizes children have before this
// parent distributes space. If the pool runs out, the children that could not
// start a line join the last one. Mirrors Clay__WrapPackLines.
func (c *Context) packWrapLines(parent *LayoutElement, xAxis bool) {
	layoutCfg := &parent.Config.Layout
	innerSize := wrapAxisSize(parent, xAxis) - wrapPadding(layoutCfg, xAxis)
	childGap := float32(layoutCfg.ChildGap)
	start := c.wrapLines.Length
	parent.WrapLines = ArraySlice[WrapLine]{Data: c.wrapLines.Data[start:start]}
	var line *WrapLine
	var lineSize float32
	lineHasContent := false
	for offset := range parent.Children.Length {
		child := c.wrapChild(parent, offset)
		childSize := wrapAxisSize(child, xAxis)
		// Epsilon keeps packing idempotent: a later pass re-sums grown float32
		// sizes that can land a hair over the inner size.
		if line == nil || (!child.Exiting && lineHasContent && lineSize+childGap+childSize > innerSize+epsilon) {
			if c.wrapLines.Length >= c.wrapLines.Capacity {
				c.reportError(ErrorTypeInternalError,
					"Clay ran out of room in its child-wrap line pool. This is an internal error and is likely a bug.")
				if line != nil {
					line.Count += parent.Children.Length - offset
				}
				break
			}
			line = c.wrapLines.Add(WrapLine{Start: offset})
			parent.WrapLines.Length++
			lineSize = 0
			lineHasContent = false
		}
		line.Count++
		if child.Exiting {
			continue
		}
		if lineHasContent {
			lineSize += childGap + childSize
		} else {
			lineSize += childSize
		}
		lineHasContent = true
	}
	parent.WrapLines.Data = c.wrapLines.Data[start : start+parent.WrapLines.Length]
	for i := range parent.WrapLines.Length {
		packed := &parent.WrapLines.Data[i]
		packed.ContentSize = c.wrapLineContent(parent, packed, xAxis)
	}
}

// updateWrapCrossContent computes each line's natural cross extent and, when
// publish is set, the parent's stacked cross-axis content and minimum. The
// size only grows and is clamped, like propagateTextHeights, so a single line
// reproduces the non-wrapping result exactly; the minimum stacks child
// minimums so an ancestor cannot squash the lines, except for a parent that
// clips on that axis, which keeps upstream's padding-only minimum and can
// hide its content. Column wrap publishes only from the first sizing sweep:
// the second sweep's x pass is the ancestor's final word on the width.
// Mirrors Clay__WrapUpdateCrossContent.
func (c *Context) updateWrapCrossContent(parent *LayoutElement, crossXAxis bool, publish bool) {
	layoutCfg := &parent.Config.Layout
	var content, minContent float32
	for i := range parent.WrapLines.Length {
		line := &parent.WrapLines.Data[i]
		var natural, naturalWithExiting, naturalMin float32
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			child := c.wrapChild(parent, offset)
			childSize := wrapAxisSize(child, crossXAxis)
			// Upstream lets exiting children grow their parent, so the size does too.
			naturalWithExiting = max(naturalWithExiting, childSize)
			if child.Exiting {
				continue
			}
			natural = max(natural, childSize)
			naturalMin = max(naturalMin, wrapAxisMin(child, crossXAxis))
		}
		line.NaturalExtent = natural
		content += naturalWithExiting
		minContent += naturalMin
	}
	gaps := float32(max(parent.WrapLines.Length-1, 0)) * float32(layoutCfg.ChildGap)
	content += gaps
	minContent += gaps
	if !publish {
		return
	}
	// Each padding converts separately, matching upstream's height + top + bottom.
	var sizing SizingAxis
	var size, minSize *float32
	if crossXAxis {
		content = content + float32(layoutCfg.Padding.Left) + float32(layoutCfg.Padding.Right)
		minContent += wrapPadding(layoutCfg, true)
		sizing = layoutCfg.Sizing.Width
		size, minSize = &parent.Dimensions.Width, &parent.MinDimensions.Width
	} else {
		content = content + float32(layoutCfg.Padding.Top) + float32(layoutCfg.Padding.Bottom)
		minContent += wrapPadding(layoutCfg, false)
		sizing = layoutCfg.Sizing.Height
		size, minSize = &parent.Dimensions.Height, &parent.MinDimensions.Height
	}
	*size = clampFloat32(max(content, *size), sizing.MinMax.Min, sizing.MinMax.Max)
	if (crossXAxis && parent.Config.Clip.Horizontal) || (!crossXAxis && parent.Config.Clip.Vertical) {
		return
	}
	*minSize = clampFloat32(minContent, sizing.MinMax.Min, sizing.MinMax.Max)
}

// distributeWrapExtents splits the parent's cross-axis slack among its lines
// the way growUnderflow / shrinkOverflow split a row's slack among children:
// positive slack grows the smallest lines first, negative slack shrinks the
// largest first (never below zero). A single line takes the whole inner size,
// which is what makes one wrapped line identical to a non-wrapping parent.
// lineIndexBuffer is scratch. Mirrors Clay__WrapDistributeExtents.
func (c *Context) distributeWrapExtents(parent *LayoutElement, crossXAxis bool, lineIndexBuffer *Array[int32]) {
	layoutCfg := &parent.Config.Layout
	lineCount := parent.WrapLines.Length
	innerSize := wrapAxisSize(parent, crossXAxis) - wrapPadding(layoutCfg, crossXAxis)
	lines := parent.WrapLines.Data
	var stacked float32
	lineIndexBuffer.Length = 0
	for i := range lineCount {
		lines[i].Extent = lines[i].NaturalExtent
		stacked += lines[i].NaturalExtent
		lineIndexBuffer.Add(i)
	}
	if lineCount == 1 {
		lines[0].Extent = innerSize
		return
	}
	stacked += float32(max(lineCount-1, 0)) * float32(layoutCfg.ChildGap)
	sizeToDistribute := innerSize - stacked
	if sizeToDistribute < 0 {
		for sizeToDistribute < -epsilon && lineIndexBuffer.Length > 0 {
			var largest, secondLargest float32
			sizeToAdd := sizeToDistribute
			for i := range lineIndexBuffer.Length {
				extent := lines[lineIndexBuffer.GetValue(i)].Extent
				if floatEqual(extent, largest) {
					continue
				}
				if extent > largest {
					secondLargest = largest
					largest = extent
				}
				if extent < largest {
					secondLargest = max(secondLargest, extent)
					sizeToAdd = secondLargest - largest
				}
			}
			sizeToAdd = max(sizeToAdd, sizeToDistribute/float32(lineIndexBuffer.Length))
			i := int32(0)
			for i < lineIndexBuffer.Length {
				line := &lines[lineIndexBuffer.GetValue(i)]
				previous := line.Extent
				if floatEqual(line.Extent, largest) {
					line.Extent += sizeToAdd
					if line.Extent <= 0 {
						line.Extent = 0
						lineIndexBuffer.RemoveSwapback(i)
						sizeToDistribute -= line.Extent - previous
						continue
					}
					sizeToDistribute -= line.Extent - previous
				}
				i++
			}
		}
		return
	}
	for sizeToDistribute > epsilon && lineIndexBuffer.Length > 0 {
		smallest := maxFloat32
		secondSmallest := maxFloat32
		sizeToAdd := sizeToDistribute
		for i := range lineIndexBuffer.Length {
			extent := lines[lineIndexBuffer.GetValue(i)].Extent
			if floatEqual(extent, smallest) {
				continue
			}
			if extent < smallest {
				secondSmallest = smallest
				smallest = extent
			}
			if extent > smallest {
				secondSmallest = min(secondSmallest, extent)
				sizeToAdd = secondSmallest - smallest
			}
		}
		sizeToAdd = min(sizeToAdd, sizeToDistribute/float32(lineIndexBuffer.Length))
		for i := range lineIndexBuffer.Length {
			line := &lines[lineIndexBuffer.GetValue(i)]
			previous := line.Extent
			if floatEqual(line.Extent, smallest) {
				line.Extent += sizeToAdd
				sizeToDistribute -= line.Extent - previous
			}
		}
	}
}

// sizeWrapLinesAlongAxis is the along-axis branch of sizeOneRoot for a
// wrapping parent: pack, then run shrinkOverflow / growUnderflow once per
// line on that line's resizable children. A TopToBottom parent (column wrap)
// also settles its cross-axis bookkeeping here, because its children's widths
// are already final. resizable is the scratch buffer sizeOneRoot borrows.
// Mirrors Clay__WrapSizeAlongAxis.
func (c *Context) sizeWrapLinesAlongAxis(parent *LayoutElement, xAxis bool, resizable *Array[int32]) {
	c.packWrapLines(parent, xAxis)
	layoutCfg := &parent.Config.Layout
	innerSize := wrapAxisSize(parent, xAxis) - wrapPadding(layoutCfg, xAxis)
	clipsAxis := (xAxis && parent.Config.Clip.Horizontal) || (!xAxis && parent.Config.Clip.Vertical)
	for i := range parent.WrapLines.Length {
		line := &parent.WrapLines.Data[i]
		resizable.Length = 0
		growContainerCount := int32(0)
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			childIdx := parent.Children.Data[offset]
			child := c.layoutElements.Get(childIdx)
			if child.Exiting {
				continue
			}
			if isResizableChild(child, xAxis) {
				resizable.Add(childIdx)
			}
			if getElementSizing(child, xAxis).Type == SizingTypeGrow {
				growContainerCount++
			}
		}
		sizeToDistribute := innerSize - line.ContentSize
		switch {
		case sizeToDistribute < 0:
			if clipsAxis {
				continue
			}
			shrinkOverflow(c, &sizeToDistribute, resizable, xAxis)
		case sizeToDistribute > 0 && growContainerCount > 0:
			growUnderflow(c, &sizeToDistribute, resizable, xAxis)
		}
	}
	if !xAxis {
		c.updateWrapCrossContent(parent, true, !c.wrapColumnLinesValid)
		c.distributeWrapExtents(parent, true, resizable)
	}
}

// sizeWrapLinesAcrossAxis is the cross-axis branch of sizeOneRoot for a
// wrapping parent: each child sizes against its line's extent instead of the
// parent's inner size, with the same clip rule that lets children keep their
// content size. It reports whether it handled the parent; false means the
// upstream cross-axis code should run (no lines were packed).
//
// A column-wrap parent has no lines until the y pass, so in the first sweep's
// x pass its children are left alone at their content widths, exactly as a
// row's children keep their content heights until the y pass. Sizing them
// against the whole parent here would turn every Grow child's grown width
// into its column's "natural" width. Mirrors Clay__WrapSizeAcrossAxis.
func (c *Context) sizeWrapLinesAcrossAxis(parent *LayoutElement, xAxis bool, lineIndexBuffer *Array[int32]) bool {
	if xAxis && !c.wrapColumnLinesValid {
		return true
	}
	if parent.WrapLines.Length == 0 {
		return false
	}
	c.distributeWrapExtents(parent, xAxis, lineIndexBuffer)
	clipsAxis := (xAxis && parent.Config.Clip.Horizontal) || (!xAxis && parent.Config.Clip.Vertical)
	for i := range parent.WrapLines.Length {
		line := &parent.WrapLines.Data[i]
		maxSize := line.Extent
		if clipsAxis {
			maxSize = max(maxSize, line.NaturalExtent)
		}
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			child := c.wrapChild(parent, offset)
			if child.Exiting || !isResizableChild(child, xAxis) {
				continue
			}
			childSizing := getElementSizing(child, xAxis)
			minSize := wrapAxisMin(child, xAxis)
			sizePtr := wrapAxisSizePtr(child, xAxis)
			if childSizing.Type == SizingTypeGrow {
				*sizePtr = min(maxSize, childSizing.MinMax.Max)
			}
			*sizePtr = max(minSize, min(*sizePtr, maxSize))
		}
	}
	return true
}

// wrapLineCrossContent is the largest live child of a line on the cross
// axis, from final child sizes. Mirrors Clay__WrapLineCrossContent.
func (c *Context) wrapLineCrossContent(parent *LayoutElement, line *WrapLine, rows bool) float32 {
	var largest float32
	for offset := line.Start; offset < line.Start+line.Count; offset++ {
		child := c.wrapChild(parent, offset)
		if child.Exiting {
			continue
		}
		largest = max(largest, wrapAxisSize(child, !rows))
	}
	return largest
}

// wrapLineAdvance is how far the cross cursor moves past a line: its extent,
// or its final content when that is larger. A line's extent can shrink below
// its content when the parent is shorter than its stacked lines and a child
// could not follow (a clipping parent leaves children alone; a Fixed or
// min-clamped child cannot shrink), and then the next line must start below
// that child rather than over it; the parent overflows instead, as upstream's
// rows do. Mirrors Clay__WrapLineAdvance.
func (c *Context) wrapLineAdvance(parent *LayoutElement, line *WrapLine, rows bool) float32 {
	return max(line.Extent, c.wrapLineCrossContent(parent, line, rows))
}

// positionWrapChildren replaces emitTreeRoot's on-axis alignment, scroll
// content size and child push for a wrapping parent. Per line, the line's
// final content is aligned on the wrap axis like a whole row, and each child
// is aligned inside its line's extent on the cross axis. Children are pushed
// onto dfs in reverse so popping yields declaration order; the grown slices
// are returned. Mirrors Clay__WrapPositionChildren.
func (c *Context) positionWrapChildren(cur *LayoutElement, bbox BoundingBox, scrollOffset Vector2,
	scrollData *scrollContainerDataInternal, dfs []layoutTreeNode, visited []bool) ([]layoutTreeNode, []bool) {
	layoutCfg := &cur.Config.Layout
	rows := layoutCfg.LayoutDirection == LeftToRight
	alongInner := wrapAxisSize(cur, rows) - wrapPadding(layoutCfg, rows)
	var alongContent, crossContent float32
	crossCursor := float32(layoutCfg.Padding.Left)
	if rows {
		crossCursor = float32(layoutCfg.Padding.Top)
	}
	childCount := int(cur.Children.Length)
	startIdx := len(dfs)
	for range childCount {
		dfs = append(dfs, layoutTreeNode{})
		visited = append(visited, false)
	}
	for i := range cur.WrapLines.Length {
		line := &cur.WrapLines.Data[i]
		var lineContent float32
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			child := c.wrapChild(cur, offset)
			if child.Exiting {
				continue
			}
			lineContent += wrapAxisSize(child, rows)
		}
		lineContent += float32(max(line.Count-1, 0)) * float32(layoutCfg.ChildGap)
		extraSpace := alongInner - lineContent
		if rows {
			switch layoutCfg.ChildAlignment.X {
			case AlignXLeft:
				extraSpace = 0
			case AlignXCenter:
				extraSpace /= 2
			}
		} else {
			switch layoutCfg.ChildAlignment.Y {
			case AlignYTop:
				extraSpace = 0
			case AlignYCenter:
				extraSpace /= 2
			}
		}
		extraSpace = max(0, extraSpace)
		alongCursor := float32(layoutCfg.Padding.Top)
		if rows {
			alongCursor = float32(layoutCfg.Padding.Left)
		}
		alongCursor += extraSpace
		lineCrossContent := c.wrapLineCrossContent(cur, line, rows)
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			child := c.wrapChild(cur, offset)
			whiteSpace := line.Extent - wrapAxisSize(child, !rows)
			crossOffset := crossCursor
			if rows {
				switch layoutCfg.ChildAlignment.Y {
				case AlignYCenter:
					crossOffset += whiteSpace / 2
				case AlignYBottom:
					crossOffset += whiteSpace
				}
			} else {
				switch layoutCfg.ChildAlignment.X {
				case AlignXCenter:
					crossOffset += whiteSpace / 2
				case AlignXRight:
					crossOffset += whiteSpace
				}
			}
			var childPos Vector2
			if rows {
				childPos = Vector2{X: bbox.X + alongCursor + scrollOffset.X, Y: bbox.Y + crossOffset + scrollOffset.Y}
			} else {
				childPos = Vector2{X: bbox.X + crossOffset + scrollOffset.X, Y: bbox.Y + alongCursor + scrollOffset.Y}
			}
			newNodeIdx := startIdx + childCount - 1 - int(offset)
			dfs[newNodeIdx] = layoutTreeNode{
				element:  child,
				position: childPos,
				nextChildOffset: Vector2{
					X: float32(child.Config.Layout.Padding.Left),
					Y: float32(child.Config.Layout.Padding.Top),
				},
			}
			visited[newNodeIdx] = false
			if !child.Exiting {
				alongCursor += wrapAxisSize(child, rows) + float32(layoutCfg.ChildGap)
			}
		}
		crossCursor += max(line.Extent, lineCrossContent) + float32(layoutCfg.ChildGap)
		alongContent = max(alongContent, lineContent)
		crossContent += lineCrossContent
	}
	crossContent += float32(max(cur.WrapLines.Length-1, 0)) * float32(layoutCfg.ChildGap)
	if scrollData != nil {
		contentSize := Dimensions{Width: crossContent, Height: alongContent}
		if rows {
			contentSize = Dimensions{Width: alongContent, Height: crossContent}
		}
		scrollData.ContentSize = Dimensions{
			Width:  contentSize.Width + float32(layoutCfg.Padding.Left+layoutCfg.Padding.Right),
			Height: contentSize.Height + float32(layoutCfg.Padding.Top+layoutCfg.Padding.Bottom),
		}
	}
	return dfs, visited
}

// emitWrapDividers emits between-children dividers for a wrapping parent.
// Within a line they follow the row/column formula of emitTreeRoot but span
// only the line's band; between lines a divider spans the parent's full size,
// mirroring the other direction's formula. Bands tile the parent: line i runs
// from the previous line's edge plus half a gap to the next line's edge minus
// half a gap, with the outer lines reaching the parent's edges. Mirrors
// Clay__WrapEmitDividers.
func (c *Context) emitWrapDividers(cur *LayoutElement, upBBox BoundingBox, scrollOffset Vector2) {
	layoutCfg := &cur.Config.Layout
	border := &cur.Config.Border
	rows := layoutCfg.LayoutDirection == LeftToRight
	// Integer halving, as upstream does on the uint16 fields.
	halfGap := float32(layoutCfg.ChildGap / 2)
	halfWidth := float32(border.Width.BetweenChildren / 2)
	dividerWidth := float32(border.Width.BetweenChildren)
	crossSize := wrapAxisSize(cur, !rows)
	crossCursor := float32(layoutCfg.Padding.Left)
	if rows {
		crossCursor = float32(layoutCfg.Padding.Top)
	}
	lineCount := cur.WrapLines.Length
	divider := func(box BoundingBox, id uint32) {
		c.emitCommand(RenderCommand{
			BoundingBox: box,
			RenderData:  RenderData{Rectangle: RectangleRenderData{BackgroundColor: border.Color}},
			UserData:    cur.Config.UserData,
			ID:          id,
			CommandType: RenderCommandTypeRectangle,
		})
	}
	for i := range lineCount {
		line := &cur.WrapLines.Data[i]
		advance := c.wrapLineAdvance(cur, line, rows)
		var bandStart float32
		if i > 0 {
			bandStart = crossCursor - halfGap
		}
		bandEnd := crossSize
		if i < lineCount-1 {
			bandEnd = crossCursor + advance + halfGap
		} else if i > 0 {
			// Rigid children can push later lines past the parent's edge; the
			// band then ends at the line's own end so a divider never gets a
			// negative size. A single line keeps upstream's parent-edge band.
			bandEnd = max(bandEnd, crossCursor+advance)
		}
		if i > 0 {
			var box BoundingBox
			if rows {
				box = BoundingBox{X: upBBox.X + scrollOffset.X, Y: upBBox.Y + (crossCursor - halfGap) + scrollOffset.Y - halfWidth, Width: cur.Dimensions.Width, Height: dividerWidth}
			} else {
				box = BoundingBox{X: upBBox.X + (crossCursor - halfGap) + scrollOffset.X - halfWidth, Y: upBBox.Y + scrollOffset.Y, Width: dividerWidth, Height: cur.Dimensions.Height}
			}
			divider(box, HashNumber(uint32(2*int32(cur.Children.Length)+1+i), cur.ID).ID)
		}
		alongOffset := float32(layoutCfg.Padding.Top)
		if rows {
			alongOffset = float32(layoutCfg.Padding.Left)
		}
		alongOffset -= halfGap
		for offset := line.Start; offset < line.Start+line.Count; offset++ {
			child := c.wrapChild(cur, offset)
			if offset > line.Start {
				var box BoundingBox
				if rows {
					box = BoundingBox{X: upBBox.X + alongOffset + scrollOffset.X - halfWidth, Y: upBBox.Y + bandStart + scrollOffset.Y, Width: dividerWidth, Height: bandEnd - bandStart}
				} else {
					box = BoundingBox{X: upBBox.X + bandStart + scrollOffset.X, Y: upBBox.Y + alongOffset + scrollOffset.Y - halfWidth, Width: bandEnd - bandStart, Height: dividerWidth}
				}
				divider(box, HashNumber(uint32(int32(cur.Children.Length)+1+offset), cur.ID).ID)
			}
			alongOffset += wrapAxisSize(child, rows) + float32(layoutCfg.ChildGap)
		}
		crossCursor += advance + float32(layoutCfg.ChildGap)
	}
}
