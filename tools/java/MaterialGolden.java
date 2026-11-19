import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.TreeMap;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeManager;
import net.minecraft.world.level.biome.FixedBiomeSource;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.Strategy;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.levelgen.Aquifer;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldGenerationContext;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.material.MaterialSystem;
import net.minecraft.world.level.levelgen.synth.NormalNoise;

// Writes the 26.3 material-system golden values internal/server/world/terrain's tests
// compare against, computed by the vanilla classes on the server jar (mise run
// gen:material-golden).
//
// surfacedepth <seed> <x> <z> -> n          MaterialSystem.getSurfaceDepth
// secondary <seed> <x> <z> -> d             getSurfaceSecondary, Double.doubleToRawLongBits as 16 hex digits
// band <seed> <x> <y> <z> -> name           getBand
// surfacechunk <dim> <seed> <cx> <cz> <level> <a> <biome> -> sha256
//   a synthetic chunk run through MaterialSystem.buildSurface with the dimension's material rule, the seed's
//   RandomState and a synthetic biome function (biomeName below, repeated by the Go test); the digest is of the
//   block names of every block of the chunk bottom to top, column by column. <biome> is the biome at the
//   chunk's centre column at y 64.
// surfacecolumn <dim> <seed> <cx> <cz> <level> <a> <x> <z> -> runs     three columns of it, run-length encoded
// melt <dim> <seed> <cx> <cz> -> 256 digits     per column z * 16 + x of a frozen ocean chunk,
//   Biome.shouldMeltFrozenOceanIcebergSlightly at the sea level
public class MaterialGolden {
    static final long[] SEEDS = {12345L, -4172144997902289642L};
    static final int[] LEVELS = {58, 100, 40, 160, 66, 220};
    static PrintWriter out;
    static HolderLookup.Provider lookup;

    static final String[] OVERWORLD_BIOMES = {"plains", "wooded_badlands", "swamp", "mangrove_swamp", "badlands", "eroded_badlands",
        "frozen_ocean", "deep_frozen_ocean", "warm_ocean", "beach", "snowy_beach", "desert", "lukewarm_ocean", "deep_lukewarm_ocean",
        "frozen_peaks", "jagged_peaks", "snowy_slopes", "grove", "stony_peaks", "stony_shore", "windswept_hills", "dripstone_caves",
        "sulfur_caves", "windswept_savanna", "windswept_gravelly_hills", "old_growth_pine_taiga", "old_growth_spruce_taiga",
        "ice_spikes", "mushroom_fields", "dappled_forest", "forest", "taiga", "ocean"};
    static final String[] NETHER_BIOMES = {"nether_wastes", "soul_sand_valley", "crimson_forest", "warped_forest", "basalt_deltas"};
    static final String[] END_BIOMES = {"the_end"};

    // biomeName is the synthetic biome of a quart position; the Go test repeats it.
    static String biomeName(String dim, int qx, int qy, int qz) {
        String[] names = dim.equals("nether") ? NETHER_BIOMES : dim.equals("end") ? END_BIOMES : OVERWORLD_BIOMES;
        int idx = Math.floorMod(5 * (qx >> 2) + 11 * (qz >> 2) + (qy < 0 ? 7 : 0), names.length);
        return names[idx];
    }

    // height is the synthetic terrain's floor; the Go test repeats it.
    static int height(int level, int a, int x, int z) {
        return level + ((x * a + z * (a + 1)) % (3 * a + 2)) + ((x / 8) * 5 + (z / 8) * 3) % 4 * 2;
    }

    static NoiseGeneratorSettings settings(String dim) {
        var key = switch (dim) {
            case "nether" -> NoiseGeneratorSettings.NETHER;
            case "end" -> NoiseGeneratorSettings.END;
            default -> NoiseGeneratorSettings.OVERWORLD;
        };
        return lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(key).value();
    }

    static String name(BlockState s) {
        return BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath();
    }

    static class Probe extends MaterialSystem {
        Probe(RandomState state, BlockState defaultBlock, int seaLevel, NoiseGeneratorSettings settings, long seed) {
            super(state, defaultBlock, seaLevel, settings.noiseRouter().chunkSurfaceLevel(), new XoroshiroRandomSource(seed).forkPositional());
        }

        int depth(int x, int z) {
            return getSurfaceDepth(x, z);
        }

