package com.fingerprint.saas.bridge;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.net.URI;
import java.net.URISyntaxException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import javax.net.ssl.SSLException;

/** 独立 HTTP 辅助模块；宿主仍须校验 Origin 与账号权限，并在后台执行请求。 */
public final class SaasHttpClient {
    public static final int DEFAULT_TIMEOUT_MILLIS = 30_000;
    public static final int MAX_REQUEST_BYTES = 64 * 1024 * 1024;
    public static final int MAX_RESPONSE_BYTES = 16 * 1024 * 1024;
    public static final int MAX_REDIRECTS = 5;
    public static final int MAX_URL_CHARACTERS = 8192;
    public static final int MAX_HEADER_COUNT = 100;
    public static final int MAX_HEADER_NAME_CHARACTERS = 128;
    public static final int MAX_HEADER_VALUE_CHARACTERS = 8192;
    public static final int MAX_HEADER_CHARACTERS = 32768;
    public static final int MAX_RESPONSE_HEADER_VALUE_UTF8_BYTES = 8192;
    public static final int MAX_RESPONSE_HEADER_UTF8_BYTES = 32768;
    private static final ScheduledThreadPoolExecutor DEADLINES = deadlineExecutor();

    private static ScheduledThreadPoolExecutor deadlineExecutor() {
        ScheduledThreadPoolExecutor executor = new ScheduledThreadPoolExecutor(2, new ThreadFactory() {
            @Override public Thread newThread(Runnable action) {
                Thread thread = new Thread(action, "saas-http-deadline");
                thread.setDaemon(true);
                return thread;
            }
        });
        executor.setRemoveOnCancelPolicy(true);
        return executor;
    }

    /** 不向调用方暴露原始异常、URL、请求头或正文。 */
    public static final class SaasHttpException extends IOException {
        private static final long serialVersionUID = 1L;
        private SaasHttpException(String message) { super(message); }
    }

    /** 请求体防御性复制；显式 Cookie/Authorization 头不依赖浏览器凭据。 */
    public static final class Request {
        private final String method;
        private final URI uri;
        private final Map<String, String> headers;
        private final byte[] body;
        private final int timeoutMillis;

        public Request(String method, String url, Map<String, String> headers, byte[] body,
                       boolean includeCredentials) throws SaasHttpException {
            this(method, url, headers, body, includeCredentials, DEFAULT_TIMEOUT_MILLIS);
        }

        public Request(String method, String url, Map<String, String> headers, byte[] body,
                       boolean includeCredentials, int timeoutMillis) throws SaasHttpException {
            if (includeCredentials) throw failure("当前模块不支持自动携带浏览器凭据，请显式提供授权或 Cookie 请求头");
            if (!supportedMethod(method)) throw failure("当前 HTTP 实现不支持该请求方法，支持 GET、POST、PUT、DELETE、HEAD、OPTIONS、TRACE");
            if (timeoutMillis < 1 || timeoutMillis > DEFAULT_TIMEOUT_MILLIS) throw failure("请求超时须为 1 至 30000 毫秒");
            if (body != null && body.length > MAX_REQUEST_BYTES) throw failure("HTTP 请求正文不能超过 64 MiB");
            if (body != null && body.length > 0 && ("GET".equals(method) || "HEAD".equals(method) || "TRACE".equals(method))) {
                // 保留既有请求模型对 GET/HEAD/TRACE 正文的限制，不能隐式改写方法。
                throw failure("当前 HTTP 实现不支持 GET、HEAD 或 TRACE 携带非空正文");
            }
            this.uri = parseUrl(url);
            this.method = method;
            this.headers = requestHeaders(headers);
            this.body = body == null ? new byte[0] : body.clone();
            this.timeoutMillis = timeoutMillis;
        }

        public String getMethod() { return method; }
        public String getUrl() { return uri.toASCIIString(); }
        public Map<String, String> getHeaders() { return headers; }
        public byte[] getBody() { return body.clone(); }
        public int getTimeoutMillis() { return timeoutMillis; }
        @Override public String toString() { return "HTTP 请求（敏感内容已隐藏）"; }
    }

