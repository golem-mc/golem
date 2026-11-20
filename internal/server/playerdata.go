package server

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Player files in vanilla's layout (one gzipped NBT file per UUID in the
// world directory), so worlds move between this server and vanilla.
const (
	// playerDataDir is relative to the world directory.
	playerDataDir = "players/data"

	// playerStatsDir holds one <uuid>.json statistics file per player,
	// relative to the world directory. vanilla: LevelResource.PLAYER_STATS_DIR
	playerStatsDir = "players/stats"

	// playerAdvancementsDir holds one <uuid>.json advancement file per player,
	// relative to the world directory.
	// vanilla: LevelResource.PLAYER_ADVANCEMENTS_DIR
	playerAdvancementsDir = "players/advancements"

	// The previous file is kept as a backup, and saves go to a temp file first
	// so a crash mid-write leaves the old file whole.
	playerFileSuffix   = ".dat"
	playerBackupSuffix = ".dat_old"
	playerTempSuffix   = ".dat_tmp"

	// dataVersion is 26.3's world_version, from the server jar's version.json;
	// the game upgrades files with an older one.
	dataVersion = 5023

	// maxPlayerFile caps decompressed size against a crafted endlessly
	// expanding file; real ones are a few kilobytes.
	maxPlayerFile = 16 << 20
)

// World versions of the data fixes applied on load: v4312 moved worn items
// from Inventory into an equipment tag, v4314 moved SpawnX/Y/Z and their
// neighbours into the respawn compound, and v4548 split its angle into yaw
// and pitch and defaulted a missing dimension. vanilla: DataFixers (PlayerEquipmentFix at
// v4312, InlineBlockPosFormatFix at v4314, PlayerRespawnDataFix at v4548)
const (
	playerEquipmentVersion      = 4312
	inlineBlockPosFormatVersion = 4314
	playerRespawnDataFixVersion = 4548
)

// playerStore reads and writes player files. mu serialises saves, since a
// leave and a shutdown can save the same player at once.
type playerStore struct {
	dir             string
	statsDir        string
	advancementsDir string
	// seq numbers the saves as they read the player.
	seq atomic.Uint64
	mu  sync.Mutex
	// written is the seq of each player's save last on disk, guarded by mu.
	written map[uuid.UUID]uint64
}

// playerWrite is one player's three files as a save read them: the
// uncompressed NBT, and the encoded statistics and advancements, nil for a
// player without them. seq orders it against the player's other saves.
type playerWrite struct {
	id                  uuid.UUID
	seq                 uint64
	state               []byte
	stats, advancements []byte
}

func newPlayerStore(level string) *playerStore {
	return &playerStore{
		dir:             filepath.Join(level, playerDataDir),
		statsDir:        filepath.Join(level, playerStatsDir),
		advancementsDir: filepath.Join(level, playerAdvancementsDir),
		written:         map[uuid.UUID]uint64{},
	}
}

// statsPath is a player's statistics file.
func (s *playerStore) statsPath(id uuid.UUID) string {
	return filepath.Join(s.statsDir, id.String()+".json")
}

// loadStats reads a player's statistics; missing or damaged reads as empty.
// vanilla: PlayerList.getPlayerStats
func (s *playerStore) loadStats(id uuid.UUID) (*player.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return player.LoadStats(s.statsPath(id))
}

// advancementsPath is a player's advancement progress file.
func (s *playerStore) advancementsPath(id uuid.UUID) string {
	return filepath.Join(s.advancementsDir, id.String()+".json")
}

// loadAdvancements reads advancement progress; missing or damaged reads as
// none. vanilla: PlayerList.getPlayerAdvancements
func (s *playerStore) loadAdvancements(id uuid.UUID) (*advancement.Progress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return advancement.Load(advancement.Current(), s.advancementsPath(id))
}

// path is a player's file with one of the three suffixes.
func (s *playerStore) path(id uuid.UUID, suffix string) string {
	return filepath.Join(s.dir, id.String()+suffix)
}

