package com.fingerprint.saas.bridge;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import com.sun.net.httpserver.HttpsConfigurator;
import com.sun.net.httpserver.HttpsServer;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.Field;
import java.net.Authenticator;
import java.net.CookieHandler;
import java.net.CookieManager;
import java.net.CookiePolicy;
import java.net.HttpCookie;
import java.net.InetSocketAddress;
import java.net.PasswordAuthentication;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.KeyStore;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;

/** 仅测试使用 JDK loopback 服务，真实传输请求/响应；不替代任何生产业务逻辑。 */
public final class SaasHttpClientSelfTest {
    private static final SaasHttpClient CLIENT = new SaasHttpClient();
    private static int passed;
    private static final byte[] BINARY = new byte[] {0, 1, 127, (byte) 128, (byte) 255};
    private static final AtomicReference<Observed> OBSERVED = new AtomicReference<Observed>();
    private static final AtomicInteger HITS = new AtomicInteger();
    private static final AtomicReference<CountDownLatch> ENTERED = new AtomicReference<CountDownLatch>();
    private static final AtomicReference<CountDownLatch> RELEASE = new AtomicReference<CountDownLatch>();
    private SaasHttpClientSelfTest() { }

    private static final class Observed {
        final String method, authorization, cookie, cookie2, marker, contentType;
        final byte[] body;
        Observed(HttpExchange exchange, byte[] body) {
            method = exchange.getRequestMethod();
            authorization = exchange.getRequestHeaders().getFirst("Authorization");
            cookie = exchange.getRequestHeaders().getFirst("Cookie");
            cookie2 = exchange.getRequestHeaders().getFirst("Cookie2");
            marker = exchange.getRequestHeaders().getFirst("X-Marker");
            contentType = exchange.getRequestHeaders().getFirst("Content-Type");
            this.body = body;
        }
    }

    public static void main(String[] args) throws Exception {
        String temp = System.getProperty("java.io.tmpdir");
        check(Paths.get(temp).toAbsolutePath().toString().toLowerCase(java.util.Locale.ROOT).startsWith("d:"), "测试输出限定 D 盘");
        if (args.length == 1 && "--backend-unavailable".equals(args[0])) {
            rejectMessage("后端缺失时明确拒绝", "不支持 JDK11 HTTP 后端", () -> execute("GET", "http://127.0.0.1:9/", null, null));
            deadlinesDrained();
            System.out.println("HTTP 后端不可用时拒绝执行验证通过");
            return;
        }
        validateInputs();
        ExecutorService workers = Executors.newCachedThreadPool();
        HttpServer first = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        HttpServer second = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        try {
            first.setExecutor(workers); second.setExecutor(workers);
            String firstBase = base(first), secondBase = base(second);
            configure(first, firstBase, secondBase);
            configure(second, secondBase, firstBase);
            first.start(); second.start();
            transport(firstBase);
            redirects(firstBase, secondBase);
            globalCredentials(firstBase, workers);
            responseHeaderSafety(firstBase);
            truncated(workers);
            limits(firstBase);
            cancellation(firstBase, workers);
            concurrent(firstBase, workers);
            tls(workers);
            deadlinesDrained();
        } finally {
            CountDownLatch release = RELEASE.get();
            if (release != null) release.countDown();
            first.stop(0); second.stop(0);
            workers.shutdownNow();
            workers.awaitTermination(5, TimeUnit.SECONDS);
        }
        System.out.println("原生 HTTP loopback 自测通过：" + passed + " 项");
    }

