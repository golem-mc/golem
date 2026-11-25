package world

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// beCodec is one walk over the saved fields BlockEntity.codec declares: a load
// reads each from in, a save writes each to out.
type beCodec struct {
	load      bool
	updateTag bool // Client output must not traverse private plugin data.
	in        nbt.Compound
	out       nbt.CompoundTag
}

// beValue is how one saved field reads from its key and writes back.
type beValue[T any] struct {
	// read is the key's value, false when it is absent or will not read.
	read func(c nbt.Compound, key string) (T, bool)
	// write is the value's tag, false to leave the key out of the save.
	write func(v T) (nbt.Tag, bool)
}

// field declares one saved field: a load sets *v from its key, or to def when
// the key does not read, and a save writes *v.
func field[T any](c *beCodec, key string, v *T, as beValue[T], def T) {
	if c.load {
		if got, ok := as.read(c.in, key); ok {
			*v = got
		} else {
			*v = def
		}
		return
	}
	if tag, ok := as.write(*v); ok {
		c.out.Set(key, tag)
	}
}

// The field encodings. A read takes only the tag types its comment names;
// anything else reads as absent.
var (
	// Saves outlive the entity lock; never share the mutable private compound.
	bePluginData = beValue[nbt.Compound]{
		read: func(c nbt.Compound, key string) (nbt.Compound, bool) {
			v, ok := nbt.Get[nbt.Compound](c, key)
			if !ok {
				return nil, false
			}
			return nbt.PlainCopy(v).(nbt.Compound), true
		},
		write: func(v nbt.Compound) (nbt.Tag, bool) {
			if len(v) == 0 {
				return nil, false
			}
			return nbt.Copy(v), true
		},
	}
	// beInt reads any number as an int (getIntOr, which floors a float) and
	// writes an int.
	beInt = beValue[int]{read: readIntField, write: func(v int) (nbt.Tag, bool) { return nbt.Int(v), true }}
	// beShort reads any number narrowed to a short (getShortOr) and writes a
	// short.
	beShort = beValue[int]{
		read: func(c nbt.Compound, key string) (int, bool) {
			v, ok := nbt.ShortField(c, key)
			return int(v), ok
		},
		write: func(v int) (nbt.Tag, bool) { return nbt.Short(v), true },
	}
	// beIntShort reads any number as an int (getIntOr) and writes it narrowed
	// to a short, so an int 70000 is kept whole until it saves as 4464s.
	// vanilla: BaseSpawner.load, BaseSpawner.save
	beIntShort = beValue[int]{read: readIntField, write: func(v int) (nbt.Tag, bool) { return nbt.Short(v), true }}
	// beInt32 reads any number as an int (getIntOr) and writes an int.
	beInt32 = beValue[int32]{read: nbt.IntField, write: func(v int32) (nbt.Tag, bool) { return nbt.Int(v), true }}
	// beFloat32 reads any number as a float (getFloatOr) and writes a float.
	beFloat32 = beValue[float32]{read: nbt.FloatField, write: func(v float32) (nbt.Tag, bool) { return nbt.Float(v), true }}
	// beLegacyDirection reads a direction's 3D data value through the byte
	// codec, which truncates any number to its low 8 bits, then wraps it into
	// 0-5 by its remainder's magnitude; it writes a byte.
	// vanilla: Direction.LEGACY_ID_CODEC, Direction.from3DDataValue
	beLegacyDirection = beValue[int8]{
		read: func(c nbt.Compound, key string) (int8, bool) {
			v, ok := nbt.IntValue(c[key])
			d := int8(v) % 6
			return max(d, -d), ok
		},
		write: func(v int8) (nbt.Tag, bool) { return nbt.Byte(v), true },
	}
	// beFlag reads any number, true when its byte is not nought
	// (getBooleanOr), and writes 1b or 0b.
	beFlag = beValue[bool]{
		read: func(c nbt.Compound, key string) (bool, bool) {
			v, ok := nbt.ByteField(c, key)
			return v != 0, ok
		},
		write: func(v bool) (nbt.Tag, bool) { return nbt.Byte(boolByte(v)), true },
	}
	// beTrueFlag is beFlag, left out while false.
	beTrueFlag = beValue[bool]{read: beFlag.read, write: func(v bool) (nbt.Tag, bool) { return nbt.Byte(1), v }}
	// beIntFlag reads any number as an int (getIntOr), true only at 1, and
	// writes 1 or 0 as an int.
	// vanilla: CrafterBlockEntity.loadAdditional, CrafterBlockEntity.isTriggered
	beIntFlag = beValue[bool]{
		read: func(c nbt.Compound, key string) (bool, bool) {
			v, ok := nbt.IntField(c, key)
			return v == 1, ok
		},
		write: func(v bool) (nbt.Tag, bool) { return nbt.Int(boolByte(v)), true },
	}
	// beOptString is beString, left out while empty.
	beOptString = beValue[string]{read: readString, write: func(v string) (nbt.Tag, bool) { return nbt.String(v), v != "" }}
	// beLootTable reads a loot table's key as an Identifier, a bare name being
	// minecraft's, and is left out while empty.
	// vanilla: LootTable.KEY_CODEC
	beLootTable = beValue[string]{
		read: func(c nbt.Compound, key string) (string, bool) {
			v, ok := readString(c, key)
			if v != "" {
				v = registry.Qualify(v)
			}
			return v, ok
		},
		write: beOptString.write,
	}
	// beSeed reads any number (LongValue) and writes a long, left out while 0.
	beSeed = beValue[int64]{read: readLongValue, write: func(v int64) (nbt.Tag, bool) { return nbt.Long(v), v != 0 }}
	// beLong reads any number (LongValue, getLong) and writes a long.
	beLong = beValue[int64]{read: readLongValue, write: func(v int64) (nbt.Tag, bool) { return nbt.Long(v), true }}
	// beUUID reads the chunk format's four-int array and is left out while
	// nil.
	beUUID = beValue[uuid.UUID]{
		read: func(c nbt.Compound, key string) (uuid.UUID, bool) {
			ids, ok := nbt.Get[nbt.IntArray](c, key)
			if !ok {
				return uuid.Nil, false
			}
			return nbt.UUIDFromIntArray(ids)
		},
		write: func(v uuid.UUID) (nbt.Tag, bool) { return nbt.UUIDToIntArray(v), v != uuid.Nil },
	}
	// beComponentName is a components compound holding only minecraft:custom_name,
	// left out while nil.
	beComponentName = beValue[*protocol.Text]{
		read: func(c nbt.Compound, key string) (*protocol.Text, bool) {
			components, ok := nbt.Get[nbt.Compound](c, key)
			if !ok {
				return nil, false
			}
			return beText.read(components, "minecraft:custom_name")
		},
		write: func(v *protocol.Text) (nbt.Tag, bool) {
			tag, ok := beText.write(v)
			if !ok {
				return nil, false
			}
			return nbt.Compound{"minecraft:custom_name": tag}, true
		},
	}
	// beText reads a text component and is left out while nil. A component
	// that will not read is dropped rather than failing the entity.
	beText = beValue[*protocol.Text]{
		read: func(c nbt.Compound, key string) (*protocol.Text, bool) {
			tag, ok := c[key]
			if !ok {
				return nil, false
			}
			text, ok := protocol.StyledTextOf(tag)
			if !ok {
				return nil, false
			}
			return &text, true
		},
		write: func(v *protocol.Text) (nbt.Tag, bool) {
			if v == nil {
				return nil, false
			}
			return protocol.TextTag(*v), true
		},
	}
	// beCompound keeps a lock's payload, writing ItemPredicate.CODEC fields in
	// RecordCodecBuilder's declaration order.
	beCompound = blockCompoundValue(saveLock, false)
	// vanilla: SpawnData.CODEC, SpawnData.<init>
	beSpawnData = blockCompoundValue(qualifiedSpawnData, false)

	// beSpawnPotentials keeps a potentials list with each entry's data
	// qualified. vanilla: SpawnData.LIST_CODEC
	beSpawnPotentials = beValue[nbt.List]{
		read: func(c nbt.Compound, key string) (nbt.List, bool) {
			list, ok := nbt.Get[nbt.List](c, key)
			if !ok || list.Elem != nbt.TypeCompound {
				return list, ok
			}
			items := make([]nbt.Tag, len(list.Items))
			for i, item := range list.Items {
				items[i] = item
				if entry, ok := item.(nbt.CompoundTag); ok {
					if data, ok := nbt.Get[nbt.CompoundTag](entry.Entries(), "data"); ok {
						next := blockRecord(entry, "data", "weight")
						next.Set("data", qualifiedSpawnData(data))
						items[i] = next
					}
				}
			}
			list.Items = items
			return list, true
		},
		write: func(v nbt.List) (nbt.Tag, bool) { return v, true },
	}
	// beList keeps a list as read.
	beList = beValue[nbt.List]{read: nbt.Get[nbt.List], write: func(v nbt.List) (nbt.Tag, bool) { return v, true }}
	// beOptCompound is beCompound, left out while empty.
	beOptCompound = blockCompoundValue(func(v nbt.CompoundTag) nbt.CompoundTag {
		return blockRecord(v, "back", "left", "right", "front")
	}, true)
)