// list answers the UUID of every player data file in the player data directory.
func (s *playerStore) list() ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		stem, ok := strings.CutSuffix(entry.Name(), playerFileSuffix)
		if !ok {
			continue
		}
		id, err := uuid.Parse(stem)
		if err != nil || id.String() != stem {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// load reads a player's file. A missing current file (the crash window
// between save's two renames) or an unreadable one falls back to the
// backup; a damaged one is first copied to a timestamped corrupted name as
// evidence. A player with neither returns nil, nil.
// vanilla: PlayerDataStorage.load
func (s *playerStore) load(id uuid.UUID) (nbt.Compound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := readPlayerFile(s.path(id, playerFileSuffix))
	if err == nil {
		return root, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		if copyErr := copyCorruptPlayerFile(s.path(id, playerFileSuffix), s.path(id, corruptedPlayerSuffix())); copyErr != nil {
			slog.Warn("could not keep a copy of the damaged player file", "error", copyErr)
		}
	}

	backup, backupErr := readPlayerFile(s.path(id, playerBackupSuffix))
	switch {
	case backupErr == nil:
		return backup, fmt.Errorf("%w (fell back to %s)", err, playerBackupSuffix)
	case errors.Is(err, fs.ErrNotExist) && errors.Is(backupErr, fs.ErrNotExist):
		return nil, nil
	case errors.Is(backupErr, fs.ErrNotExist):
		return nil, err
	default:
		return nil, backupErr
	}
}

// corruptedPlayerSuffix names a damaged file's timestamped copy, so the
// next save does not lose the evidence.
func corruptedPlayerSuffix() string {
	return "_corrupted_" + time.Now().Format("2006-01-02_15-04-05") + playerFileSuffix
}

// copyCorruptPlayerFile copies a damaged player file to dst, leaving the
// original for a later load.
func copyCorruptPlayerFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readPlayerFile reads one gzipped NBT file; a missing file returns
// fs.ErrNotExist.
func readPlayerFile(path string) (nbt.Compound, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("decompress %s: %w", path, err)
	}
	defer gz.Close()

	// Read one byte past the limit to tell a capped file from one ending there.
	data, err := io.ReadAll(io.LimitReader(gz, maxPlayerFile+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxPlayerFile {
		return nil, fmt.Errorf("read %s: larger than %d bytes", path, maxPlayerFile)
	}

	_, tag, err := nbt.DecodeOrderedFields(data, "Inventory", "EnderItems", "equipment", "ShoulderEntityLeft", "ShoulderEntityRight")
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		return nil, fmt.Errorf("parse %s: root tag is a %s, want a compound", path, tag.Type())
	}
	return root, nil
}

// write writes the player file, keeping the previous as backup, then the
// statistics and advancements, each one the save read; errors are joined. A save
// read before the one last written is dropped, so an auto-save finishing
// beside the tick never overwrites the file the player wrote on leaving.
// vanilla: PlayerList.save
func (s *playerStore) write(w playerWrite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.seq < s.written[w.id] {
		return nil
	}
	s.written[w.id] = w.seq
	var err error
	if w.state != nil {
		err = s.writePlayerFile(w.id, w.state)
	}
	if w.stats != nil {
		err = errors.Join(err, player.WriteStatsFile(s.statsPath(w.id), w.stats))
	}
	if w.advancements != nil {
		err = errors.Join(err, advancement.WriteFile(s.advancementsPath(w.id), w.advancements))
	}
	return err
}

// writePlayerFile compresses and writes a player's NBT, with mu held.
func (s *playerStore) writePlayerFile(id uuid.UUID, data []byte) error {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(data); err != nil {
		return fmt.Errorf("compress player data: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("compress player data: %w", err)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", s.dir, err)
	}

	// Write to temp, then rotate final to backup and temp to final, so the
	// loaded file is never half-written.
	final, backup, temp := s.path(id, playerFileSuffix), s.path(id, playerBackupSuffix), s.path(id, playerTempSuffix)
	// Sync the temp file before the rename, or a power loss right after could
	// leave an empty player file.
	// vanilla: NbtIo.writeCompressed uses StandardOpenOption.SYNC
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if _, err := file.Write(compressed.Bytes()); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", temp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write %s: %w", temp, err)
	}
	if err := os.Remove(backup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", backup, err)
	}
	if err := os.Rename(final, backup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("rename %s: %w", final, err)
	}
	if err := os.Rename(temp, final); err != nil {
		return fmt.Errorf("rename %s: %w", temp, err)
	}
	return nil
}

// playerState is the compound to save: the loaded one with the modelled
// fields overwritten, everything else carried through so a vanilla world's
// unmodelled data survives. spawn is the personal respawn point, nil for
// none; pearls are the flying ender pearls, written as vanilla's
// ender_pearls list; book is the recipe book, saved under recipeBook;
// portalCooldown is the ticks before a portal can take the player; deathTime
// is the corpse's elapsed ticks.
func playerState(loaded nbt.Compound, p *player.Player, flying bool, dimension string,
	xpSeed int32, spawn *respawnPos, vehicle nbt.Compound, pearls []nbt.Compound,
	book *inventory.RecipeBook, portalCooldown, deathTime int32) nbt.Compound {
	items := make([]nbt.Tag, len(pearls))
	for i, pearl := range pearls {
		items[i] = pearl
	}
	return orderedPlayerState(loaded, p, flying, dimension, xpSeed, spawn, vehicle, items, book, portalCooldown, deathTime, playerSaveExtras{}).Entries()
}

type playerSaveExtras struct{ warden, raid, post nbt.Tag }

