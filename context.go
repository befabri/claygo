package claygo

// Context is the per-instance state of a Clay layout engine. Allocate via
// Initialize and reuse across frames; do not copy.
//
// Mirrors Clay_Context from oracle/clay.h (~line 1327). The Go port leaves
// out a handful of fields that belong to later port waves (transition data,
// scroll containers, wrapped text lines, dynamic strings) — those have
// TODO comments at their slot in the struct.
type Context struct {
	// --- Configuration -----------------------------------------------------
	arena                        Arena
	layoutDimensions             Dimensions
	errorHandler                 ErrorHandler
	maxElementCount              int32
	maxMeasureTextCacheWordCount int32

	measureText     func(text StringSlice, cfg *TextElementConfig, userData any) Dimensions
	measureTextData any

	// --- Frame input -------------------------------------------------------
	pointerPosition Vector2
	pointerDown     bool
	pointerData     PointerData

	// --- Per-frame ephemeral state ----------------------------------------
	//
	// The arena offset at the end of persistent init. BeginLayout resets the
	// arena bump pointer to this offset before re-allocating the ephemeral
	// arrays so per-frame memory does not leak.
	arenaResetOffset uintptr

	layoutElements              Array[LayoutElement]
	layoutElementChildren       Array[int32]
	layoutElementChildrenBuffer Array[int32]
	openLayoutElementStack      Array[int32]
	renderCommands              Array[RenderCommand]

	// layoutElementTreeRoots holds one entry per renderable subtree. Index 0
	// is always the auto-root container (the viewport-sized box BeginLayout
	// opens). Each floating element with attachTo != AttachToNone appends an
	// additional root referring to itself, with parent-id / z-index baked in.
	// The final layout pass iterates the roots in z-order so floating
	// children compose correctly over their anchors.
	layoutElementTreeRoots Array[layoutElementTreeRoot]
	// wrappedTextLines is the pool backing each TextElementData.WrappedLines
	// slice. Built per frame by the wrap pass between sizing(x) and sizing(y).
	wrappedTextLines Array[WrappedTextLine]
	// textElements collects the indices of every text-leaf LayoutElement
	// encountered during sizing(x). The text-wrap pass iterates it to build
	// each leaf's WrappedLines view.
	textElements Array[int32]

	// pointerOverIds is the per-frame list of element ids the pointer is
	// currently inside, populated by SetPointerState. Tree-root order means
	// deeper / on-top elements appear earlier in the list.
	pointerOverIds Array[ElementID]

	// openClipElementStack tracks the chain of currently-open clip ids
	// during element declaration. configureOpenElement pushes when a clip
	// (or floating) opens; closeElement pops. Reset by BeginLayout.
	openClipElementStack Array[int32]

	// scrollContainerDatas carries per-clip-element runtime state ACROSS
	// frames so scroll position survives re-layouts. Entries that didn't
	// re-open are reaped at the start of UpdateScrollContainers.
	scrollContainerDatas Array[scrollContainerDataInternal]

	// layoutElementClipElementIds[i] is the element id of the clip ancestor
	// enclosing layoutElements[i], or 0 if i is not inside any clip. Written
	// during openElement / openElementWithID based on the current top of
	// openClipElementStack. Used by floating attach-resolution (so a
	// floating element nested inside a clip can inherit the clip's
	// scissor) and by render-command emission to scope the SCISSOR_START
	// pairings.
	layoutElementClipElementIds Array[int32]

	// dynamicElementIndex is a per-frame counter for generating stable
	// auto-IDs inside loops without the macro tricks Clay's C side uses.
	// LocalAutoID() / GetLocalAutoID() advance it. Resets in BeginLayout.
	dynamicElementIndex uint32

	// previousLayoutDimensions records the size from the most recent
	// BeginLayout so the next BeginLayout can compute rootResizedLastFrame.
	previousLayoutDimensions Dimensions

	// rootResizedLastFrame is true when the viewport changed size between
	// the previous and current frame. The transition advance loop reads it
	// to skip re-triggering position transitions on window resize (would
	// otherwise cause every element to animate to its new position).
	rootResizedLastFrame bool

	// transitionDatas carries per-element transition state ACROSS frames so
	// transitions can interpolate from a previous-frame snapshot to the
	// current target. One entry per element declared with a transition
	// handler; entries are reaped in EndLayout when their element disappears
	// without an exit transition. Mirrors Clay_Context.transitionDatas
	// (oracle/clay.h ~line 1375).
	transitionDatas Array[transitionDataInternal]

	// queryScrollOffsetFunction is the user-installed callback used when
	// externalScrollHandlingEnabled is true: instead of Clay tracking
	// scroll position, it asks the host (e.g. a native scroll view) for
	// the offset to apply each frame.
	queryScrollOffsetFunction func(elementID uint32, userData any) Vector2
	queryScrollOffsetUserData any
	// externalScrollHandlingEnabled toggles whether Clay handles scroll
	// internally (default false) or defers to queryScrollOffsetFunction.
	externalScrollHandlingEnabled bool

	// --- Persistent (cross-frame) state ------------------------------------
	layoutElementsHashMap          Array[int32]
	layoutElementsHashMapInternal  Array[LayoutElementHashMapItem]
	layoutElementsHashMapFreeList  Array[int32]

	measureTextHashMap                 Array[int32]
	measureTextHashMapInternal         Array[MeasureTextCacheItem]
	measureTextHashMapInternalFreeList Array[int32]
	measuredWords                      Array[MeasuredWord]
	measuredWordsFreeList              Array[int32]

	// measureTextCacheDefault is the sentinel returned by measureTextCached
	// when the measure callback is missing or capacity is exhausted. Mirrors
	// Clay__MeasureTextCacheItem_DEFAULT in the C reference. Never mutated.
	measureTextCacheDefault MeasureTextCacheItem

	// --- Misc flags --------------------------------------------------------
	debugMode      bool
	cullingEnabled bool
	generation     uint32

	// useStoredBoundingBoxes is the second-pass flag for transition-aware
	// layout. EndLayout runs calculateFinalLayout once normally (recording
	// true bboxes), then advances transitions, then runs calculateFinalLayout
	// again with this flag set so emitTreeRoot applies the interpolated
	// override before recording each bbox + emitting render commands.
	// Mirrors the `useStoredBoundingBoxes` parameter of
	// Clay__CalculateFinalLayout (oracle/clay.h ~line 2573, ~line 2918).
	useStoredBoundingBoxes bool

	// Boolean warnings: each is "fire the error once per Context lifetime".
	// Mirrors Clay_BooleanWarnings in the C reference (subset).
	warnHashMapCapacityExceeded      bool
	warnMaxTextMeasureCacheExceeded  bool
	warnMaxElementsExceeded          bool
	warnTextMeasurementFunctionNotSet bool

	// debugSelectedElementID is the id of the element the user clicked on in
	// the debug-panel element list, or 0 if no selection is active. Persists
	// across frames so the detail inspector keeps showing the same element
	// until a new row is clicked. Mirrors Clay_Context.debugSelectedElementId
	// (oracle/clay.h ~line 1345).
	debugSelectedElementID uint32
	// debugCollapsed maps an element id to its collapsed state in the debug
	// panel's tree view. Entries are added on first toggle; absence == not
	// collapsed. Lives on Context (not the hashmap item like upstream) so
	// re-declared elements keep their toggle state without having to round-
	// trip through DebugData. Lazily allocated by ToggleDebugCollapsed.
	debugCollapsed map[uint32]bool

	// Genuinely-unported Clay_Context fields (everything else from upstream
	// is implemented above; see the prior TODO sections in git history for
	// the porting timeline):
	//   - layoutElementIdStrings: pool of id strings for debug rendering.
	//     The Go port reaches the hashmap items for names rather than
	//     mirroring this pool. Add if debug-tool perf becomes an issue.
	//   - dynamicStringData / dynamicElementIndexBaseHash: Clay__IntToString
	//     write buffer + hash seed. The Go debug view uses strconv.Itoa.
	//   - layoutElementTreeNodeArray1, treeNodeVisited, reusableElementIndexBuffer:
	//     arena-backed scratch slots used by upstream's DFS. The Go port
	//     uses per-call Go slices instead; equivalent behavior, less arena.
	//   - exitingElementsLength / exitingElementsChildrenLength: replaced
	//     by the high-index-region trick in cloneElementsWithExitTransition.
	//   - warnings array + warningsEnabled: upstream's queued-error stream.
	//     The Go port fires a single boolean per warning type (see warn*
	//     fields above); the verbose queue is debug-only.
}

