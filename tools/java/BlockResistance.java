import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;

// Prints "name resistance" for every block of the vanilla server on the
// classpath, the number Block.getExplosionResistance gives an explosion's
// ray-cast. Run by tools/gen blockresistance.
public class BlockResistance {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            System.out.println(BuiltInRegistries.BLOCK.getKey(block) + " " + block.getExplosionResistance());
        }
    }
}
