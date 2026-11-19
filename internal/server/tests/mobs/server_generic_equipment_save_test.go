package mobs

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: LivingEntity.readAdditionalSaveData, EntityEquipment.setAll
func TestGenericMobEquipmentSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)

	want := map[string]nbt.Compound{}
	for i, kind := range []string{"zombie", "copper_golem"} {
		selector := "@e[type=minecraft:" + kind + ",limit=1]"
		c.ChatCommand(fmt.Sprintf("summon minecraft:%s %.1f 64 7.5 {NoAI:1b,PersistenceRequired:1b}", kind, 4.5+float64(i)*4))
		c.ChatCommand("item replace entity " + selector + ` armor.body with minecraft:stone[minecraft:equippable={slot:"body"},minecraft:custom_data={marker:"body"}]`)
		c.ChatCommand("item replace entity " + selector + ` saddle with minecraft:diamond_helmet[minecraft:equippable={slot:"saddle"},minecraft:custom_data={marker:"saddle"}]`)
		equipment, ok := nbt.Get[nbt.Compound](entityCompound(t, c, selector), "equipment")
		if !ok || len(equipment) != 2 {
			t.Fatalf("%s initial equipment = %v, want body and saddle", kind, equipment)
		}
		want[kind] = equipment
	}
	srv.FlushMobs()
	srv.Shutdown()

	restarted := *cfg
	back, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Keeper"))
	servertest.FreezeUntilHeld(t, back)
	again := connect().Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "both equipped mobs reloaded", func() bool {
		return len(back.MobIDsOfKind("minecraft:zombie")) == 1 && len(back.MobIDsOfKind("minecraft:copper_golem")) == 1
	})
	for kind, equipment := range want {
		got := entityCompound(t, again, "@e[type=minecraft:"+kind+",limit=1]")["equipment"]
		if !reflect.DeepEqual(got, equipment) {
			t.Errorf("%s equipment after restart = %v, want %v", kind, got, equipment)
		}
	}
}
