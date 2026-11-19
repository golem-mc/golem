import java.io.PrintWriter;
import java.util.ArrayList;
import java.util.List;
import java.util.Random;
import net.minecraft.SharedConstants;
import net.minecraft.core.HolderGetter;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.context.ContextMap;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.dimension.DimensionType;
import net.minecraft.world.level.levelgen.Aquifer;
import net.minecraft.world.level.levelgen.Beardifier;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.NoiseChunk;
import net.minecraft.world.level.levelgen.NoiseGeneratorSettings;
import net.minecraft.world.level.levelgen.NoiseRouter;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.blending.Blender;
import net.minecraft.world.level.levelgen.densityfunction.DensityBuffer;
import net.minecraft.world.level.levelgen.densityfunction.DensityFunction;
import net.minecraft.world.level.levelgen.densityfunction.DensitySampler;
import net.minecraft.world.level.levelgen.densityfunction.DensityVolume;
import net.minecraft.world.level.levelgen.densityfunction.SamplerContext;
import net.minecraft.world.level.levelgen.densityfunction.ScopedDensityBuffer;
import net.minecraft.world.level.levelgen.synth.BlendedNoise;
import net.minecraft.world.level.levelgen.synth.NormalNoise;
import net.minecraft.world.level.levelgen.synth.Noise;
import net.minecraft.world.level.levelgen.synth.NoiseStack;
import net.minecraft.world.level.levelgen.synth.PerlinNoise;
import net.minecraft.world.level.levelgen.synth.SimplexNoise;
import net.minecraft.world.level.levelgen.synth.SmearedPerlinNoise;

// Writes the 26.3 terrain-engine golden values internal/server/world/terrain's tests
// compare against, computed by the vanilla classes on the server jar (mise run
// gen:sampler-golden). Every float is Float.floatToRawIntBits as 8 hex digits
// and every double output is Double.doubleToRawLongBits as 16 hex digits;
// double inputs are Double.toString text (Go's ParseFloat reads it back
// exactly). Fields are space separated, results follow " -> ".
//
// perlin <seed> <x> <y> <z> -> f          PerlinNoise(XoroshiroRandomSource(seed)).get(x, y, z)
// perlin2d <seed> <x> <y> -> f            ...get(x, y)
// smeared <seed> <fudgeYScale> <x> <y> <z> -> f   SmearedPerlinNoise.get
// simplex2 <seed> <x> <y> -> f            SimplexNoise(XoroshiroRandomSource(seed)).get(x, y)
// simplex3 <seed> <x> <y> <z> -> f        ...get(x, y, z)
// normal <seed> <noise> <x> <y> <z> -> f  Noises.instantiate on RandomState's positional factory
//                                         (XoroshiroRandomSource(seed).forkPositional()); a NoiseStack
// normallegacy <seed> <noise> <x> <y> <z> -> f   NormalNoise.createForLegacyNetherBiome(LegacyRandomSource(seed))
// stack <seed> perlin|mixed <x> <y> <z> -> f     hand-built NoiseStack: perlin = three PerlinNoise layers
//                                         (the Perlin subclass), mixed = Perlin + SmearedPerlin (the base class)
// blended <seed> overworld|nether|end <x> <y> <z> -> f   BlendedNoise.compileSampler(XoroshiroRandomSource(seed)).sampleValue
//
// volume <kind> <seed> <sx> <sy> <sz> <minX> <minY> <minZ> <stepX> <stepY> <stepZ> <xzScale> <yScale> <amp> -> f...
//   Noise.addToVolume into a zeroed buffer, values in DensityVolume.indexUnchecked order (z, x, y innermost);
//   kind is perlin | smeared8 (fudge 8.0) | stackperlin | stackmixed.
// volumepoint <same fields> -> f...
//   the same nodes through Noise.get: amp * get(blockX * xzScale, blockY * yScale, blockZ * xzScale)
// blendedvolume <seed> <dim> <sx> <sy> <sz> <minX> <minY> <minZ> <stepX> <stepY> <stepZ> -> f...
//   BlendedNoise sampler's sampleVolume (EMPTY_UNCACHED context)
//
// router <dim> <seed> <field> <x> <y> <z> -> f
//   RandomState.create(settings) router field sampleValue at a block, each call in a fresh caching
//   SamplerContext carrying the empty Beardifier. dim is overworld | nether | end | large_biomes | amplified;
//   field is temperature | vegetation | continents | erosion | depth | ridges | chunkSurfaceLevel | finalDensity.
// endislands <seed> <x> <z> -> f         End erosion at a block.
// endbiome <seed> <quartX> <quartY> <quartZ> -> name   TheEndBiomeSource resolver.
// aquiferfn <dim> <seed> <field> <x> <y> <z> -> f
//   the overworld Aquifer.Config functions (barrier | floodedness | spread | lava | exclusion | surfaceLevel),
//   sampled like router. Only overworld-like dims.
//
// fill <dim> <seed> <chunkX> <chunkZ> <sx> <sy> <sz> <minX> <minY> <minZ> <fnv64> -> n
//   finalDensity.sampleVolume over NoiseBasedChunkGenerator.chunkVolume (16 x height x 16 at the chunk's
//   min corner and noise minY), through NoiseChunk.cachingSamplers. fnv64 is the FNV-1a 64 hash (hex) of the
//   float bit patterns, each as 4 big-endian bytes, in index order; n is the value count.
// fillcol <dim> <seed> <chunkX> <chunkZ> <x> <z> -> f...
//   that volume's column at the chunk-relative x, z for y = minY upward.
// aquifer <dim> <seed> <chunkX> <chunkZ> <x> <z> -> run,run,...
//   NoiseBasedChunkGenerator.doFill's order (z, x, y downward) calls NoiseChunk.aquifer().computeSubstance(
//   blockX, blockY, blockZ, density) with the density from that volume, over the whole chunk; the columns
//   at chunk-relative x, z in {0, 5, 10, 15} are printed as runs `block:topY:bottomY` ("null" = the settings'
//   default block would be placed), top first. The aquifer's global fluid picker is
//   NoiseBasedChunkGenerator.createFluidPicker: lava below min(-54, seaLevel), else the sea.
// aquiferflag <dim> <seed> <chunkX> <chunkZ> -> 0|1
//   Aquifer.shouldScheduleFluidUpdate after that pass.
// Chunks: overworld seed 12345 (0,0) (3,-4) (-12,47) (10,10) (-30,20), the second seed (2,2); nether (0,0) (5,-3); end (0,0) (70,5) (-8,-9).
// The material system is not involved: only the density and the aquifer.
public class SamplerGolden {
    static final long[] SEEDS = {12345L, -4172144997902289642L};
    static PrintWriter out;
    static HolderLookup.Provider lookup;