// orderedPlayerState follows Entity, LivingEntity, Player and ServerPlayer save order.
// vanilla: Entity.saveWithoutId, Player.addAdditionalSaveData, ServerPlayer.addAdditionalSaveData
func orderedPlayerState(loaded nbt.Compound, p *player.Player, flying bool, dimension string,
	xpSeed int32, spawn *respawnPos, vehicle nbt.Compound, pearls []nbt.Tag,
	book *inventory.RecipeBook, portalCooldown, deathTime int32, extra playerSaveExtras) *nbt.OrderedCompound {
	state := nbt.NewOrderedCompound(48)
	kept := maps.Clone(loaded)
	put := func(key string, value nbt.Tag) {
		delete(kept, key)
		if value != nil {
			state.Set(key, value)
		}
	}
	pos, v := p.Position(), p.Vitals()
	writeEntityBaseTag(state, "", pos.X, pos.Y, pos.Z, 0, 0, 0, pos.Yaw, pos.Pitch, p.UUID, pos.OnGround, int(max(portalCooldown, 0)), v.FireTicks, 0, false)
	put("fall_distance", nbt.Double(v.FallDistance))
	put("Air", nbt.Short(clampToShort(v.AirSupply)))
	put("Invulnerable", boolTag(nbt.BoolField(loaded, "Invulnerable")))
	// These Entity fields are carried rather than simulated on a player.
	look := p.Appearance()
	put("CustomName", nil)
	put("CustomNameVisible", nil)
	customName{wire: look.Name, visible: look.NameVisible}.put(state)
	for _, f := range []struct {
		key string
		set bool
	}{{"Silent", look.Silent}, {"NoGravity", look.NoGravity}, {"Glowing", look.Glowing}} {
		put(f.key, nil)
		if f.set {
			put(f.key, boolTag(true))
		}
	}
	put("TicksFrozen", nil)
	if v.TicksFrozen > 0 {
		put("TicksFrozen", nbt.Int(int32(v.TicksFrozen)))
	}
	put("HasVisualFire", loaded["HasVisualFire"])
	putTags(state, p.Tags())
	delete(kept, "Tags")
	put("data", loaded["data"])
	put("Health", nbt.Float(v.Health))
	living := entity.LivingState{HurtTime: v.HurtTime, DeathTime: int(deathTime), AbsorptionAmount: float32(v.Absorption), ImpulseContextResetGraceTime: v.ImpulseGraceTicks, FallFlying: p.Appearance().FallFlying}
	living.SaveWithAttributes(state, func() {
		put("current_explosion_impact_pos", nil)
		if at := v.ImpulseImpact; at != nil {
			put("current_explosion_impact_pos", vec3Tag(at[0], at[1], at[2]))
		}
		p.Attributes(func(a *player.Attributes) {
			list, _ := nbt.Get[nbt.List](loaded, "attributes")
			put("attributes", *a.SavePlayer(&list))
		})
		put("active_effects", nil)
		if effects := effect.Encode(p.Effects()); effects != nil {
			put("active_effects", *effects)
		}
	})
	put("equipment", nil)
	p.Inventory(func(inv *player.Inventory) { inv.StoreEquipment(state) })
	put("locator_bar_icon", nil)
	if icon := p.LocatorIcon(); icon.HasData() {
		put("locator_bar_icon", icon.Tag())
	}
	// Retain the loaded version claim so carried legacy fields still run through
	// vanilla's fixers; a file without a claim belongs to this server.
	claim := int32(dataVersion)
	if version, ok := nbt.Get[nbt.Int](loaded, "DataVersion"); ok && version != 0 {
		claim = int32(version)
	}
	put("DataVersion", nbt.Int(claim))
	p.Inventory(func(inv *player.Inventory) {
		inv.StoreSlots(state)
		put("SelectedItemSlot", nbt.Int(inv.Selected))
		if claim < playerEquipmentVersion {
			inv.StoreLegacyEquipment(state)
		}
	})
	put("SleepTimer", nbt.Short(clampToShort(p.Sleep().Ticks)))
	put("XpP", nbt.Float(v.ExperienceProgress))
	put("XpLevel", nbt.Int(v.ExperienceLevel))
	put("XpTotal", nbt.Int(v.ExperienceTotal))
	put("XpSeed", nbt.Int(xpSeed))
	put("Score", nbt.Int(v.Score))
	put("foodLevel", nbt.Int(v.Food))
	put("foodTickTimer", nbt.Int(v.FoodTicks))
	put("foodSaturationLevel", nbt.Float(v.Saturation))
	put("foodExhaustionLevel", nbt.Float(v.Exhaustion))
	abilities := nbt.NewOrderedCompound(7)
	creative, spectator := v.GameMode == protocol.GameModeCreative, v.GameMode == protocol.GameModeSpectator
	abilities.Set("invulnerable", boolTag(creative || spectator))
	abilities.Set("flying", boolTag(spectator || creative && flying))
	abilities.Set("mayfly", boolTag(creative || spectator))
	abilities.Set("instabuild", boolTag(creative))
	abilities.Set("mayBuild", boolTag(creative || v.GameMode == protocol.GameModeSurvival))
	previous, _ := nbt.Get[nbt.Compound](loaded, "abilities")
	for _, speed := range []struct {
		key      string
		fallback nbt.Float
	}{{"flySpeed", 0.05}, {"walkSpeed", 0.1}} {
		value := previous[speed.key]
		if value == nil {
			value = speed.fallback
		}
		abilities.Set(speed.key, value)
	}
	for _, key := range nbt.HashOrderedKeys(previous) {
		if _, ok := abilities.Entries()[key]; !ok {
			abilities.Set(key, previous[key])
		}
	}
	put("abilities", abilities)
	p.Inventory(func(inv *player.Inventory) { inv.StoreEnder(state) })
	put("LastDeathLocation", nil)
	if d := v.LastDeath; d.Set {
		put("LastDeathLocation", nbt.Compound{"dimension": nbt.String(d.Dimension), "pos": nbt.IntArray{d.X, d.Y, d.Z}})
	}
	put("warden_spawn_tracker", extra.warden)
	put("playerGameType", nbt.Int(v.GameMode))
	put("previousPlayerGameType", nil)
	if v.HasPreviousGameMode {
		put("previousPlayerGameType", nbt.Int(v.PreviousGameMode))
	}
	put("seenCredits", boolTag(p.SeenCredits()))
	put("entered_nether_pos", nil)
	if at := v.EnteredNether; at != nil {
		put("entered_nether_pos", vec3Tag(at[0], at[1], at[2]))
	}
	put("RootVehicle", nil)
	if vehicle != nil {
		root := nbt.NewOrderedCompound(2)
		root.Set("Attach", vehicle["Attach"])
		root.Set("Entity", vehicle["Entity"])
		put("RootVehicle", root)
	}
	put("recipeBook", nil)
	if book != nil {
		put("recipeBook", book.OrderedTag(loaded))
	}
	put("Dimension", nbt.String(dimension))
	put("respawn", nil)
	if spawn != nil {
		spawn.store(state, claim)
		for _, key := range []string{"SpawnX", "SpawnY", "SpawnZ", "SpawnAngle", "SpawnDimension", "SpawnForced"} {
			delete(kept, key)
		}
	}
	put("spawn_extra_particles_on_fall", boolTag(v.SpawnExtraParticlesOnFall))
	put("raid_omen_position", extra.raid)
	put("ender_pearls", nil)
	if len(pearls) > 0 {
		put("ender_pearls", nbt.List{Elem: nbt.TypeCompound, Items: pearls})
	}
	put("post_effects", extra.post)
	sh := p.Shoulders()
	for _, shoulder := range []struct {
		key string
		tag nbt.CompoundTag
	}{{"ShoulderEntityLeft", sh.Left}, {"ShoulderEntityRight", sh.Right}} {
		put(shoulder.key, nil)
		if hasShoulder(shoulder.tag) {
			put(shoulder.key, shoulder.tag)
		}
	}
	// Unmodelled loaded fields survive without overriding the native writer.
	for _, key := range nbt.HashOrderedKeys(kept) {
		if _, written := state.Entries()[key]; !written {
			state.Set(key, kept[key])
		}
	}
	return state
}