// layoutElementTreeRoot is one entry in Context.layoutElementTreeRoots:
// either the auto-root (index 0) or a floating-attached subtree root.
// Mirrors Clay__LayoutElementTreeRoot (oracle/clay.h ~line 1245).
type layoutElementTreeRoot struct {
	// LayoutElementIndex is the index into Context.layoutElements of the
	// root element this entry describes.
	LayoutElementIndex int32
	// ParentID is the element id this root is anchored to. For the
	// auto-root this is 0 (no parent). For a floating root it's the
	// resolved parent's id (per AttachTo*).
	ParentID uint32
	// ClipElementID is the id of the enclosing clip container, or 0 if
	// not inside one. Used to scissor a floating subtree to the clip's
	// boundary. Reserved for the clip wave; today always 0.
	ClipElementID uint32
	// ZIndex controls the rendering order of this root relative to
	// siblings. Lower z renders first.
	ZIndex int16
}

// reportError fires the user-supplied error handler with the given type and
// text, falling back to a no-op if no handler is installed.
func (c *Context) reportError(t ErrorType, text string) {
	if c.errorHandler.Func == nil {
		return
	}
	c.errorHandler.Func(ErrorData{
		Type:     t,
		Text:     text,
		UserData: c.errorHandler.UserData,
	})
}

