package claygo

import "reflect"

// MinMemorySize returns the arena capacity Initialize requires for the
// current max-element and word-cache counts. Package-level SetMaxElementCount /
// SetMaxMeasureTextCacheWordCount before Initialize change the defaults; after
// Initialize it follows the Context's configured caps.
//
// Mirrors Clay_MinMemorySize (oracle/clay.h ~line 4026): the summed footprint
// of every Array reserved by Initialize plus slack for alignment and future
// fields (see minMemorySizeFor). The budget runs higher than upstream's ~3.5 MB
// (~7.4 MB at the stock defaults) because LayoutElement embeds the full Decl
// inline rather than in a union and RenderCommand carries `any` payload fields;
// it scales linearly with both caps.
func MinMemorySize() uint {
	maxElements := defaultMaxElementCount
	maxWords := defaultMaxMeasureTextWordCacheSize
	if currentContext != nil {
		maxElements = currentContext.maxElementCount
		maxWords = currentContext.maxMeasureTextCacheWordCount
	}
	return minMemorySizeFor(maxElements, maxWords)
}

func minMemorySizeFor(maxElements, maxWords int32) uint {
	persistent := uintptr(0)
	persistent += sizeOfArray[LayoutElementHashMapItem](maxElements)
	persistent += sizeOfArray[int32](maxElements) // layoutElementsHashMap
	persistent += sizeOfArray[int32](maxElements) // layoutElementsHashMapFreeList
	persistent += sizeOfArray[MeasureTextCacheItem](maxElements)
	persistent += sizeOfArray[int32](maxElements) // measureTextHashMapInternalFreeList
	persistent += sizeOfArray[int32](maxWords)    // measuredWordsFreeList
	persistent += sizeOfArray[int32](maxElements) // measureTextHashMap
	persistent += sizeOfArray[MeasuredWord](maxWords)
	persistent += sizeOfArray[scrollContainerDataInternal](maxElements)
	persistent += sizeOfArray[transitionDataInternal](200)
	persistent += sizeOfArray[ElementID](maxElements) // pointerOverIds

	ephemeral := uintptr(0)
	ephemeral += sizeOfArray[int32](maxElements)                 // layoutElementChildrenBuffer
	ephemeral += sizeOfArray[LayoutElement](maxElements)         // layoutElements
	ephemeral += sizeOfArray[int32](maxElements)                 // layoutElementChildren
	ephemeral += sizeOfArray[int32](maxElements)                 // openLayoutElementStack
	ephemeral += sizeOfArray[RenderCommand](maxElements)         // renderCommands
	ephemeral += sizeOfArray[layoutElementTreeRoot](maxElements) // layoutElementTreeRoots
	ephemeral += sizeOfArray[WrappedTextLine](maxElements)       // wrappedTextLines
	ephemeral += sizeOfArray[int32](maxElements)                 // textElements
	ephemeral += sizeOfArray[int32](maxElements)                 // openClipElementStack
	ephemeral += sizeOfArray[int32](maxElements)                 // layoutElementClipElementIds
	ephemeral += sizeOfArray[WrapLine](2 * maxElements)          // wrapLines

	// Slack: 64 B cacheline-align safety, 16 B per allocation for alignUp
	// padding, plus headroom for small scratch structures and future upstream
	// fields.
	slack := uintptr(64 + 16*16 + sizeOfArray[byte](maxElements*4))

	return uint(persistent + ephemeral + slack)
}

// sizeOfArray returns the logical byte footprint reserved for an Array[T] with
// n entries. Array payloads are typed Go slices, but the arena still enforces
// the same capacity budget and soft-fail behavior as the C implementation.
func sizeOfArray[T any](n int32) uintptr {
	return uintptr(n) * reflect.TypeFor[T]().Size()
}

var (
	defaultMaxElementCount             int32 = 8192
	defaultMaxMeasureTextWordCacheSize int32 = 16384
)

// CreateArenaWithCapacity creates a Go-native arena with the given logical
// byte capacity. Internal arrays are backed by typed Go slices so strings,
// interfaces, and function pointers remain visible to the garbage collector;
// no duplicate byte buffer is needed.
func CreateArenaWithCapacity(capacity uint) Arena {
	return Arena{Capacity: capacity}
}

// alignUp rounds n up to the next multiple of align (which must be a power of two).
func alignUp(n, align uintptr) uintptr {
	return (n + align - 1) &^ (align - 1)
}

// reserveBytes advances the arena's logical bump pointer. The overflow checks
// keep malformed public Arena values from wrapping into a valid range.
func (a *Arena) reserveBytes(size, align uintptr) bool {
	if align == 0 || align&(align-1) != 0 {
		return false
	}
	start := alignUp(a.NextAllocation, align)
	limit := uintptr(a.Capacity)
	if start < a.NextAllocation || start > limit || size > limit-start {
		return false
	}
	end := start + size
	a.NextAllocation = end
	return true
}
