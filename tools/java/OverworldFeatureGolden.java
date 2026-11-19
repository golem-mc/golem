import java.io.PrintWriter;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.entity.BlockEntity;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.chunk.ChunkGenerator;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.Strategy;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.placement.PlacedFeature;

// Writes internal/server/world/testdata/overworld_feature_golden.txt (mise run
// gen:overworld-feature-golden): the blocks the vanilla placed features of the
// overworld change in a synthetic world, which internal/server/world's
// overworld_feature_golden_test.go rebuilds and runs the ported features in.
//
// The world is a function of (x, y, z) only, so both sides see the same
// terrain; WorldGenLevel is a proxy over it. A "case" runs the whole
// PlacedFeature (placement modifiers and biome filter included) from the chunk
// corner (x, -64, z); a "raw" runs the configured feature alone at (x, y, z).
// Both draw from a WorldgenRandom over Xoroshiro(seed). Case i of a feature,
// from 1 to the count its "cases" line gives, uses seed caseSeed(i) in chunk
// CHUNKS[i % 4]; only a case that changed blocks is listed, so the test
// expects the others to change none. The lines are
//   cases <feature> <count>
//   case|raw <feature> <seed> <x> <y> <z> <changed blocks> <sha256 of them>
// where a changed block is a final state different from the terrain's,
// hashed as "x y z state" lines in x, y, z order. A second argument names a
// file that gets every changed block, for finding a difference.
public class OverworldFeatureGolden {
    static final int MIN_Y = -64, HEIGHT = 384;
    static final long[] SEEDS = {1L, 2L, 3L, 4L, 5L, 6L, 7L, 8L, 9L, 10L, 11L, 12L};
    static long caseSeed(int i) { return i * 0x9E3779B97F4A7C15L; }
    static final int[][] CHUNKS = {{0, 0}, {-16, 32}, {48, -80}, {160, 96}};
    static final int CASE_SEEDS = 8000, ORE_CASE_SEEDS = 200;
    static final int[] RAW_YS = {-40, 5, 30, 45, 61, 64};
    static final String[] FEATURES = {
        "lake_lava_underground", "lake_lava_surface", "rooted_sulfur_spring", "sulfur_pool",
        "ore_dirt", "ore_gravel", "ore_granite_upper", "ore_granite_lower", "ore_diorite_upper", "ore_diorite_lower",
        "ore_andesite_upper", "ore_andesite_lower", "ore_tuff", "ore_coal_upper", "ore_coal_lower",
        "ore_iron_upper", "ore_iron_middle", "ore_iron_small", "ore_gold", "ore_gold_lower",
        "ore_redstone", "ore_redstone_lower", "ore_diamond", "ore_diamond_medium", "ore_diamond_large",
        "ore_diamond_buried", "ore_lapis", "ore_lapis_buried", "ore_copper_large", "ore_copper",
        "underwater_magma", "ore_clay", "ore_gold_extra", "disk_grass", "disk_sand", "disk_clay",
        "disk_gravel", "ore_emerald",
        "monster_room", "monster_room_deep", "forest_rock", "blue_ice", "amethyst_geode", "glow_lichen", "freeze_top_layer",
        "bamboo", "bamboo_light", "lush_caves_ceiling_vegetation", "lush_caves_clay", "lush_caves_vegetation", "pale_moss_patch",
        "sculk_vein", "ore_infested",
        "large_dripstone", "pointed_dripstone", "dripstone_cluster", "sulfur_spike", "sulfur_spike_cluster",
        "iceberg_packed", "iceberg_blue",
        "ice_spike", "ice_patch", "desert_well", "fossil_upper", "fossil_lower"};
    // casesOf is how many placed cases a feature gets: the lakes and rooms are rare
    // per case, the ores and lichen run many attempts in each.
    static int casesOf(String feature) { return feature.equals("freeze_top_layer") ? FREEZE_CASES : feature.startsWith("ore_") || feature.startsWith("disk_") || feature.startsWith("underwater_") || feature.equals("glow_lichen") || feature.startsWith("bamboo") || feature.startsWith("lush_caves_") || feature.equals("pale_moss_patch") || feature.startsWith("sculk_") || feature.equals("large_dripstone") || feature.equals("pointed_dripstone") || feature.equals("dripstone_cluster") || feature.startsWith("sulfur_spike") || feature.equals("rooted_sulfur_spring") || feature.equals("sulfur_pool") ? ORE_CASE_SEEDS : CASE_SEEDS; }

