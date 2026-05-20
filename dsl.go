package claygo

// Box declares a UI element with the given configuration and optional
// children. The children closure is invoked between Clay's open and close
// calls, so any Box/Text invocations inside it become this element's
// children.
//
// The auto-generated id is `HashNumber(parent.Children.Length, parent.ID)`,
// derived from the element's position in its parent. This is stable across
// re-declarations of the same tree, but if you reorder siblings between
// frames (for example, items in a loop without a stable key), each slot keeps
// its position-derived id while the contents change. Hover state and
// transitions then attach to the position, not the item. Inside loops over
// dynamic data, prefer BoxIDOffset(c, name, uint32(i), ...).
//
// Example:
//
//	claygo.Box(ctx, claygo.Decl{
//	    Layout: claygo.LayoutConfig{Padding: claygo.PaddingAll(16)},
//	    BackgroundColor: claygo.RGBA(40, 40, 48, 255),
//	}, func() {
//	    claygo.Text(ctx, "Hello", claygo.TextElementConfig{FontSize: 18})
//	})
//
// Passing nil for children is allowed and equivalent to declaring a leaf.
func Box(c *Context, decl Decl, children func()) {
	c.openElement()
	c.configureOpenElement(decl)
	if children != nil {
		children()
	}
	c.closeElement()
}

// BoxID is Box with an explicit string identifier, used when the element
// needs to be queried later (Hover, GetElementData, floating-attach targets).
func BoxID(c *Context, id string, decl Decl, children func()) {
	c.openElementWithID(id)
	c.configureOpenElement(decl)
	if children != nil {
		children()
	}
	c.closeElement()
}

// BoxIDOffset is BoxID with a numeric offset folded into the id, useful for
// generating per-iteration ids inside a loop:
//
//	for i, item := range items {
//	    claygo.BoxIDOffset(c, "Row", uint32(i), decl, func() { ... })
//	}
//
// Mirrors CLAY_IDI("Row", i) from upstream.
func BoxIDOffset(c *Context, id string, offset uint32, decl Decl, children func()) {
	c.openElementWithIDOffset(id, offset)
	c.configureOpenElement(decl)
	if children != nil {
		children()
	}
	c.closeElement()
}

// Text declares a text element. Text elements are leaves and cannot have
// children.
//
// Requires Context.SetMeasureTextFunction to have been installed; otherwise
// the leaf measures as 0×0 and the configured ErrorHandler receives
// ErrorTypeTextMeasurementFunctionNotProvided once per Context lifetime.
func Text(c *Context, s string, cfg TextElementConfig) {
	c.openTextElement(s, cfg)
}
