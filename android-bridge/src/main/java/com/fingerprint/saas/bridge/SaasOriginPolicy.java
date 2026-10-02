package com.fingerprint.saas.bridge;

import java.net.InetAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.UnknownHostException;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;

/**
 * Java 8 / Android API 26+ 的精确来源策略，无外部依赖。
 * 白名单和默认启动 URL 均由可信宿主提供；默认启动 URL 不会自动获得授权。
 * 授权依据必须是 Chromium 消息通道向宿主提供的真实浏览器 Origin，
 * 严禁从网页消息 args、消息体、页面自报字段或页面 URL 推断消息来源。
 */
public final class SaasOriginPolicy {
    private static final int MAX_ORIGINS = 128;
    private static final int MAX_ORIGIN_LENGTH = 2048;
    private static final int MAX_PAGE_URL_LENGTH = 8192;
    private final Set<Origin> allowedOrigins;
    private final String defaultStartupUrl;

    /** 配置错误仅输出中文说明，不包含输入地址或底层解析异常。 */
    public SaasOriginPolicy(List<String> compiledAllowedOrigins, String defaultStartupUrl) {
        if (compiledAllowedOrigins == null || compiledAllowedOrigins.isEmpty()
            || compiledAllowedOrigins.size() > MAX_ORIGINS) {
            throw new IllegalArgumentException("来源白名单不能为空，且最多允许 128 项");
        }
        Set<Origin> compiled = new HashSet<>();
        int entries = 0;
        try {
            for (Object value : compiledAllowedOrigins) {
                if (++entries > MAX_ORIGINS) throw new IllegalArgumentException("来源白名单超过支持范围");
                if (!(value instanceof String)) throw new IllegalArgumentException("来源白名单包含无效的 Origin");
                Origin origin = parse((String) value, false);
                if (origin == null) throw new IllegalArgumentException("来源白名单包含无效的 Origin");
                compiled.add(origin);
            }
        } catch (java.util.ConcurrentModificationException error) {
            throw new IllegalArgumentException("来源白名单在配置过程中发生变化，请重新配置");
        }
        if (compiled.isEmpty()) throw new IllegalArgumentException("来源白名单不能为空");
        Origin startup = parse(defaultStartupUrl, true);
        if (startup == null) throw new IllegalArgumentException("默认启动地址格式无效");
        if (!compiled.contains(startup)) throw new IllegalArgumentException("默认启动地址不在可信来源白名单中");
        this.allowedOrigins = Collections.unmodifiableSet(compiled);
        this.defaultStartupUrl = defaultStartupUrl;
    }

    /**
     * 只接受可信宿主从浏览器取得的序列化 Origin，绝不能传入网页消息 args 中的 origin。
     * 必须是无路径（包括末尾 /）、query、fragment 或凭据的 http/https Origin。
     * 协议、域名、有效端口必须完整匹配；不支持通配符、子域或后缀授权。
     * 非法值及 opaque 来源（例如 null）一律返回 false。
     */
    public boolean matchesOrigin(String serializedOrigin) {
        Origin origin = parse(serializedOrigin, false);
        return origin != null && allowedOrigins.contains(origin);
    }

    /**
     * 仅用于可信原生宿主的页面注册或导航地址校验，不能替代消息来源验证。
     * 可以包含合法路径、query 和 fragment，但不允许凭据或会改变 host 解释的编码。
     * 不进行 IDN 转换、IPv4 简写转换、尾点剥离、反斜线修正或相对地址补全。
     */
    public boolean allowsPageUrl(String nativeRegistrationUrl) {
        Origin origin = parse(nativeRegistrationUrl, true);
        return origin != null && allowedOrigins.contains(origin);
    }

    /** 返回宿主提供并已验证的启动地址，不在本模块硬编码或重新生成默认地址。 */
    public String getDefaultStartupUrl() { return defaultStartupUrl; }

    private static Origin parse(String value, boolean pageUrl) {
        if (value == null || value.isEmpty()
            || value.length() > (pageUrl ? MAX_PAGE_URL_LENGTH : MAX_ORIGIN_LENGTH)) return null;
        try {
            URI uri = new URI(value);
            if (uri.isOpaque() || !uri.isAbsolute()) return null;
            String scheme = uri.getScheme().toLowerCase(Locale.ROOT);
            if (!"http".equals(scheme) && !"https".equals(scheme)) return null;
            String authority = uri.getRawAuthority();
            if (authority == null || authority.isEmpty() || uri.getHost() == null
                || uri.getRawUserInfo() != null || authority.indexOf('@') >= 0
                || authority.indexOf('%') >= 0 || authority.indexOf('\\') >= 0) return null;
            if (!pageUrl && (!uri.getRawPath().isEmpty()
                || uri.getRawQuery() != null || uri.getRawFragment() != null)) return null;

            String host;
            String portText = null;
            if (authority.charAt(0) == '[') {
                int close = authority.indexOf(']');
                if (close < 0) return null;
                String literal = authority.substring(1, close);
                if (close + 1 < authority.length()) {
                    if (authority.charAt(close + 1) != ':') return null;
                    portText = authority.substring(close + 2);
                }
                host = canonicalIpv6(literal);
            } else {
                int colon = authority.indexOf(':');
                if (colon >= 0) {
                    host = authority.substring(0, colon);
                    portText = authority.substring(colon + 1);
                } else host = authority;
                host = canonicalDnsOrIpv4(host);
            }
            if (host == null) return null;
            int port = "http".equals(scheme) ? 80 : 443;
            if (portText != null) {
                // 浏览器可序列化端口 0；仅接受 0～65535 的规范十进制写法。
                if (portText.isEmpty() || portText.length() > 5
                    || (portText.length() > 1 && portText.charAt(0) == '0')) return null;
                for (int index = 0; index < portText.length(); index++) {
                    if (portText.charAt(index) < '0' || portText.charAt(index) > '9') return null;
                }
                port = Integer.parseInt(portText);
                if (port > 65535) return null;
            }
            return new Origin(scheme, host, port);
        } catch (URISyntaxException | UnknownHostException | IllegalArgumentException error) {
            // 输入解析失败即拒绝；不向网页暴露原始异常或地址。
            return null;
        }
    }

