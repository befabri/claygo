package claygo

import "strconv"

// Floating clip scopes are an opt-in extension. Native clipping and shared
// rectangle resolution live in native_clipping.go.

// ClipScope confines a floating tree to selected axes of an element's completed
// bounding box. Selecting neither axis adds no restriction. This is a ClayGo
// extension.
type ClipScope struct {
	ElementID  ElementID
	Horizontal bool
	Vertical   bool
}

// Contains tests a point against the selected axes of box. Clip boundaries are
// half-open: left/top are included, right/bottom excluded. It does not look up
// ElementID; callers supply the resolved box.
func (scope ClipScope) Contains(box BoundingBox, point Vector2) bool {
	return pointWithinClipAxes(box, point, scope.Horizontal, scope.Vertical)
}

func (c *Context) beginClipScopesFrame() {
	c.clipScopes, c.previousClipScopes = c.previousClipScopes, c.clipScopes
	// ElementID contains a diagnostic string. Release names from the pool
	// being reused while keeping the immediately preceding frame intact.
	clear(c.clipScopes.Data[:c.clipScopes.Length])
	c.clipScopes.Length = 0
	c.warnMaxClipScopesExceeded = false
}

// copyClipScopes also runs on exit clones: the previous pool remains intact
// until their snapshots have been copied into this frame's pool.
// Preserve an invalid snapshot's flag; fresh declarations are already zeroed.
func (c *Context) copyClipScopes(element *LayoutElement) {
	if element.IsTextElement {
		return
	}
	config := &element.Config.Floating
	supplied := config.ClipScopes
	config.ClipScopes = nil
	if config.AttachTo == AttachToNone {
		return
	}
	count := 0
	for _, scope := range supplied {
		if scope.Horizontal || scope.Vertical {
			count++
		}
	}
	if count > int(c.clipScopes.Capacity-c.clipScopes.Length) {
		element.clipScopesInvalid = true
		if !c.warnMaxClipScopesExceeded {
			c.warnMaxClipScopesExceeded = true
			c.reportError(ErrorTypeElementsCapacityExceeded,
				"Clay ran out of clip-scope storage. Raise SetMaxElementCount() and re-Initialize with a larger arena.")
		}
		return
	}
	if count == 0 {
		return
	}
	start := c.clipScopes.Length
	for _, scope := range supplied {
		if scope.Horizontal || scope.Vertical {
			c.clipScopes.Add(scope)
		}
	}
	config.ClipScopes = c.clipScopes.Data[start:c.clipScopes.Length:c.clipScopes.Length]
}

func (c *Context) pointerWithinClipScopes(root *LayoutElement) bool {
	if root.clipScopesInvalid {
		return false
	}
	for _, scope := range root.Config.Floating.ClipScopes {
		box, found := c.clipBounds(scope.ElementID.ID)
		if !found || !scope.Contains(box, c.pointerPosition) {
			return false
		}
	}
	return true
}

func (c *Context) appendClipScopes(root *LayoutElement, z int16, count uint32) uint32 {
	if root.clipScopesInvalid {
		c.emitReferencedClip(root, z, 0, ClipRenderData{Horizontal: true, Vertical: true}, count)
		return count + 1
	}
	for _, scope := range root.Config.Floating.ClipScopes {
		c.emitReferencedClip(root, z, scope.ElementID.ID, ClipRenderData{Horizontal: scope.Horizontal, Vertical: scope.Vertical}, count)
		count++
	}
	return count
}

// The inspector runs before final layout: diagnose missing declarations
// without consulting geometry stamps. Show only the copied, active scopes.
func (c *Context) debugClipScopes(element *LayoutElement) {
	scopes := element.Config.Floating.ClipScopes
	if len(scopes) == 0 && !element.clipScopesInvalid {
		return
	}
	Text(c, "Clip Scopes", debugTitleConfig())
	if element.clipScopesInvalid {
		Text(c, "Hidden: clip-scope capacity exceeded", debugTextConfig())
		return
	}
	for _, scope := range scopes {
		target := c.clipElement(scope.ElementID.ID)
		name := scope.ElementID.StringID.Text
		if name == "" && target != nil {
			name = target.ElementID.StringID.Text
		}
		label := strconv.FormatUint(uint64(scope.ElementID.ID), 10)
		if name != "" {
			label = name + " (" + label + ")"
		}
		if target == nil {
			label += " [missing]"
		}
		Text(c, "Target: "+label, debugTextConfig())
		Text(c, "Horizontal: "+boolStr(scope.Horizontal)+", Vertical: "+boolStr(scope.Vertical), debugTextConfig())
	}
}