    /** 保留重复响应头和二进制正文；返回的集合不可变，正文 getter 返回副本。 */
    public static final class Response {
        private final int status;
        private final String url;
        private final Map<String, List<String>> headers;
        private final byte[] body;
        private Response(int status, URI uri, Map<String, List<String>> headers, byte[] body) {
            this.status = status;
            this.url = uri.toASCIIString();
            this.headers = headers;
            this.body = body;
        }
        public int getStatus() { return status; }
        public String getUrl() { return url; }
        public Map<String, List<String>> getHeaders() { return headers; }
        public byte[] getBody() { return body.clone(); }
        @Override public String toString() { return "HTTP 响应（敏感内容已隐藏）"; }
    }

    public Call newCall(Request request) throws SaasHttpException {
        if (request == null) throw failure("HTTP 请求不能为空");
        return new Call(request);
    }

    /** 单次执行句柄；cancel 可在执行前或另一线程调用，取消网络任务并关闭响应正文。 */
    public static final class Call {
        private final Request request;
        private CompletableFuture<?> activeFuture;
        private InputStream activeBody;
        private boolean started, finished, cancelled, timedOut;
        private long deadline;
        private Call(Request request) { this.request = request; }

        public void cancel() { stop(false); }
        public synchronized boolean isCancelled() { return cancelled; }
        private void stop(boolean timeout) {
            CompletableFuture<?> future;
            InputStream body;
            synchronized (this) {
                if (finished) return;
                if (timeout) timedOut = true;
                else cancelled = true;
                future = activeFuture;
                body = activeBody;
            }
            cancelFuture(future);
            closeBody(body);
        }

        private synchronized int remaining() throws SaasHttpException {
            if (cancelled) throw failure("HTTP 请求已取消");
            long nanos = deadline - System.nanoTime();
            if (timedOut || nanos <= 0) throw failure("HTTP 请求超时，请稍后重试");
            return (int) Math.max(1, Math.min(request.timeoutMillis, TimeUnit.NANOSECONDS.toMillis(nanos) + 1));
        }

        public Response execute() throws SaasHttpException {
            synchronized (this) {
                if (started) throw failure("HTTP 请求句柄只能执行一次");
                started = true;
                deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(request.timeoutMillis);
            }
            ScheduledFuture<?> timer = DEADLINES.schedule(new Runnable() {
                @Override public void run() { stop(true); }
            }, request.timeoutMillis, TimeUnit.MILLISECONDS);
            try {
                Response response = perform();
                synchronized (this) { remaining(); finished = true; }
                return response;
            } catch (SaasHttpException error) {
                throw error;
            } catch (TimeoutException error) {
                stop(true);
                throw failure("HTTP 请求超时，请稍后重试");
            } catch (InterruptedException error) {
                Thread.currentThread().interrupt();
                stop(false);
                throw failure("HTTP 请求已中断并取消");
            } catch (ExecutionException | InvocationTargetException error) {
                remaining();
                throw transportFailure(error.getCause());
            } catch (ReflectiveOperationException error) {
                remaining();
                throw failure("当前运行时不支持 JDK11 HTTP 后端，不能安全执行 HTTP 请求");
            } catch (SSLException error) {
                remaining();
                throw failure("HTTPS 安全连接失败，请检查服务器证书与系统 TLS 配置");
            } catch (IOException | RuntimeException error) {
                remaining();
                throw failure("HTTP 请求失败，请检查网络、服务器或系统证书配置");
            } finally {
                timer.cancel(false);
                synchronized (this) { finished = true; }
                releaseActive();
            }
        }

