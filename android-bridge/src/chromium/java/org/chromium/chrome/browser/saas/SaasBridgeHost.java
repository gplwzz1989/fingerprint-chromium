package org.chromium.chrome.browser.saas;

import com.fingerprint.saas.bridge.SaasBridgeDispatcher;
import com.fingerprint.saas.bridge.SaasOriginPolicy;

import org.chromium.base.task.PostTask;
import org.chromium.base.task.TaskTraits;
import org.chromium.build.annotations.NullMarked;
import org.chromium.build.annotations.Nullable;
import org.chromium.chrome.browser.tab.EmptyTabObserver;
import org.chromium.chrome.browser.tab.Tab;
import org.chromium.content_public.browser.GlobalRenderFrameHostId;
import org.chromium.content_public.browser.LifecycleState;
import org.chromium.content_public.browser.MessagePayload;
import org.chromium.content_public.browser.MessagePayloadType;
import org.chromium.content_public.browser.MessagePort;
import org.chromium.content_public.browser.NavigationHandle;
import org.chromium.content_public.browser.Page;
import org.chromium.content_public.browser.RenderFrameHost;
import org.chromium.content_public.browser.WebContents;
import org.chromium.content_public.browser.WebContentsObserver;
import org.chromium.url.Origin;

import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicBoolean;

/** 只绑定原生常驻控制台；端口仅交给已校验的真实主框架，网页不能自报授权来源。 */
@NullMarked
public final class SaasBridgeHost extends EmptyTabObserver {
    private static final String NATIVE_SOURCE = "https://fingerprint-native.invalid";
    private static final long MAX_PENDING_BYTES = 64L * 1024 * 1024;
    private final Tab mTab;
    private final SaasOriginPolicy mPolicy;
    private final SaasBridgeDispatcher mDispatcher;
    private final @Nullable SaasBridgeDispatcher.NativeBackend mNativeBackend;
    private final AtomicLong mGeneration = new AtomicLong();
    private final ThreadPoolExecutor mWorker = new ThreadPoolExecutor(
            2, 2, 30, TimeUnit.SECONDS, new ArrayBlockingQueue<>(4), task -> {
                Thread thread = new Thread(task, "SaasBridgeWorker");
                thread.setDaemon(true);
                return thread;
            });
    private @Nullable WebContentsObserver mObserver;
    private @Nullable MessagePort mPort;
    private boolean mDestroyed;
    private long mPendingBytes;

    public SaasBridgeHost(Tab tab, List<String> allowedOrigins, String startupUrl) {
        this(tab, allowedOrigins, startupUrl, null);
    }

    public SaasBridgeHost(Tab tab, List<String> allowedOrigins, String startupUrl,
            @Nullable SaasBridgeDispatcher.NativeBackend nativeBackend) {
        mTab = tab;
        mPolicy = new SaasOriginPolicy(allowedOrigins, startupUrl);
        mDispatcher = new SaasBridgeDispatcher(mPolicy);
        mNativeBackend = nativeBackend;
        mWorker.allowCoreThreadTimeOut(true);
        tab.addObserver(this);
        bindWebContents();
    }

    @Override
    public void onContentChanged(Tab tab) { bindWebContents(); }

    @Override
    public void onDestroyed(Tab tab) { destroy(); }

    public void destroy() {
        if (mDestroyed) return;
        mDestroyed = true;
        revoke();
        mWorker.shutdownNow();
        if (mObserver != null) { mObserver.observe(null); mObserver = null; }
        mTab.removeObserver(this);
    }

