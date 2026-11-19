package command

import (
	"github.com/golem-mc/golem/nbt"
	"strings"
	"testing"
)

var snbtBenchTag nbt.Compound

func BenchmarkCommandSNBT(b *testing.B) {
	for _, tc := range []struct{ name, text string }{
		{"entity", "{Health:20f,NoAI:1b,Tags:['one','two'],Pos:[1d,64d,2d]}"},
		{"nested", "{a:{b:{c:[{x:1},{x:2},{x:3}]}},d:[I;1,2,3,4,5]}"},
		{"mixed", "{v:[1,'x',{a:2}]}"},
		{"array", "{v:[I;" + strings.Repeat("123,", 127) + "123]}"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _, _, snbtBenchTag = readSummonTag(tc.text)
			}
		})
	}
}
