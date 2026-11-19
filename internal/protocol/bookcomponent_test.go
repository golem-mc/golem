package protocol

import "testing"

func TestBookPagesReadsBothBookComponents(t *testing.T) {
	var writable Buffer
	writable.PutVarInt(3)
	for range 3 {
		writable.PutString("p")
		writable.PutBool(false)
	}
	var written Buffer
	written.PutString("title")
	written.PutBool(false)
	written.PutString("author")
	written.PutVarInt(0)
	written.PutVarInt(2)

	for _, tc := range []struct {
		name  string
		stack ItemStack
		want  int
		ok    bool
	}{
		{"writable", ItemStack{Raw: []RawComponent{{Type: DataComponentWritableBookContent, Data: writable.Bytes()}}}, 3, true},
		{"written", ItemStack{Raw: []RawComponent{{Type: DataComponentWrittenBookContent, Data: written.Bytes()}}}, 2, true},
		{"none", ItemStack{}, 0, false},
	} {
		got, ok := tc.stack.BookPages()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: BookPages() = %d, %v, want %d, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
