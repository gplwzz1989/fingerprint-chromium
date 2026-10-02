package com.fingerprint.saas.bridge;

import java.io.ByteArrayOutputStream;
import java.net.URI;
import java.net.URISyntaxException;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** 标准库的有界 JSON 编解码；不依赖 Android org.json 或第三方序列化库。 */
final class SnapshotJson {
    static final int MAX_DEPTH = 64;
    static final int MAX_NODES = 200_000;
    private static final String[] OPTIONS = {
        "cookies", "local_storage", "session_storage", "fingerprint", "proxy", "page"
    };

    private SnapshotJson() { }

    static byte[] encode(Map<String, ?> snapshot, int limit) {
        Writer writer = new Writer(limit);
        try {
            writer.value(snapshot, 0);
            return writer.output.toByteArray();
        } finally {
            writer.output.clear();
        }
    }

    static Map<String, Object> decode(byte[] bytes) {
        return decode(bytes, SnapshotCrypto.MAX_CIPHERTEXT_BYTES);
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> decode(byte[] bytes, int maximumBytes) {
        if (bytes.length > maximumBytes) throw invalid();
        try {
            String json = StandardCharsets.UTF_8.newDecoder()
                .onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT)
                .decode(ByteBuffer.wrap(bytes)).toString();
            Parser parser = new Parser(json);
            Object value = parser.value(0);
            parser.whitespace();
            if (parser.index != json.length() || !(value instanceof Map)) throw invalid();
            return (Map<String, Object>) value;
        } catch (CharacterCodingException error) {
            throw invalid();
        }
    }

    static void validate(Map<String, ?> snapshot, String accountId) {
        if (snapshot == null || !accountId.equals(snapshot.get("account_id"))
            || !integer(snapshot.get("schema_version"), 1, 1)
            || !(snapshot.get("cookies") instanceof List)
            || !(snapshot.get("local_storage") instanceof Map)
            || (snapshot.containsKey("session_storage") && !(snapshot.get("session_storage") instanceof Map))) {
            throw SnapshotCrypto.failure("账号快照的版本、标识或存储结构无效");
        }
        for (String name : new String[] {"local_storage", "session_storage"}) {
            Object values = snapshot.get(name);
            if (values != null) {
                for (Object value : ((Map<?, ?>) values).values()) {
                    if (!(value instanceof String)) throw SnapshotCrypto.failure("网页存储内容无效");
                }
            }
        }
        List<?> cookies = (List<?>) snapshot.get("cookies");
        if (cookies.size() > 5000) throw SnapshotCrypto.failure("Cookie 内容无效或数量超过支持范围");
        for (Object value : cookies) {
            if (!(value instanceof Map)) throw SnapshotCrypto.failure("Cookie 内容无效或数量超过支持范围");
            Map<?, ?> cookie = (Map<?, ?>) value;
            for (String name : new String[] {"name", "value", "domain", "path"}) {
                if (!(cookie.get(name) instanceof String)) throw SnapshotCrypto.failure("Cookie 内容无效或数量超过支持范围");
            }
        }
        Object rawOptions = snapshot.get("sync_options");
        boolean[] options = {true, true, true, true, true, true};
        if (rawOptions != null) {
            if (!(rawOptions instanceof Map)) throw SnapshotCrypto.failure("账号快照的同步选项无效");
            for (int index = 0; index < OPTIONS.length; index++) {
                Object option = ((Map<?, ?>) rawOptions).get(OPTIONS[index]);
                if (!(option instanceof Boolean)) throw SnapshotCrypto.failure("账号快照的同步选项无效");
                options[index] = (Boolean) option;
            }
        }
        if (options[1] || options[2]) options[5] = true;
        Object rawUrl = snapshot.get("storage_url");
        if (options[5] && rawUrl != null && !"".equals(rawUrl)) {
            if (!(rawUrl instanceof String)) throw SnapshotCrypto.failure("账号页面地址无效");
            try {
                URI url = new URI((String) rawUrl);
                String scheme = url.getScheme();
                if (!("http".equalsIgnoreCase(scheme) || "https".equalsIgnoreCase(scheme)
                    || "about:blank".equals(rawUrl)) || url.getRawUserInfo() != null
                    || (url.getRawAuthority() != null && url.getRawAuthority().contains("@"))) {
                    throw SnapshotCrypto.failure("账号页面地址无效");
                }
                if (!"about:blank".equals(rawUrl) && (url.getRawAuthority() == null || url.getRawAuthority().isEmpty())) {
                    throw SnapshotCrypto.failure("账号页面地址无效");
                }
            } catch (URISyntaxException error) {
                throw SnapshotCrypto.failure("账号页面地址无效");
            }
        }
        if ((options[1] || options[2]) && (!(rawUrl instanceof String)
            || !((String) rawUrl).matches("(?is)^https?://.*"))) {
            throw SnapshotCrypto.failure("网页存储快照缺少有效的来源页面");
        }
        if (options[3] && snapshot.containsKey("fingerprint")) {
            Object value = snapshot.get("fingerprint");
            if (!(value instanceof Map)) throw SnapshotCrypto.failure("账号指纹配置无效");
            Map<?, ?> fingerprint = (Map<?, ?>) value;
            Object agent = fingerprint.get("user_agent");
            if (!(agent instanceof String) || ((String) agent).length() > 512
                || !integer(fingerprint.get("hardware_concurrency"), 0, 64)) {
                throw SnapshotCrypto.failure("账号指纹配置无效");
            }
        }
    }

