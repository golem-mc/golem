import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;

// Prints "name true|false" for every block of the vanilla server on the
// classpath that is not air: BlockState.requiresCorrectToolForDrops, the
// per-block fact that gates a block's drops on the right tool. A block whose
// states disagree is printed as a VARIANT line, which the generator turns
// into a failure. Run by tools/gen blockdrops.
public class BlockToolRequirement {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            if (block.defaultBlockState().isAir()) {
                continue;
            }
            String name = BuiltInRegistries.BLOCK.getKey(block).toString();
            boolean required = block.defaultBlockState().requiresCorrectToolForDrops();
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                if (state.requiresCorrectToolForDrops() != required) {
                    System.out.println("VARIANT " + name);
                }
            }
            System.out.println(name + " " + required);
        }
    }
}
