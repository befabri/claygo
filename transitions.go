package claygo

// transitions.go ports Clay's transition state machine from oracle/clay.h.
// Each element declared with Decl.Transition.Handler != nil gets a persistent
// transitionDataInternal entry on Context.transitionDatas that survives
// across frames. The state machine compares the previous frame's resolved
// bounding box to this frame's, fires enter/transitioning/exit phases, and
// calls the user's handler each tick to interpolate properties into
// transitionDataInternal.currentState. The render emission pass reads that
// state via applyStoredTransitionToBoundingBox to override the laid-out bbox.
//
// What's ported:
//   - The per-frame "register/refresh" branch (Clay__ConfigureOpenElementPtr
//     ~line 2186-2214) — see configureOpenElement.
//   - The render-time "useStoredBoundingBoxes" override (Clay__CalculateFinalLayout
//     ~line 2918-2938) — see applyStoredTransitionToBoundingBox.
//   - The post-layout advance loop (Clay_EndLayout ~line 4588-4720) — see
//     advanceTransitions.
//
// What's intentionally stubbed / simplified:
//   - cloneElementsWithExitTransition: clones a removed element's entire
//     subtree into the high end of the layoutElements / layoutElementChildren
//     arenas, registers the cloned root as a new layoutElementTreeRoot, and
//     marks every clone as Exiting=true so the final-layout pass renders one
//     more frame of the subtree (driving the exit animation handler). The
//     single-element case is handled the same way as multi-element subtrees.
//     Mirrors Clay__CloneElementsWithExitTransition (oracle/clay.h ~line 4374)
//     plus the subtree-clone step embedded in Clay_EndLayout
//     (oracle/clay.h ~line 4505-4535).
//   - rootResizedLastFrame: not yet tracked anywhere in the port, so the
//     "skip transition if outer window resized" suppression is omitted. Has
//     no effect on the tests below since they hold layout dimensions constant.

// maxTransitionDatas is the persistent capacity of Context.transitionDatas.
// Matches upstream's hard-coded 200 (oracle/clay.h ~line 2252).
const maxTransitionDatas int32 = 200