    // freeze_top_layer runs in a biome picked from FREEZE_BIOMES by case, over
    // a mountain chunk as well; it draws no randoms, so it has no raw cases.
    static final int[][] FREEZE_CHUNKS = {{0, 0}, {-16, 32}, {48, -80}, {160, 96}, {320, 320}};
    // Chunk coordinates; the older lists above are block origins.
    static final int[][] SURFACE_CHUNKS = {{-80, -8}, {-80, 8}, {-96, -24}, {-96, 24}};
    static final int[] SURFACE_RAW_YS = {64, 69, 70, 71, 75, 100};
    static boolean surfaceFeature(String feature) { return feature.equals("ice_spike") || feature.equals("ice_patch") || feature.equals("desert_well"); }
    static final String[] FREEZE_BIOMES = {"plains", "snowy_plains", "windswept_hills", "frozen_ocean", "deep_frozen_ocean", "jagged_peaks"};
    static final int FREEZE_CASES = 600;

    static HolderLookup.Provider lookup;
    static net.minecraft.core.RegistryAccess templateRegistries;
    static PalettedContainerFactory factory;
    static BlockState STONE, DEEPSLATE, DIRT, GRASS, WATER, SAND, AIR, MUD, CLAY, PACKED_ICE;
    static BlockState[] PEAK_TOPS;
    static boolean largeDripstone;
    static boolean sculkOnly;

    // surface is the terrain's top solid y of a column.
    static int surface(int x, int z) { return 60 + Math.floorMod((x >> 5) * 3 + (z >> 5) * 5, 9); }

    // corridor is true for the two rows of the one-block tunnels along x that
    // the monster rooms need openings from.
    static boolean corridor(int y) { return y == -40 || y == -39 || y == 5 || y == 6 || y == 30 || y == 31 || y == 45 || y == 46; }

    static BlockState terrain(int x, int y, int z) {
        if (y < MIN_Y || y >= MIN_Y + HEIGHT) return AIR;
        // Broad caves exercise large columns and wind without changing other goldens.
        if (largeDripstone) {
            if (y > 70 || (y >= -50 && y <= -25) || (y >= 0 && y <= 35) || (y >= 40 && y <= 58)) return y < -45 ? WATER : AIR;
            return STONE;
        }
        if (x < -1024) {
            if (y > 70) return AIR;
            return y == 70 ? (z < 0 ? Blocks.SNOW_BLOCK.defaultBlockState() : SAND) : STONE;
        }
        if (x >= 256) {
            // Mountains for the height-adjusted temperature; no lake case reaches them.
            int top = 90 + Math.floorMod((x >> 2) * 7 + (z >> 2) * 3, 70);
            if (y > top) return AIR;
            return y == top ? PEAK_TOPS[Math.floorMod((x >> 1) + (z >> 1) * 3, PEAK_TOPS.length)] : STONE;
        }
        int h = surface(x, z);
        if (y <= h) {
            if ((z & 15) == 8 && corridor(y)) return AIR;
            if (y >= 10 && y <= 22 && Math.floorMod((x >> 1) + (z >> 1) + Math.floorDiv(y, 3), 3) == 0) return AIR;
            if (y == h && h >= 63) return Math.floorMod((x >> 3) + (z >> 3), 3) == 0 ? MUD : GRASS;
            if (y > h - 4 && y >= 63) return DIRT;
            if (h < 63) {
                // Sea floor: a flooded pocket under it, an air pocket under some dirt,
                // and dirt, clay or sand on top.
                if (y >= h - 7 && y <= h - 3 && Math.floorMod((x >> 3) + (z >> 3), 4) == 0) return WATER;
                if (y == h - 2 && Math.floorMod(x + 2 * z, 7) == 0) return AIR;
                int m = Math.floorMod((x >> 2) + (z >> 3), 3);
                if (m == 0 && y > h - 3) return DIRT;
                if (m == 1 && y == h) return CLAY;
            }
            if (y > h - 2) return SAND;
            return y < 0 ? DEEPSLATE : STONE;
        }
        if (y > 62) return AIR;
        return Math.floorMod(x + z + y, 3) == 0 ? PACKED_ICE : WATER;
    }

