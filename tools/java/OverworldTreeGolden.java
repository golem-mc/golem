import java.io.PrintWriter;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.function.Predicate;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.ListTag;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.EntityBlock;
import net.minecraft.world.level.block.entity.BeehiveBlockEntity;
import net.minecraft.world.level.block.entity.BlockEntity;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.placement.PlacedFeature;

// Writes internal/server/world/levelgen/testdata/overworld_trees_golden.txt: what the
// vanilla server's own placed and configured tree features write on a small
// synthetic world (mise run gen:overworld-trees-golden). The world is a
// function of the column alone, which internal/server/world/overworld_trees_golden_test.go
// recomputes, and the level is a proxy answering only what a feature asks of a
// WorldGenRegion, so a call this does not model fails loudly.
//
// Line formats (the reader is the test above):
//   placed <name> <seed> <cx> <cz> <index> <step> <placed> <changes> <sha256>
//   configured <name> <seed> <cx> <cz> <index> <step> <x> <y> <z> <placed> <changes> <sha256>
//   deep <name> <seed> <cx> <cz> <index> <step> <placed> <changes> <sha256>
// A deep line runs a placed feature of DEEP_PLACED on the same world with
// every floor DEEP_DROP blocks lower, so a full kelp column fits under the sea.
// The digest covers every block the run left different from the base world,
// in x, y, z order, and the bees of each bee nest.
public class OverworldTreeGolden {
    static final int MIN_Y = -64;
    static final int HEIGHT = 384;
    static final int SEA_TOP = 62;
    static final long[] SEEDS = {0L, 12345L};
    static final int[][] CHUNKS = {{0, 0}, {1, -1}, {-2, 3}, {5, 5}, {-7, -4}, {11, -9}};
    static final int ORIGINS = 8;
    static final String[] DEEP_PLACED = {"minecraft:kelp_cold", "minecraft:kelp_warm"};
    static final int DEEP_DROP = 30;
    // drop lowers every floor of the world the current case runs on.
    static int drop = 0;

    static BlockState grass;
    static BlockState dirt;
    static BlockState stone;
    static BlockState sand;
    static BlockState water;
    static BlockState bedrock;
    static BlockState air;

    // surface is the top solid block of a column: 8x8 cells of fixed height.
    static int surface(int x, int z) {
        int v = ((x >> 3) * 73856093) ^ ((z >> 3) * 19349663);
        return 58 + (int) ((v >>> 8) % 9);
    }

    static BlockState base(int x, int y, int z) {
        if (y < MIN_Y || y >= MIN_Y + HEIGHT) return air;
        if (y == MIN_Y) return bedrock;
        int h = surface(x, z) - drop;
        if (y > h) return y <= SEA_TOP ? water : air;
        if (y == h) return h <= SEA_TOP ? sand : grass;
        if (y >= h - 3) return h <= SEA_TOP ? sand : dirt;
        if (y >= h - 12 && ((x >> 3) + (z >> 3) & 1) == 0) return air; // a cave under every other cell
        return stone;
    }

    static class Level implements InvocationHandler {
        final Map<Long, BlockState> changed = new HashMap<>();
        final Map<Long, BlockEntity> entities = new HashMap<>();
        final HolderLookup.Provider lookup;
        final long seed;
        final net.minecraft.util.RandomSource random;
        final int cx, cz;

        Level(HolderLookup.Provider lookup, long seed, int cx, int cz) {
            this.lookup = lookup;
            this.seed = seed;
            this.cx = cx;
            this.cz = cz;
            // WorldGenRegion.random: the region's own stream, not the feature's.
            this.random = new XoroshiroRandomSource(seed).forkPositional().fromHashOf("minecraft:worldgen_region_random")
                .forkPositional().at(cx * 16, 0, cz * 16);
        }

        BlockState get(BlockPos p) {
            BlockState s = changed.get(p.asLong());
            return s != null ? s : base(p.getX(), p.getY(), p.getZ());
        }

        boolean canWrite(BlockPos p) {
            return Math.abs((p.getX() >> 4) - cx) <= 1 && Math.abs((p.getZ() >> 4) - cz) <= 1;
        }

        int height(Heightmap.Types type, int x, int z) {
            // The _WG heightmaps are filled before the features run and stay on the terrain.
            boolean terrainOnly = type == Heightmap.Types.OCEAN_FLOOR_WG || type == Heightmap.Types.WORLD_SURFACE_WG;
            for (int y = MIN_Y + HEIGHT - 1; y >= MIN_Y; y--) {
                if (type.isOpaque().test(terrainOnly ? base(x, y, z) : get(new BlockPos(x, y, z)))) return y + 1;
            }
            return MIN_Y;
        }

