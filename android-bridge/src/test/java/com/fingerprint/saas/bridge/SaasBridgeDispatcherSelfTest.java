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
        testNativeRouting(dispatcher);
        testFingerprintRouting(dispatcher);
        testFileRouting(dispatcher);
        testHttpRouting(dispatcher);
        System.out.println("原生消息路由自测通过：" + assertions + " 项");
    }

    private static Map<String, Object> invoke(SaasBridgeDispatcher dispatcher, String method, Map<String, Object> args, String origin) {
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("type", "fingerprint-saas-bridge:request"); request.put("requestId", "test-1"); request.put("method", method); request.put("args", args);
        return decode(dispatcher.dispatch(new String(SnapshotJson.encode(request, SaasBridgeDispatcher.MAX_MESSAGE_BYTES), StandardCharsets.UTF_8), origin));
    }
    private static Map<String, Object> decode(String value) { return SnapshotJson.decode(value.getBytes(StandardCharsets.UTF_8)); }

    /** 只核对路由授权，不以测试探针代替真实指纹效果。 */
    private static void testFingerprintRouting(SaasBridgeDispatcher dispatcher) {
        final int[] calls = {0};
        SaasBridgeDispatcher.NativeBackend probe = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return "fingerprint".equals(capability); }
            public Object invoke(String method, Map<String, Object> args) { calls[0]++; return args; }
        };
        Map<String, Object> request = decode("{\"type\":\"fingerprint-saas-bridge:request\",\"requestId\":\"fp-1\",\"args\":{\"tabId\":\"account-01\"}}");
        for (String method : new String[] {"fingerprint.get", "fingerprint.set"}) {
            request.put("method", method);
            String message = SaasBridgeDispatcher.encodeNativePayload(request);
            check(Boolean.TRUE.equals(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("ok")), "指纹方法未路由");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN)).get("ok")), "未绑定仍执行指纹方法");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, "https://other.test", probe)).get("ok")), "指纹方法绕过来源校验");
        }
        check(calls[0] == 2, "拒绝请求到达指纹平台层");
        request.put("method", "fingerprint.replaceProfile");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("ok")), "未知指纹方法被执行");
        check(calls[0] == 2, "未知指纹方法进入平台层");
        request.put("method", "getCapabilities");
        Map<String, Object> flags = object(object(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("fingerprint")) && Boolean.FALSE.equals(flags.get("files")), "指纹能力与文件能力混淆");
    }

    /** 仅验证路由边界的探针，不模拟 Android 标签或替代真实平台验收。 */
    private static void testNativeRouting(SaasBridgeDispatcher dispatcher) {
        final int[] calls = {0};
        SaasBridgeDispatcher.NativeBackend probe = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return "tabs".equals(capability); }
            public Object invoke(String method, Map<String, Object> args) { calls[0]++; return args; }
        };
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("type", "fingerprint-saas-bridge:request"); request.put("requestId", "native-1");
        request.put("args", decode("{\"tabId\":\"account-01\"}"));
        for (String method : new String[] {"tabs.list", "tabs.create", "tabs.activate", "tabs.navigate", "tabs.close"}) {
            request.put("method", method);
            String message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
            Map<String, Object> response = decode(dispatcher.dispatch(message, ORIGIN, probe));
            check(Boolean.TRUE.equals(response.get("ok")) && request.get("args").equals(response.get("result")), "原生标签路由或参数不一致");
        }
        check(calls[0] == 5, "允许的标签操作未全部路由");
        request.put("method", "getCapabilities");
        String message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
        Map<String, Object> flags = object(object(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("tabs")) && Boolean.FALSE.equals(flags.get("storage")), "未按已绑定的平台能力声明");
        request.put("method", "tabs.close");
        message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, "https://other.test", probe)).get("ok")), "原生路由未拒绝未授权来源");
        check(calls[0] == 5, "未授权请求到达平台层");
        request.put("method", "tabs.destroyEverything");
        message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("ok")) && calls[0] == 5, "未知方法到达平台层");
        SaasBridgeDispatcher.NativeBackend denied = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return false; }
            public Object invoke(String method, Map<String, Object> args) { throw new AssertionError("能力不可用时不应调用"); }
        };
        request.put("method", "tabs.list");
        message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN, denied)).get("ok")), "能力撤销后仍到达平台层");
        SaasBridgeDispatcher.NativeBackend failed = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return true; }
            public Object invoke(String method, Map<String, Object> args) { throw new IllegalStateException("开发者底层细节"); }
        };
        Map<String, Object> failure = decode(dispatcher.dispatch(message, ORIGIN, failed));
        check(Boolean.FALSE.equals(failure.get("ok")) && !((String) failure.get("error")).contains("开发者底层细节"), "底层异常直接泄露到网页");
        check("account-01".equals(SaasBridgeDispatcher.decodeNativeState("{\"account_id\":\"account-01\"}").get("account_id")), "原生状态编解码失败");
        testStorageRouting(dispatcher);
    }

    /** 仅验证存储路由与编解码边界，不模拟 CookieManager 或 Android 页面。 */
    private static void testStorageRouting(SaasBridgeDispatcher dispatcher) {
        final int[] calls = {0};
        SaasBridgeDispatcher.NativeBackend probe = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return "storage".equals(capability); }
            public Object invoke(String method, Map<String, Object> args) { calls[0]++; return args; }
        };
        Map<String, Object> args = decode("{\"tabId\":\"account-01\"}");
        Map<String, Object> request = new LinkedHashMap<>();
        request.put("type", "fingerprint-saas-bridge:request"); request.put("requestId", "storage-1");
        request.put("args", args);
        for (String method : new String[] {"storage.getSnapshot", "storage.writeSnapshot"}) {
            request.put("method", method);
            String message = new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8);
            check(Boolean.TRUE.equals(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("ok")), "存储操作未路由");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN)).get("ok")), "未绑定存储平台仍被执行");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, "https://other.test", probe)).get("ok")), "存储来源校验被绕过");
        }
        check(calls[0] == 2, "拒绝请求进入存储平台层");
        request.put("method", "storage.clearAllProfiles");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(
                new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8), ORIGIN, probe)).get("ok")), "未知存储方法被执行");
        check(calls[0] == 2, "未知存储方法进入平台层");
        request.put("method", "getCapabilities");
        Map<String, Object> flags = object(object(decode(dispatcher.dispatch(
                new String(SnapshotJson.encode(request, 65536), StandardCharsets.UTF_8), ORIGIN, probe)).get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("storage")) && Boolean.FALSE.equals(flags.get("tabs")), "未按绑定能力声明存储");
        check(probe.invokeAsync("storage.getSnapshot", args, () -> true).join().equals(args), "默认异步适配未保留结果");
        try { probe.invokeAsync("storage.writeSnapshot", args, () -> false); throw new AssertionError("失效授权未拒绝"); }
        catch (SaasBridgeDispatcher.NativeRequestException expected) { check(true, "授权拒绝"); }
        check(calls[0] == 3, "失效授权仍执行异步操作");
        Map<String, Object> data = decode("{\"ok\":true,\"result\":{\"local_storage\":{\"__proto__\":\"原型键\"}}}");
        check(SaasBridgeDispatcher.decodeNativeReply(SaasBridgeDispatcher.encodeNativePayload(data)).equals(data), "原生 JSON 往返丢失保留键");
        char[] large = new char[70000]; Arrays.fill(large, '测');
        Map<String, Object> big = new LinkedHashMap<>(); big.put("value", new String(large));
        check(SaasBridgeDispatcher.decodeNativeReply(SaasBridgeDispatcher.encodeNativePayload(big)).equals(big), "存储响应误用小状态的 64 KiB 限制");
    }
    /** 只验证文件白名单分发，不用探针替代系统目录授权和真实文件读写。 */
    private static void testFileRouting(SaasBridgeDispatcher dispatcher) {
        final int[] calls = {0};
        SaasBridgeDispatcher.NativeBackend probe = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return "files".equals(capability); }
            public Object invoke(String method, Map<String, Object> args) { calls[0]++; return args; }
        };
        Map<String, Object> request = decode("{\"type\":\"fingerprint-saas-bridge:request\",\"requestId\":\"files-1\",\"args\":{\"path\":\"账号/数据.json\"}}");
        for (String method : new String[] {"files.list", "files.read", "files.write"}) {
            request.put("method", method);
            String message = SaasBridgeDispatcher.encodeNativePayload(request);
            check(Boolean.TRUE.equals(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("ok")), "文件方法未路由");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN)).get("ok")), "未绑定仍执行文件方法");
            check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, "https://other.test", probe)).get("ok")), "文件方法绕过来源校验");
        }
        check(calls[0] == 3, "拒绝的文件请求进入平台层");
        request.put("method", "files.delete");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("ok")) && calls[0] == 3, "未授权删除进入平台层");
        request.put("method", "getCapabilities");
        Map<String, Object> flags = object(object(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("files")) && Boolean.FALSE.equals(flags.get("tabs")), "文件能力未按实际绑定声明");
        check(!((SaasBridgeDispatcher.RequestAuthority) () -> true).isActiveInBackground(), "默认授权允许后台访问界面对象");
    }
    /** 只验证网络分发边界，真实 HTTP 请求由 Chromium 网络层另行验收。 */
    private static void testHttpRouting(SaasBridgeDispatcher dispatcher) {
        final int[] calls = {0};
        SaasBridgeDispatcher.NativeBackend probe = new SaasBridgeDispatcher.NativeBackend() {
            public boolean supports(String capability) { return "http".equals(capability); }
            public Object invoke(String method, Map<String, Object> args) { calls[0]++; return args; }
        };
        Map<String, Object> request = decode("{\"type\":\"fingerprint-saas-bridge:request\",\"requestId\":\"http-1\",\"method\":\"http.request\",\"args\":{\"url\":\"https://example.test/\"}}");
        String message = SaasBridgeDispatcher.encodeNativePayload(request);
        check(Boolean.TRUE.equals(decode(dispatcher.dispatch(message, ORIGIN, probe)).get("ok")), "HTTP 请求未路由");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, ORIGIN)).get("ok")), "未绑定仍执行 HTTP");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(message, "https://other.test", probe)).get("ok")), "HTTP 绕过来源校验");
        check(calls[0] == 1, "被拒 HTTP 进入平台层");
        request.put("method", "http.openSocket");
        check(Boolean.FALSE.equals(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("ok")) && calls[0] == 1, "未知网络能力被执行");
        request.put("method", "getCapabilities");
        Map<String, Object> flags = object(object(decode(dispatcher.dispatch(SaasBridgeDispatcher.encodeNativePayload(request), ORIGIN, probe)).get("result")).get("capabilities"));
        check(Boolean.TRUE.equals(flags.get("http")) && Boolean.FALSE.equals(flags.get("files")), "HTTP 能力声明错误");
    }
    @SuppressWarnings("unchecked") private static Map<String, Object> object(Object value) { return (Map<String, Object>) value; }
    private static void check(boolean value, String message) { assertions++; if (!value) throw new AssertionError(message); }
}
