import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.level.saveddata.maps.MapDecorationType;

// MapDecorationTypes registers the marker tints, including MapColor-backed values.
public class MapDecorations {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (MapDecorationType type : BuiltInRegistries.MAP_DECORATION_TYPE) {
            System.out.println("MAP_DECORATION " + BuiltInRegistries.MAP_DECORATION_TYPE.getKey(type) + " " + (SharedConstants.getCurrentVersion().id().equals("26.3") ? -1 : (int) MapDecorationType.class.getMethod("mapColor").invoke(type)));
        }
    }
}
