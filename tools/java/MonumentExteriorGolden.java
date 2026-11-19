import java.io.PrintWriter;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Proxy;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.HashMap;
import java.util.HashSet;
import java.util.HexFormat;
import java.util.List;
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

// Run the actual building postProcess with only childPieces removed. The proxy
// models the block and fluid writes of WorldGenRegion and rejects unknown calls.
public class MonumentExteriorGolden {
    static final int[] SEA = {63, 32, 70};
    static BlockState terrain(int mode, BlockPos pos) {
        int x = pos.getX(), y = pos.getY(), z = pos.getZ();
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
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var children = MonumentBuilding.class.getDeclaredField("childPieces");
        children.setAccessible(true);
        Direction[] directions = {Direction.NORTH, Direction.EAST, Direction.SOUTH, Direction.WEST};
        try (PrintWriter out = new PrintWriter(args[0], StandardCharsets.UTF_8)) {
            out.println("# Generated from vanilla " + WorldgenSupport.version() + " MonumentBuilding.postProcess by MonumentExteriorGolden.java.");
            for (int mode = 0; mode < SEA.length; mode++) {
                final int terrainMode = mode;
                for (int dir = 0; dir < directions.length; dir++) {
                    var building = new MonumentBuilding(new LegacyRandomSource(12345), -19, 7, directions[dir]);
                    ((List<?>) children.get(building)).clear();
                    Map<BlockPos, BlockState> blocks = new HashMap<>();
                    Set<BlockPos> ticks = new HashSet<>();
                    WorldGenLevel level = (WorldGenLevel) Proxy.newProxyInstance(MonumentExteriorGolden.class.getClassLoader(), new Class<?>[] {WorldGenLevel.class}, (proxy, method, a) -> {
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
                    for (int cz = -1; cz <= 5; cz++) {
                        for (int cx = -3; cx <= 3; cx++) {
                            var box = new BoundingBox(cx * 16, -64, cz * 16, cx * 16 + 15, 79, cz * 16 + 15);
                            var rng = new LegacyRandomSource(0);
                            building.postProcess(level, null, null, rng, box, new ChunkPos(cx, cz), BlockPos.ZERO);
                            if (rng.nextInt() != new LegacyRandomSource(0).nextInt()) throw new IllegalStateException("Exterior consumed random");
                        }
                    }
                    MessageDigest states = MessageDigest.getInstance("SHA-256"), fluids = MessageDigest.getInstance("SHA-256");
                    for (int x = -48; x < 64; x++) {
                        for (int y = -64; y < 80; y++) {
                            for (int z = -16; z < 96; z++) {
                                var pos = new BlockPos(x, y, z);
                                states.update(WorldgenSupport.stateBytes(blocks.getOrDefault(pos, terrain(mode, pos))));
                                fluids.update((byte) (ticks.contains(pos) ? 1 : 0));
                            }
                        }
                    }
                    if (blocks.keySet().stream().anyMatch(p -> p.getX() < -48 || p.getX() >= 64 || p.getY() < -64 || p.getY() >= 80 || p.getZ() < -16 || p.getZ() >= 96)) throw new IllegalStateException("Write outside digest volume");
                    long changed = blocks.entrySet().stream().filter(e -> e.getValue() != terrain(terrainMode, e.getKey())).count();
                    out.printf("exterior %d %d %d %s %d %s%n", mode, dir, changed, HexFormat.of().formatHex(states.digest()), ticks.size(), HexFormat.of().formatHex(fluids.digest()));
                }
            }
        }
    }
}
