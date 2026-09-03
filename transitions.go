package claygo

// transitions.go ports Clay's transition state machine from oracle/clay.h.
// Each element with Decl.Transition.Handler != nil gets a persistent
// transitionDataInternal entry that survives across frames; the machine
// compares last frame's resolved bbox to this frame's, runs the
// enter/transitioning/exit phases, and calls the user's handler each tick to
// interpolate into currentState, which the render pass reads back via
// applyStoredTransitionToBoundingBox to override the laid-out bbox.
//
// The pieces map to upstream as: register/refresh in configureOpenElement
// (Clay__ConfigureOpenElementPtr), the useStoredBoundingBoxes override in
// applyStoredTransitionToBoundingBox, the advance loop in advanceTransitions
// (Clay_EndLayout), and exit cloning in cloneElementsWithExitTransition. Each
// function below carries its own oracle line references.

// maxTransitionDatas is the persistent capacity of Context.transitionDatas.
// Matches upstream's hard-coded 200 (oracle/clay.h ~line 2252).
const maxTransitionDatas int32 = 200

// transitionDataInternal is the per-element cross-frame transition state.
// Mirrors Clay__TransitionDataInternal (oracle/clay.h ~line 1251).
type transitionDataInternal struct {
	InitialState     TransitionData
	CurrentState     TransitionData
	TargetState      TransitionData
	ElementThisFrame *LayoutElement
	// ElementSnapshotSubtree keeps the last completed frame's element subtree
	// (root at index 0, children as relative indices) outside the ephemeral
	// layout arena, so exit transitions still clone the right elements after
	// BeginLayout reuses arena slots for the next frame.
	ElementSnapshotSubtree    []LayoutElement
	OldParentRelativePosition Vector2
	ElementID                 uint32
	ParentID                  uint32
	SiblingIndex              int32
	ElapsedTime               float32
	State                     TransitionState
	TransitionOut             bool
	Reparented                bool
	ActiveProperties          TransitionProperty
}

// findOrCreateTransitionData is the helper used by configureOpenElement to
// look up (or allocate) the persistent state for the currently-open element.
// Mirrors the loop + Add at oracle/clay.h ~line 2186-2212.
func (c *Context) findOrCreateTransitionData(openLE *LayoutElement, parent *LayoutElement, decl *Decl) {
	for i := range c.transitionDatas.Length {
		existing := c.transitionDatas.Get(i)
		if existing.ElementID != openLE.ID {
			continue
		}
		if existing.State == TransitionStateExiting {
			existing.State = TransitionStateIdle
			if item := c.getHashMapItem(openLE.ID); item != nil {
				item.AppearedThisFrame = false
			}
		}
		existing.ElementThisFrame = openLE
		if existing.ParentID != parent.ID {
			existing.Reparented = true
		}
		existing.ParentID = parent.ID
		existing.SiblingIndex = parent.Children.Length
		existing.TransitionOut = decl.Transition.Exit.SetFinalState != nil
		return
	}
	c.transitionDatas.Add(transitionDataInternal{
		ElementThisFrame: openLE,
		ElementID:        openLE.ID,
		ParentID:         parent.ID,
		SiblingIndex:     parent.Children.Length,
		TransitionOut:    decl.Transition.Exit.SetFinalState != nil,
	})
}

// applyStoredTransitionToBoundingBox is the read-side override called during
// final-layout DFS for elements with a transition handler. If the element's
// state machine is currently mid-transition, the per-property active mask
// dictates which axes of `bbox` are replaced with currentState values.
//
// Returns (skipSubtree=true) when the element is in an exit transition that
// has finished (no transitionDatas entry remains), matching the C version's
// "exiting element that completed this frame" branch at oracle/clay.h ~line
// 2933-2937.
//
// Mirrors the inner conditional at oracle/clay.h ~line 2918-2938.
func (c *Context) applyStoredTransitionToBoundingBox(element *LayoutElement, bbox *BoundingBox) (skipSubtree bool) {
	if element.Config.Transition.Handler == nil {
		return false
	}
	for j := range c.transitionDatas.Length {
		td := c.transitionDatas.Get(j)
		if td.ElementID != element.ID {
			continue
		}
		if td.State != TransitionStateIdle {
			ap := td.ActiveProperties
			cs := td.CurrentState.BoundingBox
			if ap&TransitionPropertyX != 0 {
				bbox.X = cs.X
			}
			if ap&TransitionPropertyY != 0 {
				bbox.Y = cs.Y
			}
			if ap&TransitionPropertyWidth != 0 {
				bbox.Width = cs.Width
			}
			if ap&TransitionPropertyHeight != 0 {
				bbox.Height = cs.Height
			}
		}
		return false
	}
	// No entry remained: an exiting element that completed its transition.
	if element.Config.Transition.Exit.SetFinalState != nil {
		return true
	}
	return false
}