        double secondary(int x, int z) {
            return getSurfaceSecondary(x, z);
        }

        BlockState bandAt(int x, int y, int z) {
            return getBand(x, y, z);
        }
    }

    static void probes() {
        NoiseGeneratorSettings settings = settings("overworld");
        for (long seed : SEEDS) {
            RandomState state = RandomState.create(noises(), seed, settings);
            Probe probe = new Probe(state, settings.defaultBlock(), settings.seaLevel(), settings, seed);
            java.util.Random rng = new java.util.Random(seed);
            for (int i = 0; i < 40; i++) {
                int x = rng.nextInt(2001) - 1000, z = rng.nextInt(2001) - 1000;
                out.println("surfacedepth " + seed + " " + x + " " + z + " -> " + probe.depth(x, z));
                out.println("secondary " + seed + " " + x + " " + z + " -> " + String.format("%016x", Double.doubleToRawLongBits(probe.secondary(x, z))));
                int y = rng.nextInt(384) - 64;
                out.println("band " + seed + " " + x + " " + y + " " + z + " -> " + name(probe.bandAt(x, y, z)));
            }
        }
    }

    @SuppressWarnings("unchecked")
    static net.minecraft.core.HolderGetter<NormalNoise> noises() {
        return (net.minecraft.core.HolderGetter<NormalNoise>) (net.minecraft.core.HolderGetter<?>) lookup.lookupOrThrow(Registries.NOISE);
    }

