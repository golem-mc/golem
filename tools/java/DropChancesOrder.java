import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.entity.DropChances;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.nbt.NbtOps;
import net.minecraft.nbt.Tag;
import net.minecraft.nbt.TextComponentTagVisitor;
import java.util.*;

// Run through vanilla.Ref.RunJava; records JVM-local enum hashes with their codec output.
class DropChancesOrder {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        if (args.length > 0) {
            for (int i = 0; i < Integer.parseInt(args[0]); i++) System.identityHashCode(new Object());
            for (EquipmentSlot slot : EquipmentSlot.values()) System.identityHashCode(slot);
        }
        Bootstrap.bootStrap();
        for (EquipmentSlot slot : EquipmentSlot.values()) {
            System.out.println("hash " + slot.getName() + " " + System.identityHashCode(slot));
        }
        System.out.println("population " + new HashMap<>(DropChances.DEFAULT.byEquipment()).keySet());
        var filter = DropChances.class.getDeclaredMethod("filterDefaultValues", Map.class);
        filter.setAccessible(true);
        for (String names : List.of("mainhand,offhand,feet,legs,chest,head", "mainhand,chest", "mainhand,head", "chest,head", "feet,legs,offhand", "head")) {
            DropChances chances = DropChances.DEFAULT;
            for (String name : names.split(",")) chances = chances.withEquipmentChance(EquipmentSlot.byName(name), 2.0f);
            System.out.println("filtered " + ((Map<?, ?>)filter.invoke(null, chances.byEquipment())).keySet());
            Tag tag = DropChances.CODEC.encodeStart(NbtOps.INSTANCE, chances).getOrThrow();
            TextComponentTagVisitor visitor = new TextComponentTagVisitor("");
            System.out.println("subset " + names + " " + visitor.visit(tag).getString());
            System.out.println("compact " + tag);
        }
    }
}
