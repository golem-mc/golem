import java.io.PrintWriter;
import java.nio.file.Path;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.core.RegistryAccess;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.datafix.DataFixers;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.LevelHeightAccessor;
import net.minecraft.world.level.biome.MultiNoiseBiomeSource;
import net.minecraft.world.level.biome.MultiNoiseBiomeSourceParameterLists;
import net.minecraft.world.level.levelgen.NoiseBasedChunkGenerator;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager;
import net.minecraft.world.level.storage.LevelStorageSource;

// Calls the actual structures at fixed chunks, bypassing their biome filter to
// exercise every setup over the same terrain and both seeds.
public class RuinedPortalGolden {
    static <T> void bindTags(net.minecraft.server.packs.resources.ResourceManager resources, net.minecraft.core.Registry<T> registry, ResourceKey<? extends net.minecraft.core.Registry<T>> key) {
        @SuppressWarnings({"unchecked", "rawtypes"})
        var tags = (java.util.Map<net.minecraft.tags.TagKey<T>, List<net.minecraft.core.Holder<T>>>) net.minecraft.tags.TagLoader.loadTagsForRegistry(resources, (ResourceKey) key, (net.minecraft.tags.TagLoader.ElementLookup) net.minecraft.tags.TagLoader.ElementLookup.fromFrozenRegistry(registry));
        registry.prepareTagReload(new net.minecraft.tags.TagLoader.LoadResult<>(key, tags)).apply();
    }
    static void age(PrintWriter out) throws Exception {
        var blocks = new net.minecraft.world.level.block.Block[]{
            net.minecraft.world.level.block.Blocks.STONE, net.minecraft.world.level.block.Blocks.STONE_BRICKS,
            net.minecraft.world.level.block.Blocks.CHISELED_STONE_BRICKS, net.minecraft.world.level.block.Blocks.STONE_BRICK_STAIRS,
            net.minecraft.world.level.block.Blocks.MOSSY_STONE_BRICK_STAIRS, net.minecraft.world.level.block.Blocks.STONE_BRICK_SLAB,
            net.minecraft.world.level.block.Blocks.STONE_BRICK_WALL, net.minecraft.world.level.block.Blocks.OBSIDIAN};
        for (float mossiness : new float[]{0.0f, 0.2f, 0.5f, 0.8f}) {
            var processor = new net.minecraft.world.level.levelgen.structure.templatesystem.BlockAgeProcessor(mossiness);
            var settings = new net.minecraft.world.level.levelgen.structure.templatesystem.StructurePlaceSettings();
            var bytes = new java.io.ByteArrayOutputStream();
            var data = new java.io.DataOutputStream(bytes);
            for (int i = 0; i < 256; i++) {
                var pos = new net.minecraft.core.BlockPos(i-128,64+i%32,37-i*2);
                for (var block : blocks) {
                    var info = new net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplate.StructureBlockInfo(pos,block.defaultBlockState(),null);
                    var result = processor.processBlock(null,pos,pos,pos,info,settings);
                    data.writeInt(net.minecraft.world.level.block.Block.getId(result.state()));
                }
            }
            out.printf("age %s %s%n",mossiness,java.util.HexFormat.of().formatHex(java.security.MessageDigest.getInstance("SHA-256").digest(bytes.toByteArray())));
        }
    }
    static void growth(PrintWriter out, Object piece) throws Exception {
        var cls = net.minecraft.world.level.levelgen.structure.structures.RuinedPortalPiece.class;
        var vines = cls.getDeclaredMethod("maybeAddVines", net.minecraft.util.RandomSource.class, net.minecraft.world.level.LevelAccessor.class, net.minecraft.core.BlockPos.class);
        var leaves = cls.getDeclaredMethod("maybeAddLeavesAbove", net.minecraft.util.RandomSource.class, net.minecraft.world.level.LevelAccessor.class, net.minecraft.core.BlockPos.class);
        vines.setAccessible(true); leaves.setAccessible(true);
        var blocks = new net.minecraft.world.level.block.Block[]{net.minecraft.world.level.block.Blocks.NETHERRACK, net.minecraft.world.level.block.Blocks.STONE,
            net.minecraft.world.level.block.Blocks.STONE_BRICK_STAIRS, net.minecraft.world.level.block.Blocks.STONE_SLAB,
            net.minecraft.world.level.block.Blocks.JUNGLE_LEAVES, net.minecraft.world.level.block.Blocks.OBSIDIAN};
        for (long seed : new long[]{0L,12345L}) {
            var cells = new java.util.HashMap<net.minecraft.core.BlockPos,net.minecraft.world.level.block.state.BlockState>();
            for (int x=-1;x<=1;x++) for (int z=-1;z<=1;z++) cells.put(new net.minecraft.core.BlockPos(x,200,z),blocks[((x+1)*3+z+1)%blocks.length].defaultBlockState());
            var level = (net.minecraft.world.level.LevelAccessor) java.lang.reflect.Proxy.newProxyInstance(cls.getClassLoader(),new Class[]{net.minecraft.world.level.LevelAccessor.class},(proxy,method,args) -> {
                if (method.getName().equals("getBlockState")) return cells.getOrDefault(args[0],net.minecraft.world.level.block.Blocks.AIR.defaultBlockState());
                if ((method.getName().equals("setBlock") || method.getName().equals("setBlockAndUpdate"))) {cells.put(((net.minecraft.core.BlockPos)args[0]).immutable(),(net.minecraft.world.level.block.state.BlockState)args[1]);return true;}
                throw new UnsupportedOperationException(method.toString());
            });
            var rng = new net.minecraft.world.level.levelgen.WorldgenRandom(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(seed));
            for (int y=200;y<=201;y++) for (int z=-2;z<=2;z++) for (int x=-2;x<=2;x++) {
                var pos = new net.minecraft.core.BlockPos(x,y,z);
                vines.invoke(piece,rng,level,pos); leaves.invoke(piece,rng,level,pos);
            }
            var bytes = new java.io.ByteArrayOutputStream(); var data = new java.io.DataOutputStream(bytes);
            for (int x=-4;x<=4;x++) for (int y=200;y<=202;y++) for (int z=-4;z<=4;z++) {
                var state=cells.get(new net.minecraft.core.BlockPos(x,y,z)); if (state==null || state.isAir()) continue;
                data.writeInt(x);data.writeInt(y);data.writeInt(z);data.writeInt(net.minecraft.world.level.block.Block.getId(state));
            }
            data.writeInt(rng.nextInt());
            out.printf("growth %d %s%n",seed,java.util.HexFormat.of().formatHex(java.security.MessageDigest.getInstance("SHA-256").digest(bytes.toByteArray())));
        }
    }
    public static void main(String[] args) throws Exception {
        try { generate(args); } catch (Throwable e) {
            e.printStackTrace(new java.io.PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.err)));
            System.exit(1);
        }
    }
    static void generate(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var lookup = WorldgenSupport.lookup();
        var access = RegistryAccess.fromRegistryOfRegistries(BuiltInRegistries.REGISTRY);
        var biomes = MultiNoiseBiomeSource.createFromPreset(lookup.lookupOrThrow(Registries.MULTI_NOISE_BIOME_SOURCE_PARAMETER_LIST)
            .getOrThrow(MultiNoiseBiomeSourceParameterLists.OVERWORLD));
        var generator = new NoiseBasedChunkGenerator(biomes, lookup.lookupOrThrow(Registries.NOISE_SETTINGS)
            .getOrThrow(NoiseGeneratorSettings.OVERWORLD));
        try (var resources = WorldgenSupport.resources();
             var storage = LevelStorageSource.createDefault(Path.of("portal-golden-worlds")).createAccess("test");
             var out = new PrintWriter(args[0], "UTF-8")) {
            bindTags(resources, BuiltInRegistries.BLOCK, Registries.BLOCK);
            var manager = new StructureTemplateManager(resources, storage, DataFixers.getDataFixer(), BuiltInRegistries.BLOCK);
            var serialization = new StructurePieceSerializationContext(resources, access, manager);
            Object sample = null;
            out.println("# Generated by tools/java/RuinedPortalGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
            for (long seed : new long[]{0L, 12345L}) {
                var random = WorldgenSupport.randomState(lookup, NoiseGeneratorSettings.OVERWORLD, seed);
                for (String id : new String[]{"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_mountain", "ruined_portal_ocean", "ruined_portal_swamp"}) {
                    var structure = (net.minecraft.world.level.levelgen.structure.structures.RuinedPortalStructure) lookup.lookupOrThrow(Registries.STRUCTURE).getOrThrow(ResourceKey.create(Registries.STRUCTURE, Identifier.withDefaultNamespace(id))).value();
                    for (int[] chunk : new int[][]{{-20,-20}, {0,0}, {12,18}, {80,-30}}) {
                        var context = WorldgenSupport.structureContext(access, generator, biomes, random, manager, seed,
                            new ChunkPos(chunk[0],chunk[1]), LevelHeightAccessor.create(-64,384), b -> true);
                        var piece = structure.findGenerationPoint(context).orElseThrow().getPiecesBuilder().build().pieces().getFirst();
                        if (sample == null) sample = piece;
                        var tag = piece.createTag(serialization);
                        out.printf("%d %d %d minecraft:%s %s%n", seed, chunk[0], chunk[1], id, tag);
                    }
                }
            }
            age(out);
            growth(out,sample);
        }
    }
}
