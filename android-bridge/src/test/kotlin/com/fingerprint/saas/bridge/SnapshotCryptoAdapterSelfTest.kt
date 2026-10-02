package com.fingerprint.saas.bridge

import kotlin.coroutines.Continuation
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.coroutines.startCoroutine

/** 直接执行真实适配器，不使用 Android 或协程库模拟桥接业务。 */
object SnapshotCryptoAdapterSelfTest {
    @JvmStatic
    fun main(args: Array<String>) {
        val adapter: SaasBridgeContract.Crypto = JvmSnapshotCryptoAdapter()
        val account = "Kotlin账号-😀"
        val password = "Kotlin测试密码-😀-123456"
        val snapshot = mapOf<String, Any?>(
            "schema_version" to 1, "account_id" to account, "cookies" to emptyList<Any>(),
            "local_storage" to mapOf("中文键😀" to "内容\u0000\ud800"),
            "storage_url" to "https://example.com/",
        )
        val options = SnapshotEncryptOptions(account, password, snapshot)
        val envelope = invoke { adapter.encryptSnapshot(options) }
        val decryptOptions = SnapshotDecryptOptions(account, password, envelope)
        val result = invoke { adapter.decryptSnapshot(decryptOptions) }
        check(result["account_id"] == account && result["local_storage"] == snapshot["local_storage"]) {
            "Kotlin 适配器往返验证失败"
        }
        check(envelope.algorithm == "AES-256-GCM" && envelope.kdf == "PBKDF2-HMAC-SHA-256"
            && envelope.iterations == 600000) { "Kotlin 信封字段验证失败" }
        check(!options.toString().contains(password) && !decryptOptions.toString().contains(password)) {
            "参数显示泄露密码"
        }
        check(!SaasBridgeCapabilities(false, false, false, false, false).crypto) { "未绑定消息通道却启用能力" }
        reject { invoke { adapter.decryptSnapshot(decryptOptions.copy(accountId = "另一个账号")) } }
        reject { invoke { adapter.decryptSnapshot(decryptOptions.copy(password = password + "错误")) } }
        reject { invoke { adapter.encryptSnapshot(options.copy(password = "")) } }
        reject { invoke { adapter.encryptSnapshot(options.copy(password = "a".repeat(4097))) } }
        println("Kotlin 适配器自测通过：8 项")
    }

    private fun reject(action: () -> Unit) {
        try {
            action()
        } catch (error: SnapshotCrypto.SnapshotCryptoException) {
            check(error.cause == null && error.message?.any { it in '\u4e00'..'\u9fff' } == true) {
                "适配器错误包装验证失败"
            }
            return
        }
        error("适配器未拒绝无效参数")
    }

    private fun <T : Any> invoke(action: suspend () -> T): T {
        var value: Result<T>? = null
        action.startCoroutine(object : Continuation<T> {
            override val context = EmptyCoroutineContext
            override fun resumeWith(result: Result<T>) { value = result }
        })
        return value?.getOrThrow() ?: error("适配器意外切换线程，请检查测试入口")
    }
}
