package blocks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestSignsResolveOpComponentsOnLoad(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Writer"))
	c := connect().Join("Writer")
	c.MoveTo(4.5, 64, 8.5, 180, 0)
	near := connect().Join("NearSign")
	near.MoveTo(12.5, 64, 8.5, 180, 0)
	near.Settle()
	c.Settle()
	c.ChatCommand(`scoreboard objectives add points dummy`)
	objective := protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{{Text: "points"}}}
	c.ExpectCommandFeedbackText("commands.scoreboard.objectives.add.success", objective)
	c.ChatCommand(`scoreboard players set NearSign points 37`)
	holder := servertest.PlayerDisplayComponent("NearSign")
	holder.HoverEntity, holder.HoverText = nil, &protocol.Text{Text: "NearSign"}
	c.ExpectCommandFeedbackText("commands.scoreboard.players.set.success.single", objective, holder, protocol.Text{Number: new(int64(37))})
	c.ChatCommand(`setblock 12 64 7 chest{CustomName:"Below"}`)
	c.ExpectCommandFeedback("commands.setblock.success", "12", "64", "7")

	for _, kind := range []string{"oak_sign", "oak_hanging_sign"} {
		for _, flag := range []string{"", "allow_op_features:0b,", "allow_op_features:1b,"} {
			t.Run(kind+"/"+flag, func(t *testing.T) {
				// @p must use the sign's centre rather than the command sender;
				// @s must have no executor. The NBT position is relative to the sign.
				c.ChatCommand(fmt.Sprintf(`setblock 12 65 7 %s{%sfront_text:{messages:[{selector:"@p"},{score:{name:"@p",objective:"points"}},{nbt:"CustomName",block:"~ ~-1 ~",interpret:true},{selector:"@s"}]},back_text:{color:"blue",has_glowing_text:true,messages:[{selector:"@p",color:"red",bold:true,extra:[{text:"!"}]},{score:{name:"@a",objective:"points"}},"",""]}}`, kind, flag))
				c.ExpectCommandFeedback("commands.setblock.success", "12", "65", "7")
				x, y, z, _, sent := c.AwaitBlockEntityData()
				if x != 12 || y != 65 || z != 7 {
					t.Fatalf("block entity at %d %d %d, want 12 65 7", x, y, z)
				}
				saved, ok := srv.BlockEntityTagAt(12, 65, 7)
				if !ok {
					t.Fatal("no saved sign")
				}
				for where, tag := range map[string]nbt.Compound{"packet": sent, "saved": saved} {
					front := signComponentLines(t, tag, "front_text")
					back := signComponentLines(t, tag, "back_text")
					if flag == "allow_op_features:1b," {
						for i, want := range []string{"NearSign", "37", "Below", ""} {
							if got := componentPlain(t, front[i]); got != want {
								t.Errorf("%s front line %d = %q, want %q (%s)", where, i, got, want, nbt.Stringify(front[i]))
							}
							if raw := nbt.Stringify(front[i]); strings.Contains(raw, "selector:") || strings.Contains(raw, "score:") || strings.Contains(raw, "nbt:") {
								t.Errorf("%s front line %d unresolved: %s", where, i, raw)
							}
						}
						if got := componentPlain(t, back[0]); got != "NearSign!" {
							t.Errorf("%s back line = %q, want NearSign!", where, got)
						}
					} else {
						for i, key := range []string{"selector", "score", "nbt", "selector"} {
							if line, _ := front[i].(nbt.Compound); line[key] == nil {
								t.Errorf("%s line %d resolved without op features: %#v", where, i, front[i])
							}
						}
					}
					side, _ := tag["back_text"].(nbt.Compound)
					if side["color"] != nbt.String("blue") || side["has_glowing_text"] != nbt.Byte(1) {
						t.Errorf("%s lost side color or glow: %#v", where, side)
					}
					styled, _ := back[0].(nbt.Compound)
					if styled["color"] != nbt.String("red") || styled["bold"] != nbt.Byte(1) {
						t.Errorf("%s lost line style: %#v", where, styled)
					}
					// An ambiguous score selector throws; resolveLine keeps the
					// original component instead of replacing it with empty text.
					failed, _ := back[1].(nbt.Compound)
					if _, ok := failed["score"]; !ok {
						t.Errorf("%s lost failed score component: %#v", where, back[1])
					}
				}
				c.ChatCommand(`setblock 12 65 7 air`)
				c.ExpectCommandFeedback("commands.setblock.success", "12", "65", "7")
			})
		}
	}
}

func TestDataLoadsSignComponentsBeforeBroadcast(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Writer", servertest.OpsListing(t, "Writer"))
	c.ChatCommand(`setblock 4 65 7 oak_sign{front_text:{messages:[{selector:"@p"},"","",""],filtered_messages:["Filtered","","",""]}}`)
	c.ExpectCommandFeedback("commands.setblock.success", "4", "65", "7")
	c.AwaitBlockEntityData()
	c.ChatCommand(`data merge block 4 65 7 {allow_op_features:1b}`)
	c.ExpectCommandFeedback("commands.data.block.modified", "4", "65", "7")
	_, _, _, _, sent := c.AwaitBlockEntityData()
	saved, ok := srv.BlockEntityTagAt(4, 65, 7)
	if !ok {
		t.Fatal("no saved sign")
	}
	for where, tag := range map[string]nbt.Compound{"packet": sent, "saved": saved} {
		front, _ := nbt.Get[nbt.Compound](tag, "front_text")
		if _, present := front["filtered_messages"]; present {
			t.Errorf("%s kept separate filtered lines after the mutable resolution pass", where)
		}
		line := signComponentLines(t, tag, "front_text")[0]
		if got := componentPlain(t, line); got != "Writer" {
			t.Errorf("%s line = %q, want Writer (%s)", where, got, nbt.Stringify(line))
		}
	}
}

func signComponentLines(t *testing.T, tag nbt.Compound, side string) [4]nbt.Tag {
	t.Helper()
	text, _ := nbt.Get[nbt.Compound](tag, side)
	list, _ := nbt.Get[nbt.List](text, "messages")
	if len(list.Items) != 4 {
		t.Fatalf("%s messages = %#v, want four", side, list)
	}
	var lines [4]nbt.Tag
	for i, line := range list.Items {
		lines[i] = nbt.ListElement(line)
	}
	return lines
}

func componentPlain(t *testing.T, tag nbt.Tag) string {
	t.Helper()
	text, ok := protocol.RawTextOf(tag)
	if !ok {
		t.Fatalf("invalid text component: %#v", tag)
	}
	return text.Plain()
}
