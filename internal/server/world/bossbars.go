package world

import (
	"errors"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// bossBarsFile is vanilla's custom boss bar file, a compound of bar id to its
// packed form.
// vanilla: CustomBossEvents.TYPE
const bossBarsFile = "data/minecraft/custom_boss_events.dat"

// Boss bar colours and overlays, numbered as Boss Event numbers them and named
// as the file spells them.
const (
	BossColorWhite = 6
	// bossDefaultMax is the maximum a bar starts at and a file without one
	// reads. vanilla: CustomBossEvent.DEFAULT_MAX
	bossDefaultMax = 100
)

var (
	bossColorNames   = []string{"pink", "blue", "red", "green", "yellow", "purple", "white"}
	bossOverlayNames = []string{"progress", "notched_6", "notched_10", "notched_12", "notched_20"}
)

// bossFormatting is the chat colour each bar colour draws its name in.
// vanilla: BossEvent.BossBarColor.getFormatting
var bossFormatting = []string{"red", "blue", "dark_red", "green", "yellow", "dark_blue", "white"}

// Flag bits of the Boss Event packet's flags byte.
const (
	bossFlagDarkenScreen = 1 << iota
	bossFlagPlayMusic
	bossFlagCreateFog
)

// BossBar is a snapshot of one custom boss bar. Wire is the identity Boss Event
// packets use, made anew with each bar as vanilla draws a fresh UUID on load.
// vanilla: CustomBossEvents.CODEC
type BossBar struct {
	ID   string
	Wire uuid.UUID
	// Name is the component as the file keeps it, relayed whole so styling this
	// server's Text cannot express (bold) survives a save and reaches the
	// client.
	Name    nbt.Tag
	Visible bool
	Value   int32
	Max     int32
	// Color and Overlay are the packet's enum ids.
	Color, Overlay int32
	DarkenScreen   bool
	PlayMusic      bool
	CreateFog      bool
	// Players are the UUIDs the bar names, online or not.
	Players []uuid.UUID
}

// Fill is the fraction of the bar shown full, clamped to the bar.
// vanilla: CustomBossEvent.setValue
func (b BossBar) Fill() float32 {
	return min(max(float32(b.Value)/float32(b.Max), 0), 1)
}

// DisplayName is the bracketed name feedback uses, in the bar's colour, with
// its id as shift-click insertion and hover.
// vanilla: CustomBossEvent.getDisplayName
func (b BossBar) DisplayName() protocol.Text {
	name, ok := protocol.StyledTextOf(b.Name)
	if !ok {
		// Styling the model cannot express is dropped from the feedback line;
		// the bar keeps it.
		if c, isCompound := b.Name.(nbt.Compound); isCompound {
			text, _ := nbt.Get[nbt.String](c, "text")
			name = protocol.Text{Text: string(text)}
		}
	}
	return protocol.Text{
		Text:      "[",
		Extra:     []protocol.Text{name, {Text: "]"}},
		Color:     bossFormatting[b.Color],
		Insertion: b.ID,
		HoverText: &protocol.Text{Text: b.ID},
	}
}

// Flags is the packet's flags byte.
func (b BossBar) Flags() uint8 {
	var flags uint8
	if b.DarkenScreen {
		flags |= bossFlagDarkenScreen
	}
	if b.PlayMusic {
		flags |= bossFlagPlayMusic
	}
	if b.CreateFog {
		flags |= bossFlagCreateFog
	}
	return flags
}

// BossBarChange is what a mutation did to a bar.
type BossBarChange struct {
	// Existed is false when no bar has the id, and then nothing else is set.
	Existed bool
	// Changed is vanilla's verdict on whether the mutation moved the bar, which
	// its "unchanged" refusals test.
	Changed       bool
	Before, After BossBar
}

// BossBars is the world's custom boss bars (CustomBossEvents saved data),
// loaded when the world opens and written whole on save.
// vanilla: CustomBossEvents
type BossBars struct {
	root  string
	mu    sync.Mutex
	bars  map[string]*BossBar
	dirty bool
}

// NewBossBars loads root's bars left by this server or vanilla, skipping any
// whose name validName refuses.
func NewBossBars(root string, validName func(nbt.Tag) bool) *BossBars {
	s := &BossBars{root: root, bars: map[string]*BossBar{}}
	data, err := readSavedData(root, bossBarsFile)
	switch {
	case errors.Is(err, errNoSavedData):
		return s
	case err != nil:
		slog.Warn("could not read the custom boss bars", "error", err)
		return s
	}
	for id, tag := range data {
		one, ok := tag.(nbt.Compound)
		if !ok {
			slog.Warn("custom boss bar with no compound", "id", id)
			continue
		}
		bar, ok := unpackBossBar(id, one, validName)
		if !ok {
			slog.Warn("custom boss bar that did not read", "id", id)
			continue
		}
		s.bars[id] = bar
	}
	return s
}

// unpackBossBar reads the packed form: every field but the name is optional
// with a default, Visible's false.
// vanilla: CustomBossEvent.Packed.CODEC
func unpackBossBar(id string, one nbt.Compound, validName func(nbt.Tag) bool) (*BossBar, bool) {
	nameTag, ok := one["Name"]
	if !ok {
		return nil, false
	}
	if !validName(nameTag) {
		return nil, false
	}
	bar := &BossBar{
		ID: id, Wire: uuid.New(), Name: nameTag, Max: bossDefaultMax, Color: BossColorWhite,
		Visible:      flag(one, "Visible"),
		DarkenScreen: flag(one, "DarkenScreen"),
		PlayMusic:    flag(one, "PlayBossMusic"),
		CreateFog:    flag(one, "CreateWorldFog"),
	}
	if v, ok := nbt.Get[nbt.Int](one, "Value"); ok {
		bar.Value = int32(v)
	}
	if v, ok := nbt.Get[nbt.Int](one, "Max"); ok && v > 0 {
		bar.Max = int32(v)
	}
	if v, ok := nbt.Get[nbt.String](one, "Color"); ok {
		if i := slices.Index(bossColorNames, string(v)); i >= 0 {
			bar.Color = int32(i)
		}
	}
	if v, ok := nbt.Get[nbt.String](one, "Overlay"); ok {
		if i := slices.Index(bossOverlayNames, string(v)); i >= 0 {
			bar.Overlay = int32(i)
		}
	}
	if list, ok := nbt.Get[nbt.List](one, "Players"); ok {
		for _, item := range list.Items {
			words, ok := item.(nbt.IntArray)
			if !ok {
				continue
			}
			if id, ok := nbt.UUIDFromIntArray(words); ok && !slices.Contains(bar.Players, id) {
				bar.Players = append(bar.Players, id)
			}
		}
	}
	return bar, true
}

func flag(c nbt.Compound, key string) bool {
	v, ok := nbt.Get[nbt.Byte](c, key)
	return ok && v != 0
}

// pack writes the packed form, omitting fields at their default as
// RecordCodecBuilder's optional fields encode.
func (b *BossBar) pack() nbt.Compound {
	one := nbt.Compound{"Name": b.Name}
	setFlag := func(key string, on bool) {
		if on {
			one[key] = nbt.Byte(1)
		}
	}
	setFlag("Visible", b.Visible)
	setFlag("DarkenScreen", b.DarkenScreen)
	setFlag("PlayBossMusic", b.PlayMusic)
	setFlag("CreateWorldFog", b.CreateFog)
	if b.Value != 0 {
		one["Value"] = nbt.Int(b.Value)
	}
	if b.Max != bossDefaultMax {
		one["Max"] = nbt.Int(b.Max)
	}
	if b.Color != BossColorWhite {
		one["Color"] = nbt.String(bossColorNames[b.Color])
	}
	if b.Overlay != 0 {
		one["Overlay"] = nbt.String(bossOverlayNames[b.Overlay])
	}
	if len(b.Players) > 0 {
		list := nbt.List{Elem: nbt.TypeIntArray}
		for _, id := range b.Players {
			list.Items = append(list.Items, nbt.UUIDToIntArray(id))
		}
		one["Players"] = list
	}
	return one
}

func (b *BossBar) snapshot() BossBar {
	c := *b
	c.Players = slices.Clone(b.Players)
	return c
}

// Add creates the bar visible and empty, false if the id exists.
// vanilla: CustomBossEvents.create
func (s *BossBars) Add(id string, name nbt.Tag) (BossBar, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bar, ok := s.bars[id]; ok {
		return bar.snapshot(), false
	}
	bar := &BossBar{ID: id, Wire: uuid.New(), Name: name, Visible: true, Max: bossDefaultMax, Color: BossColorWhite}
	s.bars[id] = bar
	s.dirty = true
	return bar.snapshot(), true
}

// Get is a snapshot of the bar the id names.
func (s *BossBars) Get(id string) (BossBar, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bar, ok := s.bars[id]
	if !ok {
		return BossBar{}, false
	}
	return bar.snapshot(), true
}

// IDs lists the bars, sorted.
func (s *BossBars) IDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Sorted(maps.Keys(s.bars))
}