    static void chunk(String dim, long seed, int cx, int cz, int level, int a, int index) throws Exception {
        NoiseGeneratorSettings settings = settings(dim);
        var biomeRegistry = lookup.lookupOrThrow(Registries.BIOME);
        int minY = dim.equals("overworld") ? -64 : 0, height = dim.equals("overworld") ? 384 : 256;
        LevelHeightAccessor heights = LevelHeightAccessor.create(minY, height);
        net.minecraft.core.IdMapper<Holder<Biome>> biomeIds = new net.minecraft.core.IdMapper<>();
        biomeRegistry.listElements().forEach(biomeIds::add);
        Holder<Biome> plains = biomeRegistry.getOrThrow(net.minecraft.world.level.biome.Biomes.PLAINS);
        PalettedContainerFactory containers = new PalettedContainerFactory(Strategy.createForBlockStates(Block.BLOCK_STATE_REGISTRY), Blocks.AIR.defaultBlockState(), null, Strategy.createForBiomes(biomeIds), plains, null);
        Holder<NoiseGeneratorSettings> settingsHolder = lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(
            dim.equals("nether") ? NoiseGeneratorSettings.NETHER : dim.equals("end") ? NoiseGeneratorSettings.END : NoiseGeneratorSettings.OVERWORLD);
        NoiseBasedChunkGenerator generator = new NoiseBasedChunkGenerator(new FixedBiomeSource(plains), settingsHolder);
        RandomState random = RandomState.create(noises(), seed, settings);
        BiomeManager biomes = new BiomeManager((qx, qy, qz) -> biomeRegistry.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.withDefaultNamespace(biomeName(dim, qx, qy, qz)))),
            BiomeManager.obfuscateSeed(seed));

        ProtoChunk chunk = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, heights, containers, null);
        chunk.getOrCreateHeightmapUnprimed(Heightmap.Types.WORLD_SURFACE_WG);
        BlockState base = settings.defaultBlock();
        BlockState water = Blocks.WATER.defaultBlockState();
        BlockState lava = Blocks.LAVA.defaultBlockState();
        BlockPos.MutableBlockPos pos = new BlockPos.MutableBlockPos();
        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                int bx = cx * 16 + x, bz = cz * 16 + z;
                int h = height(level, a, bx, bz);
                if (dim.equals("overworld")) {
                    for (int y = -64; y <= Math.max(h, 62); y++) {
                        chunk.setBlockState(pos.set(bx, y, bz), y <= h ? base : water);
                    }
                } else if (dim.equals("nether")) {
                    int floor = 20 + (h % 60), ceiling = 100 + ((bx * 3 + bz * 7) & 15);
                    for (int y = 0; y <= 127; y++) {
                        if (y <= floor || y >= ceiling) {
                            chunk.setBlockState(pos.set(bx, y, bz), base);
                        } else if (y <= 32) {
                            chunk.setBlockState(pos.set(bx, y, bz), lava);
                        }
                    }
                } else {
                    for (int y = 0; y <= 20 + (h % 50); y++) {
                        chunk.setBlockState(pos.set(bx, y, bz), base);
                    }
                }
            }
        }
        DensityVolume volume = new DensityVolume(16, settings.noiseSettings().height(), 16, cx * 16, settings.noiseSettings().minY(), cz * 16);
        Aquifer.FluidStatus sea = new Aquifer.FluidStatus(settings.seaLevel(), settings.defaultFluid());
        NoiseChunk noiseChunk = new NoiseChunk(random, Beardifier.EMPTY, settings, (x, y, z) -> sea, Blender.empty(), volume);
        random.surfaceSystem().buildSurface(random, biomes, new WorldGenerationContext(generator, heights), chunk, noiseChunk, settings.materialRule().value(), null);

        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        List<String> columns = new ArrayList<>();
        TreeMap<String, Integer> counts = new TreeMap<>();
        for (int x = 0; x < 16; x++) {
            for (int z = 0; z < 16; z++) {
                StringBuilder rle = new StringBuilder();
                String last = null;
                int run = 0;
                for (int y = minY; y < minY + height; y++) {
                    String name = name(chunk.getBlockState(pos.set(cx * 16 + x, y, cz * 16 + z)));
                    digest.update((name + "\n").getBytes(StandardCharsets.UTF_8));
                    counts.merge(name, 1, Integer::sum);
                    if (name.equals(last)) {
                        run++;
                        continue;
                    }
                    if (last != null) {
                        rle.append(last).append('*').append(run).append(',');
                    }
                    last = name;
                    run = 1;
                }
                rle.append(last).append('*').append(run);
                if ((x == 0 && z == 0) || (x == 7 && z == 9) || (x == 15 && z == 15)) {
                    columns.add("surfacecolumn " + dim + " " + seed + " " + cx + " " + cz + " " + level + " " + a + " " + x + " " + z + " -> " + rle);
                }
            }
        }
        String centre = biomes.getBiome(pos.set(cx * 16 + 8, 64, cz * 16 + 8)).unwrapKey().orElseThrow().identifier().getPath();
        out.println("surfacechunk " + dim + " " + seed + " " + cx + " " + cz + " " + level + " " + a + " " + centre + " -> " + HexFormat.of().formatHex(digest.digest()));
        for (String c : columns) {
            out.println(c);
        }
        System.err.println(dim + " " + seed + " " + cx + "," + cz + " " + centre + " " + counts);
        if (centre.equals("frozen_ocean") || centre.equals("deep_frozen_ocean")) {
            StringBuilder melt = new StringBuilder();
            for (int z = 0; z < 16; z++) {
                for (int x = 0; x < 16; x++) {
                    Holder<Biome> b = biomes.getBiome(pos.set(cx * 16 + x, chunk.getHeight(Heightmap.Types.WORLD_SURFACE_WG, x, z) + 1, cz * 16 + z));
                    melt.append(b.value().shouldMeltFrozenOceanIcebergSlightly(pos.set(cx * 16 + x, 63, cz * 16 + z), 63) ? '1' : '0');
                }
            }
            out.println("melt " + dim + " " + seed + " " + cx + " " + cz + " -> " + melt);
        }
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        lookup = WorldgenSupport.lookup();
        out = new PrintWriter(args[0], "UTF-8");
        out.println("# Generated by tools/java/MaterialGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        out.println("# Grammar: see the header comment of tools/java/MaterialGolden.java.");
        probes();
        int index = 0;
        for (int cx = 0; cx < OVERWORLD_BIOMES.length; cx++, index++) {
            chunk("overworld", SEEDS[0], cx, 0, LEVELS[index % LEVELS.length], index % 2 == 0 ? 1 : 3, index);
        }
        for (int cx = -4; cx < 4; cx++, index++) {
            chunk("overworld", SEEDS[1], cx, cx * 3 - 1, LEVELS[index % LEVELS.length], index % 2 == 0 ? 1 : 3, index);
        }
        for (int cx = 0; cx < 6; cx++, index++) {
            chunk("nether", SEEDS[0], cx, 1, 40 + 15 * (index % 4), index % 2 == 0 ? 1 : 3, index);
        }
        for (int cx = 0; cx < 2; cx++, index++) {
            chunk("end", SEEDS[0], cx * 9, -4, 30, 1, index);
        }
        out.close();
    }
}