    private static void validateInputs() throws Exception {
        String url = "http://127.0.0.1/";
        check(new SaasHttpClient.Request("GET", url, null, null, false).getTimeoutMillis() == 30000, "默认 30 秒超时");
        reject("拒绝自动浏览器凭据", () -> request("GET", url, null, null, true));
        for (String method : new String[] {"PATCH", "CONNECT", "CUSTOM", "get", "GET\r\n", "", null}) {
            reject("拒绝不支持的方法", () -> request(method, url, null, null, false));
        }
        for (String value : new String[] {"file:///D:/secret", "ftp://localhost/file", "http://user:password@localhost/",
            "http://user@localhost/", "http://localhost:0/", "http://localhost:65536/", "http://bad host/", "http:///path", "", null,
            "http://localhost/" + repeat('a', SaasHttpClient.MAX_URL_CHARACTERS), "http://localhost/" + repeat('中', 1000)}) {
            reject("拒绝非法 URL 或用户信息", () -> request("GET", value, null, null, false));
        }
        for (String method : new String[] {"GET", "HEAD", "TRACE"}) reject("拒绝隐式方法转换", () -> request(method, url, null, BINARY, false));
        reject("请求超时下界", () -> new SaasHttpClient.Request("GET", url, null, null, false, 0));
        reject("请求超时上界", () -> new SaasHttpClient.Request("GET", url, null, null, false, 30001));
        reject("请求正文超限", () -> request("POST", url, null, new byte[SaasHttpClient.MAX_REQUEST_BYTES + 1], false));
        for (Map<String, String> headers : Arrays.asList(headers("X-Test", "ok\r\nInjected: bad"), headers("X-Test", "\u0000"),
            headers("X-Test", "中文"), headers("Bad Name", "value"), headers("X-Test", repeat('v', 8193)),
            headers(repeat('n', 129), "value"), headers("Content-Length", "1"), headers("Host", "other"),
            headers("Origin", "http://other"), headers("Transfer-Encoding", "chunked"), headers("Sec-Test", "bad"))) {
            reject("请求头格式、长度或受限头", () -> request("GET", url, headers, null, false));
        }
        Map<String, String> duplicate = headers("X-Test", "a"); duplicate.put("x-test", "b");
        reject("大小写重复头", () -> request("GET", url, duplicate, null, false));
        Map<String, String> many = new LinkedHashMap<String, String>();
        for (int i = 0; i < 101; i++) many.put("X-" + i, "a");
        reject("请求头数量", () -> request("GET", url, many, null, false));
        Map<String, String> large = new LinkedHashMap<String, String>();
        for (int i = 0; i < 5; i++) large.put("X-" + i, repeat('v', 8192));
        reject("请求头总长度", () -> request("GET", url, large, null, false));
        reject("空请求句柄", () -> CLIENT.newCall(null));
    }