    static String f(float v) {
        return String.format("%08x", Float.floatToRawIntBits(v));
    }

    static String bits(float[] v) {
        StringBuilder sb = new StringBuilder();
        for (float x : v) {
            sb.append(' ').append(f(x));
        }
        return sb.toString();
    }

    static List<double[]> points3() {
        List<double[]> p = new ArrayList<>();
        p.add(new double[] {0, 0, 0});
        p.add(new double[] {0.5, 0.5, 0.5});
        p.add(new double[] {16777216.5, 3.3, -16777216.25});
        p.add(new double[] {3.3554432E7 + 0.37, -5.0E7, 1.0E8 - 0.5});
        p.add(new double[] {-3.3554432E7 - 12.75, 1.6777215E7, 16777215.9});
        p.add(new double[] {-0.0000001, 255.99999, -1.0E-9});
        p.add(new double[] {1.0, -64.0, 7.0});
        Random rng = new Random(3102);
        for (int i = 0; i < 24; i++) {
            double s = i < 12 ? 40.0 : 3000.0;
            p.add(new double[] {(rng.nextDouble() - 0.5) * s, (rng.nextDouble() - 0.5) * s, (rng.nextDouble() - 0.5) * s});
        }
        return p;
    }

    static ResourceKey<NormalNoise> noiseKey(String name) {
        return ResourceKey.create(Registries.NOISE, Identifier.withDefaultNamespace(name));
    }

    @SuppressWarnings("unchecked")
    static HolderGetter<NormalNoise> noises() {
        return (HolderGetter<NormalNoise>) (HolderGetter<?>) lookup.lookupOrThrow(Registries.NOISE);
    }