        private Response perform() throws IOException, ReflectiveOperationException, InterruptedException, ExecutionException, TimeoutException {
            remaining();
            JdkHttpBackend backend = new JdkHttpBackend(remaining());
            URI uri = request.uri;
            String method = request.method;
            byte[] body = request.body;
            Map<String, String> headers = new LinkedHashMap<String, String>(request.headers);
            for (int redirects = 0; ; redirects++) {
                remaining();
                // 每个逻辑请求使用独立客户端，不配置 CookieHandler/Authenticator，也不读取或修改全局凭据处理器。
                // 独立客户端不启用自动认证重试；使用有界请求副本，保留非 2xx 响应。
                CompletableFuture<?> future = backend.sendAsync(uri, method, headers, body, remaining());
                synchronized (this) { activeFuture = future; }
                // 响应可能在取消或清理后才完成，迟到的正文也必须关闭，不能重新挂到已结束的句柄。
                future.whenComplete((response, error) -> {
                    if (response == null) return;
                    try { adoptBody(future, backend.body(response)); }
                    catch (ReflectiveOperationException | SaasHttpException ignored) { /* 正常执行路径统一返回中文错误。 */ }
                });
                try {
                    remaining();
                    Object response = future.get(remaining(), TimeUnit.MILLISECONDS);
                    InputStream input = backend.body(response);
                    adoptBody(future, input);
                    remaining();
                    int status = backend.status(response);
                    if (status < 100 || status > 599) throw failure("服务器返回了无效的 HTTP 状态");
                    Map<String, List<String>> responseHeaders = responseHeaders(backend.headers(response));
                    String location = firstHeader(responseHeaders, "location");
                    if (redirectStatus(status) && location != null) {
                        if (redirects >= MAX_REDIRECTS) throw failure("HTTP 重定向次数超过限制");
                        URI next;
                        try { next = parseUrl(uri.resolve(new URI(location)).toASCIIString()); }
                        catch (URISyntaxException | IllegalArgumentException error) { throw failure("HTTP 重定向地址无效"); }
                        if ("https".equalsIgnoreCase(uri.getScheme()) && "http".equalsIgnoreCase(next.getScheme())) {
                            throw failure("不允许从 HTTPS 重定向到未加密的 HTTP");
                        }
                        if (!sameOrigin(uri, next)) {
                            // 一旦跨来源移除凭据，即使后续回到原来源也不能重新附加。
                            headers.remove("authorization"); headers.remove("cookie");
                            headers.remove("cookie2"); headers.remove("proxy-authorization");
                        }
                        if ((status == 303 && !"HEAD".equals(method)) || ((status == 301 || status == 302) && "POST".equals(method))) {
                            method = "GET"; body = new byte[0];
                            headers.remove("content-type"); headers.remove("content-encoding"); headers.remove("content-language");
                        }
                        uri = next;
                        continue;
                    }
                    boolean noBody = "HEAD".equals(method) || status == 204 || status == 205 || status == 304;
                    long declaredLength = contentLength(responseHeaders);
                    if (!noBody && declaredLength > MAX_RESPONSE_BYTES) throw failure("HTTP 响应正文不能超过 16 MiB");
                    byte[] responseBody = noBody ? new byte[0] : readBody(input);
                    if (!noBody && declaredLength >= 0 && responseBody.length != declaredLength) throw failure("HTTP 响应正文不完整");
                    return new Response(status, uri, responseHeaders, responseBody);
                } finally {
                    releaseActive();
                }
            }
        }

        private void adoptBody(CompletableFuture<?> future, InputStream body) {
            boolean rejected;
            synchronized (this) {
                rejected = cancelled || timedOut || finished || future != activeFuture || deadline - System.nanoTime() <= 0;
                if (!rejected) activeBody = body;
            }
            if (rejected) closeBody(body);
        }

        private void releaseActive() {
            CompletableFuture<?> future;
            InputStream body;
            synchronized (this) {
                future = activeFuture; activeFuture = null;
                body = activeBody; activeBody = null;
            }
            cancelFuture(future);
            closeBody(body);
        }

        private byte[] readBody(InputStream input) throws IOException {
            try (InputStream source = input; ByteArrayOutputStream output = new ByteArrayOutputStream()) {
                byte[] buffer = new byte[8192];
                while (true) {
                    remaining();
                    int length = source.read(buffer);
                    remaining();
                    if (length < 0) return output.toByteArray();
                    if (length > MAX_RESPONSE_BYTES - output.size()) throw failure("HTTP 响应正文不能超过 16 MiB");
                    output.write(buffer, 0, length);
                }
            } catch (SaasHttpException error) { throw error; }
            catch (IOException error) {
                remaining();
                throw failure("HTTP 响应正文读取失败或不完整");
            }
        }
    }

