package scoreboard

import (
	"maps"

	"github.com/golem-mc/golem/nbt"
)

// File is where the board is saved, under the world directory.
const File = "data/minecraft/scoreboard.dat"

// Dirty says the board changed since it was last packed.
func (b *Board) Dirty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dirty
}

// MarkDirty has the next save write the board, for a save that failed.
func (b *Board) MarkDirty() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dirty = true
}

// Pack returns scoreboard.dat's data compound and marks the board clean.
// vanilla: ScoreboardSaveData.Packed
func (b *Board) Pack() nbt.Compound {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dirty = false
	objectives := nbt.List{Elem: nbt.TypeCompound}
	for _, o := range b.objectives {
		entry := nbt.Compound{
			"Name":                nbt.String(o.Name),
			"CriteriaName":        nbt.String(o.Criteria.Name),
			"DisplayName":         o.DisplayName,
			"RenderType":          nbt.String(o.Render.String()),
			"display_auto_update": boolByte(o.AutoUpdate),
		}
		if o.Format != nil {
			entry["format"] = packFormat(o.Format)
		}
		objectives.Items = append(objectives.Items, entry)
	}
	scores := nbt.List{Elem: nbt.TypeCompound}
	for _, holder := range b.holders {
		for _, o := range b.objectives {
			score, ok := b.scores[holder][o]
			if !ok {
				continue
			}
			entry := nbt.Compound{
				"Name":      nbt.String(holder),
				"Objective": nbt.String(o.Name),
				"Score":     nbt.Int(score.Value),
				"Locked":    boolByte(score.Locked),
			}
			if score.Display != nil {
				entry["display"] = score.Display
			}
			if score.Format != nil {
				entry["format"] = packFormat(score.Format)
			}
			scores.Items = append(scores.Items, entry)
		}
	}
	slots := nbt.Compound{}
	for slot, o := range b.display {
		if o != nil {
			slots[SlotNames[slot]] = nbt.String(o.Name)
		}
	}
	return nbt.Compound{
		"Objectives":   objectives,
		"PlayerScores": scores,
		"DisplaySlots": slots,
		"Teams":        b.packTeams(),
	}
}

// Load fills an empty board from scoreboard.dat, skipping unreadable entries.
// It sends no events: no client has joined yet.
// vanilla: ScoreboardSaveData.loadFrom
func (b *Board) Load(data nbt.Compound) {
	b.mu.Lock()
	defer b.mu.Unlock()
	objectives, _ := nbt.Get[nbt.List](data, "Objectives")
	for _, tag := range objectives.Items {
		entry, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		name, _ := nbt.Get[nbt.String](entry, "Name")
		// vanilla: Objective.Packed (optionalFieldOf, the default left out)
		criteriaName := nbt.String("dummy")
		if v, ok := nbt.Get[nbt.String](entry, "CriteriaName"); ok {
			criteriaName = v
		}
		criteria, ok := LookupCriteria(string(criteriaName))
		if name == "" || !ok || b.find(string(name)) != nil {
			continue
		}
		o := &objective{Objective{ID: b.newID(), Name: string(name), Criteria: criteria, Render: RenderInteger}}
		o.DisplayName = entry["DisplayName"]
		if o.DisplayName == nil {
			o.DisplayName = nbt.String(name)
		}
		if render, ok := nbt.Get[nbt.String](entry, "RenderType"); ok {
			if r, ok := ParseRenderType(string(render)); ok {
				o.Render = r
			}
		}
		autoUpdate, _ := nbt.ByteValue(entry["display_auto_update"])
		o.AutoUpdate = autoUpdate != 0
		o.Format = unpackFormat(entry["format"])
		b.objectives = append(b.objectives, o)
	}
	b.countStatObjectives()
	scores, _ := nbt.Get[nbt.List](data, "PlayerScores")
	for _, tag := range scores.Items {
		entry, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		holder, _ := nbt.Get[nbt.String](entry, "Name")
		objectiveName, _ := nbt.Get[nbt.String](entry, "Objective")
		o := b.find(string(objectiveName))
		if o == nil {
			continue
		}
		score, _ := b.scoreFor(string(holder), o)
		score.Value, _ = nbt.IntValue(entry["Score"])
		locked, _ := nbt.ByteValue(entry["Locked"])
		score.Locked = locked != 0
		score.Display = entry["display"]
		score.Format = unpackFormat(entry["format"])
	}
	slots, _ := nbt.Get[nbt.Compound](data, "DisplaySlots")
	for name, tag := range slots {
		slot, ok := LookupSlot(name)
		objectiveName, isString := tag.(nbt.String)
		if ok && isString {
			if o := b.find(string(objectiveName)); o != nil {
				b.display[slot] = o
				b.tracked[o] = true
			}
		}
	}
	teams, _ := nbt.Get[nbt.List](data, "Teams")
	b.loadTeams(teams)
	b.dirty = false
}

func boolByte(v bool) nbt.Byte {
	if v {
		return 1
	}
	return 0
}

// formatTypes are the number formats' saved type ids, indexed by FormatKind.
var formatTypes = []string{"minecraft:blank", "minecraft:styled", "minecraft:fixed"}

// packFormat saves a number format: its type, a styled one's style inline, a
// fixed one's component under value.
// vanilla: NumberFormatTypes.CODEC
func packFormat(f *NumberFormat) nbt.Compound {
	out := nbt.Compound{}
	if f.Kind == FormatStyled {
		maps.Copy(out, f.Style)
	}
	if f.Kind == FormatFixed {
		out["value"] = f.Fixed
	}
	out["type"] = nbt.String(formatTypes[f.Kind])
	return out
}

func unpackFormat(tag nbt.Tag) *NumberFormat {
	c, ok := tag.(nbt.Compound)
	if !ok {
		return nil
	}
	kind, _ := nbt.Get[nbt.String](c, "type")
	switch string(kind) {
	case "minecraft:blank", "blank":
		return &NumberFormat{Kind: FormatBlank}
	case "minecraft:styled", "styled":
		style := nbt.Compound{}
		for k, v := range c {
			if k != "type" {
				style[k] = v
			}
		}
		return &NumberFormat{Kind: FormatStyled, Style: style}
	case "minecraft:fixed", "fixed":
		if value, ok := c["value"]; ok {
			return &NumberFormat{Kind: FormatFixed, Fixed: value}
		}
	}
	return nil
}
