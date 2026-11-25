package world

import (
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// Data versions of the fixes a 1.18.2 to 1.20.2 chunk gets.
const (
	signDoubleSidedVersion = 3439 // vanilla: DataFixers.addFixers (BlockEntitySignDoubleSidedEditableTextFix)
	signFilteredVersion    = 3564 // vanilla: DataFixers.addFixers (DropInvalidSignDataFix)
	brushableBlockVersion  = 3438 // vanilla: DataFixers.addFixers (Rename Suspicious Sand to Brushable Block)
	potSherdsVersion       = 3448 // vanilla: DataFixers.addFixers (DecoratedPotFieldRenameFix)
	shardRenameVersion     = 3447 // vanilla: DataFixers.addFixers (Pottery shard item renaming)
	grassRenameVersion     = 3692 // vanilla: DataFixers.addFixers (Rename grass block to short_grass)
)

// storedItemVersion is the data version from which stored item stacks are
// componentized; a chunk older than it that holds a stack is not upgraded.
// vanilla: DataFixers.addFixers (ItemStackComponentizationFix)
const storedItemVersion = 3818

// holdsLegacyItem reports whether a block entity list holds an item stack in
// the pre-1.20.5 shape (id and Count), which this server cannot upgrade; a
// chunk saved over it would lose the item.
func holdsLegacyItem(tag nbt.Tag) bool {
	switch v := tag.(type) {
	case nbt.List:
		for _, item := range v.Items {
			if holdsLegacyItem(item) {
				return true
			}
		}
	case nbt.CompoundTag:
		entries := v.Entries()
		if _, hasID := entries["id"].(nbt.String); hasID {
			if _, legacy := entries["Count"]; legacy {
				return true
			}
		}
		for _, child := range entries {
			if holdsLegacyItem(child) {
				return true
			}
		}
	}
	return false
}

// potShardRenames are the pottery shard item names of before 3447 and their
// current names.
// vanilla: DataFixers.addFixers (Pottery shard renaming)
var potShardRenames = map[string]string{
	"minecraft:pottery_shard_archer":  "minecraft:archer_pottery_shard",
	"minecraft:pottery_shard_prize":   "minecraft:prize_pottery_shard",
	"minecraft:pottery_shard_arms_up": "minecraft:arms_up_pottery_shard",
	"minecraft:pottery_shard_skull":   "minecraft:skull_pottery_shard",
}

// fixOldBlockEntity applies the block entity fixes between a chunk's data
// version and the one decodeChunkNBT reads.
func fixOldBlockEntity(entry nbt.CompoundTag, version int32) {
	if version == 0 || version >= grassRenameVersion {
		return
	}
	tag := entry.Entries()
	id, _ := nbt.Get[nbt.String](tag, "id")
	switch string(id) {
	case "minecraft:suspicious_sand":
		if version < brushableBlockVersion {
			tag["id"] = nbt.String("minecraft:brushable_block")
		}
	case "minecraft:sign", "minecraft:hanging_sign":
		fixSignText(tag, version)
	case "minecraft:decorated_pot":
		fixPotSherds(tag, version)
	}
}

// signTextFields are the pre-3439 fields the front and back text replace.
var signTextFields = []string{"Text1", "Text2", "Text3", "Text4", "FilteredText1", "FilteredText2", "FilteredText3", "FilteredText4", "Color", "GlowingText"}

// emptyComponent is the empty text component as 1.20.2 stores it.
// vanilla: LegacyComponentDataFixUtils.createEmptyComponent
const emptyComponent = `""`

func signLines(tag nbt.Compound, prefix string) (lines []nbt.Tag, found bool) {
	for i := 1; i <= 4; i++ {
		line, ok := tag[prefix+string(rune('0'+i))]
		lines = append(lines, line)
		found = found || ok
	}
	return lines, found
}

func messageList(items []nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeString, Items: items} }

