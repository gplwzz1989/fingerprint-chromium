package com.fingerprint.saas.bridge

const val saasBridgeMajorVersion: Int = 1

data class SaasBridgeCapabilities(
    val tabs: Boolean,
    val storage: Boolean,
    val fingerprint: Boolean,
    val files: Boolean,
    val http: Boolean,
)

data class SaasBridgeDetails(
    val version: String,
    val origin: String,
    val capabilities: SaasBridgeCapabilities,
)

data class TabCreateOptions(
    val url: String? = null,
    val accountId: String? = null,
    val proxyRules: String? = null,
    val fingerprintSeed: String? = null,
)

data class SaasFileEntry(
    val name: String,
    val path: String,
    val directory: Boolean,
    val size: Long,
)

data class SaasFileReadResult(
    val path: String,
    val size: Long,
    val data: ByteArray,
)

data class SaasFileWriteResult(
    val path: String,
    val size: Long,
)

data class HttpRequest(
    val method: String = "GET",
    val url: String,
    val headers: Map<String, String> = emptyMap(),
    val body: ByteArray? = null,
    val contentType: String? = null,
    val includeCredentials: Boolean = false,
)

data class HttpResponse(
    val status: Int,
    val headers: Map<String, String>,
    val body: ByteArray,
)

interface SaasBridgeContract {
    suspend fun getCapabilities(): SaasBridgeDetails

    val tabs: Tabs
    val storage: Storage
    val fingerprint: Fingerprint
    val files: Files
    val http: Http

    interface Tabs {
        suspend fun list(): List<Map<String, Any?>>
        suspend fun create(options: TabCreateOptions): Map<String, Any?>
        suspend fun activate(tabId: String): Boolean
        suspend fun navigate(tabId: String, url: String): Boolean
        suspend fun close(tabId: String): Boolean
    }

    interface Storage {
        suspend fun getSnapshot(
            tabId: String,
            options: Map<String, Any?> = emptyMap(),
        ): Map<String, Any?>

        suspend fun writeSnapshot(
            tabId: String,
            snapshot: Map<String, Any?>,
            options: Map<String, Any?> = emptyMap(),
        ): Boolean
    }

    interface Fingerprint {
        suspend fun get(tabId: String): Map<String, Any?>
        suspend fun set(tabId: String, fingerprint: Map<String, Any?>): Map<String, Any?>
    }

    interface Files {
        suspend fun list(options: Map<String, Any?> = emptyMap()): List<SaasFileEntry>
        suspend fun read(options: Map<String, Any?>): SaasFileReadResult
        suspend fun write(options: Map<String, Any?>): SaasFileWriteResult
    }

    interface Http {
        suspend fun request(request: HttpRequest): HttpResponse
    }
}