    private void bindWebContents() {
        if (mDestroyed) return;
        revoke();
        if (mObserver != null) mObserver.observe(null);
        WebContents contents = mTab.getWebContents();
        if (contents == null || contents.isDestroyed()) { mObserver = null; return; }
        mObserver = new WebContentsObserver(contents) {
            @Override
            public void didStartNavigationInPrimaryMainFrame(NavigationHandle navigation) {
                if (!navigation.isSameDocument()) revoke();
            }
            @Override
            public void didFinishNavigationInPrimaryMainFrame(NavigationHandle navigation) {
                if (!navigation.isSameDocument() && !navigation.hasCommitted() && !navigation.isErrorPage()) connect(contents);
            }
            @Override
            public void documentLoadedInPrimaryMainFrame(
                    Page page, GlobalRenderFrameHostId frameId, @LifecycleState int state) {
                connect(contents);
            }
            @Override
            public void primaryPageChanged(Page page) {
                revoke();
                PostTask.postTask(TaskTraits.UI_DEFAULT, () -> {
                    if (!contents.isDestroyed() && !contents.isLoading()) connect(contents);
                });
            }
            @Override
            public void primaryMainFrameRenderProcessGone(int terminationStatus) { revoke(); }
            @Override
            public void webContentsDestroyed() { revoke(); }
        };
        // 已恢复的控制台可能已经完成加载，消息端口仍通过真实主框架定向发送。
        PostTask.postTask(TaskTraits.UI_DEFAULT, () -> {
            if (!contents.isDestroyed() && !contents.isLoading()) connect(contents);
        });
    }

    private @Nullable String trustedOrigin(WebContents contents) {
        if (mDestroyed || contents.isDestroyed() || mTab.getWebContents() != contents) return null;
        RenderFrameHost frame = contents.getMainFrame();
        Origin origin = frame.getLastCommittedOrigin();
        if (origin == null || origin.isOpaque()) return null;
        String value = origin.toString();
        return mPolicy.matchesOrigin(value) ? value : null;
    }

    private void connect(WebContents contents) {
        if (mPort != null) return;
        String origin = trustedOrigin(contents);
        if (origin == null) return;
        MessagePort[] pair = contents.createMessageChannel();
        MessagePort port = pair[0];
        mPort = port;
        long generation = mGeneration.get();
        port.setMessageCallback((payload, sentPorts) -> {
            boolean extraPorts = sentPorts != null && sentPorts.length > 0;
            if (extraPorts) for (MessagePort extra : sentPorts) extra.close();
            if (mPort != port || mGeneration.get() != generation) return;
            if (extraPorts) {
                revoke(); return;
            }
            if (!origin.equals(trustedOrigin(contents))) return;
            if (payload.getType() != MessagePayloadType.STRING) { revoke(); return; }
            String message = payload.getAsString();
            if (message == null || message.length() > SaasBridgeDispatcher.MAX_MESSAGE_BYTES) { revoke(); return; }
            long reserved = 2L * message.length();
            if (mPendingBytes + reserved > MAX_PENDING_BYTES) { revoke(); return; }
            mPendingBytes += reserved;
            BridgeWork work = new BridgeWork(message, origin, contents, port, generation, reserved);
            try {
                mWorker.execute(work);
            } catch (RejectedExecutionException error) { work.release(); revoke(); }
        }, null);
        try {
            contents.postMessageToMainFrame(new MessagePayload(
                    "{\"type\":\"fingerprint-saas-bridge:init\",\"version\":\"1.0\",\"origin\":\"" + origin + "\"}"),
                    NATIVE_SOURCE, origin, new MessagePort[] {pair[1]});
        } catch (RuntimeException error) {
            revoke();
            if (!pair[1].isTransferred() && !pair[1].isClosed()) pair[1].close();
        }
    }

    private final class BridgeWork implements Runnable {
        private final String mMessage, mOrigin;
        private final WebContents mContents;
        private final MessagePort mWorkPort;
        private final long mWorkGeneration, mReserved;
        private final AtomicBoolean mReleased = new AtomicBoolean();

