package registry

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// ApplyPackRegistries preserves registry order; resolve the index once because
// song timing and comparator reads run while ticking block entities.
var jukeboxSongRegistryIndex = slices.IndexFunc(Synchronized, func(r Registry) bool {
	return r.Name == "minecraft:jukebox_song"
})

// JukeboxSong is a minecraft:jukebox_song entry's gameplay data: the
// comparator strength a jukebox holding its disc delivers, and the length the
// song plays for. The disc itself names its song through the item's
// jukebox_playable component, whose default follows the disc's name.
// vanilla: JukeboxSong; data/minecraft/jukebox_song
type JukeboxSong struct {
	Comparator  int
	LengthTicks int64
}

// JukeboxSongOf returns the gameplay data of a minecraft:jukebox_song entry,
// false for a name the registry does not carry.
func JukeboxSongOf(name string) (JukeboxSong, bool) {
	r := Current()[jukeboxSongRegistryIndex]
	if data, packed := nbt.CompoundView(r.Data[name]); packed {
		seconds, sok := nbt.FloatField(data, "length_in_seconds")
		output, ook := nbt.IntValue(data["comparator_output"])
		if !sok || !ook || seconds <= 0 || math.IsNaN(float64(seconds)) || math.IsInf(float64(seconds), 0) || output < 0 || output > 15 {
			return JukeboxSong{}, false
		}
		// vanilla: JukeboxSong.lengthInTicks multiplies as a float.
		return JukeboxSong{Comparator: int(output), LengthTicks: int64(math.Ceil(float64(seconds * 20)))}, true
	}
	song, ok := jukeboxSongs[name]
	return song, ok
}

// JukeboxSongID returns the numeric ID a minecraft:jukebox_song entry travels
// as, which is what level event 1010 names the song with, or -1 for a name the
// registry does not carry.
func JukeboxSongID(name string) int32 {
	r := Current()[jukeboxSongRegistryIndex]
	return r.ID(name)
}

var discSongs = func() map[int32]string {
	songs := make(map[int32]string, len(discSongNames))
	for name, song := range discSongNames {
		songs[MustItem(name)] = song
	}
	return songs
}()

// JukeboxSongForItem returns the jukebox_song a music disc plays, false for an
// item that is no music disc: the item report supplies
// each disc's jukebox_playable default.
func JukeboxSongForItem(itemID int32) (string, bool) {
	song, ok := discSongs[itemID]
	return song, ok
}

// JukeboxSongForStack returns the stack's song component over its item default.
// vanilla: JukeboxSong.fromStack
func JukeboxSongForStack(stack protocol.ItemStack) (string, bool) {
	if stack.Empty() || stack.Removes(protocol.DataComponentJukeboxPlayable) {
		return "", false
	}
	if id, ok := stack.JukeboxPlayableOf(); ok {
		r := Current()[jukeboxSongRegistryIndex]
		return r.Entries[id], true
	}
	return JukeboxSongForItem(stack.ItemID)
}

func withJukeboxPlayableTag(components nbt.Compound, stack protocol.ItemStack) nbt.Compound {
	if id, ok := stack.JukeboxPlayableOf(); ok {
		if components == nil {
			components = nbt.Compound{}
		}
		r := Current()[jukeboxSongRegistryIndex]
		components["minecraft:jukebox_playable"] = nbt.String(r.Entries[id])
	}
	return components
}

func readJukeboxPlayable(stack *protocol.ItemStack, value nbt.Tag) bool {
	name, ok := value.(nbt.String)
	if !ok {
		return false
	}
	id := JukeboxSongID(Qualify(string(name)))
	if id < 0 {
		return false
	}
	stack.SetJukeboxPlayable(id)
	return true
}