// transitionDataInternal is the per-element cross-frame transition state.
// Mirrors Clay__TransitionDataInternal (oracle/clay.h ~line 1251).
type transitionDataInternal struct {
	InitialState              TransitionData
	CurrentState              TransitionData
	TargetState               TransitionData
	ElementThisFrame          *LayoutElement
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
	for i := int32(0); i < c.transitionDatas.Length; i++ {
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
	for j := int32(0); j < c.transitionDatas.Length; j++ {
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
// Clay_EndLayout (oracle/clay.h ~line 4472-4577) but skips the C version's
// subtree-cloning step. Our test scenes drive single-element exits so the
// elementThisFrame pointer from the previous frame still references valid
// LayoutElement storage until the next BeginLayout's arena reset.
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
		// Element exited this frame. If we haven't already entered EXITING,
		// stamp the initial state and let the advance loop drive the rest.
		if data.State != TransitionStateExiting {
			if data.ElementThisFrame == nil {
				// Nothing to drive — element was never registered, just drop.
				c.transitionDatas.RemoveSwapback(i)
				i--
				continue
			}
			cfg := &data.ElementThisFrame.Config.Transition
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
// frame subtree into the high end of layoutElements / layoutElementChildren
// so the second final-layout pass can size, position, and render the subtree
// one more frame (during which the user's exit handler animates it out).
//
// Mirrors Clay__CloneElementsWithExitTransition (oracle/clay.h ~line 4374) and
// folds in the parent-reattachment logic the C version performs separately in
// Clay_EndLayout (~line 4505-4567). The Go port deviates from upstream in two
// ways:
//   1. Cloned subtrees are always registered as a NEW layoutElementTreeRoot
//      (positioned by floating-attach to the root container at the element's
//      last-known bbox), rather than being spliced into a still-living
//      parent's children list. This avoids mutating user-declared parents
//      mid-pass and keeps the cloned subtree visible even when its real
//      parent is also exiting.
//   2. layoutElements.Length / layoutElementChildren.Length are bumped to
//      their capacities at the end of the call so plain Get() reads of the
//      cloned high-index slots succeed during the second layout pass. The C
//      version relies on GetCheckCapacity for the equivalent reads.
//
// Walks the transition table once. For each entry in EXITING state whose
// ElementThisFrame still points at the previous frame's LayoutElement, runs a
// BFS from that element: copies each visited element into the high-index slot
// nextIndex (descending from capacity-1), reserves a contiguous high-index
// block in layoutElementChildren for the parent's child indices, and rewires
// the parent's Children.Data slice header to view that block. Each clone is
// stamped Exiting=true.
//
// Returns silently after reporting ErrorTypeElementsCapacityExceeded if the
// high-end region would collide with the live low-end region.
func (c *Context) cloneElementsWithExitTransition() {
	if c.transitionDatas.Length == 0 {
		return
	}
	if c.layoutElements.Capacity == 0 || c.layoutElementChildren.Capacity == 0 {
		return
	}

	nextIndex := c.layoutElements.Capacity - 1
	nextChildIndex := c.layoutElementChildren.Capacity - 1
	rootContainerID := HashString(String{Text: rootElementIDString}, 0).ID

	// Track which source element IDs we've already cloned this pass so a
	// nested exiting element (e.g. child also has its own transition entry)
	// isn't cloned twice when its ancestor's subtree already covered it.
	cloned := make(map[uint32]bool, c.transitionDatas.Length)

	// Local BFS scratch — avoids reusing openLayoutElementStack since it's an
	// ephemeral arena array we don't want to disturb here.
	var bfs []int32

	for i := int32(0); i < c.transitionDatas.Length; i++ {
		td := c.transitionDatas.Get(i)
		if td.State != TransitionStateExiting || td.ElementThisFrame == nil {
			continue
		}
		if cloned[td.ElementID] {
			// Already covered by an ancestor's exit subtree clone above.
			// Point this entry at the already-cloned slot so its handler
			// drives the same LayoutElement the parent clone exposed.
			continue
		}

		// Defensive capacity check: refuse to clone if the high-end region
		// would overlap the live low-end region. The C version doesn't check
		// because the bigger clone uses Add() (which would soft-fail at
		// capacity); we're explicit so callers get a clean error.
		if nextIndex < c.layoutElements.Length {
			c.reportError(ErrorTypeElementsCapacityExceeded,
				"Clay has run out of space for exit-transition subtree clones in layoutElements. Try using SetMaxElementCount() with a higher value.")
			c.layoutElements.Length = c.layoutElements.Capacity
			c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
			return
		}

		// Clone the subtree root into the high-end slot. The clone inherits
		// the source's Config / Dimensions / Children (slice header still
		// points at low-end children, fixed up below in the BFS pass when
		// we visit this clone).
		srcRoot := td.ElementThisFrame
		rootCloneIdx := nextIndex
		rootClone := c.layoutElements.SetDontTouchLength(rootCloneIdx, *srcRoot)
		if rootClone == nil {
			return
		}
		rootClone.Exiting = true
		// Make the cloned root float at its last-known position so the
		// layout pass places it where the user last saw it. The hashmap
		// still carries the element's stale bbox from the prior frame.
		if prevItem := c.getHashMapItem(td.ElementID); prevItem != nil {
			rootClone.Config.Floating.AttachTo = AttachToRoot
			rootClone.Config.Floating.ParentID = rootContainerID
			rootClone.Config.Floating.Offset = Vector2{X: prevItem.BoundingBox.X, Y: prevItem.BoundingBox.Y}
		}
		// Pin sizing to the recorded dimensions so the layout solver doesn't
		// try to FIT/GROW the exiting element relative to its (now-missing)
		// parent. Mirrors C clay.h:4494-4495.
		rootClone.Config.Layout.Sizing.Width = SizingFixed(rootClone.Dimensions.Width)
		rootClone.Config.Layout.Sizing.Height = SizingFixed(rootClone.Dimensions.Height)

		cloned[srcRoot.ID] = true
		td.ElementThisFrame = rootClone

		// BFS the subtree, cloning each child level by level. The buffer
		// holds INDICES into c.layoutElements (high-end slots only).
		bfs = bfs[:0]
		bfs = append(bfs, rootCloneIdx)
		nextIndex--

		for bi := 0; bi < len(bfs); bi++ {
			parentIdx := bfs[bi]
			parent := c.layoutElements.GetCheckCapacity(parentIdx)
			childCount := parent.Children.Length
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
			for j := childCount - 1; j >= 0; j-- {
				srcChildIdx := parent.Children.Data[j]
				srcChild := c.layoutElements.GetCheckCapacity(srcChildIdx)
				childClone := c.layoutElements.SetDontTouchLength(nextIndex, *srcChild)
				if childClone == nil {
					return
				}
				childClone.Exiting = true
				cloned[srcChild.ID] = true
				c.layoutElementChildren.SetDontTouchLength(nextChildIndex, nextIndex)
				bfs = append(bfs, nextIndex)
				nextIndex--
				nextChildIndex--
			}
			// Rewire the parent's Children.Data slice header to the freshly-
			// populated high-index range. Now subsequent code that reads
			// parent.Children.Data[k] sees the cloned child indices, and
			// c.layoutElements.Get(thatIdx) returns the cloned LayoutElement.
			start := nextChildIndex + 1
			parent.Children.Data = c.layoutElementChildren.Data[start : start+childCount]

			// If any cloned child also has its own transition entry, point
			// that entry's ElementThisFrame at the freshly-cloned slot so
			// the second layout pass's applyStoredTransitionToBoundingBox
			// finds it and the advance loop next frame keeps animating the
			// correct LayoutElement.
			for k := childCount - 1; k >= 0; k-- {
				cloneIdx := parent.Children.Data[k]
				clone := c.layoutElements.GetCheckCapacity(cloneIdx)
				for ti := int32(0); ti < c.transitionDatas.Length; ti++ {
					other := c.transitionDatas.Get(ti)
					if other.ElementID == clone.ID && other.State == TransitionStateExiting {
						other.ElementThisFrame = clone
					}
				}
			}
		}

		// Register the cloned subtree root as its own tree root so the
		// sizing / final-layout passes visit it. Floating-attach config set
		// above places it at the previous bbox.
		c.layoutElementTreeRoots.Add(layoutElementTreeRoot{
			LayoutElementIndex: rootCloneIdx,
			ParentID:           rootContainerID,
		})
	}

	// Bump Length to capacity so plain Get() reads inside the final-layout
	// DFS hit the cloned high-index slots. This is safe because (a) the
	// cloned region is the only thing using those slots and (b) BeginLayout
	// resets Length to 0 next frame before any new declarations.
	c.layoutElements.Length = c.layoutElements.Capacity
	c.layoutElementChildren.Length = c.layoutElementChildren.Capacity
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
				!(parentAppeared && cfg.Enter.Trigger == TransitionEnterSkipOnFirstParentFrame)
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
					(!floatEqual(oldRelativePosition.X, newRelativePosition.X) || td.Reparented) {
					newActive |= TransitionPropertyX
				}
			}
			if properties&TransitionPropertyY != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Y, targetState.BoundingBox.Y) &&
					(!floatEqual(oldRelativePosition.Y, newRelativePosition.Y) || td.Reparented) {
					newActive |= TransitionPropertyY
				}
			}
			if properties&TransitionPropertyWidth != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Width, targetState.BoundingBox.Width) {
					newActive |= TransitionPropertyWidth
				}
			}
			if properties&TransitionPropertyHeight != 0 {
				if !floatEqual(oldTargetState.BoundingBox.Height, targetState.BoundingBox.Height) {
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
		// Handler return semantics differ from upstream: the Go port (see
		// EaseOut in setters.go) returns true while STILL PROGRESSING and
		// false ONCE COMPLETE. We invert to a "complete?" bool here to match
		// the state-machine bookkeeping below.
		stillProgressing := handler(TransitionCallbackArguments{
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

		if !stillProgressing {
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