// storedVec3 reads a list of three doubles. vanilla: Vec3.CODEC
func storedVec3(root nbt.Compound, key string) ([3]float64, bool) {
	var xyz [3]float64
	list, ok := nbt.Get[nbt.List](root, key)
	if !ok || len(list.Items) != 3 {
		return xyz, false
	}
	for i, item := range list.Items {
		value, ok := item.(nbt.Double)
		if !ok {
			return xyz, false
		}
		xyz[i] = float64(value)
	}
	return xyz, true
}

// vec3Tag writes a list of three doubles. vanilla: Vec3.CODEC
func vec3Tag(x, y, z float64) nbt.List {
	return nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(x), nbt.Double(y), nbt.Double(z)}}
}

// storedPosition reads a saved position; missing or out of the world is
// false, and the player starts at spawn.
func storedPosition(root nbt.Compound) (player.Position, bool) {
	xyz, ok := storedVec3(root, "Pos")
	if !ok || !inWorld(xyz[0], xyz[1], xyz[2]) {
		return player.Position{}, false
	}
	pos := player.Position{X: xyz[0], Y: xyz[1], Z: xyz[2]}

	// A bad rotation is ignored rather than refusing the position. The yaw is
	// kept raw: the join teleport sends it as stored, and the client's accept
	// is where vanilla wraps it. The pitch is clamped at load, where vanilla
	// clamps it.
	if rotation, ok := nbt.Get[nbt.List](root, "Rotation"); ok && len(rotation.Items) == 2 {
		yaw, yawOK := rotation.Items[0].(nbt.Float)
		pitch, pitchOK := rotation.Items[1].(nbt.Float)
		if yawOK && pitchOK && finite(float64(yaw), float64(pitch)) {
			pos.Yaw = float32(yaw)
			pos.Pitch = clampBodyPitch(float32(pitch))
		}
	}
	if onGround, ok := nbt.Get[nbt.Byte](root, "OnGround"); ok {
		pos.OnGround = onGround != 0
	}
	return pos, true
}

// compound is the respawn point as vanilla writes it in the player file:
// the position as an int array, the dimension, the angles, and the forced
// flag only where vanilla writes it (a /spawnpoint point).
// vanilla: ServerPlayer.addAdditionalSaveData
func (r respawnPos) compound() *nbt.OrderedCompound {
	out := nbt.NewOrderedCompound(5)
	out.Set("dimension", nbt.String(r.dim))
	out.Set("pos", nbt.IntArray{r.pos.X, r.pos.Y, r.pos.Z})
	out.Set("yaw", nbt.Float(r.yaw))
	out.Set("pitch", nbt.Float(r.pitch))
	if r.forced {
		out.Set("forced", nbt.Byte(1))
	}
	return out
}

