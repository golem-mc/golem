package world

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// wanderingTraderFile is the wandering trader spawner's server-wide file.
// vanilla: WanderingTraderData.TYPE
const wanderingTraderFile = "data/minecraft/wandering_trader.dat"

// TraderSpawnState is the wandering trader spawner's saved countdown: ticks
// until the next roll and its percent chance.
// vanilla: WanderingTraderData
type TraderSpawnState struct {
	SpawnDelay  int32
	SpawnChance int32
}

// WanderingTraderData is the saved TraderSpawnState, read at start and written
// by later saves when it changed.
type WanderingTraderData struct {
	root string

	mu    sync.Mutex
	state TraderSpawnState
	dirty bool
}

// NewWanderingTraderData loads root's wandering_trader.dat, with vanilla's
// defaults (24000 ticks, 25%) for a missing file or field.
// vanilla: WanderingTraderData.CODEC
func NewWanderingTraderData(root string) *WanderingTraderData {
	d := &WanderingTraderData{root: root, state: TraderSpawnState{SpawnDelay: 24000, SpawnChance: 25}}
	data, err := readSavedData(root, wanderingTraderFile)
	if err != nil {
		if !errors.Is(err, errNoSavedData) {
			slog.Warn("could not read wandering_trader.dat, using the defaults", "error", err)
		}
		return d
	}
	if v, ok := nbt.IntValue(data["spawn_delay"]); ok {
		d.state.SpawnDelay = v
	}
	if v, ok := nbt.IntValue(data["spawn_chance"]); ok {
		d.state.SpawnChance = v
	}
	return d
}

// Update runs f on the state under the lock and marks it for saving if f
// changed it.
func (d *WanderingTraderData) Update(f func(*TraderSpawnState)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	was := d.state
	f(&d.state)
	d.dirty = d.dirty || d.state != was
}

// Save writes wandering_trader.dat if the state changed since the last save.
func (d *WanderingTraderData) Save() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.dirty {
		return
	}
	data := nbt.Compound{"spawn_delay": nbt.Int(d.state.SpawnDelay), "spawn_chance": nbt.Int(d.state.SpawnChance)}
	if err := writeSavedData(d.root, wanderingTraderFile, data); err != nil {
		slog.Error("could not save wandering_trader.dat", "error", err)
		return
	}
	d.dirty = false
}
