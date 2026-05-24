package claygo

import "math"

// element.go ports Clay's element open/configure/close lifecycle from
// oracle/clay.h. The functions here run during BeginLayout..EndLayout and
// build the layoutElements tree that the sizing solver consumes, including
// floating roots, clip/scroll containers, and transition registration.

// rootElementIDString is the static name used to seed the auto-created root
// container's ElementID. Matches CLAY_ID("Clay__RootContainer") in upstream
// (oracle/clay.h ~line 4365).
const rootElementIDString = "Clay__RootContainer"

// maxFloat32 is the sentinel "no max" value matching CLAY__MAXFLOAT.
const maxFloat32 float32 = math.MaxFloat32

// openElement appends a new LayoutElement to the elements array, pushes its
// index onto the open-element stack, and registers it in the hashmap with an
// auto-generated id derived from (parent.id, parent.children + floating).
// Mirrors Clay__OpenElement (oracle/clay.h ~line 2041).
func (c *Context) openElement() {
	if c.layoutElements.Length == c.layoutElements.Capacity-1 || c.warnMaxElementsExceeded {
		c.warnMaxElementsExceeded = true
		return
	}
	c.layoutElements.Add(LayoutElement{})
	newIdx := c.layoutElements.Length - 1
	c.openLayoutElementStack.Add(newIdx)

	// Parent is the element two slots down on the stack — BeginLayout pushes
	// the root index twice so this lookup is always safe for user-opened
	// elements.
	parentIdx := c.openLayoutElementStack.GetValue(c.openLayoutElementStack.Length - 2)
	parent := c.layoutElements.Get(parentIdx)
	offset := uint32(parent.Children.Length) + uint32(parent.FloatingChildrenCount)
	elementID := HashNumber(offset, parent.ID)

	newElement := c.layoutElements.Get(newIdx)
	newElement.ID = elementID.ID
	c.addHashMapItem(elementID, newElement)
	c.recordClipAncestor(newIdx)
}

// openElementWithID is openElement but seeds the element with a caller-supplied
// hashed ElementID (e.g. CLAY_ID("Foo")). Mirrors Clay__OpenElementWithId
// (oracle/clay.h ~line 2064).
func (c *Context) openElementWithID(id string) {
	c.openElementWithElementID(HashString(String{Text: id}, 0))
}

func (c *Context) openElementWithElementID(elementID ElementID) {
	if c.layoutElements.Length == c.layoutElements.Capacity-1 || c.warnMaxElementsExceeded {
		c.warnMaxElementsExceeded = true
		return
	}
	c.layoutElements.Add(LayoutElement{ID: elementID.ID})
	newIdx := c.layoutElements.Length - 1
	c.openLayoutElementStack.Add(newIdx)
	c.addHashMapItem(elementID, c.layoutElements.Get(newIdx))
	c.recordClipAncestor(newIdx)
}

// openElementWithIDOffset is openElementWithID seeded with a numeric offset
// for loop iteration. Mirrors CLAY_IDI(id, offset) at oracle/clay.h ~line 88.
func (c *Context) openElementWithIDOffset(id string, offset uint32) {
	if c.layoutElements.Length == c.layoutElements.Capacity-1 || c.warnMaxElementsExceeded {
		c.warnMaxElementsExceeded = true
		return
	}
	elementID := HashStringWithOffset(String{Text: id}, offset, 0)
	c.layoutElements.Add(LayoutElement{ID: elementID.ID})
	newIdx := c.layoutElements.Length - 1
	c.openLayoutElementStack.Add(newIdx)
	c.addHashMapItem(elementID, c.layoutElements.Get(newIdx))
	c.recordClipAncestor(newIdx)
}

// recordClipAncestor stamps layoutElementClipElementIds[idx] with the id of
// the innermost open clip element (top of openClipElementStack), or 0 if
// the element isn't inside a clip. Called by the element-open helpers
// immediately after the new element is pushed.
func (c *Context) recordClipAncestor(idx int32) {
	if idx < 0 || idx >= c.layoutElementClipElementIds.Capacity {
		return
	}
	if c.openClipElementStack.Length > 0 {
		c.layoutElementClipElementIds.Data[idx] = c.openClipElementStack.GetValue(c.openClipElementStack.Length - 1)
	} else {
		c.layoutElementClipElementIds.Data[idx] = 0
	}
}