// store writes the respawn point in the shape of the file's DataVersion,
// which its fixers upgrade: before v4314 top-level SpawnX/Y/Z with the
// angle, before v4548 the compound with one angle, from playerRespawnDataFixVersion the
// compound with yaw and pitch. vanilla: DataFixers
func (r respawnPos) store(root nbt.CompoundTag, version int32) {
	for _, key := range []string{
		"respawn", "SpawnX", "SpawnY", "SpawnZ", "SpawnAngle", "SpawnDimension", "SpawnForced",
	} {
		root.Delete(key)
	}
	if version < inlineBlockPosFormatVersion {
		root.Set("SpawnX", nbt.Int(r.pos.X))
		root.Set("SpawnY", nbt.Int(r.pos.Y))
		root.Set("SpawnZ", nbt.Int(r.pos.Z))
		root.Set("SpawnAngle", nbt.Float(r.yaw))
		root.Set("SpawnDimension", nbt.String(r.dim))
		if r.forced {
			root.Set("SpawnForced", nbt.Byte(1))
		}
		return
	}
	if version < playerRespawnDataFixVersion {
		respawn := nbt.Compound{
			"pos":       nbt.IntArray{r.pos.X, r.pos.Y, r.pos.Z},
			"dimension": nbt.String(r.dim),
			"angle":     nbt.Float(r.yaw),
		}
		if r.forced {
			respawn["forced"] = nbt.Byte(1)
		}
		root.Set("respawn", respawn)
		return
	}
	root.Set("respawn", r.compound())
}

// fixPlayerRespawn upgrades a loaded file's respawn data to the current
// version, the respawn part of the game's fixers: before v4314, top-level SpawnX/Y/Z
// with angle, dimension and forced flag move into the respawn compound;
// before v4548, its angle splits into yaw and pitch and a missing dimension
// defaults to the overworld. A file at or past the current version is untouched.
// vanilla: DataFixTypes.PLAYER.updateToCurrentVersion
func fixPlayerRespawn(root nbt.Compound) {
	version, _ := nbt.Get[nbt.Int](root, "DataVersion")
	if int(version) >= dataVersion {
		return
	}
	if int(version) < inlineBlockPosFormatVersion {
		migrateSpawnFields(root)
	}
	if int(version) < playerRespawnDataFixVersion {
		migrateRespawnAngle(root)
	}
}

// migrateSpawnFields is InlineBlockPosFormatFix's player half: the old
// top-level fields become a respawn compound (position as an int array,
// angle, dimension and forced flag renamed).
// vanilla: InlineBlockPosFormatFix.fixPlayer
func migrateSpawnFields(root nbt.Compound) {
	x, xok := nbt.Get[nbt.Int](root, "SpawnX")
	y, yok := nbt.Get[nbt.Int](root, "SpawnY")
	z, zok := nbt.Get[nbt.Int](root, "SpawnZ")
	if !xok || !yok || !zok {
		return
	}
	respawn := nbt.Compound{"pos": nbt.IntArray{int32(x), int32(y), int32(z)}}
	if angle, ok := nbt.Get[nbt.Float](root, "SpawnAngle"); ok {
		respawn["angle"] = nbt.Float(angle)
	}
	if dimension, ok := nbt.Get[nbt.String](root, "SpawnDimension"); ok {
		respawn["dimension"] = nbt.String(dimension)
	}
	if forced, ok := nbt.Get[nbt.Byte](root, "SpawnForced"); ok {
		respawn["forced"] = nbt.Byte(forced)
	}
	for _, key := range []string{"SpawnX", "SpawnY", "SpawnZ", "SpawnAngle", "SpawnDimension", "SpawnForced"} {
		delete(root, key)
	}
	root["respawn"] = respawn
}

// migrateRespawnAngle is PlayerRespawnDataFix: the compound's angle becomes
// yaw, pitch 0, and a missing dimension defaults to the overworld. A
// compound already naming yaw keeps its rotation, so only an old angle is
// folded. vanilla: PlayerRespawnDataFix
func migrateRespawnAngle(root nbt.Compound) {
	respawn, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		return
	}
	respawn = maps.Clone(respawn)
	if _, ok := nbt.Get[nbt.String](respawn, "dimension"); !ok {
		respawn["dimension"] = nbt.String(world.DimensionName)
	}
	if _, ok := nbt.Get[nbt.Float](respawn, "yaw"); !ok {
		angle, _ := nbt.Get[nbt.Float](respawn, "angle")
		respawn["yaw"] = nbt.Float(angle)
		respawn["pitch"] = nbt.Float(0)
		delete(respawn, "angle")
	}
	root["respawn"] = respawn
}

