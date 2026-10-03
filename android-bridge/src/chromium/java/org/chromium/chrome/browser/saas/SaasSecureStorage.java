package org.chromium.chrome.browser.saas;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;

import com.fingerprint.saas.bridge.SaasBridgeDispatcher;
import com.fingerprint.saas.bridge.SaasSecureValue;

import java.security.Key;
import java.security.KeyStore;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;

/** 原生通用安全存储：只保存来源隔离的密文，不承载 SaaS 用户系统。 */
public final class SaasSecureStorage {
    private static final String ALIAS = "fingerprint.native.secure-value.v1";
    private static final ThreadPoolExecutor WORKER = worker();
    private final SharedPreferences preferences;
    private volatile boolean closed;

    public SaasSecureStorage(Context context) {
        preferences = context.getSharedPreferences("fingerprint_native_secure_values", Context.MODE_PRIVATE);
    }

    private static ThreadPoolExecutor worker() {
        // 所有宿主串行获取/创建同一 Keystore 密钥，不能并发重建导致其他密文失效。
        ThreadPoolExecutor result = new ThreadPoolExecutor(1, 1, 30, TimeUnit.SECONDS,
                new ArrayBlockingQueue<>(4), task -> {
                    Thread thread = new Thread(task, "SaasSecureStorage"); thread.setDaemon(true); return thread;
                });
        result.allowCoreThreadTimeOut(true); return result;
    }

    public CompletableFuture<Object> invoke(String method, Map<String, Object> args, String origin,
            SaasBridgeDispatcher.RequestAuthority authority) {
        if (closed || !authority.isActive()) throw error("页面授权已失效，安全存储操作已停止");
        boolean set = method.equals("secureStorage.set");
        if (!set && !method.equals("secureStorage.get") && !method.equals("secureStorage.remove")) throw error("不支持该安全存储操作");
        for (String field : args.keySet()) if (!field.equals("key") && !(set && field.equals("value"))) throw error("安全存储参数包含不支持的字段");
        String key = SaasSecureValue.key(args.get("key"));
        String value = set ? SaasSecureValue.value(args.get("value")) : null;
        Work work = new Work(method, key, value, origin, authority);
        try { WORKER.execute(work); }
        catch (RejectedExecutionException busy) { work.result.completeExceptionally(error("安全存储操作繁忙，请稍后重试")); }
        return work.result;
    }

    public void revoke() {
        for (Runnable task : WORKER.getQueue().toArray(new Runnable[0])) {
            Work work = (Work) task;
            if (work.owner == this && WORKER.getQueue().remove(work)) work.cancel();
        }
    }
    public void close() { closed = true; revoke(); }

    private final class Work implements Runnable {
        final SaasSecureStorage owner = SaasSecureStorage.this;
        final String method, key, value, origin;
        final SaasBridgeDispatcher.RequestAuthority authority;
        final CompletableFuture<Object> result = new CompletableFuture<>();
        Work(String method, String key, String value, String origin, SaasBridgeDispatcher.RequestAuthority authority) {
            this.method = method; this.key = key; this.value = value; this.origin = origin; this.authority = authority;
        }
        void check() {
            if (closed || Thread.currentThread().isInterrupted() || result.isDone() || !authority.isActiveInBackground()) throw error("页面授权已失效，安全存储操作已停止");
        }
        void cancel() { result.completeExceptionally(error("安全存储操作已取消")); }
        @Override public void run() {
            try {
                check();
                String entry = SaasSecureValue.entry(origin, key);
                Map<String, Object> reply = new LinkedHashMap<>();
                if (method.equals("secureStorage.get")) {
                    String envelope = preferences.getString(entry, null);
                    String plain = envelope == null ? null : SaasSecureValue.decrypt(secret(false), origin, key, envelope);
                    check(); reply.put("value", plain);
                } else if (method.equals("secureStorage.set")) {
                    Map<String, ?> saved = preferences.getAll();
                    if (!saved.containsKey(entry)) {
                        String prefix = SaasSecureValue.originPrefix(origin);
                        int scoped = 0;
                        for (String name : saved.keySet()) if (name.startsWith(prefix)) scoped++;
                        if (scoped >= 16 || saved.size() >= 256) throw error("安全存储条目已达上限，请移除不再使用的值");
                    }
                    check();
                    String envelope = SaasSecureValue.encrypt(secret(true), origin, key, value);
                    check();
                    if (!preferences.edit().putString(entry, envelope).commit()) throw error("安全存储写入未完成，请重试");
                    check(); reply.put("ok", true);
                } else {
                    check();
                    // 仅清除当前来源的逻辑条目，不删除文件或共享密钥，也不触及其他来源。
                    if (!preferences.edit().remove(entry).commit()) throw error("安全存储清除未完成，请重试");
                    check(); reply.put("ok", true);
                }
                result.complete(reply);
            } catch (SaasBridgeDispatcher.NativeRequestException failure) { result.completeExceptionally(failure); }
            catch (Exception failure) { result.completeExceptionally(error("安全存储不可用，请检查设备密钥状态后重试")); }
        }
    }

    private static SecretKey secret(boolean create) throws Exception {
        KeyStore store = KeyStore.getInstance("AndroidKeyStore"); store.load(null);
        Key key = store.getKey(ALIAS, null);
        if (key != null) {
            if (!(key instanceof SecretKey) || !"AES".equals(key.getAlgorithm())) throw error("安全存储密钥类型不受支持");
            return (SecretKey) key;
        }
        if (!create) throw error("安全存储密钥已失效，请重新登录");
        KeyGenerator generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
        generator.init(new KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setKeySize(256).setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true).build());
        return generator.generateKey();
    }

    private static SaasBridgeDispatcher.NativeRequestException error(String message) { return new SaasBridgeDispatcher.NativeRequestException(message); }
}
