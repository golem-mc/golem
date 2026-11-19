import java.lang.reflect.Method;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.FireBlock;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.BlockStateProperties;

// Prints "name ignite burn" for every block whose entry in FireBlock's
// flammability table is not zero, the two numbers FireBlock.getIgniteOdds and
// FireBlock.getBurnOdds give a state of it. The odds are set in
// FireBlock.bootStrap's setFlammable calls, so rather than reading source this
// boots the server's registries and asks the block itself, the way
// BlockResistance does for explosion resistance. Run by
// tools/gen flammability.
//
// The getters answer per state, but the table behind them is per block; the
// one state-dependent rule is waterlogged, which zeroes both odds. Any other
// disagreement between a block's states is printed as a VARIANT line, which
// the generator turns into a failure rather than a table it would have to
// guess at.
public class FireFlammability {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        FireBlock fire = (FireBlock) Blocks.FIRE;
        Method ignite = null;
        Method burn = null;
        for (Method m : FireBlock.class.getDeclaredMethods()) {
            if (m.getParameterCount() != 1 || m.getParameterTypes()[0] != BlockState.class) {
                continue;
            }
            if (m.getName().equals("getIgniteOdds")) {
                ignite = m;
            }
            if (m.getName().equals("getBurnOdds")) {
                burn = m;
            }
        }
        if (ignite == null || burn == null) {
            System.out.println("MISSING getIgniteOdds/getBurnOdds on FireBlock; declared methods:");
            for (Method m : FireBlock.class.getDeclaredMethods()) {
                System.out.println("  " + m);
            }
            System.exit(1);
        }
        ignite.setAccessible(true);
        burn.setAccessible(true);

        for (Block block : BuiltInRegistries.BLOCK) {
            BlockState def = block.defaultBlockState();
            int defIgnite = (int) ignite.invoke(fire, def);
            int defBurn = (int) burn.invoke(fire, def);
            if (defIgnite != 0 || defBurn != 0) {
                System.out.println(BuiltInRegistries.BLOCK.getKey(block) + " " + defIgnite + " " + defBurn);
            }
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                if (state == def) {
                    continue;
                }
                int i = (int) ignite.invoke(fire, state);
                int b = (int) burn.invoke(fire, state);
                if (i == defIgnite && b == defBurn) {
                    continue;
                }
                boolean waterlogged = state.hasProperty(BlockStateProperties.WATERLOGGED)
                        && state.getValue(BlockStateProperties.WATERLOGGED);
                if (waterlogged && i == 0 && b == 0) {
                    continue;
                }
                System.out.println("VARIANT " + BuiltInRegistries.BLOCK.getKey(block)
                        + " " + state + " " + i + " " + b);
            }
        }
    }
}