// openTextElement appends a text-leaf LayoutElement. Text elements have no
// children and don't get a configure/close cycle — they're declared and
// finalized in one call. The unwrapped dimensions are recorded immediately so
// the sizing solver knows how big the leaf wants to be before wrapping.
// Mirrors Clay__OpenTextElement (oracle/clay.h ~line 2083).
func (c *Context) openTextElement(text string, cfg TextElementConfig) {
	if c.layoutElements.Length == c.layoutElements.Capacity-1 || c.warnMaxElementsExceeded {
		c.warnMaxElementsExceeded = true
		return
	}
	parent := c.getOpenLayoutElement()

	c.layoutElements.Add(LayoutElement{
		IsTextElement: true,
		TextConfig:    cfg,
	})
	newIdx := c.layoutElements.Length - 1
	textElement := c.layoutElements.Get(newIdx)
	c.layoutElementChildrenBuffer.Add(newIdx)

	measured := c.measureTextCached(text, &cfg)
	offset := uint32(parent.Children.Length) + uint32(parent.FloatingChildrenCount)
	elementID := HashNumber(offset, parent.ID)
	textElement.ID = elementID.ID
	c.addHashMapItem(elementID, textElement)

	height := measured.UnwrappedDimensions.Height
	if cfg.LineHeight > 0 {
		height = float32(cfg.LineHeight)
	}
	textElement.Dimensions = Dimensions{Width: measured.UnwrappedDimensions.Width, Height: height}
	textElement.MinDimensions = Dimensions{Width: measured.MinWidth, Height: height}
	textElement.TextElementData = TextElementData{
		Text:                String{Text: text},
		PreferredDimensions: measured.UnwrappedDimensions,
	}
	parent.Children.Length++
}

