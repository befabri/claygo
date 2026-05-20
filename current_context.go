package claygo

var currentContext *Context

// GetCurrentContext returns the package-level current Context. It exists for
// parity with Clay_GetCurrentContext; most Go code should prefer explicit
// *Context method calls.
func GetCurrentContext() *Context { return currentContext }

// SetCurrentContext sets the package-level current Context. It exists for
// parity with Clay_SetCurrentContext and is not safe for concurrent use.
func SetCurrentContext(c *Context) { currentContext = c }

// GetScrollOffset returns GetCurrentContext().GetScrollOffset(), or zero if no
// current Context is installed.
func GetScrollOffset() Vector2 {
	if currentContext == nil {
		return Vector2{}
	}
	return currentContext.GetScrollOffset()
}

// SetPointerState updates pointer state on the current Context.
func SetPointerState(position Vector2, pointerDown bool) {
	if currentContext != nil {
		currentContext.SetPointerState(position, pointerDown)
	}
}

// GetPointerState returns pointer state from the current Context.
func GetPointerState() PointerData {
	if currentContext == nil {
		return PointerData{}
	}
	return currentContext.PointerState()
}

// UpdateScrollContainers advances scroll containers on the current Context.
func UpdateScrollContainers(enableDragScrolling bool, scrollDelta Vector2, deltaTime float32) {
	if currentContext != nil {
		currentContext.UpdateScrollContainers(enableDragScrolling, scrollDelta, deltaTime)
	}
}

// SetLayoutDimensions updates layout dimensions on the current Context.
func SetLayoutDimensions(dimensions Dimensions) {
	if currentContext != nil {
		currentContext.SetLayoutDimensions(dimensions)
	}
}

// GetLayoutDimensions returns layout dimensions from the current Context.
func GetLayoutDimensions() Dimensions {
	if currentContext == nil {
		return Dimensions{}
	}
	return currentContext.LayoutDimensions()
}

// BeginLayout starts a frame on the current Context.
func BeginLayout() {
	if currentContext != nil {
		currentContext.BeginLayout()
	}
}

// EndLayout ends a frame on the current Context.
func EndLayout(deltaTime float32) RenderCommandArray {
	if currentContext == nil {
		return RenderCommandArray{}
	}
	return currentContext.EndLayout(deltaTime)
}

// GetOpenElementID returns the currently-open element id from the current
// Context.
func GetOpenElementID() ElementID {
	if currentContext == nil {
		return ElementID{}
	}
	return currentContext.GetOpenElementID()
}

// GetElementData returns element data from the current Context.
func GetElementData(id ElementID) ElementData {
	if currentContext == nil {
		return ElementData{}
	}
	return currentContext.GetElementData(id)
}

// Hovered reports hover state for the current open element on the current
// Context.
func Hovered() bool {
	return currentContext != nil && currentContext.Hovered()
}

// OnHover registers a hover callback on the current Context.
func OnHover(fn HoverHandler, userData any) {
	if currentContext != nil {
		currentContext.OnHover(fn, userData)
	}
}

// PointerOver reports whether the pointer is over id on the current Context.
func PointerOver(id ElementID) bool {
	return currentContext != nil && currentContext.PointerOver(id)
}

// GetPointerOverIds returns pointer-over ids from the current Context.
func GetPointerOverIds() []ElementID {
	if currentContext == nil {
		return nil
	}
	return currentContext.GetPointerOverIds()
}

// GetScrollContainerData returns scroll data from the current Context.
func GetScrollContainerData(id ElementID) ScrollContainerData {
	if currentContext == nil {
		return ScrollContainerData{}
	}
	return currentContext.GetScrollContainerData(id)
}

// SetMeasureTextFunction installs the measure-text callback on the current
// Context.
func SetMeasureTextFunction(fn func(text StringSlice, cfg *TextElementConfig, userData any) Dimensions, userData any) {
	if currentContext != nil {
		currentContext.SetMeasureTextFunction(fn, userData)
	}
}

// SetQueryScrollOffsetFunction installs the external scroll query callback on
// the current Context.
func SetQueryScrollOffsetFunction(fn func(elementID uint32, userData any) Vector2, userData any) {
	if currentContext != nil {
		currentContext.SetQueryScrollOffsetFunction(fn, userData)
	}
}

// SetExternalScrollHandlingEnabled toggles external scroll handling on the
// current Context.
func SetExternalScrollHandlingEnabled(enabled bool) {
	if currentContext != nil {
		currentContext.SetExternalScrollHandlingEnabled(enabled)
	}
}

// ExternalScrollHandlingEnabled reports whether the current Context defers
// scroll positions to an external scroll manager.
func ExternalScrollHandlingEnabled() bool {
	return currentContext != nil && currentContext.ExternalScrollHandlingEnabled()
}

