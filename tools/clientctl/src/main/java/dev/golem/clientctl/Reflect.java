package dev.golem.clientctl;

import java.lang.reflect.Field;
import java.lang.reflect.Method;

/**
 * The handful of reflection reads the client's own classes do not expose: the
 * chat component inside the Hud, the chat component's message list, the
 * disconnect reason inside a DisconnectedScreen. Everything else is called
 * directly.
 */
final class Reflect {

    private Reflect() {
    }

    /**
     * Reads a private field by name, walking up the class hierarchy, and
     * returns null when there is nothing of the expected kind there - a field
     * a future version renamed should lose one feature, not crash the agent.
     */
    static <T> T get(Object owner, String name, Class<T> type) {
        if (owner == null) {
            return null;
        }
        Class<?> c = owner.getClass();
        while (c != null) {
            try {
                Field field = c.getDeclaredField(name);
                field.setAccessible(true);
                Object value = field.get(owner);
                return type.isInstance(value) ? type.cast(value) : null;
            } catch (NoSuchFieldException e) {
                c = c.getSuperclass();
            } catch (ReflectiveOperationException e) {
                return null;
            }
        }
        return null;
    }

    /** Calls a no-argument method by name, or null when there is none. */
    static Object call(Object owner, String name) {
        if (owner == null) {
            return null;
        }
        Class<?> c = owner.getClass();
        while (c != null) {
            try {
                Method method = c.getDeclaredMethod(name);
                method.setAccessible(true);
                return method.invoke(owner);
            } catch (NoSuchMethodException e) {
                c = c.getSuperclass();
            } catch (ReflectiveOperationException e) {
                return null;
            }
        }
        return null;
    }
}
