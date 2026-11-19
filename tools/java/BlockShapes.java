import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;
import net.minecraft.world.phys.shapes.CollisionContext;
import net.minecraft.world.phys.shapes.VoxelShape;

import java.util.List;

// Prints, for every block state of a vanilla server, the boxes of its
// collision shape as BlockState.getCollisionShape answers it with the empty
// collision context — the read the entity sweep stands on. Entity-dependent
// shapes read as the empty context leaves them. Run by
// tools/gen blockshapes.
//
// One line per state: STATE <state>|<minX>,<minY>,<minZ>,<maxX>,<maxY>,
// <maxZ>|..., the numbers sixteenths of a block.
public class BlockShapes {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        EmptyBlockGetter level = EmptyBlockGetter.INSTANCE;
        BlockPos pos = BlockPos.ZERO;
        CollisionContext context = CollisionContext.empty();
        for (Block block : BuiltInRegistries.BLOCK) {
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                VoxelShape collision = state.getCollisionShape(level, pos, context);
                StringBuilder sb = new StringBuilder("STATE ").append(state);
                for (AABB b : collision.toAabbs()) {
                    sb.append(String.format("|%.9f,%.9f,%.9f,%.9f,%.9f,%.9f",
                        b.minX * 16, b.minY * 16, b.minZ * 16,
                        b.maxX * 16, b.maxY * 16, b.maxZ * 16));
                }
                System.out.println(sb);
            }
        }
    }
}