        BridgeWork(String message, String origin, WebContents contents,
                MessagePort port, long generation, long reserved) {
            mMessage = message; mOrigin = origin; mContents = contents;
            mWorkPort = port; mWorkGeneration = generation; mReserved = reserved;
        }
        void release() {
            // 计费跨导航代次保留；只有实际完成或从等待队列取消时才释放预算。
            if (mReleased.compareAndSet(false, true)) mPendingBytes -= mReserved;
        }
        @Override public void run() {
            try {
                if (mGeneration.get() != mWorkGeneration) return;
                String response = mDispatcher.dispatch(mMessage, mOrigin,
                        mNativeBackend == null ? null : new SaasBridgeDispatcher.NativeBackend() {
                            @Override public boolean supports(String capability) {
                                return mNativeBackend != null && mNativeBackend.supports(capability);
                            }
                            @Override public Object invoke(String method, Map<String, Object> args) {
                                return invokeOnUiThread(method, args);
                            }
                        });
                PostTask.postTask(TaskTraits.UI_DEFAULT, () -> {
                    if (mGeneration.get() != mWorkGeneration) return;
                    try {
                        if (mPort == mWorkPort && mOrigin.equals(trustedOrigin(mContents)) && !mWorkPort.isClosed()) {
                            mWorkPort.postMessage(new MessagePayload(response), null);
                        }
                    } catch (RuntimeException error) { revoke(); }
                });
            } catch (RuntimeException error) {
                PostTask.postTask(TaskTraits.UI_DEFAULT, () -> {
                    if (mGeneration.get() == mWorkGeneration) revoke();
                });
            } finally { PostTask.postTask(TaskTraits.UI_DEFAULT, this::release); }
        }

        private Object invokeOnUiThread(String method, Map<String, Object> args) {
            CompletableFuture<Object> result = new CompletableFuture<>();
            long timeoutSeconds = method.startsWith("storage.") ? 35 : 15;
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(timeoutSeconds);
            SaasBridgeDispatcher.RequestAuthority authority = () -> !result.isDone() &&
                    System.nanoTime() < deadline && !mDestroyed &&
                    mGeneration.get() == mWorkGeneration && mPort == mWorkPort &&
                    !mWorkPort.isClosed() && mOrigin.equals(trustedOrigin(mContents));
            PostTask.postTask(TaskTraits.UI_DEFAULT, () -> {
                if (result.isDone()) return;
                try {
                    if (System.nanoTime() >= deadline || mDestroyed ||
                            mGeneration.get() != mWorkGeneration || mPort != mWorkPort ||
                            mWorkPort.isClosed() || !mOrigin.equals(trustedOrigin(mContents))) {
                        result.completeExceptionally(new SaasBridgeDispatcher.NativeRequestException("页面授权已失效，标签操作已停止"));
                        return;
                    }
                    if (mNativeBackend == null) throw new SaasBridgeDispatcher.NativeRequestException("当前客户端尚未提供标签管理能力");
                    mNativeBackend.invokeAsync(method, args, authority).whenComplete((value, error) -> {
                        if (error != null) result.completeExceptionally(error);
                        else result.complete(value);
                    });
                } catch (RuntimeException error) { result.completeExceptionally(error); }
                catch (LinkageError error) {
                    result.completeExceptionally(new SaasBridgeDispatcher.NativeRequestException("客户端原生接口版本不匹配，请更新客户端"));
                }
            });
            try { return result.get(timeoutSeconds, TimeUnit.SECONDS); }
            catch (InterruptedException error) {
                Thread.currentThread().interrupt();
                result.cancel(false);
                throw new SaasBridgeDispatcher.NativeRequestException("标签操作已取消");
            } catch (TimeoutException error) {
                result.cancel(false);
                throw new SaasBridgeDispatcher.NativeRequestException("标签操作等待超时，请重试");
            } catch (ExecutionException error) {
                if (error.getCause() instanceof SaasBridgeDispatcher.NativeRequestException) {
                    throw (SaasBridgeDispatcher.NativeRequestException) error.getCause();
                }
                throw new SaasBridgeDispatcher.NativeRequestException("标签操作失败，请检查账号环境后重试");
            }
        }
    }

    private void revoke() {
        mGeneration.incrementAndGet();
        Runnable queued;
        while ((queued = mWorker.getQueue().poll()) != null) ((BridgeWork) queued).release();
        if (mPort != null) {
            MessagePort port = mPort;
            mPort = null;
            try {
                if (!port.isClosed()) port.postMessage(new MessagePayload("{\"type\":\"fingerprint-saas-bridge:revoked\"}"), null);
            } catch (RuntimeException error) {
                // WebContents 销毁或渲染进程退出时端口可能已经无效，仍需释放本地持有状态。
            } finally {
                try { if (!port.isClosed()) port.close(); } catch (RuntimeException error) { /* 已失效的端口无需继续操作。 */ }
            }
        }
    }
}
