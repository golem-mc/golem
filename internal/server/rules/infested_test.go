package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestInfestedStatesSwapWithTheirHostsKeepingTheAxis(t *testing.T) {
	deepslate, ok := registry.FindState("minecraft:deepslate", map[string]string{"axis": "x"})
	if !ok {
		t.Fatal("no deepslate state")
	}
	infested, ok := InfestedStateByHost(deepslate)
	if !ok {
		t.Fatal("deepslate has no infested state")
	}
	name, props, _ := registry.BlockStateName(infested)
	if name != "minecraft:infested_deepslate" || registry.ParseStateProps(props)["axis"] != "x" {
		t.Errorf("infested deepslate = %s [%s], want axis x", name, props)
	}
	if back, ok := HostStateByInfested(infested); !ok || back != deepslate {
		t.Errorf("host of the infested deepslate = %d, want %d", back, deepslate)
	}
}

func TestSilkTouchPreventsInfestation(t *testing.T) {
	if !InfestationSpawns(LootTool{}) {
		t.Error("a bare hand lets the silverfish out")
	}
	if InfestationSpawns(LootTool{Enchantments: map[string]int32{"minecraft:silk_touch": 1}}) {
		t.Error("silk touch lets the silverfish out")
	}
}
