package com.fingerprint.saas.bridge;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.Map;

/** 安卓授权目录的相对路径和单文件预算；不把网页路径解释为本机绝对路径。 */
public final class SaasFilePolicy {
    public static final int MAX_FILE_BYTES = 16 * 1024 * 1024;
    public static final int MAX_ENTRIES = 1000;
    private SaasFilePolicy() {}

    public static String path(Map<String, Object> args, boolean allowRoot) {
        Object value = args.get("path");
        if (value == null && !args.containsKey("path") && allowRoot) return "";
        if (!(value instanceof String)) throw error("请提供授权目录内的相对路径");
        return path((String) value, allowRoot);
    }

    public static String path(String value, boolean allowRoot) {
        if (value == null || value.length() > 4096) throw error("文件路径无效或过长");
        if (value.isEmpty()) {
            if (allowRoot) return value;
            throw error("文件路径不能为空");
        }
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (c < 32 || c == 127 || c == '\\' || c == ':') throw error("文件路径包含不支持的字符");
            if (Character.isHighSurrogate(c)) {
                if (++i >= value.length() || !Character.isLowSurrogate(value.charAt(i))) throw error("文件路径编码无效");
            } else if (Character.isLowSurrogate(c)) throw error("文件路径编码无效");
        }
        if (value.getBytes(StandardCharsets.UTF_8).length > 4096) throw error("文件路径过长");
        for (String name : value.split("/", -1)) {
            if (name.isEmpty() || name.equals(".") || name.equals("..") ||
                    name.getBytes(StandardCharsets.UTF_8).length > 255) throw error("文件路径必须位于授权目录内");
        }
        return value;
    }

    public static byte[] decodeData(Object value) {
        if (!(value instanceof String)) throw error("请提供文件的 Base64 内容");
        String data = (String) value;
        if (data.length() > ((MAX_FILE_BYTES + 2) / 3) * 4 || data.length() % 4 != 0) {
            throw error("文件内容无效或超过 16 MiB 限制");
        }
        byte[] bytes;
        try { bytes = Base64.getDecoder().decode(data); }
        catch (IllegalArgumentException invalid) { throw error("文件内容不是有效的 Base64 编码"); }
        if (bytes.length > MAX_FILE_BYTES || !Base64.getEncoder().encodeToString(bytes).equals(data)) {
            java.util.Arrays.fill(bytes, (byte) 0);
            throw error("文件内容无效或超过 16 MiB 限制");
        }
        return bytes;
    }

    public static void check(SaasBridgeDispatcher.RequestAuthority authority) {
        if (Thread.currentThread().isInterrupted() || authority == null || !authority.isActiveInBackground()) {
            throw error("页面授权已失效，文件操作已停止");
        }
    }

    public static byte[] read(InputStream input, SaasBridgeDispatcher.RequestAuthority authority) throws IOException {
        if (input == null) throw error("无法打开所选文件");
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        byte[] buffer = new byte[16384];
        try {
            while (true) {
                check(authority);
                int count = input.read(buffer);
                check(authority);
                if (count < 0) return output.toByteArray();
                if (count == 0) {
                    int single = input.read();
                    check(authority);
                    if (single < 0) return output.toByteArray();
                    buffer[0] = (byte) single; count = 1;
                }
                if (output.size() > MAX_FILE_BYTES - count) throw error("文件超过 16 MiB 读取限制");
                output.write(buffer, 0, count);
            }
        } finally { java.util.Arrays.fill(buffer, (byte) 0); }
    }

    public static SaasBridgeDispatcher.NativeRequestException error(String message) {
        return new SaasBridgeDispatcher.NativeRequestException(message);
    }
}
