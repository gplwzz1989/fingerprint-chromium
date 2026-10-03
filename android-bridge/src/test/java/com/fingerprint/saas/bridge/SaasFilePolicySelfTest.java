package com.fingerprint.saas.bridge;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.Arrays;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.Map;

/** 真实路径、编码和流边界测试；不代替 Android 文档提供方或设备验收。 */
public final class SaasFilePolicySelfTest {
    private static int assertions;
    private static final SaasBridgeDispatcher.RequestAuthority ACTIVE = new SaasBridgeDispatcher.RequestAuthority() {
        public boolean isActive() { return true; }
        public boolean isActiveInBackground() { return true; }
    };
    public static void main(String[] args) throws Exception {
        for (String path : new String[] {"账号/数据.json", "data.bin", "%2e%2e/file", "😀/配置"}) {
            check(path.equals(SaasFilePolicy.path(path, false)), "合法相对路径被更改");
        }
        for (String path : new String[] {"", "/data", "data/", "a//b", ".", "..", "a/../b", "./data",
                "D:/data", "content://files/x", "a\\b", "a\u0000b", "a\nb", "a\u007fb", "\ud800", "\udc00"}) {
            rejects(() -> SaasFilePolicy.path(path, false));
        }
        check("".equals(SaasFilePolicy.path("", true)), "根目录路径被拒绝");
        Map<String, Object> options = new LinkedHashMap<>();
        check("".equals(SaasFilePolicy.path(options, true)), "默认目录不是授权根目录");
        rejects(() -> SaasFilePolicy.path(options, false));
        options.put("path", null); rejects(() -> SaasFilePolicy.path(options, true));
        options.put("path", 12); rejects(() -> SaasFilePolicy.path(options, true));
        rejects(() -> SaasFilePolicy.path(repeat('中', 86), false));
        check(SaasFilePolicy.path(repeat('中', 85), false).length() == 85, "255 字节文件名边界错误");
        rejects(() -> SaasFilePolicy.path(repeat('a', 4097), false));
        String longPath = repeat('中', 80);
        for (int i = 0; i < 18; i++) longPath += "/" + repeat('中', 80);
        final String overBudget = longPath; rejects(() -> SaasFilePolicy.path(overBudget, false));
        byte[] actual = {0, 1, 2, -1, -128};
        check(Arrays.equals(actual, SaasFilePolicy.decodeData(Base64.getEncoder().encodeToString(actual))), "二进制往返失败");
        check(SaasFilePolicy.decodeData("").length == 0, "空文件不能写入");
        for (String encoded : new String[] {"a", "YQ", "YQ=", "YQ==\n", "YR==", "YQ--", "====", "😃=="}) rejects(() -> SaasFilePolicy.decodeData(encoded));
        rejects(() -> SaasFilePolicy.decodeData(null));
        rejects(() -> SaasFilePolicy.decodeData(repeat('A', ((SaasFilePolicy.MAX_FILE_BYTES + 2) / 3) * 4 + 4)));
        byte[] limit = new byte[SaasFilePolicy.MAX_FILE_BYTES]; limit[limit.length - 1] = 42;
        check(Arrays.equals(limit, SaasFilePolicy.decodeData(Base64.getEncoder().encodeToString(limit))), "最大文件编码边界失败");
        check(Arrays.equals(actual, SaasFilePolicy.read(new ByteArrayInputStream(actual), ACTIVE)), "真实流读取丢失数据");
        check(SaasFilePolicy.read(new ByteArrayInputStream(new byte[0]), ACTIVE).length == 0, "空流读取失败");
        check(SaasFilePolicy.read(new ByteArrayInputStream(limit), ACTIVE).length == limit.length, "最大读取边界失败");
        rejects(() -> SaasFilePolicy.read(new ByteArrayInputStream(new byte[SaasFilePolicy.MAX_FILE_BYTES + 1]), ACTIVE));
        rejects(() -> SaasFilePolicy.read(new ByteArrayInputStream(actual), () -> true));
        rejects(() -> SaasFilePolicy.read(null, ACTIVE));
        final boolean[] active = {true};
        SaasBridgeDispatcher.RequestAuthority revoked = new SaasBridgeDispatcher.RequestAuthority() {
            public boolean isActive() { return active[0]; }
            public boolean isActiveInBackground() { return active[0]; }
        };
        InputStream revoking = new ByteArrayInputStream(actual) {
            @Override public synchronized int read(byte[] buffer, int off, int len) {
                int read = super.read(buffer, off, len); active[0] = false; return read;
            }
        };
        rejects(() -> SaasFilePolicy.read(revoking, revoked));
        InputStream zeroOnce = new ByteArrayInputStream(actual) {
            boolean first = true;
            @Override public synchronized int read(byte[] buffer, int off, int len) {
                if (first) { first = false; return 0; } return super.read(buffer, off, len);
            }
        };
        check(Arrays.equals(actual, SaasFilePolicy.read(zeroOnce, ACTIVE)), "零长度读取未推进或丢失字节");
        Thread.currentThread().interrupt();
        rejects(() -> SaasFilePolicy.check(ACTIVE)); Thread.interrupted();
        System.out.println("安卓文件策略自测通过：" + assertions + " 项");
    }
    private interface Action { void run() throws Exception; }
    private static void rejects(Action action) throws Exception {
        try { action.run(); throw new AssertionError("无效输入或失效授权未被拒绝"); }
        catch (SaasBridgeDispatcher.NativeRequestException expected) { check(expected.getMessage().matches(".*[\u4e00-\u9fff].*"), "错误未使用中文"); }
    }
    private static String repeat(char value, int count) { char[] text = new char[count]; Arrays.fill(text, value); return new String(text); }
    private static void check(boolean value, String message) { assertions++; if (!value) throw new AssertionError(message); }
}
