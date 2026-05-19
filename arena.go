package claygo

import "unsafe"

// MinMemorySize returns the byte size of the arena that Initialize requires
// at the current configured maximums (max element count, max measure-text
// cache words).
//
// Mirrors Clay_MinMemorySize (oracle/clay.h ~line 4026): we sum the exact
// byte footprint of every Array allocated by Initialize (both persistent and
// ephemeral) plus a small slack for alignment and future fields.
//
// With the stock defaults (maxElementCount=8192, maxMeasureTextWordCacheSize=16384)
// the foundation port lands at ~7.4 MB. The Go figure is higher than the
// upstream C ~3.5 MB because LayoutElement carries the full Decl (with
// transition-handler func pointers, image/custom interface fields, etc.)
// inline rather than in a union with the text-leaf payload, and Clay's
// RenderCommand is similarly fattened by `any` payload fields. Memory budgets
// scale linearly with maxElementCount and maxMeasureTextWordCacheSize.
//
// The slack term covers:
//   - alignUp padding between heterogeneously-aligned allocations (~16 B per alloc)
//   - 64 B cacheline alignment for the arena base
//   - 4 * maxElementCount bytes of headroom for the structures later port
//     waves will add (scroll containers, transitions, wrapped lines, id
//     strings, dynamic strings, tree nodes, pointer-over ids, warnings).
//
// Keeping the figure conservatively large means callers don't need to
// re-allocate when later waves expand the Context.
func MinMemorySize() uint {
	persistent := uintptr(0)
	persistent += sizeOfArray[LayoutElementHashMapItem](defaultMaxElementCount)
	persistent += sizeOfArray[int32](defaultMaxElementCount)             // layoutElementsHashMap
	persistent += sizeOfArray[int32](defaultMaxElementCount)             // layoutElementsHashMapFreeList
	persistent += sizeOfArray[MeasureTextCacheItem](defaultMaxElementCount)
	persistent += sizeOfArray[int32](defaultMaxElementCount)             // measureTextHashMapInternalFreeList
	persistent += sizeOfArray[int32](defaultMaxMeasureTextWordCacheSize) // measuredWordsFreeList
	persistent += sizeOfArray[int32](defaultMaxElementCount)             // measureTextHashMap
	persistent += sizeOfArray[MeasuredWord](defaultMaxMeasureTextWordCacheSize)
	persistent += sizeOfArray[scrollContainerDataInternal](defaultMaxElementCount)
	persistent += sizeOfArray[transitionDataInternal](200)

	ephemeral := uintptr(0)
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // layoutElementChildrenBuffer
	ephemeral += sizeOfArray[LayoutElement](defaultMaxElementCount)          // layoutElements
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // layoutElementChildren
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // openLayoutElementStack
	ephemeral += sizeOfArray[RenderCommand](defaultMaxElementCount)          // renderCommands
	ephemeral += sizeOfArray[layoutElementTreeRoot](defaultMaxElementCount)  // layoutElementTreeRoots
	ephemeral += sizeOfArray[WrappedTextLine](defaultMaxElementCount)        // wrappedTextLines
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // textElements
	ephemeral += sizeOfArray[ElementID](defaultMaxElementCount)              // pointerOverIds
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // openClipElementStack
	ephemeral += sizeOfArray[int32](defaultMaxElementCount)                  // layoutElementClipElementIds

	// Slack: 64 B cacheline-align safety, 16 B per allocation for alignUp
	// padding (currently 13 allocs), plus headroom for the structures the
	// future waves will add (scroll containers, transitions, wrapped lines,
	// id strings, dynamic strings, tree nodes, pointer-over ids, warnings).
	slack := uintptr(64 + 16*16 + sizeOfArray[byte](defaultMaxElementCount*4))

	return uint(persistent + ephemeral + slack)
}

// sizeOfArray returns the byte footprint of an Array[T] with n entries (the
// payload only — Array headers themselves live on the Go heap, not in the
// arena).
func sizeOfArray[T any](n int32) uintptr {
	var zero T
	return uintptr(n) * unsafe.Sizeof(zero)
}

const (
	defaultMaxElementCount             int32 = 8192
	defaultMaxMeasureTextWordCacheSize int32 = 16384
)

// CreateArenaWithCapacityAndMemory wraps a caller-supplied byte slice as an
// arena. The caller is responsible for keeping memory alive for as long as the
// Context derived from this arena is in use.
func CreateArenaWithCapacityAndMemory(capacity uint, memory []byte) Arena {
	return Arena{
		NextAllocation: 0,
		Capacity:       capacity,
		Memory:         memory,
	}
}

// alignUp rounds n up to the next multiple of align (which must be a power of two).
func alignUp(n, align uintptr) uintptr {
	return (n + align - 1) &^ (align - 1)
}

// allocBytes carves a sub-slice out of the arena, advancing the bump pointer.
// Returns nil if the request would exceed the arena's capacity.
func (a *Arena) allocBytes(size, align uintptr) []byte {
	start := alignUp(a.NextAllocation, align)
	end := start + size
	if end > uintptr(a.Capacity) {
		return nil
	}
	a.NextAllocation = end
	return a.Memory[start:end]
}

// reset rewinds the arena bump pointer to zero. Used by the ephemeral memory
// region between frames.
func (a *Arena) reset() { a.NextAllocation = 0 }

// allocSliceOf reserves capacity for n elements of type T inside the arena
// and returns it as a Go slice header backed by the arena bytes. Callers
// retain the slice for the lifetime of the arena allocation.
//
// This is a low-level primitive used by the internal Context to lay out its
// fixed-capacity arrays. Because the backing memory is owned by the caller's
// byte slice, the returned slice does not introduce additional GC pressure.
func allocSliceOf[T any](a *Arena, n int) []T {
	if n == 0 {
		// Zero-length request: return a non-nil empty slice so callers can
		// distinguish "successful zero-cap allocation" from "arena exhausted".
		// Skipping allocBytes also avoids dereferencing an empty backing slice.
		return []T{}
	}
	var zero T
	size := unsafe.Sizeof(zero)
	align := unsafe.Alignof(zero)
	raw := a.allocBytes(uintptr(n)*size, align)
	if raw == nil {
		return nil
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&raw[0])), n)
}