    private static String canonicalDnsOrIpv4(String host) {
        if (host.isEmpty() || host.length() > 254) return null;
        String canonical = host.toLowerCase(Locale.ROOT);
        boolean trailingDot = canonical.endsWith(".");
        String labelsHost = trailingDot ? canonical.substring(0, canonical.length() - 1) : canonical;
        if (labelsHost.isEmpty() || labelsHost.length() > 253) return null;
        String[] labels = labelsHost.split("\\.", -1);
        for (String label : labels) {
            if (label.isEmpty() || label.length() > 63 || !asciiLetterOrDigit(label.charAt(0))
                || !asciiLetterOrDigit(label.charAt(label.length() - 1))) return null;
            for (int index = 0; index < label.length(); index++) {
                if (!asciiLetterOrDigit(label.charAt(index)) && label.charAt(index) != '-') return null;
            }
        }
        String last = labels[labels.length - 1];
        if (decimal(last) || last.matches("0x[0-9a-f]*")) {
            // 拒绝浏览器会重解释的十进制整数、八进制、十六进制及缩写 IPv4。
            if (trailingDot || labels.length != 4) return null;
            for (String label : labels) {
                if (!decimal(label) || label.length() > 3
                    || (label.length() > 1 && label.charAt(0) == '0')
                    || Integer.parseInt(label) > 255) return null;
            }
        }
        // DNS 尾点作为独立 host 保留，不使 example.com. 获得 example.com 的授权。
        return canonical;
    }

    private static boolean asciiLetterOrDigit(char value) {
        return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9');
    }

    private static boolean decimal(String value) {
        if (value.isEmpty()) return false;
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < '0' || value.charAt(index) > '9') return false;
        }
        return true;
    }

    private static String canonicalIpv6(String literal) throws UnknownHostException {
        if (literal.indexOf(':') < 0) return null;
        for (int index = 0; index < literal.length(); index++) {
            char value = literal.charAt(index);
            if (!((value >= '0' && value <= '9') || (value >= 'a' && value <= 'f')
                || (value >= 'A' && value <= 'F') || value == ':' || value == '.')) return null;
        }
        if (literal.indexOf('.') >= 0) {
            String tail = literal.substring(literal.lastIndexOf(':') + 1);
            // IPv6 中嵌入的 IPv4 也必须是四段规范十进制，不能借 IP 解析库容忍前导零。
            if (canonicalDnsOrIpv4(tail) == null || tail.split("\\.", -1).length != 4) return null;
        }
        // URI 已确认带方括号的 IPv6；仅向标准解析库传入纯地址字面量，不进行 DNS 查询。
        byte[] bytes = InetAddress.getByName(literal).getAddress();
        if (bytes.length == 4) {
            // 部分 JVM 将 IPv4-mapped IPv6 返回为 IPv4，保留其 IPv6 来源身份，避免跨地址族授权。
            byte[] mapped = new byte[16];
            mapped[10] = (byte) 0xff; mapped[11] = (byte) 0xff;
            System.arraycopy(bytes, 0, mapped, 12, 4);
            bytes = mapped;
        }
        if (bytes.length != 16) return null;
        int[] groups = new int[8];
        for (int index = 0; index < groups.length; index++) {
            groups[index] = ((bytes[index * 2] & 255) << 8) | (bytes[index * 2 + 1] & 255);
        }
        int longestStart = -1, longestLength = 1;
        for (int index = 0; index < groups.length;) {
            if (groups[index] != 0) { index++; continue; }
            int start = index;
            while (index < groups.length && groups[index] == 0) index++;
            if (index - start > longestLength) { longestStart = start; longestLength = index - start; }
        }
        StringBuilder host = new StringBuilder("[");
        for (int index = 0; index < groups.length;) {
            if (index == longestStart) { host.append("::"); index += longestLength; }
            else {
                char last = host.charAt(host.length() - 1);
                if (last != '[' && last != ':') host.append(':');
                host.append(Integer.toHexString(groups[index++]));
            }
        }
        return host.append(']').toString();
    }

    private static final class Origin {
        private final String scheme, host;
        private final int port;
        Origin(String scheme, String host, int port) { this.scheme = scheme; this.host = host; this.port = port; }
        @Override public boolean equals(Object value) {
            if (!(value instanceof Origin)) return false;
            Origin other = (Origin) value;
            return port == other.port && scheme.equals(other.scheme) && host.equals(other.host);
        }
        @Override public int hashCode() { return 31 * (31 * scheme.hashCode() + host.hashCode()) + port; }
    }
}