// Initialize allocates and returns a Context backed by the caller-supplied
// arena. The arena must be at least MinMemorySize() bytes.
//
// Mirrors Clay_Initialize + Clay__InitializePersistentMemory +
// Clay__InitializeEphemeralMemory (oracle/clay.h ~lines 4184, 2245, 2220).
// The order of allocations matters: it determines the byte layout in the
// arena, which affects MinMemorySize.
func Initialize(arena Arena, layoutDimensions Dimensions, errorHandler ErrorHandler) *Context {
	ctx := &Context{
		arena:                        arena,
		layoutDimensions:             layoutDimensions,
		errorHandler:                 errorHandler,
		maxElementCount:              defaultMaxElementCount,
		maxMeasureTextCacheWordCount: defaultMaxMeasureTextWordCacheSize,
		cullingEnabled:               true,
		// Zero value of PointerDataInteractionState is PressedThisFrame; the
		// state machine assumes Released as the initial "nothing pressed" state.
		pointerData: PointerData{State: PointerDataReleased},
		// Seed previousLayoutDimensions so the first BeginLayout reports
		// no resize event (rootResizedLastFrame == false).
		previousLayoutDimensions: layoutDimensions,
	}
	ctx.initializePersistentMemory()
	ctx.initializeEphemeralMemory()
	// layoutElementsHashMap slots default to -1 ("empty bucket").
	for i := int32(0); i < ctx.layoutElementsHashMap.Capacity; i++ {
		ctx.layoutElementsHashMap.Data[i] = -1
	}
	// measureTextHashMap slots default to 0 ("end of chain"); upstream
	// reserves slot 0 of measureTextHashMapInternal as the null entry.
	for i := int32(0); i < ctx.measureTextHashMap.Capacity; i++ {
		ctx.measureTextHashMap.Data[i] = 0
	}
	ctx.measureTextHashMapInternal.Length = 1 // reserve slot 0
	return ctx
}

// initializePersistentMemory allocates the cross-frame arrays. Mirrors
// Clay__InitializePersistentMemory (oracle/clay.h ~line 2245).
func (c *Context) initializePersistentMemory() {
	maxElements := c.maxElementCount
	maxWords := c.maxMeasureTextCacheWordCount
	a := &c.arena

	c.layoutElementsHashMapInternal = NewArray[LayoutElementHashMapItem](maxElements, a)
	c.layoutElementsHashMap = NewArray[int32](maxElements, a)
	c.layoutElementsHashMapFreeList = NewArray[int32](maxElements, a)
	c.measureTextHashMapInternal = NewArray[MeasureTextCacheItem](maxElements, a)
	c.measureTextHashMapInternalFreeList = NewArray[int32](maxElements, a)
	c.measuredWordsFreeList = NewArray[int32](maxWords, a)
	c.measureTextHashMap = NewArray[int32](maxElements, a)
	c.measuredWords = NewArray[MeasuredWord](maxWords, a)
	c.scrollContainerDatas = NewArray[scrollContainerDataInternal](maxElements, a)
	// transitionDatas: upstream hard-codes 200 entries (oracle/clay.h ~line
	// 2252). We follow suit; a UI rarely has hundreds of concurrently
	// transitioning elements and the per-entry footprint is small.
	c.transitionDatas = NewArray[transitionDataInternal](maxTransitionDatas, a)
	c.arenaResetOffset = c.arena.NextAllocation
}