    private static void configure(HttpServer server, String own, String other) {
        server.createContext("/echo", exchange -> {
            byte[] body = read(exchange.getRequestBody());
            HITS.incrementAndGet(); OBSERVED.set(new Observed(exchange, body));
            exchange.getResponseHeaders().add("X-Multi", "first");
            exchange.getResponseHeaders().add("X-Multi", "second");
            exchange.getResponseHeaders().add("Set-Cookie", "server-only=secret; Path=/");
            send(exchange, 200, body.length == 0 ? BINARY : body);
        });
        server.createContext("/status", exchange -> {
            HITS.incrementAndGet();
            int code = Integer.parseInt(exchange.getRequestURI().getQuery());
            if (code == 401) exchange.getResponseHeaders().set("WWW-Authenticate", "Basic realm=\"self-test\"");
            if (code == 407) exchange.getResponseHeaders().set("Proxy-Authenticate", "Basic realm=\"self-test\"");
            send(exchange, code, BINARY);
        });
        server.createContext("/redirect", exchange -> {
            String query = exchange.getRequestURI().getQuery();
            String target = query.contains("cross") ? other + "/echo" : query.contains("back") ? other + "/return" :
                query.contains("host") ? own.replace("127.0.0.1", "localhost") + "/echo" : "/echo";
            exchange.getResponseHeaders().set("Location", target);
            read(exchange.getRequestBody());
            send(exchange, Integer.parseInt(query.substring(0, 3)), new byte[0]);
        });
        server.createContext("/return", exchange -> { exchange.getResponseHeaders().set("Location", other + "/echo"); send(exchange, 302, new byte[0]); });
        server.createContext("/loop", exchange -> { exchange.getResponseHeaders().set("Location", own + "/loop"); HITS.incrementAndGet(); send(exchange, 302, new byte[0]); });
        server.createContext("/chain", exchange -> {
            int remaining = Integer.parseInt(exchange.getRequestURI().getQuery());
            if (remaining == 0) { send(exchange, 200, BINARY); return; }
            exchange.getResponseHeaders().set("Location", "/chain?" + (remaining - 1)); send(exchange, 302, new byte[0]);
        });
        server.createContext("/bad-redirect", exchange -> {
            exchange.getResponseHeaders().set("Location", exchange.getRequestURI().getQuery().equals("userinfo") ? "http://user:password@127.0.0.1/" : "file:///D:/secret");
            send(exchange, 302, new byte[0]);
        });
        server.createContext("/missing-location", exchange -> send(exchange, 302, BINARY));
        server.createContext("/bytes", exchange -> {
            String[] query = exchange.getRequestURI().getQuery().split("-");
            int size = Integer.parseInt(query[0]);
            exchange.sendResponseHeaders(200, query.length == 1 ? size : 0);
            try (OutputStream output = exchange.getResponseBody()) {
                byte[] block = new byte[8192];
                for (int written = 0; written < size; written += block.length) output.write(block, 0, Math.min(block.length, size - written));
            } catch (IOException expectedDisconnect) { /* 超限拒绝后允许客户端断开。 */ }
            finally { exchange.close(); }
        });
        server.createContext("/large-header", exchange -> { exchange.getResponseHeaders().set("X-Large", repeat('a', 8193)); send(exchange, 200, BINARY); });
        server.createContext("/control-header", exchange -> {
            char control = (char) Integer.parseInt(exchange.getRequestURI().getQuery());
            exchange.getResponseHeaders().set("X-Control", "before" + control + "after");
            send(exchange, 200, BINARY);
        });
        server.createContext("/utf8-header", exchange -> {
            exchange.getResponseHeaders().set("X-Utf8", repeat('\u00e9', Integer.parseInt(exchange.getRequestURI().getQuery())));
            send(exchange, 200, BINARY);
        });
        server.createContext("/utf8-total", exchange -> {
            for (int i = 0; i < 4; i++) exchange.getResponseHeaders().set("X-Utf8-" + i, repeat('\u00e9', 4096));
            send(exchange, 200, BINARY);
        });
        server.createContext("/trickle", exchange -> {
            try {
                exchange.sendResponseHeaders(200, 0);
                for (int i = 0; i < 20; i++) {
                    exchange.getResponseBody().write(1); exchange.getResponseBody().flush(); Thread.sleep(40);
                }
            } catch (InterruptedException error) { Thread.currentThread().interrupt(); }
            catch (IOException expectedDisconnect) { /* 总超时后客户端断开。 */ }
            finally { exchange.close(); }
        });
        server.createContext("/slow-redirect", exchange -> {
            try {
                Thread.sleep(70);
                exchange.getResponseHeaders().set("Location", "/slow-redirect"); send(exchange, 302, new byte[0]);
            } catch (InterruptedException error) { Thread.currentThread().interrupt(); }
            catch (IOException expectedDisconnect) { /* 重定向链共用截止时间，超时后断开。 */ }
            finally { exchange.close(); }
        });
        server.createContext("/upload", exchange -> {
            try {
                MessageDigest digest = MessageDigest.getInstance("SHA-256");
                int size = 0; byte[] block = new byte[8192];
                try (InputStream input = exchange.getRequestBody()) {
                    int length; while ((length = input.read(block)) >= 0) { size += length; digest.update(block, 0, length); }
                }
                exchange.getResponseHeaders().set("X-Received-Bytes", String.valueOf(size));
                send(exchange, 200, digest.digest());
            } catch (java.security.GeneralSecurityException error) { throw new IOException("测试摘要算法不可用"); }
        });
        server.createContext("/hold", exchange -> {
            CountDownLatch entered = ENTERED.get(), release = RELEASE.get();
            boolean body = "body".equals(exchange.getRequestURI().getQuery());
            try {
                if (body) { exchange.sendResponseHeaders(200, 0); exchange.getResponseBody().write(1); exchange.getResponseBody().flush(); }
                entered.countDown();
                if (!release.await(10, TimeUnit.SECONDS)) throw new IOException("测试等待超时");
                if (body) exchange.getResponseBody().close(); else send(exchange, 200, BINARY);
            } catch (InterruptedException error) { Thread.currentThread().interrupt(); }
            catch (IOException expectedDisconnect) { /* 真实取消或超时导致对端断开。 */ }
            finally { exchange.close(); }
        });
    }