    static Noise stack(long seed, String kind) {
        var r = new XoroshiroRandomSource(seed);
        var b = NoiseStack.builder();
        if (kind.equals("perlin")) {
            b.add(new PerlinNoise(r), 1.0, 1.0f).add(new PerlinNoise(r), 0.5, 0.25f).add(new PerlinNoise(r), 4.2, 0.3f);
        } else {
            b.add(new PerlinNoise(r), 1.0, 1.0f).add(new SmearedPerlinNoise(r, 8.0), 0.5, 0.25f);
        }
        return b.build();
    }

    static Noise volumeNoise(String kind, long seed) {
        var r = new XoroshiroRandomSource(seed);
        return switch (kind) {
            case "perlin" -> new PerlinNoise(r);
            case "smeared8" -> new SmearedPerlinNoise(r, 8.0);
            case "stackperlin" -> stack(seed, "perlin");
            default -> stack(seed, "mixed");
        };
    }

    static void noiseRecords() throws Exception {
        List<double[]> pts = points3();
        for (long seed : new long[] {0L, 12345L}) {
            var perlin = new PerlinNoise(new XoroshiroRandomSource(seed));
            var simplex = new SimplexNoise(new XoroshiroRandomSource(seed));
            for (double[] p : pts) {
                String in = p[0] + " " + p[1] + " " + p[2];
                out.println("perlin " + seed + " " + in + " -> " + f(perlin.get(p[0], p[1], p[2])));
                out.println("perlin2d " + seed + " " + p[0] + " " + p[1] + " -> " + f(perlin.get(p[0], p[1])));
                out.println("simplex2 " + seed + " " + p[0] + " " + p[1] + " -> " + f(simplex.get(p[0], p[1])));
                out.println("simplex3 " + seed + " " + in + " -> " + f(simplex.get(p[0], p[1], p[2])));
                for (double fudge : new double[] {0.8, 8.0}) {
                    var smeared = new SmearedPerlinNoise(new XoroshiroRandomSource(seed), fudge);
                    out.println("smeared " + seed + " " + fudge + " " + in + " -> " + f(smeared.get(p[0], p[1], p[2])));
                }
            }
        }
        for (long seed : new long[] {1L, 12345L}) {
            for (String kind : new String[] {"perlin", "mixed"}) {
                Noise s = stack(seed, kind);
                for (double[] p : pts) {
                    out.println("stack " + seed + " " + kind + " " + p[0] + " " + p[1] + " " + p[2] + " -> " + f(s.get(p[0], p[1], p[2])));
                }
            }
        }
        String[] names = {"temperature", "continentalness", "erosion", "vegetation", "ridge", "jagged", "cave_layer", "offset", "aquifer_barrier", "spaghetti_3d_1", "ore_gap"};
        for (long seed : new long[] {12345L, -4172144997902289642L}) {
            var factory = new XoroshiroRandomSource(seed).forkPositional();
            for (String name : names) {
                Noise n = net.minecraft.world.level.levelgen.Noises.instantiate(noises(), factory, noiseKey(name));
                for (double[] p : pts) {
                    out.println("normal " + seed + " " + name + " " + p[0] + " " + p[1] + " " + p[2] + " -> " + f(n.get(p[0], p[1], p[2])));
                }
            }
        }
        for (long seed : new long[] {12345L, 0L}) {
            for (String name : new String[] {"nether/temperature", "nether/vegetation"}) {
                NormalNoise params = noises().getOrThrow(noiseKey(name)).value();
                Noise n = params.createForLegacyNetherBiome(new LegacyRandomSource(seed));
                for (double[] p : pts) {
                    out.println("normallegacy " + seed + " " + name + " " + p[0] + " " + p[1] + " " + p[2] + " -> " + f(n.get(p[0], p[1], p[2])));
                }
            }
        }
    }

    static BlendedNoise blendedParams(String dim) {
        return switch (dim) {
            case "overworld" -> new BlendedNoise(0.25, 0.125, 80.0, 160.0, 8.0);
            case "nether" -> new BlendedNoise(0.25, 0.375, 80.0, 60.0, 8.0);
            default -> new BlendedNoise(0.25, 0.25, 80.0, 160.0, 4.0);
        };
    }