// applyTransitionedPropertiesToElement writes the (just-computed) currentState
// of a transition back onto a real LayoutElement and/or its hashmap bbox.
// Mirrors Clay_ApplyTransitionedPropertiesToElement (oracle/clay.h ~line 4410).
func applyTransitionedPropertiesToElement(currentElement *LayoutElement, properties TransitionProperty, data TransitionData, bbox *BoundingBox, reparented bool) {
	if currentElement == nil {
		return
	}
	if properties&TransitionPropertyWidth != 0 {
		if !reparented {
			currentElement.Dimensions.Width = data.BoundingBox.Width
			currentElement.Config.Layout.Sizing.Width = SizingFixed(data.BoundingBox.Width)
		} else if bbox != nil {
			bbox.Width = data.BoundingBox.Width
		}
	}
	if properties&TransitionPropertyHeight != 0 {
		if !reparented {
			currentElement.Dimensions.Height = data.BoundingBox.Height
			currentElement.Config.Layout.Sizing.Height = SizingFixed(data.BoundingBox.Height)
		} else if bbox != nil {
			bbox.Height = data.BoundingBox.Height
		}
	}
	if bbox != nil {
		if properties&TransitionPropertyX != 0 {
			bbox.X = data.BoundingBox.X
		}
		if properties&TransitionPropertyY != 0 {
			bbox.Y = data.BoundingBox.Y
		}
	}
	if properties&TransitionPropertyOverlayColor != 0 {
		currentElement.Config.OverlayColor = data.OverlayColor
	}
	if properties&TransitionPropertyBackgroundColor != 0 {
		currentElement.Config.BackgroundColor = data.BackgroundColor
	}
	if properties&TransitionPropertyBorderColor != 0 {
		currentElement.Config.Border.Color = data.BorderColor
	}
	if properties&TransitionPropertyBorderWidth != 0 {
		currentElement.Config.Border.Width = data.BorderWidth
	}
}

// colorEqual is the Color analogue of floatEqual, used by the per-property
// activation check below.
func colorEqual(a, b Color) bool {
	return floatEqual(a.R, b.R) && floatEqual(a.G, b.G) && floatEqual(a.B, b.B) && floatEqual(a.A, b.A)
}

// borderWidthEqual compares two BorderWidth structs componentwise.
func borderWidthEqual(a, b BorderWidth) bool {
	return a.Left == b.Left && a.Right == b.Right && a.Top == b.Top && a.Bottom == b.Bottom && a.BetweenChildren == b.BetweenChildren
}

// pruneDeadTransitions removes transitionDatas entries that are no longer
// reachable: elements that didn't re-declare this frame and don't have an
// exit transition. Mirrors the first loop in Clay_EndLayout
// (oracle/clay.h ~line 4459-4470). Called from EndLayout before
// calculateFinalLayout because the final-layout pass needs to know which
// transitions are still live.
func (c *Context) pruneDeadTransitions() {
	for i := int32(0); i < c.transitionDatas.Length; i++ {
		data := c.transitionDatas.Get(i)
		item := c.getHashMapItem(data.ElementID)
		// Element didn't appear this frame (generation <= current) AND it has
		// no exit transition to play out: drop the entry.
		stale := item == nil || item.Generation <= c.generation
		noHandler := item == nil || item.LayoutElement == nil || item.LayoutElement.Config.Transition.Handler == nil
		if !data.TransitionOut && (stale || noHandler) {
			c.transitionDatas.RemoveSwapback(i)
			i--
		}
	}
}