// configureOpenElement applies a declaration to the currently open element.
// Mirrors Clay__ConfigureOpenElementPtr (oracle/clay.h ~line 2112).
func (c *Context) configureOpenElement(decl Decl) {
	openLE := c.getOpenLayoutElement()
	if openLE == nil {
		return
	}
	openLE.Config = decl

	if (decl.Layout.Sizing.Width.Type == SizingTypePercent && decl.Layout.Sizing.Width.Percent > 1) ||
		(decl.Layout.Sizing.Height.Type == SizingTypePercent && decl.Layout.Sizing.Height.Percent > 1) {
		c.reportError(ErrorTypePercentageOver1,
			"An element was configured with SizingPercent, but the provided percentage value was over 1.0. Clay expects a value between 0 and 1, i.e. 20% is 0.2.")
	}

	// Floating element handling. Mirrors Clay__ConfigureOpenElementPtr's
	// floating branch (oracle/clay.h ~line 2130-2170). When an element opts
	// into floating, it becomes its own tree root with a resolved parent id.
	if decl.Floating.AttachTo != AttachToNone {
		floatingCfg := &openLE.Config.Floating
		// Hierarchical parent is the second-to-last entry on the open-element
		// stack (the [root, root, ...] sentinel band makes this always valid).
		var hierarchicalParent *LayoutElement
		if c.openLayoutElementStack.Length >= 2 {
			hierarchicalParent = c.layoutElements.Get(c.openLayoutElementStack.GetValue(c.openLayoutElementStack.Length - 2))
		}
		if hierarchicalParent != nil {
			var clipElementID int32
			switch decl.Floating.AttachTo {
			case AttachToParent:
				floatingCfg.ParentID = hierarchicalParent.ID
				if c.openClipElementStack.Length > 0 {
					clipElementID = c.openClipElementStack.GetValue(c.openClipElementStack.Length - 1)
				}
			case AttachToElementWithID:
				parentItem := c.getHashMapItem(floatingCfg.ParentID)
				if parentItem == nil {
					c.reportError(ErrorTypeFloatingContainerParentNotFound,
						"A floating element was declared with a parentId, but no element with that ID was found this frame. The parent must be declared (via BoxID with that string id) earlier in the same frame, before the floating element opens.")
				} else if parentItem.LayoutElement != nil {
					// Find the parent's index by pointer-walking layoutElements;
					// parentItem.LayoutElement points into that backing array.
					for i := int32(0); i < c.layoutElements.Length; i++ {
						if c.layoutElements.Get(i) == parentItem.LayoutElement {
							if i < c.layoutElementClipElementIds.Length {
								clipElementID = c.layoutElementClipElementIds.Data[i]
							}
							break
						}
					}
				}
			case AttachToRoot:
				floatingCfg.ParentID = HashString(String{Text: rootElementIDString}, 0).ID
			}
			// ClipTo=ClipToNone explicitly opts out of inheriting the
			// ancestor scissor — the floating element renders unclipped.
			if decl.Floating.ClipTo == ClipToNone {
				clipElementID = 0
			}
			currentElementIndex := c.openLayoutElementStack.GetValue(c.openLayoutElementStack.Length - 1)
			// Stamp the floating element's own clip-ancestor entry too, so
			// any children it opens during its closure block inherit the
			// right scissor.
			if currentElementIndex < c.layoutElementClipElementIds.Capacity {
				c.layoutElementClipElementIds.Data[currentElementIndex] = clipElementID
			}
			// Push the resolved clipElementID onto openClipElementStack so
			// closeElement's matching pop (which fires for any floating
			// element OR clip owner) balances. Mirrors C clay.h:2153.
			// Without this push, a floating element nested in a real clip
			// would corrupt the stack on close by popping the real clip's
			// entry instead of its own.
			c.openClipElementStack.Add(clipElementID)
			c.layoutElementTreeRoots.Add(layoutElementTreeRoot{
				LayoutElementIndex: currentElementIndex,
				ParentID:           floatingCfg.ParentID,
				ClipElementID:      uint32(clipElementID),
				ZIndex:             floatingCfg.ZIndex,
			})
		}
	}
	// Clip-container handling. When .clip.Horizontal or .clip.Vertical is
	// set, the element becomes a scissor / scroll boundary: closeElement
	// pops the matching entry off openClipElementStack. The persistent
	// scrollContainerDatas entry tracks scroll position across frames.
	if decl.Clip.Horizontal || decl.Clip.Vertical {
		c.openClipElementStack.Add(int32(openLE.ID))
		c.findOrCreateScrollContainer(openLE)
	}
	// Transition handling. Mirrors the transitions branch of
	// Clay__ConfigureOpenElementPtr (oracle/clay.h ~line 2183-2213): when
	// the user declared a handler, we look up (or allocate) the persistent
	// transitionDataInternal entry keyed on the element id. The advance loop
	// in EndLayout drives the state machine from there.
	if decl.Transition.Handler != nil {
		// Hierarchical parent for the siblingIndex/parentId bookkeeping is
		// the same lookup the floating branch uses: stack[len-2].
		var parent *LayoutElement
		if c.openLayoutElementStack.Length >= 2 {
			parent = c.layoutElements.Get(c.openLayoutElementStack.GetValue(c.openLayoutElementStack.Length - 2))
		}
		if parent != nil {
			c.findOrCreateTransitionData(openLE, parent, &decl)
		}
	}
}