func readLongValue(c nbt.Compound, key string) (int64, bool) {
	return nbt.LongValue(c[key])
}

func readString(c nbt.Compound, key string) (string, bool) {
	v, ok := nbt.Get[nbt.String](c, key)
	return string(v), ok
}

func readIntField(c nbt.Compound, key string) (int, bool) {
	v, ok := nbt.IntField(c, key)
	return int(v), ok
}

// The encodings one kind uses.
var (
	// beRecipesUsed is the smelting history, recipe id to smelt count, written
	// even when nothing is owed. An entry that is no positive int is skipped
	// rather than failing the entity; none left reads as nil.
	beRecipesUsed = beValue[recipeHistory]{
		read: func(c nbt.Compound, key string) (recipeHistory, bool) {
			used, ok := nbt.Get[nbt.CompoundTag](c, key)
			var out recipeHistory
			if ok {
				for _, recipe := range nbt.HashOrderedKeys(used) {
					if n, ok := nbt.Get[nbt.Int](used.Entries(), recipe); ok && n > 0 {
						out.add(recipe, int(n))
					}
				}
			}
			return out, ok
		},
		write: func(v recipeHistory) (nbt.Tag, bool) {
			used := nbt.NewOrderedCompound(v.Len())
			for _, recipe := range v.keys {
				used.Set(recipe, nbt.Int(v.counts[recipe]))
			}
			return used, true
		},
	}
	// beCampfireTimes is one int per cooking slot. A short array fills only
	// its length, the rest staying zero.
	// vanilla: CampfireBlockEntity.loadAdditional
	beCampfireTimes = beValue[[CampfireSlots]int32]{
		read: func(c nbt.Compound, key string) ([CampfireSlots]int32, bool) {
			var out [CampfireSlots]int32
			times, ok := nbt.Get[nbt.IntArray](c, key)
			copy(out[:], times)
			return out, ok
		},
		write: func(v [CampfireSlots]int32) (nbt.Tag, bool) {
			return nbt.IntArray(append([]int32(nil), v[:]...)), true
		},
	}
	// beSignText is one sign side, written even at its defaults.
	// vanilla: SignText.DIRECT_CODEC
	beSignText = beValue[SignText]{
		read: func(c nbt.Compound, key string) (SignText, bool) {
			side, ok := nbt.Get[nbt.Compound](c, key)
			if !ok {
				return SignText{}, false
			}
			return decodeSignText(side), true
		},
		write: func(v SignText) (nbt.Tag, bool) { return v.encode(), true },
	}
	// beBeaconEffect keeps only an effect the pyramid offers, a name without
	// namespace being minecraft's as the effect holder's codec reads it, and is
	// left out while unset.
	// vanilla: BeaconBlockEntity.loadEffect
	beBeaconEffect = beValue[int32]{
		read: func(c nbt.Compound, key string) (int32, bool) {
			name, ok := nbt.Get[nbt.String](c, key)
			if !ok {
				return 0, false
			}
			id, known := registry.MobEffectID(registry.Qualify(string(name)))
			return id, known && registry.BeaconOffersEffect(id)
		},
		write: func(v int32) (nbt.Tag, bool) {
			name := registry.MobEffectName(v)
			return nbt.String(name), name != ""
		},
	}
	// beMovedState reads the codec's compact or full state tag.
	beMovedState = beValue[uint16]{
		read: func(c nbt.Compound, key string) (uint16, bool) {
			blockState, ok := c[key]
			if !ok {
				return 0, false
			}
			name, props := registry.BlockStateOf(blockState)
			return registry.FindState(name, props)
		},
		write: func(v uint16) (nbt.Tag, bool) { return registry.BlockStateCodecTag(v), true },
	}
	// beListener is a sensor's or shrieker's vibration listener.
	// vanilla: VibrationSystem.Data.CODEC
	beListener = beValue[vibrationListener]{
		read: func(c nbt.Compound, key string) (vibrationListener, bool) {
			listener, ok := nbt.Get[nbt.Compound](c, key)
			if !ok {
				return vibrationListener{}, false
			}
			return decodeListener(listener)
		},
		write: func(v vibrationListener) (nbt.Tag, bool) { return v.encode(), true },
	}
	// beCursors is the sculk catalyst's spreader; cursorsFromNBT reads the
	// cursors key itself.
	// vanilla: SculkSpreader.load, SculkSpreader.save
	beCursors = beValue[[]chargeCursor]{
		read:  func(c nbt.Compound, _ string) ([]chargeCursor, bool) { return cursorsFromNBT(c), true },
		write: func(v []chargeCursor) (nbt.Tag, bool) { return cursorsToNBT(v), true },
	}
)