// markExitingElements scans the transition table after element close and
// records the EXITING state for elements whose hashmap generation is stale
// AND that have an exit transition configured. Mirrors the second loop in
// Clay_EndLayout (oracle/clay.h ~line 4472-4577); clone/reparent work is
// factored into cloneElementsWithExitTransition below.
func (c *Context) markExitingElements() {
	for i := int32(0); i < c.transitionDatas.Length; i++ {
		data := c.transitionDatas.Get(i)
		if !data.TransitionOut {
			continue
		}
		item := c.getHashMapItem(data.ElementID)
		if item == nil || item.Generation > c.generation {
			continue
		}
		if len(data.ElementSnapshotSubtree) > 0 && data.ElementSnapshotSubtree[0].ID == data.ElementID {
			data.ElementThisFrame = &data.ElementSnapshotSubtree[0]
		}
		if data.ElementThisFrame == nil {
			// Nothing to drive — element was never registered, just drop.
			c.transitionDatas.RemoveSwapback(i)
			i--
			continue
		}
		cfg := &data.ElementThisFrame.Config.Transition
		parentItem := c.getHashMapItem(data.ParentID)
		parentAlive := parentItem != nil && parentItem.Generation > c.generation
		if cfg.Exit.Trigger != TransitionExitTriggerWhenParentExits && parentItem != nil && !parentAlive {
			// Parent exited too and the default behavior is to skip child exits.
			c.transitionDatas.RemoveSwapback(i)
			i--
			continue
		}
		// Element exited this frame. If we haven't already entered EXITING,
		// stamp the initial state and let the advance loop drive the rest.
		if data.State != TransitionStateExiting {
			if !parentAlive {
				rootContainerID := HashString(String{Text: rootElementIDString}, 0).ID
				data.ElementThisFrame.Config.Floating.AttachTo = AttachToRoot
				data.ElementThisFrame.Config.Floating.Offset = Vector2{X: item.BoundingBox.X, Y: item.BoundingBox.Y}
				data.ElementThisFrame.Config.Floating.ParentID = rootContainerID
			}
			item.AppearedThisFrame = false
			data.ElementThisFrame.Exiting = true
			data.State = TransitionStateExiting
			data.ActiveProperties = cfg.Properties
			data.ElapsedTime = 0
			if cfg.Exit.SetFinalState != nil {
				data.TargetState = cfg.Exit.SetFinalState(data.TargetState, cfg.Properties)
			}
		}
	}
}

