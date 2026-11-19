import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import net.minecraft.SharedConstants;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.lighting.LightEngine;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.shapes.Shapes;
import net.minecraft.world.phys.shapes.VoxelShape;

// Prints the light data of every block state of a vanilla server, the values
// the vanilla light engine consults (LightEngine): getLightDampening, the
// per-cell opacity, and - only consulted between two states that both occlude
// by shape (canOcclude && useShapeForLightOcclusion) - the occlusion footprint
// of each of the state's six faces, two of which seal the shared face between
// neighbouring cells when their union covers the whole plane. Run by
// tools/gen lightdata.
//
// A face is printed as a code: 0 for Shapes.empty(), 1 for a face covering the
// whole plane (Shapes.block() or any slice spanning it - the two seal against
// everything, which is what faceShapeOccludes' early return and full-coverage
// union both give), otherwise an index into the dumped MASK lines. A mask is
// the face's footprint on the 16x16 grid of the two axes that are not the
// face's own, in X/Y/Z order, bit x + z*16 of four 64-bit words. Faces come
// from LightEngine.getOcclusionShape, which answers empty for every state that
// does not occlude by shape, so only the slabs, stairs, snow layers and the
// other shape-occluding blocks get non-zero codes.
public class LightData {
    private static final Map<String, Integer> MASK_INDEX = new HashMap<>();
    private static int maskCount = 0;

    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                StringBuilder faces = new StringBuilder();
                for (Direction d : Direction.values()) {
                    if (faces.length() > 0) {
                        faces.append(',');
                    }
                    faces.append(faceCode(LightEngine.getOcclusionShape(state, d), d));
                }
                System.out.println("STATE " + state + "|" + state.getLightDampening() + "|" + faces);
            }
        }
    }

    static String faceCode(VoxelShape face, Direction d) {
        if (face.isEmpty()) {
            return "0";
        }
        if (face == Shapes.block()) {
            return "1";
        }
        Direction.Axis axis = d.getAxis();
        Direction.Axis plane1 = null;
        Direction.Axis plane2 = null;
        for (Direction.Axis a : Direction.Axis.values()) {
            if (a == axis) {
                continue;
            }
            if (plane1 == null) {
                plane1 = a;
            } else {
                plane2 = a;
            }
        }
        boolean[] plane = new boolean[256];
        int count = 0;
        for (AABB box : face.toAabbs()) {
            int lo1 = gridCell(box.min(plane1) * 16.0);
            int hi1 = gridCell(box.max(plane1) * 16.0);
            int lo2 = gridCell(box.min(plane2) * 16.0);
            int hi2 = gridCell(box.max(plane2) * 16.0);
            for (int c2 = lo2; c2 < hi2; c2++) {
                for (int c1 = lo1; c1 < hi1; c1++) {
                    int i = c1 + c2 * 16;
                    if (!plane[i]) {
                        plane[i] = true;
                        count++;
                    }
                }
            }
        }
        if (count == 256) {
            return "1";
        }
        if (count == 0) {
            return "0";
        }
        long[] words = new long[4];
        for (int i = 0; i < 256; i++) {
            if (plane[i]) {
                words[i >> 6] |= 1L << (i & 63);
            }
        }
        StringBuilder key = new StringBuilder();
        StringBuilder line = new StringBuilder("MASK ").append(maskCount);
        boolean first = true;
        for (long w : words) {
            key.append(Long.toHexString(w)).append(':');
            line.append(first ? ' ' : ':').append(Long.toHexString(w));
            first = false;
        }
        Integer idx = MASK_INDEX.get(key.toString());
        if (idx == null) {
            idx = maskCount++;
            MASK_INDEX.put(key.toString(), idx);
            System.out.println(line);
        }
        return String.valueOf(idx + 2);
    }

    static int gridCell(double v) {
        long r = Math.round(v);
        if (Math.abs(v - (double) r) > 1e-6) {
            throw new IllegalStateException("face coordinate off the 1/16 grid: " + v);
        }
        return (int) r;
    }
}
