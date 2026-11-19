import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.item.Item;
import net.minecraft.world.item.ItemStackTemplate;

// Prints "name remainder" for every item of the vanilla server on the
// classpath that leaves a crafting remainder behind, the plain item stack
// CraftingRecipe.defaultCraftingReminder sets back into the ingredient's
// grid cell. A remainder whose template carries more than one item or
// non-empty components is printed as a REMAINDER line, which the
// generator turns into a failure. Run by tools/gen craftingremainders.
public class CraftingRemainders {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Item item : BuiltInRegistries.ITEM) {
            ItemStackTemplate remainder = item.getCraftingRemainder();
            if (remainder == null) {
                continue;
            }
            String name = BuiltInRegistries.ITEM.getKey(item).toString();
            if (remainder.count() != 1 || !remainder.components().isEmpty()) {
                System.out.println("REMAINDER " + name);
                continue;
            }
            System.out.println(name + " " + BuiltInRegistries.ITEM.getKey(remainder.item().value()).toString());
        }
    }
}
