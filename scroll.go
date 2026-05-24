package claygo

// scroll.go ports Clay's clip-container scroll handling: drag-scroll with
// momentum, wheel-scroll, and the GetScrollContainerData query API.
//
// Persistent state lives in Context.scrollContainerDatas; one entry per
// clip element seen at least once. Entries that didn't open this frame are
// reaped at the start of UpdateScrollContainers. Per-frame state
// (openClipElementStack, layoutElementClipElementIds) is reset by
// BeginLayout via the arena rewind.
//
// Mirrors Clay_UpdateScrollContainers (oracle/clay.h ~line 4241), the clip
// branch of Clay__ConfigureOpenElementPtr (~2185), and Clay_GetScrollContainerData
// (~4845).

// scrollContainerDataInternal is the persistent per-element scroll state.
// Mirrors Clay__ScrollContainerDataInternal (oracle/clay.h ~line 1247).
type scrollContainerDataInternal struct {
	LayoutElement       *LayoutElement
	BoundingBox         BoundingBox
	ContentSize         Dimensions
	ScrollOrigin        Vector2
	PointerOrigin       Vector2
	ScrollPosition      Vector2
	ScrollMomentum      Vector2
	ElementID           uint32
	PointerScrollActive bool
	OpenThisFrame       bool
	MomentumTime        float32
}

