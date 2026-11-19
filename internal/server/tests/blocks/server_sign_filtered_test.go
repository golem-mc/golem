package blocks

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestSignsKeepFilteredMessagesWithoutOpFeatures(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Writer", servertest.OpsListing(t, "Writer"))
	servertest.HoldTicks(t, srv)
	const data = `{front_text:{messages:["Raw",{text:"Styled",bold:true},{selector:"@p"},["a","b"]],filtered_messages:["Filtered",{text:"Safe",color:"red"},{translate:"sign.front_text"},["c","d"]]},back_text:{messages:["Back","","",""],filtered_messages:["Safe back","","",""]}}`
	want, err := nbt.ParseSNBT(data)
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, tag nbt.Compound) {
		t.Helper()
		for _, side := range []string{"front_text", "back_text"} {
			gotSide, _ := nbt.Get[nbt.Compound](tag, side)
			wantSide, _ := nbt.Get[nbt.Compound](want, side)
			for _, key := range []string{"messages", "filtered_messages"} {
				got, _ := nbt.Get[nbt.List](gotSide, key)
				expected, _ := nbt.Get[nbt.List](wantSide, key)
				if len(got.Items) != 4 {
					t.Errorf("%s %s.%s = %v, want four lines", where, side, key, gotSide[key])
					continue
				}
				for i := range expected.Items {
					if !nbt.Equal(nbt.ListElement(got.Items[i]), nbt.ListElement(expected.Items[i])) {
						t.Errorf("%s %s.%s line %d = %s, want %s", where, side, key, i, nbt.Stringify(got.Items[i]), nbt.Stringify(expected.Items[i]))
					}
				}
			}
		}
	}
	for i, kind := range []string{"oak_sign", "oak_hanging_sign"} {
		x := 4 + i*2
		c.ChatCommand(fmt.Sprintf("setblock %d 65 7 stone", x))
		c.ExpectCommandFeedback("commands.setblock.success", fmt.Sprint(x), "65", "7")
		c.ChatCommand(fmt.Sprintf("setblock %d 64 7 %s%s", x, kind, data))
		c.ExpectCommandFeedback("commands.setblock.success", fmt.Sprint(x), "64", "7")
		servertest.RunTicks(t, srv, 1)
		_, _, _, _, sent := c.AwaitBlockEntityData()
		check(kind+" packet", sent)
		saved, ok := srv.BlockEntityTagAt(int32(x), 64, 7)
		if !ok {
			t.Fatal("no saved sign")
		}
		check(kind+" saved", saved)
	}
	c.Disconnect()
	srv.Shutdown()
	restarted := *cfg
	srv, c = servertest.Join(t, &restarted, "Writer", servertest.OpsListing(t, "Writer"))
	servertest.HoldTicks(t, srv)
	for _, x := range []int32{4, 6} {
		saved, ok := srv.BlockEntityTagAt(x, 64, 7)
		if !ok {
			t.Fatal("no sign after restart")
		}
		check("restarted saved", saved)
		c.ChatCommand(fmt.Sprintf("data merge block %d 64 7 {is_waxed:true}", x))
		c.ExpectCommandFeedback("commands.data.block.modified", fmt.Sprint(x), "64", "7")
		servertest.RunTicks(t, srv, 1)
		_, _, _, _, sent := c.AwaitBlockEntityData()
		check("restarted packet", sent)
	}
}

func TestSignsIgnoreInvalidOrRedundantFilteredMessages(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	for i, filtered := range []string{
		``,
		`,filtered_messages:["Raw","","",""]`,
		`,filtered_messages:[{text:"Raw"},{text:""},{text:""},{text:""}]`,
		`,filtered_messages:"invalid"`,
		`,filtered_messages:["short"]`,
		`,filtered_messages:["a","b","c","d","e"]`,
		`,filtered_messages:[1,2,3,4]`,
		`,filtered_messages:[[],"","",""]`,
	} {
		x := int32(2 + i)
		srv.RunLine(fmt.Sprintf(`setblock %d 64 3 oak_sign{front_text:{messages:["Raw","","",""]%s}}`, x, filtered))
		tag, ok := srv.BlockEntityTagAt(x, 64, 3)
		if !ok {
			t.Fatal("no saved sign")
		}
		front, _ := nbt.Get[nbt.Compound](tag, "front_text")
		if _, present := front["filtered_messages"]; present {
			t.Errorf("%s saved filtered_messages: %s", filtered, nbt.Stringify(front))
		}
		messages, _, _ := signSide(t, tag, "front_text")
		if messages != [4]string{"Raw", "", "", ""} {
			t.Errorf("%s changed raw messages to %q", filtered, messages)
		}
	}
}