// closeElement finalizes the currently open element: it pulls its children
// from the in-progress children buffer onto the contiguous layoutElementChildren
// arena, computes its fit/min dimensions, clamps to its sizing config, applies
// aspect-ratio adjustment, pops the stack, and registers this element as a
// child of its parent. Mirrors Clay__CloseElement (oracle/clay.h ~line 1867).
func (c *Context) closeElement() {
	if c.warnMaxElementsExceeded {
		return
	}
	openLE := c.getOpenLayoutElement()
	if openLE == nil {
		return
	}
	layoutCfg := &openLE.Config.Layout
	elementHasClipHorizontal := openLE.Config.Clip.Horizontal
	elementHasClipVertical := openLE.Config.Clip.Vertical

	// Pop openClipElementStack for clip-owners or floating roots. Mirrors
	// the C check at oracle/clay.h:1876.
	if elementHasClipHorizontal || elementHasClipVertical ||
		openLE.Config.Floating.AttachTo != AttachToNone {
		if c.openClipElementStack.Length > 0 {
			c.openClipElementStack.Length--
		}
	}

	leftRightPadding := float32(layoutCfg.Padding.Left) + float32(layoutCfg.Padding.Right)
	topBottomPadding := float32(layoutCfg.Padding.Top) + float32(layoutCfg.Padding.Bottom)

	// Snapshot the start of this element's children in the contiguous arena.
	// We splice them off the in-progress childrenBuffer (which acts as a
	// per-depth scratchpad) and append onto layoutElementChildren.
	childCount := openLE.Children.Length
	childrenStart := c.layoutElementChildren.Length

	if layoutCfg.LayoutDirection == LeftToRight {
		openLE.Dimensions.Width = leftRightPadding
		openLE.MinDimensions.Width = leftRightPadding
		for i := int32(0); i < childCount; i++ {
			childIdx := c.layoutElementChildrenBuffer.GetValue(
				c.layoutElementChildrenBuffer.Length - childCount + i)
			child := c.layoutElements.Get(childIdx)
			openLE.Dimensions.Width += child.Dimensions.Width
			if child.Dimensions.Height+topBottomPadding > openLE.Dimensions.Height {
				openLE.Dimensions.Height = child.Dimensions.Height + topBottomPadding
			}
			// Clip containers can shrink below their content on clipped axes.
			if !elementHasClipHorizontal {
				openLE.MinDimensions.Width += child.MinDimensions.Width
			}
			if !elementHasClipVertical && child.MinDimensions.Height+topBottomPadding > openLE.MinDimensions.Height {
				openLE.MinDimensions.Height = child.MinDimensions.Height + topBottomPadding
			}
			c.layoutElementChildren.Add(childIdx)
		}
		childGap := float32(maxInt32(childCount-1, 0)) * float32(layoutCfg.ChildGap)
		openLE.Dimensions.Width += childGap
		if !elementHasClipHorizontal {
			openLE.MinDimensions.Width += childGap
		}
	} else if layoutCfg.LayoutDirection == TopToBottom {
		openLE.Dimensions.Height = topBottomPadding
		openLE.MinDimensions.Height = topBottomPadding
		for i := int32(0); i < childCount; i++ {
			childIdx := c.layoutElementChildrenBuffer.GetValue(
				c.layoutElementChildrenBuffer.Length - childCount + i)
			child := c.layoutElements.Get(childIdx)
			openLE.Dimensions.Height += child.Dimensions.Height
			if child.Dimensions.Width+leftRightPadding > openLE.Dimensions.Width {
				openLE.Dimensions.Width = child.Dimensions.Width + leftRightPadding
			}
			if !elementHasClipVertical {
				openLE.MinDimensions.Height += child.MinDimensions.Height
			}
			if !elementHasClipHorizontal && child.MinDimensions.Width+leftRightPadding > openLE.MinDimensions.Width {
				openLE.MinDimensions.Width = child.MinDimensions.Width + leftRightPadding
			}
			c.layoutElementChildren.Add(childIdx)
		}
		childGap := float32(maxInt32(childCount-1, 0)) * float32(layoutCfg.ChildGap)
		openLE.Dimensions.Height += childGap
		if !elementHasClipVertical {
			openLE.MinDimensions.Height += childGap
		}
	}

	// Children of this element have been committed; the parent's slice view
	// can be wired up. We use the segment [childrenStart, childrenStart+count)
	// of layoutElementChildren as the canonical children list.
	if childCount > 0 {
		openLE.Children.Data = c.layoutElementChildren.Data[childrenStart : childrenStart+childCount]
	}

	c.layoutElementChildrenBuffer.Length -= childCount

	// Clamp to the user-configured sizing bounds. PERCENT axes are deferred:
	// they get sized by the solver based on the parent's final dimensions, so
	// we zero them out here as a placeholder. Mirrors C clay.h:1937-1959.
	if layoutCfg.Sizing.Width.Type != SizingTypePercent {
		if layoutCfg.Sizing.Width.MinMax.Max <= 0 {
			layoutCfg.Sizing.Width.MinMax.Max = maxFloat32
		}
		openLE.Dimensions.Width = clampFloat32(openLE.Dimensions.Width,
			layoutCfg.Sizing.Width.MinMax.Min, layoutCfg.Sizing.Width.MinMax.Max)
		openLE.MinDimensions.Width = clampFloat32(openLE.MinDimensions.Width,
			layoutCfg.Sizing.Width.MinMax.Min, layoutCfg.Sizing.Width.MinMax.Max)
	} else {
		openLE.Dimensions.Width = 0
	}

	if layoutCfg.Sizing.Height.Type != SizingTypePercent {
		if layoutCfg.Sizing.Height.MinMax.Max <= 0 {
			layoutCfg.Sizing.Height.MinMax.Max = maxFloat32
		}
		openLE.Dimensions.Height = clampFloat32(openLE.Dimensions.Height,
			layoutCfg.Sizing.Height.MinMax.Min, layoutCfg.Sizing.Height.MinMax.Max)
		openLE.MinDimensions.Height = clampFloat32(openLE.MinDimensions.Height,
			layoutCfg.Sizing.Height.MinMax.Min, layoutCfg.Sizing.Height.MinMax.Max)
	} else {
		openLE.Dimensions.Height = 0
	}

	updateAspectRatioBox(openLE)

	isFloating := openLE.Config.Floating.AttachTo != AttachToNone

	// Pop this element off the open stack; figure out what parent it now
	// reports its existence to.
	closingElementIndex := c.openLayoutElementStack.RemoveSwapback(c.openLayoutElementStack.Length - 1)
	openLE = c.getOpenLayoutElement()

	// openLayoutElementStack.Length > 1 means we're inside the [root, root,
	// ...] sentinel band and have a real parent waiting for this child.
	// For the outermost EndLayout close (root closing root), Length == 1.
	if c.openLayoutElementStack.Length > 1 && openLE != nil {
		if isFloating {
			openLE.FloatingChildrenCount++
			return
		}
		openLE.Children.Length++
		c.layoutElementChildrenBuffer.Add(closingElementIndex)
	}
}