// SetDebugModeEnabled toggles debug mode on the current Context.
func SetDebugModeEnabled(enabled bool) {
	if currentContext != nil {
		currentContext.SetDebugModeEnabled(enabled)
	}
}

// IsDebugModeEnabled reports debug mode on the current Context.
func IsDebugModeEnabled() bool {
	return currentContext != nil && currentContext.IsDebugModeEnabled()
}

// SetCullingEnabled toggles render-command culling on the current Context.
func SetCullingEnabled(enabled bool) {
	if currentContext != nil {
		currentContext.SetCullingEnabled(enabled)
	}
}

// SetMaxElementCount adjusts the current Context's element cap.
func SetMaxElementCount(n int32) {
	if currentContext == nil {
		defaultMaxElementCount = n
		defaultMaxMeasureTextWordCacheSize = n * 2
		return
	}
	currentContext.SetMaxElementCount(n)
}

// GetMaxElementCount returns the current Context's element cap.
func GetMaxElementCount() int32 {
	if currentContext == nil {
		return defaultMaxElementCount
	}
	return currentContext.MaxElementCount()
}

// SetMaxMeasureTextCacheWordCount adjusts the current Context's word-cache cap.
func SetMaxMeasureTextCacheWordCount(n int32) {
	if currentContext == nil {
		defaultMaxMeasureTextWordCacheSize = n
		return
	}
	currentContext.SetMaxMeasureTextCacheWordCount(n)
}

// GetMaxMeasureTextCacheWordCount returns the current Context's word-cache cap.
func GetMaxMeasureTextCacheWordCount() int32 {
	if currentContext == nil {
		return defaultMaxMeasureTextWordCacheSize
	}
	return currentContext.MaxMeasureTextCacheWordCount()
}

// ResetMeasureTextCache clears the current Context's measure-text cache.
func ResetMeasureTextCache() {
	if currentContext != nil {
		currentContext.ResetMeasureTextCache()
	}
}

// OpenElement opens an auto-id element on the current Context.
func OpenElement() {
	if currentContext != nil {
		currentContext.openElement()
	}
}

// OpenElementWithID opens an element with a precomputed id on the current
// Context.
func OpenElementWithID(id ElementID) {
	if currentContext != nil {
		currentContext.openElementWithElementID(id)
	}
}

// ConfigureOpenElement applies a declaration to the current open element on
// the current Context.
func ConfigureOpenElement(decl Decl) {
	if currentContext != nil {
		currentContext.configureOpenElement(decl)
	}
}

// ConfigureOpenElementPtr applies a declaration pointer to the current open
// element on the current Context.
func ConfigureOpenElementPtr(decl *Decl) {
	if currentContext != nil && decl != nil {
		currentContext.configureOpenElement(*decl)
	}
}

// CloseElement closes the current open element on the current Context.
func CloseElement() {
	if currentContext != nil {
		currentContext.closeElement()
	}
}

// OpenTextElement declares a text leaf on the current Context.
func OpenTextElement(text String, cfg TextElementConfig) {
	if currentContext != nil {
		currentContext.openTextElement(text.Text, cfg)
	}
}

// OpenElement opens an auto-id element.
func (c *Context) OpenElement() { c.openElement() }

// OpenElementWithID opens an element with a precomputed id.
func (c *Context) OpenElementWithID(id ElementID) { c.openElementWithElementID(id) }

// ConfigureOpenElement applies a declaration to the current open element.
func (c *Context) ConfigureOpenElement(decl Decl) { c.configureOpenElement(decl) }

// ConfigureOpenElementPtr applies a declaration pointer to the current open
// element.
func (c *Context) ConfigureOpenElementPtr(decl *Decl) {
	if decl != nil {
		c.configureOpenElement(*decl)
	}
}

// CloseElement closes the current open element.
func (c *Context) CloseElement() { c.closeElement() }

// OpenTextElement declares a text leaf.
func (c *Context) OpenTextElement(text String, cfg TextElementConfig) {
	c.openTextElement(text.Text, cfg)
}

// GetLayoutDimensions returns the configured root layout dimensions.
func (c *Context) GetLayoutDimensions() Dimensions { return c.LayoutDimensions() }

// GetMaxElementCount returns the configured maximum element count.
func (c *Context) GetMaxElementCount() int32 { return c.MaxElementCount() }

// GetMaxMeasureTextCacheWordCount returns the configured maximum word-cache
// count.
func (c *Context) GetMaxMeasureTextCacheWordCount() int32 {
	return c.MaxMeasureTextCacheWordCount()
}
