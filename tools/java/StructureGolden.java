import java.io.PrintWriter;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HexFormat;
import java.util.List;
import java.util.Optional;
import net.minecraft.SharedConstants;
import net.minecraft.core.Holder;
import net.minecraft.core.HolderLookup;
import net.minecraft.core.HolderSet;
import net.minecraft.core.Vec3i;
import net.minecraft.core.registries.Registries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.biome.BiomeSource;
import net.minecraft.world.level.chunk.ChunkGeneratorStructureState;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.RandomState;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.structure.Structure;
import net.minecraft.world.level.levelgen.structure.StructureSet;
import net.minecraft.world.level.levelgen.structure.placement.ConcentricRingsStructurePlacement;
import net.minecraft.world.level.levelgen.structure.placement.RandomSpreadStructurePlacement;
import net.minecraft.world.level.levelgen.structure.placement.RandomSpreadType;
import net.minecraft.world.level.levelgen.structure.placement.StructurePlacement;

// Writes the structure-layer golden values internal/server/world/levelgen's tests
// compare against, from the vanilla classes on the server jar (mise run
// gen:structure-golden); the line formats are the ones
// internal/server/world/levelgen/structure_golden_test.go reads. Registries list in
// identifier order, the order a server loads them from its data pack in.
//
// structure: name, biome tag, decoration step ordinal, terrain adaptation.
// set: name, then "spread" spacing separation spread_type salt frequency
// reducer offset_x offset_y offset_z exclusion_set exclusion_chunks, or
// "rings" distance spread count preferred_tag.
// entry: set, structure, weight, in the set's order.
// chunks: seed, set, range, the number of chunks isStructureChunk passes for
// and a digest of them ("x z" lines, x outer).
// select: seed, set, range, the number of its passing chunks and a digest of
// "x z" and the order createStructures tries the entries in when each fails.
// thinned: seed, spacing, separation, salt, frequency, range, count and
// digest as for chunks, of a synthetic placement with the default reducer.
public class StructureGolden {
    static final long[] SEEDS = {0L, 12345L, -7343414273836990780L};
    static final int RANGE = 100;

    static String digest(List<String> lines) throws Exception {
        MessageDigest md = MessageDigest.getInstance("SHA-256");
        md.update(String.join("\n", lines).getBytes(StandardCharsets.UTF_8));
        return HexFormat.of().formatHex(md.digest());
    }

    static Object call(Object target, String name) throws Exception {
        Method m = spreadingClass().getDeclaredMethod(name);
        m.setAccessible(true);
        return m.invoke(target);
    }

    static String name(Holder<?> h) {
        return h.unwrapKey().get().identifier().toString();
    }

    static String tag(HolderSet<?> set) {
        return "#" + set.unwrapKey().get().location().toString();
    }

    // The order ChunkGenerator.createStructures tries a set's entries in, as
    // though each failed.
    static List<String> tryOrder(StructureSet set, long seed, int x, int z) {
        List<StructureSet.StructureSelectionEntry> options = new ArrayList<>(set.structures());
        List<String> out = new ArrayList<>();
        WorldgenRandom random = new WorldgenRandom(new LegacyRandomSource(0L));
        random.setLargeFeatureSeed(seed, x, z);
        int total = 0;
        for (var option : options) {
            total += option.weight();
        }
        while (!options.isEmpty()) {
            int choice = random.nextInt(total);
            int index = 0;
            for (var option : options) {
                if ((choice -= option.weight()) < 0) {
                    break;
                }
                index++;
            }
            var selected = options.remove(index);
            out.add(name(selected.structure()));
            total -= selected.weight();
        }
        return out;
    }

    static Class<?> spreadingClass() throws Exception {
        return Class.forName("net.minecraft.world.level.levelgen.structure.placement.AbstractSpreadingStructurePlacement");
    }

    static String frequencyName(StructurePlacement placement) throws Exception {
        Object reducer = call(placement, "frequencyReductionMethod");
        return (String) reducer.getClass().getMethod("getSerializedName").invoke(reducer);
    }

    static String exclusionName(Optional<?> zone) throws Exception {
        return zone.isEmpty() ? "-" : name((Holder<?>) zone.get().getClass().getMethod("otherSet").invoke(zone.get()));
    }

    static int exclusionChunks(Optional<?> zone) throws Exception {
        return zone.isEmpty() ? 0 : (Integer) zone.get().getClass().getMethod("chunkCount").invoke(zone.get());
    }

    static StructurePlacement thinnedPlacement() throws Exception {
        var reducer = Class.forName(spreadingClass().getName() + "$FrequencyReductionMethod");
        return RandomSpreadStructurePlacement.class.getConstructor(Vec3i.class, reducer, float.class, int.class,
            Optional.class, int.class, int.class, RandomSpreadType.class).newInstance(Vec3i.ZERO,
            reducer.getField("DEFAULT").get(null), 0.5f, 12345, Optional.empty(), 7, 3, RandomSpreadType.LINEAR);
    }