// storedRespawn reads the saved respawn point from vanilla's compound, by
// tag type rather than the game's codec: a well-shaped compound reads back
// whatever its dimension and distance, and angles beyond the codec's
// [-180, 180] and [-90, 90] are kept (no respawn here uses them, but they
// survive to the next save), while a missing field or wrong tag type reads
// as none and the next save drops it.
// vanilla: ServerPlayer.readAdditionalSaveData
func storedRespawn(root nbt.Compound) (respawnPos, bool) {
	compound, ok := nbt.Get[nbt.Compound](root, "respawn")
	if !ok {
		return respawnPos{}, false
	}
	name, ok := nbt.Get[nbt.String](compound, "dimension")
	if !ok {
		return respawnPos{}, false
	}
	pos, ok := nbt.Get[nbt.IntArray](compound, "pos")
	if !ok || len(pos) != 3 {
		return respawnPos{}, false
	}
	yaw, ok := nbt.Get[nbt.Float](compound, "yaw")
	if !ok {
		return respawnPos{}, false
	}
	pitch, ok := nbt.Get[nbt.Float](compound, "pitch")
	if !ok {
		return respawnPos{}, false
	}
	forced, _ := nbt.Get[nbt.Byte](compound, "forced")
	return respawnPos{
		pos:    world.SpawnPos{X: pos[0], Y: pos[1], Z: pos[2]},
		dim:    string(name),
		yaw:    float32(yaw),
		pitch:  float32(pitch),
		forced: forced != 0,
	}, true
}

// boolTag encodes a flag as a byte; NBT has no boolean tag.
func boolTag(v bool) nbt.Byte {
	if v {
		return 1
	}
	return 0
}

