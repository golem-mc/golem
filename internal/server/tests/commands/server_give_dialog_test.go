package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: Dialog.CODEC, NoticeDialog.MAP_CODEC, CommonDialogData.MAP_CODEC
func TestGiveRefusesMalformedDialogClicks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))
	servertest.HoldTicks(t, srv)
	for _, click := range []string{
		`{action:"show_dialog"}`,
		`{action:"show_dialog",dialog:"minecraft:missing"}`,
		`{action:"show_dialog",dialog:{type:"notice"}}`,
		`{action:"show_dialog",dialog:{type:"missing",title:"Title"}}`,
		`{action:"show_dialog",dialog:{type:"notice",title:5}}`,
		`{action:"show_dialog",dialog:{type:"notice",title:"Title",action:{label:"Bad",width:0}}}`,
	} {
		c.ChatCommand(`give @s stone[custom_name={text:"Bad",click_event:` + click + `}]`)
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		tag, _, err := nbt.DecodeNetwork(data)
		if err != nil || !strings.Contains(nbt.Stringify(tag), "arguments.item.component.malformed") {
			t.Fatalf("%s: refusal = %v, %v", click, tag, err)
		}
		c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayContainerSlot))
	}
	c.Disconnect()
}
