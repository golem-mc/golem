import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.block.Block;

// Prints "name instrument" for every block of the vanilla server on the
// classpath, the NoteBlockInstrument its BlockBehaviour.Properties name.
// The instrument is a property of the block, not of the state, so the
// default state answers for all of them. Run by
// tools/gen noteblockinstruments.
public class NoteBlockInstruments {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Block block : BuiltInRegistries.BLOCK) {
            System.out.println(BuiltInRegistries.BLOCK.getKey(block) + " "
                    + block.defaultBlockState().instrument().getSerializedName());
        }
    }
}