// cloneElementsWithExitTransition copies each EXITING transition's previous-
// frame subtree into the high end of layoutElements / layoutElementChildren so
// the second final-layout pass can render it one more frame while the user's
// exit handler animates it out.
//
// Mirrors Clay__CloneElementsWithExitTransition (oracle/clay.h ~line 4374) plus
// the reattachment logic Clay_EndLayout does separately (~line 4505-4567):
// non-floating clones are spliced back into a live parent's child list per
// exit.siblingOrdering, while floating or orphaned clones become independent
// tree roots. When a clone is written, both array Lengths are bumped to capacity
// so plain Get reads reach the high-index clone slots during the second pass.
//
// Reports ErrorTypeElementsCapacityExceeded and bails if the high-end region
// would collide with the live low-end region.
func (c *Context) cloneElementsWithExitTransition() {
	if c.transitionDatas.Length == 0 {
		return
	}
	if c.layoutElements.Capacity == 0 || c.layoutElementChildren.Capacity == 0 {
		return
	}

	nextIndex := c.layoutElements.Capacity - 1
	nextChildIndex := c.layoutElementChildren.Capacity - 1
	// Track which source element IDs we've already cloned this pass so a
	// nested exiting element (e.g. child also has its own transition entry)
	// isn't cloned twice when its ancestor's subtree already covered it.
	var cloned map[uint32]bool
	// Upstream now removes transition records for nested children cloned as
	// part of an exiting ancestor; the ancestor owns the subtree during exit.
	var removeTransitionIDs []uint32
	wroteClone := false

	type cloneFrame struct {
		cloneIdx int32
		source   *LayoutElement
	}
	// Local BFS scratch — avoids reusing openLayoutElementStack since it's an
	// ephemeral arena array we don't want to disturb here.
	var bfs []cloneFrame

	for i := range c.transitionDatas.Length {
		td := c.transitionDatas.Get(i)
		if td.State != TransitionStateExiting || td.ElementThisFrame == nil {
			continue
		}
		if cloned[td.ElementID] {
			// Already covered by an ancestor's exit subtree clone above; the
			// nested transition record is removed after this loop, matching C.
			continue
		}
		if cloned == nil {
			cloned = make(map[uint32]bool, c.transitionDatas.Length)
		}

		// Defensive capacity check: refuse to clone if the high-end region
		// would overlap the live low-end region. The C version doesn't check
		// because its clone path uses Add(), which would soft-fail at capacity;
		// we're explicit so callers get a clean error.
		if nextIndex < c.layoutElements.Length {
			c.reportError(ErrorTypeElementsCapacityExceeded,
				"Clay has run out of space for exit-transition subtree clones in layoutElements. Try using SetMaxElementCount() with a higher value.")
			c.layoutElements.Length = c.layoutElements.Capacity
			c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
			return
		}

		srcRoot := td.ElementThisFrame
		usingSnapshot := len(td.ElementSnapshotSubtree) > 0 && srcRoot == &td.ElementSnapshotSubtree[0]
		// Clone the subtree root into the high-end slot. The clone inherits
		// the source's Config / Dimensions / Children, fixed up below in the
		// BFS pass when we visit this clone.
		rootCloneIdx := nextIndex
		rootClone := c.layoutElements.SetDontTouchLength(rootCloneIdx, *srcRoot)
		if rootClone == nil {
			return
		}
		wroteClone = true
		c.recordExitCloneSlot(rootCloneIdx)
		rootClone.Exiting = true
		rootClone.WrapLines = ArraySlice[WrapLine]{} // stale view into the previous pass's line pool
		// Pin sizing to the recorded dimensions so the layout solver doesn't
		// try to FIT/GROW the exiting element relative to its (now-missing)
		// parent. Mirrors C clay.h:4494-4495.
		rootClone.Config.Layout.Sizing.Width = SizingFixed(rootClone.Dimensions.Width)
		rootClone.Config.Layout.Sizing.Height = SizingFixed(rootClone.Dimensions.Height)

		cloned[srcRoot.ID] = true
		td.ElementThisFrame = rootClone

		// BFS the subtree, cloning each child level by level. The buffer keeps
		// both the high-end clone index and the source node whose children should
		// be copied; exit snapshots use relative child indices, not live arena
		// indices, after BeginLayout has rewound ephemeral memory.
		bfs = bfs[:0]
		bfs = append(bfs, cloneFrame{cloneIdx: rootCloneIdx, source: srcRoot})
		nextIndex--

		for bi := 0; bi < len(bfs); bi++ {
			parentIdx := bfs[bi].cloneIdx
			srcParent := bfs[bi].source
			parent := c.layoutElements.GetCheckCapacity(parentIdx)
			item := c.getHashMapItem(parent.ID)
			if item != nil && item.Generation > c.generation {
				continue
			}
			if item != nil {
				item.Generation = c.generation + 1
				item.LayoutElement = parent
				item.AppearedThisFrame = false
			} else {
				c.addHashMapItem(ElementID{ID: parent.ID}, parent)
			}
			childCount := srcParent.Children.Length
			if childCount == 0 {
				continue
			}
			// Reserve childCount slots for the cloned children and another
			// childCount slots for the children index list. Fail closed if
			// either region would overflow into the live low end.
			if nextIndex-childCount+1 < c.layoutElements.Length ||
				nextChildIndex-childCount+1 < c.layoutElementChildren.Length {
				c.reportError(ErrorTypeElementsCapacityExceeded,
					"Clay has run out of space for exit-transition subtree clones. Try using SetMaxElementCount() with a higher value.")
				c.layoutElements.Length = c.layoutElements.Capacity
				c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
				return
			}
			// Walk children in REVERSE so that descending nextIndex /
			// nextChildIndex end up storing them in DECLARATION order
			// within their contiguous high-index range. Matches the C
			// loop at oracle/clay.h ~line 4395-4403.
			newChildCount := int32(0)
			for j := childCount - 1; j >= 0; j-- {
				srcChildIdx := srcParent.Children.Data[j]
				var srcChild *LayoutElement
				if usingSnapshot {
					if srcChildIdx < 0 || int(srcChildIdx) >= len(td.ElementSnapshotSubtree) {
						continue
					}
					srcChild = &td.ElementSnapshotSubtree[srcChildIdx]
				} else {
					srcChild = c.layoutElements.GetCheckCapacity(srcChildIdx)
				}
				childItem := c.getHashMapItem(srcChild.ID)
				if childItem != nil && childItem.Generation > c.generation {
					continue
				}
				if !srcChild.IsTextElement && srcChild.Config.Transition.Handler != nil {
					removeTransitionIDs = append(removeTransitionIDs, srcChild.ID)
				}
				childClone := c.layoutElements.SetDontTouchLength(nextIndex, *srcChild)
				if childClone == nil {
					return
				}
				c.recordExitCloneSlot(nextIndex)
				childClone.Exiting = true
				childClone.WrapLines = ArraySlice[WrapLine]{}
				if childClone.IsTextElement {
					childClone.TextElementData.WrappedLines.Length = 0
				}
				cloned[srcChild.ID] = true
				c.layoutElementChildren.SetDontTouchLength(nextChildIndex, nextIndex)
				bfs = append(bfs, cloneFrame{cloneIdx: nextIndex, source: srcChild})
				nextIndex--
				nextChildIndex--
				newChildCount++
			}
			// Rewire the parent's Children.Data slice header to the freshly-
			// populated high-index range. Now subsequent code that reads
			// parent.Children.Data[k] sees the cloned child indices, and
			// c.layoutElements.Get(thatIdx) returns the cloned LayoutElement.
			start := nextChildIndex + 1
			parent.Children.Length = newChildCount
			parent.Children.Data = c.layoutElementChildren.Data[start : start+newChildCount]
		}

		parentItem := c.getHashMapItem(td.ParentID)
		parentAlive := parentItem != nil && parentItem.Generation > c.generation && parentItem.LayoutElement != nil
		reattached := false
		if parentAlive && rootClone.Config.Floating.AttachTo == AttachToNone {
			reattached = c.reattachExitingClone(parentItem.LayoutElement, rootCloneIdx, td, &nextChildIndex)
		}
		if !reattached {
			// Floating elements and orphaned exits render as independent roots.
			c.layoutElementTreeRoots.Add(layoutElementTreeRoot{
				LayoutElementIndex: rootCloneIdx,
				ParentID:           rootClone.Config.Floating.ParentID,
				ZIndex:             rootClone.Config.Floating.ZIndex,
			})
		}
	}

	for _, id := range removeTransitionIDs {
		for i := int32(0); i < c.transitionDatas.Length; i++ {
			if c.transitionDatas.Get(i).ElementID == id {
				c.transitionDatas.RemoveSwapback(i)
				break
			}
		}
	}

	if wroteClone {
		// Bump Length to capacity so plain Get() reads inside the final-layout
		// DFS hit the cloned high-index slots. This is safe because (a) the
		// cloned region is the only thing using those slots and (b) BeginLayout
		// resets Length to 0 next frame before any new declarations. The clone
		// region [prevLayoutElementsCloneStart, capacity) was recorded slot by
		// slot via recordExitCloneSlot as each clone was written.
		c.layoutElements.Length = c.layoutElements.Capacity
		c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
	}
}

