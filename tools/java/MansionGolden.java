import java.io.PrintWriter;
import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import java.util.ArrayList;
import java.util.List;
import java.nio.file.Path;
import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.packs.PackType;
import net.minecraft.server.packs.resources.MultiPackResourceManager;
import net.minecraft.util.datafix.DataFixers;
import net.minecraft.world.level.block.Rotation;
import net.minecraft.world.level.levelgen.structure.TemplateStructurePiece;
import net.minecraft.world.level.levelgen.structure.structures.WoodlandMansionPieces;
import net.minecraft.world.level.levelgen.structure.templatesystem.StructureTemplateManager;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.LegacyRandomSource;

// Reads the private grid and room collections from the server jar, without
// copying the generator. Grid digests include every cell and the next RNG draw.
public class MansionGolden {
    static final String PREFIX = "net.minecraft.world.level.levelgen.structure.structures.WoodlandMansionPieces$";

    static Object field(Object object, String name) throws Exception {
        Field f = object.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(object);
    }

    static Object construct(String name, Class<?>[] types, Object... args) throws Exception {
        Constructor<?> c = Class.forName(PREFIX + name).getDeclaredConstructor(types);
        c.setAccessible(true);
        return c.newInstance(args);
    }

    static String digest(String text) throws Exception {
        return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8)));
    }

    // Empty templates suffice: assembly uses fixed grid offsets, never their
    // dimensions or blocks. Read the final transforms directly from the pieces.
    static void assembly(PrintWriter out, long[] seeds) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        Field name = TemplateStructurePiece.class.getDeclaredField("templateName");
        name.setAccessible(true);
        try (var resources = new MultiPackResourceManager(PackType.SERVER_DATA, List.of());
             var storage = LevelStorageSource.createDefault(Path.of("mansion-golden-worlds")).createAccess("test")) {
            var manager = new StructureTemplateManager(resources, storage, DataFixers.getDataFixer(), BuiltInRegistries.BLOCK);
            for (long seed : seeds) {
                for (Rotation rotation : Rotation.values()) {
                    StringBuilder batch = new StringBuilder();
                    int count = 0;
                    for (int sample = 0; sample < 256; sample++) {
                        RandomSource rng = new LegacyRandomSource(seed + sample);
                        var pieces = new ArrayList<WoodlandMansionPieces.WoodlandMansionPiece>();
                        BlockPos origin = new BlockPos(-137 + sample, 70 + sample % 5, 211 - sample);
                        WoodlandMansionPieces.generateMansion(manager, origin, rotation, pieces, rng);
                        batch.append(pieces.size()).append('\n');
                        count += pieces.size();
                        for (var piece : pieces) {
                            BlockPos pos = piece.templatePosition();
                            batch.append(name.get(piece)).append(' ').append(pos.getX()).append(' ')
                                .append(pos.getY()).append(' ').append(pos.getZ()).append(' ')
                                .append(piece.getRotation().ordinal()).append(' ')
                                .append(piece.placeSettings().getMirror().ordinal()).append('\n');
                        }
                        batch.append(rng.nextInt()).append('\n');
                    }
                    out.printf("pieces %d %d 256 %d %s%n", seed, rotation.ordinal(), count, digest(batch.toString()));
                }
            }
        }
    }

    public static void main(String[] args) throws Exception {
        net.minecraft.SharedConstants.tryDetectVersion();
        try (PrintWriter out = new PrintWriter(args[0], StandardCharsets.UTF_8)) {
            out.println("# Generated from the vanilla " + WorldgenSupport.version() + " WoodlandMansionPieces classes by MansionGolden.java.");
            Method get = Class.forName(PREFIX + "SimpleGrid").getDeclaredMethod("get", int.class, int.class);
            get.setAccessible(true);
            long[] seeds = {0L, 12345L, -7343414273836990780L, 987654321L, Long.MAX_VALUE};
            for (long seed : seeds) {
                int absent = 0, stairs = 0, secrets = 0;
                StringBuilder batch = new StringBuilder();
                for (int sample = 0; sample < 256; sample++) {
                    RandomSource rng = new LegacyRandomSource(seed + sample);
                    Object grid = construct("MansionGrid", new Class<?>[]{RandomSource.class}, rng);
                    Object[] rooms = (Object[]) field(grid, "floorRooms");
                    Object[] grids = {field(grid, "baseGrid"), field(grid, "thirdFloorGrid"), rooms[0], rooms[1], rooms[2]};
                    boolean hasThird = false;
                    for (int i = 0; i < grids.length; i++) {
                        for (int z = 0; z < 11; z++) {
                            for (int x = 0; x < 11; x++) {
                                int value = (Integer) get.invoke(grids[i], x, z);
                                batch.append(value).append('\n');
                                if (i == 1 && value >= 1 && value <= 4) hasThird = true;
                                if (i >= 2 && (value & 0x400000) != 0) stairs++;
                                if (i >= 2 && (value & 0x100000) != 0 && (value & 0x200000) == 0) secrets++;
                            }
                        }
                    }
                    if (!hasThird) absent++;
                    batch.append(rng.nextInt()).append('\n');
                }
                out.printf("grid %d 256 %d %d %d %s%n", seed, absent, stairs, secrets, digest(batch.toString()));
            }
            assembly(out, seeds);
            Method set = Class.forName(PREFIX + "SimpleGrid").getDeclaredMethod("set", int.class, int.class, int.class);
            set.setAccessible(true);
            Method setup = Class.forName(PREFIX + "MansionGrid").getDeclaredMethod("setupThirdFloor");
            setup.setAccessible(true);
            for (int scenario = 0; scenario < 2; scenario++) {
                RandomSource rng = new LegacyRandomSource(12345L);
                Object grid = construct("MansionGrid", new Class<?>[]{RandomSource.class}, rng);
                Object[] rooms = (Object[]) field(grid, "floorRooms");
                Object[] grids = {field(grid, "baseGrid"), field(grid, "thirdFloorGrid"), rooms[0], rooms[1], rooms[2]};
                for (Object g : grids) {
                    for (int z = 0; z < 11; z++) {
                        for (int x = 0; x < 11; x++) set.invoke(g, x, z, 0);
                    }
                }
                if (scenario == 1) {
                    set.invoke(grids[0], 5, 5, 2);
                    set.invoke(grids[0], 6, 5, 2);
                    set.invoke(rooms[1], 5, 5, 0x32000a);
                    set.invoke(rooms[1], 6, 5, 0x2000a);
                }
                rng.setSeed(12345L);
                setup.invoke(grid);
                StringBuilder batch = new StringBuilder();
                for (Object g : grids) {
                    for (int z = 0; z < 11; z++) {
                        for (int x = 0; x < 11; x++) batch.append(get.invoke(g, x, z)).append('\n');
                    }
                }
                batch.append(rng.nextInt()).append('\n');
                out.printf("fallback %d %s%n", scenario, digest(batch.toString()));
            }
            String[] names = {"get1x1", "get1x1Secret", "get1x2SideEntrance", "get1x2FrontEntrance", "get1x2Secret", "get2x2", "get2x2Secret"};
            Class<?> base = Class.forName(PREFIX + "FloorRoomCollection");
            for (int floor = 0; floor < 3; floor++) {
                String collection = new String[]{"FirstFloorRoomCollection", "SecondFloorRoomCollection", "ThirdFloorRoomCollection"}[floor];
                Object rooms = construct(collection, new Class<?>[]{});
                for (int kind = 0; kind < names.length; kind++) {
                    boolean medium = kind == 2 || kind == 3;
                    Method method = medium ? base.getDeclaredMethod(names[kind], RandomSource.class, boolean.class) : base.getDeclaredMethod(names[kind], RandomSource.class);
                    method.setAccessible(true);
                    for (boolean stairs : new boolean[]{false, true}) {
                        RandomSource rng = new LegacyRandomSource(12345L);
                        StringBuilder batch = new StringBuilder();
                        for (int sample = 0; sample < 256; sample++) {
                            batch.append(medium ? method.invoke(rooms, rng, stairs) : method.invoke(rooms, rng)).append('\n');
                            batch.append(rng.nextInt()).append('\n');
                        }
                        out.printf("room %d %d %b %s%n", floor, kind, stairs, digest(batch.toString()));
                    }
                }
            }
        }
    }
}
