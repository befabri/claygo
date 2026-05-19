package claygo

import "testing"

// Golden values harvested from the C reference implementation in
// oracle/clay.h by linking against it with #define CLAY_IMPLEMENTATION
// and printing the (id, offset, baseId) tuple for each input. See the
// task notes / commit message for the harvesting program; we lock the
// exact uint32s here so the Go port can never silently drift.

func TestHashNumber(t *testing.T) {
	cases := []struct {
		name           string
		offset, seed   uint32
		wantID, wantBase uint32
	}{
		{"0,0", 0, 0, 1849449580, 0},
		{"1,0", 1, 0, 2154528970, 0},
		{"255,0", 255, 0, 371534839, 0},
		{"65535,0", 65535, 0, 2779611686, 0},
		{"0,12345", 0, 12345, 3804510166, 12345},
		{"7,42", 7, 42, 3392050243, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HashNumber(tc.offset, tc.seed)
			if got.ID != tc.wantID {
				t.Errorf("ID: got %d, want %d", got.ID, tc.wantID)
			}
			if got.Offset != tc.offset {
				t.Errorf("Offset: got %d, want %d", got.Offset, tc.offset)
			}
			if got.BaseID != tc.wantBase {
				t.Errorf("BaseID: got %d, want %d", got.BaseID, tc.wantBase)
			}
			if got.StringID != (String{}) {
				t.Errorf("StringID: got %q, want zero-value String", got.StringID.Text)
			}
		})
	}
}

func TestHashString(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		seed   uint32
		wantID uint32
	}{
		{"empty,0", "", 0, 1},
		{"empty,42", "", 42, 12386683},
		{"a,0", "a", 0, 3392050243},
		{"ab,0", "ab", 0, 1172708953},
		{"hello,0", "hello", 0, 3372029980},
		{"null-byte,0", "\x00", 0, 1},
		{"hello,42", "hello", 42, 2756652565},
		{"button,0", "button", 0, 323626366},
		{"button,99", "button", 99, 4287871430},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := String{Text: tc.key}
			got := HashString(key, tc.seed)
			if got.ID != tc.wantID {
				t.Errorf("ID: got %d, want %d", got.ID, tc.wantID)
			}
			if got.BaseID != tc.wantID {
				t.Errorf("BaseID: got %d, want %d (BaseID must equal ID for HashString)", got.BaseID, tc.wantID)
			}
			if got.Offset != 0 {
				t.Errorf("Offset: got %d, want 0", got.Offset)
			}
			if got.StringID.Text != tc.key {
				t.Errorf("StringID.Text: got %q, want %q", got.StringID.Text, tc.key)
			}
		})
	}
}

func TestHashStringWithOffset(t *testing.T) {
	cases := []struct {
		name             string
		key              string
		offset, seed     uint32
		wantID, wantBase uint32
	}{
		{"empty,0,0", "", 0, 0, 1, 1},
		{"a,0,0", "a", 0, 0, 1442201245, 3392050243},
		{"hello,1,0", "hello", 1, 0, 1345579723, 3372029980},
		{"hello,255,0", "hello", 255, 0, 2738426492, 3372029980},
		{"row,7,42", "row", 7, 42, 2978117709, 3152242242},
		{"item,3,100", "item", 3, 100, 1686752890, 263781954},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := String{Text: tc.key}
			got := HashStringWithOffset(key, tc.offset, tc.seed)
			if got.ID != tc.wantID {
				t.Errorf("ID: got %d, want %d", got.ID, tc.wantID)
			}
			if got.BaseID != tc.wantBase {
				t.Errorf("BaseID: got %d, want %d", got.BaseID, tc.wantBase)
			}
			if got.Offset != tc.offset {
				t.Errorf("Offset: got %d, want %d", got.Offset, tc.offset)
			}
			if got.StringID.Text != tc.key {
				t.Errorf("StringID.Text: got %q, want %q", got.StringID.Text, tc.key)
			}
		})
	}
}

// TestHashStringBaseEqualsID is a structural invariant check: per upstream,
// HashString must satisfy id == baseID for every input. We cover this in
// TestHashString but pin it explicitly with a few extra keys here so a future
// regression in the BaseID field is loud.
func TestHashStringBaseEqualsID(t *testing.T) {
	for _, k := range []string{"", "x", "panel", "very long key with spaces"} {
		for _, seed := range []uint32{0, 1, 12345, 0xDEADBEEF} {
			got := HashString(String{Text: k}, seed)
			if got.ID != got.BaseID {
				t.Errorf("HashString(%q, %d): ID=%d, BaseID=%d (must be equal)",
					k, seed, got.ID, got.BaseID)
			}
		}
	}
}

// TestHashStringWithOffsetBaseMatchesPlain verifies the documented relationship
// between HashStringWithOffset and HashString: the BaseID returned by
// HashStringWithOffset(key, anyOffset, seed) must equal the ID/BaseID of
// HashString(key, seed). This is what lets CLAY_IDI share a parent base with
// CLAY_ID.
func TestHashStringWithOffsetBaseMatchesPlain(t *testing.T) {
	for _, k := range []string{"", "a", "hello", "menu_item"} {
		for _, seed := range []uint32{0, 42, 0xCAFEBABE} {
			plain := HashString(String{Text: k}, seed)
			for _, off := range []uint32{0, 1, 7, 255, 65535} {
				idx := HashStringWithOffset(String{Text: k}, off, seed)
				if idx.BaseID != plain.ID {
					t.Errorf("key=%q seed=%d off=%d: BaseID=%d, want %d (= HashString id)",
						k, seed, off, idx.BaseID, plain.ID)
				}
			}
		}
	}
}