// UpdateScrollContainers advances per-clip-container scroll positions:
//   - momentum decays toward zero each frame
//   - wheel scroll (scrollDelta) translates the innermost clip the
//     pointer is over
//   - if enableDragScrolling is true and the pointer is held inside a
//     clip, the container scrolls to follow drag; on release, momentum
//     is seeded from drag velocity
//
// Stale entries (clips not declared this frame) are reaped.
//
// Mirrors Clay_UpdateScrollContainers (oracle/clay.h ~line 4241).
func (c *Context) UpdateScrollContainers(enableDragScrolling bool, scrollDelta Vector2, deltaTime float32) {
	isPointerActive := enableDragScrolling &&
		(c.pointerData.State == PointerDataPressed ||
			c.pointerData.State == PointerDataPressedThisFrame)

	highestPriorityIdx := int32(-1)
	var highestPriority *scrollContainerDataInternal

	// First pass: reap stale entries, decay momentum, find the pointer-over
	// scroll container with the highest priority (deepest in hit order).
	for i := int32(0); i < c.scrollContainerDatas.Length; {
		sd := c.scrollContainerDatas.Get(i)
		if !sd.OpenThisFrame {
			c.scrollContainerDatas.RemoveSwapback(i)
			continue
		}
		sd.OpenThisFrame = false
		item := c.getHashMapItem(sd.ElementID)
		if item == nil {
			c.scrollContainerDatas.RemoveSwapback(i)
			continue
		}

		// Release-on-pointer-up: convert any residual drag delta into
		// momentum so the container coasts to a stop.
		if !isPointerActive && sd.PointerScrollActive {
			xDiff := sd.ScrollPosition.X - sd.ScrollOrigin.X
			if xDiff < -10 || xDiff > 10 {
				sd.ScrollMomentum.X = xDiff / (sd.MomentumTime * 25)
			}
			yDiff := sd.ScrollPosition.Y - sd.ScrollOrigin.Y
			if yDiff < -10 || yDiff > 10 {
				sd.ScrollMomentum.Y = yDiff / (sd.MomentumTime * 25)
			}
			sd.PointerScrollActive = false
			sd.PointerOrigin = Vector2{}
			sd.ScrollOrigin = Vector2{}
			sd.MomentumTime = 0
		}

		// Apply momentum; zero it out if it's stalled or if a wheel scroll
		// is happening this frame (so user input takes priority).
		sd.ScrollPosition.X += sd.ScrollMomentum.X
		sd.ScrollMomentum.X *= 0.95
		scrollOccurred := scrollDelta.X != 0 || scrollDelta.Y != 0
		if (sd.ScrollMomentum.X > -0.1 && sd.ScrollMomentum.X < 0.1) || scrollOccurred {
			sd.ScrollMomentum.X = 0
		}
		// Clamp X to [-(content - container), 0].
		maxScrollX := sd.ContentSize.Width - sd.LayoutElement.Dimensions.Width
		if maxScrollX < 0 {
			maxScrollX = 0
		}
		if sd.ScrollPosition.X < -maxScrollX {
			sd.ScrollPosition.X = -maxScrollX
		}
		if sd.ScrollPosition.X > 0 {
			sd.ScrollPosition.X = 0
		}

		sd.ScrollPosition.Y += sd.ScrollMomentum.Y
		sd.ScrollMomentum.Y *= 0.95
		if (sd.ScrollMomentum.Y > -0.1 && sd.ScrollMomentum.Y < 0.1) || scrollOccurred {
			sd.ScrollMomentum.Y = 0
		}
		maxScrollY := sd.ContentSize.Height - sd.LayoutElement.Dimensions.Height
		if maxScrollY < 0 {
			maxScrollY = 0
		}
		if sd.ScrollPosition.Y < -maxScrollY {
			sd.ScrollPosition.Y = -maxScrollY
		}
		if sd.ScrollPosition.Y > 0 {
			sd.ScrollPosition.Y = 0
		}

		// Find priority: the pointer-over hit with the latest index "wins"
		// (the deepest hit in declaration order).
		for j := int32(0); j < c.pointerOverIds.Length; j++ {
			if sd.LayoutElement.ID == c.pointerOverIds.GetValue(j).ID {
				highestPriorityIdx = j
				highestPriority = sd
			}
		}
		i++
	}

	if highestPriorityIdx > -1 && highestPriority != nil {
		scrollEl := highestPriority.LayoutElement
		clipCfg := &scrollEl.Config.Clip
		canScrollVert := clipCfg.Vertical && highestPriority.ContentSize.Height > scrollEl.Dimensions.Height
		canScrollHoriz := clipCfg.Horizontal && highestPriority.ContentSize.Width > scrollEl.Dimensions.Width

		// Wheel scroll: multiply by 10 to feel snappy (matches C).
		if canScrollVert {
			highestPriority.ScrollPosition.Y += scrollDelta.Y * 10
		}
		if canScrollHoriz {
			highestPriority.ScrollPosition.X += scrollDelta.X * 10
		}

		// Drag scroll.
		if isPointerActive {
			highestPriority.ScrollMomentum = Vector2{}
			if !highestPriority.PointerScrollActive {
				highestPriority.PointerOrigin = c.pointerData.Position
				highestPriority.ScrollOrigin = highestPriority.ScrollPosition
				highestPriority.PointerScrollActive = true
			} else {
				var dx, dy float32
				if canScrollHoriz {
					oldX := highestPriority.ScrollPosition.X
					highestPriority.ScrollPosition.X = highestPriority.ScrollOrigin.X +
						(c.pointerData.Position.X - highestPriority.PointerOrigin.X)
					maxX := highestPriority.ContentSize.Width - highestPriority.BoundingBox.Width
					if maxX < 0 {
						maxX = 0
					}
					if highestPriority.ScrollPosition.X > 0 {
						highestPriority.ScrollPosition.X = 0
					}
					if highestPriority.ScrollPosition.X < -maxX {
						highestPriority.ScrollPosition.X = -maxX
					}
					dx = highestPriority.ScrollPosition.X - oldX
				}
				if canScrollVert {
					oldY := highestPriority.ScrollPosition.Y
					highestPriority.ScrollPosition.Y = highestPriority.ScrollOrigin.Y +
						(c.pointerData.Position.Y - highestPriority.PointerOrigin.Y)
					maxY := highestPriority.ContentSize.Height - highestPriority.BoundingBox.Height
					if maxY < 0 {
						maxY = 0
					}
					if highestPriority.ScrollPosition.Y > 0 {
						highestPriority.ScrollPosition.Y = 0
					}
					if highestPriority.ScrollPosition.Y < -maxY {
						highestPriority.ScrollPosition.Y = -maxY
					}
					dy = highestPriority.ScrollPosition.Y - oldY
				}
				// If the pointer has barely moved for a while, reset the
				// drag origin so a subsequent flick has fresh velocity.
				stationary := dx > -0.1 && dx < 0.1 && dy > -0.1 && dy < 0.1
				if stationary && highestPriority.MomentumTime > 0.15 {
					highestPriority.MomentumTime = 0
					highestPriority.PointerOrigin = c.pointerData.Position
					highestPriority.ScrollOrigin = highestPriority.ScrollPosition
				} else {
					highestPriority.MomentumTime += deltaTime
				}
			}
		}

		// Final clamp.
		if canScrollVert {
			maxY := highestPriority.ContentSize.Height - scrollEl.Dimensions.Height
			if maxY < 0 {
				maxY = 0
			}
			if highestPriority.ScrollPosition.Y > 0 {
				highestPriority.ScrollPosition.Y = 0
			}
			if highestPriority.ScrollPosition.Y < -maxY {
				highestPriority.ScrollPosition.Y = -maxY
			}
		}
		if canScrollHoriz {
			maxX := highestPriority.ContentSize.Width - scrollEl.Dimensions.Width
			if maxX < 0 {
				maxX = 0
			}
			if highestPriority.ScrollPosition.X > 0 {
				highestPriority.ScrollPosition.X = 0
			}
			if highestPriority.ScrollPosition.X < -maxX {
				highestPriority.ScrollPosition.X = -maxX
			}
		}
	}
}