    private static boolean integer(Object value, int min, int max) {
        if (!(value instanceof Number)) return false;
        double number = ((Number) value).doubleValue();
        return number >= min && number <= max && number == Math.rint(number);
    }

    private static SnapshotCrypto.SnapshotCryptoException invalid() {
        return SnapshotCrypto.failure("账号快照 JSON 格式无效或超过支持范围");
    }

    private static final class Buffer extends ByteArrayOutputStream {
        Buffer() { super(1024); }
        void clear() { java.util.Arrays.fill(buf, (byte) 0); reset(); }
    }

    private static final class Writer {
        final Buffer output = new Buffer();
        final int limit;
        int nodes;
        Writer(int limit) { this.limit = limit; }

        void put(int value) {
            if (output.size() >= limit) throw SnapshotCrypto.failure("账号快照过大，请减少同步内容");
            output.write(value);
        }

        void ascii(String value) { for (int index = 0; index < value.length(); index++) put(value.charAt(index)); }

        void string(String value) {
            put('"');
            for (int index = 0; index < value.length(); index++) {
                char c = value.charAt(index);
                if (c == '"' || c == '\\') { put('\\'); put(c); }
                else if (c == '\b') ascii("\\b");
                else if (c == '\t') ascii("\\t");
                else if (c == '\n') ascii("\\n");
                else if (c == '\f') ascii("\\f");
                else if (c == '\r') ascii("\\r");
                else if (c < 32 || (Character.isSurrogate(c)
                    && !(Character.isHighSurrogate(c) && index + 1 < value.length()
                         && Character.isLowSurrogate(value.charAt(index + 1))))) {
                    ascii(String.format(java.util.Locale.ROOT, "\\u%04x", (int) c));
                } else {
                    int point = c;
                    if (Character.isHighSurrogate(c)) point = Character.toCodePoint(c, value.charAt(++index));
                    if (point < 128) put(point);
                    else if (point < 2048) { put(0xc0 | (point >> 6)); put(0x80 | (point & 63)); }
                    else if (point < 65536) { put(0xe0 | (point >> 12)); put(0x80 | ((point >> 6) & 63)); put(0x80 | (point & 63)); }
                    else { put(0xf0 | (point >> 18)); put(0x80 | ((point >> 12) & 63)); put(0x80 | ((point >> 6) & 63)); put(0x80 | (point & 63)); }
                }
            }
            put('"');
        }