// recordExitCloneSlot widens the recorded exit-clone region to include high-end
// slot idx. cloneElementsWithExitTransition calls it as each clone is written,
// so resetEphemeralMemory clears every written clone next frame no matter which
// path (normal completion or any capacity-error early return) the clone loop
// exits through — recording at the write site rather than at each Length-bump
// site removes the "remember to record on this error path too" footgun. The
// region is [prevLayoutElementsCloneStart, capacity); EndLayout seeds the start
// at capacity (empty) before the clone pass.
func (c *Context) recordExitCloneSlot(idx int32) {
	if idx < c.prevLayoutElementsCloneStart {
		c.prevLayoutElementsCloneStart = idx
	}
}

func (c *Context) reattachExitingClone(parent *LayoutElement, cloneIdx int32, td *transitionDataInternal, nextChildIndex *int32) bool {
	oldLen := parent.Children.Length
	newLen := oldLen + 1
	start := *nextChildIndex - newLen + 1
	if start < c.layoutElementChildren.Length {
		c.reportError(ErrorTypeElementsCapacityExceeded,
			"Clay has run out of space for exit-transition sibling ordering. Try using SetMaxElementCount() with a higher value.")
		c.layoutElements.Length = c.layoutElements.Capacity
		c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
		return false
	}

	write := start
	foundNaturalSlot := false
	exitOrdering := c.layoutElements.GetCheckCapacity(cloneIdx).Config.Transition.Exit.SiblingOrdering
	if exitOrdering == ExitTransitionOrderingUnderneathSiblings {
		c.layoutElementChildren.Data[write] = cloneIdx
		write++
		foundNaturalSlot = true
	}
	for j := range oldLen {
		if exitOrdering == ExitTransitionOrderingNaturalOrder && j == td.SiblingIndex {
			c.layoutElementChildren.Data[write] = cloneIdx
			write++
			foundNaturalSlot = true
		}
		c.layoutElementChildren.Data[write] = parent.Children.Data[j]
		write++
	}
	if !foundNaturalSlot {
		c.layoutElementChildren.Data[write] = cloneIdx
	}

	parent.Children.Length = newLen
	parent.Children.Data = c.layoutElementChildren.Data[start : start+newLen]
	*nextChildIndex = start - 1
	return true
}

