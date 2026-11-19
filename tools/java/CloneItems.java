import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.RegistryAccess;
import net.minecraft.core.component.DataComponentMap;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.item.Item;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.LevelReader;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;

// Prints what vanilla pick block hands back for every block state: "CLONE
// block item props" from BlockState.getCloneItemStack, the same call
// ServerGamePacketListenerImpl.handlePickItemFromBlock makes. The item is
// "minecraft:air" where the state has no item form. Run by
// tools/gen blockitems.
public class CloneItems {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        // A new ItemStack reads the item holder's default components, which
        // the server bakes when it loads its data pack. Baking needs that
        // whole load (the fire resistant initializer alone reads a damage
        // type tag from a data-driven registry), so every holder gets the
        // empty map: the tool only reads which item each block hands back,
        // and the components carry no part of that answer.
        for (Item item : BuiltInRegistries.ITEM) {
            item.builtInRegistryHolder().bindComponents(DataComponentMap.EMPTY);
        }
        RegistryAccess access = RegistryAccess.fromRegistryOfRegistries(BuiltInRegistries.REGISTRY);
        // The stem blocks read the level's registries and the banner reads a
        // block entity; a proxy that answers registry lookups and nothing else
        // stands in for the plain-item case this server hands out.
        LevelReader level = (LevelReader) Proxy.newProxyInstance(CloneItems.class.getClassLoader(),
                new Class<?>[]{LevelReader.class},
                (Object proxy, Method method, Object[] call) -> {
                    if (method.getName().equals("registryAccess")) {
                        return access;
                    }
                    return defaults(method.getReturnType());
                });
        for (Block block : BuiltInRegistries.BLOCK) {
            for (BlockState state : block.getStateDefinition().getPossibleStates()) {
                ItemStack stack = state.getCloneItemStack(level, BlockPos.ZERO, false);
                System.out.println("CLONE " + BuiltInRegistries.BLOCK.getKey(block) + " "
                        + BuiltInRegistries.ITEM.getKey(stack.getItem()) + " " + props(state));
            }
        }
    }

    private static Object defaults(Class<?> type) {
        if (type == boolean.class) {
            return false;
        }
        if (type == int.class) {
            return 0;
        }
        if (type == long.class) {
            return 0L;
        }
        if (type == float.class) {
            return 0.0f;
        }
        if (type == double.class) {
            return 0.0;
        }
        return null;
    }

    private static String props(BlockState state) {
        if (state.getProperties().isEmpty()) {
            return "-";
        }
        List<String> parts = state.getProperties().stream()
                .map(p -> p.getName() + "=" + state.getValue((Property) p)).toList();
        return String.join(",", parts);
    }
}