    private static void transport(String base) throws Exception {
        for (String method : new String[] {"GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS", "TRACE"}) {
            SaasHttpClient.Response result = execute(method, base + "/echo", headers("X-Marker", "explicit"), null);
            check(OBSERVED.get().method.equals(method), "真实方法保持 " + method);
            check("explicit".equals(OBSERVED.get().marker), "自定义请求头传输");
            check(result.getStatus() == 200 && Arrays.equals(result.getBody(), method.equals("HEAD") ? new byte[0] : BINARY), "二进制或 HEAD 响应");
            check(OBSERVED.get().cookie == null && OBSERVED.get().authorization == null, "默认无 Cookie 和授权");
        }
        for (String method : new String[] {"POST", "PUT", "DELETE", "OPTIONS"}) {
            SaasHttpClient.Response response = execute(method, base + "/echo", headers("Content-Type", "application/octet-stream"), BINARY);
            check(OBSERVED.get().method.equals(method) && Arrays.equals(OBSERVED.get().body, BINARY) && Arrays.equals(response.getBody(), BINARY), "二进制请求保持方法");
        }
        Map<String, String> credentials = headers("Authorization", "Bearer explicit-test"); credentials.put("Cookie", "explicit=value");
        execute("GET", base + "/echo", credentials, null);
        check("Bearer explicit-test".equals(OBSERVED.get().authorization) && "explicit=value".equals(OBSERVED.get().cookie), "显式凭据支持");
        execute("GET", base + "/echo", null, null);
        check(OBSERVED.get().cookie == null, "Set-Cookie 不进入自动 Cookie 容器");
        int initialHits = HITS.get();
        for (int code : new int[] {400, 401, 403, 404, 407, 429, 500}) {
            SaasHttpClient.Response response = execute("GET", base + "/status?" + code, null, null);
            check(response.getStatus() == code && Arrays.equals(response.getBody(), BINARY), "非成功状态及正文正常返回");
        }
        check(HITS.get() - initialHits == 7, "认证挑战不触发重试");
        check(execute("POST", base + "/status?401", null, BINARY).getBody().length == BINARY.length, "上传认证错误保留正文");
        Map<String, String> inputHeaders = headers("X-Marker", "original"); byte[] inputBody = BINARY.clone();
        SaasHttpClient.Request request = request("POST", base + "/echo", inputHeaders, inputBody, false);
        inputHeaders.put("X-Marker", "changed"); inputBody[0] = 99; request.getBody()[0] = 88;
        SaasHttpClient.Response response = CLIENT.newCall(request).execute();
        check("original".equals(OBSERVED.get().marker) && Arrays.equals(OBSERVED.get().body, BINARY), "请求防御性复制");
        response.getBody()[0] = 44;
        check(Arrays.equals(response.getBody(), BINARY), "响应正文不可变");
        expectUnsupported(() -> request.getHeaders().put("x", "y"));
        expectUnsupported(() -> response.getHeaders().clear());
        List<String> multi = response.getHeaders().get("x-multi");
        check(multi.size() == 2 && multi.contains("first") && multi.contains("second"), "重复响应头保留");
        expectUnsupported(() -> multi.add("other"));
        check(!request.toString().contains(base) && !response.toString().contains(base), "对象描述不暴露敏感内容");
        SaasHttpClient.Call once = CLIENT.newCall(request("GET", base + "/echo", null, null, false)); once.execute();
        reject("句柄只执行一次", once::execute);
    }