// codec declares each kind's saved fields once, for loadFrom and EncodeNBT
// alike: the name and lock any kind may keep, the contents, then the kind's
// own. A load reads them in this order, so a field read after the contents
// sees the slots.
func (b *BlockEntity) codec(c *beCodec) {
	if !c.updateTag {
		field(c, "plugins", &b.pluginData, bePluginData, nil)
	}
	if lockableKind(b.Kind) && b.Kind != KindBeacon {
		field(c, "lock", &b.Lock, beCompound, nil)
	}
	if b.Kind != KindCopperGolemStatue && b.Kind != KindBeacon {
		field(c, "CustomName", &b.CustomName, beText, nil)
	}
	// Furnaces, brewing stands, crafters and pots write their contents between
	// their own fields; their loaders still need the contents before those fields.
	delayedContents := IsCooking(b.Kind) || b.Kind == KindBrewingStand || b.Kind == KindCrafter || b.Kind == KindDecoratedPot
	if c.load || !delayedContents {
		b.contents(c)
	}
	switch b.Kind {
	case KindFurnace, KindBlastFurnace, KindSmoker:
		field(c, "cooking_time_spent", &b.CookingSpent, beInt32, 0)
		field(c, "cooking_total_time", &b.CookingTotal, beInt32, 0)
		field(c, "lit_time_remaining", &b.LitRemaining, beInt32, 0)
		field(c, "lit_total_time", &b.LitTotal, beInt32, 0)
		field(c, "speed_multiplier", &b.SpeedMultiplier, beFloat32, 1)
		if !c.load {
			b.contents(c)
		}
		field(c, "RecipesUsed", &b.RecipesUsed, beRecipesUsed, recipeHistory{})
	case KindBrewingStand:
		// vanilla: BrewingStandBlockEntity.loadAdditional, saveAdditional
		field(c, "BrewTime", &b.BrewTime, beInt, 0)
		field(c, "total_brew_time", &b.BrewTotal, beInt, 400)
		if !c.load {
			b.contents(c)
		}
		field(c, "Fuel", &b.Fuel, beInt, 0)
		field(c, "total_fuel", &b.FuelTotal, beInt, 20)
		field(c, "speed_multiplier", &b.SpeedMultiplier, beFloat32, 1)
		// A running brew's ingredient is the slot's item at save, so a mid-brew
		// swap still cancels it after a load.
		// vanilla: BrewingStandBlockEntity.loadAdditional
		if c.load && b.BrewTime > 0 {
			b.BrewingIngredient = b.Slots[3].ItemID
		}
	case KindCrafter:
		field(c, "crafting_ticks_remaining", &b.CraftingTicks, beInt, 0)
		if !c.load {
			b.contents(c)
		}
		b.disabledSlots(c)
		// An in-place load without the key keeps the flag.
		field(c, "triggered", &b.Triggered, beIntFlag, b.Triggered)
	case KindCampfire:
		// vanilla: CampfireBlockEntity.saveAdditional
		field(c, "CookingTimes", &b.CampfireProgress, beCampfireTimes, [CampfireSlots]int32{})
		field(c, "CookingTotalTimes", &b.CampfireTotals, beCampfireTimes, [CampfireSlots]int32{})
	case KindShelf:
		// vanilla: ShelfBlockEntity.loadAdditional, saveAdditional
		field(c, "align_items_to_bottom", &b.BottomAligned, beFlag, false)
	case KindChiseledBookshelf:
		// The last cell is always written, -1 included.
		// vanilla: ChiseledBookShelfBlockEntity.loadAdditional, saveAdditional
		field(c, "last_interacted_slot", &b.LastInteractedSlot, beInt, -1)
	case KindHopper:
		// The cooldown is saved raw, -1 included: vanilla reads the file's
		// value or keeps its initial -1, idle either way until the tick sets
		// it.
		field(c, "TransferCooldown", &b.Cooldown, beInt32, -1)
	case KindSpawner:
		// BaseSpawner's numbers, its initial values the defaults; the next
		// spawn's data as its codec writes it, absent for a fresh spawner; the
		// potentials list always.
		// vanilla: SpawnerBlockEntity.saveAdditional
		field(c, "Delay", &b.Delay, beShort, 20)
		field(c, "MinSpawnDelay", &b.MinSpawnDelay, beIntShort, 200)
		field(c, "MaxSpawnDelay", &b.MaxSpawnDelay, beIntShort, 800)
		field(c, "SpawnCount", &b.SpawnCount, beIntShort, 4)
		field(c, "MaxNearbyEntities", &b.MaxNearbyEntities, beIntShort, 6)
		field(c, "RequiredPlayerRange", &b.RequiredPlayerRange, beIntShort, 16)
		field(c, "SpawnRange", &b.SpawnRange, beIntShort, 4)
		field(c, "SpawnData", &b.SpawnData, beSpawnData, b.SpawnData)
		potentials := nbt.List{}
		if c.load {
			// An absent list defaults to one entry; an explicit empty list stays empty.
			// vanilla: BaseSpawner.load
			data := b.SpawnData
			if data == nil {
				data = nbt.Compound{"entity": nbt.Compound{}}
			}
			potentials = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"data": data, "weight": nbt.Int(1)},
			}}
		}
		field(c, "SpawnPotentials", &b.SpawnPotentials, beSpawnPotentials, potentials)
	case KindComparator:
		// vanilla: ComparatorBlockEntity.loadAdditional, saveAdditional
		field(c, "OutputSignal", &b.Output, beInt, 0)
	case KindJukebox:
		// The song's ticks are saved only while a song is set, so a finished
		// one saves none; a load reading a tag past the song's end names no
		// song, as setSongWithoutPlaying refuses it.
		// vanilla: JukeboxBlockEntity.loadAdditional, saveAdditional
		if c.load || b.JukeboxPlaying() {
			field(c, "ticks_since_song_started", &b.TicksSinceSongStarted, beLong, -1)
		}
		if c.load && (!b.JukeboxPlaying() || b.JukeboxFinished()) {
			b.TicksSinceSongStarted = -1
		}
	case KindLectern:
		// The page is saved only beside a book, and a load clamps it to the
		// book's pages.
		// vanilla: LecternBlockEntity.loadAdditional, saveAdditional
		if c.load || c.out.Entries()["Book"] != nil {
			field(c, "Page", &b.Page, beInt, 0)
		}
		if c.load {
			b.Page = clampPage(b.Page, b.BookPages())
		}
	case KindBeacon:
		// The level is saved for the client's beam and window but never read:
		// the 80-tick update recounts it.
		// vanilla: BeaconBlockEntity.saveAdditional
		field(c, "primary_effect", &b.BeaconPrimary, beBeaconEffect, -1)
		field(c, "secondary_effect", &b.BeaconSecondary, beBeaconEffect, -1)
		if !c.load {
			c.out.Set("Levels", nbt.Int(b.BeaconLevels))
		}
		field(c, "CustomName", &b.CustomName, beText, nil)
		field(c, "lock", &b.Lock, beCompound, nil)
	case KindSign, KindHangingSign:
		// Both sides default to empty black, so a tag naming no side blanks it
		// rather than keeping old lines; the editor is runtime.
		// vanilla: SignBlockEntity.loadAdditional
		field(c, "allow_op_features", &b.AllowOpFeatures, beTrueFlag, false)
		field(c, "front_text", &b.SignFront, beSignText, SignText{Color: "black"})
		field(c, "back_text", &b.SignBack, beSignText, SignText{Color: "black"})
		field(c, "is_waxed", &b.Waxed, beFlag, false)
	case KindDecoratedPot:
		// vanilla: PotDecorations
		field(c, "sherds", &b.Sherds, beOptCompound, nil)
		if !c.load {
			b.contents(c)
		}
	case KindSculkSensor, KindCalibratedSculkSensor:
		// vanilla: SculkSensorBlockEntity.loadAdditional, saveAdditional
		field(c, "last_vibration_frequency", &b.SculkFrequency, beInt, 0)
		b.vibrationData(c)
	case KindSculkShrieker:
		// vanilla: SculkShriekerBlockEntity.loadAdditional, saveAdditional
		field(c, "warning_level", &b.WarningLevel, beInt, 0)
		b.vibrationData(c)
	case KindSculkCatalyst:
		field(c, "cursors", &b.cursors, beCursors, nil)
		if c.load {
			b.hasCursors.Store(len(b.cursors) > 0)
		}
	case KindConduit:
		// The beam's target, absent while resting; the active flag is
		// runtime, recomputed on each load's first refresh.
		// vanilla: ConduitBlockEntity.saveAdditional
		field(c, "Target", &b.Target, beUUID, uuid.Nil)
	case KindPotentSulfur:
		// vanilla: PotentSulfurBlockEntity.saveAdditional
		field(c, "countdown", &b.Countdown, beInt, -1)
	case KindCreakingHeart:
		// vanilla: CreakingHeartBlockEntity.saveAdditional
		field(c, "creaking", &b.Creaking, beUUID, uuid.Nil)
	case KindCommandBlock:
		b.commandBlockFields(c)
	}
	// vanilla: BlockEntity.saveWithoutMetadata appends components after saveAdditional.
	if !c.updateTag {
		if b.Kind == KindCopperGolemStatue {
			field(c, "components", &b.CustomName, beComponentName, nil)
			if c.load {
				b.Components = leftoverNamed(c.in)
			}
		} else {
			field(c, "components", &b.Components, bePluginData, nil)
		}
	}
}

