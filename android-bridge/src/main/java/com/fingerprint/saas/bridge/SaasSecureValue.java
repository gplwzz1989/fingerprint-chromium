package com.fingerprint.saas.bridge;

import java.nio.ByteBuffer;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import javax.crypto.Cipher;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/** 通用安全值编码；不识别登录、令牌或账号业务，平台密钥只能由原生宿主提供。 */
public final class SaasSecureValue {
    public static final int MAX_VALUE_BYTES = 16384;
    public static final int MAX_ENVELOPE_CHARACTERS = 22000;
    private SaasSecureValue() {}

    public static String key(Object key) {
        if (!(key instanceof String) || !((String) key).matches("[A-Za-z0-9._-]{1,128}")) throw error("安全存储键名无效");
        return (String) key;
    }

    public static String value(Object value) {
        if (!(value instanceof String) || ((String) value).length() > MAX_VALUE_BYTES) throw error("安全存储值必须为不超过 16 KiB 的文本");
        String text = (String) value;
        for (int i = 0; i < text.length(); i++) {
            char c = text.charAt(i);
            if (Character.isHighSurrogate(c)) {
                if (++i >= text.length() || !Character.isLowSurrogate(text.charAt(i))) throw error("安全存储文本编码无效");
            } else if (Character.isLowSurrogate(c)) throw error("安全存储文本编码无效");
        }
        if (text.getBytes(StandardCharsets.UTF_8).length > MAX_VALUE_BYTES) throw error("安全存储值超过 16 KiB 限制");
        return text;
    }

    public static String originPrefix(String origin) {
        validateOrigin(origin);
        return digest(origin) + ".";
    }

    public static String entry(String origin, String key) { return originPrefix(origin) + digest(key(key)); }

    private static void validateOrigin(String origin) {
        try { new SaasOriginPolicy(Collections.singletonList(origin), origin + "/"); }
        catch (RuntimeException invalid) { throw error("安全存储页面来源无效"); }
    }

    private static String digest(String value) {
        try {
            return Base64.getUrlEncoder().withoutPadding().encodeToString(
                    MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8)));
        } catch (Exception invalid) { throw error("无法识别安全存储范围"); }
    }

    private static byte[] aad(String origin, String key) {
        validateOrigin(origin);
        return ("fingerprint-secure-value-v1\u0000" + origin + "\u0000" + key(key)).getBytes(StandardCharsets.UTF_8);
    }

    public static String encrypt(SecretKey secret, String origin, String key, String value) {
        byte[] associated = aad(origin, key);
        byte[] plain = value(value).getBytes(StandardCharsets.UTF_8);
        byte[] encrypted = null;
        try {
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            // AndroidKeyStore 自行生成随机 IV；不能向加密过程注入固定 IV。
            cipher.init(Cipher.ENCRYPT_MODE, secret);
            cipher.updateAAD(associated);
            encrypted = cipher.doFinal(plain);
            byte[] iv = cipher.getIV();
            if (iv == null || iv.length != 12) throw error("安全存储加密参数不受支持");
            return "1:" + Base64.getEncoder().encodeToString(iv) + ":" + Base64.getEncoder().encodeToString(encrypted);
        } catch (SaasBridgeDispatcher.NativeRequestException invalid) { throw invalid; }
        catch (Exception invalid) { throw error("安全存储加密失败，请检查设备密钥状态"); }
        finally { Arrays.fill(plain, (byte) 0); Arrays.fill(associated, (byte) 0); if (encrypted != null) Arrays.fill(encrypted, (byte) 0); }
    }

    public static String decrypt(SecretKey secret, String origin, String key, String envelope) {
        byte[] associated = aad(origin, key);
        byte[] iv = null, encrypted = null, plain = null;
        try {
            if (envelope == null || envelope.length() > MAX_ENVELOPE_CHARACTERS) throw error("安全存储密文无效或过大");
            String[] parts = envelope.split(":", -1);
            if (parts.length != 3 || !parts[0].equals("1") || parts[1].length() != 16) throw error("安全存储密文格式不受支持");
            iv = decode(parts[1]); encrypted = decode(parts[2]);
            if (iv.length != 12 || encrypted.length < 16 || encrypted.length > MAX_VALUE_BYTES + 16) throw error("安全存储密文长度无效");
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, secret, new GCMParameterSpec(128, iv));
            cipher.updateAAD(associated);
            plain = cipher.doFinal(encrypted);
            return StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(plain)).toString();
        } catch (SaasBridgeDispatcher.NativeRequestException invalid) { throw invalid; }
        catch (Exception invalid) { throw error("安全存储内容无法解密，请重新登录或写入新值"); }
        finally {
            Arrays.fill(associated, (byte) 0);
            if (iv != null) Arrays.fill(iv, (byte) 0);
            if (encrypted != null) Arrays.fill(encrypted, (byte) 0);
            if (plain != null) Arrays.fill(plain, (byte) 0);
        }
    }

    private static byte[] decode(String value) {
        byte[] bytes = Base64.getDecoder().decode(value);
        if (!Base64.getEncoder().encodeToString(bytes).equals(value)) { Arrays.fill(bytes, (byte) 0); throw error("安全存储密文编码无效"); }
        return bytes;
    }

    private static SaasBridgeDispatcher.NativeRequestException error(String message) { return new SaasBridgeDispatcher.NativeRequestException(message); }
}
