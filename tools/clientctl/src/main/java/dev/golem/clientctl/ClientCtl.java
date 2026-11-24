package dev.golem.clientctl;

import com.mojang.blaze3d.pipeline.RenderTarget;
import com.mojang.blaze3d.platform.NativeImage;
import java.io.File;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;
import net.minecraft.client.Minecraft;
import net.minecraft.client.gui.Hud;
import net.minecraft.client.gui.components.ChatComponent;
import net.minecraft.client.gui.screens.ConnectScreen;
import net.minecraft.client.gui.screens.DeathScreen;
import net.minecraft.client.gui.screens.DisconnectedScreen;
import net.minecraft.client.gui.screens.Screen;
import net.minecraft.client.gui.screens.TitleScreen;
import net.minecraft.client.gui.screens.inventory.AbstractContainerScreen;
import net.minecraft.client.gui.screens.inventory.InventoryScreen;
import net.minecraft.client.gui.screens.multiplayer.JoinMultiplayerScreen;
import net.minecraft.client.multiplayer.ServerData;
import net.minecraft.client.multiplayer.chat.GuiMessage;
import net.minecraft.client.multiplayer.resolver.ServerAddress;
import net.minecraft.client.player.ClientInput;
import net.minecraft.client.player.LocalPlayer;
import net.minecraft.client.sounds.SoundEventListener;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.core.component.DataComponents;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.chat.Component;
import net.minecraft.network.protocol.game.ServerboundSetCarriedItemPacket;
import net.minecraft.resources.Identifier;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.InteractionResult;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.entity.player.Input;
import net.minecraft.world.entity.player.Inventory;
import net.minecraft.world.inventory.AbstractContainerMenu;
import net.minecraft.world.inventory.ContainerInput;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.item.component.SwingAnimation;
import net.minecraft.world.item.enchantment.Enchantment;
import net.minecraft.world.item.enchantment.ItemEnchantments;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.BlockHitResult;
import net.minecraft.world.phys.EntityHitResult;
import net.minecraft.world.phys.HitResult;
import net.minecraft.world.phys.Vec2;

/**
 * Everything the agent answers with, read off and done to the client's own
 * objects. Every method here runs on the render thread - the agent queues it
 * there - so the field reads need no locking beyond what the game already
 * gives its own thread.
 */
final class ClientCtl {

    private ClientCtl() {
    }

    /**
     * The held-input request still running, if any. Only ever touched on the
     * render thread - writes arrive through the queued task - so there is
     * nothing to guard.
     */
    private static HeldInput held;

    // ---------------------------------------------------------------- state

    /** Collects everything /state answers with. entityRange is in blocks. */
    static Map<String, Object> state(Minecraft mc, double entityRange) {
        Map<String, Object> out = new LinkedHashMap<>();
        out.put("agent", "clientctl");
        LocalPlayer p = mc.player;
        boolean inWorld = p != null && mc.level != null && mc.getConnection() != null;
        out.put("connected", inWorld);
        out.put("ticks", inWorld ? p.tickCount : null);
        out.put("screen", screenState(mc));
        if (p != null) {
            out.put("player", playerState(mc, p));
            out.put("inventory", inventoryState(p));
            out.put("container", containerState(p));
        } else {
            out.put("player", null);
            out.put("inventory", null);
            out.put("container", null);
        }
        out.put("target", targetState(mc));
        out.put("chat", chatState(mc));
        out.put("sounds", soundsState(mc));
        out.put("entities", inWorld ? entitiesState(mc, entityRange) : List.of());
        return out;
    }

