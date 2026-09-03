package claygo

// LayoutElement is the per-element internal node produced by the layout
// pipeline. Mirrors Clay_LayoutElement (oracle/clay.h ~line 1212).
//
// In the C original the (config, textConfig+textElementData) pair is a union;
// in Go we just keep both members and use IsTextElement as the discriminator.
// Memory cost is small (a few words) and avoids low-level pointer punning.
type LayoutElement struct {
	// Children holds indices into Context.layoutElements for each child of
	// this element, in declaration order. The slice header points into the
	// fixed-capacity Context.layoutElementChildren buffer; the elements
	// themselves are not owned here. For text elements this is unused.
	Children ArraySlice[int32]

	// Dimensions is the final laid-out size in pixels.
	Dimensions Dimensions
	// MinDimensions is the minimum size required by the element's content
	// during sizing. Used by Fit/Grow logic.
	MinDimensions Dimensions

	// Config carries the element declaration (layout, background, border,
	// floating, clip, etc.). Meaningful only when IsTextElement is false.
	Config Decl

	// TextConfig is the text rendering/measurement settings. Meaningful only
	// when IsTextElement is true.
	TextConfig TextElementConfig
	// TextElementData carries the raw text plus its measured dimensions and
	// wrapped lines. Meaningful only when IsTextElement is true.
	TextElementData TextElementData

	// ID is the hashed element identifier (matches the id field on the
	// corresponding LayoutElementHashMapItem).
	ID uint32
	// FloatingChildrenCount counts children that have floating.attachTo set;
	// the layout solver iterates these separately.
	FloatingChildrenCount uint16
	// IsTextElement discriminates between Config and TextConfig+TextElementData.
	IsTextElement bool
	// Exiting is true when the element is currently in an exit transition and
	// its data was retained from a previous frame.
	Exiting bool

	// WrapLines views the lines packed into Context.wrapLines for this element
	// when Config.Layout.WrapChildren is set. Every layout pass repacks the
	// pool, so a view held across passes goes stale.
	WrapLines ArraySlice[WrapLine]
}

// TextElementData is the per-text-leaf bookkeeping. Mirrors
// Clay__TextElementData (oracle/clay.h ~line 1201). The wrapped-line slice
// view becomes meaningful only after the wrapping pass runs.
type TextElementData struct {
	Text                String
	PreferredDimensions Dimensions
	WrappedLines        ArraySlice[WrappedTextLine]
}

// WrappedTextLine is one line produced by the text-wrapping pass. Mirrors
// Clay__WrappedTextLine (oracle/clay.h ~line 1194).
type WrappedTextLine struct {
	Dimensions Dimensions
	Line       String
}

// HoverHandler is the user-installed callback that fires when the pointer is
// over an element with a registered OnHover.
type HoverHandler func(elementID ElementID, pointer PointerData, userData any)

// LayoutElementHashMapItem is one entry in Clay's ID-to-element open-addressed
// hash map. Mirrors Clay_LayoutElementHashMapItem (oracle/clay.h ~line 1269).
//
// The probe chain is materialized via NextIndex, which points at the next
// item in the same bucket (the "internal" array index, not a bucket index).
// -1 sentinels terminate a chain.
type LayoutElementHashMapItem struct {
	BoundingBox   BoundingBox
	ElementID     ElementID
	LayoutElement *LayoutElement

	// OnHoverFunction is called by SetPointerState when the pointer is over
	// this element.
	OnHoverFunction       HoverHandler
	HoverFunctionUserData any

	// NextIndex is the next slot in the same hash-bucket chain, or -1 if this
	// is the tail of the chain.
	NextIndex int32
	// Generation is the frame counter at which this entry was (re)used. The
	// solver compares against Context.generation to detect stale entries.
	Generation uint32
	// IDAlias mirrors upstream's idAlias field, used by re-declared elements
	// that share an ID with a previously-emitted one.
	IDAlias uint32
	// AppearedThisFrame is true when AddHashMapItem (re)issued this slot in
	// the current frame.
	AppearedThisFrame bool

	// DebugData is the in-process debug overlay payload. The fields here are
	// only read when Context.debugMode is true.
	DebugData struct {
		Collision bool
		Collapsed bool
	}
}