func (c *Context) snapshotTransitionElements() {
	for i := range c.transitionDatas.Length {
		data := c.transitionDatas.Get(i)
		if data.ElementThisFrame != nil && data.ElementThisFrame.ID == data.ElementID {
			// Reuse last frame's backing array so a stable subtree snapshots
			// without allocating. Safe because the previous snapshot was already
			// consumed earlier this EndLayout (cloneElementsWithExitTransition),
			// and ElementThisFrame now points at live/clone arena memory, never
			// into ElementSnapshotSubtree.
			subtree := c.snapshotElementSubtree(data.ElementThisFrame, data.ElementSnapshotSubtree)
			if len(subtree) == 0 {
				continue
			}
			data.ElementSnapshotSubtree = subtree
		}
	}
}

// snapshotElementSubtree flattens root's live subtree into a single slice in
// BFS order, reusing reuse's backing array when capacity allows. Each node's
// Children.Data is rewritten to relative indices into the returned slice (a
// sub-slice of the shared snapshotIndexIdentity buffer), so the result is
// self-contained and survives BeginLayout rewinding the arena next frame.
//
// BFS order is what makes a node's children land in a contiguous index range,
// which in turn lets Children.Data be an identity sub-slice instead of a fresh
// allocation. Steady-state allocations: zero.
func (c *Context) snapshotElementSubtree(root *LayoutElement, reuse []LayoutElement) []LayoutElement {
	if root == nil {
		return nil
	}
	oldLen := len(reuse)
	out := reuse[:0]
	// Append the root keeping its (arena) Children temporarily so the BFS below
	// can read the original child indices before we overwrite them.
	out = append(out, *root)

	for i := 0; i < len(out); i++ {
		// Copy the slice header now; appending to out may reallocate and
		// invalidate &out[i], but the arena child buffer it points at is stable.
		arenaChildren := out[i].Children
		firstChild := int32(len(out))
		count := int32(0)
		for j := range arenaChildren.Length {
			srcChild := c.layoutElements.GetCheckCapacity(arenaChildren.Data[j])
			if srcChild.ID == 0 {
				continue
			}
			out = append(out, *srcChild)
			count++
		}
		out[i].Children.Length = count
		out[i].Children.Data = c.snapshotChildIndexRange(firstChild, count)
	}
	if len(out) < oldLen {
		clear(reuse[len(out):oldLen])
	}
	return out
}

