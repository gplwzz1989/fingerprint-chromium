package com.fingerprint.saas.bridge;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/** 使用真实 URI 和 IP 地址解析库的独立 JVM 自测，不模拟消息通道或浏览器业务。 */
public final class SaasOriginPolicySelfTest {
    private static int passed;
    private SaasOriginPolicySelfTest() { }

    public static void main(String[] args) {
        List<String> whitelist = new ArrayList<>(Arrays.asList(
            "https://example.com", "http://localhost:8080", "http://127.0.0.1",
            "https://[2001:0DB8:0:0:0:0:0:1]:8443", "https://xn--fiqs8s.cn"
        ));
        String startup = "https://example.com/管理?页面=账号#设置";
        SaasOriginPolicy policy = new SaasOriginPolicy(whitelist, startup);
        check(startup.equals(policy.getDefaultStartupUrl()), "启动地址由宿主提供并完整保留");
        for (String origin : new String[] {
            "https://example.com", "https://example.com:443", "HTTPS://EXAMPLE.COM",
            "http://localhost:8080", "http://127.0.0.1:80", "https://[2001:db8::1]:8443",
            "https://[2001:db8:0:0:0:0:0:1]:8443", "https://xn--fiqs8s.cn"
        }) check(policy.matchesOrigin(origin), "规范化后精确来源匹配");

        for (String origin : new String[] {
            null, "", "null", "opaque", "https://example.com/", "https://example.com/path",
            "https://example.com?", "https://example.com?x=1", "https://example.com#", "https://example.com#x",
            "http://example.com", "https://example.com:80", "https://example.com:8443",
            "http://localhost", "https://sub.example.com", "https://example.com.evil.test",
            "https://evil-example.com", "https://example.com.", "https://example.com..",
            "https://*.example.com", "https://user@example.com", "https://user:password@example.com",
            "https://@example.com", "https://example.com@evil.test", "https://%65xample.com",
            "https://example%2ecom", "https://example.com%2fevil.test", "https://example.com%00",
            "https://", "https:///example.com", "https:example.com", "//example.com",
            "file://example.com", "blob:https://example.com/id", "data:text/plain,test", "about:blank",
            " https://example.com", "https://example.com ", "https://example.com\n", "https://exam\tple.com",
            "https://example.com\\@evil.test", "https://example.com:", "https://example.com:0443",
            "https://example.com:+443", "https://example.com:-1", "https://example.com:65536",
            "https://example.com:999999999999999999999", "https://example.com:443:80",
            "https://ｅxample.com", "https://中国.cn", "http://127.1", "http://0177.0.0.1",
            "http://127.0.0.01", "http://0x7f000001", "http://2130706433", "http://127.0.0.1.",
            "https://[2001:db8::2]:8443", "http://[2001:db8::1]:8443", "https://[2001:db8::1]",
            "https://[fe80::1%25eth0]", "https://2001:db8::1", "https://[2001:::1]"
        }) check(!policy.matchesOrigin(origin), "拒绝非授权或非规范来源");

        for (String url : new String[] {
            startup, "https://example.com", "https://example.com/", "https://example.com:443/a?x=1#b",
            "https://EXAMPLE.COM/中文/😀", "https://example.com/%2F%40evil.test?next=https://evil.test/",
            "https://example.com//evil.test/path", "http://localhost:8080/admin", "https://[2001:db8::1]:8443/admin"
        }) check(policy.allowsPageUrl(url), "原生注册地址仅按精确来源授权");
        for (String url : new String[] {
            null, "", "/admin", "//example.com/admin", "https://evil.test/?next=https://example.com",
            "https://example.com@evil.test/admin", "https://user:password@example.com/admin",
            "https://example.com./admin", "https://sub.example.com/admin", "https://%65xample.com/admin",
            "https://example.com\\evil.test/admin", "https://example.com/a b", "http://127.1/admin",
            "https://example.com:444/admin", "javascript:alert(1)", "blob:https://example.com/id"
        }) check(!policy.allowsPageUrl(url), "拒绝注册地址扩大授权");

        whitelist.clear(); whitelist.add("https://evil.test");
        check(policy.matchesOrigin("https://example.com") && !policy.matchesOrigin("https://evil.test"), "白名单编译为不可变副本");
        SaasOriginPolicy dotted = new SaasOriginPolicy(Collections.singletonList("https://example.com."), "https://example.com./");
        check(dotted.matchesOrigin("https://EXAMPLE.COM.") && !dotted.matchesOrigin("https://example.com"), "DNS 尾点来源独立");
        SaasOriginPolicy mapped = new SaasOriginPolicy(Collections.singletonList("http://[::ffff:127.0.0.1]"), "http://[::ffff:7f00:1]/");
        check(mapped.matchesOrigin("http://[0:0:0:0:0:ffff:7f00:1]:80"), "IPv4-mapped IPv6 同地址匹配");
        check(!mapped.matchesOrigin("http://127.0.0.1") && !mapped.matchesOrigin("http://[::127.0.0.1]"), "不跨地址族或映射身份授权");
        check(!mapped.matchesOrigin("http://[::ffff:127.00.0.1]")
            && !mapped.allowsPageUrl("http://[::ffff:127.00.0.1]/"), "IPv6 嵌入 IPv4 不接受前导零");
        for (String host : new String[] {"[::]", "[::1]", "[2001:db8:0:1:1:1:1:1]", "[2001:db8:1:1:1:1:0:0]", "[1:0:0:2:0:0:3:4]"}) {
            SaasOriginPolicy ipv6 = new SaasOriginPolicy(Collections.singletonList("http://" + host), "http://" + host + "/");
            check(ipv6.matchesOrigin("http://" + host + ":80"), "IPv6 零段及默认端口");
        }
        SaasOriginPolicy zero = new SaasOriginPolicy(Collections.singletonList("http://localhost:0"), "http://localhost:0/");
        check(zero.matchesOrigin("http://localhost:0") && !zero.matchesOrigin("http://localhost"), "端口零保持独立来源");
        SaasOriginPolicy lastPort = new SaasOriginPolicy(Collections.singletonList("http://localhost:65535"), "http://localhost:65535/");
        check(lastPort.matchesOrigin("http://localhost:65535"), "端口上限");

        reject(() -> new SaasOriginPolicy(null, startup));
        reject(() -> new SaasOriginPolicy(Collections.emptyList(), startup));
        reject(() -> new SaasOriginPolicy(Collections.nCopies(129, "https://example.com"), startup));
        reject(() -> new SaasOriginPolicy(wrongEntryType(), startup));
        for (String origin : new String[] {
            null, "", "https://*.example.com", "https://example.com/", "https://example.com?",
            "https://user:password@example.com", "https://%65xample.com", "https://中国.cn",
            "http://127.1", "http://2130706433", "http://0177.0.0.1", "http://example.123",
            "http://example.0x7f", "https://example.com:0443", "https://[fe80::1%eth0]",
            "https://" + repeat('a', 64) + ".com", "https://" + repeat('a', 2049)
        }) reject(() -> new SaasOriginPolicy(Collections.singletonList(origin), startup));
        for (String url : new String[] {null, "", "https://evil.test/", "http://example.com/", "https://user@example.com/"}) {
            reject(() -> new SaasOriginPolicy(Collections.singletonList("https://example.com"), url));
        }
        check(!policy.matchesOrigin("https://example.com" + repeat('a', 2049)), "来源长度上限");
        check(!policy.allowsPageUrl("https://example.com/" + repeat('a', 8192)), "页面地址长度上限");
        System.out.println("Origin 策略自测通过：" + passed + " 项");
    }

    private static String repeat(char value, int count) {
        char[] chars = new char[count]; Arrays.fill(chars, value); return new String(chars);
    }
    @SuppressWarnings({"rawtypes", "unchecked"})
    private static List<String> wrongEntryType() {
        return (List) Collections.singletonList(1);
    }
    private static void check(boolean condition, String label) {
        if (!condition) throw new AssertionError(label + "未通过");
        passed++;
    }
    private static void reject(Runnable action) {
        try { action.run(); }
        catch (IllegalArgumentException error) {
            check(error.getCause() == null && error.getMessage().matches(".*[\u4e00-\u9fff].*"), "中文配置错误包装");
            return;
        }
        throw new AssertionError("未拒绝无效配置");
    }
}
