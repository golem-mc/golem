import java.nio.file.*;
import java.util.*;
import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.core.component.DataComponentInitializers;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.nbt.*;
import net.minecraft.resources.RegistryOps;
import net.minecraft.world.item.ItemStack;

// Records native stack codec children or the component root (key *). Arguments: input TSV, output TSV.
// Columns: name, stack SNBT, component key, rich text, plain text, compact SNBT.
// Input column 4 may be root_mutations to record insertion, replacement, reset and reinsertion.
public class StackComponentHistory {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createReloadableLookup(VanillaRegistries.createWorldLookup());
        BuiltInRegistries.DATA_COMPONENT_INITIALIZERS.build(lookup).forEach(DataComponentInitializers.PendingComponents::apply);
        var ops = RegistryOps.create(NbtOps.INSTANCE, lookup);
        var lines = new ArrayList<String>();
        lines.add("# Vanilla 26.3; tools/java/StackComponentHistory.java, JDK 25");
        for (var line : Files.readAllLines(Path.of(args[0]))) {
            if (line.startsWith("#") || line.isBlank()) continue;
            var fields = line.split("\t");
            var stack = ItemStack.CODEC.parse(ops, TagParser.parseCompoundFully(fields[1])).getOrThrow();
            if (fields.length > 3 && fields[3].equals("wire")) {
                var buffer = io.netty.buffer.Unpooled.buffer();
                var codec = net.minecraft.world.item.component.BlockItemStateProperties.STREAM_CODEC;
                codec.encode(buffer, stack.get(net.minecraft.core.component.DataComponents.BLOCK_STATE));
                stack.set(net.minecraft.core.component.DataComponents.BLOCK_STATE, codec.decode(buffer));
                buffer.release();
            }
            if (fields.length > 3 && fields[3].equals("root_mutations")) {
                var name = fields[0];
                record(lines, fields, name + "_initial", stack, ops);
                stack.set(net.minecraft.core.component.DataComponents.UNBREAKABLE, net.minecraft.util.Unit.INSTANCE);
                record(lines, fields, name + "_insert", stack, ops);
                stack.set(net.minecraft.core.component.DataComponents.DAMAGE, 2);
                record(lines, fields, name + "_replace", stack, ops);
                stack.set(net.minecraft.core.component.DataComponents.DAMAGE, 0);
                record(lines, fields, name + "_removed", stack, ops);
                stack.set(net.minecraft.core.component.DataComponents.DAMAGE, 3);
                record(lines, fields, name + "_reinsert", stack, ops);
            } else {
                record(lines, fields, fields[0], stack, ops);
            }
        }
        Files.write(Path.of(args[1]), lines);
    }

    private static void record(List<String> lines, String[] fields, String name,
                               ItemStack stack, RegistryOps<Tag> ops) {
        var saved = (CompoundTag) ItemStack.CODEC.encodeStart(ops, stack).getOrThrow();
        var input = fields[1];
        if (fields.length > 3 && fields[3].equals("native_input")) {
            input = new TextComponentTagVisitor("", TextComponentTagVisitor.RichStyling.INSTANCE).visit(saved).getString();
        }
        var components = (CompoundTag) saved.get("components");
        var child = fields[2].equals("*") ? components : components.get(fields[2]);
        var rich = new TextComponentTagVisitor("", TextComponentTagVisitor.RichStyling.INSTANCE).visit(child).getString();
        var plain = new TextComponentTagVisitor("", TextComponentTagVisitor.PlainStyling.INSTANCE).visit(child).getString();
        lines.add(String.join("\t", name, input, fields[2], rich, plain, child.toString()));
    }
}