        void value(Object value, int depth) {
            if (depth > MAX_DEPTH || ++nodes > MAX_NODES) throw invalid();
            if (value == null) ascii("null");
            else if (value instanceof String) string((String) value);
            else if (value instanceof Boolean) ascii(value.toString());
            else if (value instanceof Number) {
                if (!(value instanceof Byte || value instanceof Short || value instanceof Integer
                    || value instanceof Long || value instanceof Float || value instanceof Double)) throw invalid();
                double number = ((Number) value).doubleValue();
                if (Double.isInfinite(number) || Double.isNaN(number)) throw invalid();
                String representation = value.toString();
                if (value instanceof Float || value instanceof Double) {
                    // JSON.parse 后的整数不能因 JVM 的 .0 后缀额外占用快照配额。
                    representation = number == 0 ? "0" : representation.replace(".0E", "E");
                    if (representation.endsWith(".0")) representation = representation.substring(0, representation.length() - 2);
                }
                ascii(representation);
            } else if (value instanceof Map) {
                put('{');
                boolean first = true;
                for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
                    if (!(entry.getKey() instanceof String)) throw invalid();
                    if (!first) put(',');
                    first = false;
                    string((String) entry.getKey()); put(':'); value(entry.getValue(), depth + 1);
                }
                put('}');
            } else if (value instanceof List) {
                put('[');
                boolean first = true;
                for (Object item : (List<?>) value) {
                    if (!first) put(',');
                    first = false;
                    value(item, depth + 1);
                }
                put(']');
            } else throw invalid();
        }
    }

    private static final class Parser {
        final String json;
        int index, nodes;
        Parser(String json) { this.json = json; }

        void whitespace() {
            while (index < json.length()) {
                char c = json.charAt(index);
                if (c != ' ' && c != '\t' && c != '\r' && c != '\n') return;
                index++;
            }
        }

        boolean take(char expected) {
            if (index < json.length() && json.charAt(index) == expected) { index++; return true; }
            return false;
        }

        void expect(char expected) { if (!take(expected)) throw invalid(); }

        Object value(int depth) {
            if (depth > MAX_DEPTH || ++nodes > MAX_NODES) throw invalid();
            whitespace();
            if (index >= json.length()) throw invalid();
            char c = json.charAt(index);
            if (c == '"') return string();
            if (c == '{') {
                index++; whitespace();
                Map<String, Object> map = new LinkedHashMap<>();
                if (take('}')) return map;
                do {
                    whitespace(); String key = string(); whitespace(); expect(':');
                    if (map.containsKey(key)) throw invalid();
                    map.put(key, value(depth + 1)); whitespace();
                    if (take('}')) return map;
                    expect(',');
                } while (true);
            }
            if (c == '[') {
                index++; whitespace();
                List<Object> list = new ArrayList<>();
                if (take(']')) return list;
                do {
                    list.add(value(depth + 1)); whitespace();
                    if (take(']')) return list;
                    expect(',');
                } while (true);
            }
            for (String literal : new String[] {"true", "false", "null"}) {
                if (json.startsWith(literal, index)) {
                    index += literal.length();
                    return "null".equals(literal) ? null : Boolean.valueOf(literal);
                }
            }
            int start = index;
            take('-');
            if (!take('0')) digits();
            if (take('.')) digits();
            if (take('e') || take('E')) { if (!take('+')) take('-'); digits(); }
            if (index == start) throw invalid();
            try {
                double number = Double.parseDouble(json.substring(start, index));
                if (Double.isInfinite(number) || Double.isNaN(number)) throw invalid();
                return number;
            } catch (NumberFormatException error) { throw invalid(); }
        }

        void digits() {
            int start = index;
            while (index < json.length() && json.charAt(index) >= '0' && json.charAt(index) <= '9') index++;
            if (start == index) throw invalid();
        }

        String string() {
            expect('"');
            StringBuilder value = new StringBuilder();
            while (index < json.length()) {
                char c = json.charAt(index++);
                if (c == '"') return value.toString();
                if (c < 32) throw invalid();
                if (c == '\\') {
                    if (index >= json.length()) throw invalid();
                    char escape = json.charAt(index++);
                    switch (escape) {
                        case '"': c = '"'; break;
                        case '\\': c = '\\'; break;
                        case '/': c = '/'; break;
                        case 'b': c = '\b'; break;
                        case 'f': c = '\f'; break;
                        case 'n': c = '\n'; break;
                        case 'r': c = '\r'; break;
                        case 't': c = '\t'; break;
                        case 'u':
                            if (index + 4 > json.length()) throw invalid();
                            int code = 0;
                            for (int digit = 0; digit < 4; digit++) {
                                char hex = json.charAt(index++);
                                int decoded = hex >= '0' && hex <= '9' ? hex - '0'
                                    : hex >= 'a' && hex <= 'f' ? hex - 'a' + 10
                                    : hex >= 'A' && hex <= 'F' ? hex - 'A' + 10 : -1;
                                if (decoded < 0) throw invalid();
                                code = (code << 4) | decoded;
                            }
                            c = (char) code; break;
                        default: throw invalid();
                    }
                }
                value.append(c);
            }
            throw invalid();
        }
    }
}
