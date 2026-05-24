package claygo

// This file ports the text measurement cache from oracle/clay.h:
//   - Clay__MeasuredWord                       (~line 1287)
//   - Clay__MeasureTextCacheItem               (~line 1295)
//   - Clay__HashStringContentsWithConfig       (~line 1594)
//   - Clay__AddMeasuredWord                    (~line 1625)
//   - Clay__MeasureTextCached                  (~line 1639)
//
// The cache is keyed on (text contents, fontId, fontSize, letterSpacing); a
// bucket chain in measureTextHashMap (bucket -> head index) walks
// measureTextHashMapInternal entries via their NextIndex field. Each cache item
// owns a linked list of MeasuredWord entries, stored as indices into the
// measuredWords array. Word slots that age out are pushed onto a free list for
// reuse.

// MeasuredWord is a single word-or-newline run from the measured text.
// Mirrors Clay__MeasuredWord (oracle/clay.h ~line 1287).
type MeasuredWord struct {
	StartOffset int32   // byte offset into the source text
	Length      int32   // byte length (0 for synthetic newline markers)
	Width       float32 // measured pixel width, including trailing space if any
	Next        int32   // next word index in measuredWords, or -1 for tail
}

// MeasureTextCacheItem is one entry in the text-measurement cache. Mirrors
// Clay__MeasureTextCacheItem (oracle/clay.h ~line 1295).
type MeasureTextCacheItem struct {
	UnwrappedDimensions     Dimensions
	MeasuredWordsStartIndex int32 // index of the first MeasuredWord in the chain, or -1
	MinWidth                float32
	ContainsNewlines        bool
	ID                      uint32 // hash key (hashStringContentsWithConfig)
	NextIndex               int32  // next slot in the same hash bucket, or 0 for tail
	Generation              uint32 // frame this entry was last touched on
}

// hashStringContentsWithConfig is the cache key. Port of
// Clay__HashStringContentsWithConfig (oracle/clay.h ~line 1594).
//
// Deviation from upstream: the C reference has a fast path when
// text.isStaticallyAllocated is true that folds the *pointer* of the string
// (not the bytes) into the hash. That's a microoptimization for string
// literals and is inherently non-portable. The Go port always takes the
// contents-only branch, matching upstream's "non-static" path with
// CLAY_DISABLE_SIMD (Bob-Jenkins one-at-a-time over bytes). We tested this
// against the C oracle compiled with -DCLAY_DISABLE_SIMD; see measure_test.go
// for the locked-down golden values.
//
// Also note: upstream mixes in fontId, fontSize, and letterSpacing, but not
// lineHeight. We follow the C reference verbatim so cache hits/misses match.
func hashStringContentsWithConfig(text string, cfg *TextElementConfig) uint32 {
	// Contents-only HashData: Bob-Jenkins one-at-a-time fold over bytes,
	// then take mod UINT32_MAX (i.e. clamp 0xFFFFFFFF -> 0).
	var contents uint64
	for i := 0; i < len(text); i++ {
		contents += uint64(text[i])
		contents += contents << 10
		contents ^= contents >> 6
	}
	// `% UINT32_MAX` in C (where UINT32_MAX = 0xFFFFFFFF) is *not* the same as
	// `& 0xFFFFFFFF`: e.g. 0xFFFFFFFF % 0xFFFFFFFF = 0. Match it exactly.
	var hash uint32
	if contents != 0 {
		hash = uint32(contents % uint64(0xFFFFFFFF))
	}

	hash += uint32(cfg.FontID)
	hash += hash << 10
	hash ^= hash >> 6

	hash += uint32(cfg.FontSize)
	hash += hash << 10
	hash ^= hash >> 6

	hash += uint32(cfg.LetterSpacing)
	hash += hash << 10
	hash ^= hash >> 6

	hash += hash << 3
	hash ^= hash >> 11
	hash += hash << 15
	return hash + 1 // upstream reserves 0 as "null id"
}