    private static Map<String, Object> screenState(Minecraft mc) {
        Map<String, Object> out = new LinkedHashMap<>();
        Screen screen = mc.gui.screen();
        if (screen == null) {
            out.put("class", null);
            out.put("title", null);
            out.put("kind", "none");
            return out;
        }
        String className = screen.getClass().getSimpleName();
        out.put("class", className);
        out.put("title", screen.getTitle() == null ? null : screen.getTitle().getString());
        String kind = "screen";
        if (screen instanceof DeathScreen) {
            kind = "death";
        } else if (screen instanceof DisconnectedScreen) {
            kind = "disconnected";
            // The disconnect reason lives in the screen's DisconnectionDetails,
            // which the class does not expose; reflection is what the agent is
            // for.
            DisconnectedDetails details = DisconnectedDetails.of(screen);
            out.put("reason", details == null ? null : details.reason());
        } else if (screen instanceof InventoryScreen) {
            kind = "inventory";
        } else if (screen instanceof AbstractContainerScreen<?>) {
            kind = "container";
            LocalPlayer p = mc.player;
            if (p != null && p.containerMenu != null) {
                out.put("menu", menuTypeName(p.containerMenu));
            }
        }
        out.put("kind", kind);
        return out;
    }

    private static Map<String, Object> playerState(Minecraft mc, LocalPlayer p) {
        Map<String, Object> out = new LinkedHashMap<>();
        out.put("x", p.getX());
        out.put("y", p.getY());
        out.put("z", p.getZ());
        out.put("yaw", p.getYRot());
        out.put("pitch", p.getXRot());
        out.put("on_ground", p.onGround());
        out.put("health", p.getHealth());
        out.put("max_health", p.getMaxHealth());
        out.put("food", p.getFoodData().getFoodLevel());
        out.put("saturation", p.getFoodData().getSaturationLevel());
        out.put("air", p.getAirSupply());
        out.put("on_fire", p.isOnFire());
        out.put("game_mode", mc.gameMode.getPlayerMode().getName());
        out.put("selected_slot", p.getInventory().getSelectedSlot());
        return out;
    }

    private static Map<String, Object> inventoryState(LocalPlayer p) {
        Map<String, Object> out = new LinkedHashMap<>();
        Inventory inventory = p.getInventory();
        Map<String, Object> slots = new LinkedHashMap<>();
        for (int i = 0; i < inventory.getNonEquipmentItems().size(); i++) {
            Map<String, Object> item = itemState(inventory.getNonEquipmentItems().get(i));
            if (item != null) {
                slots.put(String.valueOf(i), item);
            }
        }
        out.put("slots", slots);
        Map<String, Object> equipment = new LinkedHashMap<>();
        for (EquipmentSlot slot : List.of(EquipmentSlot.HEAD, EquipmentSlot.CHEST, EquipmentSlot.LEGS,
                EquipmentSlot.FEET, EquipmentSlot.OFFHAND)) {
            Map<String, Object> item = itemState(p.getItemBySlot(slot));
            if (item != null) {
                equipment.put(slot.getName(), item);
            }
        }
        out.put("equipment", equipment);
        return out;
    }

    /**
     * One item stack as JSON: the item's registry name, the count and the
     * components worth reading back - the wear, the enchantments, the custom
     * name, the unbreakable flag, the repair cost and the glint override.
     * An empty stack reads as null, which is what an empty slot is.
     */
    static Map<String, Object> itemState(ItemStack stack) {
        if (stack == null || stack.isEmpty()) {
            return null;
        }
        Map<String, Object> out = new LinkedHashMap<>();
        Identifier id = BuiltInRegistries.ITEM.getKey(stack.getItem());
        out.put("id", id == null ? String.valueOf(stack.getItem()) : id.toString());
        out.put("count", stack.getCount());
        Map<String, Object> components = new LinkedHashMap<>();
        Integer damage = stack.get(DataComponents.DAMAGE);
        if (damage != null && damage > 0) {
            components.put("damage", damage);
        }
        ItemEnchantments enchantments = stack.get(DataComponents.ENCHANTMENTS);
        if (enchantments != null && !enchantments.isEmpty()) {
            Map<String, Object> levels = new LinkedHashMap<>();
            for (var entry : enchantments.entrySet()) {
                levels.put(enchantmentName(entry.getKey()), entry.getIntValue());
            }
            components.put("enchantments", levels);
        }        Component customName = stack.get(DataComponents.CUSTOM_NAME);
        if (customName != null) {
            components.put("custom_name", customName.getString());
        }
        if (stack.get(DataComponents.UNBREAKABLE) != null) {
            components.put("unbreakable", true);
        }
        Integer repairCost = stack.get(DataComponents.REPAIR_COST);
        if (repairCost != null && repairCost > 0) {
            components.put("repair_cost", repairCost);
        }
        Boolean glint = stack.get(DataComponents.ENCHANTMENT_GLINT_OVERRIDE);
        if (Boolean.TRUE.equals(glint)) {
            components.put("glint", true);
        }
        if (!components.isEmpty()) {
            out.put("components", components);
        }
        return out;
    }

