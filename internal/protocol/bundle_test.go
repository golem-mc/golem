package protocol

import "testing"

// TestSetBundleContentsLeavesACopiedStackAlone checks that filling a bundle
// writes its own Raw rather than the backing array a copy shares: the clone a
// creative middle-click makes is a value copy of the stack, so writing
// through the shared array would rewrite the original's contents with it.
func TestSetBundleContentsLeavesACopiedStackAlone(t *testing.T) {
	original := ItemStack{ItemID: 1, Count: 1}
	original.SetBundleContents([]ItemStack{{ItemID: 2, Count: 1}, {ItemID: 3, Count: 1}})
	clone := original

	clone.SetBundleContents([]ItemStack{{ItemID: 4, Count: 1}})

	got := original.BundleContents()
	if len(got) != 2 || got[0].ItemID != 2 || got[1].ItemID != 3 {
		t.Errorf("the original holds %+v, want its two entries untouched", got)
	}
}
