package protocol

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: TranslatableContents.ARG_CODEC, TranslatableContents.getArgument
func TestTranslationArgumentsKeepPrimitiveNumericTags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg  nbt.Tag
		want string
	}{
		{nbt.Byte(1), "[1]"},
		{nbt.Short(-2), "[-2]"},
		{nbt.Double(2.5), "[2.5]"},
		{nbt.Double(1.2345678901234567), "[1.2345678901234567]"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			tag := nbt.Compound{"translate": nbt.String("chat.square_brackets"), "with": nbt.NewList([]nbt.Tag{tc.arg})}
			text, ok := TextOf(tag)
			if !ok || text.Plain() != tc.want {
				t.Fatalf("TextOf(%v) = %+v, %v, want %s", tag, text, ok, tc.want)
			}
			for _, encoded := range []func() []byte{
				func() []byte {
					var b Buffer
					b.PutText(text)
					return b.Bytes()
				},
				func() []byte {
					data, err := nbt.EncodeNetwork(TextTag(text))
					if err != nil {
						t.Fatal(err)
					}
					return data
				},
			} {
				data := encoded()
				got, _, err := nbt.DecodeNetwork(data)
				if err != nil {
					t.Fatal(err)
				}
				args := got.(nbt.Compound)["with"].(nbt.List)
				if len(args.Items) != 1 || !nbt.Equal(nbt.ListElement(args.Items[0]), tc.arg) {
					t.Fatalf("encoded argument = %v, want %v (%s)", args, tc.arg, tc.arg.Type())
				}
				read, ok := ReadText(NewReader(data))
				if !ok || read.Plain() != tc.want {
					t.Fatalf("read argument = %+v, %v, want %s", read, ok, tc.want)
				}
			}
			tag["bold"] = nbt.Byte(1)
			styled, ok := StyledTextOf(tag)
			if !ok || !nbt.Equal(styled.Raw, tag) || styled.Plain() != tc.want {
				t.Fatalf("styled argument = %+v, %v, want styled %s", styled, ok, tc.want)
			}
			if _, ok := TextOf(tc.arg); ok {
				t.Fatal("a primitive number was accepted outside a translation argument")
			}
		})
	}
}

func TestRawTranslationPlainText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		snbt string
		want string
	}{
		{`{translate:"chat.square_brackets",with:[2.5d],bold:true,extra:["!"]}`, "[2.5]!"},
		{`{translate:"chat.square_brackets",with:[{translate:"chat.square_brackets",with:[2.5d],bold:true}]}`, "[[2.5]]"},
		{`["prefix",{translate:"chat.square_brackets",with:[2.5d],bold:true}]`, "prefix[2.5]"},
		{`{translate:"missing.key",fallback:"%2$s/%1$s/%2$s",with:[2.5d,"x"],bold:true}`, "x/2.5/x"},
		{`{translate:"missing.key",with:[2.5d],bold:true}`, "missing.key"},
		{`{translate:"missing.key",fallback:"",with:[2.5d],bold:true}`, ""},
		{`{translate:"chat.square_brackets",fallback:"unused",with:[2.5d],bold:true}`, "[2.5]"},
		{`{text:"literal",translate:"chat.square_brackets",with:[2.5d],bold:true}`, "literal"},
	} {
		tag, _, err := nbt.ParseValueTag(tc.snbt)
		if err != nil {
			t.Fatal(err)
		}
		if got := (Text{Raw: tag}).Plain(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.snbt, got, tc.want)
		}
	}
}

func TestPlainDoublesJavaStyle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n    float64
		want string
	}{
		{2.5, "2.5"}, {2, "2.0"}, {0, "0.0"}, {math.Copysign(0, -1), "-0.0"},
		{0.001, "0.001"}, {0.0001, "1.0E-4"}, {1e6, "1000000.0"}, {1e7, "1.0E7"},
		{-1.25e20, "-1.25E20"}, {1.2345678901234567, "1.2345678901234567"},
		{math.SmallestNonzeroFloat64, "4.9E-324"}, {1e-323, "9.9E-324"},
		{math.Inf(1), "Infinity"}, {math.Inf(-1), "-Infinity"}, {math.NaN(), "NaN"},
	} {
		if got := (Text{Raw: nbt.Double(tc.n)}).Plain(); got != tc.want {
			t.Errorf("%g: got %q, want %q", tc.n, got, tc.want)
		}
	}
}
