package containers

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestEnchantErrorRefusesIndexedPercentTemplate(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	for _, test := range []struct{ template, want string }{
		{"%1$%", "%1$%"},
		{"prefix %s %% %1$% suffix", "prefix %s %% %1$% suffix"},
		{"%s%%", "argument%"},
	} {
		t.Run(test.template, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf(`item replace entity @s weapon.mainhand with diamond_sword[custom_name={translate:%q,with:[{text:"argument"}]}]`, test.template))
			c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot)
			c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
			c.ChatCommand("enchant @s protection")
			want := servertest.ItemFailureText("commands.enchant.failed.incompatible", protocol.Text{Text: test.want})
			if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
				text, _ := protocol.ReadText(protocol.NewReader(got))
				t.Errorf("enchant failure = %q, want %q cannot support that enchantment", text.Plain(), test.want)
			}
		})
	}
}

func TestEnchantErrorKeepsStyledTranslatedItemName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`item replace entity @s weapon.mainhand with minecraft:diamond_sword[minecraft:custom_name={translate:"chat.square_brackets",with:[{text:"suffix",bold:true}]}]`)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayContainerSlot)
	c.ChatCommand("enchant @s minecraft:protection")
	want := servertest.ItemFailureText("commands.enchant.failed.incompatible", protocol.Text{Text: "[suffix]"})
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || len(text.Extra) != 1 || text.Extra[0].Translate != "commands.enchant.failed.incompatible" {
			return false
		}
		if !bytes.Equal(data, want) {
			t.Errorf("enchant failure = %q, want [suffix] cannot support that enchantment", text.Plain())
		}
		return true
	}) {
		t.Fatal("no incompatible enchantment failure arrived")
	}
}
