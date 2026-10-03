package com.fingerprint.saas.bridge;

import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.CompletableFuture;

/** 原生消息路由；来源参数只能由持有消息端口的 Chromium 宿主提供。 */
public final class SaasBridgeDispatcher {
    /** 平台实现只暴露已接入的能力；消息宿主必须在执行前重新检查真实页面授权。 */
    public interface NativeBackend {
        boolean supports(String capability);
        Object invoke(String method, Map<String, Object> args);
        default CompletableFuture<Object> invokeAsync(String method, Map<String, Object> args,
                RequestAuthority authority) {
            if (authority == null || !authority.isActive()) throw new NativeRequestException("页面授权已失效，操作已停止");
            return CompletableFuture.completedFuture(invoke(method, args));
        }
    }

    public interface RequestAuthority { boolean isActive(); }

    public static final class NativeRequestException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;
        public NativeRequestException(String message) { super(message); }
    }

    public static final int MAX_MESSAGE_BYTES = 24 * 1024 * 1024;
    private final SaasOriginPolicy policy;
    private final SnapshotCrypto crypto = new SnapshotCrypto();

    public SaasBridgeDispatcher(SaasOriginPolicy policy) {
        if (policy == null) throw new IllegalArgumentException("缺少原生桥来源策略");
        this.policy = policy;
    }

    public String dispatch(String message, String trustedOrigin) {
        return dispatch(message, trustedOrigin, null);
    }

    public String dispatch(String message, String trustedOrigin, NativeBackend backend) {
        Map<String, Object> response = new LinkedHashMap<>();
        response.put("type", "fingerprint-saas-bridge:response");
        response.put("requestId", "");
        try {
            // 不读取消息内的 origin，也不接受网页自行声明的授权状态。
            if (!policy.matchesOrigin(trustedOrigin)) throw invalid("页面来源未在编译白名单中");
            if (message == null || message.length() > MAX_MESSAGE_BYTES) throw invalid("原生桥消息超过大小限制");
            checkUtf8Budget(message);
            byte[] encoded = message.getBytes(StandardCharsets.UTF_8);
            Map<String, Object> request;
            try { request = SnapshotJson.decode(encoded, MAX_MESSAGE_BYTES); }
            finally { java.util.Arrays.fill(encoded, (byte) 0); }
            String id = string(request, "requestId", 128);
            if (!id.matches("[A-Za-z0-9_.:-]+")) throw invalid("原生桥请求标识无效");
            response.put("requestId", id);
            if (!"fingerprint-saas-bridge:request".equals(request.get("type"))) throw invalid("原生桥消息类型无效");
            String method = string(request, "method", 64);
            Map<String, Object> args = object(request.get("args"));
            response.put("result", invoke(method, args, trustedOrigin, backend));
            response.put("ok", true);
        } catch (SnapshotCrypto.SnapshotCryptoException error) {
            response.put("ok", false); response.put("error", error.getMessage());
        } catch (BridgeException error) {
            response.put("ok", false); response.put("error", error.getMessage());
        } catch (NativeRequestException error) {
            response.put("ok", false); response.put("error", error.getMessage());
        } catch (RuntimeException error) {
            response.put("ok", false); response.put("error", "原生桥请求失败，请检查参数后重试");
        }
        try { return new String(SnapshotJson.encode(response, MAX_MESSAGE_BYTES), StandardCharsets.UTF_8); }
        catch (RuntimeException error) {
            response.remove("result"); response.put("ok", false);
            response.put("error", "原生桥响应超过支持范围，请减少快照内容");
            return new String(SnapshotJson.encode(response, MAX_MESSAGE_BYTES), StandardCharsets.UTF_8);
        }
    }

    private static void checkUtf8Budget(String message) {
        int bytes = 0;
        for (int index = 0; index < message.length(); index++) {
            char c = message.charAt(index);
            if (Character.isHighSurrogate(c)) {
                if (index + 1 >= message.length() || !Character.isLowSurrogate(message.charAt(++index))) throw invalid("原生桥消息编码无效");
                bytes += 4;
            } else if (Character.isLowSurrogate(c)) throw invalid("原生桥消息编码无效");
            else bytes += c < 128 ? 1 : c < 2048 ? 2 : 3;
            if (bytes > MAX_MESSAGE_BYTES) throw invalid("原生桥消息超过大小限制");
        }
    }

    public static Map<String, Object> decodeNativeState(String json) {
        if (json == null || json.length() > 65536) throw new NativeRequestException("原生标签状态无效");
        return SnapshotJson.decode(json.getBytes(StandardCharsets.UTF_8), 65536);
    }

    public static Map<String, Object> decodeNativeReply(String json) {
        if (json == null || json.length() > 16 * 1024 * 1024) throw new NativeRequestException("原生存储响应超过大小限制");
        byte[] bytes = json.getBytes(StandardCharsets.UTF_8);
        try { return SnapshotJson.decode(bytes, 16 * 1024 * 1024); }
        finally { java.util.Arrays.fill(bytes, (byte) 0); }
    }

    @SuppressWarnings("unchecked")
    public static String encodeNativePayload(Object value) {
        if (!(value instanceof Map)) throw new NativeRequestException("存储快照格式无效");
        byte[] bytes = SnapshotJson.encode((Map<String, Object>) value, 14 * 1024 * 1024);
        try { return new String(bytes, StandardCharsets.UTF_8); }
        finally { java.util.Arrays.fill(bytes, (byte) 0); }
    }

    private Object invoke(String method, Map<String, Object> args, String origin, NativeBackend backend) {
        if ("getCapabilities".equals(method)) {
            Map<String, Object> capabilities = new LinkedHashMap<>();
            for (String name : new String[] {"tabs", "storage", "fingerprint", "files", "http"}) capabilities.put(name, false);
            capabilities.put("tabs", backend != null && backend.supports("tabs"));
            capabilities.put("storage", backend != null && backend.supports("storage"));
            capabilities.put("fingerprint", backend != null && backend.supports("fingerprint"));
            capabilities.put("crypto", true);
            Map<String, Object> details = new LinkedHashMap<>();
            details.put("version", "1.0"); details.put("origin", origin); details.put("capabilities", capabilities);
            return details;
        }
        String accountId;
        char[] password;
        if ("tabs.list".equals(method) || "tabs.create".equals(method) ||
                "tabs.activate".equals(method) || "tabs.navigate".equals(method) || "tabs.close".equals(method)) {
            if (backend == null || !backend.supports("tabs")) throw invalid("当前客户端尚未提供标签管理能力");
            return backend.invoke(method, args);
        }
        if ("storage.getSnapshot".equals(method) || "storage.writeSnapshot".equals(method)) {
            if (backend == null || !backend.supports("storage")) throw invalid("当前客户端尚未提供存储管理能力");
            return backend.invoke(method, args);
        }
        if ("fingerprint.get".equals(method) || "fingerprint.set".equals(method)) {
            if (backend == null || !backend.supports("fingerprint")) throw invalid("当前客户端尚未提供指纹配置能力");
            return backend.invoke(method, args);
        }
        if (!"crypto.encryptSnapshot".equals(method) && !"crypto.decryptSnapshot".equals(method)) {
            throw invalid("当前客户端尚未提供该原生能力");
        }
        accountId = string(args, "accountId", SnapshotCrypto.MAX_ACCOUNT_CHARACTERS);
        password = string(args, "password", SnapshotCrypto.MAX_PASSWORD_CHARACTERS).toCharArray();
        try {
            if ("crypto.encryptSnapshot".equals(method)) {
                SnapshotCrypto.Envelope value = crypto.encryptSnapshot(accountId, password, object(args.get("snapshot")));
                Map<String, Object> envelope = new LinkedHashMap<>();
                envelope.put("algorithm", value.algorithm); envelope.put("kdf", value.kdf);
                envelope.put("iterations", value.iterations); envelope.put("salt", value.salt);
                envelope.put("nonce", value.nonce); envelope.put("ciphertext", value.ciphertext); envelope.put("tag", value.tag);
                return envelope;
            }
            Map<String, Object> value = object(args.get("envelope"));
            Object count = value.get("iterations");
            if (!(count instanceof Number)) throw invalid("快照加密参数不受支持");
            double number = ((Number) count).doubleValue();
            if (number != Math.rint(number) || number < SnapshotCrypto.DEFAULT_ITERATIONS || number > SnapshotCrypto.MAX_ITERATIONS) {
                throw invalid("快照加密参数不受支持");
            }
            return crypto.decryptSnapshot(accountId, password, new SnapshotCrypto.Envelope(
                string(value, "algorithm", 32), string(value, "kdf", 32), (int) number,
                string(value, "salt", 92), string(value, "nonce", 20),
                string(value, "ciphertext", SnapshotCrypto.MAX_ENVELOPE_CHARACTERS), string(value, "tag", 28)));
        } finally { java.util.Arrays.fill(password, '\u0000'); }
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> object(Object value) {
        if (!(value instanceof Map)) throw invalid("原生桥请求参数无效");
        return (Map<String, Object>) value;
    }

    private static String string(Map<String, Object> value, String name, int max) {
        Object field = value.get(name);
        if (!(field instanceof String) || ((String) field).isEmpty() || ((String) field).length() > max) {
            throw invalid("原生桥请求参数无效");
        }
        return (String) field;
    }

    private static BridgeException invalid(String message) { return new BridgeException(message); }
    private static final class BridgeException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;
        BridgeException(String message) { super(message); }
    }
}
