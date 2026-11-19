package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestARiddenHorseClopsThenGallops(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider := connect().Join("Rider")
	listener := connect().Join("Listener")
	rider.MoveToGround(2.5, 64, 5.5, true)
	listener.MoveToGround(2.5, 64, 7.5, true)
	rider.Settle()
	servertest.HoldTicks(t, srv)
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(4); x <= 16; x++ {
		srv.SetBlock(x, 63, 5, stone)
	}
	rider.ChatCommand(`summon minecraft:horse 5.5 64 5.5 {Tame:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	id := rider.ExpectMobSpawn(protocol.EntityTypeHorse).EntityID
	rider.UseOn(id)
	rider.Settle()
	var sounds []string
	var nearby []string
	record := func(heard *[]string) func(int32, []byte) {
		return func(packet int32, data []byte) {
			if packet == protocol.IDPlaySoundEffect {
				switch sound := servertest.SoundOf(data); sound {
				case "minecraft:entity.horse.step_wood", "minecraft:entity.horse.gallop":
					*heard = append(*heard, sound)
				}
			}
		}
	}
	for tick := range 40 {
		x := 5.5 + float64(min(tick+1, 40-tick-1))*0.5
		moveVehicle(rider, x, 64, 5.5, -90)
		rider.SettleAndCollect(record(&sounds))
		listener.SettleAndCollect(record(&nearby))
		servertest.RunTicks(t, srv, 1)
	}
	clops, gallops := 0, 0
	for _, sound := range sounds {
		if sound == "minecraft:entity.horse.step_wood" {
			if gallops > 0 {
				t.Fatal("clop after galloping began")
			}
			clops++
		} else {
			gallops++
		}
	}
	if clops != 5 || gallops == 0 {
		t.Fatalf("heard %d clops and %d gallops, want five clops then gallops", clops, gallops)
	}
	if !slices.Equal(sounds, nearby) {
		t.Fatalf("rider heard %v, nearby listener heard %v", sounds, nearby)
	}
}
