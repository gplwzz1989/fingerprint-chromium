package com.fingerprint.saas.bridge;

import java.util.Arrays;
import java.util.Base64;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;

/** 使用真实 JCA AES-GCM 验证密文与范围，不模拟 AndroidKeyStore 或磁盘平台。 */
public final class SaasSecureValueSelfTest {
    private static int assertions;
    private static final String ORIGIN = "https://saas.example.test";
    public static void main(String[] args) throws Exception {
        KeyGenerator generator = KeyGenerator.getInstance("AES"); generator.init(256);
        SecretKey secret = generator.generateKey();
        SecretKey different = generator.generateKey();
        for (String text : new String[] {"", "真实安全值", "emoji😀\u0000组合e\u0301", repeat('x', 16384), repeat('中', 5461)}) {
            String envelope = SaasSecureValue.encrypt(secret, ORIGIN, "test.value", text);
            check(text.equals(SaasSecureValue.decrypt(secret, ORIGIN, "test.value", envelope)), "真实密文往返不一致");
            check(envelope.length() <= SaasSecureValue.MAX_ENVELOPE_CHARACTERS, "密文超过预算");
            String repeated = SaasSecureValue.encrypt(secret, ORIGIN, "test.value", text);
            check(!repeated.equals(envelope), "重复加密没有随机 IV");
            rejects(() -> SaasSecureValue.decrypt(different, ORIGIN, "test.value", envelope));
            rejects(() -> SaasSecureValue.decrypt(secret, "http://saas.example.test", "test.value", envelope));
            rejects(() -> SaasSecureValue.decrypt(secret, "https://saas.example.test:444", "test.value", envelope));
            rejects(() -> SaasSecureValue.decrypt(secret, "https://other.test", "test.value", envelope));
            rejects(() -> SaasSecureValue.decrypt(secret, ORIGIN, "other.value", envelope));
            String[] parts = envelope.split(":"); byte[] cipher = Base64.getDecoder().decode(parts[2]); cipher[0] ^= 1;
            String tampered = parts[0] + ":" + parts[1] + ":" + Base64.getEncoder().encodeToString(cipher);
            rejects(() -> SaasSecureValue.decrypt(secret, ORIGIN, "test.value", tampered));
        }
        for (Object key : new Object[] {null, 5, "", "../value", "a/b", "key:other", "账号", repeat('k', 129)}) rejects(() -> SaasSecureValue.key(key));
        check(SaasSecureValue.key(repeat('k', 128)).length() == 128, "键名边界错误");
        for (Object text : new Object[] {null, 5, repeat('x', 16385), repeat('中', 5462), "\ud800", "\udc00"}) rejects(() -> SaasSecureValue.value(text));
        String envelope = SaasSecureValue.encrypt(secret, ORIGIN, "test.value", "仅测试的秘密内容");
        for (String encoded : new String[] {null, "", "2:" + envelope.substring(2), envelope + ":extra", repeat('x', 22001),
                "1:AAAAAAAAAAAAAAAA:YQ", "1:AAAAAAAAAAAAAAAA:AAAA", "1:AAAAAAAAAAAAAAAA:===="}) rejects(() -> SaasSecureValue.decrypt(secret, ORIGIN, "test.value", encoded));
        for (String origin : new String[] {null, "null", "https://saas.example.test/path", "https://u:p@saas.example.test", "file:///data", "https://saas.example.test/"}) rejects(() -> SaasSecureValue.entry(origin, "test.value"));
        check(SaasSecureValue.entry(ORIGIN, "test.value").equals(SaasSecureValue.entry(ORIGIN, "test.value")), "条目标识不稳定");
        check(!SaasSecureValue.entry(ORIGIN, "test.value").equals(SaasSecureValue.entry(ORIGIN, "other.value")), "不同键共享条目");
        check(!SaasSecureValue.entry(ORIGIN, "test.value").equals(SaasSecureValue.entry("https://other.test", "test.value")), "不同来源共享条目");
        check(!SaasSecureValue.entry(ORIGIN, "test.value").contains("saas.example"), "持久索引暴露来源明文");
        rejects(() -> SaasSecureValue.encrypt(null, ORIGIN, "test.value", "仅测试值"));
        System.out.println("安全值真实加密自测通过：" + assertions + " 项");
    }
    private interface Action { void run() throws Exception; }
    private static void rejects(Action action) throws Exception {
        try { action.run(); throw new AssertionError("无效范围或篡改未被拒绝"); }
        catch (SaasBridgeDispatcher.NativeRequestException expected) {
            check(expected.getMessage().matches(".*[\u4e00-\u9fff].*") && !expected.getMessage().contains("仅测试"), "错误非中文或泄露内容");
        }
    }
    private static String repeat(char value, int count) { char[] text = new char[count]; Arrays.fill(text, value); return new String(text); }
    private static void check(boolean value, String message) { assertions++; if (!value) throw new AssertionError(message); }
}