    /**
     * An enchantment's registry name, read off the holder's key. The
     * enchantment registry is data-driven on the client, so the key is the
     * only name there is; a holder without one falls back to the holder's
     * own string, which should not happen for a vanilla enchantment.
     */
    private static String enchantmentName(Holder<Enchantment> holder) {
        return holder.unwrapKey()
                .map(key -> key.identifier().toString())
                .orElseGet(() -> String.valueOf(holder.value()));
    }

    /**
     * The menu's registry name. The player's own InventoryMenu carries no
     * MenuType - it is never opened by type - and getType() refuses it, so
     * that reads as null.
     */
    private static String menuTypeName(AbstractContainerMenu menu) {
        try {
            Identifier type = BuiltInRegistries.MENU.getKey(menu.getType());
            return type == null ? String.valueOf(menu.getType()) : type.toString();
        } catch (UnsupportedOperationException e) {
            return null;
        }
    }

    private static Map<String, Object> containerState(LocalPlayer p) {
        AbstractContainerMenu menu = p.containerMenu;
        if (menu == null) {
            return null;
        }
        Map<String, Object> out = new LinkedHashMap<>();
        boolean ownInventory = menu == p.inventoryMenu;
        out.put("open", !ownInventory);
        out.put("kind", menuTypeName(menu));
        out.put("container_id", menu.containerId);
        List<Object> slots = new ArrayList<>();
        var items = menu.getItems();
        for (int i = 0; i < items.size(); i++) {
            Map<String, Object> slot = new LinkedHashMap<>();
            slot.put("slot", i);
            slot.put("item", itemState(items.get(i)));
            slots.add(slot);
        }
        out.put("slots", slots);
        out.put("carried", itemState(menu.getCarried()));
        return out;
    }

    private static Map<String, Object> targetState(Minecraft mc) {
        HitResult hit = mc.hitResult;
        Map<String, Object> out = new LinkedHashMap<>();
        if (hit == null) {
            out.put("type", "none");
            return out;
        }
        out.put("type", hit.getType().name().toLowerCase());
        if (hit.getType() == HitResult.Type.BLOCK && hit instanceof BlockHitResult blockHit && mc.level != null) {
            BlockPos pos = blockHit.getBlockPos();
            out.put("x", pos.getX());
            out.put("y", pos.getY());
            out.put("z", pos.getZ());
            out.put("face", blockHit.getDirection().getName());
            Identifier block = BuiltInRegistries.BLOCK.getKey(mc.level.getBlockState(pos).getBlock());
            out.put("block", block == null ? null : block.toString());
        } else if (hit.getType() == HitResult.Type.ENTITY && hit instanceof EntityHitResult entityHit) {
            Entity entity = entityHit.getEntity();
            out.put("entity_id", entity.getId());
            Identifier type = BuiltInRegistries.ENTITY_TYPE.getKey(entity.getType());
            out.put("entity_type", type == null ? String.valueOf(entity.getType()) : type.toString());
        }
        out.put("distance", mc.player == null ? null : hit.distanceTo(mc.player));
        return out;
    }

