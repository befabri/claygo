package claygo

// Native clipping corrections are independent of optional extensions.
// See oracle/UPSTREAM.md and oracle/patches/native-clipping.h.

// clipCommand defers rectangle lookup until every root has current geometry.
// Keeping this outside RenderCommand leaves renderer-facing data unchanged.
type clipCommand struct {
	elementID uint32
	index     int32
}

func pointWithinClipAxes(box BoundingBox, point Vector2, horizontal, vertical bool) bool {
	return (!horizontal || (point.X >= box.X && point.X < box.X+box.Width)) &&
		(!vertical || (point.Y >= box.Y && point.Y < box.Y+box.Height))
}

// clipElement excludes stale hash entries whose arena slot may already have
// been reused. Declaration-time ancestry does not require positioned geometry.
func (c *Context) clipElement(id uint32) *LayoutElementHashMapItem {
	if id == 0 {
		return nil
	}
	item := c.getHashMapItem(id)
	if item == nil || item.Generation <= c.generation || item.LayoutElement == nil || item.LayoutElement.ID != id {
		return nil
	}
	return item
}

func (c *Context) clipBounds(id uint32) (BoundingBox, bool) {
	item := c.clipElement(id)
	if item == nil || item.LayoutElement.clipLayoutPass != c.clipLayoutPass {
		return BoundingBox{}, false
	}
	return item.BoundingBox, true
}

func (c *Context) pointerWithinNativeClips(id uint32) bool {
	// Invalid declarations (such as duplicate IDs) must not make an ancestry
	// walk loop forever. A valid chain cannot exceed the element capacity.
	for remaining := c.layoutElements.Capacity; id != 0; remaining-- {
		if remaining == 0 {
			return false
		}
		item := c.clipElement(id)
		if item == nil || item.LayoutElement.clipLayoutPass != c.clipLayoutPass {
			return false
		}
		config := item.LayoutElement.Config.Clip
		if !pointWithinClipAxes(item.BoundingBox, c.pointerPosition, config.Horizontal, config.Vertical) {
			return false
		}
		id = item.LayoutElement.clipAncestorID
	}
	return true
}

func (c *Context) beginClipLayout() {
	c.clipLayoutPass++
	c.clipCommands.Length = 0
}

func (c *Context) resolveClipCommands() {
	for _, ref := range c.clipCommands.Data[:c.clipCommands.Length] {
		box, _ := c.clipBounds(ref.elementID)
		c.renderCommands.Data[ref.index].BoundingBox = box
	}
}

func (c *Context) emitReferencedClip(root *LayoutElement, z int16, elementID uint32, axes ClipRenderData, ordinal uint32) {
	// Preserve the established inherited-scissor representation in the
	// upstream goldens: no render flags means both axes.
	if axes.Horizontal && axes.Vertical {
		axes = ClipRenderData{}
	}
	index := c.renderCommands.Length
	c.emitCommand(RenderCommand{
		RenderData:  RenderData{Clip: axes},
		ID:          HashNumber(root.ID, uint32(root.Children.Length)+10+2*ordinal).ID,
		ZIndex:      z,
		CommandType: RenderCommandTypeScissorStart,
	})
	if c.renderCommands.Length > index {
		c.clipCommands.Add(clipCommand{elementID: elementID, index: index})
	}
}

func (c *Context) beginNativeFloatingClips(root *LayoutElement, treeRoot *layoutElementTreeRoot, position *Vector2) uint32 {
	// The host's scroll offset applies once, at the nearest native viewport.
	if c.externalScrollHandlingEnabled {
		if item := c.clipElement(treeRoot.ClipElementID); item != nil {
			config := item.LayoutElement.Config.Clip
			if config.Horizontal {
				position.X += config.ChildOffset.X
			}
			if config.Vertical {
				position.Y += config.ChildOffset.Y
			}
		}
	}
	count := uint32(0)
	for id, remaining := treeRoot.ClipElementID, c.layoutElements.Capacity; id != 0; remaining-- {
		item := c.clipElement(id)
		if item == nil || remaining == 0 {
			c.emitReferencedClip(root, treeRoot.ZIndex, 0, ClipRenderData{Horizontal: true, Vertical: true}, count)
			count++
			break
		}
		config := item.LayoutElement.Config.Clip
		// Exit snapshots can retain an ancestor that no longer clips. Skip its
		// rectangle, but continue to any still-active outer ancestors.
		if config.Horizontal || config.Vertical {
			c.emitReferencedClip(root, treeRoot.ZIndex, id, ClipRenderData{Horizontal: config.Horizontal, Vertical: config.Vertical}, count)
			count++
		}
		id = item.LayoutElement.clipAncestorID
	}
	return count
}

func (c *Context) endFloatingClips(root *LayoutElement, count uint32) {
	for count > 0 {
		count--
		c.emitCommand(RenderCommand{
			ID:          HashNumber(root.ID, uint32(root.Children.Length)+11+2*count).ID,
			CommandType: RenderCommandTypeScissorEnd,
		})
	}
}

// Offscreen owners need scissors only when a descendant reaches the screen.
// Deferring them avoids spending the render budget on fully culled subtrees.
// Flush outermost first, using the geometry recorded during descent.
func (c *Context) flushPendingOwnedClips(ancestors []layoutTreeNode, z int16) {
	for i := range ancestors {
		node := &ancestors[i]
		if node.clipPending {
			node.clipPending = false
			box, _ := c.clipBounds(node.element.ID)
			c.beginOwnedClip(node.element, box, z)
		}
	}
}

func (c *Context) beginOwnedClip(element *LayoutElement, box BoundingBox, z int16) {
	config := element.Config.Clip
	if !config.Horizontal && !config.Vertical {
		return
	}
	c.emitCommand(RenderCommand{
		BoundingBox: box,
		RenderData:  RenderData{Clip: ClipRenderData{Horizontal: config.Horizontal, Vertical: config.Vertical}},
		UserData:    element.Config.UserData,
		ID:          element.ID,
		ZIndex:      z,
		CommandType: RenderCommandTypeScissorStart,
	})
}

func (c *Context) endOwnedClip(element *LayoutElement, rootChildCount int32) {
	if element.Config.Clip.Horizontal || element.Config.Clip.Vertical {
		c.emitCommand(RenderCommand{
			ID:          HashNumber(element.ID, uint32(rootChildCount)+11).ID,
			CommandType: RenderCommandTypeScissorEnd,
		})
	}
}
