import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;

// Prints "name hardness" for every block of the vanilla server on the
// classpath that is not air, the BlockState.getDestroySpeed a dig divides
// by. A block whose states disagree is printed as a VARIANT line, which the
// generator turns into a failure. Run by tools/gen blockdrops.
public class BlockHardness {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            if (block.defaultBlockState().isAir()) {
                continue;
            }
            String name = BuiltInRegistries.BLOCK.getKey(block).toString();
            float hardness = block.defaultBlockState().getDestroySpeed(EmptyBlockGetter.INSTANCE, BlockPos.ZERO);
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                if (state.getDestroySpeed(EmptyBlockGetter.INSTANCE, BlockPos.ZERO) != hardness) {
                    System.out.println("VARIANT " + name);
                }
            }
            System.out.println(name + " " + hardness);
        }
    }
}
