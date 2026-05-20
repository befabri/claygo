package claygo

import "reflect"

// Array is a fixed-capacity typed slice with arena-enforced capacity. Mirrors
// Clay's CLAY__ARRAY_DEFINE pattern: callers allocate via NewArray with a known
// capacity, then Add/Get/Set/RemoveSwapback at runtime. Out-of-range Get/Add
// returns a pointer to a zero sentinel rather than panicking, matching the C
// original's "soft fail" semantics so chained calls don't NPE.
type Array[T any] struct {
	Capacity int32
	Length   int32
	Data     []T
	zero     T // returned by Get/Add when out of range; callers must not write through it on a failed lookup
}

// NewArray reserves space for capacity elements of T against the arena budget
// and returns an Array[T] backed by a normal typed Go slice. Keeping the data in
// typed slices is required for GC safety: many Clay structs contain Go pointers
// (strings, interfaces, funcs), and storing them in a []byte arena would hide
// those pointers from the collector when the arena is reused.
//
// Returns a zero Array{} (with nil Data) if the arena cannot satisfy the
// reservation, preserving Clay's soft-fail capacity behavior.
func NewArray[T any](capacity int32, arena *Arena) Array[T] {
	if capacity < 0 {
		return Array[T]{}
	}
	if arena != nil {
		typ := reflect.TypeFor[T]()
		if arena.allocBytes(uintptr(capacity)*typ.Size(), uintptr(typ.Align())) == nil {
			return Array[T]{}
		}
	}
	return Array[T]{
		Capacity: capacity,
		Length:   0,
		Data:     make([]T, capacity),
	}
}

// rangeCheck mirrors Clay__Array_RangeCheck: 0 <= i < bound.
func rangeCheck(i, bound int32) bool {
	return i >= 0 && i < bound
}

// Get returns a pointer to the i-th element if i < Length, otherwise a
// pointer to the array's zero sentinel.
func (a *Array[T]) Get(i int32) *T {
	if rangeCheck(i, a.Length) {
		return &a.Data[i]
	}
	return &a.zero
}

// GetValue is like Get but returns a copy. Useful when the caller doesn't
// need to mutate.
func (a *Array[T]) GetValue(i int32) T {
	if rangeCheck(i, a.Length) {
		return a.Data[i]
	}
	return a.zero
}

// GetCheckCapacity returns a pointer if i < Capacity (not Length), used for
// pre-sizing patterns where Set will be called later.
func (a *Array[T]) GetCheckCapacity(i int32) *T {
	if rangeCheck(i, a.Capacity) {
		return &a.Data[i]
	}
	return &a.zero
}

// Add appends item and returns a pointer to its slot. If the array is at
// capacity, returns &zero (and does not append).
func (a *Array[T]) Add(item T) *T {
	if a.Length < a.Capacity {
		a.Data[a.Length] = item
		a.Length++
		return &a.Data[a.Length-1]
	}
	return &a.zero
}

// Set writes value to index i. Updates Length to max(Length, i+1) if i+1 >
// Length. Returns a pointer to the slot, or nil if i >= Capacity.
func (a *Array[T]) Set(i int32, value T) *T {
	if rangeCheck(i, a.Capacity) {
		a.Data[i] = value
		if i >= a.Length {
			a.Length = i + 1
		}
		return &a.Data[i]
	}
	return nil
}

// SetDontTouchLength writes value to index i without modifying Length.
// Returns a pointer to the slot, or nil if i >= Capacity.
func (a *Array[T]) SetDontTouchLength(i int32, value T) *T {
	if rangeCheck(i, a.Capacity) {
		a.Data[i] = value
		return &a.Data[i]
	}
	return nil
}

// RemoveSwapback removes the i-th element by moving the last element into
// its slot and decrementing Length. Returns the removed value (by value),
// or the zero value if i is out of range.
func (a *Array[T]) RemoveSwapback(i int32) T {
	if rangeCheck(i, a.Length) {
		a.Length--
		removed := a.Data[i]
		a.Data[i] = a.Data[a.Length]
		return removed
	}
	return a.zero
}

// ArraySlice is a non-owning view into a section of an Array's Data,
// mirroring Clay's `arrayName##Slice` pattern.
type ArraySlice[T any] struct {
	Length int32
	Data   []T
	zero   T
}

// Get returns a pointer to the i-th element if i < Length, else &zero.
func (s *ArraySlice[T]) Get(i int32) *T {
	if rangeCheck(i, s.Length) {
		return &s.Data[i]
	}
	return &s.zero
}