// fixSignText moves a sign's Text1..4 into front_text and back_text, and drops
// filtered lines that merely repeat the unfiltered ones.
// vanilla: BlockEntitySignDoubleSidedEditableTextFix, DropInvalidSignDataFix
func fixSignText(tag nbt.Compound, version int32) {
	if version < signDoubleSidedVersion {
		lines, _ := signLines(tag, "Text")
		for i, line := range lines {
			if line == nil {
				lines[i] = nbt.String(emptyComponent)
			}
		}
		color, ok := tag["Color"]
		if !ok {
			color = nbt.String("black")
		}
		glowing, ok := tag["GlowingText"]
		if !ok {
			glowing = nbt.Byte(0)
		}
		front := nbt.Compound{"messages": messageList(lines), "color": color, "has_glowing_text": glowing}
		if filtered, found := signLines(tag, "FilteredText"); found {
			for i, line := range filtered {
				if line == nil {
					filtered[i] = lines[i]
				}
			}
			front["filtered_messages"] = messageList(filtered)
		}
		empty := []nbt.Tag{nbt.String(emptyComponent), nbt.String(emptyComponent), nbt.String(emptyComponent), nbt.String(emptyComponent)}
		tag["front_text"] = front
		tag["back_text"] = nbt.Compound{"messages": messageList(empty), "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)}
		tag["is_waxed"] = nbt.Byte(0)
		tag["_filtered_correct"] = nbt.Byte(1)
	}
	if version < signFilteredVersion {
		if correct, _ := nbt.Get[nbt.Byte](tag, "_filtered_correct"); correct != 0 {
			delete(tag, "_filtered_correct")
		} else {
			for _, side := range []string{"front_text", "back_text"} {
				if text, ok := nbt.Get[nbt.Compound](tag, side); ok {
					dropRepeatedFilteredLines(text)
				}
			}
		}
	}
	for _, key := range signTextFields {
		delete(tag, key)
	}
}

// dropRepeatedFilteredLines fills a filtered line that is empty with its
// unfiltered one and drops the list when it then equals the messages.
// vanilla: DropInvalidSignDataFix.fixText
func dropRepeatedFilteredLines(text nbt.Compound) {
	filtered, ok := nbt.Get[nbt.List](text, "filtered_messages")
	if !ok {
		return
	}
	messages, _ := nbt.Get[nbt.List](text, "messages")
	same := len(filtered.Items) == len(messages.Items)
	items := make([]nbt.Tag, len(filtered.Items))
	for i, line := range filtered.Items {
		var fallback nbt.Tag = nbt.String(emptyComponent)
		if i < len(messages.Items) {
			fallback = messages.Items[i]
		}
		if s, ok := line.(nbt.String); ok && string(s) == emptyComponent {
			line = fallback
		}
		items[i] = line
		same = same && i < len(messages.Items) && nbt.Equal(line, messages.Items[i])
	}
	if same {
		delete(text, "filtered_messages")
		return
	}
	filtered.Items = items
	text["filtered_messages"] = filtered
}

// fixPotSherds renames a decorated pot's shards to sherds along with the item
// names in them.
// vanilla: DecoratedPotFieldRenameFix, ItemRenameFix (v3438, v3447)
func fixPotSherds(tag nbt.Compound, version int32) {
	if version >= potSherdsVersion {
		return
	}
	shards, ok := nbt.Get[nbt.List](tag, "shards")
	if !ok {
		return
	}
	delete(tag, "shards")
	for i, item := range shards.Items {
		name, ok := item.(nbt.String)
		if !ok {
			continue
		}
		if version < brushableBlockVersion {
			if to, ok := potShardRenames[string(name)]; ok {
				name = nbt.String(to)
			}
		}
		if version < shardRenameVersion {
			name = nbt.String(strings.Replace(string(name), "_pottery_shard", "_pottery_sherd", 1))
		}
		shards.Items[i] = name
	}
	tag["sherds"] = shards
}
