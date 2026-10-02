package com.fingerprint.saas.bridge

import java.util.Arrays

/** 仅提供计算适配；Chromium 消息通道必须另行绑定、校验来源并在工作线程调用。 */
class JvmSnapshotCryptoAdapter : SaasBridgeContract.Crypto {
    private val engine = SnapshotCrypto()

    override suspend fun encryptSnapshot(options: SnapshotEncryptOptions): SaasSnapshotEnvelope {
        val password = passwordCharacters(options.password)
        try {
            val envelope = engine.encryptSnapshot(options.accountId, password, options.snapshot)
            return SaasSnapshotEnvelope(
                envelope.algorithm, envelope.kdf, envelope.iterations,
                envelope.salt, envelope.nonce, envelope.ciphertext, envelope.tag,
            )
        } finally {
            Arrays.fill(password, '\u0000')
        }
    }

    override suspend fun decryptSnapshot(options: SnapshotDecryptOptions): Map<String, Any?> {
        val password = passwordCharacters(options.password)
        try {
            val value = options.envelope
            return engine.decryptSnapshot(options.accountId, password, SnapshotCrypto.Envelope(
                value.algorithm, value.kdf, value.iterations,
                value.salt, value.nonce, value.ciphertext, value.tag,
            ))
        } finally {
            Arrays.fill(password, '\u0000')
        }
    }

    private fun passwordCharacters(password: String): CharArray {
        if (password.length !in 12..SnapshotCrypto.MAX_PASSWORD_CHARACTERS) {
            throw SnapshotCrypto.failure("快照密码需要 12 至 4096 位")
        }
        return password.toCharArray()
    }
}