// initializeEphemeralMemory allocates the per-frame arrays. Mirrors
// Clay__InitializeEphemeralMemory (oracle/clay.h ~line 2220). Resets the
// arena's bump pointer to arenaResetOffset first so previous-frame ephemeral
// allocations are reclaimed.
func (c *Context) initializeEphemeralMemory() {
	maxElements := c.maxElementCount
	a := &c.arena
	a.NextAllocation = c.arenaResetOffset

	c.layoutElementChildrenBuffer = NewArray[int32](maxElements, a)
	c.layoutElements = NewArray[LayoutElement](maxElements, a)
	c.layoutElementChildren = NewArray[int32](maxElements, a)
	c.openLayoutElementStack = NewArray[int32](maxElements, a)
	c.renderCommands = NewArray[RenderCommand](maxElements, a)
	c.layoutElementTreeRoots = NewArray[layoutElementTreeRoot](maxElements, a)
	c.wrappedTextLines = NewArray[WrappedTextLine](maxElements, a)
	c.textElements = NewArray[int32](maxElements, a)
	c.pointerOverIds = NewArray[ElementID](maxElements, a)
	c.openClipElementStack = NewArray[int32](maxElements, a)
	c.layoutElementClipElementIds = NewArray[int32](maxElements, a)
	// Pre-grow to capacity-equivalent length so [idx] writes are valid.
	c.layoutElementClipElementIds.Length = c.layoutElementClipElementIds.Capacity
	for i := int32(0); i < c.layoutElementClipElementIds.Length; i++ {
		c.layoutElementClipElementIds.Data[i] = 0
	}
}

// SetMeasureTextFunction installs the user-supplied callback Clay uses to
// measure text for sizing and wrapping. Required: layout will report
// ErrorTypeTextMeasurementFunctionNotProvided if no callback is set.
func (c *Context) SetMeasureTextFunction(
	fn func(text StringSlice, cfg *TextElementConfig, userData any) Dimensions,
	userData any,
) {
	c.measureText = fn
	c.measureTextData = userData
}

// SetLayoutDimensions updates the root layout dimensions (typically the
// window size). Take effect on the next BeginLayout.
func (c *Context) SetLayoutDimensions(d Dimensions) { c.layoutDimensions = d }

// LayoutDimensions returns the current root dimensions.
func (c *Context) LayoutDimensions() Dimensions { return c.layoutDimensions }

// BeginLayout resets the per-frame state and opens the auto-root container.
// Pair every BeginLayout with exactly one EndLayout.
//
// Mirrors Clay_BeginLayout (oracle/clay.h ~line 4354): re-initializes
// ephemeral memory, bumps the generation counter, clears boolean warnings,
// then opens a root LayoutElement with id = HashString("Clay__RootContainer",
// 0) and sizing fixed to the configured viewport. The root index is pushed
// onto openLayoutElementStack twice so user-opened elements can look up their
// parent via openLayoutElementStack[Length-2] without special-casing the top
// of the tree.
func (c *Context) BeginLayout() {
	// Detect viewport resize before re-init wipes the dimensions field.
	// Transitions read this to skip re-triggering position animations
	// when the user resizes the window (without it, every element on
	// screen would animate to its new spot).
	c.rootResizedLastFrame = c.previousLayoutDimensions != c.layoutDimensions
	c.previousLayoutDimensions = c.layoutDimensions

	c.initializeEphemeralMemory()
	c.generation++
	c.dynamicElementIndex = 0
	c.warnHashMapCapacityExceeded = false
	c.warnMaxTextMeasureCacheExceeded = false
	c.warnMaxElementsExceeded = false
	c.warnTextMeasurementFunctionNotSet = false

	// Open the auto-root and configure it to span the viewport. When the
	// debug overlay is enabled the root container shrinks by debugViewWidth
	// on the right to make room for the side panel; mirrors clay.h:4362-4364.
	rootWidth := c.layoutDimensions.Width
	if c.debugMode {
		rootWidth -= debugViewWidth
	}
	c.openElementWithID(rootElementIDString)
	c.configureOpenElement(Decl{
		Layout: LayoutConfig{
			Sizing: Sizing{
				Width:  SizingFixed(rootWidth),
				Height: SizingFixed(c.layoutDimensions.Height),
			},
		},
	})
	// Push root index again — the "[root, root, ...]" sentinel band lets
	// openElement read parent as stack[Length-2] without bounds gymnastics
	// for top-level user elements.
	c.openLayoutElementStack.Add(0)
	// Register the auto-root as the first tree root. Floating elements
	// append additional entries via configureOpenElement.
	c.layoutElementTreeRoots.Add(layoutElementTreeRoot{LayoutElementIndex: 0})
}

