package claygo

// interact.go ports Clay's pointer / hover / element-lookup API:
// SetPointerState recomputes which elements the pointer is over and fires
// OnHover callbacks; the Hovered / PointerOver / GetPointerOverIds /
// GetElementData queries read that state back. Each function documents itself
// below.
//
// Mirrors Clay_SetPointerState (oracle/clay.h ~line 4084), Clay_Hovered
// (~4808), Clay_PointerOver (~4834), Clay_OnHover (~4822), and the inline
// PointIsInsideRect helper (~1780).

// pointIsInsideRect returns true if a point lies inside the closed rectangle.
// Matches Clay__PointIsInsideRect (oracle/clay.h ~line 1780).
func pointIsInsideRect(p Vector2, r BoundingBox) bool {
	return p.X >= r.X && p.X <= r.X+r.Width && p.Y >= r.Y && p.Y <= r.Y+r.Height
}

// SetPointerState updates the pointer position + button state, recomputes
// pointerOverIds for the current frame, advances the press/release state
// machine, and fires any registered OnHover callbacks. Should be called once
// per frame BEFORE BeginLayout so Hovered() during element declaration sees
// fresh pointer-over data from the previous frame's bounding boxes.
//
// Mirrors Clay_SetPointerState (oracle/clay.h ~line 4084).
func (c *Context) SetPointerState(pos Vector2, isDown bool) {
	if c.warnMaxElementsExceeded {
		return
	}
	c.pointerPosition = pos
	c.pointerDown = isDown
	c.pointerData.Position = pos
	c.pointerOverIds.Length = 0

	// Walk every tree root in REVERSE z-order so top-most elements appear
	// first in the resulting list (matches C). Capturing floating roots stop
	// hit-testing lower roots; passthrough roots allow the scan to continue.
	for i := c.layoutElementTreeRoots.Length - 1; i >= 0; i-- {
		treeRoot := c.layoutElementTreeRoots.Get(i)
		found := c.collectPointerOver(treeRoot.LayoutElementIndex)
		rootElement := c.layoutElements.Get(treeRoot.LayoutElementIndex)
		if found && rootElement.Config.Floating.AttachTo != AttachToNone &&
			rootElement.Config.Floating.PointerCaptureMode == PointerCaptureModeCapture {
			break
		}
	}

	// Transition the press/release state machine.
	prev := c.pointerData.State
	switch {
	case isDown && (prev == PointerDataPressed || prev == PointerDataPressedThisFrame):
		c.pointerData.State = PointerDataPressed
	case isDown:
		c.pointerData.State = PointerDataPressedThisFrame
	case !isDown && (prev == PointerDataReleased || prev == PointerDataReleasedThisFrame):
		c.pointerData.State = PointerDataReleased
	default:
		c.pointerData.State = PointerDataReleasedThisFrame
	}
}

// collectPointerOver does an iterative DFS from rootIdx, appending every
// element whose bbox contains the pointer position to c.pointerOverIds. It
// returns true when any element in the tree was hit.
func (c *Context) collectPointerOver(rootIdx int32) bool {
	// Scratch stack uses a Go slice rather than the arena buffers, which are
	// still holding solver state at this point.
	stack := []int32{rootIdx}
	found := false
	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		element := c.layoutElements.Get(idx)
		item := c.getHashMapItem(element.ID)
		if item == nil {
			continue
		}
		if c.skipPointerTree(element) {
			continue
		}
		clipElementID := int32(0)
		if idx >= 0 && idx < c.layoutElementClipElementIds.Capacity {
			clipElementID = c.layoutElementClipElementIds.Data[idx]
		}
		clipAllowsHit := clipElementID == 0 || c.externalScrollHandlingEnabled
		if !clipAllowsHit {
			if clipItem := c.getHashMapItem(uint32(clipElementID)); clipItem != nil {
				clipAllowsHit = pointIsInsideRect(c.pointerPosition, clipItem.BoundingBox)
			}
		}
		if pointIsInsideRect(c.pointerPosition, item.BoundingBox) && clipAllowsHit {
			if item.OnHoverFunction != nil {
				item.OnHoverFunction(item.ElementID, c.pointerData, item.HoverFunctionUserData)
			}
			c.pointerOverIds.Add(item.ElementID)
			found = true
		}

		// Descend into children. Order doesn't matter for hit-testing
		// purposes; deeper hits land later in the list within a tree, and
		// REVERSE tree-root order handles z-stacking at the root level.
		for i := element.Children.Length; i > 0; i-- {
			stack = append(stack, element.Children.Data[i-1])
		}
	}
	return found
}

func (c *Context) skipPointerTree(element *LayoutElement) bool {
	if element.IsTextElement || element.Config.Transition.Handler == nil {
		return false
	}
	for i := int32(0); i < c.transitionDatas.Length; i++ {
		data := c.transitionDatas.Get(i)
		if data.ElementID != element.ID {
			continue
		}
		switch element.Config.Transition.InteractionHandling {
		case TransitionDisableInteractionsWhileTransitioningPosition:
			return data.State == TransitionStateExiting ||
				data.State == TransitionStateEntering ||
				(data.State == TransitionStateTransitioning && data.ActiveProperties&TransitionPropertyPosition != 0)
		case TransitionAllowInteractionsWhileTransitioningPosition:
			return data.State == TransitionStateExiting
		}
	}
	return false
}

// PointerState returns the current pointer position and interaction state.
func (c *Context) PointerState() PointerData { return c.pointerData }