    private static void redirects(String own, String other) throws Exception {
        Map<String, String> credentials = headers("Authorization", "Bearer explicit-test");
        credentials.put("Cookie", "explicit=value"); credentials.put("Cookie2", "second=value"); credentials.put("X-Marker", "forward");
        SaasHttpClient.Response same = execute("GET", own + "/redirect?302-same", credentials, null);
        check(same.getUrl().equals(own + "/echo") && "Bearer explicit-test".equals(OBSERVED.get().authorization) && "explicit=value".equals(OBSERVED.get().cookie), "同来源保留显式凭据");
        SaasHttpClient.Response cross = execute("GET", own + "/redirect?302-cross", credentials, null);
        check(cross.getUrl().equals(other + "/echo") && OBSERVED.get().authorization == null && OBSERVED.get().cookie == null && OBSERVED.get().cookie2 == null && "forward".equals(OBSERVED.get().marker), "跨端口来源移除凭据");
        execute("GET", own + "/redirect?302-back", credentials, null);
        check(OBSERVED.get().authorization == null && OBSERVED.get().cookie == null, "回到原来源不恢复凭据");
        execute("GET", own + "/redirect?302-host", credentials, null);
        check(OBSERVED.get().authorization == null && OBSERVED.get().cookie == null, "同端口不同主机仍为跨来源");
        execute("POST", own + "/redirect?307-cross", credentials, BINARY);
        check("POST".equals(OBSERVED.get().method) && Arrays.equals(OBSERVED.get().body, BINARY) &&
            OBSERVED.get().authorization == null && OBSERVED.get().cookie == null, "跨来源 307 保持二进制正文但移除凭据头");
        for (int code : new int[] {307, 308}) {
            execute("POST", own + "/redirect?" + code + "-same", headers("Content-Type", "application/octet-stream"), BINARY);
            check("POST".equals(OBSERVED.get().method) && Arrays.equals(OBSERVED.get().body, BINARY), "307/308 保持方法和二进制正文");
        }
        for (int code : new int[] {301, 302, 303}) {
            execute("POST", own + "/redirect?" + code + "-same", headers("Content-Type", "application/octet-stream"), BINARY);
            check("GET".equals(OBSERVED.get().method) && OBSERVED.get().body.length == 0 && OBSERVED.get().contentType == null, "重定向转换方法并移除正文头");
        }
        int initial = HITS.get(); reject("重定向跳数限制", () -> execute("GET", own + "/loop", null, null));
        check(HITS.get() - initial == SaasHttpClient.MAX_REDIRECTS + 1, "重定向最多五跳");
        check(execute("GET", own + "/chain?5", null, null).getStatus() == 200, "重定向五跳精确边界");
        reject("重定向六跳拒绝", () -> execute("GET", own + "/chain?6", null, null));
        reject("重定向拒绝文件协议", () -> execute("GET", own + "/bad-redirect?file", null, null));
        reject("重定向拒绝用户信息", () -> execute("GET", own + "/bad-redirect?userinfo", null, null));
        SaasHttpClient.Response response = execute("GET", own + "/missing-location", null, null);
        check(response.getStatus() == 302 && Arrays.equals(response.getBody(), BINARY), "无 Location 返回真实 3xx 正文");
    }

    private static void limits(String base) throws Exception {
        check(execute("GET", base + "/bytes?" + SaasHttpClient.MAX_RESPONSE_BYTES, null, null).getBody().length == SaasHttpClient.MAX_RESPONSE_BYTES, "16 MiB 固定长度精确边界");
        check(execute("GET", base + "/bytes?" + SaasHttpClient.MAX_RESPONSE_BYTES + "-chunked", null, null).getBody().length == SaasHttpClient.MAX_RESPONSE_BYTES, "16 MiB 分块精确边界");
        reject("固定长度响应超限", () -> execute("GET", base + "/bytes?" + (SaasHttpClient.MAX_RESPONSE_BYTES + 1), null, null));
        reject("分块响应超限", () -> execute("GET", base + "/bytes?" + (SaasHttpClient.MAX_RESPONSE_BYTES + 1) + "-chunked", null, null));
        reject("响应头超限", () -> execute("GET", base + "/large-header", null, null));
        byte[] body = new byte[SaasHttpClient.MAX_REQUEST_BYTES]; body[0] = 42; body[body.length - 1] = 43;
        SaasHttpClient.Response response = execute("POST", base + "/upload", null, body);
        check(response.getHeaders().get("x-received-bytes").get(0).equals(String.valueOf(body.length)) &&
            Arrays.equals(MessageDigest.getInstance("SHA-256").digest(body), response.getBody()), "64 MiB 请求真实传输及摘要校验");
    }

    private static final class CountingCookieManager extends CookieManager {
        final AtomicInteger gets = new AtomicInteger(), puts = new AtomicInteger();
        CountingCookieManager(URI uri) {
            super(null, CookiePolicy.ACCEPT_ALL);
            HttpCookie cookie = new HttpCookie("ambient", "test-global-cookie");
            cookie.setPath("/"); getCookieStore().add(uri, cookie);
        }
        @Override public Map<String, List<String>> get(URI uri, Map<String, List<String>> requestHeaders) throws IOException {
            gets.incrementAndGet(); return super.get(uri, requestHeaders);
        }
        @Override public void put(URI uri, Map<String, List<String>> responseHeaders) throws IOException {
            puts.incrementAndGet(); super.put(uri, responseHeaders);
        }
    }