    static void blendedRecords() {
        Random rng = new Random(77);
        for (long seed : new long[] {12345L, 0L}) {
            for (String dim : new String[] {"overworld", "nether", "end"}) {
                DensitySampler s = blendedParams(dim).compileSampler(new XoroshiroRandomSource(seed));
                for (int i = 0; i < 24; i++) {
                    int x = rng.nextInt(2000) - 1000;
                    int y = rng.nextInt(384) - 64;
                    int z = rng.nextInt(2000) - 1000;
                    if (i == 0) {
                        x = 0;
                        y = 0;
                        z = 0;
                    }
                    out.println("blended " + seed + " " + dim + " " + x + " " + y + " " + z + " -> " + f(s.sampleValue(SamplerContext.EMPTY_UNCACHED, x, y, z)));
                }
                DensityVolume v = new DensityVolume(3, 6, 3, -20, -64, 36, 4, 8, 4);
                DensityBuffer buf = DensityBuffer.createUnpooled(v.size());
                s.sampleVolume(SamplerContext.EMPTY_UNCACHED, buf, v);
                float[] vals = new float[v.size()];
                for (int i = 0; i < vals.length; i++) {
                    vals[i] = buf.get(i);
                }
                out.println("blendedvolume " + seed + " " + dim + " 3 6 3 -20 -64 36 4 8 4 ->" + bits(vals));
            }
        }
    }

