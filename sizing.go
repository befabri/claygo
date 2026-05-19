package claygo

// sizing.go ports Clay__SizeContainersAlongAxis (oracle/clay.h ~line 2281).
// This is the heart of the layout solver: it walks the element tree breadth-
// first and resolves each child's dimension on one axis (x or y), distributing
// the parent's leftover space among GROW children and shrinking the largest
// resizable child first when content overflows.
//
// Run sizeContainersAlongAxis(true) then sizeContainersAlongAxis(false) to
// fully size the tree.
//
// What is intentionally skipped (deferred to later port waves):
//   - layoutElementTreeRoots: we currently have exactly one root (index 0).
//     Floating-attached subtrees will add additional roots; until then the
//     solver iterates a single hardcoded root.
//   - Clip-container fast paths (parent.clip.horizontal/vertical): these
//     toggle whether children get compressed or scroll-overflow. Treated as
//     always-false for now.
//   - The textElementsOut and aspectRatioElementsOut callback-buffers used
//     by the wrapping and aspect-ratio passes are not used here yet.

const epsilon float32 = 0.01

// sizeContainersAlongAxis runs one pass of Clay's per-axis sizing solver.
// xAxis=true sizes widths; xAxis=false sizes heights. When xAxis=true, every
// text leaf encountered is also pushed onto c.textElements so the wrap pass
// can find them.
func (c *Context) sizeContainersAlongAxis(xAxis bool) {
	// Reuse openLayoutElementStack and layoutElementChildrenBuffer as scratch
	// arrays during the solver, matching C. Both have already been cleared by
	// closeElement when the layout finished building; reset their lengths
	// defensively in case future waves leave them dirty.
	bfs := &c.layoutElementChildrenBuffer
	resizable := &c.openLayoutElementStack

	if xAxis {
		c.textElements.Length = 0
	}

	// Iterate every tree root: the auto-root (index 0) plus any floating
	// element registered as its own root by configureOpenElement.
	for rootIdx := int32(0); rootIdx < c.layoutElementTreeRoots.Length; rootIdx++ {
		bfs.Length = 0
		treeRoot := c.layoutElementTreeRoots.Get(rootIdx)
		root := c.layoutElements.Get(treeRoot.LayoutElementIndex)
		bfs.Add(treeRoot.LayoutElementIndex)

		// Floating tree roots are sized relative to their attachment parent:
		// GROW takes the parent's full size on that axis, PERCENT scales it.
		// FIXED and FIT stay as declared. Mirrors clay.h:2292-2317.
		if root.Config.Floating.AttachTo != AttachToNone {
			parentItem := c.getHashMapItem(root.Config.Floating.ParentID)
			if parentItem != nil && parentItem.LayoutElement != nil {
				parentLE := parentItem.LayoutElement
				switch root.Config.Layout.Sizing.Width.Type {
				case SizingTypeGrow:
					root.Dimensions.Width = parentLE.Dimensions.Width
				case SizingTypePercent:
					root.Dimensions.Width = parentLE.Dimensions.Width * root.Config.Layout.Sizing.Width.Percent
				}
				switch root.Config.Layout.Sizing.Height.Type {
				case SizingTypeGrow:
					root.Dimensions.Height = parentLE.Dimensions.Height
				case SizingTypePercent:
					root.Dimensions.Height = parentLE.Dimensions.Height * root.Config.Layout.Sizing.Height.Percent
				}
			}
		}

		// Clamp root to its own sizing min/max if not PERCENT.
		if root.Config.Layout.Sizing.Width.Type != SizingTypePercent {
			root.Dimensions.Width = clampFloat32(root.Dimensions.Width,
				root.Config.Layout.Sizing.Width.MinMax.Min,
				root.Config.Layout.Sizing.Width.MinMax.Max)
		}
		if root.Config.Layout.Sizing.Height.Type != SizingTypePercent {
			root.Dimensions.Height = clampFloat32(root.Dimensions.Height,
				root.Config.Layout.Sizing.Height.MinMax.Min,
				root.Config.Layout.Sizing.Height.MinMax.Max)
		}

		c.sizeOneRoot(xAxis, bfs, resizable)
	}
}

