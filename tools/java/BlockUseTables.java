import java.lang.reflect.Field;
import java.util.Map;
import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.item.BlockItem;
import net.minecraft.world.item.Item;
import net.minecraft.world.level.ItemLike;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.ComposterBlock;
import net.minecraft.world.level.block.FlowerPotBlock;

// Prints the tables the blocks' own item uses read: "POT item potted" for
// every block item FlowerPotBlock.useItemOn pots (POTTED_BY_CONTENT of the
// item's block), "UNPOT potted item" for what useWithoutItem hands back, and
// "COMPOST item chance" from ComposterBlock.COMPOSTABLES. Run by
// tools/gen blockuse.
public class BlockUseTables {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        Field f = FlowerPotBlock.class.getDeclaredField("POTTED_BY_CONTENT");
        f.setAccessible(true);
        @SuppressWarnings("unchecked")
        Map<Block, Block> potted = (Map<Block, Block>) f.get(null);
        for (Item item : BuiltInRegistries.ITEM) {
            if (item instanceof BlockItem blockItem) {
                Block pot = potted.get(blockItem.getBlock());
                if (pot != null && pot != net.minecraft.world.level.block.Blocks.AIR) {
                    System.out.println("POT " + BuiltInRegistries.ITEM.getKey(item) + " " + BuiltInRegistries.BLOCK.getKey(pot));
                }
            }
        }
        for (Block block : BuiltInRegistries.BLOCK) {
            if (block instanceof FlowerPotBlock pot && pot.getPotted() != net.minecraft.world.level.block.Blocks.AIR) {
                System.out.println("UNPOT " + BuiltInRegistries.BLOCK.getKey(block) + " "
                        + BuiltInRegistries.ITEM.getKey(pot.getPotted().asItem()));
            }
        }
        // 26.3's compostable components are read from the item reports by Go.
        if (args.length == 0) {
            Field compostables = ComposterBlock.class.getDeclaredField("COMPOSTABLES");
            @SuppressWarnings("unchecked")
            Map<ItemLike, Float> chances = (Map<ItemLike, Float>) compostables.get(null);
            for (var e : chances.entrySet()) {
                System.out.println("COMPOST " + BuiltInRegistries.ITEM.getKey(e.getKey().asItem()) + " " + e.getValue());
            }
        }
    }
}