    static ChunkGeneratorStructureState structureState(long seed, List<Holder<StructureSet>> sets) throws Exception {
        java.lang.reflect.Constructor<ChunkGeneratorStructureState> ctor;
        try {
            ctor = ChunkGeneratorStructureState.class.getDeclaredConstructor(RandomState.class, BiomeSource.class,
                long.class, net.minecraft.world.level.ChunkPos.class, long.class, List.class);
        } catch (NoSuchMethodException e) {
            ctor = ChunkGeneratorStructureState.class.getDeclaredConstructor(RandomState.class, BiomeSource.class,
                long.class, long.class, List.class);
            ctor.setAccessible(true);
            return ctor.newInstance(null, null, seed, seed, sets);
        }
        ctor.setAccessible(true);
        return ctor.newInstance(null, null, seed, new net.minecraft.world.level.ChunkPos(0, 0), seed, sets);
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        PrintWriter out = new PrintWriter(args[0], "UTF-8");
        out.println("# Generated by tools/java/StructureGolden.java from the vanilla " + WorldgenSupport.version() + " server jar. DO NOT EDIT.");
        HolderLookup.Provider lookup = WorldgenSupport.lookup();

        List<Holder.Reference<Structure>> structures = new ArrayList<>(lookup.lookupOrThrow(Registries.STRUCTURE).listElements().toList());
        structures.sort(Comparator.comparing(StructureGolden::name));
        for (var h : structures) {
            Structure s = h.value();
            out.println("structure " + name(h) + " " + tag(s.biomes()) + " " + s.step().ordinal() + " " + s.terrainAdaptation().getSerializedName());
        }

        List<Holder.Reference<StructureSet>> sets = new ArrayList<>(lookup.lookupOrThrow(Registries.STRUCTURE_SET).listElements().toList());
        sets.sort(Comparator.comparing(StructureGolden::name));
        for (var h : sets) {
            StructurePlacement p = h.value().placement();
            if (p instanceof RandomSpreadStructurePlacement r) {
                Vec3i offset = (Vec3i) call(p, "locateOffset");
                Optional<?> ex = (Optional<?>) call(p, "exclusionZone");
                out.println("set " + name(h) + " spread " + r.spacing() + " " + r.separation() + " " + r.spreadType().getSerializedName()
                    + " " + call(p, "salt") + " " + call(p, "frequency") + " " + frequencyName(p)
                    + " " + offset.getX() + " " + offset.getY() + " " + offset.getZ()
                    + " " + exclusionName(ex) + " " + exclusionChunks(ex));
            } else {
                ConcentricRingsStructurePlacement c = (ConcentricRingsStructurePlacement) p;
                out.println("set " + name(h) + " rings " + c.distance() + " " + c.spread() + " " + c.count() + " " + tag(c.preferredBiomes()));
            }
            for (var e : h.value().structures()) {
                out.println("entry " + name(h) + " " + name(e.structure()) + " " + e.weight());
            }
        }

        // The tags are unbound in a bootstrap lookup, so the state is built
        // over every set rather than filtered by the biome source; only the
        // seed and the placements are read.
        List<Holder<StructureSet>> possible = new ArrayList<>(sets);
        for (long seed : SEEDS) {
            ChunkGeneratorStructureState state = structureState(seed, possible);
            for (var h : possible) {
                StructureSet set = h.value();
                if (!(set.placement() instanceof RandomSpreadStructurePlacement)) {
                    continue;
                }
                // The outposts' exclusion zone needs many villages to be
                // pinned at its edge.
                int r = name(h).equals("minecraft:pillager_outposts") ? 15 * RANGE : RANGE;
                List<String> chunks = new ArrayList<>();
                List<String> picks = new ArrayList<>();
                for (int x = -r; x < r; x++) {
                    for (int z = -r; z < r; z++) {
                        if (!set.placement().isStructureChunk(state, x, z)) {
                            continue;
                        }
                        chunks.add(x + " " + z);
                        if (set.structures().size() > 1) {
                            picks.add(x + " " + z + " " + String.join(",", tryOrder(set, seed, x, z)));
                        }
                    }
                }
                String range = -r + " " + (r - 1);
                out.println("chunks " + seed + " " + name(h) + " " + range + " " + chunks.size() + " " + digest(chunks));
                if (!picks.isEmpty()) {
                    out.println("select " + seed + " " + name(h) + " " + range + " " + picks.size() + " " + digest(picks));
                }
            }
        }

        // No vanilla set thins with the default reducer, so a synthetic
        // placement pins it.
        StructurePlacement thinned = thinnedPlacement();
        for (long seed : SEEDS) {
            ChunkGeneratorStructureState state = structureState(seed, possible);
            List<String> chunks = new ArrayList<>();
            for (int x = -RANGE; x < RANGE; x++) {
                for (int z = -RANGE; z < RANGE; z++) {
                    if (thinned.isStructureChunk(state, x, z)) {
                        chunks.add(x + " " + z);
                    }
                }
            }
            out.println("thinned " + seed + " 7 3 12345 0.5 " + -RANGE + " " + (RANGE - 1) + " " + chunks.size() + " " + digest(chunks));
        }

        out.close();
    }
}