// GetScrollContainerData returns the runtime state of a clip container by
// ElementID. The returned ScrollPosition pointer is live: callers can mutate
// it to drive scroll programmatically (a scroll bar, autoscroll, etc.).
// Found is false if no clip element has been declared with that id.
//
// Mirrors Clay_GetScrollContainerData (oracle/clay.h ~line 4845).
func (c *Context) GetScrollContainerData(id ElementID) ScrollContainerData {
	for i := int32(0); i < c.scrollContainerDatas.Length; i++ {
		sd := c.scrollContainerDatas.Get(i)
		if sd.ElementID == id.ID {
			return ScrollContainerData{
				ScrollPosition:            &sd.ScrollPosition,
				ScrollContainerDimensions: Dimensions{Width: sd.BoundingBox.Width, Height: sd.BoundingBox.Height},
				ContentDimensions:         sd.ContentSize,
				Config:                    sd.LayoutElement.Config.Clip,
				Found:                     true,
			}
		}
	}
	return ScrollContainerData{}
}

// GetScrollOffset returns the scroll offset for the currently-open clip
// element, or zero when no matching scroll container has been registered yet.
// This mirrors Clay_GetScrollOffset (oracle/clay.h ~line 4224).
func (c *Context) GetScrollOffset() Vector2 {
	if c.warnMaxElementsExceeded {
		return Vector2{}
	}
	openLE := c.getOpenLayoutElement()
	if openLE == nil {
		return Vector2{}
	}
	for i := int32(0); i < c.scrollContainerDatas.Length; i++ {
		sd := c.scrollContainerDatas.Get(i)
		if sd.ElementID == openLE.ID {
			return sd.ScrollPosition
		}
	}
	return Vector2{}
}

// findOrCreateScrollContainer locates the scrollContainerDataInternal entry
// for the given LayoutElement, creating one if it doesn't exist. Called
// from configureOpenElement's clip branch. The returned pointer is valid
// until the next scrollContainerDatas.Add (which the caller doesn't trigger
// after this call), so OK to mutate in place.
func (c *Context) findOrCreateScrollContainer(le *LayoutElement) *scrollContainerDataInternal {
	for i := int32(0); i < c.scrollContainerDatas.Length; i++ {
		sd := c.scrollContainerDatas.Get(i)
		if sd.ElementID == le.ID {
			sd.LayoutElement = le
			sd.OpenThisFrame = true
			if c.externalScrollHandlingEnabled && c.queryScrollOffsetFunction != nil {
				sd.ScrollPosition = c.queryScrollOffsetFunction(sd.ElementID, c.queryScrollOffsetUserData)
			}
			return sd
		}
	}
	sd := c.scrollContainerDatas.Add(scrollContainerDataInternal{
		LayoutElement: le,
		ElementID:     le.ID,
		ScrollOrigin:  Vector2{X: -1, Y: -1},
		OpenThisFrame: true,
	})
	if c.externalScrollHandlingEnabled && c.queryScrollOffsetFunction != nil {
		sd.ScrollPosition = c.queryScrollOffsetFunction(sd.ElementID, c.queryScrollOffsetUserData)
	}
	return sd
}