    private static void globalCredentials(String base, ExecutorService workers) throws Exception {
        // 仅独立测试临时安装有凭据的全局处理器；必须恢复原对象，生产后端完全不接触它们。
        CookieHandler originalCookie = CookieHandler.getDefault();
        Authenticator originalAuth = (Authenticator) Authenticator.class.getMethod("getDefault").invoke(null);
        CountingCookieManager first = new CountingCookieManager(new URI(base + "/"));
        CountingCookieManager second = new CountingCookieManager(new URI(base + "/"));
        AtomicInteger authCalls = new AtomicInteger();
        Authenticator installedAuth = new Authenticator() {
            @Override protected PasswordAuthentication getPasswordAuthentication() {
                authCalls.incrementAndGet(); return new PasswordAuthentication("test-global-user", "test-global-password".toCharArray());
            }
        };
        AtomicBoolean changing = new AtomicBoolean();
        AtomicInteger changes = new AtomicInteger();
        Future<?> changer = null;
        try {
            CookieHandler.setDefault(first); Authenticator.setDefault(installedAuth);
            execute("GET", base + "/echo", null, null);
            check(OBSERVED.get().cookie == null && OBSERVED.get().authorization == null, "有全局凭据仍默认不携带");
            check(CookieHandler.getDefault() == first && Authenticator.class.getMethod("getDefault").invoke(null) == installedAuth, "请求未修改全局处理器");
            Map<String, String> explicit = headers("Cookie", "explicit=only"); explicit.put("Authorization", "Bearer explicit-only");
            execute("GET", base + "/echo", explicit, null);
            check("explicit=only".equals(OBSERVED.get().cookie) && "Bearer explicit-only".equals(OBSERVED.get().authorization), "显式凭据不混入全局凭据");
            int hits = HITS.get();
            SaasHttpClient.Response unauthorized = execute("GET", base + "/status?401", null, null);
            check(unauthorized.getStatus() == 401 && Arrays.equals(unauthorized.getBody(), BINARY) && HITS.get() == hits + 1 && authCalls.get() == 0, "401 不调用全局认证或重试");
            CountDownLatch started = new CountDownLatch(1);
            changing.set(true);
            changer = workers.submit(() -> {
                while (changing.get()) {
                    CookieHandler.setDefault(changes.incrementAndGet() % 2 == 0 ? first : second);
                    started.countDown();
                    try { Thread.sleep(1); }
                    catch (InterruptedException error) { Thread.currentThread().interrupt(); return; }
                }
            });
            check(started.await(2, TimeUnit.SECONDS), "全局 Cookie 并发变更已开始");
            for (int i = 0; i < 24; i++) {
                execute("GET", base + (i % 2 == 0 ? "/echo" : "/redirect?302-cross"), null, null);
                check(OBSERVED.get().cookie == null && OBSERVED.get().cookie2 == null && OBSERVED.get().authorization == null, "全局 Cookie 并发变化不影响请求或重定向");
            }
            check(changes.get() > 1 && first.gets.get() == 0 && first.puts.get() == 0 && second.gets.get() == 0 && second.puts.get() == 0, "新后端从不调用全局 CookieHandler");
            check(first.getCookieStore().getCookies().size() == 1 && second.getCookieStore().getCookies().size() == 1, "Set-Cookie 不写入全局容器");
            check(authCalls.get() == 0 && Authenticator.class.getMethod("getDefault").invoke(null) == installedAuth, "全局认证对象与凭据不被使用或修改");
        } finally {
            changing.set(false);
            try { if (changer != null) changer.get(5, TimeUnit.SECONDS); }
            finally { CookieHandler.setDefault(originalCookie); Authenticator.setDefault(originalAuth); }
        }
        check(CookieHandler.getDefault() == originalCookie && Authenticator.class.getMethod("getDefault").invoke(null) == originalAuth, "测试结束恢复原全局状态");
    }

    private static void responseHeaderSafety(String base) throws Exception {
        for (int control : new int[] {0, 1, 31, 127, 133}) {
            reject("响应头拒绝控制字符", () -> execute("GET", base + "/control-header?" + control, null, null));
        }
        SaasHttpClient.Response tab = execute("GET", base + "/control-header?9", null, null);
        check(tab.getStatus() == 200 && tab.getHeaders().get("x-control").get(0).replace('\t', ' ').equals("before after"), "响应头允许合法水平制表符或标准化空格");
        SaasHttpClient.Response exact = execute("GET", base + "/utf8-header?4096", null, null);
        check(exact.getHeaders().get("x-utf8").get(0).getBytes(StandardCharsets.UTF_8).length == SaasHttpClient.MAX_RESPONSE_HEADER_VALUE_UTF8_BYTES, "响应头 UTF-8 单项精确边界");
        rejectMessage("响应头 UTF-8 单项超限", "UTF-8", () -> execute("GET", base + "/utf8-header?4097", null, null));
        rejectMessage("响应头 UTF-8 总长度超限", "UTF-8", () -> execute("GET", base + "/utf8-total", null, null));
    }

