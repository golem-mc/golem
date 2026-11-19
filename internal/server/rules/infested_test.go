package rules

import (
	"math"
	"math/rand"
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

func TestOnlyTheSevenHostsAreInfestable(t *testing.T) {
	n := 0
	for host, target := range infestedOf {
		if target == 0 {
			continue
		}
		n++
		if !IsInfestableHost(registry.Block(host).DefaultState()) || !IsInfestedBlock(target-1) {
			t.Errorf("%s / %s not recognised", registry.Block(host).Name(), (target - 1).Name())
		}
	}
	if n != 7 {
		t.Errorf("%d hosts, want 7", n)
	}
	if IsInfestableHost(registry.BlockStates["minecraft:granite"]) || IsInfestedBlock(registry.BlockStone) {
		t.Error("granite or plain stone counted")
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

func TestInfestedLaunchTurnsTheLookAboutTheVertical(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 50 {
		vx, vy, vz := InfestedLaunch(r, 0, 0.5, 1)
		if h := math.Hypot(vx, vz); math.Abs(h-0.3) > 1e-6 {
			t.Fatalf("horizontal speed %v, want 0.3 whatever the angle", h)
		}
		if math.Abs(vy-0.225) > 1e-6 {
			t.Fatalf("rise %v, want 0.5 * 0.3 * 1.5", vy)
		}
		if vz < -1e-6 {
			t.Fatalf("vz %v: a quarter turn either way never points backward", vz)
		}
		if n := InfestedCount(r); n != 1 && n != 2 {
			t.Fatalf("count %d, want 1 or 2", n)
		}
	}
}

func TestInfestedChanceIsAboutATenth(t *testing.T) {
	if InfestedChance != float32(0.1) {
		t.Errorf("chance %v, want vanilla's 0.1f", InfestedChance)
	}
}
