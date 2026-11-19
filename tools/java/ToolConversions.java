import java.lang.reflect.Field;
import java.util.Map;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.item.AxeItem;
import net.minecraft.world.item.HoneycombItem;
import net.minecraft.world.item.ShovelItem;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.WeatheringCopper;
import net.minecraft.world.level.block.state.BlockState;

// Prints the block conversions a tool's right-click makes: the three
// AxeItem.evaluateNewBlockState tries, in its order — "STRIP from to" from
// AxeItem.STRIPPABLES, "SCRAPE from to" from WeatheringCopper.PREVIOUS_BY_BLOCK
// and "WAXOFF from to" from HoneycombItem.WAX_OFF_BY_BLOCK — and "FLATTEN
// from to" from ShovelItem.FLATTENABLES. Run by tools/gen toolconversions.
//
// The stripped block takes only the axis of the old state; the copper ones
// take every property (withPropertiesOf). A pair whose two blocks do not have
// the same properties is printed as a MISMATCH line, which the generator
// refuses, since the Go side copies the property map whole. A flattened block
// becomes a fixed state, which must be its block's default.
public class ToolConversions {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        Field strippables = AxeItem.class.getDeclaredField("STRIPPABLES");
        strippables.setAccessible(true);
        @SuppressWarnings("unchecked")
        Map<Block, Block> strip = (Map<Block, Block>) strippables.get(null);
        print("STRIP", strip);
        print("SCRAPE", WeatheringCopper.PREVIOUS_BY_BLOCK.get());
        print("WAXOFF", HoneycombItem.WAX_OFF_BY_BLOCK.get());
        Field flattenables = ShovelItem.class.getDeclaredField("FLATTENABLES");
        flattenables.setAccessible(true);
        @SuppressWarnings("unchecked")
        Map<Block, BlockState> flatten = (Map<Block, BlockState>) flattenables.get(null);
        for (Map.Entry<Block, BlockState> e : flatten.entrySet()) {
            BlockState to = e.getValue();
            String line = "FLATTEN " + BuiltInRegistries.BLOCK.getKey(e.getKey()) + " "
                    + BuiltInRegistries.BLOCK.getKey(to.getBlock());
            if (to != to.getBlock().defaultBlockState()) {
                System.out.println("MISMATCH " + line);
            }
            System.out.println(line);
        }
    }

    private static void print(String kind, Map<Block, Block> pairs) {
        for (Map.Entry<Block, Block> e : pairs.entrySet()) {
            String from = BuiltInRegistries.BLOCK.getKey(e.getKey()).toString();
            String to = BuiltInRegistries.BLOCK.getKey(e.getValue()).toString();
            if (!e.getKey().getStateDefinition().getProperties()
                    .equals(e.getValue().getStateDefinition().getProperties())) {
                System.out.println("MISMATCH " + kind + " " + from + " " + to);
            }
            System.out.println(kind + " " + from + " " + to);
        }
    }
}
