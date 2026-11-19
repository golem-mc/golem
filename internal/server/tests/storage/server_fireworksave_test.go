package storage

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
)

// TestFireworksSurviveASaveAndReload saves a crafted-looking rocket and its
// star to the player file and reads them back after a restart: the file
// holds the components in vanilla's shape and the stacks return whole.
// vanilla: Fireworks.CODEC, FireworkExplosion.CODEC
func TestFireworksSurviveASaveAndReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, connect := servertest.Start(t, cfg)

	burst := protocol.FireworkExplosion{
		Shape:      1, // large_ball
		Colors:     []int32{11743532},
		FadeColors: []int32{2437522},
		Trail:      true,
	}
	rocket := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:firework_rocket"), Count: 3}
	rocket.SetRaw(protocol.FireworksRawComponent(2, []protocol.FireworkExplosion{burst}))
	star := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:firework_star"), Count: 1}
	star.SetRaw(burst.RawComponent())

	c := connect().Join("Pyro")
	srv.GiveSlot("Pyro", player.HotbarSlot, rocket)
	srv.GiveSlot("Pyro", player.HotbarSlot+1, star)
	c.Disconnect()
	srv.Shutdown()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Pyro"))
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok || len(items.Items) != 2 {
		t.Fatalf("Inventory = %v, %v; want the rocket and the star", items, ok)
	}
	byID := map[string]nbt.Compound{}
	for _, item := range items.Items {
		entry, _ := item.(nbt.Compound)
		id, _ := nbt.Get[nbt.String](entry, "id")
		components, _ := nbt.Get[nbt.Compound](entry, "components")
		byID[string(id)] = components
	}
	fireworks, ok := nbt.Get[nbt.Compound](byID["minecraft:firework_rocket"], "minecraft:fireworks")
	if !ok {
		t.Fatalf("the rocket's components = %v, want minecraft:fireworks", byID["minecraft:firework_rocket"])
	}
	if got, _ := nbt.Get[nbt.Byte](fireworks, "flight_duration"); got != 2 {
		t.Errorf("flight_duration = %v, want 2b", fireworks["flight_duration"])
	}
	explosions, _ := nbt.Get[nbt.List](fireworks, "explosions")
	if len(explosions.Items) != 1 {
		t.Fatalf("explosions = %v, want one", fireworks["explosions"])
	}
	wantBurst := nbt.Compound{
		"shape":       nbt.String("large_ball"),
		"colors":      nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(11743532)}},
		"fade_colors": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(2437522)}},
		"has_trail":   nbt.Byte(1),
	}
	if got := explosions.Items[0]; !reflect.DeepEqual(got, wantBurst) {
		t.Errorf("the rocket's explosion = %v, want %v", got, wantBurst)
	}
	if got := byID["minecraft:firework_star"]["minecraft:firework_explosion"]; !reflect.DeepEqual(got, wantBurst) {
		t.Errorf("the star's explosion = %v, want %v", got, wantBurst)
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Pyro")
	again.Configure()
	slots := again.Inventory()
	if got := slots[player.HotbarSlot]; !got.Equal(rocket) {
		t.Errorf("the rocket came back as %+v, want %+v", got, rocket)
	}
	if got := slots[player.HotbarSlot+1]; !got.Equal(star) {
		t.Errorf("the star came back as %+v, want %+v", got, star)
	}
	again.Disconnect()
}