    static void volumeRecords() {
        int[][] volumes = {{3, 5, 3, -13, -64, 29, 4, 8, 4}, {2, 9, 4, 1000, 60, -77, 1, 1, 1}};
        double[][] scales = {{0.0371, 0.0413, 1.0f}, {1.7, 0.9, 0.75f}, {684.412 * 0.25, 684.412 * 0.125, 1.0f}};
        for (String kind : new String[] {"perlin", "smeared8", "stackperlin", "stackmixed"}) {
            for (long seed : new long[] {12345L}) {
                for (int[] v : volumes) {
                    for (double[] sc : scales) {
                        DensityVolume vol = new DensityVolume(v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8]);
                        float amp = (float) sc[2];
                        Noise n = volumeNoise(kind, seed);
                        DensityBuffer buf = DensityBuffer.createUnpooled(vol.size());
                        buf.fill(0.0f);
                        n.addToVolume(buf, vol, sc[0], sc[1], amp);
                        float[] a = new float[vol.size()];
                        float[] b = new float[vol.size()];
                        Noise p = volumeNoise(kind, seed);
                        int i = 0;
                        for (int z = 0; z < vol.sizeZ(); z++) {
                            for (int x = 0; x < vol.sizeX(); x++) {
                                for (int y = 0; y < vol.sizeY(); y++) {
                                    a[i] = buf.get(i);
                                    b[i] = amp * p.get(vol.blockX(x) * sc[0], vol.blockY(y) * sc[1], vol.blockZ(z) * sc[0]);
                                    i++;
                                }
                            }
                        }
                        String head = kind + " " + seed + " " + v[0] + " " + v[1] + " " + v[2] + " " + v[3] + " " + v[4] + " " + v[5] + " " + v[6] + " " + v[7] + " " + v[8]
                            + " " + sc[0] + " " + sc[1] + " " + sc[2];
                        out.println("volume " + head + " ->" + bits(a));
                        out.println("volumepoint " + head + " ->" + bits(b));
                    }
                }
            }
        }
    }

    static NoiseGeneratorSettings settings(String dim) {
        var key = switch (dim) {
            case "overworld" -> NoiseGeneratorSettings.OVERWORLD;
            case "nether" -> NoiseGeneratorSettings.NETHER;
            case "end" -> NoiseGeneratorSettings.END;
            case "large_biomes" -> NoiseGeneratorSettings.LARGE_BIOMES;
            default -> NoiseGeneratorSettings.AMPLIFIED;
        };
        return lookup.lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(key).value();
    }

    static SamplerContext freshContext() {
        return SamplerContext.builder().setUserFields(ContextMap.builder().set(Beardifier.CONTEXT_KEY, Beardifier.EMPTY).build()).enableCaches().build();
    }

    static float at(RandomState state, DensityFunction fn, int x, int y, int z) {
        return state.getSampler(fn).sampleValue(freshContext(), x, y, z);
    }

    static void routerRecords() throws Exception {
        int[][] columns = {{0, 0}, {37, -91}, {-1234, 567}, {4096, -8192}, {100003, 250007}};
        for (String dim : new String[] {"overworld", "nether", "end", "large_biomes", "amplified"}) {
            int[] ys = switch (dim) {
                case "nether" -> new int[] {0, 5, 31, 64, 100, 127};
                case "end" -> new int[] {0, 16, 40, 64, 70, 100, 127};
                default -> new int[] {-64, -33, 0, 17, 63, 64, 95, 128, 200, 319};
            };
            boolean wide = dim.equals("overworld") || dim.equals("nether") || dim.equals("end");
            for (long seed : wide ? SEEDS : new long[] {12345L}) {
                NoiseGeneratorSettings settings = settings(dim);
                RandomState state = RandomState.create(noises(), seed, settings);
                NoiseRouter r = settings.noiseRouter();
                String[] fields = {"temperature", "vegetation", "continents", "erosion", "depth", "ridges", "chunkSurfaceLevel", "finalDensity"};
                DensityFunction[] fns = {r.temperature(), r.vegetation(), r.continents(), r.erosion(), r.depth(), r.ridges(), r.chunkSurfaceLevel(), r.finalDensity()};
                for (int fi = 0; fi < fields.length; fi++) {
                    for (int[] c : columns) {
                        for (int y : ys) {
                            out.println("router " + dim + " " + seed + " " + fields[fi] + " " + c[0] + " " + y + " " + c[1] + " -> " + f(at(state, fns[fi], c[0], y, c[1])));
                        }
                    }
                }
                if (settings.aquifers().isPresent()) {
                    Aquifer.Config a = settings.aquifers().get();
                    String[] names = {"barrier", "floodedness", "spread", "lava", "exclusion", "surfaceLevel"};
                    DensityFunction[] afn = {a.barrierNoise(), a.fluidLevelFloodednessNoise(), a.fluidLevelSpreadNoise(), a.lavaNoise(), a.exclusion(), a.surfaceLevel()};
                    for (int fi = 0; fi < names.length; fi++) {
                        for (int[] c : columns) {
                            for (int y : ys) {
                                out.println("aquiferfn " + dim + " " + seed + " " + names[fi] + " " + c[0] + " " + y + " " + c[1] + " -> " + f(at(state, afn[fi], c[0], y, c[1])));
                            }
                        }
                    }
                }
            }
        }
    }

    // Island points and biome ranges retain the retired End engine's coverage.
    static void endRecords() throws Exception {
        var settings = settings("end");
        var source = net.minecraft.world.level.biome.TheEndBiomeSource.create(lookup.lookupOrThrow(Registries.BIOME));
        int[][] points = {{0, 0}, {7, -9}, {-8, -8}, {100, 0}, {-1000, 37}, {1024, 1024}, {8203, -501}, {-6080, -6048}, {80008, 984}, {160000, 1600}, {24000000, 40}, {800, 0}, {-800, 0}, {1031, -1031}};
        int[][] chunks = {{0, 0}, {-1, -1}, {3, -4}, {-4, 3}, {6, 1}, {60, 62}, {-70, 5}, {100, -100}, {-380, -378}};
        for (long seed : new long[] {0L, 12345L, -4172144997902289642L}) {
            var state = RandomState.create(noises(), seed, settings);
            for (int[] p : points) {
                out.println("endislands " + seed + " " + p[0] + " " + p[1] + " -> " + f(at(state, settings.noiseRouter().erosion(), p[0], 64, p[1])));
            }
            var resolver = source.createResolver(state.createClimateSampler(SamplerContext.EMPTY_UNCACHED));
            for (int cx = -200; cx <= 200; cx += 23) {
                for (int cz = -200; cz <= 200; cz += 29) {
                    int qx = cx * 4 + 1, qz = cz * 4 + 2;
                    out.println("endbiome " + seed + " " + qx + " 16 " + qz + " -> " + resolver.getNoiseBiome(qx, 16, qz).getRegisteredName());
                }
            }
            for (int[] c : chunks) {
                if (seed == 12345L && c[0] == 0 && c[1] == 0) continue;
                fillRecord("end", seed, c[0], c[1]);
            }
        }
    }

    static Aquifer.FluidPicker fluidPicker(NoiseGeneratorSettings settings) {
        Aquifer.FluidStatus lava = new Aquifer.FluidStatus(-54, Blocks.LAVA.defaultBlockState());
        int seaLevel = settings.seaLevel();
        Aquifer.FluidStatus sea = new Aquifer.FluidStatus(seaLevel, settings.defaultFluid());
        return (x, y, z) -> y < Math.min(-54, seaLevel) ? lava : sea;
    }

    static String blockName(BlockState s) {
        return s == null ? "null" : BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath();
    }

    static void fillRecord(String dim, long seed, int cx, int cz) {
        NoiseGeneratorSettings settings = settings(dim);
        RandomState state = RandomState.create(noises(), seed, settings);
        var ns = settings.noiseSettings();
        DensityVolume volume = new DensityVolume(16, ns.height(), 16, cx * 16, ns.minY(), cz * 16);
        String chunk = dim + " " + seed + " " + cx + " " + cz;
        try (NoiseChunk nc = new NoiseChunk(state, Beardifier.EMPTY, settings, fluidPicker(settings), Blender.empty(), volume)) {
            Aquifer aquifer = nc.aquifer();
            DensitySampler.Bound finalDensity = nc.cachingSamplers().get(settings.noiseRouter().finalDensity());
            try (ScopedDensityBuffer buf = finalDensity.sampleVolume(volume)) {
                long hash = 0xcbf29ce484222325L;
                for (int i = 0; i < volume.size(); i++) {
                    int b = Float.floatToRawIntBits(buf.get(i));
                    for (int s = 24; s >= 0; s -= 8) {
                        hash = (hash ^ ((b >>> s) & 0xFF)) * 0x100000001b3L;
                    }
                }
                out.println("fill " + chunk + " " + volume.sizeX() + " " + volume.sizeY() + " " + volume.sizeZ() + " " + volume.minBlockX() + " "
                    + volume.minBlockY() + " " + volume.minBlockZ() + " " + String.format("%016x", hash) + " -> " + volume.size());
                int[] cols = {0, 5, 10, 15};
                for (int z : cols) {
                    for (int x : cols) {
                        float[] col = new float[volume.sizeY()];
                        for (int y = 0; y < col.length; y++) {
                            col[y] = buf.get(volume.indexUnchecked(x, y, z));
                        }
                        out.println("fillcol " + chunk + " " + x + " " + z + " ->" + bits(col));
                    }
                }
                String[][] runs = new String[16 * 16][];
                for (int z = 0; z < volume.sizeZ(); z++) {
                    for (int x = 0; x < volume.sizeX(); x++) {
                        StringBuilder sb = new StringBuilder();
                        String last = null;
                        int top = 0;
                        int bottom = 0;
                        for (int y = volume.sizeY() - 1; y >= 0; y--) {
                            int by = volume.blockY(y);
                            String name = blockName(aquifer.computeSubstance(volume.blockX(x), by, volume.blockZ(z), buf.get(volume.indexUnchecked(x, y, z))));
                            if (name.equals(last)) {
                                bottom = by;
                                continue;
                            }
                            if (last != null) {
                                sb.append(last).append(':').append(top).append(':').append(bottom).append(',');
                            }
                            last = name;
                            top = by;
                            bottom = by;
                        }
                        sb.append(last).append(':').append(top).append(':').append(bottom);
                        runs[z * 16 + x] = new String[] {sb.toString()};
                    }
                }
                for (int z : cols) {
                    for (int x : cols) {
                        out.println("aquifer " + chunk + " " + x + " " + z + " -> " + runs[z * 16 + x][0]);
                    }
                }
                out.println("aquiferflag " + chunk + " -> " + (aquifer.shouldScheduleFluidUpdate() ? 1 : 0));
            }
        }
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        lookup = WorldgenSupport.lookup();
        out = new PrintWriter(args[0], "UTF-8");
        out.println("# Generated by tools/java/SamplerGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        out.println("# Grammar: see the header comment of tools/java/SamplerGolden.java. Floats are floatToRawIntBits hex; double inputs are Double.toString text.");
        noiseRecords();
        blendedRecords();
        volumeRecords();
        routerRecords();
        endRecords();
        for (int[] c : new int[][] {{0, 0}, {3, -4}, {-12, 47}, {10, 10}, {-30, 20}}) {
            fillRecord("overworld", 12345L, c[0], c[1]);
        }
        fillRecord("overworld", -4172144997902289642L, 2, 2);
        for (int[] c : new int[][] {{0, 0}, {5, -3}}) {
            fillRecord("nether", 12345L, c[0], c[1]);
        }
        for (int[] c : new int[][] {{0, 0}, {70, 5}, {-8, -9}}) {
            fillRecord("end", 12345L, c[0], c[1]);
        }
        out.close();
    }
}
