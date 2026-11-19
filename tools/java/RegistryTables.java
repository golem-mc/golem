import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.component.DataComponentMap;
import net.minecraft.core.particles.SimpleParticleType;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.server.Bootstrap;
import net.minecraft.util.TriState;
import net.minecraft.world.attribute.AttributeTypes;
import net.minecraft.world.attribute.EnvironmentAttribute;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.entity.ai.attributes.RangedAttribute;
import net.minecraft.world.item.BlockItem;
import net.minecraft.world.item.DyeColor;
import net.minecraft.world.level.EmptyBlockGetter;
import net.minecraft.world.level.block.ShulkerBoxBlock;
import net.minecraft.world.level.block.entity.BeaconBlockEntity;
import net.minecraft.world.level.block.entity.BlockEntity;
import net.minecraft.world.level.block.piston.PistonBaseBlock;

// Runtime-only registry data; reports/data-pack JSON supply the other tables.
public class RegistryTables {
    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        Bootstrap.bootStrap();

        for (var item : BuiltInRegistries.ITEM) {
            item.builtInRegistryHolder().bindComponents(DataComponentMap.EMPTY);
        }

        for (var attribute : BuiltInRegistries.ATTRIBUTE) {
            if (!(attribute instanceof RangedAttribute ranged)) {
                throw new IllegalStateException("Non-ranged attribute: " + attribute);
            }
            System.out.println("TABLE ATTRIBUTE " + BuiltInRegistries.ATTRIBUTE.getKey(attribute)
                    + " " + BuiltInRegistries.ATTRIBUTE.getId(attribute)
                    + " " + attribute.getDefaultValue() + " " + ranged.getMinValue()
                    + " " + ranged.getMaxValue() + " " + attribute.isClientSyncable());
        }

        // A float-valued attribute's range is read back through sanitizeValue.
        for (EnvironmentAttribute<?> attribute : BuiltInRegistries.ENVIRONMENT_ATTRIBUTE) {
            var type = attribute.type();
            String values = "- - -";
            if (type == AttributeTypes.BOOLEAN) {
                values = ((Boolean) attribute.defaultValue() ? "1" : "0") + " 0 1";
            } else if (type == AttributeTypes.TRI_STATE) {
                if (attribute.defaultValue() != TriState.DEFAULT) {
                    throw new IllegalStateException("Unsupported tri-state default: " + attribute);
                }
                values = "-1 -1 1";
            }
            if (type.toFloat() != null) {
                if (type != AttributeTypes.FLOAT && type != AttributeTypes.ANGLE_DEGREES) {
                    throw new IllegalStateException("Float-convertible attribute of type " + type + ": " + attribute);
                }
                @SuppressWarnings("unchecked")
                var floatAttribute = (EnvironmentAttribute<Float>) attribute;
                values = floatAttribute.defaultValue() + " " + floatAttribute.sanitizeValue(Float.NEGATIVE_INFINITY)
                        + " " + floatAttribute.sanitizeValue(Float.POSITIVE_INFINITY);
            }
            System.out.println("TABLE ENVATTR " + attribute + " " + type + " " + (type.toFloat() != null)
                    + " " + (type.toInt() != null) + " " + attribute.isPositional() + " " + values);
        }

        for (var effect : BuiltInRegistries.MOB_EFFECT) {
            var holder = BuiltInRegistries.MOB_EFFECT.wrapAsHolder(effect);
            var options = effect.createParticleOptions(new MobEffectInstance(holder, 20));
            var particle = BuiltInRegistries.PARTICLE_TYPE.getKey(options.getType()).toString();
            System.out.println("TABLE EFFECT " + BuiltInRegistries.MOB_EFFECT.getKey(effect)
                    + " " + effect.getColor() + " " + effect.isInstantaneous() + " " + particle);
        }

        for (var row : BeaconBlockEntity.BEACON_EFFECTS) {
            var effects = row.stream()
                    .map(holder -> BuiltInRegistries.MOB_EFFECT.getKey(holder.value()).toString())
                    .toList();
            System.out.println("TABLE BEACON " + String.join(" ", effects));
        }

        for (var dye : DyeColor.values()) {
            System.out.println("TABLE DYE " + dye.getId() + " " + dye.getName()
                    + " " + dye.getTextureDiffuseColor() + " " + dye.getFireworkColor());
        }
        for (var component : BuiltInRegistries.DATA_COMPONENT_TYPE) {
            System.out.println("TABLE COMPONENT " + BuiltInRegistries.DATA_COMPONENT_TYPE.getKey(component)
                    + " " + component.isTransient());
        }
        for (var particle : BuiltInRegistries.PARTICLE_TYPE) {
            if (!(particle instanceof SimpleParticleType)) {
                System.out.println("TABLE PARTICLEDATA " + BuiltInRegistries.PARTICLE_TYPE.getKey(particle));
            }
        }

        for (var item : BuiltInRegistries.ITEM) {
            if (item instanceof BlockItem blockItem) {
                var itemName = BuiltInRegistries.ITEM.getKey(item);
                var blockName = BuiltInRegistries.BLOCK.getKey(blockItem.getBlock());
                if (!itemName.equals(blockName)) {
                    System.out.println("TABLE ITEMBLOCK " + itemName + " " + blockName);
                }
            }
        }

        for (var block : BuiltInRegistries.BLOCK) {
            var name = BuiltInRegistries.BLOCK.getKey(block);
            var state = block.defaultBlockState();
            boolean conductor = state.isRedstoneConductor(EmptyBlockGetter.INSTANCE, BlockPos.ZERO)
                    && !(block instanceof ShulkerBoxBlock);
            System.out.println("TABLE REDSTONE " + name + " " + conductor + " " + state.isSignalSource());

            var reaction = block instanceof PistonBaseBlock ? "PUSH_PULL" : state.getPistonPushReaction();
            boolean unbreakable = state.getDestroySpeed(EmptyBlockGetter.INSTANCE, BlockPos.ZERO) < 0;
            System.out.println("TABLE PISTON " + name + " " + reaction
                    + " " + unbreakable + " " + state.hasBlockEntity());

            for (var type : BuiltInRegistries.BLOCK_ENTITY_TYPE) {
                if (type.isValid(state)) {
                    var key = BuiltInRegistries.BLOCK_ENTITY_TYPE.getKey(type);
                    var entity = type.create(BlockPos.ZERO, state);
                    if (entity == null) {
                        throw new IllegalStateException("No block entity: " + name);
                    }
                    boolean packet = false;
                    boolean tag = false;
                    for (var method : entity.getClass().getMethods()) {
                        if (method.getDeclaringClass() != BlockEntity.class) {
                            if (method.getName().equals("getUpdatePacket")) {
                                packet = true;
                            }
                            if (method.getName().equals("getUpdateTag")) {
                                tag = true;
                            }
                        }
                    }
                    System.out.println("TABLE BLOCKENTITY " + name + " " + key + " " + packet + " " + tag);
                }
            }
        }
    }
}
