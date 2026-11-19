import java.io.PrintWriter;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Proxy;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HashMap;
import java.util.HashSet;
import java.util.HexFormat;
import java.util.List;
import java.lang.reflect.Field;
import net.minecraft.world.level.levelgen.structure.StructurePiece;
import java.util.Map;
import java.util.Set;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.WorldGenLevel;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.BlockStateProperties;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.structures.OceanMonumentPieces.MonumentBuilding;

// Run the jar's fitted interior rooms, retaining their graph and chunk random stream.
public class MonumentRoomsGolden {
    static final int[] SEA = {63, 32, 70, 48};
    static BlockState terrain(int mode, BlockPos pos) {
        int x = pos.getX(), y = pos.getY(), z = pos.getZ();
        if (mode == 3) {
            return switch (Math.floorMod(x + y + 2 * z, 10)) {
                case 0 -> Blocks.WATER.defaultBlockState().setValue(BlockStateProperties.LEVEL, 7);
                case 1 -> Blocks.ICE.defaultBlockState();
                case 2 -> Blocks.PACKED_ICE.defaultBlockState();
                case 3 -> Blocks.BLUE_ICE.defaultBlockState();
                case 4 -> Blocks.STONE.defaultBlockState();
                case 5 -> Blocks.LAVA.defaultBlockState();
                case 6 -> Blocks.AIR.defaultBlockState();
                default -> Blocks.WATER.defaultBlockState();
            };
        }
        int floor = mode == 2 ? 60 + Math.floorMod(x + z, 9) : 25 + Math.floorMod(x * 3 + z * 5, 11);
        if (mode == 1) floor = Math.floorMod(x + z, 7) == 0 ? -65 : -60 + Math.floorMod(x + z, 5);
        if (y <= floor) return Blocks.STONE.defaultBlockState();
        int stripe = Math.floorMod(x + 2 * z, 9);
        if (y == SEA[mode]) {
            return switch (stripe) {
                case 0 -> Blocks.ICE.defaultBlockState();
                case 1 -> Blocks.PACKED_ICE.defaultBlockState();
                case 2 -> Blocks.BLUE_ICE.defaultBlockState();
                case 3 -> Blocks.WATER.defaultBlockState().setValue(BlockStateProperties.LEVEL, 7);
                default -> Blocks.AIR.defaultBlockState();
            };
        }
        if (y == 37) {
            return switch (stripe) {
                case 0 -> Blocks.GLOW_LICHEN.defaultBlockState();
                case 1 -> Blocks.SEAGRASS.defaultBlockState();
                case 2 -> Blocks.TALL_SEAGRASS.defaultBlockState();
                case 3 -> Blocks.LAVA.defaultBlockState();
                case 4 -> Blocks.KELP.defaultBlockState();
                default -> Blocks.WATER.defaultBlockState();
            };
        }
        return (y < SEA[mode] ? Blocks.WATER : Blocks.AIR).defaultBlockState();
    }