// PointerOver returns true if the pointer is inside the bounding box of the
// element with the given id, using bboxes recorded during the previous
// EndLayout. Mirrors Clay_PointerOver (oracle/clay.h ~line 4834).
func (c *Context) PointerOver(id ElementID) bool {
	for i := int32(0); i < c.pointerOverIds.Length; i++ {
		if c.pointerOverIds.GetValue(i).ID == id.ID {
			return true
		}
	}
	return false
}

// Hovered reports whether the pointer is over the currently-open layout
// element. Intended to be called inside a Box closure so the user can
// branch on hover state when configuring an element. Uses the previous
// frame's bounding box — fresh-this-frame bboxes aren't computed until
// EndLayout. Mirrors Clay_Hovered (oracle/clay.h ~line 4808).
func (c *Context) Hovered() bool {
	if c.warnMaxElementsExceeded {
		return false
	}
	openLE := c.getOpenLayoutElement()
	if openLE == nil {
		return false
	}
	for i := int32(0); i < c.pointerOverIds.Length; i++ {
		if c.pointerOverIds.GetValue(i).ID == openLE.ID {
			return true
		}
	}
	return false
}

// OnHover registers a callback fired by SetPointerState whenever the
// pointer is over the currently-open element. The callback receives the
// element's id, current PointerData (position + press state), and the
// caller-supplied userData. Replaces any prior OnHover on the same element
// in the same frame.
//
// Mirrors Clay_OnHover (oracle/clay.h ~line 4822).
func (c *Context) OnHover(fn HoverHandler, userData any) {
	openLE := c.getOpenLayoutElement()
	if openLE == nil {
		return
	}
	item := c.getHashMapItem(openLE.ID)
	if item == nil {
		return
	}
	item.OnHoverFunction = fn
	item.HoverFunctionUserData = userData
}

// GetPointerOverIds returns a snapshot of all element ids the pointer is
// currently over. The returned slice is owned by the Context and is
// overwritten on the next SetPointerState call; callers that need to
// retain the snapshot should copy it.
//
// Mirrors Clay_GetPointerOverIds (oracle/clay.h ~line 3194).
func (c *Context) GetPointerOverIds() []ElementID {
	return c.pointerOverIds.Data[:c.pointerOverIds.Length]
}

// GetElementData returns the most recent bounding box for an element by id,
// along with a Found flag. If the id was never declared this frame (or in
// the previous frame), Found is false and BoundingBox is the zero value.
//
// Mirrors Clay_GetElementData (oracle/clay.h ~line 4866).
func (c *Context) GetElementData(id ElementID) ElementData {
	item := c.getHashMapItem(id.ID)
	if item == nil {
		return ElementData{}
	}
	return ElementData{BoundingBox: item.BoundingBox, Found: true}
}

// GetElementID is a thin wrapper that hashes a Go string into an ElementID
// using the same seed (0) as the C upstream's CLAY_ID macro. Useful when
// the caller wants to look up an element they declared via BoxID.
//
// Mirrors Clay_GetElementId (oracle/clay.h ~line 4799).
func GetElementID(s string) ElementID {
	return HashString(String{Text: s}, 0)
}

// GetElementIDWithIndex returns the ElementID equivalent of CLAY_IDI(s, i):
// HashStringWithOffset folding a numeric index into the hash for
// loop-stable IDs without pre-formatting the string.
//
// Mirrors Clay_GetElementIdWithIndex (oracle/clay.h ~line 4804).
func GetElementIDWithIndex(s string, index uint32) ElementID {
	return HashStringWithOffset(String{Text: s}, index, 0)
}

// GetElementIDLocal is the ElementID equivalent of CLAY_ID_LOCAL(s): it hashes
// using the currently open element id on the current Context as the seed.
func GetElementIDLocal(s string) ElementID {
	if currentContext == nil {
		return HashString(String{Text: s}, 0)
	}
	return currentContext.GetElementIDLocal(s)
}

// GetElementIDWithIndexLocal is the ElementID equivalent of
// CLAY_IDI_LOCAL(s, i): it folds index into a hash seeded by the currently open
// element id on the current Context.
func GetElementIDWithIndexLocal(s string, index uint32) ElementID {
	if currentContext == nil {
		return HashStringWithOffset(String{Text: s}, index, 0)
	}
	return currentContext.GetElementIDWithIndexLocal(s, index)
}

// GetElementIDLocal hashes s using this Context's currently open element id as
// the seed.
func (c *Context) GetElementIDLocal(s string) ElementID {
	seed := c.GetOpenElementID().ID
	return HashString(String{Text: s}, seed)
}

// GetElementIDWithIndexLocal hashes s and index using this Context's currently
// open element id as the seed.
func (c *Context) GetElementIDWithIndexLocal(s string, index uint32) ElementID {
	seed := c.GetOpenElementID().ID
	return HashStringWithOffset(String{Text: s}, index, seed)
}

// GetOpenElementID returns the id of the element currently being
// configured between BoxID's open and close (typically inside the
// children closure passed to Box / BoxID / BoxIDOffset). Useful for
// CLAY_ID_LOCAL-style derived ids that fold the parent id into a child
// element's seed.
//
// Returns zero ElementID when called outside of a frame or when no
// element is open. Mirrors Clay_GetOpenElementId (oracle/clay.h ~line 4794).
func (c *Context) GetOpenElementID() ElementID {
	le := c.getOpenLayoutElement()
	if le == nil {
		return ElementID{}
	}
	return ElementID{ID: le.ID, BaseID: le.ID}
}
