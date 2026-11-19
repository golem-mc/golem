import java.nio.file.Files;
import java.nio.file.Path;
import net.minecraft.SharedConstants;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.TagParser;
import net.minecraft.util.datafix.DataFixers;
import net.minecraft.util.datafix.DataFixTypes;

// Prints the STRUCTURE fixer output for each named SNBT fixture.
public class TemplateFixGolden {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            if (line.isBlank() || line.startsWith("#")) continue;
            String[] parts = line.split("\t", 2);
            CompoundTag tag = TagParser.parseCompoundFully(parts[1]);
            CompoundTag fixed = DataFixTypes.STRUCTURE.updateToCurrentVersion(
                    DataFixers.getDataFixer(), tag, tag.getIntOr("DataVersion", 500));
            System.out.println("FIXED " + parts[0] + "\t" + fixed);
        }
    }
}