// contents walks what a container holds: a pending loot table or its stacks,
// never both in one save. A kind holding one stack under its own key
// (stackKey) reads and saves only that key; an Items list on one is ignored.
// vanilla: RandomizableContainerBlockEntity, ChestBlockEntity.saveAdditional,
// JukeboxBlockEntity.loadAdditional, LecternBlockEntity.loadAdditional,
// DecoratedPotBlockEntity.loadAdditional
func (b *BlockEntity) contents(c *beCodec) {
	key := stackKey(b.Kind)
	stacked := false
	if !c.load {
		stacked = b.saveStacks(c.out, key)
	}
	// A table means rolled contents: a load takes items only when the file
	// names no table, and a save writes the table only while no stack is in.
	// A seed is saved only beside its table.
	if lootTableKind(b.Kind) && !stacked {
		field(c, "LootTable", &b.LootTable, beLootTable, "")
		if c.load || b.LootTable != "" {
			field(c, "LootTableSeed", &b.LootTableSeed, beSeed, 0)
		}
	}
	if !c.load {
		// Shulker boxes omit empty Items; other slot-keeping containers write
		// an End-typed empty list. Ender chests have no slots to save.
		// vanilla: ShulkerBoxBlockEntity.saveAdditional, ContainerHelper.saveAllItems
		if !stacked && key == "" && len(b.Slots) > 0 && b.Kind != KindShulkerBox && (b.LootTable == "" || !lootTableKind(b.Kind)) {
			c.out.Set("Items", nbt.List{Elem: nbt.TypeEnd})
		}
		return
	}
	if b.LootTable != "" {
		return
	}
	if key == "" {
		b.loadItems(c.in)
		return
	}
	if stack, ok := nbt.Get[nbt.Compound](c.in, key); ok {
		b.loadOneStack(stack)
	}
}

