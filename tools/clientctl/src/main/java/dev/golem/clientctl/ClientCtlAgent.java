package dev.golem.clientctl;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Supplier;
import net.minecraft.client.Minecraft;

/**
 * The clientctl agent: a java agent (Premain-Class) that attaches to the
 * official Minecraft client and answers HTTP with what the client knows and
 * does what a test would otherwise type at it.
 *
 * <p>Every request is run on the client's render thread - queued with the
 * game's own task queue and waited for - so the state read is consistent and
 * the actions happen between ticks exactly like the ones the game performs
 * itself. The HTTP server listens on 127.0.0.1 only, on the port of the
 * {@code clientctl.port} system property (default 25700).</p>
 *
 * <p>Load it with {@code -javaagent:clientctl-agent.jar} and talk to it with
 * tools/clientctl; see tools/README.md.</p>
 */
public final class ClientCtlAgent {

    private static final String PORT_PROPERTY = "clientctl.port";
    private static final int DEFAULT_PORT = 25700;
    private static final long RENDER_THREAD_TIMEOUT_SECONDS = 30;

    private ClientCtlAgent() {
    }

    /** The JVM calls this before the client's main runs; the server starts here. */
    public static void premain(String args, java.lang.instrument.Instrumentation inst) {
        try {
            int port = portFrom(args);
            HttpServer server = HttpServer.create(
                    new InetSocketAddress(InetAddress.getLoopbackAddress(), port), 0);
            server.createContext("/", ClientCtlAgent::handle);
            server.setExecutor(Executors.newCachedThreadPool(r -> {
                Thread thread = new Thread(r, "clientctl-http");
                thread.setDaemon(true);
                return thread;
            }));
            server.start();
            System.out.println("[clientctl] listening on 127.0.0.1:" + port);
        } catch (Throwable t) {
            // A failing agent must not take the game down with it: the client
            // runs fine without one, only blind.
            System.err.println("[clientctl] not started: " + t);
        }
    }

    /** The port comes from the system property, or agent args of the form port=N. */
    private static int portFrom(String args) {
        if (args != null && args.startsWith("port=")) {
            return Integer.parseInt(args.substring("port=".length()).trim());
        }
        return Integer.getInteger(PORT_PROPERTY, DEFAULT_PORT);
    }

    // -------------------------------------------------------------- routing

    private static void handle(HttpExchange exchange) throws IOException {
        String path = exchange.getRequestURI().getPath();
        String query = exchange.getRequestURI().getRawQuery();
        String method = exchange.getRequestMethod();
        Map<String, Object> response;
        int status = 200;
        try {
            if (!path.startsWith("/")) {
                throw new IllegalArgumentException("bad path");
            }
            String route = path.substring(1);
            String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            response = route(route, method, body, query);
        } catch (NotFound e) {
            response = Map.of("ok", false, "error", "no such endpoint: " + path);
            status = 404;
        } catch (Throwable t) {
            response = Map.of("ok", false, "error", String.valueOf(t));
        }
        byte[] bytes = Json.write(response).getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "application/json");
        exchange.sendResponseHeaders(status, bytes.length);
        try (OutputStream out = exchange.getResponseBody()) {
            out.write(bytes);
        }
    }

    private static Map<String, Object> route(String route, String method, String body, String query)
            throws Exception {
        Minecraft mc = Minecraft.getInstance();
        if (mc == null) {
            return Map.of("ok", false, "error", "client not started yet");
        }
        Map<String, Object> parsed = method.equals("POST") ? Json.parseObject(body) : Map.of();
        return switch (route) {
            case "state" -> onRenderThread(() -> ClientCtl.state(mc, range(query)));
            case "chat" -> onRenderThread(() -> ClientCtl.chat(mc, text(parsed)));
            case "input" -> onRenderThread(() -> ClientCtl.input(mc, parsed));
            case "use" -> onRenderThread(() -> ClientCtl.use(mc));
            case "attack" -> onRenderThread(() -> ClientCtl.attack(mc));
            case "slot" -> onRenderThread(() -> ClientCtl.selectSlot(mc, (int) (double) number(parsed, "slot")));
            case "drop" -> onRenderThread(() -> ClientCtl.drop(mc, bool(parsed, "all")));
            case "click" -> onRenderThread(() -> ClientCtl.click(mc,
                    (int) (double) number(parsed, "slot"),
                    (int) (double) number(parsed, "button"),
                    parsed.get("mode") == null ? null : String.valueOf(parsed.get("mode"))));
            case "respawn" -> onRenderThread(() -> ClientCtl.respawn(mc));
            case "disconnect" -> onRenderThread(() -> ClientCtl.disconnect(mc));
            case "connect" -> onRenderThread(() -> ClientCtl.connect(mc, text(parsed)));
            case "screenshot" -> screenshot(mc, text(parsed));
            default -> throw new NotFound();
        };
    }

    private static Map<String, Object> screenshot(Minecraft mc, String path) throws Exception {
        AtomicReference<CountDownLatch> latch = new AtomicReference<>();
        Map<String, Object> started = onRenderThread(() -> ClientCtl.screenshot(mc, path, latch));
        CountDownLatch waiting = latch.get();
        if (waiting != null) {
            if (!ClientCtl.awaitScreenshot(waiting)) {
                Map<String, Object> out = new LinkedHashMap<>(started);
                out.put("ok", false);
                out.put("error", "timed out waiting for the screenshot to be written");
                return out;
            }
        }
        return started;
    }

    /** Queues a task on the render thread and waits for its answer. */
    private static <T> T onRenderThread(Supplier<T> task) throws Exception {
        return Minecraft.getInstance().submit(task)
                .get(RENDER_THREAD_TIMEOUT_SECONDS, TimeUnit.SECONDS);
    }

    /** The entity radius of /state, in blocks. */
    private static double range(String query) {
        Map<String, String> params = queryParams(query);
        String value = params.get("range");
        if (value == null) {
            return 32;
        }
        try {
            return Double.parseDouble(value);
        } catch (NumberFormatException e) {
            return 32;
        }
    }

    private static Map<String, String> queryParams(String query) {
        Map<String, String> out = new HashMap<>();
        if (query == null) {
            return out;
        }
        for (String pair : query.split("&")) {
            int eq = pair.indexOf('=');
            if (eq <= 0) {
                continue;
            }
            out.put(URLDecoder.decode(pair.substring(0, eq), StandardCharsets.UTF_8),
                    URLDecoder.decode(pair.substring(eq + 1), StandardCharsets.UTF_8));
        }
        return out;
    }

    private static String text(Map<String, Object> body) {
        Object value = body.get("text");
        if (value == null) {
            value = body.get("address");
        }
        if (value == null) {
            value = body.get("path");
        }
        if (value == null) {
            throw new IllegalArgumentException("missing text/address/path in the body");
        }
        return String.valueOf(value);
    }

    private static double number(Map<String, Object> body, String key) {
        Object value = body.get(key);
        if (value instanceof Number n) {
            return n.doubleValue();
        }
        if (value == null) {
            throw new IllegalArgumentException("missing " + key + " in the body");
        }
        return Double.parseDouble(String.valueOf(value));
    }

    private static boolean bool(Map<String, Object> body, String key) {
        Object value = body.get(key);
        if (value instanceof Boolean b) {
            return b;
        }
        if (value instanceof Number n) {
            return n.doubleValue() != 0;
        }
        return Boolean.parseBoolean(String.valueOf(value));
    }

    /** Marks an unknown route so the handler answers 404 instead of 200. */
    private static final class NotFound extends RuntimeException {
    }
}