// sizeOneRoot is the per-root BFS body, factored out so the main entrypoint
// can iterate every tree root without indenting the whole body two extra
// levels. The bfs buffer must be seeded with the root index.
func (c *Context) sizeOneRoot(xAxis bool, bfs, resizable *Array[int32]) {
	for i := int32(0); i < bfs.Length; i++ {
		parentIdx := bfs.GetValue(i)
		parent := c.layoutElements.Get(parentIdx)
		parentLayoutCfg := &parent.Config.Layout
		growContainerCount := int32(0)
		parentSize := parent.Dimensions.Width
		var parentPadding float32 = float32(parentLayoutCfg.Padding.Left) + float32(parentLayoutCfg.Padding.Right)
		if !xAxis {
			parentSize = parent.Dimensions.Height
			parentPadding = float32(parentLayoutCfg.Padding.Top) + float32(parentLayoutCfg.Padding.Bottom)
		}
		var innerContentSize float32
		totalPaddingAndChildGaps := parentPadding
		sizingAlongAxis := (xAxis && parentLayoutCfg.LayoutDirection == LeftToRight) ||
			(!xAxis && parentLayoutCfg.LayoutDirection == TopToBottom)
		resizable.Length = 0
		parentChildGap := float32(parentLayoutCfg.ChildGap)
		isFirstChild := true

		for childOffset := int32(0); childOffset < parent.Children.Length; childOffset++ {
			childIdx := parent.Children.Data[childOffset]
			child := c.layoutElements.Get(childIdx)
			childSizing := getElementSizing(child, xAxis)
			var childSize float32 = child.Dimensions.Width
			if !xAxis {
				childSize = child.Dimensions.Height
			}

			// BFS descent: text leaves and aspect-ratio elements are handled
			// elsewhere; everything else with children gets visited later.
			if !child.IsTextElement && child.Children.Length > 0 {
				bfs.Add(childIdx)
			}
			// Collect text leaves on the x-axis pass so the wrap pass can
			// iterate them with parent widths now known.
			if xAxis && child.IsTextElement {
				c.textElements.Add(childIdx)
			}

			// Resizable set: anything that's not FIXED/PERCENT and not a
			// non-wrapping text leaf. Text with WRAP_WORDS is allowed because
			// the wrapping pass may need to flow into a smaller width.
			isWrappingText := child.IsTextElement && child.TextConfig.WrapMode == TextWrapWords
			if childSizing.Type != SizingTypePercent && childSizing.Type != SizingTypeFixed &&
				(!child.IsTextElement || isWrappingText) {
				resizable.Add(childIdx)
			}

			if sizingAlongAxis {
				if childSizing.Type != SizingTypePercent {
					innerContentSize += childSize
				}
				if childSizing.Type == SizingTypeGrow {
					growContainerCount++
				}
				if !isFirstChild {
					innerContentSize += parentChildGap
					totalPaddingAndChildGaps += parentChildGap
				}
			} else if childSize > innerContentSize {
				innerContentSize = childSize
			}
			isFirstChild = false
		}

		// Resolve PERCENT children now that we know the parent's total
		// padding+gap consumption.
		for childOffset := int32(0); childOffset < parent.Children.Length; childOffset++ {
			childIdx := parent.Children.Data[childOffset]
			child := c.layoutElements.Get(childIdx)
			childSizing := getElementSizing(child, xAxis)
			if childSizing.Type != SizingTypePercent {
				continue
			}
			pct := childSizing.Percent
			val := (parentSize - totalPaddingAndChildGaps) * pct
			if xAxis {
				child.Dimensions.Width = val
			} else {
				child.Dimensions.Height = val
			}
			if sizingAlongAxis {
				innerContentSize += val
			}
			updateAspectRatioBox(child)
		}

		if sizingAlongAxis {
			sizeToDistribute := parentSize - parentPadding - innerContentSize
			switch {
			case sizeToDistribute < 0:
				// Clip containers don't compress their content — overflow
				// is what they're for. Matches C clay.h:2407-2410: when the
				// parent clips on this axis, leave children at their full
				// preferred sizes and let SCISSOR_START hide the overflow.
				if (xAxis && parent.Config.Clip.Horizontal) ||
					(!xAxis && parent.Config.Clip.Vertical) {
					break
				}
				// Otherwise shrink the largest resizable children toward
				// their minDimensions, taking equal-sized peers together.
				shrinkOverflow(c, &sizeToDistribute, resizable, xAxis)
			case sizeToDistribute > 0 && growContainerCount > 0:
				// Underflow with GROW siblings: grow smallest first.
				growUnderflow(c, &sizeToDistribute, resizable, xAxis)
			}
		} else {
			// Cross-axis sizing: GROW children expand to parent inner size,
			// clamped to their own max; everything else stays put but is
			// clamped to (minDim, parent inner size).
			maxSize := parentSize - parentPadding
			for childOffset := int32(0); childOffset < resizable.Length; childOffset++ {
				childIdx := resizable.GetValue(childOffset)
				child := c.layoutElements.Get(childIdx)
				childSizing := getElementSizing(child, xAxis)
				var minSize float32 = child.MinDimensions.Width
				var sizePtr *float32 = &child.Dimensions.Width
				if !xAxis {
					minSize = child.MinDimensions.Height
					sizePtr = &child.Dimensions.Height
				}
				if childSizing.Type == SizingTypeGrow {
					m := maxSize
					if childSizing.MinMax.Max > 0 && childSizing.MinMax.Max < m {
						m = childSizing.MinMax.Max
					}
					*sizePtr = m
				}
				if *sizePtr > maxSize {
					*sizePtr = maxSize
				}
				if *sizePtr < minSize {
					*sizePtr = minSize
				}
			}
		}
	}
}