// restore loads a returning player's file into c.saved so unmodelled
// fields survive the next save. It runs under the player list lock, before
// the player is shown or the join reads them, so nobody sees them at the
// spawn they move from and the join describes the restored dimension.
// Without a readable file the player keeps play()'s placement.
func (c *client) restore() {
	stats, err := c.srv.players.loadStats(c.player.UUID)
	if err != nil {
		c.log.Warn("could not load player statistics", "error", err)
	}
	// The first Award Statistics after a join sends every counter.
	// vanilla: PlayerList.placeNewPlayer
	stats.MarkAllDirty()
	c.stats.Store(stats)

	progress, err := c.srv.players.loadAdvancements(c.player.UUID)
	if err != nil {
		c.log.Warn("could not load player advancements", "error", err)
	}
	progress.SetVeto(c.pluginCriterionVeto)
	c.advancements.Store(progress)

	root, err := c.srv.players.load(c.player.UUID)
	if err != nil {
		c.log.Warn("could not load player data", "error", err)
	}
	if c.srv.plugins != nil {
		c.pluginFirstPlayed, _ = c.srv.pluginPlayedTimes(c.player.UUID, root)
		if c.pluginFirstPlayed.IsZero() {
			c.pluginFirstPlayed = time.Now()
		}
	}
	// Store the recipe book before anything reads it: the join sends it and
	// every save writes it, which leave and shutdown can reach without a file.
	c.recipeBook.Store(inventory.RecipeBookOf(root))
	if root == nil {
		return
	}
	c.saved = root
	c.loadPostEffects(root)
	// vanilla: ServerPlayer.readAdditionalSaveData (warden_spawn_tracker)
	tracker, _ := nbt.Get[nbt.Compound](root, "warden_spawn_tracker")
	c.srv.wardenMu.Lock()
	c.wardenTracker = rules.WardenSpawnTrackerFromTag(tracker)
	c.srv.wardenMu.Unlock()
	// Fix old respawn data before anything reads it, as the game upgrades a
	// file on load.
	fixPlayerRespawn(root)

	loaded := player.LoadInventory(root)
	c.player.Inventory(func(inv *player.Inventory) { *inv = loaded })

	// An unknown dimension name falls back to the overworld.
	if name, ok := nbt.Get[nbt.String](root, "Dimension"); ok {
		c.dim.Store(c.srv.dimensionByName(string(name)))
	}

	// The join's abilities packet reads the restored flying flag.
	if abilities, ok := nbt.Get[nbt.Compound](root, "abilities"); ok {
		if flying, ok := nbt.Get[nbt.Byte](abilities, "flying"); ok {
			c.flying.Store(flying != 0)
		}
	}

	// Take the respawn point before handling the dead below, whose respawn
	// needs it, unlike the position restored at the end.
	if spawn, ok := storedRespawn(root); ok {
		c.spawnPoint.Store(&spawn)
	}

	// The boat the file has them riding, held for the post-join tick that
	// reseats them.
	if saved, ok := nbt.Get[nbt.Compound](root, "RootVehicle"); ok {
		c.vehicleOnJoin.Store(&saved)
	}

	// Flying pearls from the file, held for the post-join tick that respawns
	// them; the vanish rule is checked there, whatever the returning body's
	// vitals. vanilla: ThrownEnderpearl.tick's gamerule check
	if pearls := storedPearlTags(root); pearls != nil {
		c.pearlsOnJoin.Store(&pearls)
	}

	// Values are trusted as vanilla trusts them, clamped only to the possible:
	// mode 0..3, health, food and saturation in [0, max], exhaustion not
	// negative. The health cap waits for the effects below, whose health boost
	// raises it.
	v := c.player.Vitals()
	if mode, ok := nbt.Get[nbt.Int](root, "playerGameType"); ok && mode >= 0 && mode <= 3 {
		v.GameMode = int32(mode)
	}
	if mode, ok := nbt.Get[nbt.Int](root, "previousPlayerGameType"); ok && mode >= 0 && mode <= 3 {
		v.PreviousGameMode, v.HasPreviousGameMode = int32(mode), true
	}
	if loc, ok := nbt.Get[nbt.Compound](root, "LastDeathLocation"); ok {
		dim, okDim := nbt.Get[nbt.String](loc, "dimension")
		pos, okPos := nbt.Get[nbt.IntArray](loc, "pos")
		if okDim && okPos && len(pos) == 3 {
			v.LastDeath = player.DeathLocation{Set: true, Dimension: string(dim), X: pos[0], Y: pos[1], Z: pos[2]}
		}
	}
	// vanilla: ServerPlayer.readAdditionalSaveData (raid_omen_position)
	if pos, ok := nbt.Get[nbt.IntArray](root, "raid_omen_position"); ok && len(pos) == 3 {
		c.srv.setRaidOmenPosition(c.player.UUID, raid.Pos{X: int(pos[0]), Y: int(pos[1]), Z: int(pos[2])})
	}
	// vanilla: ServerPlayer.calculateGameModeForNewPlayer
	if c.srv.forceGameMode.Load() {
		v.GameMode = c.srv.defaultGameMode.Load()
	}
	if health, ok := nbt.Get[nbt.Float](root, "Health"); ok {
		v.Health = math.Max(float64(health), 0)
	}
	// Zero health means the player left on the death screen.
	v.Dead = v.Health <= 0
	if food, ok := nbt.Get[nbt.Int](root, "foodLevel"); ok {
		v.Food = int32(math.Min(math.Max(float64(food), 0), rules.MaxFood))
	}
	if sat, ok := nbt.Get[nbt.Float](root, "foodSaturationLevel"); ok {
		v.Saturation = math.Min(math.Max(float64(sat), 0), rules.MaxFood)
	}
	if exhausted, ok := nbt.Get[nbt.Float](root, "foodExhaustionLevel"); ok {
		v.Exhaustion = math.Max(float64(exhausted), 0)
	}
	// vanilla: FoodData.readAdditionalSaveData
	if timer, ok := nbt.IntField(root, "foodTickTimer"); ok {
		v.FoodTicks = int(timer)
	}
	// Air and fire as Entity.load reads them, and the fall, falling back to
	// the legacy FallDistance float. vanilla: Entity.load
	if air, ok := nbt.IntField(root, "Air"); ok {
		v.AirSupply = int(air)
	}
	if fire, ok := nbt.ShortField(root, "Fire"); ok {
		v.FireTicks = int(fire)
	}
	if cooldown, ok := nbt.IntField(root, "PortalCooldown"); ok {
		c.portalCooldown.Store(max(cooldown, 0))
	}
	v.FallDistance = max(0, nbt.DoubleFieldOr(root, "fall_distance",
		nbt.DoubleFieldOr(root, "FallDistance", 0)))
	if entered, ok := storedVec3(root, "entered_nether_pos"); ok {
		v.EnteredNether = &entered
	}
	// vanilla: LivingEntity.readAdditionalSaveData
	var living entity.LivingState
	living.LoadFrom(root)
	v.HurtTime = living.HurtTime
	v.ImpulseGraceTicks = living.ImpulseContextResetGraceTime
	c.deathTicks.Store(int32(living.DeathTime))
	look := c.player.Appearance()
	look.FallFlying = living.FallFlying
	name := customNameOfTag(root)
	look.Name, look.NameVisible = name.wire, name.visible
	look.Silent, look.NoGravity, look.Glowing = nbt.BoolField(root, "Silent"), nbt.BoolField(root, "NoGravity"), nbt.BoolField(root, "Glowing")
	c.fallFlying.Store(living.FallFlying)
	c.player.SetAppearance(look)
	if frozen, ok := nbt.IntField(root, "TicksFrozen"); ok {
		v.TicksFrozen = int(frozen)
	}
	if impact, ok := storedVec3(root, "current_explosion_impact_pos"); ok {
		v.ImpulseImpact = &impact
	}
	// vanilla: ServerPlayer.readAdditionalSaveData
	v.SpawnExtraParticlesOnFall = nbt.BoolField(root, "spawn_extra_particles_on_fall")
	if progress, ok := nbt.Get[nbt.Float](root, "XpP"); ok && progress > 0 {
		v.ExperienceProgress = float32(progress)
	}
	if level, ok := nbt.Get[nbt.Int](root, "XpLevel"); ok {
		v.ExperienceLevel = int(level)
	}
	if total, ok := nbt.Get[nbt.Int](root, "XpTotal"); ok {
		v.ExperienceTotal = int(total)
	}
	if score, ok := nbt.Get[nbt.Int](root, "Score"); ok {
		v.Score = int32(score)
	}
	// A stored seed of 0 is rerolled so players who never enchanted do not
	// share their offers.
	// vanilla: Player.readAdditionalSaveData
	seed, _ := nbt.Get[nbt.Int](root, "XpSeed")
	if seed == 0 {
		seed = nbt.Int(int32(c.srv.levelRng.Uint32()))
	}
	c.xpSeed.Store(int32(seed))
	if absorption, ok := nbt.Get[nbt.Float](root, "AbsorptionAmount"); ok {
		v.Absorption = math.Max(float64(absorption), 0)
	}
	// The file's scoreboard tags; absent means none.
	// vanilla: Entity's read of Tags
	c.player.SetTags(tagsOfTag(root))
	// vanilla: LivingEntity.readAdditionalSaveData's locator_bar_icon
	if icon, ok := nbt.Get[nbt.Compound](root, "locator_bar_icon"); ok {
		c.player.SetLocatorIcon(player.LocatorIconFromTag(icon))
	}
	// Seen credits from the file, so the next end exit skips them.
	// vanilla: ServerPlayer.readAdditionalSaveData's seenCredits
	if seen, ok := nbt.Get[nbt.Byte](root, "seenCredits"); ok {
		c.player.SetSeenCredits(seen != 0)
	}
	c.player.SetShoulders(shouldersFromTag(root))
	// The file's sleep counter (a sleeper's count or a wake's residue), so a
	// body that left mid-count resumes; one that left awake reads 0, and lying
	// down is not restored. vanilla: Player.readAdditionalSaveData's SleepTimer
	if timer, ok := nbt.ShortField(root, "SleepTimer"); ok {
		c.player.SetSleep(player.Sleep{Ticks: int(timer)})
	}
	if deadOnArrival := v.Dead; deadOnArrival {
		// A dead body returns dead where it fell: the empty bar alone shows the
		// death screen, and the respawn creates a fresh body. The experience bar is
		// normalised as for a live respawn (the join's Set Experience reads it), but
		// health stays empty and effects are not restored; the respawn replaces
		// both. vanilla: LivingEntity.readAdditionalSaveData
		hurtTime, frozen := v.HurtTime, v.TicksFrozen
		keepInv := c.srv.world.BoolRule(world.RuleKeepInventory)
		v, _ = rules.RespawnVitalsKeeping(v, keepInv, keepInv)
		v.HurtTime, v.TicksFrozen = hurtTime, frozen
		v.Health = 0
		v.Dead = true
		c.player.SetVitals(v)
	} else {
		c.player.SetVitals(v)
		c.player.UpdateBody(func(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
			if list, ok := nbt.Get[nbt.List](root, "active_effects"); ok {
				*effects = effect.Decode(list)
			}
			if list, ok := nbt.Get[nbt.List](root, "attributes"); ok {
				a.Load(list)
			}
			rules.ClampHealth(v, effects, a)
		})
	}

	pos, ok := storedPosition(root)
	if !ok {
		return
	}
	c.player.SetPosition(pos)
	c.log.Debug("restored player", "x", pos.X, "y", pos.Y, "z", pos.Z)
}