// EndLayout finalizes the current frame: closes the auto-root container,
// runs the sizing solver, lays out children, and returns a sorted array of
// render commands ready to draw.
//
// Mirrors Clay_EndLayout (oracle/clay.h ~line 4448). The solver passes
// (sizing x/y, positioning, render command emission) are implemented in
// sizing.go and finallayout.go.
func (c *Context) EndLayout(deltaTime float32) RenderCommandArray {
	// Pop the duplicate-root sentinel; closeElement pops the real root and
	// finalizes its dimensions via the FIT/clamp pass.
	if c.openLayoutElementStack.Length > 0 {
		c.openLayoutElementStack.Length--
	}
	c.closeElement()

	// When debug mode is on, append the debug overlay's element tree
	// BEFORE running any final layout pass so the panel participates in
	// the same sizing/positioning solve as the user UI. Mirrors clay.h:4722
	// where Clay__RenderDebugView() is called just before the second
	// Clay__CalculateFinalLayout. Inert when debugMode is false.
	if c.debugMode {
		c.renderDebugView()
	}

	// Transition handling. Order mirrors Clay_EndLayout (oracle/clay.h
	// ~line 4459-4737): prune dead → mark-exit → first layout pass (records
	// true bboxes on hashmap items) → advance state machine → second layout
	// pass with useStoredBoundingBoxes so emitTreeRoot applies the
	// interpolated overrides on the way out. The first-pass render command
	// list is discarded; the visible commands come from the second pass.
	if c.transitionDatas.Length > 0 {
		c.pruneDeadTransitions()
		c.markExitingElements()
		// First pass: lay out + emit using LAST frame's transition state on
		// the hashmap bbox (the override fires below). This pass writes
		// fresh bboxes onto the hashmap so the advance loop sees the new
		// targets.
		c.useStoredBoundingBoxes = false
		c.calculateFinalLayout(deltaTime)
		// Advance the state machine using the freshly-recorded targets.
		c.advanceTransitions(deltaTime)
		// Clone any EXITING subtrees back into the layout arena (high-end
		// slots) and register them as additional tree roots so the second
		// pass renders one more frame of their exit animation. Must run
		// AFTER advance (which mutates ElementThisFrame state we want
		// captured in the clone) and BEFORE pass 2 (which iterates tree
		// roots to emit render commands). See cloneElementsWithExitTransition
		// for the deviation notes versus upstream.
		c.cloneElementsWithExitTransition()
		// Second pass: re-emit with the now-current transition state
		// overriding the bbox of each transitioning element.
		c.useStoredBoundingBoxes = true
		result := c.calculateFinalLayout(deltaTime)
		c.useStoredBoundingBoxes = false
		return result
	}

	return c.calculateFinalLayout(deltaTime)
}

// SetDebugModeEnabled toggles Clay's built-in debug overlay (a side panel
// that renders the element tree). Off by default.
func (c *Context) SetDebugModeEnabled(enabled bool) { c.debugMode = enabled }

// IsDebugModeEnabled reports whether the built-in debug overlay is on.
func (c *Context) IsDebugModeEnabled() bool { return c.debugMode }

// SetCullingEnabled toggles render command culling for offscreen elements.
// On by default.
func (c *Context) SetCullingEnabled(enabled bool) { c.cullingEnabled = enabled }