    @SuppressWarnings("unchecked")
    private static List<Object> chatState(Minecraft mc) {
        Hud hud = mc.gui.hud;
        ChatComponent chat = Reflect.get(hud, "chat", ChatComponent.class);
        if (chat == null) {
            return List.of();
        }
        List<GuiMessage> messages = Reflect.get(chat, "allMessages", List.class);
        if (messages == null) {
            return List.of();
        }
        List<Object> out = new ArrayList<>();
        int from = Math.max(0, messages.size() - 100);
        for (int i = from; i < messages.size(); i++) {
            GuiMessage message = messages.get(i);
            Map<String, Object> line = new LinkedHashMap<>();
            line.put("time", message.addedTime());
            line.put("text", message.content() == null ? null : message.content().getString());
            out.add(line);
        }
        return out;
    }

    /** The last sounds the client played, oldest first; filled once the first state call registers the listener. */
    private static final ArrayDeque<Map<String, Object>> sounds = new ArrayDeque<>();
    private static boolean listening;

    // Only fires while the sound engine runs, which needs an OpenAL device: lab.sh gives
    // the client OpenAL's null output.
    private static List<Object> soundsState(Minecraft mc) {
        synchronized (sounds) {
            if (!listening) {
                listening = true;
                SoundEventListener listener = (instance, event, range) -> {
                    Map<String, Object> entry = new LinkedHashMap<>();
                    entry.put("time", mc.level == null ? null : mc.level.getGameTime());
                    entry.put("id", instance.getIdentifier().toString());
                    entry.put("source", instance.getSource().getName());
                    entry.put("x", instance.getX());
                    entry.put("y", instance.getY());
                    entry.put("z", instance.getZ());
                    entry.put("volume", instance.getVolume());
                    entry.put("pitch", instance.getPitch());
                    synchronized (sounds) {
                        if (sounds.size() == 100) {
                            sounds.removeFirst();
                        }
                        sounds.addLast(entry);
                    }
                };
                mc.getSoundManager().addListener(listener);
            }
            return new ArrayList<>(sounds);
        }
    }

    private static List<Object> entitiesState(Minecraft mc, double range) {
        LocalPlayer p = mc.player;
        List<Object> out = new ArrayList<>();
        for (Entity entity : mc.level.entitiesForRendering()) {
            if (entity == p || entity.distanceTo(p) > range) {
                continue;
            }
            Map<String, Object> entry = new LinkedHashMap<>();
            entry.put("id", entity.getId());
            Identifier type = BuiltInRegistries.ENTITY_TYPE.getKey(entity.getType());
            entry.put("type", type == null ? String.valueOf(entity.getType()) : type.toString());
            entry.put("x", entity.getX());
            entry.put("y", entity.getY());
            entry.put("z", entity.getZ());
            entry.put("on_fire", entity.isOnFire());
            Component name = entity.getCustomName();
            entry.put("custom_name", name == null ? null : name.getString());
            if (entity instanceof LivingEntity living) {
                entry.put("health", living.getHealth());
            }
            if (entity instanceof ItemEntity itemEntity) {
                entry.put("item", itemState(itemEntity.getItem()));
            }
            out.add(entry);
        }
        return out;
    }

    // -------------------------------------------------------------- actions

    /** Sends a chat line, or a command when the text begins with a slash. */
    static Map<String, Object> chat(Minecraft mc, String text) {
        LocalPlayer p = requirePlayer(mc);
        if (text.startsWith("/")) {
            p.connection.sendCommand(text.substring(1));
        } else {
            p.connection.sendChat(text);
        }
        return Map.of("ok", true, "sent", text);
    }

