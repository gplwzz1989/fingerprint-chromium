package com.fingerprint.saas.bridge;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

/** 独立 JVM 自测与标准输入互操作入口，测试密码仅在进程内存中传递。 */
public final class SnapshotCryptoSelfTest {
    private static final SnapshotCrypto ENGINE = new SnapshotCrypto();
    private static final String ACCOUNT = "账号-中文-😀";
    private static final char[] PASSWORD = "测试密码-中文-😀-e\u0301-\u0000-安全".toCharArray();
    private static int passed;

    private SnapshotCryptoSelfTest() { }

    public static void main(String[] args) throws Exception {
        if (args.length == 1 && "--stdio".equals(args[0])) {
            exchange();
            return;
        }
        pbkdfVector(1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b");
        pbkdfVector(2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43");
        pbkdfVector(4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a");
        Map<String, Object> snapshot = snapshot(ACCOUNT);
        SnapshotCrypto.Envelope envelope = ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, snapshot);
        check(envelope.iterations == 600_000, "默认派生次数");
        check(Base64.getDecoder().decode(envelope.salt).length == 16, "盐长度");
        check(Base64.getDecoder().decode(envelope.nonce).length == 12, "随机数长度");
        check(Base64.getDecoder().decode(envelope.tag).length == 16, "标签长度");
        check(snapshot.get("local_storage").equals(ENGINE.decryptSnapshot(ACCOUNT, PASSWORD, envelope).get("local_storage")), "Unicode 往返");
        SnapshotCrypto.Envelope second = ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, snapshot);
        check(!envelope.salt.equals(second.salt) && !envelope.nonce.equals(second.nonce)
            && !envelope.ciphertext.equals(second.ciphertext), "每次使用新盐和随机数");
        reject("错误密码", () -> ENGINE.decryptSnapshot(ACCOUNT, "这是另一个错误的密码123".toCharArray(), envelope));
        reject("跨账号", () -> ENGINE.decryptSnapshot("另一个账号", PASSWORD, envelope));
        byte[] damaged = Base64.getDecoder().decode(envelope.tag); damaged[0] ^= 1;
        final String damagedTag = Base64.getEncoder().encodeToString(damaged);
        reject("标签篡改", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, envelope.nonce, envelope.ciphertext, damagedTag)));
        damaged = Base64.getDecoder().decode(envelope.ciphertext); damaged[0] ^= 1;
        final String damagedCiphertext = Base64.getEncoder().encodeToString(damaged);
        reject("密文篡改", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, envelope.nonce, damagedCiphertext, envelope.tag)));
        reject("空账号", () -> ENGINE.encryptSnapshot("", PASSWORD, snapshot));
        reject("空白账号", () -> ENGINE.encryptSnapshot("  ", PASSWORD, snapshot));
        reject("空密码", () -> ENGINE.encryptSnapshot(ACCOUNT, null, snapshot));
        reject("短密码", () -> ENGINE.encryptSnapshot(ACCOUNT, "12345678901".toCharArray(), snapshot));
        reject("长密码", () -> ENGINE.encryptSnapshot(ACCOUNT, new char[4097], snapshot));
        reject("长账号", () -> ENGINE.encryptSnapshot(repeat('账', 1025), PASSWORD, snapshot));
        reject("空快照", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, null));
        reject("空信封", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD, null));
        reject("不匹配快照账号", () -> ENGINE.encryptSnapshot("另一个账号", PASSWORD, snapshot));
        reject("低派生次数", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, snapshot, 599_999));
        reject("高派生次数", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            new SnapshotCrypto.Envelope(envelope.algorithm, envelope.kdf, 2_000_001,
                envelope.salt, envelope.nonce, envelope.ciphertext, envelope.tag)));
        reject("错误算法", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            new SnapshotCrypto.Envelope("AES-128-GCM", envelope.kdf, envelope.iterations,
                envelope.salt, envelope.nonce, envelope.ciphertext, envelope.tag)));
        reject("错误派生算法", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            new SnapshotCrypto.Envelope(envelope.algorithm, "PBKDF2-SHA-1", envelope.iterations,
                envelope.salt, envelope.nonce, envelope.ciphertext, envelope.tag)));
        for (String invalid : new String[] {"@@@", "A", "AA=", "AA===", "AA-A", "AA_A", "AA\u000bAA", ""}) {
            reject("损坏 Base64", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
                copy(envelope, invalid, envelope.nonce, envelope.ciphertext, envelope.tag)));
        }
        for (int saltLength : new int[] {15, 65}) {
            reject("盐长度限制", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
                copy(envelope, Base64.getEncoder().encodeToString(new byte[saltLength]), envelope.nonce, envelope.ciphertext, envelope.tag)));
        }
        reject("随机数长度限制", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, "", envelope.ciphertext, envelope.tag)));
        reject("标签长度限制", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, envelope.nonce, envelope.ciphertext, "")));
        reject("密文长度限制", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, envelope.nonce, "AA==", envelope.tag)));
        String oversized = repeat('A', SnapshotCrypto.MAX_ENVELOPE_CHARACTERS + 1);
        reject("信封总大小限制", () -> ENGINE.decryptSnapshot(ACCOUNT, PASSWORD,
            copy(envelope, envelope.salt, envelope.nonce, oversized, envelope.tag)));
        Map<String, Object> huge = snapshot(ACCOUNT);
        huge.put("extra", repeat('a', SnapshotCrypto.MAX_PLAINTEXT_BYTES));
        reject("明文大小限制", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, huge));
        Map<String, Object> cyclic = snapshot(ACCOUNT); cyclic.put("cycle", cyclic);
        reject("循环结构", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, cyclic));
        Map<String, Object> deep = snapshot(ACCOUNT);
        Object nested = Collections.emptyMap();
        for (int index = 0; index < 65; index++) nested = Collections.singletonList(nested);
        deep.put("extra", nested);
        reject("深度限制", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, deep));
        Map<String, Object> many = snapshot(ACCOUNT);
        many.put("extra", Collections.nCopies(SnapshotJson.MAX_NODES, null));
        reject("节点数限制", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, many));
        Map<String, Object> broken = snapshot(ACCOUNT); broken.put("local_storage", Collections.singletonMap("a", 1));
        final Map<String, Object> brokenStorage = broken;
        reject("存储值类型", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, brokenStorage));
        broken = snapshot(ACCOUNT); broken.put("cookies", Collections.nCopies(5001, Collections.emptyMap()));
        final Map<String, Object> tooManyCookies = broken;
        reject("Cookie 数量限制", () -> ENGINE.encryptSnapshot(ACCOUNT, PASSWORD, tooManyCookies));
        for (String json : new String[] {"{\"a\":1,\"a\":2}", "{\"a\":01}", "{\"a\":1,}", "{}x", "[]", "{\"a\":\"\\uZZZZ\"}", "{\"a\":1e400}"}) {
            reject("严格 JSON 校验", () -> SnapshotJson.decode(json.getBytes(StandardCharsets.UTF_8)));
        }
        reject("损坏 UTF-8", () -> SnapshotJson.decode(new byte[] {(byte) 0xc0, (byte) 0xaf}));
        SnapshotCrypto.Envelope unpadded = copy(envelope, envelope.salt.replace("=", ""),
            " \n" + envelope.nonce, envelope.ciphertext, envelope.tag.replace("=", ""));
        check(ACCOUNT.equals(ENGINE.decryptSnapshot(ACCOUNT, PASSWORD, unpadded).get("account_id")), "兼容 atob 空白和省略填充");
        Arrays.fill(PASSWORD, '\u0000');
        System.out.println("JVM 自测通过：" + passed + " 项");
    }

    private static void pbkdfVector(int count, String expected) throws GeneralSecurityException {
        byte[] key = SnapshotCrypto.derive("password".getBytes(StandardCharsets.UTF_8), "salt".getBytes(StandardCharsets.UTF_8), count);
        StringBuilder hex = new StringBuilder();
        for (byte b : key) hex.append(String.format(java.util.Locale.ROOT, "%02x", b & 255));
        check(expected.equals(hex.toString()), "PBKDF2 标准向量");
        Arrays.fill(key, (byte) 0);
    }

    static Map<String, Object> snapshot(String account) {
        Map<String, Object> snapshot = new LinkedHashMap<>();
        snapshot.put("schema_version", 1);
        snapshot.put("account_id", account);
        snapshot.put("cookies", new ArrayList<>());
        snapshot.put("local_storage", Collections.singletonMap("中文键😀", "汉字-é-e\u0301-😀-\u0000-\ud800"));
        snapshot.put("session_storage", Collections.singletonMap("会话", "测试"));
        snapshot.put("storage_url", "https://example.com/测试?q=😀");
        return snapshot;
    }

    private static SnapshotCrypto.Envelope copy(SnapshotCrypto.Envelope envelope, String salt, String nonce, String ciphertext, String tag) {
        return new SnapshotCrypto.Envelope(envelope.algorithm, envelope.kdf, envelope.iterations, salt, nonce, ciphertext, tag);
    }

    private static String repeat(char value, int size) { char[] chars = new char[size]; Arrays.fill(chars, value); return new String(chars); }
    private static void check(boolean condition, String label) { if (!condition) throw new AssertionError(label + "未通过"); passed++; }
    private static void reject(String label, Runnable action) {
        try { action.run(); } catch (SnapshotCrypto.SnapshotCryptoException error) {
            check(error.getCause() == null && error.getMessage().matches(".*[\u4e00-\u9fff].*"), label + "中文错误包装");
            return;
        }
        throw new AssertionError(label + "未拒绝");
    }

    @SuppressWarnings("unchecked")
    private static void exchange() throws Exception {
        // 互操作测试专用：不通过命令行或磁盘传递测试密码和解密结果。
        BufferedReader reader = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));
        StringBuilder input = new StringBuilder();
        int c;
        while ((c = reader.read()) != -1 && c != '\n') {
            if (input.length() > SnapshotCrypto.MAX_ENVELOPE_CHARACTERS + 16_384) throw new AssertionError("测试输入过大");
            input.append((char) c);
        }
        Map<String, Object> request = SnapshotJson.decode(input.toString().getBytes(StandardCharsets.UTF_8),
            SnapshotCrypto.MAX_ENVELOPE_CHARACTERS + 16_384);
        String account = (String) request.get("accountId");
        char[] password = ((String) request.get("password")).toCharArray();
        Map<String, ?> response;
        try {
            if ("encrypt".equals(request.get("action"))) {
                Number count = (Number) request.get("iterations");
                SnapshotCrypto.Envelope e = ENGINE.encryptSnapshot(account, password,
                    (Map<String, ?>) request.get("snapshot"), count == null ? 600_000 : count.intValue());
                Map<String, Object> result = new LinkedHashMap<>();
                result.put("algorithm", e.algorithm); result.put("kdf", e.kdf); result.put("iterations", e.iterations);
                result.put("salt", e.salt); result.put("nonce", e.nonce); result.put("ciphertext", e.ciphertext); result.put("tag", e.tag);
                response = result;
            } else {
                Map<String, Object> e = (Map<String, Object>) request.get("envelope");
                response = ENGINE.decryptSnapshot(account, password, new SnapshotCrypto.Envelope(
                    (String) e.get("algorithm"), (String) e.get("kdf"), ((Number) e.get("iterations")).intValue(),
                    (String) e.get("salt"), (String) e.get("nonce"), (String) e.get("ciphertext"), (String) e.get("tag")));
            }
            System.out.write(SnapshotJson.encode(response, SnapshotCrypto.MAX_ENVELOPE_CHARACTERS));
            System.out.flush();
        } catch (SnapshotCrypto.SnapshotCryptoException error) {
            System.err.println(error.getMessage());
            System.exit(2);
        } finally {
            Arrays.fill(password, '\u0000');
        }
    }
}
