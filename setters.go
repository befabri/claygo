package claygo

// setters.go collects the small public configuration knobs that don't fit
// in any other file: max-count tuning, cache resets, external scroll
// integration, and the EaseOut transition helper. These mirror the
// upstream Clay_SetX* APIs verbatim (oracle/clay.h ~line 1016-1031).

// SetMaxElementCount adjusts the maximum number of layout elements the
// Context can hold. Takes effect immediately — but because the arena has
// already been carved at Initialize time, calling this above the original
// budget will not reallocate. Callers who need a bigger budget should
// Initialize with a larger arena, then call this.
//
// Mirrors Clay_SetMaxElementCount (oracle/clay.h ~line 4909).
func (c *Context) SetMaxElementCount(n int32) { c.maxElementCount = n }

// MaxElementCount returns the currently configured maximum element count.
// Mirrors Clay_GetMaxElementCount (oracle/clay.h ~line 4903).
func (c *Context) MaxElementCount() int32 { return c.maxElementCount }

// SetMaxMeasureTextCacheWordCount adjusts the maximum number of measured
// words the text cache holds. Same caveat as SetMaxElementCount: existing
// arena allocations aren't resized.
//
// Mirrors Clay_SetMaxMeasureTextCacheWordCount (oracle/clay.h ~line 4926).
func (c *Context) SetMaxMeasureTextCacheWordCount(n int32) { c.maxMeasureTextCacheWordCount = n }

// MaxMeasureTextCacheWordCount returns the currently configured maximum
// word-cache count. Mirrors Clay_GetMaxMeasureTextCacheWordCount.
func (c *Context) MaxMeasureTextCacheWordCount() int32 { return c.maxMeasureTextCacheWordCount }

// ResetMeasureTextCache clears every entry in the text-measurement cache.
// Call after the user changes a font file or otherwise invalidates the
// previous text measurements. Pointers handed out by GetScrollContainerData
// or previous measureTextCached calls become stale.
//
// Mirrors Clay_ResetMeasureTextCache (oracle/clay.h ~line 4936).
func (c *Context) ResetMeasureTextCache() {
	c.measureTextHashMapInternal.Length = 1 // slot 0 stays the null sentinel
	c.measureTextHashMapInternalFreeList.Length = 0
	c.measuredWords.Length = 0
	c.measuredWordsFreeList.Length = 0
	for i := int32(0); i < c.measureTextHashMap.Capacity; i++ {
		c.measureTextHashMap.Data[i] = 0
	}
}

// SetQueryScrollOffsetFunction installs a callback that Clay calls to read
// scroll offsets from an external scroll manager (e.g. a native OS scroll
// view) when externalScrollHandlingEnabled is true. The callback receives
// the clip element's id and returns the current scroll offset for that
// element.
//
// Mirrors Clay_SetQueryScrollOffsetFunction (oracle/clay.h ~line 4063).
func (c *Context) SetQueryScrollOffsetFunction(fn func(elementID uint32, userData any) Vector2, userData any) {
	c.queryScrollOffsetFunction = fn
	c.queryScrollOffsetUserData = userData
}

// SetExternalScrollHandlingEnabled toggles whether Clay tracks scroll
// internally (default) or defers to a host-supplied scroll manager via
// SetQueryScrollOffsetFunction.
//
// Mirrors Clay_SetExternalScrollHandlingEnabled (oracle/clay.h ~line 4897).
func (c *Context) SetExternalScrollHandlingEnabled(enabled bool) {
	c.externalScrollHandlingEnabled = enabled
}

// LocalAutoID returns a per-frame monotonically increasing index that the
// caller can fold into HashStringWithOffset to build stable element IDs
// for items inside a loop. Mirrors upstream's dynamicElementIndex.
//
// Pattern:
//
//	for i, item := range items {
//	    claygo.BoxIDOffset(c, "Row", c.LocalAutoID(), claygo.Decl{...}, func(){})
//	}
//
// Each call advances the counter; BeginLayout resets it to zero.
func (c *Context) LocalAutoID() uint32 {
	c.dynamicElementIndex++
	return c.dynamicElementIndex
}

// RootResizedLastFrame reports whether the viewport changed dimensions
// between the most recent two BeginLayout calls. The transition engine
// uses this to skip re-triggering position transitions on window resize;
// user code can read it for the same purpose. Mirrors upstream's
// rootResizedLastFrame flag.
func (c *Context) RootResizedLastFrame() bool { return c.rootResizedLastFrame }

// ExternalScrollHandlingEnabled reports the current value of the toggle.
func (c *Context) ExternalScrollHandlingEnabled() bool { return c.externalScrollHandlingEnabled }

// EaseOut is the built-in cubic ease-out curve, intended as a
// TransitionElementConfig.Handler. It interpolates each transitioning
// property linearly along an ease-out cubic and writes the result into
// args.Current. Returns true while the transition is still progressing,
// false once it's complete.
//
// Mirrors Clay_EaseOut (oracle/clay.h ~line 4952).
func EaseOut(args TransitionCallbackArguments) bool {
	if args.Duration <= 0 {
		return false
	}
	t := args.ElapsedTime / args.Duration
	if t >= 1 {
		t = 1
	}
	// Ease-out cubic: f(t) = 1 - (1-t)^3
	inv := 1 - t
	progress := 1 - inv*inv*inv

	if args.Current == nil {
		return t < 1
	}
	initial := args.Initial
	target := args.Target

	if args.Properties&TransitionPropertyX != 0 {
		args.Current.BoundingBox.X = initial.BoundingBox.X + (target.BoundingBox.X-initial.BoundingBox.X)*progress
	}
	if args.Properties&TransitionPropertyY != 0 {
		args.Current.BoundingBox.Y = initial.BoundingBox.Y + (target.BoundingBox.Y-initial.BoundingBox.Y)*progress
	}
	if args.Properties&TransitionPropertyWidth != 0 {
		args.Current.BoundingBox.Width = initial.BoundingBox.Width + (target.BoundingBox.Width-initial.BoundingBox.Width)*progress
	}
	if args.Properties&TransitionPropertyHeight != 0 {
		args.Current.BoundingBox.Height = initial.BoundingBox.Height + (target.BoundingBox.Height-initial.BoundingBox.Height)*progress
	}
	if args.Properties&TransitionPropertyBackgroundColor != 0 {
		args.Current.BackgroundColor = lerpColor(initial.BackgroundColor, target.BackgroundColor, progress)
	}
	if args.Properties&TransitionPropertyOverlayColor != 0 {
		args.Current.OverlayColor = lerpColor(initial.OverlayColor, target.OverlayColor, progress)
	}
	if args.Properties&TransitionPropertyBorderColor != 0 {
		args.Current.BorderColor = lerpColor(initial.BorderColor, target.BorderColor, progress)
	}

	return t < 1
}

func lerpColor(a, b Color, t float32) Color {
	return Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}