    private static final class JdkHttpBackend {
        // 仅通过 JDK11+ 公开 API 反射，不访问私有字段、不调用全局 Authenticator。
        // Java8/缺少该后端的 Android 运行时明确拒绝，不声称已实现凭据隔离。
        private final Object client;
        private final Class<?> requestType, requestBuilderType, bodyPublisherType, bodyPublishersType, bodyHandlerType;
        private final Method sendAsync, responseBody, responseStatus, responseHeaders, headersMap;

        private JdkHttpBackend(int timeoutMillis) throws SaasHttpException, ReflectiveOperationException {
            try {
                Class<?> clientType = Class.forName("java.net.http.HttpClient");
                Class<?> builderType = Class.forName("java.net.http.HttpClient$Builder");
                Class<?> redirectType = Class.forName("java.net.http.HttpClient$Redirect");
                Class<?> versionType = Class.forName("java.net.http.HttpClient$Version");
                requestType = Class.forName("java.net.http.HttpRequest");
                requestBuilderType = Class.forName("java.net.http.HttpRequest$Builder");
                bodyPublisherType = Class.forName("java.net.http.HttpRequest$BodyPublisher");
                bodyPublishersType = Class.forName("java.net.http.HttpRequest$BodyPublishers");
                bodyHandlerType = Class.forName("java.net.http.HttpResponse$BodyHandler");
                Class<?> responseType = Class.forName("java.net.http.HttpResponse");
                Class<?> headersType = Class.forName("java.net.http.HttpHeaders");
                Object builder = clientType.getMethod("newBuilder").invoke(null);
                builderType.getMethod("connectTimeout", Duration.class).invoke(builder, Duration.ofMillis(timeoutMillis));
                builderType.getMethod("followRedirects", redirectType).invoke(builder, redirectType.getField("NEVER").get(null));
                builderType.getMethod("version", versionType).invoke(builder, versionType.getField("HTTP_1_1").get(null));
                client = builderType.getMethod("build").invoke(builder);
                sendAsync = clientType.getMethod("sendAsync", requestType, bodyHandlerType);
                responseBody = responseType.getMethod("body");
                responseStatus = responseType.getMethod("statusCode");
                responseHeaders = responseType.getMethod("headers");
                headersMap = headersType.getMethod("map");
            } catch (ClassNotFoundException | NoSuchMethodException | NoSuchFieldException | LinkageError error) {
                throw failure("当前运行时不支持 JDK11 HTTP 后端，不能安全执行 HTTP 请求");
            }
        }

        private CompletableFuture<?> sendAsync(URI uri, String method, Map<String, String> headers, byte[] body,
                                                int timeoutMillis) throws ReflectiveOperationException, SaasHttpException {
            Object builder = requestType.getMethod("newBuilder", URI.class).invoke(null, uri);
            requestBuilderType.getMethod("timeout", Duration.class).invoke(builder, Duration.ofMillis(timeoutMillis));
            Method header = requestBuilderType.getMethod("header", String.class, String.class);
            // 禁止运行时隐式 gzip 解压，响应大小始终按实际返回的二进制正文判断。
            if (!headers.containsKey("accept-encoding")) header.invoke(builder, "Accept-Encoding", "identity");
            for (Map.Entry<String, String> item : headers.entrySet()) header.invoke(builder, item.getKey(), item.getValue());
            Object publisher = body.length == 0 ? bodyPublishersType.getMethod("noBody").invoke(null) :
                bodyPublishersType.getMethod("ofByteArray", byte[].class).invoke(null, (Object) body);
            requestBuilderType.getMethod("method", String.class, bodyPublisherType).invoke(builder, method, publisher);
            Object request = requestBuilderType.getMethod("build").invoke(builder);
            Object handler = Class.forName("java.net.http.HttpResponse$BodyHandlers").getMethod("ofInputStream").invoke(null);
            Object future = sendAsync.invoke(client, request, handler);
            if (!(future instanceof CompletableFuture<?>)) throw failure("HTTP 后端返回的异步任务无效");
            return (CompletableFuture<?>) future;
        }

