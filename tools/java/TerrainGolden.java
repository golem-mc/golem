import java.io.PrintWriter;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
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
import net.minecraft.world.level.biome.Biomes;
import net.minecraft.world.level.biome.FixedBiomeSource;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.CarvingMask;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.Strategy;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldGenerationContext;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.synth.NormalNoise;

// Writes the 26.3 terrain golden values internal/server/world/terrain's tests compare
// against: NoiseBasedChunkGenerator's doFill, buildSurface and generateCarvers
// run in that order on an empty ProtoChunk (mise run gen:terrain-golden).
//
// terrain <dim> <seed> <cx> <cz> -> sha256
//   the digest is of the block names of every block of the chunk bottom to top, column by column.
//   The surface reads MaterialGolden.biomeName's synthetic biomes; the carvers read the one biome the
//   dimension's biome source holds (plains, nether_wastes or the_end).
// terraincolumn <dim> <seed> <cx> <cz> <x> <z> -> runs     three columns of it, run-length encoded
// terrainheight <dim> <seed> <cx> <cz> -> 256 ints         per column z * 16 + x, ProtoChunk.getHeight(WORLD_SURFACE_WG)
// terrainpost <dim> <seed> <cx> <cz> -> count sha256       post-processing lists as "section packed-offset\n", preserving duplicates and order
// terrainmask <dim> <seed> <cx> <cz> -> count sha256       one byte (0/1) per block, x then z then y ascending
public class TerrainGolden {
    static final long[] SEEDS = {12345L, -4172144997902289642L};
    static PrintWriter out;
    static HolderLookup.Provider lookup;

    static String name(BlockState s) {
        return BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath();
    }

    @SuppressWarnings("unchecked")
    static net.minecraft.core.HolderGetter<NormalNoise> noises() {
        return (net.minecraft.core.HolderGetter<NormalNoise>) (net.minecraft.core.HolderGetter<?>) lookup.lookupOrThrow(Registries.NOISE);
    }

    static Method method(String name) {
        for (Method m : NoiseBasedChunkGenerator.class.getDeclaredMethods()) {
            if (m.getName().equals(name)) {
                m.setAccessible(true);
                return m;
            }
        }
        throw new IllegalStateException(name);
    }

    static void chunk(String dim, long seed, int cx, int cz) throws Exception {
        chunk(dim, seed, cx, cz, null);
    }

    static void chunk(String dim, long seed, int cx, int cz, OverworldTerrainGolden.NaturalBiomes natural) throws Exception {
        var biomeRegistry = lookup.lookupOrThrow(Registries.BIOME);
        var key = dim.equals("nether") ? NoiseGeneratorSettings.NETHER : dim.equals("end") ? NoiseGeneratorSettings.END : NoiseGeneratorSettings.OVERWORLD;
        Holder<NoiseGeneratorSettings> settingsHolder = lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(key);
        NoiseGeneratorSettings settings = settingsHolder.value();
        int minY = dim.equals("overworld") ? -64 : 0, height = dim.equals("overworld") ? 384 : 256;
        LevelHeightAccessor heights = LevelHeightAccessor.create(minY, height);
        net.minecraft.core.IdMapper<Holder<Biome>> biomeIds = new net.minecraft.core.IdMapper<>();
        biomeRegistry.listElements().forEach(biomeIds::add);
        Holder<Biome> plains = biomeRegistry.getOrThrow(Biomes.PLAINS);
        PalettedContainerFactory containers = new PalettedContainerFactory(Strategy.createForBlockStates(Block.BLOCK_STATE_REGISTRY), Blocks.AIR.defaultBlockState(), null, Strategy.createForBiomes(biomeIds), plains, null);
        Holder<Biome> fixed = biomeRegistry.getOrThrow(dim.equals("nether") ? Biomes.NETHER_WASTES : dim.equals("end") ? Biomes.THE_END : Biomes.PLAINS);
        NoiseBasedChunkGenerator generator = new NoiseBasedChunkGenerator(natural == null ? new FixedBiomeSource(fixed) : natural.source, settingsHolder);
        RandomState random = RandomState.create(noises(), seed, settings);
        BiomeManager biomes = new BiomeManager((qx, qy, qz) -> biomeRegistry.getOrThrow(ResourceKey.create(Registries.BIOME, Identifier.withDefaultNamespace(MaterialGolden.biomeName(dim, qx, qy, qz)))),
            BiomeManager.obfuscateSeed(seed));

        if (natural != null) {
            random = natural.random;
            biomes = natural.manager;
        }

        ProtoChunk chunk = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, heights, containers, null);
        DensityVolume volume = new DensityVolume(16, settings.noiseSettings().height(), 16, cx * 16, settings.noiseSettings().minY(), cz * 16);
        var fluids = (net.minecraft.world.level.levelgen.Aquifer.FluidPicker) method("createFluidPicker").invoke(null, settings);
        NoiseChunk noiseChunk = new NoiseChunk(random, Beardifier.EMPTY, settings, fluids, Blender.empty(), volume);
        Object rule = settings.materialRule().value();
        method("doFill").invoke(generator, noiseChunk, chunk);
        method("buildSurface").invoke(generator, chunk, noiseChunk, random, biomes, null, rule);
        method("generateCarvers").invoke(generator, chunk, Blender.empty(), noiseChunk, random, biomes, null, rule);

