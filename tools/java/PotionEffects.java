import net.minecraft.SharedConstants;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.item.alchemy.Potion;

// Prints "POTION id name count" for every potion of the vanilla registry,
// each effect on its own "EFFECT effect amplifier duration ambient
// show-particles show-icon" line. The potion registry is built in the
// server's code, in no report or data pack file. Run by tools/gen potions.
public class PotionEffects {
    public static void main(String[] args) {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();
        for (Potion potion : BuiltInRegistries.POTION) {
            System.out.println("POTION " + BuiltInRegistries.POTION.getId(potion)
                    + " " + BuiltInRegistries.POTION.getKey(potion)
                    + " " + potion.getEffects().size());
            for (MobEffectInstance effect : potion.getEffects()) {
                System.out.println("EFFECT " + BuiltInRegistries.MOB_EFFECT.getKey(effect.getEffect().value())
                        + " " + effect.getAmplifier() + " " + effect.getDuration()
                        + " " + effect.isAmbient() + " " + effect.isVisible() + " " + effect.showIcon());
            }
        }
    }
}
