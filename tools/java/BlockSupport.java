import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.SupportType;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.shapes.CollisionContext;
import net.minecraft.world.phys.shapes.BooleanOp;
import net.minecraft.world.phys.shapes.Shapes;
import net.minecraft.world.phys.shapes.VoxelShape;

import java.util.List;

// Prints, for every block state of a vanilla server, what the canSurvive
// rules ask of a neighbour: isFaceSturdy per direction for each SupportType,
// isSolid, isSolidRender, and the isFaceFull tests (MultifaceBlock's
// support-or-collision one per direction, and the collision shape's top and
// bottom that SnowLayerBlock and CeilingHangingSignBlock read), plus what a
// body standing on the state's own cell asks of its collision shape: the max
// Y the shape reaches and whether the whole shape is one box covering the
// cell floor. Entity-dependent shapes read as the empty collision context
// leaves them. Also, per state, which of WallBlock's test shapes (the post,
// then the north, south, west and east arms, in that bit order) the DOWN
// face of its collision shape covers, WallBlock.isCovered. Run by
// tools/gen blocksupport.
//
// One line per state: STATE <state>|<full>|<center>|<rigid>|<attach>|<flags>|<top>|<flat>|<wall>|<collisionFaces>,
// each mask a bit per direction in Direction ordinal order, flags bit 0
// isSolid, bit 1 the collision shape's top face full, bit 2 isSolidRender,
// bit 3 the collision shape's bottom face full, bit 4 isShapeFullBlock of
// the collision shape, vanilla's isCollisionShapeFullBlock, bit 5 the same
// of the outline shape (getShape).
public class BlockSupport {
    private static final VoxelShape[] WALL_TESTS = {
        Shapes.box(7 / 16.0, 0, 7 / 16.0, 9 / 16.0, 1, 9 / 16.0),
        Shapes.box(7 / 16.0, 0, 0, 9 / 16.0, 1, 9 / 16.0),
        Shapes.box(7 / 16.0, 0, 7 / 16.0, 9 / 16.0, 1, 1),
        Shapes.box(0, 0, 7 / 16.0, 9 / 16.0, 1, 9 / 16.0),
        Shapes.box(7 / 16.0, 0, 7 / 16.0, 1, 1, 9 / 16.0),
    };

    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        EmptyBlockGetter level = EmptyBlockGetter.INSTANCE;
        BlockPos pos = BlockPos.ZERO;
        CollisionContext context = CollisionContext.empty();
        for (Block block : BuiltInRegistries.BLOCK) {
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                int full = 0, center = 0, rigid = 0, attach = 0, collisionFaces = 0;
                VoxelShape support = state.getBlockSupportShape(level, pos);
                VoxelShape collision = state.getCollisionShape(level, pos, context);
                for (Direction d : Direction.values()) {
                    int bit = 1 << d.ordinal();
                    if (Block.isFaceFull(collision, d)) {
                        collisionFaces |= bit;
                    }
                    if (state.isFaceSturdy(level, pos, d, SupportType.FULL)) {
                        full |= bit;
                    }
                    if (state.isFaceSturdy(level, pos, d, SupportType.CENTER)) {
                        center |= bit;
                    }
                    if (state.isFaceSturdy(level, pos, d, SupportType.RIGID)) {
                        rigid |= bit;
                    }
                    if (Block.isFaceFull(support, d) || Block.isFaceFull(collision, d)) {
                        attach |= bit;
                    }
                }
                int flags = 0;
                if (state.isSolid()) {
                    flags |= 1;
                }
                if (Block.isFaceFull(collision, Direction.UP)) {
                    flags |= 2;
                }
                if (state.isSolidRender()) {
                    flags |= 4;
                }
                if (Block.isFaceFull(collision, Direction.DOWN)) {
                    flags |= 8;
                }
                if (Block.isShapeFullBlock(collision)) {
                    flags |= 16;
                }
                if (Block.isShapeFullBlock(state.getShape(level, pos))) {
                    flags |= 32;
                }
                double top = collision.isEmpty() ? 0.0 : collision.max(Direction.Axis.Y);
                List<AABB> boxes = collision.toAabbs();
                boolean flat = boxes.size() == 1
                    && boxes.get(0).minX == 0.0 && boxes.get(0).maxX == 1.0
                    && boxes.get(0).minZ == 0.0 && boxes.get(0).maxZ == 1.0;
 int wall = 0;
                VoxelShape aboveShape = collision.getFaceShape(Direction.DOWN);
                for (int i = 0; i < WALL_TESTS.length; i++) {
                    if (!Shapes.joinIsNotEmpty(WALL_TESTS[i], aboveShape, BooleanOp.ONLY_FIRST)) {
                        wall |= 1 << i;
                    }
                }
                System.out.println("STATE " + state + "|" + full + "|" + center + "|" + rigid
                    + "|" + attach + "|" + flags + "|" + top + "|" + (flat ? 1 : 0) + "|" + wall + "|" + collisionFaces);
            }
        }
    }
}