// shrinkOverflow distributes a negative sizeToDistribute among resizable
// children: it repeatedly picks the LARGEST current child and shrinks it
// (along with any same-size peers) toward the second-largest, stopping when
// the deficit is consumed or every child has hit its MinDimensions.
//
// The buffer mutation inside the inner loop matches C's `--childIndex` trick
// (RemoveSwapback shrinks the slice, so we reuse the slot just produced).
func shrinkOverflow(c *Context, sizeToDistribute *float32, resizable *Array[int32], xAxis bool) {
	for *sizeToDistribute < -epsilon && resizable.Length > 0 {
		var largest, secondLargest float32
		widthToAdd := *sizeToDistribute
		for ci := int32(0); ci < resizable.Length; ci++ {
			child := c.layoutElements.Get(resizable.GetValue(ci))
			var cs float32 = child.Dimensions.Width
			if !xAxis {
				cs = child.Dimensions.Height
			}
			if floatEqual(cs, largest) {
				continue
			}
			if cs > largest {
				secondLargest = largest
				largest = cs
			}
			if cs < largest {
				if cs > secondLargest {
					secondLargest = cs
				}
				widthToAdd = secondLargest - largest
			}
		}
		// Bound by the per-child share so we never overshoot on the last step.
		share := *sizeToDistribute / float32(resizable.Length)
		if share > widthToAdd {
			widthToAdd = share
		}

		ci := int32(0)
		for ci < resizable.Length {
			child := c.layoutElements.Get(resizable.GetValue(ci))
			var sizePtr *float32 = &child.Dimensions.Width
			var minSize float32 = child.MinDimensions.Width
			if !xAxis {
				sizePtr = &child.Dimensions.Height
				minSize = child.MinDimensions.Height
			}
			previous := *sizePtr
			if floatEqual(*sizePtr, largest) {
				*sizePtr += widthToAdd
				if *sizePtr <= minSize {
					*sizePtr = minSize
					resizable.RemoveSwapback(ci)
					*sizeToDistribute -= (*sizePtr - previous)
					continue
				}
				*sizeToDistribute -= (*sizePtr - previous)
			}
			ci++
		}
	}
}

// growUnderflow distributes a positive sizeToDistribute among GROW children
// in the resizable buffer. Non-GROW resizables are removed first (they don't
// expand). Then GROW children grow smallest-first toward second-smallest,
// honoring per-child Max constraints by removing children that hit their cap.
func growUnderflow(c *Context, sizeToDistribute *float32, resizable *Array[int32], xAxis bool) {
	// Filter resizable down to only GROW children.
	ci := int32(0)
	for ci < resizable.Length {
		child := c.layoutElements.Get(resizable.GetValue(ci))
		if getElementSizing(child, xAxis).Type != SizingTypeGrow {
			resizable.RemoveSwapback(ci)
			continue
		}
		ci++
	}

	for *sizeToDistribute > epsilon && resizable.Length > 0 {
		smallest := maxFloat32
		secondSmallest := maxFloat32
		widthToAdd := *sizeToDistribute
		for ci := int32(0); ci < resizable.Length; ci++ {
			child := c.layoutElements.Get(resizable.GetValue(ci))
			var cs float32 = child.Dimensions.Width
			if !xAxis {
				cs = child.Dimensions.Height
			}
			if floatEqual(cs, smallest) {
				continue
			}
			if cs < smallest {
				secondSmallest = smallest
				smallest = cs
			}
			if cs > smallest {
				if cs < secondSmallest {
					secondSmallest = cs
				}
				widthToAdd = secondSmallest - smallest
			}
		}
		share := *sizeToDistribute / float32(resizable.Length)
		if share < widthToAdd {
			widthToAdd = share
		}

		ci := int32(0)
		for ci < resizable.Length {
			child := c.layoutElements.Get(resizable.GetValue(ci))
			childSizing := getElementSizing(child, xAxis)
			var sizePtr *float32 = &child.Dimensions.Width
			if !xAxis {
				sizePtr = &child.Dimensions.Height
			}
			previous := *sizePtr
			maxSize := childSizing.MinMax.Max
			if floatEqual(*sizePtr, smallest) {
				*sizePtr += widthToAdd
				if maxSize > 0 && *sizePtr >= maxSize {
					*sizePtr = maxSize
					resizable.RemoveSwapback(ci)
					*sizeToDistribute -= (*sizePtr - previous)
					continue
				}
				*sizeToDistribute -= (*sizePtr - previous)
			}
			ci++
		}
	}
}

// getElementSizing mirrors Clay__GetElementSizing: text elements report a
// zero SizingAxis (Fit + min=max=0); everything else reports its width or
// height axis from its layout config.
func getElementSizing(le *LayoutElement, xAxis bool) SizingAxis {
	if le.IsTextElement {
		return SizingAxis{}
	}
	if xAxis {
		return le.Config.Layout.Sizing.Width
	}
	return le.Config.Layout.Sizing.Height
}

// floatEqual matches Clay__FloatEqual: within epsilon (0.01).
func floatEqual(a, b float32) bool {
	d := a - b
	return d < epsilon && d > -epsilon
}