// savePlayerData writes the player's file, with the boat and pearls given,
// without touching windows: /save-all must leave an online player's open
// container, grid and cursor alone. Errors are logged, not returned.
// vanilla: PlayerList.saveAll
func (c *client) savePlayerData(vehicle nbt.Compound, pearls []nbt.Tag) {
	w, ok := c.snapshotPlayerData(vehicle, pearls)
	if !ok {
		return
	}
	if err := c.srv.players.write(w); err != nil {
		c.log.Error("could not save player data", "error", err)
	}
}

// snapshotPlayerData reads and encodes what savePlayerData writes, so the
// files can be written later; false for a client without a player. An encode
// error is logged and leaves that file out.
func (c *client) snapshotPlayerData(vehicle nbt.Compound, pearls []nbt.Tag) (playerWrite, bool) {
	if c.player == nil {
		return playerWrite{}, false
	}
	state := c.playerSaveState(vehicle, pearls)
	if c.srv.plugins != nil {
		state.Set("golem:first_played", nbt.Long(c.pluginFirstPlayed.UnixMilli()))
		state.Set("golem:last_seen", nbt.Long(c.pluginSeenTime().UnixMilli()))
	}
	w := playerWrite{id: c.player.UUID, seq: c.srv.players.seq.Add(1)}
	var err error
	// The root tag is unnamed, as the game writes it.
	if w.state, err = nbt.Encode("", state); err != nil {
		c.log.Error("could not save player data", "error", err)
	}
	if stats := c.stats.Load(); stats != nil {
		if w.stats, err = stats.Encode(dataVersion); err != nil {
			c.log.Error("could not save player statistics", "error", err)
		}
	}
	if progress := c.advancements.Load(); progress != nil {
		if w.advancements, err = progress.Encode(dataVersion); err != nil {
			c.log.Error("could not save player advancements", "error", err)
		}
	}
	return w, true
}

// savePlayer saves a departing player with the ridden boat and the pearls
// the departure removed. Grids and cursor drop where the player stands, as
// vanilla's Player.remove does on disconnect; none are saved.
func (c *client) savePlayer(vehicle nbt.Compound, pearls []nbt.Tag) {
	if c.player == nil {
		return
	}
	if err := c.closeWindows(false, true, true); err != nil {
		c.log.Debug("closing the window of a departing player failed", "error", err)
	}
	// vanilla: PlayerList.remove
	c.awardStat(player.Custom(registry.CustomStatLeaveGame), 1)
	c.savePlayerData(vehicle, pearls)
}