    static <T extends Comparable<T>> String valueName(BlockState s, net.minecraft.world.level.block.state.properties.Property<T> p) {
        return p.getName(s.getValue(p));
    }

    static String name(BlockState s) {
        StringBuilder b = new StringBuilder(net.minecraft.core.registries.BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
        TreeMap<String, String> props = new TreeMap<>();
        for (var p : s.getProperties()) props.put(p.getName(), valueName(s, p));
        if (!props.isEmpty()) {
            b.append('[');
            boolean first = true;
            for (var e : props.entrySet()) {
                if (!first) b.append(',');
                first = false;
                b.append(e.getKey()).append('=').append(e.getValue());
            }
            b.append(']');
        }
        return b.toString();
    }

    // LazyChunk is a chunk whose sections hold the terrain once first asked
    // for, so a feature writing sections directly (OreFeature) sees and changes
    // the same world the level proxy serves.
    static class LazyChunk extends ProtoChunk {
        final boolean[] filled = new boolean[HEIGHT >> 4];
        final int cx, cz;
        final World world;

        LazyChunk(int cx, int cz, net.minecraft.world.level.LevelHeightAccessor height, World world) {
            super(new ChunkPos(cx, cz), UpgradeData.EMPTY, height, factory, null);
            this.cx = cx;
            this.cz = cz;
            this.world = world;
        }

        @Override
        public net.minecraft.world.level.chunk.LevelChunkSection getSection(int index) {
            var section = super.getSection(index);
            if (!filled[index]) {
                filled[index] = true;
                for (int y = 0; y < 16; y++) for (int z = 0; z < 16; z++) for (int x = 0; x < 16; x++) {
                    BlockPos p = new BlockPos(cx * 16 + x, MIN_Y + index * 16 + y, cz * 16 + z);
                    // BlockStateSearch reads sections directly, including earlier feature writes.
                    BlockState s = world.overlay.getOrDefault(p.asLong(), terrain(p.getX(), p.getY(), p.getZ()));
                    if (s != AIR) section.setBlockState(x, y, z, s, false);
                }
            }
            return section;
        }
    }

    static class World implements InvocationHandler {
        final Map<Long, BlockState> overlay = new HashMap<>();
        final Map<Long, BlockEntity> entities = new HashMap<>();
        RandomSource random;
        long seed;
        Holder<Biome> biome;
        final Map<Long, LazyChunk> chunks = new HashMap<>();

        BlockState state(BlockPos p) {
            BlockState s = overlay.get(p.asLong());
            if (s != null) return s;
            LazyChunk c = chunks.get(ChunkPos.pack(p.getX() >> 4, p.getZ() >> 4));
            if (c != null && p.getY() >= MIN_Y && p.getY() < MIN_Y + HEIGHT && c.filled[(p.getY() - MIN_Y) >> 4]) {
                return c.getSection((p.getY() - MIN_Y) >> 4).getBlockState(p.getX() & 15, p.getY() & 15, p.getZ() & 15);
            }
            return terrain(p.getX(), p.getY(), p.getZ());
        }

        // set is setBlock: the overlay, and the chunk section when it exists.
        void set(BlockPos p, BlockState state) {
            overlay.put(p.asLong(), state);
            LazyChunk c = chunks.get(ChunkPos.pack(p.getX() >> 4, p.getZ() >> 4));
            if (c != null && p.getY() >= MIN_Y && p.getY() < MIN_Y + HEIGHT && c.filled[(p.getY() - MIN_Y) >> 4]) {
                c.getSection((p.getY() - MIN_Y) >> 4).setBlockState(p.getX() & 15, p.getY() & 15, p.getZ() & 15, state, false);
            }
        }

        @Override
        @SuppressWarnings("unchecked")
        public Object invoke(Object proxy, Method m, Object[] a) throws Throwable {
            switch (m.getName()) {
                case "getBlockState": return state((BlockPos) a[0]);
                case "isStateAtPosition": return ((java.util.function.Predicate<BlockState>) a[1]).test(state((BlockPos) a[0]));
                case "getFluidState": return state((BlockPos) a[0]).getFluidState();
                case "setBlock": {
                    // A block entity exists once its block does: the features draw from the random for it.
                    BlockPos at = (BlockPos) a[0];
                    // vanilla: ProtoChunk.setBlockState rejects writes outside build height.
                    if (at.getY() < MIN_Y || at.getY() >= MIN_Y + HEIGHT) return true;
                    BlockState placed = (BlockState) a[1];
                    set(at, placed);
                    if (placed.getBlock() instanceof net.minecraft.world.level.block.EntityBlock eb) entities.put(at.asLong(), eb.newBlockEntity(at, placed));
                    else entities.remove(at.asLong());
                    return true;
                }
                case "getLevel": return templateLevel;
                case "registryAccess": return templateRegistries;
                case "getMinY": return MIN_Y;
                case "getSeaLevel": return 63;
                case "getHeight":
                    if (a == null || a.length == 0) return HEIGHT;
                    if (m.isDefault()) return InvocationHandler.invokeDefault(proxy, m, a);
                    int x = (Integer) a[1], z = (Integer) a[2];
                    var pred = ((Heightmap.Types) a[0]).isOpaque();
                    // The _WG heightmaps are the terrain's alone: features' writes do not move them.
                    boolean wg = a[0] == Heightmap.Types.WORLD_SURFACE_WG || a[0] == Heightmap.Types.OCEAN_FLOOR_WG;
                    for (int y = MIN_Y + HEIGHT - 1; y >= MIN_Y; y--) {
                        if (pred.test(wg ? terrain(x, y, z) : state(new BlockPos(x, y, z)))) return y + 1;
                    }
                    return MIN_Y;
                case "getBiome": return biome;
                // Features run before the chunk is lit: block light is 0 everywhere.
                case "getBrightness": return 0;
                case "getRandom": return random;
                case "getSeed": return seed;
                case "ensureCanWrite": return true;
                case "scheduleTick": return null;
                // WorldGenRegion drops these; the sculk spreader calls them.
                case "playSound", "levelEvent", "gameEvent", "addParticle": return null;
                case "getEntities": return java.util.List.of();
                case "getBlockEntity":
                    if (m.isDefault()) return InvocationHandler.invokeDefault(proxy, m, a);
                    return entities.get(((BlockPos) a[0]).asLong());
                case "getChunk": {
                    // The post-processing marks and OreFeature's BulkSectionAccess land
                    // here; the chunk's sections are filled from the terrain on first use.
                    int cx = a[0] instanceof BlockPos bp ? bp.getX() >> 4 : (Integer) a[0], cz = a[0] instanceof BlockPos bq ? bq.getZ() >> 4 : (Integer) a[1];
                    return chunks.computeIfAbsent(ChunkPos.pack(cx, cz), k -> new LazyChunk(cx, cz, (net.minecraft.world.level.LevelHeightAccessor) proxy, this));
                }
                case "hashCode": return System.identityHashCode(proxy);
                case "equals": return proxy == a[0];
                case "toString": return "World";
                default:
                    if (m.isDefault()) return InvocationHandler.invokeDefault(proxy, m, a);
                    throw new UnsupportedOperationException(m.getName());
            }
        }
    }

    // biomeIds numbers the biomes in registry order; a chunk's sections need a
    // biome palette, which nothing here reads.
    static net.minecraft.core.IdMap<Holder<Biome>> biomeIds(HolderLookup.RegistryLookup<Biome> biomes) {
        List<Holder<Biome>> list = new ArrayList<>();
        biomes.listElements().forEach(list::add);
        return new net.minecraft.core.IdMap<>() {
            public int getId(Holder<Biome> b) { return list.indexOf(b); }
            public Holder<Biome> byId(int i) { return list.get(i); }
            public int size() { return list.size(); }
            public java.util.Iterator<Holder<Biome>> iterator() { return list.iterator(); }
        };
    }

    static net.minecraft.server.level.ServerLevel templateLevel;

    interface Placer { boolean place(WorldGenLevel level, WorldgenRandom random, BlockPos pos) throws Exception; }

    static void run(PrintWriter out, PrintWriter dump, String kind, String feature, long seed, int x, int y, int z, Placer placer, Holder<Biome> carrier) throws Exception {
        World w = new World();
        w.seed = seed;
        w.biome = carrier;
        WorldgenRandom random = new WorldgenRandom(new XoroshiroRandomSource(seed));
        // WorldGenRegion.random: the region's own stream, not the feature's.
        w.random = new XoroshiroRandomSource(seed).forkPositional().fromHashOf("minecraft:worldgen_region_random")
            .forkPositional().at((x >> 4) * 16, 0, (z >> 4) * 16);
        WorldGenLevel level = (WorldGenLevel) Proxy.newProxyInstance(
            OverworldFeatureGolden.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, w);
        boolean success = placer.place(level, random, new BlockPos(x, y, z));
        TreeMap<String, String> changed = new TreeMap<>();
        for (var e : w.overlay.entrySet()) {
            BlockPos p = BlockPos.of(e.getKey());
            if (e.getValue() == terrain(p.getX(), p.getY(), p.getZ())) continue;
            changed.put(String.format("%09d %04d %09d", p.getX() + 100000000L, p.getY() + 1000, p.getZ() + 100000000L),
                p.getX() + " " + p.getY() + " " + p.getZ() + " " + name(e.getValue()));
        }
        for (LazyChunk c : w.chunks.values()) {
            for (int i = 0; i < c.filled.length; i++) {
                if (!c.filled[i]) continue;
                var section = c.getSection(i);
                for (int ly = 0; ly < 16; ly++) for (int lz = 0; lz < 16; lz++) for (int lx = 0; lx < 16; lx++) {
                    BlockState v = section.getBlockState(lx, ly, lz);
                    int wx = c.cx * 16 + lx, wy = MIN_Y + i * 16 + ly, wz = c.cz * 16 + lz;
                    if (v == terrain(wx, wy, wz)) continue;
                    changed.put(String.format("%09d %04d %09d", wx + 100000000L, wy + 1000, wz + 100000000L), wx + " " + wy + " " + wz + " " + name(v));
                }
            }
        }
        // A placed case that changed nothing is not listed; the test expects
        // none of those either.
        if (sculkOnly) out.println("result " + kind + " " + feature + " " + seed + " " + x + " " + y + " " + z + " " + success);
        if (sculkOnly) out.println("next " + kind + " " + feature + " " + seed + " " + x + " " + y + " " + z + " " + random.nextLong());
        if (kind.equals("case") && changed.isEmpty()) return;
        MessageDigest sha = MessageDigest.getInstance("SHA-256");
        for (String line : changed.values()) {
            sha.update((line + "\n").getBytes("UTF-8"));
            if (dump != null) dump.println(kind + " " + feature + " " + seed + " " + x + " " + y + " " + z + " " + line);
        }
        out.println(kind + " " + feature + " " + seed + " " + x + " " + y + " " + z + " " + changed.size() + " " + java.util.HexFormat.of().formatHex(sha.digest()));
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        // The tags the features' predicates read (air, features_cannot_replace,
        // ...) come from the vanilla data pack, bound as the server does on load.
        var resources = WorldgenSupport.resources();
        WorldgenSupport.bindTags(resources);
        var storage = net.minecraft.world.level.storage.LevelStorageSource.createDefault(java.nio.file.Path.of("feature-golden-worlds")).createAccess("test");
        templateLevel = WorldgenSupport.templateLevel(resources, storage);
        lookup = WorldgenSupport.lookup();
        templateRegistries = (net.minecraft.core.RegistryAccess) WorldgenSupport.loadWorld(resources);
        PACKED_ICE = Blocks.PACKED_ICE.defaultBlockState();
        STONE = Blocks.STONE.defaultBlockState();
        DEEPSLATE = Blocks.DEEPSLATE.defaultBlockState();
        DIRT = Blocks.DIRT.defaultBlockState();
        GRASS = Blocks.GRASS_BLOCK.defaultBlockState();
        WATER = Blocks.WATER.defaultBlockState();
        SAND = Blocks.SAND.defaultBlockState();
        AIR = Blocks.AIR.defaultBlockState();
        MUD = Blocks.MUD.defaultBlockState();
        CLAY = Blocks.CLAY.defaultBlockState();
        PEAK_TOPS = new BlockState[] {GRASS, STONE, Blocks.PACKED_ICE.defaultBlockState(), Blocks.SOUL_SAND.defaultBlockState(), Blocks.MUD.defaultBlockState(), SAND};

        HolderLookup.RegistryLookup<Biome> allBiomes = lookup.lookupOrThrow(Registries.BIOME);
        factory = new PalettedContainerFactory(Strategy.createForBlockStates(Block.BLOCK_STATE_REGISTRY), AIR, null,
            Strategy.createForBiomes(biomeIds(allBiomes)), allBiomes.getOrThrow(net.minecraft.world.level.biome.Biomes.PLAINS), null);
        HolderLookup.RegistryLookup<PlacedFeature> placedLookup = lookup.lookupOrThrow(Registries.PLACED_FEATURE);
        HolderLookup.RegistryLookup<Biome> biomes = lookup.lookupOrThrow(Registries.BIOME);
        // The generator is only asked for the dimension's height.
        ChunkGenerator generator = new NoiseBasedChunkGenerator(
            MultiNoiseBiomeSource.createFromPreset(lookup.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST).getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD)),
            lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(NoiseGeneratorSettings.OVERWORLD));
        PrintWriter out = new PrintWriter(args[0], "UTF-8");
        PrintWriter dump = args.length > 1 && !args[1].isEmpty() ? new PrintWriter(args[1], "UTF-8") : null;
        out.println("# Generated by tools/java/OverworldFeatureGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        sculkOnly = args.length > 2;
        String[] features = sculkOnly ? args[2].split(",") : FEATURES;
        for (String feature : features) {
            largeDripstone = feature.equals("large_dripstone");
            PlacedFeature placed = placedLookup.getOrThrow(ResourceKey.create(Registries.PLACED_FEATURE, Identifier.withDefaultNamespace(feature))).value();
            boolean freeze = feature.equals("freeze_top_layer");
            List<Holder<Biome>> carriers = new ArrayList<>();
            if (freeze) {
                for (String name : FREEZE_BIOMES) carriers.add(biomes.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.withDefaultNamespace(name))));
            } else {
                for (var b : (Iterable<Holder.Reference<Biome>>) biomes.listElements()::iterator) {
                    if (b.value().getGenerationSettings().hasFeature(placed)) {
                        carriers.add(b);
                        break;
                    }
                }
            }
            for (var c : carriers) {
                if (!c.value().getGenerationSettings().hasFeature(placed)) throw new IllegalStateException(c + " does not carry " + feature);
            }
            if (feature.equals("sculk_patch_ancient_city")) carriers.add(biomes.getOrThrow(net.minecraft.world.level.biome.Biomes.DEEP_DARK));
            if (carriers.isEmpty()) throw new IllegalStateException("no biome carries " + feature);
            int cases = freeze ? FREEZE_CASES : casesOf(feature);
            boolean surface = surfaceFeature(feature);
            int[][] chunks = freeze ? FREEZE_CHUNKS : surface ? SURFACE_CHUNKS : CHUNKS;
            out.println("cases " + feature + " " + cases);
            for (int i = 1; i <= cases; i++) {
                long seed = caseSeed(i);
                Holder<Biome> carrier = carriers.get(i % carriers.size());
                int[] c = chunks[i % chunks.length];
                if (surface) c = new int[] {c[0] * 16, c[1] * 16};
                final int x = c[0], z = c[1];
                run(out, dump, "case", feature, seed, x, MIN_Y, z, (level, random, p) -> WorldgenSupport.placeWithBiomeCheck(placed, level, generator, random, p), carrier);
                if (freeze || i > SEEDS.length) continue;
                for (int y : surface ? SURFACE_RAW_YS : RAW_YS) {
                    run(out, dump, "raw", feature, seed, x + 8, y, z + 8,
                        (level, random, p) -> placed.feature().value().place(level, generator, random, p), carrier);
                }
            }
        }
        out.close();
        if (dump != null) dump.close();
        storage.close();
        resources.close();
    }
}
