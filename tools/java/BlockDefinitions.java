import java.lang.reflect.Field;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.ConcretePowderBlock;

// The block report omits definitions in 26.3; retain the class families and
// concrete targets used by tools/gen blocks.
public class BlockDefinitions {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        Field concrete = ConcretePowderBlock.class.getDeclaredField("concrete");
        concrete.setAccessible(true);
        for (Block block : BuiltInRegistries.BLOCK) {
            Class<?> type = block.getClass();
            while (type.isAnonymousClass()) type = type.getSuperclass();
            String target = block instanceof ConcretePowderBlock
                    ? BuiltInRegistries.BLOCK.getKey((Block) concrete.get(block)).toString() : "-";
            System.out.println("DEFINITION " + BuiltInRegistries.BLOCK.getKey(block) + " " + type.getSimpleName() + " " + target);
        }
    }
}