// stackKey is the key a kind keeps its one stack under instead of an Items
// list, "" for Items.
// vanilla: JukeboxBlockEntity, LecternBlockEntity, DecoratedPotBlockEntity
func stackKey(kind BlockEntityKind) string {
	switch kind {
	case KindJukebox:
		return "RecordItem"
	case KindLectern:
		return "Book"
	case KindDecoratedPot:
		// An ItemStack codec tag without Slot.
		return "item"
	}
	return ""
}

// saveStacks writes the stacks under key, or as an Items list when key is "",
// reporting whether any went in. Unshowable slots go back as the compounds
// read; live slots as fresh stack-codec entries in slot order.
func (b *BlockEntity) saveStacks(out nbt.CompoundTag, key string) bool {
	if key != "" {
		if stored, ok := b.Stored[0]; ok {
			out.Set(key, stored)
			return true
		}
		if s := b.Slots[0]; !s.Empty() {
			if _, known := registry.ItemName(s.ItemID); known {
				out.Set(key, registry.StackTag(s))
				return true
			}
		}
		return false
	}
	items := make([]nbt.Tag, 0, len(b.Slots))
	for slot, stack := range b.Slots {
		if stored, ok := b.Stored[slot]; ok {
			items = append(items, stored)
			continue
		}
		if stack.Empty() {
			continue
		}
		if _, ok := registry.ItemName(stack.ItemID); ok {
			items = append(items, registry.SlottedStackTag(slot, stack))
		}
	}
	if len(items) == 0 {
		return false
	}
	out.Set("Items", nbt.List{Elem: nbt.TypeCompound, Items: items})
	return true
}