        String prefix = dim + " " + seed + " " + cx + " " + cz;
        if (natural != null) {
            StringBuilder melt = new StringBuilder();
            BlockPos.MutableBlockPos pos = new BlockPos.MutableBlockPos();
            for (int z = 0; z < 16; z++) {
                for (int x = 0; x < 16; x++) {
                    pos.set(cx * 16 + x, 63, cz * 16 + z);
                    melt.append(biomes.getBiome(pos).value().shouldMeltFrozenOceanIcebergSlightly(pos, 63) ? '1' : '0');
                }
            }
            out.println("terrainmelt " + prefix + " -> " + melt);
        }
        MessageDigest postDigest = MessageDigest.getInstance("SHA-256");
        int postCount = 0;
        var post = chunk.getPostProcessing();
        for (int section = 0; section < post.length; section++) {
            if (post[section] == null) continue;
            for (short offset : post[section]) {
                postDigest.update((section + " " + offset + "\n").getBytes(StandardCharsets.UTF_8));
                postCount++;
            }
        }
        out.println("terrainpost " + prefix + " -> " + postCount + " " + HexFormat.of().formatHex(postDigest.digest()));

        WorldGenerationContext context = new WorldGenerationContext(generator, heights);
        CarvingMask mask = new CarvingMask(context.getMinGenY() + 1, context.getMinGenY() + context.getGenDepth() - 1 - 7);
        WorldgenRandom carverRandom = new WorldgenRandom(new LegacyRandomSource(0));
        for (int dx = -8; dx <= 8; dx++) {
            for (int dz = -8; dz <= 8; dz++) {
                ChunkPos source = new ChunkPos(cx + dx, cz + dz);
                int index = 0;
                var sourceBiome = natural == null ? fixed : natural.carving.getNoiseBiome(source.x() * 4, 0, source.z() * 4);
                for (var carver : sourceBiome.value().getGenerationSettings().getCarvers()) {
                    carverRandom.setLargeFeatureSeed(seed + index, source.x(), source.z());
                    if (carver.value().isStartChunk(carverRandom)) {
                        carver.value().carve(context, carverRandom, chunk.getPos(), source, mask);
                    }
                    index++;
                }
            }
        }
        byte[] carved = new byte[16 * 16 * height];
        int[] carvedCount = {0};
        mask.visit((x, z, bottomY, topY) -> {
            for (int y = bottomY; y <= topY; y++) {
                carved[(x * 16 + z) * height + y - minY] = 1;
                carvedCount[0]++;
            }
        });
        out.println("terrainmask " + prefix + " -> " + carvedCount[0] + " " + HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(carved)));

        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        TreeMap<String, Integer> counts = new TreeMap<>();
        StringBuilder columns = new StringBuilder();
        BlockPos.MutableBlockPos pos = new BlockPos.MutableBlockPos();
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
                    columns.append("terraincolumn ").append(prefix).append(' ').append(x).append(' ').append(z).append(" -> ").append(rle).append('\n');
                }
            }
        }
        out.println("terrain " + prefix + " -> " + HexFormat.of().formatHex(digest.digest()));
        out.print(columns);
        StringBuilder tops = new StringBuilder();
        for (int z = 0; z < 16; z++) {
            for (int x = 0; x < 16; x++) {
                tops.append(tops.length() == 0 ? "" : " ").append(chunk.getHeight(Heightmap.Types.WORLD_SURFACE_WG, x, z));
            }
        }
        out.println("terrainheight " + prefix + " -> " + tops);
        System.err.println(prefix + " " + counts);
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        try (var resources = WorldgenSupport.resources()) {
            WorldgenSupport.bindTags(resources);
            lookup = WorldgenSupport.lookup();
        }
        MaterialGolden.lookup = lookup;
        out = new PrintWriter(args[0], "UTF-8");
        out.println("# Generated by tools/java/TerrainGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        out.println("# Grammar: see the header comment of tools/java/TerrainGolden.java.");
        int[][] overworld = {{0, 0}, {5, -3}, {-7, 10}, {30, 30}, {-20, 4}, {12, -25}};
        for (int[] c : overworld) {
            chunk("overworld", SEEDS[0], c[0], c[1]);
        }
        chunk("overworld", SEEDS[1], 3, 3);
        chunk("overworld", SEEDS[1], -9, 6);
        chunk("nether", SEEDS[0], 0, 0);
        chunk("nether", SEEDS[0], 7, -5);
        chunk("nether", SEEDS[1], -3, 2);
        chunk("end", SEEDS[0], 0, 0);
        chunk("end", SEEDS[0], 9, -4);
        out.close();
    }
}