        @Override
        @SuppressWarnings("unchecked")
        public Object invoke(Object proxy, Method m, Object[] a) throws Throwable {
            switch (m.getName()) {
                case "getBlockState":
                    return get((BlockPos) a[0]);
                case "getFluidState":
                    return get((BlockPos) a[0]).getFluidState();
                case "isStateAtPosition":
                    return ((Predicate<BlockState>) a[1]).test(get((BlockPos) a[0]));
                case "isFluidAtPosition":
                    return ((Predicate<net.minecraft.world.level.material.FluidState>) a[1]).test(get((BlockPos) a[0]).getFluidState());
                case "getRawBrightness":
                    // vanilla: SkyLightSectionStorage.getLightValue; FEATURES runs before INITIALIZE_LIGHT, so the column has no sky sections
                    return 15;
                case "isClientSide":
                    return false;
                case "getMinY":
                    return MIN_Y;
                case "getHeight":
                    if (a == null || a.length == 0) return HEIGHT;
                    if (a.length == 2) return height((Heightmap.Types) a[0], ((BlockPos) a[1]).getX(), ((BlockPos) a[1]).getZ());
                    return height((Heightmap.Types) a[0], (Integer) a[1], (Integer) a[2]);
                case "ensureCanWrite":
                    return canWrite((BlockPos) a[0]);
                case "getChunk":
                    // Only Feature.markAboveForPostProcessing asks, to mark a position;
                    // postprocessing runs after the chunk is complete, outside this stage.
                    return chunk();
                case "getRandom":
                    return random;
                case "registryAccess":
                    return (net.minecraft.core.RegistryAccess) lookup;
                case "getLevel":
                    return level();
                case "getSeed":
                    return seed;
                case "setBlock": {
                    BlockPos p = (BlockPos) a[0];
                    BlockState s = (BlockState) a[1];
                    if (!canWrite(p) || p.getY() < MIN_Y || p.getY() >= MIN_Y + HEIGHT) return false;
                    changed.put(p.asLong(), s);
                    entities.remove(p.asLong());
                    return true;
                }
                case "getBlockEntity": {
                    if (a.length != 1) return InvocationHandler.invokeDefault(proxy, m, a);
                    BlockPos p = (BlockPos) a[0];
                    BlockEntity be = entities.get(p.asLong());
                    BlockState s = get(p);
                    if (be == null && s.hasBlockEntity()) {
                        be = ((EntityBlock) s.getBlock()).newBlockEntity(p, s);
                        entities.put(p.asLong(), be);
                    }
                    return be;
                }
                case "blockUpdated":
                case "scheduleTick":
                case "neighborShapeChanged":
                case "updateNeighborsAt":
                    return null;
                case "equals":
                    return proxy == a[0];
                case "hashCode":
                    return System.identityHashCode(proxy);
                case "toString":
                    return "TreeGoldenLevel";
                default:
                    if (m.isDefault()) return InvocationHandler.invokeDefault(proxy, m, a);
                    throw new UnsupportedOperationException(m.toString());
            }
        }
    }

    static net.minecraft.world.level.chunk.ProtoChunk chunk() throws Exception {
        var f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        return (net.minecraft.world.level.chunk.ProtoChunk) ((sun.misc.Unsafe) f.get(null)).allocateInstance(NoMarkChunk.class);
    }

    static net.minecraft.world.level.chunk.ChunkGenerator generatorOfLevel;

    static net.minecraft.server.level.ServerLevel templateLevel;

    static net.minecraft.server.level.ServerLevel level() {
        return templateLevel;
    }

    static void prepareLevel() throws Exception {
        var f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        var unsafe = (sun.misc.Unsafe) f.get(null);
        var level = templateLevel;
        var cache = (net.minecraft.server.level.ServerChunkCache) unsafe.allocateInstance(net.minecraft.server.level.ServerChunkCache.class);
        var map = (net.minecraft.server.level.ChunkMap) unsafe.allocateInstance(net.minecraft.server.level.ChunkMap.class);
        var context = new net.minecraft.world.level.chunk.status.WorldGenContext(level, generatorOfLevel, null, null, null, null);
        // vanilla: ServerChunkCache.getGenerator -> ChunkMap.generator -> WorldGenContext.generator.
        for (Object[] binding : new Object[][] {{map, "worldGenContext", context}, {cache, "chunkMap", map}, {level, "chunkSource", cache}}) {
            var field = binding[0].getClass().getDeclaredField((String) binding[1]);
            field.setAccessible(true);
            field.set(binding[0], binding[2]);
        }
    }

    static class NoMarkChunk extends net.minecraft.world.level.chunk.ProtoChunk {
        NoMarkChunk() {
            super(null, null, null, null, null);
        }

