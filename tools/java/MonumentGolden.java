import java.io.PrintWriter;
import java.lang.reflect.Field;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HexFormat;
import java.util.List;
import java.util.TreeMap;
import net.minecraft.SharedConstants;
import net.minecraft.core.Direction;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.RandomSource;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.StructurePiece;
import net.minecraft.world.level.levelgen.structure.structures.OceanMonumentPieces.MonumentBuilding;

// Invoke the jar's assembler, then read its private graph and child pieces.
public class MonumentGolden {
    static Object field(Object object, String name) throws Exception {
        for (Class<?> type = object.getClass(); type != null; type = type.getSuperclass()) {
            try {
                Field f = type.getDeclaredField(name);
                f.setAccessible(true);
                return f.get(object);
            } catch (NoSuchFieldException e) { }
        }
        throw new NoSuchFieldException(name);
    }

    static void visit(Object room, TreeMap<Integer, Object> rooms) throws Exception {
        int index = (Integer) field(room, "index");
        if (rooms.putIfAbsent(index, room) != null) return;
        for (Object other : (Object[]) field(room, "connections")) {
            if (other != null) visit(other, rooms);
        }
    }

    static void box(StringBuilder text, BoundingBox b) {
        text.append(b.minX()).append(' ').append(b.minY()).append(' ').append(b.minZ()).append(' ')
            .append(b.maxX()).append(' ').append(b.maxY()).append(' ').append(b.maxZ()).append('\n');
    }

    static int kind(StructurePiece piece) {
        return switch (piece.getClass().getSimpleName()) {
            case "OceanMonumentEntryRoom" -> 0;
            case "OceanMonumentCoreRoom" -> 1;
            case "OceanMonumentDoubleXYRoom" -> 2;
            case "OceanMonumentDoubleYZRoom" -> 3;
            case "OceanMonumentDoubleZRoom" -> 4;
            case "OceanMonumentDoubleXRoom" -> 5;
            case "OceanMonumentDoubleYRoom" -> 6;
            case "OceanMonumentSimpleTopRoom" -> 7;
            case "OceanMonumentSimpleRoom" -> 8;
            case "OceanMonumentWingRoom" -> 9;
            case "OceanMonumentPenthouse" -> 10;
            default -> throw new IllegalStateException("Unsupported piece " + piece.getClass());
        };
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        long[] seeds = {0L, 12345L, -7343414273836990780L, 987654321L, Long.MAX_VALUE};
        Direction[] directions = {Direction.NORTH, Direction.EAST, Direction.SOUTH, Direction.WEST};
        try (PrintWriter out = new PrintWriter(args[0], StandardCharsets.UTF_8)) {
            out.println("# Generated from vanilla " + WorldgenSupport.version() + " OceanMonumentPieces by MonumentGolden.java.");
            for (long seed : seeds) {
                for (int dir = 0; dir < directions.length; dir++) {
                    StringBuilder text = new StringBuilder();
                    int count = 0;
                    for (int sample = 0; sample < 256; sample++) {
                        RandomSource rng = new LegacyRandomSource(seed + sample * -7046029254386353131L);
                        var building = new MonumentBuilding(rng, -137 + sample, 211 - sample, directions[dir]);
                        box(text, building.getBoundingBox());
                        Object source = field(building, "sourceRoom");
                        text.append(field(source, "index")).append(' ').append(field(field(building, "coreRoom"), "index")).append('\n');
                        var rooms = new TreeMap<Integer, Object>();
                        visit(source, rooms);
                        text.append(rooms.size()).append('\n');
                        for (var entry : rooms.entrySet()) {
                            Object room = entry.getValue();
                            text.append(entry.getKey());
                            for (Object other : (Object[]) field(room, "connections")) {
                                text.append(' ').append(other == null ? -1 : field(other, "index"));
                            }
                            int mask = 0;
                            boolean[] openings = (boolean[]) field(room, "hasOpening");
                            for (int d = 0; d < 6; d++) if (openings[d]) mask |= 1 << d;
                            text.append(' ').append(mask).append(' ').append(field(room, "claimed"))
                                .append(' ').append(field(room, "isSource")).append('\n');
                        }
                        List<?> pieces = (List<?>) field(building, "childPieces");
                        text.append(pieces.size()).append('\n');
                        count += pieces.size();
                        for (Object child : pieces) {
                            StructurePiece piece = (StructurePiece) child;
                            int kind = kind(piece);
                            Object room = field(piece, "roomDefinition");
                            int design = kind == 8 || kind == 9 ? (Integer) field(piece, "mainDesign") : 0;
                            if (piece.getOrientation() != directions[dir]) throw new IllegalStateException("Child orientation mismatch");
                            text.append(kind).append(' ').append(room == null ? -1 : field(room, "index"))
                                .append(' ').append(design).append('\n');
                            box(text, piece.getBoundingBox());
                        }
                        text.append(rng.nextInt()).append('\n');
                    }
                    String digest = HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(text.toString().getBytes(StandardCharsets.UTF_8)));
                    out.printf("layout %d %d 256 %d %s%n", seed, dir, count, digest);
                }
            }
        }
    }
}
