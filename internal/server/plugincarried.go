package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

type pluginCarriedBlockEntity struct {
	world  pluginWorld
	entity *world.CarriedBlockEntity
}

func pluginCarriedHandle(w pluginWorld, entity *world.CarriedBlockEntity) plugin.BlockEntity {
	b := pluginCarriedBlockEntity{world: w, entity: entity}
	switch entity.Kind() {
	case world.KindSkull.Name():
		return pluginSkull{b}
	case world.KindTrialSpawner.Name():
		return pluginTrialSpawner{b}
	}
	return b
}

func (b pluginCarriedBlockEntity) World() plugin.World { return b.world }
func (b pluginCarriedBlockEntity) Pos() plugin.BlockPos {
	p := b.entity.Pos()
	return plugin.BlockPos{X: int(p[0]), Y: int(p[1]), Z: int(p[2])}
}
func (b pluginCarriedBlockEntity) Type() plugin.Key {
	k, _ := plugin.ParseKey(b.entity.Kind())
	return k
}
func (b pluginCarriedBlockEntity) IsValid() bool { return b.entity.Read(nil) }
func (b pluginCarriedBlockEntity) Data() plugin.Data {
	return pluginData{read: b.entity.ReadPluginData, keys: b.entity.PluginDataKeys, write: b.entity.SetPluginData}
}
func (b pluginCarriedBlockEntity) write(change func(nbt.Compound)) {
	if b.entity.Edit(change) {
		p := b.entity.Pos()
		b.world.srv.tellBlockEntity(b.world.dim.w, p[0], p[1], p[2], b.entity.Kind(), false)
	}
}

type pluginSkull struct{ pluginCarriedBlockEntity }

func (b pluginSkull) Profile() (profile *plugin.Profile) {
	b.entity.Read(func(tag nbt.Compound) {
		p, ok := nbt.Get[nbt.Compound](tag, "profile")
		if !ok {
			if name, isName := nbt.Get[nbt.String](tag, "profile"); isName {
				p = nbt.Compound{"name": name}
			} else {
				return
			}
		}
		raw, ok := protocol.ProfileFromTag(p)
		if !ok {
			return
		}
		stack := protocol.ItemStack{Raw: []protocol.RawComponent{{Type: protocol.DataComponentProfile, Data: raw}}}
		value, ok := stack.ProfileOf()
		if !ok {
			return
		}
		profile = &plugin.Profile{ID: value.UUID, Name: value.Name}
		for _, p := range value.Properties {
			profile.Properties = append(profile.Properties, plugin.ProfileProperty{Name: p.Name, Value: p.Value, Signature: p.Signature})
		}
	})
	return profile
}

// vanilla: SkullBlockEntity.loadAdditional, ResolvableProfile.CODEC
func (b pluginSkull) SetProfile(profile *plugin.Profile) {
	var tag nbt.CompoundTag
	if profile != nil {
		value := protocol.Profile{UUID: profile.ID, HasUUID: profile.ID != uuid.Nil, Name: profile.Name, HasName: profile.Name != ""}
		for _, p := range profile.Properties {
			value.Properties = append(value.Properties, protocol.ProfileProperty{Name: p.Name, Value: p.Value, Signature: p.Signature, Signed: p.Signature != ""})
		}
		var stack protocol.ItemStack
		if !stack.SetProfile(value) {
			return
		}
		tag, _ = stack.ProfileTag()
		if _, ok := protocol.ProfileFromTag(tag.Entries()); !ok {
			return
		}
	}
	b.write(func(entry nbt.Compound) {
		if tag == nil {
			delete(entry, "profile")
		} else {
			entry["profile"] = tag
		}
	})
}
func (b pluginSkull) NoteBlockSound() (key plugin.Key) {
	b.entity.Read(func(tag nbt.Compound) {
		value, _ := nbt.Get[nbt.String](tag, "note_block_sound")
		key, _ = plugin.ParseKey(string(value))
	})
	return key
}
func (b pluginSkull) SetNoteBlockSound(key plugin.Key) {
	if key != (plugin.Key{}) {
		if parsed, err := plugin.ParseKey(key.String()); err != nil || parsed != key {
			return
		}
	}
	b.write(func(tag nbt.Compound) {
		if key == (plugin.Key{}) {
			delete(tag, "note_block_sound")
		} else {
			tag["note_block_sound"] = nbt.String(key.String())
		}
	})
}
