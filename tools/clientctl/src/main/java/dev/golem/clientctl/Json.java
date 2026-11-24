package dev.golem.clientctl;

import java.util.List;
import java.util.Map;

/**
 * A small JSON writer and parser, enough for the answers this agent gives and
 * the bodies it reads. Nothing here knows about the game; the agent hands it
 * maps, lists, strings, numbers, booleans and nulls.
 *
 * <p>Written by hand because the agent is compiled against the client's own
 * classpath, which carries no JSON library the agent could reach without
 * shading one in.</p>
 */
final class Json {

    private Json() {
    }

    /** Writes any value the object model allows, escaping what has to be. */
    static String write(Object value) {
        StringBuilder sb = new StringBuilder();
        write(sb, value);
        return sb.toString();
    }

    private static void write(StringBuilder sb, Object value) {
        if (value == null) {
            sb.append("null");
        } else if (value instanceof String s) {
            writeString(sb, s);
        } else if (value instanceof Boolean || value instanceof Number) {
            sb.append(value);
        } else if (value instanceof Map<?, ?> map) {
            sb.append('{');
            boolean first = true;
            for (Map.Entry<?, ?> entry : map.entrySet()) {
                if (!first) {
                    sb.append(',');
                }
                first = false;
                writeString(sb, String.valueOf(entry.getKey()));
                sb.append(':');
                write(sb, entry.getValue());
            }
            sb.append('}');
        } else if (value instanceof List<?> list) {
            sb.append('[');
            boolean first = true;
            for (Object item : list) {
                if (!first) {
                    sb.append(',');
                }
                first = false;
                write(sb, item);
            }
            sb.append(']');
        } else {
            // Anything else is a bug in a handler, not in the input: say so
            // rather than emitting a shape the caller cannot read.
            writeString(sb, String.valueOf(value));
        }
    }

    private static void writeString(StringBuilder sb, String s) {
        sb.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"' -> sb.append("\\\"");
                case '\\' -> sb.append("\\\\");
                case '\n' -> sb.append("\\n");
                case '\r' -> sb.append("\\r");
                case '\t' -> sb.append("\\t");
                default -> {
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
                }
            }
        }
        sb.append('"');
    }

    /**
     * Parses a JSON document into maps, lists, strings, doubles, booleans and
     * null. The bodies this agent reads are small flat objects, but the parser
     * is complete enough to reject malformed input loudly instead of guessing.
     */
    static Object parse(String text) {
        Parser parser = new Parser(text);
        parser.skipWhitespace();
        Object value = parser.value();
        parser.skipWhitespace();
        if (!parser.done()) {
            throw new IllegalArgumentException("trailing content at offset " + parser.pos);
        }
        return value;
    }

    /** A body of zero bytes reads as an empty object, which is what a POST with no fields is. */
    static Map<String, Object> parseObject(String text) {
        if (text == null || text.isBlank()) {
            return Map.of();
        }
        Object parsed = parse(text);
        if (!(parsed instanceof Map<?, ?> map)) {
            throw new IllegalArgumentException("expected a JSON object");
        }
        @SuppressWarnings("unchecked")
        Map<String, Object> result = (Map<String, Object>) map;
        return result;
    }

    private static final class Parser {
        private final String text;
        private int pos;

        Parser(String text) {
            this.text = text;
        }

        boolean done() {
            return pos >= text.length();
        }

        void skipWhitespace() {
            while (pos < text.length() && Character.isWhitespace(text.charAt(pos))) {
                pos++;
            }
        }

        Object value() {
            if (done()) {
                throw new IllegalArgumentException("unexpected end of JSON");
            }
            char c = text.charAt(pos);
            return switch (c) {
                case '{' -> object();
                case '[' -> array();
                case '"' -> string();
                case 't' -> literal("true", true);
                case 'f' -> literal("false", false);
                case 'n' -> literal("null", null);
                default -> number();
            };
        }

        private Map<String, Object> object() {
            expect('{');
            Map<String, Object> map = new java.util.LinkedHashMap<>();
            skipWhitespace();
            if (peek() == '}') {
                pos++;
                return map;
            }
            while (true) {
                skipWhitespace();
                String key = string();
                skipWhitespace();
                expect(':');
                skipWhitespace();
                map.put(key, value());
                skipWhitespace();
                char c = peek();
                if (c == ',') {
                    pos++;
                } else if (c == '}') {
                    pos++;
                    return map;
                } else {
                    throw new IllegalArgumentException("expected ',' or '}' at offset " + pos);
                }
            }
        }

        private List<Object> array() {
            expect('[');
            List<Object> list = new java.util.ArrayList<>();
            skipWhitespace();
            if (peek() == ']') {
                pos++;
                return list;
            }
            while (true) {
                skipWhitespace();
                list.add(value());
                skipWhitespace();
                char c = peek();
                if (c == ',') {
                    pos++;
                } else if (c == ']') {
                    pos++;
                    return list;
                } else {
                    throw new IllegalArgumentException("expected ',' or ']' at offset " + pos);
                }
            }
        }

        private String string() {
            expect('"');
            StringBuilder sb = new StringBuilder();
            while (true) {
                if (done()) {
                    throw new IllegalArgumentException("unterminated string");
                }
                char c = text.charAt(pos++);
                if (c == '"') {
                    return sb.toString();
                }
                if (c == '\\') {
                    char escape = text.charAt(pos++);
                    switch (escape) {
                        case '"' -> sb.append('"');
                        case '\\' -> sb.append('\\');
                        case '/' -> sb.append('/');
                        case 'b' -> sb.append('\b');
                        case 'f' -> sb.append('\f');
                        case 'n' -> sb.append('\n');
                        case 'r' -> sb.append('\r');
                        case 't' -> sb.append('\t');
                        case 'u' -> {
                            sb.append((char) Integer.parseInt(text.substring(pos, pos + 4), 16));
                            pos += 4;
                        }
                        default -> throw new IllegalArgumentException("bad escape \\" + escape);
                    }
                } else {
                    sb.append(c);
                }
            }
        }

        private Object literal(String word, Object value) {
            if (!text.startsWith(word, pos)) {
                throw new IllegalArgumentException("bad literal at offset " + pos);
            }
            pos += word.length();
            return value;
        }

        private Object number() {
            int start = pos;
            while (!done() && "+-0123456789.eE".indexOf(text.charAt(pos)) >= 0) {
                pos++;
            }
            try {
                return Double.parseDouble(text.substring(start, pos));
            } catch (NumberFormatException e) {
                throw new IllegalArgumentException("bad number at offset " + start);
            }
        }

        private char peek() {
            if (done()) {
                throw new IllegalArgumentException("unexpected end of JSON");
            }
            return text.charAt(pos);
        }

        private void expect(char c) {
            if (done() || text.charAt(pos) != c) {
                throw new IllegalArgumentException("expected '" + c + "' at offset " + pos);
            }
            pos++;
        }
    }
}