    private static void deadlinesDrained() throws Exception {
        // 只读测试自身的定时器队列，确认请求结束后没有等待原截止时间的取消任务。
        Field field = SaasHttpClient.class.getDeclaredField("DEADLINES"); field.setAccessible(true);
        ScheduledThreadPoolExecutor deadlines = (ScheduledThreadPoolExecutor) field.get(null);
        check(deadlines.getRemoveOnCancelPolicy(), "取消的定时任务立即移除");
        check(deadlines.getQueue().isEmpty(), "请求完成或取消后无截止时间任务滞留");
    }

    private static void cancellation(String base, ExecutorService workers) throws Exception {
        SaasHttpClient.Call before = CLIENT.newCall(request("GET", base + "/echo", null, null, false));
        int initial = HITS.get(); before.cancel(); reject("执行前取消", before::execute);
        check(before.isCancelled() && HITS.get() == initial, "执行前取消不访问网络");
        for (String stage : new String[] {"headers", "body"}) {
            ENTERED.set(new CountDownLatch(1)); RELEASE.set(new CountDownLatch(1));
            SaasHttpClient.Call call = CLIENT.newCall(new SaasHttpClient.Request("GET", base + "/hold?" + stage, null, null, false, 5000));
            Future<String> result = workers.submit(() -> { try { call.execute(); return "意外成功"; } catch (SaasHttpClient.SaasHttpException error) { return error.getMessage(); } });
            try {
                check(ENTERED.get().await(3, TimeUnit.SECONDS), "取消前请求已到达真实服务");
                Future<?> stopped = workers.submit(call::cancel);
                String message = result.get(2, TimeUnit.SECONDS); stopped.get(2, TimeUnit.SECONDS);
                check(message.contains("取消") && call.isCancelled(), "等待响应头/正文期间取消并断开连接");
            } finally { RELEASE.get().countDown(); }
        }
        ENTERED.set(new CountDownLatch(1)); RELEASE.set(new CountDownLatch(1));
        long started = System.nanoTime();
        try {
            rejectMessage("真实响应超时", "超时", () -> CLIENT.newCall(new SaasHttpClient.Request("GET", base + "/hold?headers", null, null, false, 150)).execute());
            check(TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - started) < 2000, "超时有界");
        } finally { RELEASE.get().countDown(); }
        rejectMessage("持续到达的数据不能延长总超时", "超时", () -> CLIENT.newCall(new SaasHttpClient.Request("GET", base + "/trickle", null, null, false, 180)).execute());
        rejectMessage("重定向共用总超时", "超时", () -> CLIENT.newCall(new SaasHttpClient.Request("GET", base + "/slow-redirect", null, null, false, 180)).execute());
    }

    private static void concurrent(String base, ExecutorService workers) throws Exception {
        java.util.ArrayList<Future<Boolean>> futures = new java.util.ArrayList<Future<Boolean>>();
        for (int i = 0; i < 12; i++) futures.add(workers.submit(() -> Arrays.equals(execute("POST", base + "/echo", null, BINARY).getBody(), BINARY)));
        for (Future<Boolean> future : futures) check(future.get(5, TimeUnit.SECONDS), "独立句柄并发传输");
    }

    private static void truncated(ExecutorService workers) throws Exception {
        // HttpServer 普通 close 对少写正文不会立即关闭连接；独立服务强制断开制造真实截断。
        HttpServer server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.setExecutor(workers);
        server.createContext("/", exchange -> {
            try {
                exchange.sendResponseHeaders(200, 10);
                exchange.getResponseBody().write(BINARY); exchange.getResponseBody().flush();
            } finally { server.stop(0); exchange.close(); }
        });
        server.start();
        try {
            rejectMessage("截断响应拒绝", "不完整", () -> CLIENT.newCall(new SaasHttpClient.Request("GET", base(server) + "/", null, null, false, 2000)).execute());
        } finally { server.stop(0); }
    }

    private static void tls(ExecutorService workers) throws Exception {
        // 仅在 D 盘测试输出中生成独立证书；客户端不导入证书，不替换系统 TLS 验证。
        Path directory = Files.createTempDirectory(Paths.get(System.getProperty("java.io.tmpdir")), "saas-http-tls-");
        Path store = directory.resolve("self-test.p12");
        String keytool = Paths.get(System.getProperty("java.home"), "bin", "keytool.exe").toString();
        Process process = new ProcessBuilder(keytool, "-genkeypair", "-alias", "loopback", "-keyalg", "RSA", "-keysize", "2048",
            "-storetype", "PKCS12", "-keystore", store.toString(), "-storepass", "test-only-password", "-keypass", "test-only-password",
            "-dname", "CN=localhost", "-ext", "SAN=IP:127.0.0.1,DNS:localhost", "-validity", "1", "-noprompt").redirectErrorStream(true).start();
        read(process.getInputStream());
        check(process.waitFor(15, TimeUnit.SECONDS) && process.exitValue() == 0, "独立 TLS 测试证书生成");
        KeyStore keys = KeyStore.getInstance("PKCS12");
        try (InputStream input = Files.newInputStream(store)) { keys.load(input, "test-only-password".toCharArray()); }
        KeyManagerFactory managers = KeyManagerFactory.getInstance(KeyManagerFactory.getDefaultAlgorithm());
        managers.init(keys, "test-only-password".toCharArray());
        SSLContext tls = SSLContext.getInstance("TLS"); tls.init(managers.getKeyManagers(), null, null);
        HttpsServer server = HttpsServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.setHttpsConfigurator(new HttpsConfigurator(tls)); server.setExecutor(workers);
        AtomicInteger accepted = new AtomicInteger();
        server.createContext("/", exchange -> { accepted.incrementAndGet(); send(exchange, 200, BINARY); });
        server.start();
        try {
            rejectMessage("系统拒绝不可信 TLS 证书", "HTTPS 安全连接失败", () -> execute("GET", "https://127.0.0.1:" + server.getAddress().getPort() + "/", null, null));
            check(accepted.get() == 0, "未绕过证书信任进入 HTTPS 业务处理");
        } finally { server.stop(0); }
    }

    private static String base(HttpServer server) { return "http://127.0.0.1:" + server.getAddress().getPort(); }
    private static SaasHttpClient.Request request(String method, String url, Map<String, String> headers, byte[] body, boolean credentials) throws Exception {
        return new SaasHttpClient.Request(method, url, headers, body, credentials);
    }
    private static SaasHttpClient.Response execute(String method, String url, Map<String, String> headers, byte[] body) throws Exception {
        return CLIENT.newCall(request(method, url, headers, body, false)).execute();
    }
    private static Map<String, String> headers(String name, String value) { Map<String, String> result = new LinkedHashMap<String, String>(); result.put(name, value); return result; }
    private static String repeat(char value, int count) { char[] chars = new char[count]; Arrays.fill(chars, value); return new String(chars); }
    private static byte[] read(InputStream input) throws IOException {
        try (InputStream source = input; ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[8192]; int length;
            while ((length = source.read(buffer)) >= 0) output.write(buffer, 0, length);
            return output.toByteArray();
        }
    }
    private static void send(HttpExchange exchange, int status, byte[] body) throws IOException {
        try {
            if ("HEAD".equals(exchange.getRequestMethod())) { exchange.sendResponseHeaders(status, -1); return; }
            exchange.sendResponseHeaders(status, body.length == 0 ? -1 : body.length);
            if (body.length > 0) try (OutputStream output = exchange.getResponseBody()) { output.write(body); }
        } finally { exchange.close(); }
    }
    private interface Action { void run() throws Exception; }
    private static void reject(String name, Action action) throws Exception { rejectMessage(name, null, action); }
    private static void rejectMessage(String name, String message, Action action) throws Exception {
        try { action.run(); }
        catch (SaasHttpClient.SaasHttpException error) {
            check(error.getCause() == null && error.getMessage().matches(".*[\u4e00-\u9fff].*") &&
                !error.getMessage().contains("password") && !error.getMessage().contains("Bearer") &&
                (message == null || error.getMessage().contains(message)), name + "：中文且隐藏原始异常");
            return;
        }
        throw new AssertionError(name + "：没有拒绝无效操作");
    }
    private static void expectUnsupported(Action action) throws Exception {
        try { action.run(); } catch (UnsupportedOperationException expected) { check(true, "集合不可变"); return; }
        throw new AssertionError("集合可以被修改");
    }
    private static void check(boolean condition, String name) { if (!condition) throw new AssertionError(name); passed++; }
}