// getOpenLayoutElement returns a pointer to the currently open LayoutElement
// (top of the openLayoutElementStack). Mirrors Clay__GetOpenLayoutElement
// (oracle/clay.h ~line 1406). Returns nil only when the stack is empty.
func (c *Context) getOpenLayoutElement() *LayoutElement {
	if c.openLayoutElementStack.Length <= 0 {
		return nil
	}
	idx := c.openLayoutElementStack.GetValue(c.openLayoutElementStack.Length - 1)
	return c.layoutElements.Get(idx)
}

// updateAspectRatioBox adjusts an element's width or height to honor its
// aspectRatio configuration when exactly one dimension has been set. Mirrors
// Clay__UpdateAspectRatioBox (oracle/clay.h ~line 1857).
func updateAspectRatioBox(le *LayoutElement) {
	ar := le.Config.AspectRatio.AspectRatio
	if ar == 0 {
		return
	}
	if le.IsTextElement {
		return
	}
	if le.Dimensions.Width == 0 && le.Dimensions.Height != 0 {
		le.Dimensions.Width = le.Dimensions.Height * ar
	} else if le.Dimensions.Width != 0 && le.Dimensions.Height == 0 {
		le.Dimensions.Height = le.Dimensions.Width * (1 / ar)
	}
}

// maxInt32 / clampFloat32 are tiny helpers used by closeElement. Kept here
// rather than in a generic utility file because this is the only consumer.
func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}