        @Override
        public void markPosForPostProcessing(BlockPos p) {}
    }

    static <T extends Comparable<T>> String valueName(Property<T> p, BlockState s) {
        return p.getName(s.getValue(p));
    }

    static String describe(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
        TreeMap<String, String> props = new TreeMap<>();
        for (Property<?> p : s.getProperties()) props.put(p.getName(), valueName(p, s));
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

    // digest hashes every changed block and bee nest; it returns "<changes> <sha>".
    static String digest(Level level) throws Exception {
        List<long[]> keys = new ArrayList<>();
        for (var e : level.changed.entrySet()) {
            BlockPos p = BlockPos.of(e.getKey());
            if (e.getValue() != base(p.getX(), p.getY(), p.getZ())) keys.add(new long[] {p.getX(), p.getY(), p.getZ()});
        }
        keys.sort((p, q) -> p[0] != q[0] ? Long.compare(p[0], q[0]) : p[1] != q[1] ? Long.compare(p[1], q[1]) : Long.compare(p[2], q[2]));
        MessageDigest sha = MessageDigest.getInstance("SHA-256");
        for (long[] k : keys) {
            BlockPos p = new BlockPos((int) k[0], (int) k[1], (int) k[2]);
            sha.update((k[0] + " " + k[1] + " " + k[2] + " " + describe(level.changed.get(p.asLong())) + "\n").getBytes(StandardCharsets.UTF_8));
        }
        List<long[]> hives = new ArrayList<>();
        for (var e : level.entities.entrySet()) {
            if (e.getValue() instanceof BeehiveBlockEntity) {
                BlockPos p = BlockPos.of(e.getKey());
                hives.add(new long[] {p.getX(), p.getY(), p.getZ()});
            }
        }
        hives.sort((p, q) -> p[0] != q[0] ? Long.compare(p[0], q[0]) : p[1] != q[1] ? Long.compare(p[1], q[1]) : Long.compare(p[2], q[2]));
        for (long[] k : hives) {
            BlockEntity be = level.entities.get(new BlockPos((int) k[0], (int) k[1], (int) k[2]).asLong());
            CompoundTag tag = be.saveWithoutMetadata(level.lookup);
            ListTag bees = tag.getListOrEmpty("bees");
            StringBuilder line = new StringBuilder("hive " + k[0] + " " + k[1] + " " + k[2]);
            for (int i = 0; i < bees.size(); i++) line.append(' ').append(bees.getCompoundOrEmpty(i).getIntOr("ticks_in_hive", -1));
            sha.update((line + "\n").getBytes(StandardCharsets.UTF_8));
        }
        return keys.size() + " " + HexFormat.of().formatHex(sha.digest());
    }

    static WorldGenLevel proxy(Level level) {
        return (WorldGenLevel) Proxy.newProxyInstance(OverworldTreeGolden.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, level);
    }

    static WorldgenRandom random(long seed, int cx, int cz, int index, int step) {
        WorldgenRandom r = new WorldgenRandom(new XoroshiroRandomSource(0L));
        long decoration = r.setDecorationSeed(seed, cx * 16, cz * 16);
        r.setFeatureSeed(decoration, index, step);
        return r;
    }

    static net.minecraft.world.level.chunk.ChunkGenerator generator(HolderLookup.Provider lookup) {
        var settings = net.minecraft.world.level.levelgen.flat.FlatLevelGeneratorSettings.getDefault(
            lookup.lookupOrThrow(Registries.BIOME), lookup.lookupOrThrow(Registries.STRUCTURE_SET), lookup.lookupOrThrow(Registries.PLACED_FEATURE));
        return new net.minecraft.world.level.levelgen.FlatLevelSource(settings);
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        // Block and fluid tags are empty until a data pack binds them, and the
        // features ask for them (SUPPORTS_VEGETATION, LOGS, REPLACEABLE, ...).
        var resources = WorldgenSupport.resources();
        WorldgenSupport.bindTags(resources);
        grass = Blocks.GRASS_BLOCK.defaultBlockState();
        dirt = Blocks.DIRT.defaultBlockState();
        stone = Blocks.STONE.defaultBlockState();
        sand = Blocks.SAND.defaultBlockState();
        water = Blocks.WATER.defaultBlockState();
        bedrock = Blocks.BEDROCK.defaultBlockState();
        air = Blocks.AIR.defaultBlockState();
        PrintWriter out = new PrintWriter(args[0], "UTF-8");
        out.println("# Generated by tools/java/OverworldTreeGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        // The data pack's JSON is what a server decodes; the code-built registries
        // differ from it where a codec's getter does (CherryFoliagePlacer).
        HolderLookup.Provider lookup = WorldgenSupport.loadWorld(resources);
        var chunkGenerator = generator(lookup);
        generatorOfLevel = chunkGenerator;
        var storage = net.minecraft.world.level.storage.LevelStorageSource.createDefault(java.nio.file.Path.of("tree-golden-worlds")).createAccess("test");
        templateLevel = WorldgenSupport.templateLevel(resources, storage);
        prepareLevel();
        List<String> placedNames = new ArrayList<>();
        List<String> configuredNames = new ArrayList<>();
        for (String line : java.nio.file.Files.readAllLines(java.nio.file.Path.of(args[1]))) {
            String[] f = line.split(" ", 3);
            if (f.length < 3) continue;
            if (f[0].equals("placed")) placedNames.add(f[1]);
            else if (f[0].equals("configured")) configuredNames.add(f[1]);
        }
        int index = 0;
        int deepIndex = 0;
        for (long seed : SEEDS) {
            for (String name : placedNames) {
                PlacedFeature feature = lookup.lookupOrThrow(Registries.PLACED_FEATURE)
                    .getOrThrow(ResourceKey.create(Registries.PLACED_FEATURE, Identifier.parse(name))).value();
                // The biome filter asks which biome lists the feature, which is the
                // decoration pipeline's business and not this stage's.
                List<net.minecraft.world.level.levelgen.placement.PlacementModifier> mods = new ArrayList<>();
                for (var m : feature.placement()) if (!(m instanceof net.minecraft.world.level.levelgen.placement.BiomeFilter)) mods.add(m);
                feature = new PlacedFeature(feature.feature(), mods);
                for (int[] c : CHUNKS) {
                    int idx = (index++ % 40) + 1, step = 8;
                    Level level = new Level(lookup, seed, c[0], c[1]);
                    boolean placed = feature.place(proxy(level), chunkGenerator, random(seed, c[0], c[1], idx, step), new BlockPos(c[0] * 16, MIN_Y, c[1] * 16));
                    out.println("placed " + name + " " + seed + " " + c[0] + " " + c[1] + " " + idx + " " + step + " " + (placed ? 1 : 0) + " " + digest(level));
                }
            }
            drop = DEEP_DROP;
            for (String name : DEEP_PLACED) {
                PlacedFeature feature = lookup.lookupOrThrow(Registries.PLACED_FEATURE)
                    .getOrThrow(ResourceKey.create(Registries.PLACED_FEATURE, Identifier.parse(name))).value();
                List<net.minecraft.world.level.levelgen.placement.PlacementModifier> mods = new ArrayList<>();
                for (var m : feature.placement()) if (!(m instanceof net.minecraft.world.level.levelgen.placement.BiomeFilter)) mods.add(m);
                feature = new PlacedFeature(feature.feature(), mods);
                for (int[] c : CHUNKS) {
                    int idx = (deepIndex++ % 40) + 1, step = 9; // vanilla: VEGETAL_DECORATION
                    Level level = new Level(lookup, seed, c[0], c[1]);
                    boolean placed = feature.place(proxy(level), chunkGenerator, random(seed, c[0], c[1], idx, step), new BlockPos(c[0] * 16, MIN_Y, c[1] * 16));
                    out.println("deep " + name + " " + seed + " " + c[0] + " " + c[1] + " " + idx + " " + step + " " + (placed ? 1 : 0) + " " + digest(level));
                }
            }
            drop = 0;
            for (String name : configuredNames) {
                var registry = WorldgenSupport.featureRegistry();
                Object feature = lookup.lookupOrThrow(registry)
                    .getOrThrow(ResourceKey.create(registry, Identifier.parse(name))).value();
                for (int k = 0; k < ORIGINS; k++) {
                    int[] c = CHUNKS[k % CHUNKS.length];
                    int x = c[0] * 16 + (k * 5 + 3) % 16;
                    int z = c[1] * 16 + (k * 11 + 7) % 16;
                    int y = name.equals("minecraft:rooted_azalea_tree") ? surface(x, z) - 8 : surface(x, z) + 1;
                    int idx = (index++ % 40) + 1, step = 8;
                    Level level = new Level(lookup, seed, c[0], c[1]);
                    boolean placed = WorldgenSupport.place(feature, proxy(level), chunkGenerator, random(seed, c[0], c[1], idx, step), new BlockPos(x, y, z));
                    out.println("configured " + name + " " + seed + " " + c[0] + " " + c[1] + " " + idx + " " + step + " " + x + " " + y + " " + z + " " + (placed ? 1 : 0) + " " + digest(level));
                }
            }
        }
        out.close();
        storage.close();
        resources.close();
    }
}
