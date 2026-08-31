package claygo

// This file ports Clay's three Bob-Jenkins-one-at-a-time hash variants from
// the C reference at oracle/clay.h (see Clay__HashNumber, Clay__HashString,
// and Clay__HashStringWithOffset around lines 1424-1472). All arithmetic is
// on uint32 so the natural wraparound matches C's unsigned overflow rules.
//
// The "+1" applied to every returned id is intentional and matches upstream:
// id == 0 is reserved as the null-id sentinel.
//
// Portability note: upstream C reads key bytes via `key.chars[i]` which is
// `char`. On targets where `char` is signed (x86_64 Linux/macOS defaults),
// bytes ≥ 0x80 sign-extend through int promotion before being added to the
// uint32 hash accumulator, producing different hashes than on unsigned-char
// targets. The Go port reads `key.Text[i]` as `byte` (uint8) and explicitly
// casts via `uint32(...)`, so it always treats bytes as unsigned. For
// all-ASCII keys (the common case for element IDs) the outputs are identical;
// for non-ASCII keys the Go port is deterministic and matches what C produces
// under `-funsigned-char`. We choose portability over bug-for-bug fidelity.

// HashNumber returns the ElementID for a numeric offset, useful for indexed
// IDs generated in loops. The resulting id is hash+1 (id==0 is reserved as
// "null id" per upstream).
func HashNumber(offset, seed uint32) ElementID {
	hash := seed
	// +48 mirrors upstream verbatim. The constant is ASCII '0': upstream treats
	// numeric IDs as if they were digits being hashed via HashString's per-byte
	// loop, so the offset is mixed in with the same "+ char + (hash<<10)..." step.
	hash += offset + 48
	hash += hash << 10
	hash ^= hash >> 6

	hash += hash << 3
	hash ^= hash >> 11
	hash += hash << 15
	return ElementID{
		ID:       hash + 1,
		Offset:   offset,
		BaseID:   seed,
		StringID: String{},
	}
}

// HashString returns the ElementID for a string key. The id and baseID are
// identical for plain HashString — baseID becomes the seed for any nested
// HashStringWithOffset calls.
func HashString(key String, seed uint32) ElementID {
	hash := seed
	for i := range len(key.Text) {
		hash += uint32(key.Text[i])
		hash += hash << 10
		hash ^= hash >> 6
	}

	hash += hash << 3
	hash ^= hash >> 11
	hash += hash << 15
	return ElementID{
		ID:       hash + 1,
		Offset:   0,
		BaseID:   hash + 1,
		StringID: key,
	}
}

// HashStringWithOffset is the indexed-key variant: it produces a unique id
// for (key, offset) while keeping baseID stable for the un-indexed parent.
// Used by CLAY_IDI / CLAY_IDI_LOCAL upstream.
func HashStringWithOffset(key String, offset, seed uint32) ElementID {
	var hash uint32
	base := seed

	for i := range len(key.Text) {
		base += uint32(key.Text[i])
		base += base << 10
		base ^= base >> 6
	}
	hash = base
	hash += offset
	hash += hash << 10
	hash ^= hash >> 6

	hash += hash << 3
	base += base << 3
	hash ^= hash >> 11
	base ^= base >> 11
	hash += hash << 15
	base += base << 15
	return ElementID{
		ID:       hash + 1,
		Offset:   offset,
		BaseID:   base + 1,
		StringID: key,
	}
}