        private InputStream body(Object response) throws ReflectiveOperationException, SaasHttpException {
            Object body = responseBody.invoke(response);
            if (!(body instanceof InputStream)) throw failure("HTTP 后端返回的响应正文无效");
            return (InputStream) body;
        }
        private int status(Object response) throws ReflectiveOperationException { return ((Integer) responseStatus.invoke(response)).intValue(); }
        @SuppressWarnings("unchecked")
        private Map<String, List<String>> headers(Object response) throws ReflectiveOperationException {
            return (Map<String, List<String>>) headersMap.invoke(responseHeaders.invoke(response));
        }
    }

    private static URI parseUrl(String value) throws SaasHttpException {
        if (value == null || value.isEmpty() || value.length() > MAX_URL_CHARACTERS) throw failure("HTTP 地址为空或超过长度限制");
        try {
            URI uri = new URI(value);
            if (!("http".equalsIgnoreCase(uri.getScheme()) || "https".equalsIgnoreCase(uri.getScheme())) ||
                uri.getHost() == null || uri.getHost().isEmpty() || uri.getRawUserInfo() != null ||
                uri.getPort() == 0 || uri.getPort() > 65535) throw failure("地址必须为有效 HTTP 或 HTTPS 地址，且不能包含用户名或密码");
            if (uri.toASCIIString().length() > MAX_URL_CHARACTERS) throw failure("HTTP 地址编码后超过长度限制");
            return uri;
        } catch (URISyntaxException error) { throw failure("HTTP 地址格式无效"); }
    }

    private static boolean supportedMethod(String method) {
        return "GET".equals(method) || "POST".equals(method) || "PUT".equals(method) || "DELETE".equals(method) ||
            "HEAD".equals(method) || "OPTIONS".equals(method) || "TRACE".equals(method);
    }

    private static Map<String, String> requestHeaders(Map<String, String> source) throws SaasHttpException {
        Map<String, String> result = new LinkedHashMap<String, String>();
        if (source == null) return Collections.unmodifiableMap(result);
        if (source.size() > MAX_HEADER_COUNT) throw failure("HTTP 请求头数量超过限制");
        int total = 0;
        for (Map.Entry<String, String> item : source.entrySet()) {
            String name = item.getKey(), value = item.getValue();
            if (name == null || name.isEmpty() || name.length() > MAX_HEADER_NAME_CHARACTERS || !headerName(name) ||
                value == null || value.length() > MAX_HEADER_VALUE_CHARACTERS || !headerValue(value)) throw failure("HTTP 请求头格式无效或超过长度限制");
            name = name.toLowerCase(Locale.ROOT);
            if (restrictedHeader(name) || result.containsKey(name)) throw failure("HTTP 请求头不受支持或存在重复名称");
            total += name.length() + value.length() + 4;
            if (total > MAX_HEADER_CHARACTERS) throw failure("HTTP 请求头总长度超过限制");
            result.put(name, value);
        }
        return Collections.unmodifiableMap(result);
    }

