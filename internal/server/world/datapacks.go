package world

import "github.com/golem-mc/golem/nbt"

// DataPacks returns the pack ids level.dat lists as enabled, in layer order,
// and as disabled. ok is false for a level.dat without a DataPacks compound.
// vanilla: DataPackConfig.CODEC
func (w *World) DataPacks() (enabled, disabled []string, ok bool) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	packs, ok := nbt.Get[nbt.Compound](w.level.raw, "DataPacks")
	if !ok {
		return nil, nil, false
	}
	return stringList(packs, "Enabled"), stringList(packs, "Disabled"), true
}

// SetDataPacks replaces the DataPacks compound level.dat is written with.
// vanilla: MinecraftServer.reloadResources, WorldData.setDataConfiguration
func (w *World) SetDataPacks(enabled, disabled []string) {
	w.timeMu.Lock()
	defer w.timeMu.Unlock()
	if w.level.raw == nil {
		w.level.raw = nbt.Compound{}
	}
	w.level.raw["DataPacks"] = nbt.Compound{
		"Enabled":  stringTags(enabled),
		"Disabled": stringTags(disabled),
	}
}

func stringList(c nbt.Compound, key string) []string {
	list, _ := nbt.Get[nbt.List](c, key)
	var out []string
	for _, item := range list.Items {
		if s, ok := item.(nbt.String); ok {
			out = append(out, string(s))
		}
	}
	return out
}

func stringTags(ids []string) nbt.List {
	items := make([]nbt.Tag, len(ids))
	for i, id := range ids {
		items[i] = nbt.String(id)
	}
	return nbt.List{Elem: nbt.TypeString, Items: items}
}

// SavedDataPacks is the pack selection level.dat under dir lists, ok false for
// a new world or a level.dat without one; the world is not opened.
func SavedDataPacks(dir string) (enabled, disabled []string, ok bool) {
	level, err := LoadLevel(dir)
	if err != nil || !level.existed {
		return nil, nil, false
	}
	packs, ok := nbt.Get[nbt.Compound](level.raw, "DataPacks")
	if !ok {
		return nil, nil, false
	}
	return stringList(packs, "Enabled"), stringList(packs, "Disabled"), true
}
