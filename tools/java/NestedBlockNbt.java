import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.component.DataComponentInitializers;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.nbt.TagParser;
import net.minecraft.nbt.TextComponentTagVisitor;
import net.minecraft.resources.Identifier;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.ProblemReporter;
import net.minecraft.world.level.block.EntityBlock;
import net.minecraft.world.level.block.piston.PistonMovingBlockEntity;
import net.minecraft.world.level.storage.TagValueInput;

// Records nested block saves from the vanilla server jar. Arguments: input TSV,
// output TSV. Run with the extracted server jar and libraries on the classpath.
// Input columns: block, SNBT, saved field. Extra columns are ignored on re-record.
public class NestedBlockNbt {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createReloadableLookup(VanillaRegistries.createWorldLookup());
        BuiltInRegistries.DATA_COMPONENT_INITIALIZERS.build(lookup).forEach(DataComponentInitializers.PendingComponents::apply);
        var result = new ArrayList<String>();
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            if (line.isBlank() || line.startsWith("#")) continue;
            var parts = line.split("\t");
            var block = BuiltInRegistries.BLOCK.getValue(Identifier.parse(parts[0]));
            var pos = new BlockPos(2, 64, 3);
            var state = block.defaultBlockState();
            var be = ((EntityBlock) block).newBlockEntity(pos, state);
            // MovingPistonBlock.newBlockEntity deliberately returns null; its
            // persisted entity is constructed by the piston resolver instead.
            if (be == null) be = new PistonMovingBlockEntity(pos, state);
            be.loadWithComponents(TagValueInput.create(ProblemReporter.DISCARDING, lookup, TagParser.parseCompoundFully(parts[1])));
            var tag = be.saveWithFullMetadata(lookup).get(parts[2]);
            var rich = new TextComponentTagVisitor("", TextComponentTagVisitor.RichStyling.INSTANCE).visit(tag).getString();
            var plain = new TextComponentTagVisitor("", TextComponentTagVisitor.PlainStyling.INSTANCE).visit(tag).getString();
            result.add(String.join("\t", parts[0], parts[1], parts[2], rich, plain));
        }
        Files.write(Path.of(args[1]), result);
    }
}
