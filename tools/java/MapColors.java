import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;

// Prints "stateId mapColorId" for every block state of the vanilla server on
// the classpath. The map colour is the block state's own property, so state
// variants that differ (a bed's foot and head, a crop's age) get their own
// answer. Run by tools/gen mapcolors.
public class MapColors {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                int id = Block.getId(state);
                int color = state.getMapColor(EmptyBlockGetter.INSTANCE, BlockPos.ZERO).id;
                System.out.println(id + " " + color);
            }
        }
    }
}