// All is a snapshot of every bar.
func (s *BossBars) All() []BossBar {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BossBar, 0, len(s.bars))
	for _, id := range slices.Sorted(maps.Keys(s.bars)) {
		out = append(out, s.bars[id].snapshot())
	}
	return out
}

// change runs fn on the bar under the lock; fn reports whether it moved the
// bar.
func (s *BossBars) change(id string, fn func(b *BossBar) bool) BossBarChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	bar, ok := s.bars[id]
	if !ok {
		return BossBarChange{}
	}
	before := bar.snapshot()
	changed := fn(bar)
	if changed {
		s.dirty = true
	}
	return BossBarChange{Existed: true, Changed: changed, Before: before, After: bar.snapshot()}
}

// SetPlayers makes the bar's players exactly those given; it moved only if a
// UUID was added or dropped.
// vanilla: CustomBossEvent.setPlayers
func (s *BossBars) SetPlayers(id string, players []uuid.UUID) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		var next []uuid.UUID
		for _, p := range players {
			if !slices.Contains(next, p) {
				next = append(next, p)
			}
		}
		if len(next) == len(b.Players) && !slices.ContainsFunc(next, func(p uuid.UUID) bool { return !slices.Contains(b.Players, p) }) {
			return false
		}
		b.Players = next
		return true
	})
}

