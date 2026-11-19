package protocol

import "testing"

func BenchmarkBundleTemplateSerialization(b *testing.B) {
	items := []ItemStack{
		{ItemID: 1, Count: 64},
		{ItemID: 2, Count: 16, CustomName: &Text{Text: "Supplies"}},
	}
	var value Buffer
	b.ReportAllocs()
	for b.Loop() {
		value.Reset()
		value.PutBundleContents(items)
	}
}