    public static void main(String[] args) throws Exception {
        try { generate(args); } catch (Throwable e) {
            e.printStackTrace(new java.io.PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.err)));
            throw e;
        }
    }

    static void generate(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var children = MonumentBuilding.class.getDeclaredField("childPieces");
        children.setAccessible(true);
        Direction[] directions = {Direction.NORTH, Direction.EAST, Direction.SOUTH, Direction.WEST};
        try (PrintWriter out = new PrintWriter(args[0], StandardCharsets.UTF_8)) {
            out.println("# Generated from vanilla " + WorldgenSupport.version() + " OceanMonumentPieces interior postProcess methods by MonumentRoomsGolden.java.");
            long[] seeds = {0L, 12345L, -7343414273836990780L, 987654321L, 1L, 2L, 3L, 18L};
            boolean[] kinds = new boolean[11], designs = new boolean[3];
            for (long seed : seeds) {
                for (int mode = 0; mode < SEA.length; mode++) {
                    final int terrainMode = mode;
                    for (int dir = 0; dir < directions.length; dir++) {
                        var building = new MonumentBuilding(new LegacyRandomSource(seed), -19, 7, directions[dir]);
                        @SuppressWarnings("unchecked")
                        List<StructurePiece> pieces = (List<StructurePiece>) children.get(building);
                        Set<BlockPos> elders = new HashSet<>();
                        for (int i = 0; i < pieces.size(); i++) {
                            StructurePiece piece = pieces.get(i);
                            if (kind(piece) == 9) {
                                Field design = piece.getClass().getDeclaredField("mainDesign");
                                design.setAccessible(true);
                                pieces.set(i, new net.minecraft.world.level.levelgen.structure.structures.OceanMonumentPieces.OceanMonumentWingRoom(directions[dir], piece.getBoundingBox(), design.getInt(piece)) {
                                    @Override protected void spawnElder(WorldGenLevel level, BoundingBox clip, int x, int y, int z) {
                                        BlockPos pos = getWorldPos(x, y, z).immutable();
                                        if (clip.isInside(pos) && !elders.add(pos)) throw new IllegalStateException("Duplicate elder");
                                    }
                                });
                            } else if (kind(piece) == 10) {
                                pieces.set(i, new net.minecraft.world.level.levelgen.structure.structures.OceanMonumentPieces.OceanMonumentPenthouse(directions[dir], piece.getBoundingBox()) {
                                    @Override protected void spawnElder(WorldGenLevel level, BoundingBox clip, int x, int y, int z) {
                                        BlockPos pos = getWorldPos(x, y, z).immutable();
                                        if (clip.isInside(pos) && !elders.add(pos)) throw new IllegalStateException("Duplicate elder");
                                    }
                                });
                            }
                        }
                        for (Object child : pieces) {
                            int kind = kind((StructurePiece) child);
                            kinds[kind] = true;
                            if (kind == 8) {
                                Field design = child.getClass().getDeclaredField("mainDesign");
                                design.setAccessible(true);
                                designs[design.getInt(child)] = true;
                            }
                        }
                        Map<BlockPos, BlockState> blocks = new HashMap<>();
                        Set<BlockPos> ticks = new HashSet<>();
                        WorldGenLevel level = (WorldGenLevel) Proxy.newProxyInstance(MonumentRoomsGolden.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, (proxy, method, a) -> {
                            switch (method.getName()) {
                                case "getBlockState": return blocks.getOrDefault((BlockPos) a[0], terrain(terrainMode, (BlockPos) a[0]));
                                case "getFluidState": return blocks.getOrDefault((BlockPos) a[0], terrain(terrainMode, (BlockPos) a[0])).getFluidState();
                                case "setBlock": blocks.put(((BlockPos) a[0]).immutable(), (BlockState) a[1]); return true;
                                case "getMinY": return -64;
                                case "getSeaLevel": return SEA[terrainMode];
                                case "scheduleTick": ticks.add(((BlockPos) a[0]).immutable()); return null;
                                default:
                                    if (method.isDefault()) return InvocationHandler.invokeDefault(proxy, method, a);
                                    throw new UnsupportedOperationException(method.getName());
                            }
                        });
                        MessageDigest randoms = MessageDigest.getInstance("SHA-256");
                        for (int cz = -1; cz <= 5; cz++) {
                            for (int cx = -3; cx <= 3; cx++) {
                                var box = new BoundingBox(cx * 16, mode == 3 ? 43 : -64, cz * 16, cx * 16 + 15, mode == 3 ? 49 : 79, cz * 16 + 15);
                                var rng = new LegacyRandomSource(seed + cx * 31L + cz * 17L);
                                for (Object child : pieces) {
                                    StructurePiece piece = (StructurePiece) child;
                                    if (piece.getBoundingBox().intersects(box)) {
                                        piece.postProcess(level, null, null, rng, box, new ChunkPos(cx, cz), BlockPos.ZERO);
                                    }
                                }
                                int next = rng.nextInt();
                                for (int shift = 24; shift >= 0; shift -= 8) randoms.update((byte) (next >> shift));
                            }
                        }
                        MessageDigest states = MessageDigest.getInstance("SHA-256"), fluids = MessageDigest.getInstance("SHA-256"), spawns = MessageDigest.getInstance("SHA-256");
                        for (int x = -48; x < 64; x++) {
                            for (int y = 32; y < 64; y++) {
                                for (int z = -16; z < 96; z++) {
                                    var pos = new BlockPos(x, y, z);
                                    states.update(WorldgenSupport.stateBytes(blocks.getOrDefault(pos, terrain(mode, pos))));
                                    fluids.update((byte) (ticks.contains(pos) ? 1 : 0));
                                    spawns.update((byte) (elders.contains(pos) ? 1 : 0));
                                }
                            }
                        }
                        if (blocks.keySet().stream().anyMatch(p -> p.getX() < -48 || p.getX() >= 64 || p.getY() < 32 || p.getY() >= 64 || p.getZ() < -16 || p.getZ() >= 96)) throw new IllegalStateException("Write outside digest volume");
                        long changed = blocks.entrySet().stream().filter(e -> e.getValue() != terrain(terrainMode, e.getKey())).count();
                        out.printf("rooms %d %d %d %d %s %d %s %s %d %s%n", seed, mode, dir, changed, HexFormat.of().formatHex(states.digest()), ticks.size(), HexFormat.of().formatHex(fluids.digest()), HexFormat.of().formatHex(randoms.digest()), elders.size(), HexFormat.of().formatHex(spawns.digest()));
                    }
                }
            }
            for (boolean seen : kinds) if (!seen) throw new IllegalStateException("Missing room kind");
            for (boolean seen : designs) if (!seen) throw new IllegalStateException("Missing simple design");
        }
    }

    static int kind(StructurePiece piece) {
        Class<?> type = piece.getClass();
        if (type.isAnonymousClass()) type = type.getSuperclass();
        return switch (type.getSimpleName()) {
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
}
