import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.Registry;
import net.minecraft.core.RegistryAccess;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.RegistryDataLoader;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.packs.PackResources;
import net.minecraft.server.packs.PackType;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.server.packs.resources.MultiPackResourceManager;
import net.minecraft.server.packs.resources.ResourceManager;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.chunk.ChunkGenerator;
import net.minecraft.world.level.levelgen.placement.PlacedFeature;

// Shared setup for the worldgen helpers: registries, tags, random state and structure context.
class WorldgenSupport {
    // Registry ids change independently of the algorithms these fixtures check.
    private static final java.util.Map<net.minecraft.world.level.block.state.BlockState, byte[]> stateBytes = new java.util.IdentityHashMap<>();

    static byte[] stateBytes(net.minecraft.world.level.block.state.BlockState state) {
        return stateBytes.computeIfAbsent(state, s -> {
            var props = new java.util.TreeMap<String, String>();
            for (var property : s.getProperties()) props.put(property.getName(), valueName(s, property));
            var text = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).toString());
            if (!props.isEmpty()) {
                text.append('[');
                text.append(String.join(",", props.entrySet().stream().map(e -> e.getKey() + "=" + e.getValue()).toList()));
                text.append(']');
            }
            return text.append('\n').toString().getBytes(java.nio.charset.StandardCharsets.UTF_8);
        });
    }

    private static <T extends Comparable<T>> String valueName(net.minecraft.world.level.block.state.BlockState state,
            net.minecraft.world.level.block.state.properties.Property<T> property) {
        return property.getName(state.getValue(property));
    }

    static HolderLookup.Provider lookup() throws Exception {
        return VanillaRegistries.createWorldLookup();
    }

    static MultiPackResourceManager resources() throws Exception {
        // vanilla: VanillaPackResources.fullResources
        PackResources pack = ServerPacksSource.createVanillaPackSource().fullResources();
        return new MultiPackResourceManager(PackType.SERVER_DATA, List.of(pack));
    }

    static void bindTags(ResourceManager resources) {
        net.minecraft.tags.TagLoader.loadTagsForExistingRegistries(resources,
            RegistryAccess.fromRegistryOfRegistries(BuiltInRegistries.REGISTRY)).forEach(Registry.PendingTags::apply);
    }

    static HolderLookup.Provider loadWorld(ResourceManager resources) throws Exception {
        var context = new ArrayList<HolderLookup.RegistryLookup<?>>();
        RegistryAccess.fromRegistryOfRegistries(BuiltInRegistries.REGISTRY).registries().forEach(e -> context.add(e.value()));
        return RegistryDataLoader.load(resources, context, RegistryDataLoader.WORLD_REGISTRIES, Runnable::run).join();
    }

    // vanilla: RandomState.create
    static net.minecraft.world.level.levelgen.RandomState randomState(HolderLookup.Provider lookup,
            ResourceKey<net.minecraft.world.level.levelgen.NoiseGeneratorSettings> key, long seed) {
        return net.minecraft.world.level.levelgen.RandomState.create(lookup.lookupOrThrow(Registries.NOISE), seed,
            lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(key).value());
    }

    // Uncached contexts can be sampled by the biome helper's independent threads.
    // vanilla: SamplerContext.EMPTY_UNCACHED, RandomState.createClimateSampler.
    static net.minecraft.world.level.biome.Climate.Sampler climateSampler(net.minecraft.world.level.levelgen.RandomState random) throws Exception {
        var context = Class.forName("net.minecraft.world.level.levelgen.densityfunction.SamplerContext");
        return (net.minecraft.world.level.biome.Climate.Sampler) random.getClass().getMethod("createClimateSampler", context)
            .invoke(random, context.getField("EMPTY_UNCACHED").get(null));
    }

    // vanilla: ChunkGenerator.doCreateBiomes (26.3).
    static net.minecraft.world.level.biome.Climate.Sampler cachedClimateSampler(net.minecraft.world.level.levelgen.RandomState random) throws Exception {
        var context = Class.forName("net.minecraft.world.level.levelgen.densityfunction.SamplerContext");
        Object builder = context.getMethod("builder").invoke(null);
        builder.getClass().getMethod("enableCaches").invoke(builder);
        Object caches = builder.getClass().getMethod("build").invoke(builder);
        return (net.minecraft.world.level.biome.Climate.Sampler) random.getClass().getMethod("createClimateSampler", context).invoke(random, caches);
    }

    static net.minecraft.world.level.levelgen.structure.Structure.GenerationContext structureContext(
            RegistryAccess access, ChunkGenerator generator, net.minecraft.world.level.biome.BiomeSource biomes,
            net.minecraft.world.level.levelgen.RandomState random,
            net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager templates, long seed,
            net.minecraft.world.level.ChunkPos chunk, net.minecraft.world.level.LevelHeightAccessor height,
            java.util.function.Predicate<net.minecraft.core.Holder<net.minecraft.world.level.biome.Biome>> validBiome) throws Exception {
        Class<?>[] signature = {RegistryAccess.class, ChunkGenerator.class, net.minecraft.world.level.biome.BiomeSource.class,
            net.minecraft.world.level.biome.Climate.Sampler.class, net.minecraft.world.level.levelgen.RandomState.class,
            net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager.class, long.class,
            net.minecraft.world.level.ChunkPos.class, net.minecraft.world.level.LevelHeightAccessor.class, java.util.function.Predicate.class};
        return net.minecraft.world.level.levelgen.structure.Structure.GenerationContext.class.getConstructor(signature)
            .newInstance(access, generator, biomes, climateSampler(random), random, templates, seed, chunk, height, validBiome);
    }

    static boolean placeWithBiomeCheck(PlacedFeature feature, WorldGenLevel level, ChunkGenerator generator,
                                       RandomSource random, BlockPos pos) throws Exception {
        var placer = Class.forName("net.minecraft.world.level.levelgen.placement.FeaturePlacer");
        Object instance = placer.getConstructor(WorldGenLevel.class, ChunkGenerator.class).newInstance(level, generator);
        return (Boolean) placer.getMethod("placeWithBiomeCheck", PlacedFeature.class, RandomSource.class, BlockPos.class)
            .invoke(instance, feature, random, pos);
    }

    @SuppressWarnings("unchecked")
    static ResourceKey<Registry<Object>> featureRegistry() throws Exception {
        return (ResourceKey<Registry<Object>>) (ResourceKey<?>) Registries.FEATURE;
    }

    static boolean place(Object feature, WorldGenLevel level, ChunkGenerator generator, RandomSource random, BlockPos pos) throws Exception {
        return (Boolean) feature.getClass().getMethod("place", WorldGenLevel.class, ChunkGenerator.class, RandomSource.class, BlockPos.class)
            .invoke(feature, level, generator, random, pos);
    }

    // The template manager is reached through getLevel().getServer(); only that
    // access chain is allocated, without starting a server or its threads.
    static net.minecraft.server.level.ServerLevel templateLevel(ResourceManager resources,
                                 net.minecraft.world.level.storage.LevelStorageSource.LevelStorageAccess storage) throws Exception {
        var f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        var unsafe = (sun.misc.Unsafe) f.get(null);
        var server = (net.minecraft.server.MinecraftServer) unsafe.allocateInstance(net.minecraft.server.dedicated.DedicatedServer.class);
        var manager = new net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager(
            resources, storage, net.minecraft.util.datafix.DataFixers.getDataFixer(), net.minecraft.core.registries.BuiltInRegistries.BLOCK);
        var managerField = net.minecraft.server.MinecraftServer.class.getDeclaredField("structureTemplateManager");
        managerField.setAccessible(true);
        managerField.set(server, manager);
        var templateLevel = (net.minecraft.server.level.ServerLevel) unsafe.allocateInstance(net.minecraft.server.level.ServerLevel.class);
        var serverField = net.minecraft.server.level.ServerLevel.class.getDeclaredField("server");
        serverField.setAccessible(true);
        serverField.set(templateLevel, server);
        return templateLevel;
    }

    static String version() { return SharedConstants.getCurrentVersion().name(); }
}
