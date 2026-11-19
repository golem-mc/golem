import java.nio.file.*;
import java.util.*;
import net.minecraft.SharedConstants;
import net.minecraft.server.Bootstrap;
import net.minecraft.nbt.*;

// Records Entity's HashSet<String>, List.copyOf(tags) save and load sequence.
// Arguments: input TSV, output TSV. Columns 4-7 are rich, plain, compact and
// rich after loading the saved list into a fresh entity's default HashSet.
public class EntityTagOrder {
    static List<String> keys(String s) {
        return s.isEmpty() ? List.of() : Arrays.asList(s.replace("\\uE000", "\uE000").split(","));
    }
    static ListTag tag(Set<String> tags) {
        var list = new ListTag();
        for (var key : List.copyOf(tags)) list.add(StringTag.valueOf(key));
        return list;
    }
    static String rich(Set<String> tags) {
        return new TextComponentTagVisitor("", TextComponentTagVisitor.RichStyling.INSTANCE).visit(tag(tags)).getString();
    }
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        var lines = new ArrayList<String>();
        lines.add("# Vanilla 26.3 Entity tag order; recorded by tools/java/EntityTagOrder.java on JDK 25.");
        for (var line : Files.readAllLines(Path.of(args[0]))) {
            if (line.startsWith("#") || line.isBlank()) continue;
            var fields = line.split("\t");
            var tags = new HashSet<String>();
            tags.addAll(keys(fields[1]));
            for (var op : fields[2].split(" ")) {
                var value = op.substring(1);
                switch (op.charAt(0)) {
                    case '+' -> tags.add(value);
                    case '-' -> tags.remove(value);
                    case '>' -> {
                        var edited = new ArrayList<String>(tags);
                        edited.add(value);
                        tags.clear(); tags.addAll(edited);
                    }
                    case '=' -> { tags.clear(); tags.addAll(keys(value)); }
                }
            }
            var plain = new TextComponentTagVisitor("", TextComponentTagVisitor.PlainStyling.INSTANCE).visit(tag(tags)).getString();
            var reloaded = new HashSet<String>();
            reloaded.addAll(new ArrayList<String>(tags));
            lines.add(String.join("\t", fields[0], fields[1], fields[2], rich(tags), plain, tag(tags).toString(), rich(reloaded)));
        }
        Files.write(Path.of(args[1]), lines);
    }
}
