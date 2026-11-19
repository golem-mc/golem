import java.io.FileOutputStream;
import java.io.IOException;
import java.io.OutputStreamWriter;
import java.io.Writer;
import java.nio.charset.StandardCharsets;
import java.util.zip.GZIPOutputStream;

// Writes the gzipped table Character.codePointOf answers from, the lookup
// vanilla's SNBT reader resolves a \N{name} escape with. A code point with a
// name of its own gives an "N <hex> <name>" line. One without gives its
// getName fallback, "<block> <hex>", which codePointOf accepts too, so a run
// of such code points inside one block is a "B <first> <last> <block>" line.
// Every name is checked to read back to its code point. Run by
// `mise run gen:unicodenames`.
public class UnicodeNames {
    public static void main(String[] args) throws IOException {
        try (Writer out = new OutputStreamWriter(new GZIPOutputStream(new FileOutputStream(args[0])), StandardCharsets.US_ASCII)) {
            int runStart = -1;
            int runEnd = -1;
            String runBlock = null;
            for (int cp = 0; cp <= Character.MAX_CODE_POINT + 1; cp++) {
                String name = cp > Character.MAX_CODE_POINT ? null : Character.getName(cp);
                String block = null;
                if (name != null) {
                    Character.UnicodeBlock b = Character.UnicodeBlock.of(cp);
                    String fallback = b == null ? null : b.toString().replace('_', ' ') + " " + Integer.toHexString(cp).toUpperCase();
                    if (name.equals(fallback)) {
                        block = b.toString().replace('_', ' ');
                    } else if (Character.codePointOf(name) != cp) {
                        throw new IllegalStateException(name);
                    } else {
                        out.write("N " + Integer.toHexString(cp) + " " + name + "\n");
                    }
                }
                if (block != null && block.equals(runBlock) && runEnd == cp - 1) {
                    runEnd = cp;
                    continue;
                }
                if (runBlock != null) {
                    out.write("B " + Integer.toHexString(runStart) + " " + Integer.toHexString(runEnd) + " " + runBlock + "\n");
                }
                runBlock = block;
                runStart = cp;
                runEnd = cp;
            }
        }
    }
}
