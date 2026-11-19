import java.io.PrintWriter;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.Callable;
import net.minecraft.SharedConstants;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.Registries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.Biome;
import net.minecraft.world.level.biome.BiomeManager;
import net.minecraft.world.level.biome.BiomeResolver;
import net.minecraft.world.level.biome.Biomes;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterList;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.chunk.PalettedContainerFactory;
import net.minecraft.world.level.chunk.ProtoChunk;
import net.minecraft.world.level.chunk.Strategy;
import net.minecraft.world.level.chunk.UpgradeData;
import net.minecraft.world.level.chunk.status.ChunkStatus;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.RandomState;

// Records the same terrain/post/mask/height/column grammar as TerrainGolden,
// with the overworld climate source instead of synthetic biome stripes.
// terrainmelt <dim> <seed> <cx> <cz> -> 256 digits, z then x: vanilla's
// shouldMeltFrozenOceanIcebergSlightly at sea level, the only temperature query
// the material system makes. Structures and features are not placed.
public class OverworldTerrainGolden {
    static final long[] SEEDS = {0L, 12345L, -4172144997902289642L};
    static final int[][] CHUNKS = {{0, 0}, {-1, -1}, {3, -4}, {-4, 3}, {6, 1}, {60, 62}, {-70, 5},
        {100, -100}, {-380, -378}, {25, -31}, {-12, 47}, {210, 40}, {1500, -900}, {-2400, 2600}};

    // Each fill owns a fresh climate search; last-result ties follow the
    // section/x/y/z order within that chunk. vanilla: ChunkAccess.fillBiomesFromNoise
    static <T> T freshSearch(Callable<T> task) throws Exception {
        Object[] result = new Object[1];
        Throwable[] failure = new Throwable[1];
        Thread thread = new Thread(() -> {
            try {
                result[0] = task.call();
            } catch (Throwable e) {
                failure[0] = e;
            }
        });
        thread.start();
        thread.join();
        if (failure[0] != null) throw new Exception(failure[0]);
        @SuppressWarnings("unchecked") T value = (T) result[0];
        return value;
    }

    static class NaturalBiomes {
        final MultiNoiseBiomeSource source;
        final RandomState random;
        final BiomeResolver carving;
        final BiomeManager manager;
        final Map<Long, ProtoChunk> chunks = new HashMap<>();
        final PalettedContainerFactory containers;
        final LevelHeightAccessor heights = LevelHeightAccessor.create(-64, 384);

        NaturalBiomes(long seed) throws Exception {
            var registry = TerrainGolden.lookup.lookupOrThrow(Registries.BIOME);
            source = MultiNoiseBiomeSource.createFromPreset(Holder.direct(new MultiNoiseBiomeSourceParameterList(
                MultiNoiseBiomeSourceParameterList.Preset.OVERWORLD, registry)));
            for (var biome : source.possibleBiomes()) {
                var names = new java.util.ArrayList<String>();
                biome.value().getGenerationSettings().getCarvers().forEach(c -> names.add(c.unwrapKey().orElseThrow().identifier().getPath()));
                if (!names.equals(java.util.List.of("cave", "cave_extra_underground", "canyon"))) {
                    throw new IllegalStateException("unmodeled overworld carvers: " + names);
                }
            }
            random = WorldgenSupport.randomState(TerrainGolden.lookup, NoiseGeneratorSettings.OVERWORLD, seed);
            carving = source.createUncachedResolver(random);
            var biomeIds = new net.minecraft.core.IdMapper<Holder<Biome>>();
            registry.listElements().forEach(biomeIds::add);
            containers = new PalettedContainerFactory(Strategy.createForBlockStates(Block.BLOCK_STATE_REGISTRY),
                Blocks.AIR.defaultBlockState(), null, Strategy.createForBiomes(biomeIds), registry.getOrThrow(Biomes.PLAINS), null);
            manager = new BiomeManager((qx, qy, qz) -> chunk(qx >> 2, qz >> 2).getNoiseBiome(qx, qy, qz), BiomeManager.obfuscateSeed(seed));
        }

        ProtoChunk chunk(int cx, int cz) {
            long key = ((long)cx << 32) ^ (cz & 0xffffffffL);
            return chunks.computeIfAbsent(key, ignored -> {
                ProtoChunk chunk = new ProtoChunk(new ChunkPos(cx, cz), UpgradeData.EMPTY, heights, containers, null);
                try {
                    freshSearch(() -> {
                        chunk.fillBiomesFromNoise(source.createResolverForChunk(WorldgenSupport.cachedClimateSampler(random), cx * 4, -16, cz * 4, 4, 96, 4));
                        chunk.setPersistedStatus(ChunkStatus.BIOMES);
                        return null;
                    });
                } catch (Exception e) {
                    throw new RuntimeException(e);
                }
                return chunk;
            });
        }
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        try (var resources = WorldgenSupport.resources()) {
            WorldgenSupport.bindTags(resources);
            TerrainGolden.lookup = WorldgenSupport.lookup();
        }
        TerrainGolden.out = new PrintWriter(args[0], "UTF-8");
        TerrainGolden.out.println("# Generated by tools/java/OverworldTerrainGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        TerrainGolden.out.println("# Grammar: TerrainGolden.java and OverworldTerrainGolden.java; real noise biomes, no structures or features.");
        for (long seed : SEEDS) {
            NaturalBiomes biomes = new NaturalBiomes(seed);
            for (int[] c : CHUNKS) {
                freshSearch(() -> {
                    TerrainGolden.chunk("overworld", seed, c[0], c[1], biomes);
                    return null;
                });
            }
        }
        TerrainGolden.out.close();
    }
}