    /**
     * Holds movement keys for the given number of ticks by replacing the
     * player's ClientInput with one the agent drives; the player's own tick
     * then presses them for the player, exactly as if the keys were held. A
     * yaw or pitch in the body is set on the player directly. A negative tick
     * count holds the keys until the next /input says otherwise.
     */
    static Map<String, Object> input(Minecraft mc, Map<String, Object> body) {
        LocalPlayer p = requirePlayer(mc);
        Boolean forward = bool(body, "forward");
        Boolean backward = bool(body, "back");
        Boolean left = bool(body, "left");
        Boolean right = bool(body, "right");
        Boolean jump = bool(body, "jump");
        Boolean sneak = bool(body, "sneak");
        Boolean sprint = bool(body, "sprint");
        if (body.containsKey("yaw")) {
            p.setYRot(num(body, "yaw").floatValue());
        }
        if (body.containsKey("pitch")) {
            p.setXRot(num(body, "pitch").floatValue());
        }
        if (forward == null && backward == null && left == null && right == null
                && jump == null && sneak == null && sprint == null) {
            return Map.of("ok", true);
        }
        if (held == null) {
            held = new HeldInput(p);
        }
        int ticks = 20;
        if (body.containsKey("ticks")) {
            Double value = num(body, "ticks");
            ticks = value < 0 ? Integer.MAX_VALUE : (int) (double) value;
        }
        held.hold(new Input(
                Boolean.TRUE.equals(forward),
                Boolean.TRUE.equals(backward),
                Boolean.TRUE.equals(left),
                Boolean.TRUE.equals(right),
                Boolean.TRUE.equals(jump),
                Boolean.TRUE.equals(sneak),
                Boolean.TRUE.equals(sprint)),
                ticks);
        if (p.input != held) {
            held.original = p.input;
            p.input = held;
        }
        return Map.of("ok", true);
    }

    /** The crosshair's use: right-click on what it points at, or use the held item. */
    static Map<String, Object> use(Minecraft mc) {
        LocalPlayer p = requirePlayer(mc);
        HitResult hit = mc.hitResult;
        // vanilla: Minecraft.startUseItem
        for (InteractionHand hand : InteractionHand.values()) {
            ItemStack heldItem = p.getItemInHand(hand);
            SwingAnimation animation = heldItem.getInteractAnimation();
            if (hit != null) {
                if (hit.getType() == HitResult.Type.ENTITY && hit instanceof EntityHitResult entityHit) {
                    InteractionResult result = mc.gameMode.interact(p, entityHit.getEntity(), entityHit, hand);
                    if (result instanceof InteractionResult.Success success) {
                        if (success.swingSource() == InteractionResult.SwingSource.PREDICTED) {
                            p.swing(hand, animation, false);
                        }
                        return Map.of("ok", true, "target", "entity");
                    }
                }
                if (hit.getType() == HitResult.Type.BLOCK && hit instanceof BlockHitResult blockHit) {
                    int oldCount = heldItem.getCount();
                    InteractionResult result = mc.gameMode.useItemOn(p, hand, blockHit);
                    if (result instanceof InteractionResult.Success success) {
                        if (success.swingSource() == InteractionResult.SwingSource.PREDICTED) {
                            p.swing(hand, animation, false);
                            if (!heldItem.isEmpty() && (heldItem.getCount() != oldCount || p.hasInfiniteMaterials())) {
                                p.itemUsed(hand);
                            }
                        }
                        return Map.of("ok", true, "target", "block", "result", result.getClass().getSimpleName());
                    }
                    if (result instanceof InteractionResult.Fail) {
                        return Map.of("ok", true, "target", "block", "result", result.getClass().getSimpleName());
                    }
                }
            }
            if (!heldItem.isEmpty()) {
                InteractionResult result = mc.gameMode.useItem(p, hand);
                if (result instanceof InteractionResult.Success success) {
                    if (success.swingSource() == InteractionResult.SwingSource.PREDICTED) {
                        p.swing(hand, animation, false);
                    }
                    p.itemUsed(hand);
                    return Map.of("ok", true, "target", "item", "result", result.getClass().getSimpleName());
                }
            }
        }
        return Map.of("ok", true, "target", "none");
    }