// loadItems reads the entry's Items list into the slots, a stack with
// unshowable components kept whole in Stored.
func (b *BlockEntity) loadItems(entry nbt.Compound) {
	items, ok := nbt.Get[nbt.List](entry, "Items")
	if !ok {
		return
	}
	for _, item := range items.Items {
		compound, ok := nbt.CompoundView(item)
		if !ok {
			continue
		}
		// Slot keeps the codec's low 8 bits, so an int 259 is slot 3, not
		// dropped.
		slot, hasSlot := nbt.ByteValue(compound["Slot"])
		if !hasSlot || slot < 0 || int(slot) >= len(b.Slots) {
			continue
		}
		stack, stored, ok := storedStack(compound)
		if !ok {
			continue
		}
		if stored != nil {
			if b.Stored == nil {
				b.Stored = make(map[int]nbt.Compound)
			}
			b.Stored[int(slot)] = stored
			continue
		}
		b.Slots[slot] = stack
		b.setFilled(int(slot), stack.Count > 0)
	}
}

// loadOneStack reads a one-stack kind's stack into slot 0. One with
// unshowable components is kept whole in Stored for the save, its item still
// in the slot so it can be ejected. An item id that does not read leaves the
// slot empty.
func (b *BlockEntity) loadOneStack(tag nbt.Compound) {
	stack, stored, _ := storedStack(tag)
	if stored == nil {
		b.Slots[0] = stack
		b.setFilled(0, stack.Count > 0)
		return
	}
	b.Slots[0], _ = registry.StoredStackBase(stored)
	b.setFilled(0, b.Slots[0].Count > 0)
	if b.Stored == nil {
		b.Stored = make(map[int]nbt.Compound)
	}
	b.Stored[0] = stored
}

