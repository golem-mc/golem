package protocol

import "testing"

func TestRemoveLastBannerPatternKeepsInlineLayers(t *testing.T) {
	t.Parallel()
	var value Buffer
	value.PutVarInt(2)
	value.PutVarInt(0) // an inline pattern
	value.PutString("minecraft:custom")
	value.PutString("custom.key")
	value.PutVarInt(3)
	value.PutVarInt(5) // registry pattern 4
	value.PutVarInt(7)
	stack := ItemStack{ItemID: 1, Count: 1, Raw: []RawComponent{{Type: DataComponentBannerPatterns, Data: value.Bytes()}}}
	if !stack.RemoveLastBannerPattern() {
		t.Fatal("RemoveLastBannerPattern = false, want a layer dropped")
	}
	var want Buffer
	want.PutVarInt(1)
	want.PutVarInt(0)
	want.PutString("minecraft:custom")
	want.PutString("custom.key")
	want.PutVarInt(3)
	if got := stack.Raw[0].Data; string(got) != string(want.Bytes()) {
		t.Errorf("patterns after = %x, want %x", got, want.Bytes())
	}
	if (ItemStack{}).HasDyedColor() {
		t.Error("an empty stack has a dyed colour")
	}
	stack.SetDyedColor(5)
	if !stack.HasDyedColor() {
		t.Error("SetDyedColor left no colour")
	}
	stack.RemoveDyedColor()
	if stack.HasDyedColor() {
		t.Error("RemoveDyedColor left the colour")
	}
}

func TestRemoveLastBannerPatternDropsAnEmptyList(t *testing.T) {
	t.Parallel()
	var value Buffer
	value.PutVarInt(1)
	value.PutVarInt(5)
	value.PutVarInt(7)
	stack := ItemStack{ItemID: 1, Count: 1, Raw: []RawComponent{{Type: DataComponentBannerPatterns, Data: value.Bytes()}}}
	if !stack.RemoveLastBannerPattern() {
		t.Fatal("RemoveLastBannerPattern = false, want the layer dropped")
	}
	if len(stack.Raw) != 0 {
		t.Errorf("components after the last layer = %+v, want the default left out", stack.Raw)
	}
}