    /** The crosshair's attack through the client's own left-click path, Minecraft.startAttack. */
    static Map<String, Object> attack(Minecraft mc) {
        LocalPlayer p = requirePlayer(mc);
        HitResult hit = mc.hitResult;
        if (hit == null) {
            return Map.of("ok", false, "error", "no crosshair target");
        }
        try {
            // startAttack is private and returns early while missTime > 0 (the cooldown after a miss).
            var missTime = Minecraft.class.getDeclaredField("missTime");
            missTime.setAccessible(true);
            missTime.setInt(mc, 0);
            var startAttack = Minecraft.class.getDeclaredMethod("startAttack");
            startAttack.setAccessible(true);
            boolean sent = (boolean) startAttack.invoke(mc);
            return Map.of("ok", true, "target", hit.getType().name().toLowerCase(), "endAttack", sent);
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException("Minecraft.startAttack is not reachable", e);
        }
    }

    /** Selects a hotbar slot and sends the carried-item packet the client sends for it. */
    static Map<String, Object> selectSlot(Minecraft mc, int slot) {
        LocalPlayer p = requirePlayer(mc);
        if (slot < 0 || slot > 8) {
            throw new IllegalArgumentException("hotbar slot must be 0..8, got " + slot);
        }
        p.getInventory().setSelectedSlot(slot);
        p.connection.send(new ServerboundSetCarriedItemPacket(slot));
        return Map.of("ok", true, "slot", slot);
    }

    /** Throws the held stack, or one item of it, the way the drop key does. */
    static Map<String, Object> drop(Minecraft mc, boolean all) {
        LocalPlayer p = requirePlayer(mc);
        boolean dropped = !p.getMainHandItem().isEmpty();
        mc.gameMode.dropItem(p, all);
        return Map.of("ok", true, "dropped", dropped);
    }

    /** Clicks a slot of the open container the way the client's own click does. */
    static Map<String, Object> click(Minecraft mc, int slot, int button, String mode) {
        LocalPlayer p = requirePlayer(mc);
        ContainerInput input = switch (mode == null ? "pickup" : mode) {
            case "pickup" -> ContainerInput.PICKUP;
            case "quick_move", "shift" -> ContainerInput.QUICK_MOVE;
            case "swap" -> ContainerInput.SWAP;
            case "throw" -> ContainerInput.THROW;
            default -> throw new IllegalArgumentException("unknown click mode: " + mode);
        };
        mc.gameMode.handleContainerInput(p.containerMenu.containerId, slot, button, input, p);
        return Map.of("ok", true, "slot", slot, "button", button, "mode", mode == null ? "pickup" : mode);
    }

    /** Asks the server for a respawn, the way the death screen's button does. */
    static Map<String, Object> respawn(Minecraft mc) {
        requirePlayer(mc).respawn();
        return Map.of("ok", true);
    }

    /** Leaves the server, the way the client's own disconnect does. */
    static Map<String, Object> disconnect(Minecraft mc) {
        mc.disconnectFromWorld(Component.literal("clientctl: requested disconnect"));
        return Map.of("ok", true);
    }

    /** Starts a connection to a server, the way the multiplayer screen does. */
    static Map<String, Object> connect(Minecraft mc, String address) {
        ServerData data = new ServerData("clientctl", address, ServerData.Type.OTHER);
        ConnectScreen.startConnecting(new JoinMultiplayerScreen(new TitleScreen()), mc,
                ServerAddress.parseString(address), data, false, null);
        return Map.of("ok", true, "address", address);
    }

