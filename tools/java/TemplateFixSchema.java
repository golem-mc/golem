import com.mojang.datafixers.schemas.Schema;
import com.mojang.datafixers.types.templates.*;
import java.lang.reflect.Field;
import java.util.*;
import net.minecraft.SharedConstants;
import net.minecraft.util.datafix.DataFixers;
import net.minecraft.util.datafix.fixes.StonecutterRecipeRenameFix;

// Emits only typed edges reachable from STRUCTURE; remainder/custom data is opaque.
public class TemplateFixSchema {
    static Schema schema;
    static Map<Integer, String> recursive;
    static Set<String> pending, seen;
    static int version;

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        int current = SharedConstants.getCurrentVersion().dataVersion().version();
        if (current != 4903 && current != 5023) {
            throw new IllegalStateException("Review the structure fixer chain for this data version");
        }
        // DataFixUtils keys include the subversion: the two pot fixes have
        // different input schemas and both must run for a 4995 template.
        int[] stages = current == 4903
                ? new int[]{45350, 45410, 46490, 47630, 47690, 48850, 48880}
                : new int[]{45350, 45410, 46490, 47630, 47690, 48850, 48880,
                    49960, 49961, 50020, 50060, 50070, 50080, 50120, 50140, 50160};
        for (int v : stages) {
            version = v;
            schema = DataFixers.getDataFixer().getSchema(v - 1);
            recursive = new HashMap<>();
            for (String name : schema.types()) {
                if (schema.id(name) instanceof RecursivePoint p) recursive.put(p.index(), name);
            }
            pending = new TreeSet<>(Set.of("structure"));
            seen = new HashSet<>();
            while (!pending.isEmpty()) {
                String name = pending.iterator().next();
                pending.remove(name);
                if (!seen.add(name)) continue;
                TypeTemplate t = schema.resolveTemplate(name);
                if (t instanceof Named n && n.name().equals(name)) t = n.element();
                System.out.println("TYPE " + version + " " + name);
                walk(name, t, "");
            }
        }
        new TreeMap<>(StonecutterRecipeRenameFix.RECIPE_RENAMES).forEach((a, b) ->
                System.out.println("RECIPE " + a + " " + b));
    }

    static void edge(String owner, String path, String target) {
        System.out.println("EDGE " + version + " " + owner + " " + (path.isEmpty() ? "." : path) + " " + target);
        if (!seen.contains(target)) pending.add(target);
    }

    static Object field(Object object, String name) throws Exception {
        Field f = object.getClass().getDeclaredField(name);
        f.setAccessible(true);
        return f.get(object);
    }

    static void walk(String owner, TypeTemplate t, String path) throws Exception {
        switch (t) {
            case Named n -> edge(owner, path, n.name());
            case RecursivePoint p -> edge(owner, path, Objects.requireNonNull(recursive.get(p.index())));
            case Tag tag -> walk(owner, tag.element(), path + "/" + tag.name());
            case com.mojang.datafixers.types.templates.List list -> walk(owner, list.element(), path + "/*");
            case Product p -> { walk(owner, p.f(), path); walk(owner, p.g(), path); }
            case Sum s -> { walk(owner, s.f(), path); walk(owner, s.g(), path); }
            case Hook h -> {
                // vanilla: V3818_5.UNPACK_PLAIN_ID; reject any other hook.
                if (!owner.equals("item_stack") || !path.isEmpty()
                        || !field(h, "preRead").getClass().getName().equals("net.minecraft.util.datafix.schemas.V3818_5$1")
                        || field(h, "postWrite") != Hook.HookFunction.IDENTITY) {
                    throw new IllegalStateException("Unmodelled hook: " + h);
                }
                System.out.println("HOOK " + version + " plain_item_id");
                walk(owner, (TypeTemplate) field(h, "element"), path);
            }
            case CompoundList c -> { walk(owner, c.key(), path + "/#"); walk(owner, c.element(), path + "/@"); }
            case TaggedChoice<?> choice -> {
                String key = (String) field(choice, "name");
                Map<?, ?> choices = (Map<?, ?>) field(choice, "templates");
                for (String id : choices.keySet().stream().map(Object::toString).sorted().toList()) {
                    walk(owner, (TypeTemplate) choices.get(id), path + "/?" + key + "=" + id);
                }
            }
            case Const c -> { }
            default -> throw new IllegalStateException("Unmodelled template: " + t);
        }
    }
}
