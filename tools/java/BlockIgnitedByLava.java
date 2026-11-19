import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockBehaviour;
import net.minecraft.world.level.block.state.BlockState;

// Prints the name of every block whose default state carries vanilla's
// ignitedByLava property, the flag LavaFluid's random tick reads when it
// decides which blocks a fire it lights may stand beside. The flag is set on
// the block properties the vanilla blocks are built with, so rather than
// reading source this boots the server's registries and asks every block's
// default state, the way FireFlammability does for the flammability table.
// Run by tools/gen ignitedbylava.
//
// The flag is a per-block property, so the default state's answer is every
// state's — there is no waterlogged rule to it, and no VARIANT line to watch
// for.
public class BlockIgnitedByLava {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            BlockState def = block.defaultBlockState();
            if (((BlockBehaviour.BlockStateBase) def).ignitedByLava()) {
                System.out.println(BuiltInRegistries.BLOCK.getKey(block));
            }
        }
    }
}
