package com.fingerprint.saas.bridge;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;

/** 仅用于真实消息编解码、真实加密以及网页互操作测试，不替代 Android 宿主。 */
public final class SaasBridgeDispatcherSelfTest {
    private static final String ORIGIN = "https://saas.example.test";
    private static int assertions;

    public static void main(String[] args) throws Exception {
        SaasBridgeDispatcher dispatcher = new SaasBridgeDispatcher(new SaasOriginPolicy(Arrays.asList(ORIGIN), ORIGIN + "/"));
        if (args.length == 1 && "--wire".equals(args[0])) {
            BufferedReader reader = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
            String line;
            while ((line = reader.readLine()) != null) System.out.println(dispatcher.dispatch(line, ORIGIN));
            return;
        }
        Map<String, Object> caps = invoke(dispatcher, "getCapabilities", new LinkedHashMap<>(), ORIGIN);
        check(Boolean.TRUE.equals(caps.get("ok")), "能力查询失败");
        Map<String, Object> flags = object(object(caps.get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("crypto")), "真实加密模块未声明");
        for (String name : new String[] {"tabs", "storage", "fingerprint", "files", "http"}) check(Boolean.FALSE.equals(flags.get(name)), "未实现能力不能声称可用");
        check(Boolean.FALSE.equals(invoke(dispatcher, "getCapabilities", new LinkedHashMap<>(), "https://other.test").get("ok")), "未授权来源获得能力");
        Map<String, Object> forged = new LinkedHashMap<>(); forged.put("origin", ORIGIN);
        check(Boolean.FALSE.equals(invoke(dispatcher, "getCapabilities", forged, "https://other.test").get("ok")), "消息自报来源绕过校验");
        check(Boolean.FALSE.equals(invoke(dispatcher, "tabs.create", new LinkedHashMap<>(), ORIGIN).get("ok")), "未实现能力被执行");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch("[]", ORIGIN)).get("ok")), "数组消息未拒绝");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch("{\"requestId\":\"one\",\"requestId\":\"two\"}", ORIGIN)).get("ok")), "重复字段未拒绝");
        Map<String, Object> snapshot = decode("{\"schema_version\":1,\"account_id\":\"wire-account\",\"cookies\":[],\"local_storage\":{\"标签\":\"消息测试\"},\"session_storage\":{},\"storage_url\":\"https://example.test/\"}");
        Map<String, Object> encrypt = new LinkedHashMap<>();
        encrypt.put("accountId", "wire-account"); encrypt.put("password", "message-wire-test-123"); encrypt.put("snapshot", snapshot);
        Map<String, Object> encrypted = invoke(dispatcher, "crypto.encryptSnapshot", encrypt, ORIGIN);
        check(Boolean.TRUE.equals(encrypted.get("ok")), "消息加密失败");
        Map<String, Object> decrypt = new LinkedHashMap<>();
        decrypt.put("accountId", "wire-account"); decrypt.put("password", "message-wire-test-123"); decrypt.put("envelope", encrypted.get("result"));
        Map<String, Object> decrypted = invoke(dispatcher, "crypto.decryptSnapshot", decrypt, ORIGIN);
        check(Boolean.TRUE.equals(decrypted.get("ok")) && snapshot.equals(decrypted.get("result")), "真实消息加密往返不一致");
        Map<String, Object> envelope = object(encrypted.get("result"));
        for (double count : new double[] {600000.5, 4295567296d, -1d, 2000001d}) {
            envelope.put("iterations", count);
            check(Boolean.FALSE.equals(invoke(dispatcher, "crypto.decryptSnapshot", decrypt, ORIGIN).get("ok")), "派生次数被截断或越界");
        }
        char[] large = new char[SaasBridgeDispatcher.MAX_MESSAGE_BYTES / 3 + 1];
        Arrays.fill(large, '中');
        Map<String, Object> overBudget = decode(dispatcher.dispatch(new String(large), ORIGIN));
        check(Boolean.FALSE.equals(overBudget.get("ok")) && ((String) overBudget.get("error")).contains("大小限制"), "UTF-8 预算未在分配前拒绝消息");
        Map<String, Object> deep = new LinkedHashMap<>();
        deep.putAll(snapshot);
        Object nested = "测试叶节点";
        for (int level = 0; level < 63; level++) {
            Map<String, Object> parent = new LinkedHashMap<>(); parent.put("next", nested); nested = parent;
        }
        deep.put("nested", nested);
        SnapshotCrypto.Envelope deepEncrypted = new SnapshotCrypto().encryptSnapshot("wire-account", "message-wire-test-123".toCharArray(), deep);
        Map<String, Object> deepEnvelope = new LinkedHashMap<>();
        deepEnvelope.put("algorithm", deepEncrypted.algorithm); deepEnvelope.put("kdf", deepEncrypted.kdf);
        deepEnvelope.put("iterations", deepEncrypted.iterations); deepEnvelope.put("salt", deepEncrypted.salt);
        deepEnvelope.put("nonce", deepEncrypted.nonce); deepEnvelope.put("ciphertext", deepEncrypted.ciphertext); deepEnvelope.put("tag", deepEncrypted.tag);
        decrypt.put("envelope", deepEnvelope);
        Map<String, Object> deepResponse = invoke(dispatcher, "crypto.decryptSnapshot", decrypt, ORIGIN);
        check(Boolean.FALSE.equals(deepResponse.get("ok")) && ((String) deepResponse.get("error")).contains("支持范围"), "过深响应未包装为安全错误");
        System.out.println("原生消息路由自测通过：" + assertions + " 项");
    }

    private static Map<String, Object> invoke(SaasBridgeDispatcher dispatcher, String method, Map<String, Object> args, String origin) {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("type", "fingerprint-saas-bridge:request"); request.put("requestId", "test-1"); request.put("method", method); request.put("args", args);
        return decode(dispatcher.dispatch(new String(SnapshotJson.encode(request, SaasBridgeDispatcher.MAX_MESSAGE_BYTES), StandardCharsets.UTF_8), origin));
    }
    private static Map<String, Object> decode(String value) { return SnapshotJson.decode(value.getBytes(StandardCharsets.UTF_8)); }
    @SuppressWarnings("unchecked") private static Map<String, Object> object(Object value) { return (Map<String, Object>) value; }
    private static void check(boolean value, String message) { assertions++; if (!value) throw new AssertionError(message); }
}