// snapshotChildIndexRange returns identity[start:start+count] (i.e. the values
// start, start+1, …), growing the shared identity buffer as needed. Because the
// buffer always satisfies identity[k] == k and is never mutated after growth,
// every snapshot node can share it.
func (c *Context) snapshotChildIndexRange(start, count int32) []int32 {
	if count == 0 {
		return nil
	}
	need := int(start + count)
	for len(c.snapshotIndexIdentity) < need {
		c.snapshotIndexIdentity = append(c.snapshotIndexIdentity, int32(len(c.snapshotIndexIdentity)))
	}
	return c.snapshotIndexIdentity[start : start+count]
}

// advanceTransitions runs the per-frame state-machine tick: for every live
// transition, compare last frame's target to this frame's resolved bbox,
// trigger enter/transitioning phases if anything changed, then call the
// user's handler exactly once with the elapsed time advanced by deltaTime.
//
// Mirrors the per-transition loop in Clay_EndLayout (oracle/clay.h
// ~line 4591-4720). The pre/post structure (prune, mark-exit, advance) is
// unchanged but inlined into EndLayout.
func (c *Context) advanceTransitions(deltaTime float32) {
	for i := int32(0); i < c.transitionDatas.Length; i++ {
		td := c.transitionDatas.Get(i)
		currentElement := td.ElementThisFrame
		if currentElement == nil {
			continue
		}
		mapItem := c.getHashMapItem(td.ElementID)
		parentMapItem := c.getHashMapItem(td.ParentID)
		if mapItem == nil {
			continue
		}

		// Compute the live target for this frame. For exiting elements the
		// pre-recorded TargetState (from setFinalState) sticks; for everything
		// else, the target is the freshly-laid-out hashmap bbox + style.
		targetState := td.TargetState
		if td.State != TransitionStateExiting {
			targetState = TransitionData{
				BoundingBox:     mapItem.BoundingBox,
				BackgroundColor: currentElement.Config.BackgroundColor,
				OverlayColor:    currentElement.Config.OverlayColor,
				BorderColor:     currentElement.Config.Border.Color,
				BorderWidth:     currentElement.Config.Border.Width,
			}
		}
		oldTargetState := td.TargetState
		td.TargetState = targetState

		// Decide which sub-state we're in:
		//   - appearedThisFrame=true: brand-new element this frame (possibly
		//     start an enter transition)
		//   - otherwise: maybe trigger a TRANSITIONING phase if any tracked
		//     property's target moved
		if mapItem.AppearedThisFrame {
			cfg := &currentElement.Config.Transition
			parentAppeared := parentMapItem != nil && parentMapItem.AppearedThisFrame
			canEnter := cfg.Enter.SetInitialState != nil &&
				(!parentAppeared || cfg.Enter.Trigger != TransitionEnterSkipOnFirstParentFrame)
			if canEnter {
				td.State = TransitionStateEntering
				td.InitialState = cfg.Enter.SetInitialState(td.TargetState, cfg.Properties)
				td.CurrentState = td.InitialState
				td.ActiveProperties = cfg.Properties
				applyTransitionedPropertiesToElement(currentElement, cfg.Properties, td.InitialState, &mapItem.BoundingBox, td.Reparented)
			} else {
				td.InitialState = targetState
				td.CurrentState = targetState
				td.ActiveProperties = TransitionPropertyNone
			}
		} else if td.State != TransitionStateExiting {
			properties := currentElement.Config.Transition.Properties
			var newActive TransitionProperty

			// Parent-relative position delta tracking. We only re-trigger a
			// position transition when the element actually moved relative to
			// its parent (not just because the parent moved). Mirrors the
			// extended comment block at oracle/clay.h ~line 4631-4648.
			var newRelativePosition Vector2
			if parentMapItem != nil {
				var parentScroll Vector2
				if parentMapItem.LayoutElement != nil {
					parentScroll = parentMapItem.LayoutElement.Config.Clip.ChildOffset
				}
				newRelativePosition = Vector2{
					X: mapItem.BoundingBox.X - parentMapItem.BoundingBox.X - parentScroll.X,
					Y: mapItem.BoundingBox.Y - parentMapItem.BoundingBox.Y - parentScroll.Y,
				}
			}
			oldRelativePosition := td.OldParentRelativePosition
			td.OldParentRelativePosition = newRelativePosition

			if properties&TransitionPropertyX != 0 {
				if !floatEqual(oldTargetState.BoundingBox.X, targetState.BoundingBox.X) &&
					(!floatEqual(oldRelativePosition.X, newRelativePosition.X) || td.Reparented) &&
					!c.rootResizedLastFrame {
					newActive |= TransitionPropertyX
				}
			}
			if properties&TransitionPropertyY != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Y, targetState.BoundingBox.Y) &&
					(!floatEqual(oldRelativePosition.Y, newRelativePosition.Y) || td.Reparented) &&
					!c.rootResizedLastFrame {
					newActive |= TransitionPropertyY
				}
			}
			if properties&TransitionPropertyWidth != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Width, targetState.BoundingBox.Width) && !c.rootResizedLastFrame {
					newActive |= TransitionPropertyWidth
				}
			}
			if properties&TransitionPropertyHeight != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Height, targetState.BoundingBox.Height) && !c.rootResizedLastFrame {
					newActive |= TransitionPropertyHeight
				}
			}
			if properties&TransitionPropertyBackgroundColor != 0 &&
				!colorEqual(oldTargetState.BackgroundColor, targetState.BackgroundColor) {
				newActive |= TransitionPropertyBackgroundColor
			}
			if properties&TransitionPropertyOverlayColor != 0 &&
				!colorEqual(oldTargetState.OverlayColor, targetState.OverlayColor) {
				newActive |= TransitionPropertyOverlayColor
			}
			if properties&TransitionPropertyBorderColor != 0 &&
				!colorEqual(oldTargetState.BorderColor, targetState.BorderColor) {
				newActive |= TransitionPropertyBorderColor
			}
			if properties&TransitionPropertyBorderWidth != 0 &&
				!borderWidthEqual(oldTargetState.BorderWidth, targetState.BorderWidth) {
				newActive |= TransitionPropertyBorderWidth
			}
			if newActive != 0 {
				td.ElapsedTime = 0
				td.InitialState = td.CurrentState
				td.State = TransitionStateTransitioning
				td.ActiveProperties |= newActive
			}
		}

		// Run the handler unless we're fully idle (in which case there's
		// nothing to interpolate; just sync states).
		if td.State == TransitionStateIdle {
			td.InitialState = targetState
			td.CurrentState = targetState
			td.TargetState = targetState
			td.ActiveProperties = TransitionPropertyNone
			continue
		}
		handler := currentElement.Config.Transition.Handler
		if handler == nil {
			// Defensive — Decl mutation between frames could remove it.
			td.State = TransitionStateIdle
			continue
		}
		// Clay transition handlers return true once the transition is complete.
		transitionComplete := handler(TransitionCallbackArguments{
			TransitionState: td.State,
			Initial:         td.InitialState,
			Current:         &td.CurrentState,
			Target:          targetState,
			ElapsedTime:     td.ElapsedTime,
			Duration:        currentElement.Config.Transition.Duration,
			Properties:      td.ActiveProperties,
		})
		applyTransitionedPropertiesToElement(currentElement, td.ActiveProperties, td.CurrentState, &mapItem.BoundingBox, td.Reparented)
		td.ElapsedTime += deltaTime

		if transitionComplete {
			switch td.State {
			case TransitionStateEntering, TransitionStateTransitioning:
				td.State = TransitionStateIdle
				td.ElapsedTime = 0
				td.Reparented = false
				td.ActiveProperties = TransitionPropertyNone
			case TransitionStateExiting:
				c.transitionDatas.RemoveSwapback(i)
				i--
			}
		}
	}
}