// addHashMapItem inserts (or refreshes) the entry for elementID -> element in
// the open-addressed hashmap. Mirrors Clay__AddHashMapItem (oracle/clay.h
// ~line 1784): chains collisions via NextIndex, prefers the free list when
// reusing slots, and reports duplicate IDs via the error handler.
//
// Returns the stored item, or nil if capacity was exceeded.
func (c *Context) addHashMapItem(elementID ElementID, element *LayoutElement) *LayoutElementHashMapItem {
	item := LayoutElementHashMapItem{
		ElementID:         elementID,
		LayoutElement:     element,
		NextIndex:         -1,
		Generation:        c.generation + 1,
		AppearedThisFrame: true,
	}
	cap32 := c.layoutElementsHashMap.Capacity
	if cap32 <= 0 {
		return nil
	}
	hashBucket := int32(elementID.ID % uint32(cap32))
	hashItemPrevious := int32(-1)
	hashItemIndex := c.layoutElementsHashMap.Data[hashBucket]
	for hashItemIndex != -1 {
		hashItem := c.layoutElementsHashMapInternal.Get(hashItemIndex)
		if hashItem.ElementID.ID == elementID.ID {
			// Same element id appearing again. If it's stale (last touched in a
			// previous frame) we replace it in place; if it's fresh this frame
			// we report a duplicate-id error. Either way `hashItem` is the
			// already-chained slot, so we never insert a new node here.
			if hashItem.Generation <= c.generation {
				hashItem.AppearedThisFrame = hashItem.Generation < c.generation
				hashItem.ElementID = elementID
				hashItem.Generation = c.generation + 1
				hashItem.LayoutElement = element
				hashItem.DebugData.Collision = false
				hashItem.OnHoverFunction = nil
				hashItem.HoverFunctionUserData = nil
			} else {
				c.reportError(ErrorTypeDuplicateID,
					"An element with this ID was already declared during this layout. Each id must be unique per frame; inside loops, prefer BoxIDOffset(c, name, uint32(i), ...) so each iteration gets a distinct id.")
				if c.debugMode {
					hashItem.DebugData.Collision = true
				}
			}
			return hashItem
		}
		hashItemPrevious = hashItemIndex
		hashItemIndex = hashItem.NextIndex
	}

	// The capacity guard must sit on the append path only: internal.Length is a
	// high-water mark that pruneStaleHashMapItems never lowers, so checking it
	// before the free list (or before the reuse walk above) would permanently
	// reject new slots once the mark reaches capacity, even with most slots
	// recycled and idle.
	var indexToUse int32
	if c.layoutElementsHashMapFreeList.Length > 0 {
		indexToUse = c.layoutElementsHashMapFreeList.GetValue(c.layoutElementsHashMapFreeList.Length - 1)
		c.layoutElementsHashMapFreeList.Length--
	} else if c.layoutElementsHashMapInternal.Length >= c.layoutElementsHashMapInternal.Capacity-1 {
		if !c.warnHashMapCapacityExceeded {
			c.reportError(ErrorTypeHashMapCapacityExceeded,
				"Clay has run out of space in its internal element ID hashmap. Try using SetMaxElementCount() with a higher value.")
			c.warnHashMapCapacityExceeded = true
		}
		return nil
	} else {
		indexToUse = c.layoutElementsHashMapInternal.Length
	}
	hashItem := c.layoutElementsHashMapInternal.Set(indexToUse, item)
	if hashItem == nil {
		return nil
	}
	if hashItemPrevious != -1 {
		c.layoutElementsHashMapInternal.Get(hashItemPrevious).NextIndex = indexToUse
	} else {
		c.layoutElementsHashMap.Data[hashBucket] = indexToUse
	}
	return hashItem
}

// getHashMapItem returns the entry for id, or nil if no such id is in the
// map. Mirrors Clay__GetHashMapItem (oracle/clay.h ~line 1843). The C version
// returns a pointer to a static "default" item on miss; in Go nil is more
// idiomatic and lets callers compare with == without aliasing pitfalls.
func (c *Context) getHashMapItem(id uint32) *LayoutElementHashMapItem {
	cap32 := c.layoutElementsHashMap.Capacity
	if cap32 <= 0 {
		return nil
	}
	hashBucket := int32(id % uint32(cap32))
	elementIndex := c.layoutElementsHashMap.Data[hashBucket]
	for elementIndex != -1 {
		hashEntry := c.layoutElementsHashMapInternal.Get(elementIndex)
		if hashEntry.ElementID.ID == id {
			return hashEntry
		}
		elementIndex = hashEntry.NextIndex
	}
	return nil
}

func (c *Context) pruneStaleHashMapItems() {
	for bucket := range c.layoutElementsHashMap.Capacity {
		currentIndex := c.layoutElementsHashMap.Data[bucket]
		previousIndex := int32(-1)
		for currentIndex != -1 {
			current := c.layoutElementsHashMapInternal.Get(currentIndex)
			nextIndex := current.NextIndex
			if current.Generation <= c.generation {
				c.layoutElementsHashMapInternal.Set(currentIndex, LayoutElementHashMapItem{NextIndex: -1})
				c.layoutElementsHashMapFreeList.Add(currentIndex)
				if previousIndex == -1 {
					c.layoutElementsHashMap.Data[bucket] = nextIndex
				} else {
					c.layoutElementsHashMapInternal.Get(previousIndex).NextIndex = nextIndex
				}
				currentIndex = nextIndex
				continue
			}
			previousIndex = currentIndex
			currentIndex = nextIndex
		}
	}
}