// disabledSlots walks the crafter's disabled slots, an int array of indexes.
// A load clears every slot's state first, so an in-place load enables a slot
// the tag does not list; an index outside the grid or a slot holding an item
// is skipped.
// vanilla: CrafterBlockEntity.loadAdditional
func (b *BlockEntity) disabledSlots(c *beCodec) {
	if !c.load {
		disabled := nbt.IntArray{}
		for slot, off := range b.DisabledSlots {
			if off {
				disabled = append(disabled, int32(slot))
			}
		}
		c.out.Set("disabled_slots", disabled)
		return
	}
	b.DisabledSlots = [9]bool{}
	disabled, _ := nbt.Get[nbt.IntArray](c.in, "disabled_slots")
	for _, slot := range disabled {
		if b.crafterSlotCanBeDisabled(int(slot)) {
			b.DisabledSlots[slot] = true
		}
	}
}

// vibrationData walks the vibration listener; a listener the codec
// refuses, or none, loads fresh. Each load marks the entity for Physics to
// take the listener up.
// vanilla: VibrationSystem.Data.CODEC
func (b *BlockEntity) vibrationData(c *beCodec) {
	field(c, "listener", &b.listener, beListener, vibrationListener{})
	if c.load {
		b.listenerLoaded.Store(true)
	}
}