    /**
     * Takes a screenshot through the client's own Screenshot grab and writes
     * it to path. The GPU readback the grab schedules lands after the call
     * returns, so the caller waits on the latch the callback counts down.
     */
    static Map<String, Object> screenshot(Minecraft mc, String path, AtomicReference<CountDownLatch> latchOut) {
        File file = new File(path);
        File parent = file.getParentFile();
        if (parent != null) {
            parent.mkdirs();
        }
        CountDownLatch latch = new CountDownLatch(1);
        AtomicReference<String> failure = new AtomicReference<>();
        RenderTarget target = mc.gameRenderer.mainRenderTarget();
        Consumer<NativeImage> callback = image -> {
            try (NativeImage img = image) {
                img.writeToFile(file);
            } catch (Exception e) {
                failure.set(String.valueOf(e));
            } finally {
                latch.countDown();
            }
        };
        net.minecraft.client.Screenshot.takeScreenshot(target, 1, callback);
        latchOut.set(latch);
        Map<String, Object> out = new LinkedHashMap<>();
        out.put("path", path);
        if (failure.get() != null) {
            out.put("ok", false);
            out.put("error", failure.get());
        } else {
            out.put("ok", true);
        }
        return out;
    }

    /** Waits for the screenshot's GPU readback to have written its file. */
    static boolean awaitScreenshot(CountDownLatch latch) {
        try {
            return latch.await(15, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return false;
        }
    }

    private static LocalPlayer requirePlayer(Minecraft mc) {
        if (mc.player == null || mc.getConnection() == null) {
            throw new IllegalStateException("not connected to a world");
        }
        return mc.player;
    }

    private static Boolean bool(Map<String, Object> body, String key) {
        Object value = body.get(key);
        if (value == null) {
            return null;
        }
        if (value instanceof Boolean b) {
            return b;
        }
        if (value instanceof Number n) {
            return n.doubleValue() != 0;
        }
        return Boolean.parseBoolean(String.valueOf(value));
    }

    private static Double num(Map<String, Object> body, String key) {
        Object value = body.get(key);
        if (value instanceof Number n) {
            return n.doubleValue();
        }
        return Double.parseDouble(String.valueOf(value));
    }

    /**
     * The ClientInput the agent puts in the player's place while a /input is
     * running. The game calls tick() once a player tick and then reads
     * keyPresses, so holding keys is a matter of writing them there; once the
     * hold has run out the game's own input is put back.
     */
    private static final class HeldInput extends ClientInput {
        private final LocalPlayer owner;
        private Input keys = Input.EMPTY;
        private int remaining;
        private ClientInput original;

        HeldInput(LocalPlayer owner) {
            this.owner = owner;
        }

        void hold(Input keys, int ticks) {
            this.keys = keys;
            this.remaining = ticks;
        }

        @Override
        public void tick() {
            if (remaining > 0) {
                remaining--;
                this.keyPresses = keys;
            } else {
                this.keyPresses = Input.EMPTY;
            }
            float forwardImpulse = impulse(keys.forward(), keys.backward());
            float leftImpulse = impulse(keys.left(), keys.right());
            this.moveVector = new Vec2(leftImpulse, forwardImpulse).normalized();
            if (remaining == 0 && keys != Input.EMPTY) {
                keys = Input.EMPTY;
            }
            if (remaining == 0 && owner.input == this && original != null) {
                owner.input = original;
                original = null;
                held = null;
            }
        }

        private static float impulse(boolean positive, boolean negative) {
            if (positive == negative) {
                return 0.0f;
            }
            return positive ? 1.0f : -1.0f;
        }
    }

    /** Reads the disconnect reason out of a DisconnectedScreen by reflection. */
    private static final class DisconnectedDetails {
        private final Object details;

        private DisconnectedDetails(Object details) {
            this.details = details;
        }

        static DisconnectedDetails of(Screen screen) {
            Object details = Reflect.get(screen, "details", Object.class);
            return details == null ? null : new DisconnectedDetails(details);
        }

        String reason() {
            Object reason = Reflect.call(details, "reason");
            return reason == null ? null : String.valueOf(Reflect.call(reason, "getString"));
        }
    }
}