// SetValue moves the bar's value; it moved only where the value differs.
// vanilla: BossBarCommands.setValue
func (s *BossBars) SetValue(id string, value int32) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Value == value {
			return false
		}
		b.Value = value
		return true
	})
}

// SetMax sets the bar's maximum; it moved only where the maximum differs.
// vanilla: CustomBossEvent.setMax
func (s *BossBars) SetMax(id string, maxValue int32) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Max == maxValue {
			return false
		}
		b.Max = maxValue
		return true
	})
}

// SetVisible shows or hides the bar.
// vanilla: BossBarCommands.setVisible
func (s *BossBars) SetVisible(id string, visible bool) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Visible == visible {
			return false
		}
		b.Visible = visible
		return true
	})
}

// Remove deletes the bar; Before is the bar as it stood.
// vanilla: CustomBossEvents.remove
func (s *BossBars) Remove(id string) BossBarChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	bar, ok := s.bars[id]
	if !ok {
		return BossBarChange{}
	}
	delete(s.bars, id)
	s.dirty = true
	snap := bar.snapshot()
	return BossBarChange{Existed: true, Changed: true, Before: snap, After: snap}
}

// SetName renames the bar; it moved only where the component differs.
// vanilla: ServerBossEvent.setName
func (s *BossBars) SetName(id string, name nbt.Tag) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if bossNameEqual(b.Name, name) {
			return false
		}
		b.Name = name
		return true
	})
}

// bossNameEqual compares resolved components, so a plain string and the same
// text as a compound are equal.
// vanilla: BossBarCommands.setName
func bossNameEqual(a, b nbt.Tag) bool {
	ta, okA := protocol.TextOf(a)
	tb, okB := protocol.TextOf(b)
	if okA && okB {
		return protocol.TextEqual(ta, tb)
	}
	return reflect.DeepEqual(a, b)
}

// SetColor sets the bar's colour, a Boss Event colour id.
// vanilla: ServerBossEvent.setColor
func (s *BossBars) SetColor(id string, color int32) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Color == color {
			return false
		}
		b.Color = color
		return true
	})
}

// SetOverlay sets the bar's style, a Boss Event overlay id.
// vanilla: ServerBossEvent.setOverlay
func (s *BossBars) SetOverlay(id string, overlay int32) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Overlay == overlay {
			return false
		}
		b.Overlay = overlay
		return true
	})
}

// Save writes the bars when something has changed one since the last save.
func (s *BossBars) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	data := nbt.Compound{}
	for id, bar := range s.bars {
		data[id] = bar.pack()
	}
	if err := writeSavedData(s.root, bossBarsFile, data); err != nil {
		slog.Error("could not save the custom boss bars", "error", err)
		return
	}
	s.dirty = false
}

// Update changes a live bar, rejecting a handle to an earlier bar with the same id.
func (s *BossBars) Update(id string, wire uuid.UUID, fn func(*BossBar) bool) BossBarChange {
	return s.change(id, func(b *BossBar) bool {
		if b.Wire != wire {
			return false
		}
		return fn(b)
	})
}