// addMeasuredWord links a new MeasuredWord into the chain after prev. If the
// free list has reusable slots it pops one; otherwise it appends to
// measuredWords. Mirrors Clay__AddMeasuredWord (oracle/clay.h ~line 1625).
func (c *Context) addMeasuredWord(word MeasuredWord, prev *MeasuredWord) *MeasuredWord {
	if c.measuredWordsFreeList.Length > 0 {
		newItemIndex := c.measuredWordsFreeList.GetValue(c.measuredWordsFreeList.Length - 1)
		c.measuredWordsFreeList.Length--
		c.measuredWords.Set(newItemIndex, word)
		prev.Next = newItemIndex
		return c.measuredWords.Get(newItemIndex)
	}
	prev.Next = c.measuredWords.Length
	return c.measuredWords.Add(word)
}

// measureTextCached returns the cached measurement for (text, cfg),
// populating the cache on first use. Mirrors Clay__MeasureTextCached
// (oracle/clay.h ~line 1639). The returned pointer aliases an internal cache
// entry; callers should not retain it across cache mutations or frames.
//
// If the measure-text callback is not installed the function reports
// ErrorTypeTextMeasurementFunctionNotProvided and returns &c.measureTextCacheDefault.
func (c *Context) measureTextCached(text string, cfg *TextElementConfig) *MeasureTextCacheItem {
	if c.measureText == nil {
		if !c.warnTextMeasurementFunctionNotSet {
			c.warnTextMeasurementFunctionNotSet = true
			c.reportError(ErrorTypeTextMeasurementFunctionNotProvided,
				"Clay's internal MeasureText function is null. You may have forgotten to call SetMeasureTextFunction(), or passed a nil function by mistake.")
		}
		return &c.measureTextCacheDefault
	}

	id := hashStringContentsWithConfig(text, cfg)
	// Upstream's bucket modulus is maxMeasureTextCacheWordCount / 32. Keep that
	// exactly so cache bucket placement matches the C reference.
	bucketCount := c.maxMeasureTextCacheWordCount / 32
	if bucketCount <= 0 {
		return &c.measureTextCacheDefault
	}
	hashBucket := int32(id % uint32(bucketCount))
	elementIndexPrevious := int32(0)
	elementIndex := c.measureTextHashMap.Data[hashBucket]
	for elementIndex != 0 {
		hashEntry := c.measureTextHashMapInternal.Get(elementIndex)
		if hashEntry.ID == id {
			hashEntry.Generation = c.generation
			return hashEntry
		}
		// Evict entries that haven't been touched in the last few frames.
		if c.generation-hashEntry.Generation > 2 {
			// Push every word slot owned by this entry onto the word free list.
			nextWordIndex := hashEntry.MeasuredWordsStartIndex
			for nextWordIndex != -1 {
				measuredWord := c.measuredWords.Get(nextWordIndex)
				c.measuredWordsFreeList.Add(nextWordIndex)
				nextWordIndex = measuredWord.Next
			}
			nextIndex := hashEntry.NextIndex
			c.measureTextHashMapInternal.Set(elementIndex, MeasureTextCacheItem{MeasuredWordsStartIndex: -1})
			c.measureTextHashMapInternalFreeList.Add(elementIndex)
			if elementIndexPrevious == 0 {
				c.measureTextHashMap.Data[hashBucket] = nextIndex
			} else {
				previousHashEntry := c.measureTextHashMapInternal.Get(elementIndexPrevious)
				previousHashEntry.NextIndex = nextIndex
			}
			elementIndex = nextIndex
		} else {
			elementIndexPrevious = elementIndex
			elementIndex = hashEntry.NextIndex
		}
	}

	var newItemIndex int32
	newCacheItem := MeasureTextCacheItem{MeasuredWordsStartIndex: -1, ID: id, Generation: c.generation}
	var measured *MeasureTextCacheItem
	if c.measureTextHashMapInternalFreeList.Length > 0 {
		newItemIndex = c.measureTextHashMapInternalFreeList.GetValue(c.measureTextHashMapInternalFreeList.Length - 1)
		c.measureTextHashMapInternalFreeList.Length--
		c.measureTextHashMapInternal.Set(newItemIndex, newCacheItem)
		measured = c.measureTextHashMapInternal.Get(newItemIndex)
	} else {
		if c.measureTextHashMapInternal.Length == c.measureTextHashMapInternal.Capacity-1 {
			if !c.warnMaxTextMeasureCacheExceeded {
				c.reportError(ErrorTypeElementsCapacityExceeded,
					"Clay ran out of capacity while attempting to measure text elements. Try using SetMaxElementCount() with a higher value.")
				c.warnMaxTextMeasureCacheExceeded = true
			}
			return &c.measureTextCacheDefault
		}
		measured = c.measureTextHashMapInternal.Add(newCacheItem)
		newItemIndex = c.measureTextHashMapInternal.Length - 1
	}

	var start int32
	var end int32
	var lineWidth, measuredWidth, measuredHeight float32
	spaceWidth := c.measureText(
		StringSlice{Text: " ", Base: " "},
		cfg,
		c.measureTextData,
	).Width
	tempWord := MeasuredWord{Next: -1}
	previousWord := &tempWord
	textLen := int32(len(text))
	for end < textLen {
		if c.measuredWords.Length == c.measuredWords.Capacity-1 {
			if !c.warnMaxTextMeasureCacheExceeded {
				c.reportError(ErrorTypeTextMeasurementCapacityExceeded,
					"Clay has run out of space in its internal text measurement cache. Try using SetMaxMeasureTextCacheWordCount() (default 16384, with 1 unit storing 1 measured word).")
				c.warnMaxTextMeasureCacheExceeded = true
			}
			return &c.measureTextCacheDefault
		}
		current := text[end]
		if current == ' ' || current == '\n' {
			length := end - start
			var dims Dimensions
			if length > 0 {
				dims = c.measureText(
					StringSlice{Text: text[start:end], Base: text},
					cfg,
					c.measureTextData,
				)
			}
			if dims.Width > measured.MinWidth {
				measured.MinWidth = dims.Width
			}
			if dims.Height > measuredHeight {
				measuredHeight = dims.Height
			}
			if current == ' ' {
				dims.Width += spaceWidth
				previousWord = c.addMeasuredWord(MeasuredWord{
					StartOffset: start, Length: length + 1, Width: dims.Width, Next: -1,
				}, previousWord)
				lineWidth += dims.Width
			}
			if current == '\n' {
				if length > 0 {
					previousWord = c.addMeasuredWord(MeasuredWord{
						StartOffset: start, Length: length, Width: dims.Width, Next: -1,
					}, previousWord)
				}
				previousWord = c.addMeasuredWord(MeasuredWord{
					StartOffset: end + 1, Length: 0, Width: 0, Next: -1,
				}, previousWord)
				lineWidth += dims.Width
				if lineWidth > measuredWidth {
					measuredWidth = lineWidth
				}
				measured.ContainsNewlines = true
				lineWidth = 0
			}
			start = end + 1
		}
		end++
	}
	if end-start > 0 {
		dims := c.measureText(
			StringSlice{Text: text[start:end], Base: text},
			cfg,
			c.measureTextData,
		)
		c.addMeasuredWord(MeasuredWord{
			StartOffset: start, Length: end - start, Width: dims.Width, Next: -1,
		}, previousWord)
		lineWidth += dims.Width
		if dims.Height > measuredHeight {
			measuredHeight = dims.Height
		}
		if dims.Width > measured.MinWidth {
			measured.MinWidth = dims.Width
		}
	}
	if lineWidth > measuredWidth {
		measuredWidth = lineWidth
	}
	measuredWidth -= float32(cfg.LetterSpacing)

	measured.MeasuredWordsStartIndex = tempWord.Next
	measured.UnwrappedDimensions.Width = measuredWidth
	measured.UnwrappedDimensions.Height = measuredHeight

	if elementIndexPrevious != 0 {
		c.measureTextHashMapInternal.Get(elementIndexPrevious).NextIndex = newItemIndex
	} else {
		c.measureTextHashMap.Data[hashBucket] = newItemIndex
	}
	return measured
}
