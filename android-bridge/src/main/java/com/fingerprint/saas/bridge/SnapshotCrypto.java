package com.fingerprint.saas.bridge;

import java.nio.ByteBuffer;
import java.nio.CharBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.SecureRandom;
import java.util.Arrays;
import java.util.Base64;
import java.util.Map;
import javax.crypto.Cipher;
import javax.crypto.Mac;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/** Android API 26+ / JVM 8+ 的无状态快照加密引擎；不保存密码、密钥或快照。 */
public final class SnapshotCrypto {
    public static final int DEFAULT_ITERATIONS = 600_000;
    public static final int MAX_ITERATIONS = 2_000_000;
    public static final int MAX_PLAINTEXT_BYTES = 14 * 1024 * 1024;
    public static final int MAX_CIPHERTEXT_BYTES = 16 * 1024 * 1024;
    public static final int MAX_ENVELOPE_CHARACTERS = ((MAX_CIPHERTEXT_BYTES + 2) / 3) * 4 + 256;
    public static final int MAX_PASSWORD_CHARACTERS = 4096;
    public static final int MAX_ACCOUNT_CHARACTERS = 1024;
    private static final String ALGORITHM = "AES-256-GCM";
    private static final String KDF = "PBKDF2-HMAC-SHA-256";
    private static final String AAD_PREFIX = "fingerprint-manager:v1:";

    /** 字段名和网页 Base64 信封完全一致；标签独立于密文。 */
    public static final class Envelope {
        public final String algorithm, kdf, salt, nonce, ciphertext, tag;
        public final int iterations;

        public Envelope(String algorithm, String kdf, int iterations, String salt,
                        String nonce, String ciphertext, String tag) {
            this.algorithm = algorithm;
            this.kdf = kdf;
            this.iterations = iterations;
            this.salt = salt;
            this.nonce = nonce;
            this.ciphertext = ciphertext;
            this.tag = tag;
        }
    }

    /** 只携带可展示的中文消息，不将底层异常或敏感参数带入响应。 */
    public static final class SnapshotCryptoException extends IllegalArgumentException {
        private static final long serialVersionUID = 1L;
        SnapshotCryptoException(String message) { super(message); }
    }

    public Envelope encryptSnapshot(String accountId, char[] password, Map<String, ?> snapshot) {
        return encryptSnapshot(accountId, password, snapshot, DEFAULT_ITERATIONS);
    }

    public Envelope encryptSnapshot(String accountId, char[] password, Map<String, ?> snapshot,
                                    int iterations) {
        byte[] plaintext = null, passwordBytes = null, key = null, encrypted = null;
        try {
            validateCredentials(accountId, password);
            validateIterations(iterations);
            // 先序列化再验证稳定副本，避免校验与加密之间可变 Map 被修改。
            plaintext = SnapshotJson.encode(snapshot, MAX_PLAINTEXT_BYTES);
            SnapshotJson.validate(SnapshotJson.decode(plaintext), accountId);
            byte[] salt = new byte[16], nonce = new byte[12];
            SecureRandom random = new SecureRandom();
            random.nextBytes(salt);
            random.nextBytes(nonce);
            passwordBytes = passwordUtf8(password);
            key = derive(passwordBytes, salt, iterations);
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
            cipher.updateAAD(aad(accountId));
            encrypted = cipher.doFinal(plaintext);
            Base64.Encoder encoder = Base64.getEncoder();
            return new Envelope(ALGORITHM, KDF, iterations, encoder.encodeToString(salt),
                encoder.encodeToString(nonce), encoder.encodeToString(Arrays.copyOf(encrypted, encrypted.length - 16)),
                encoder.encodeToString(Arrays.copyOfRange(encrypted, encrypted.length - 16, encrypted.length)));
        } catch (SnapshotCryptoException error) {
            throw error;
        } catch (GeneralSecurityException | RuntimeException error) {
            throw failure("快照加密失败，请检查参数或客户端加密支持");
        } finally {
            wipe(plaintext); wipe(passwordBytes); wipe(key); wipe(encrypted);
        }
    }

    public Map<String, Object> decryptSnapshot(String accountId, char[] password, Envelope envelope) {
        byte[] plaintext = null, passwordBytes = null, key = null, encrypted = null;
        try {
            validateCredentials(accountId, password);
            if (envelope == null || !ALGORITHM.equals(envelope.algorithm) || !KDF.equals(envelope.kdf)) {
                throw failure("快照加密参数不受支持");
            }
            validateIterations(envelope.iterations);
            long size = length(envelope.salt) + length(envelope.nonce)
                + length(envelope.ciphertext) + length(envelope.tag);
            if (size > MAX_ENVELOPE_CHARACTERS) throw failure("加密快照超过大小限制");
            byte[] salt = decodeBase64(envelope.salt, 64);
            byte[] nonce = decodeBase64(envelope.nonce, 12);
            byte[] ciphertext = decodeBase64(envelope.ciphertext, MAX_CIPHERTEXT_BYTES);
            byte[] tag = decodeBase64(envelope.tag, 16);
            if (salt.length < 16 || nonce.length != 12 || tag.length != 16 || ciphertext.length < 16) {
                throw failure("加密快照格式无效");
            }
            encrypted = Arrays.copyOf(ciphertext, ciphertext.length + tag.length);
            System.arraycopy(tag, 0, encrypted, ciphertext.length, tag.length);
            passwordBytes = passwordUtf8(password);
            key = derive(passwordBytes, salt, envelope.iterations);
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
            cipher.updateAAD(aad(accountId));
            plaintext = cipher.doFinal(encrypted);
            Map<String, Object> snapshot = SnapshotJson.decode(plaintext);
            SnapshotJson.validate(snapshot, accountId);
            return snapshot;
        } catch (SnapshotCryptoException error) {
            throw error;
        } catch (GeneralSecurityException | RuntimeException error) {
            throw failure("快照解密失败，请核对快照密码与账号");
        } finally {
            wipe(plaintext); wipe(passwordBytes); wipe(key); wipe(encrypted);
        }
    }