// BlockStateKeyPiston is the moving-piston block entity's moved state key.
const BlockStateKeyPiston = "blockState"

// BlockStateNBTKeys lists every block-entity NBT key decoded with BlockState.CODEC.
var BlockStateNBTKeys = []string{
	BlockStateKeyPiston,
}

// commandBlockFields walks the command block's BaseCommandBlock and
// CommandBlockEntity fields.
// vanilla: CommandBlockEntity.loadAdditional
func (b *BlockEntity) commandBlockFields(c *beCodec) {
	if c.load {
		b.Load(c.in)
	} else {
		b.Save(c.out)
	}
	field(c, "powered", &b.Powered, beFlag, false)
	field(c, "conditionMet", &b.ConditionMet, beFlag, false)
	field(c, "auto", &b.Automatic, beFlag, false)
}

// qualifiedSpawnData returns data with its entity's id written as an
// Identifier (namespace defaulted); data is left alone when it has none.
func qualifiedSpawnData(data nbt.CompoundTag) nbt.CompoundTag {
	if data == nil {
		return nil
	}
	next := blockRecord(data, "entity", "custom_spawn_rules", "equipment")
	entity, ok := nbt.Get[nbt.CompoundTag](data.Entries(), "entity")
	if !ok {
		return next
	}
	// CompoundTag.CODEC copies the arbitrary payload on decode and encode.
	nextEntity := nbt.OrderedCopy(entity).(nbt.CompoundTag)
	if id, ok := nbt.Get[nbt.String](entity.Entries(), "id"); ok {
		nextEntity.Set("id", nbt.String(registry.Qualify(string(id))))
	}
	next.Set("entity", nextEntity)
	return next
}

// blockRecord writes a codec's known fields in encoder traversal order, while
// retaining extra fields this model carries without interpreting their values.
func blockRecord(value nbt.CompoundTag, fields ...string) nbt.CompoundTag {
	if value == nil {
		return nil
	}
	out := nbt.NewOrderedCompound(len(value.Entries()))
	for _, key := range fields {
		if tag, ok := value.Entries()[key]; ok {
			out.Set(key, tag)
		}
	}
	for _, key := range nbt.HashOrderedKeys(value) {
		if _, exists := out.Entries()[key]; !exists {
			out.Set(key, value.Entries()[key])
		}
	}
	return out
}

// saveLock writes ItemPredicate.CODEC and the custom-data predicate's SNBT codec.
func saveLock(value nbt.CompoundTag) nbt.CompoundTag {
	if value == nil {
		return nil
	}
	return registry.LockTag(value)
}

// blockCompoundValue normalizes record structure on load, retaining it between
// saves; runtime component application normalizes replacements the same way.
func blockCompoundValue(normalize func(nbt.CompoundTag) nbt.CompoundTag, omitEmpty bool) beValue[nbt.CompoundTag] {
	return beValue[nbt.CompoundTag]{
		read: func(c nbt.Compound, key string) (nbt.CompoundTag, bool) {
			value, ok := nbt.Get[nbt.CompoundTag](c, key)
			return normalize(value), ok
		},
		write: func(value nbt.CompoundTag) (nbt.Tag, bool) {
			return value, value != nil && (!omitEmpty || len(value.Entries()) > 0)
		},
	}
}

// leftoverNamed is a statue's saved components minus the custom_name its
// CustomName field holds.
func leftoverNamed(c nbt.Compound) nbt.Compound {
	read, ok := bePluginData.read(c, "components")
	if !ok {
		return nil
	}
	delete(read, "minecraft:custom_name")
	if len(read) == 0 {
		return nil
	}
	return read
}