    private static boolean headerName(String value) {
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || "!#$%&'*+-.^_`|~".indexOf(c) >= 0)) return false;
        }
        return true;
    }
    private static boolean headerValue(String value) {
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if ((c < 32 && c != '\t') || c > 126) return false;
        }
        return true;
    }
    private static boolean restrictedHeader(String name) {
        return name.startsWith("sec-") || name.startsWith("proxy-") || "host".equals(name) || "content-length".equals(name) ||
            "transfer-encoding".equals(name) || "connection".equals(name) || "keep-alive".equals(name) || "upgrade".equals(name) ||
            "trailer".equals(name) || "te".equals(name) || "via".equals(name) || "origin".equals(name) ||
            "content-transfer-encoding".equals(name) || "access-control-request-headers".equals(name) || "access-control-request-method".equals(name);
    }

    private static Map<String, List<String>> responseHeaders(Map<String, List<String>> source) throws SaasHttpException {
        Map<String, List<String>> result = new LinkedHashMap<String, List<String>>();
        int count = 0, total = 0;
        for (Map.Entry<String, List<String>> item : source.entrySet()) {
            if (item.getKey() == null) continue;
            String name = item.getKey();
            if (name.length() > MAX_HEADER_NAME_CHARACTERS || !headerName(name)) throw failure("HTTP 响应头格式无效");
            String normalized = name.toLowerCase(Locale.ROOT);
            List<String> values = result.get(normalized);
            if (values == null) { values = new ArrayList<String>(); result.put(normalized, values); }
            for (String value : item.getValue()) {
                if (value == null || value.length() > MAX_HEADER_VALUE_CHARACTERS || ++count > MAX_HEADER_COUNT) throw failure("HTTP 响应头超过长度或数量限制");
                total += name.length() + responseHeaderUtf8Bytes(value) + 4;
                if (total > MAX_RESPONSE_HEADER_UTF8_BYTES) throw failure("HTTP 响应头 UTF-8 总长度超过限制");
                values.add(value);
            }
        }
        for (Map.Entry<String, List<String>> item : result.entrySet()) item.setValue(Collections.unmodifiableList(item.getValue()));
        return Collections.unmodifiableMap(result);
    }

    private static int responseHeaderUtf8Bytes(String value) throws SaasHttpException {
        // 头值拒绝控制字符与未配对代理项，按返回字符串的 UTF-8 体积计算限额。
        int bytes = 0;
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (Character.isISOControl(c) && c != '\t') throw failure("HTTP 响应头包含无效控制字符");
            if (Character.isHighSurrogate(c)) {
                if (++i >= value.length() || !Character.isLowSurrogate(value.charAt(i))) throw failure("HTTP 响应头包含无效 Unicode 字符");
                bytes += 4;
            } else {
                if (Character.isLowSurrogate(c)) throw failure("HTTP 响应头包含无效 Unicode 字符");
                bytes += c < 128 ? 1 : c < 2048 ? 2 : 3;
            }
            if (bytes > MAX_RESPONSE_HEADER_VALUE_UTF8_BYTES) throw failure("HTTP 响应头 UTF-8 长度超过限制");
        }
        return bytes;
    }

    private static long contentLength(Map<String, List<String>> headers) throws SaasHttpException {
        List<String> values = headers.get("content-length");
        if (values == null || values.isEmpty()) return -1;
        long length = -1;
        for (String value : values) {
            try {
                if (!value.matches("[0-9]+")) throw failure("HTTP 响应正文长度无效");
                long current = Long.parseLong(value);
                if (length >= 0 && length != current) throw failure("HTTP 响应正文长度不一致");
                length = current;
            } catch (NumberFormatException error) { throw failure("HTTP 响应正文长度无效"); }
        }
        return length;
    }
    private static String firstHeader(Map<String, List<String>> headers, String name) {
        List<String> values = headers.get(name);
        return values == null || values.isEmpty() ? null : values.get(0);
    }
    private static boolean redirectStatus(int status) { return status == 301 || status == 302 || status == 303 || status == 307 || status == 308; }
    private static boolean sameOrigin(URI left, URI right) {
        return left.getScheme().equalsIgnoreCase(right.getScheme()) && left.getHost().equalsIgnoreCase(right.getHost()) && port(left) == port(right);
    }
    private static int port(URI uri) { return uri.getPort() >= 0 ? uri.getPort() : "https".equalsIgnoreCase(uri.getScheme()) ? 443 : 80; }
    private static void cancelFuture(CompletableFuture<?> future) {
        if (future == null) return;
        try { future.cancel(true); }
        catch (RuntimeException ignored) { /* 清理阶段不能暴露底层异常，也不能掩盖取消或请求结果。 */ }
    }
    private static void closeBody(InputStream body) {
        if (body == null) return;
        try { body.close(); }
        catch (IOException | RuntimeException ignored) { /* 清理阶段不能暴露底层异常，也不能掩盖取消或请求结果。 */ }
    }
    private static SaasHttpException transportFailure(Throwable error) {
        for (int depth = 0; error != null && depth < 8; depth++, error = error.getCause()) {
            if (error instanceof SSLException) return failure("HTTPS 安全连接失败，请检查服务器证书与系统 TLS 配置");
            if (error instanceof TimeoutException || error.getClass().getName().equals("java.net.http.HttpTimeoutException") ||
                error.getClass().getName().equals("java.net.http.HttpConnectTimeoutException")) return failure("HTTP 请求超时，请稍后重试");
        }
        return failure("HTTP 请求失败，请检查网络、服务器或系统证书配置");
    }
    private static SaasHttpException failure(String message) { return new SaasHttpException(message); }
}