    private static long length(String value) {
        if (value == null) throw failure("加密快照格式无效");
        return value.length();
    }

    private static void validateCredentials(String accountId, char[] password) {
        if (accountId == null || accountId.trim().isEmpty() || accountId.length() > MAX_ACCOUNT_CHARACTERS) {
            throw failure("账号标识为空或超过支持范围");
        }
        if (password == null || password.length < 12 || password.length > MAX_PASSWORD_CHARACTERS) {
            throw failure("快照密码需要 12 至 4096 位");
        }
    }

    private static void validateIterations(int iterations) {
        if (iterations < DEFAULT_ITERATIONS || iterations > MAX_ITERATIONS) {
            throw failure("快照加密参数不受支持");
        }
    }

    private static byte[] aad(String accountId) {
        return passwordUtf8((AAD_PREFIX + accountId).toCharArray());
    }

    private static byte[] passwordUtf8(char[] password) {
        // Web TextEncoder 对未配对代理项使用 U+FFFD；不能使用平台默认的问号替换。
        ByteBuffer buffer = null;
        try {
            buffer = StandardCharsets.UTF_8.newEncoder()
                .onMalformedInput(CodingErrorAction.REPLACE).onUnmappableCharacter(CodingErrorAction.REPLACE)
                .replaceWith(new byte[] {(byte) 0xef, (byte) 0xbf, (byte) 0xbd}).encode(CharBuffer.wrap(password));
            byte[] bytes = new byte[buffer.remaining()];
            buffer.get(bytes);
            return bytes;
        } catch (CharacterCodingException error) {
            throw failure("快照密码编码无效");
        } finally {
            if (buffer != null && buffer.hasArray()) wipe(buffer.array());
        }
    }

    /** 按 RFC 8018 直接使用 UTF-8 密码字节，避开 Android 各 PBEKeySpec 提供者的字符差异。 */
    static byte[] derive(byte[] password, byte[] salt, int iterations) throws GeneralSecurityException {
        Mac mac = Mac.getInstance("HmacSHA256");
        mac.init(new SecretKeySpec(password, "HmacSHA256"));
        byte[] block = Arrays.copyOf(salt, salt.length + 4);
        block[block.length - 1] = 1;
        byte[] previous = new byte[32], next = new byte[32], key = null;
        try {
            mac.update(block);
            mac.doFinal(previous, 0);
            key = previous.clone();
            for (int round = 1; round < iterations; round++) {
                mac.update(previous);
                mac.doFinal(next, 0);
                for (int index = 0; index < key.length; index++) key[index] ^= next[index];
                byte[] swap = previous; previous = next; next = swap;
            }
            byte[] result = key;
            key = null;
            return result;
        } finally {
            wipe(block); wipe(previous); wipe(next); wipe(key);
            // JCA 内部状态无法保证擦除，但只在本次调用栈中存活，不缓存或持久化。
        }
    }

    private static byte[] decodeBase64(String value, int maximum) {
        if (value == null || value.length() > ((maximum + 2L) / 3) * 4 + 4) {
            throw failure("加密快照格式无效");
        }
        // 与 atob 一样接受 ASCII 空白和省略的末尾填充，拒绝 URL-safe 字母表及错位填充。
        StringBuilder normalized = new StringBuilder(value.length());
        for (int index = 0; index < value.length(); index++) {
            char c = value.charAt(index);
            if (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f') continue;
            if (!(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z')
                && !(c >= '0' && c <= '9') && c != '+' && c != '/' && c != '=') {
                throw failure("加密快照格式无效");
            }
            normalized.append(c);
        }
        try {
            byte[] bytes = Base64.getDecoder().decode(normalized.toString());
            if (bytes.length > maximum) throw failure("加密快照超过大小限制");
            return bytes;
        } catch (IllegalArgumentException error) {
            if (error instanceof SnapshotCryptoException) throw error;
            throw failure("加密快照格式无效");
        }
    }

    static SnapshotCryptoException failure(String message) { return new SnapshotCryptoException(message); }
    private static void wipe(byte[] value) { if (value != null) Arrays.fill(value, (byte) 0); }
}
