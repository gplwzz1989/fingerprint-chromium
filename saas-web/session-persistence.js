(function installSessionPersistence(global) {
  'use strict';

  const secretKey = 'saas.refresh.v1';
  const revokedKey = 'fingerprint-saas.native-session-revoked.v1';
  const protectedKey = 'fingerprint-saas.native-session-protected.v1';

  // 会话业务留在独立 Web；原生只保存不透明文本，绝不保存快照密码或访问令牌。
  function create({bridge, serviceUrl, deviceId, storage = global.localStorage}) {
    const endpoint = new URL(serviceUrl);
    if (!['http:', 'https:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.hash) {
      throw new Error('会话服务地址无效');
    }
    const service = endpoint.href;
    let enabled = false, revision = 0, tail = Promise.resolve();
    let protectedMode;
    try { protectedMode = storage.getItem(protectedKey) === '1'; }
    catch (_) { protectedMode = true; }

    function serialized(work) {
      const result = tail.then(work);
      tail = result.catch(() => {});
      return result;
    }
    function markRevoked() {
      try { storage.setItem(revokedKey, '1'); }
      catch (_) { throw new Error('无法记录会话退出状态，请检查本地存储权限'); }
    }
    function isRevoked() {
      try { return storage.getItem(revokedKey) === '1'; }
      catch (_) { throw new Error('无法确认本地会话状态，请重新登录'); }
    }
    function validate(record) {
      if (!record || record.schemaVersion !== 1 || record.serviceUrl !== service ||
          Object.keys(record).some((name) => !['schemaVersion', 'serviceUrl', 'refreshToken', 'deviceId'].includes(name)) ||
          typeof record.refreshToken !== 'string' || !record.refreshToken || record.refreshToken.length > 8192 ||
          typeof record.deviceId !== 'string' || !record.deviceId || record.deviceId.length > 128) {
        throw new Error('保存的会话无效或属于其他服务，请重新登录');
      }
      return record;
    }

    return Object.freeze({
      configure(details) {
        // 已启用后遇到掉线不能降级回明文存储；只等待原生能力重新连接。
        if (details?.available && details.capabilities?.secureStorage === true) {
          enabled = true; protectedMode = true;
          // 标志不含秘密；原生端口尚未重连时也不能把令牌降级写回网页存储。
          try { storage.setItem(protectedKey, '1'); } catch (_) { /* 仍在本页面保持保护模式。 */ }
        }
        return enabled;
      },
      isEnabled: () => enabled,
      requiresSecureStorage: () => protectedMode,
      save(session) {
        const current = ++revision;
        if (!enabled) return protectedMode ? Promise.reject(new Error('原生安全存储尚未连接，本次登录仅在当前页面有效')) : Promise.resolve(false);
        let payload;
        try {
          payload = JSON.stringify(validate({schemaVersion: 1, serviceUrl: service,
            refreshToken: session?.refreshToken, deviceId: session?.deviceId || deviceId()}));
          if (new TextEncoder().encode(payload).length > 16384) throw new Error('会话内容超过安全存储限制');
          // 先阻止恢复旧值，只有本次真实写入确认成功后才恢复自动登录。
          markRevoked();
        } catch (error) { return Promise.reject(error); }
        return serialized(async () => {
          if (revision !== current) return false;
          const result = await bridge.secureStorage.set({key: secretKey, value: payload});
          if (revision !== current) return false;
          if (result?.ok !== true) throw new Error('会话安全保存未完成，本次登录仅在当前页面有效');
          try { storage.removeItem(revokedKey); }
          catch (_) { throw new Error('会话已加密保存，但恢复状态未更新，请重新登录'); }
          return true;
        });
      },
      clear() {
        const current = ++revision;
        let markerError = null;
        try { markRevoked(); } catch (error) { markerError = error; }
        if (!enabled) return markerError ? Promise.reject(markerError) : Promise.resolve(false);
        return serialized(async () => {
          if (revision !== current) return false;
          const result = await bridge.secureStorage.remove({key: secretKey});
          if (result?.ok !== true) throw new Error('安全存储会话清除未完成，请恢复原生连接后重试');
          // 即使本地标志不可写，原生真实清除成功也不再存在可恢复令牌。
          return true;
        });
      },
      restore() {
        const current = revision;
        if (!enabled) return Promise.resolve(null);
        return serialized(async () => {
          if (revision !== current || isRevoked()) return null;
          const result = await bridge.secureStorage.get({key: secretKey});
          if (revision !== current || isRevoked()) return null;
          if (result?.value === null) return null;
          if (typeof result?.value !== 'string' || result.value.length > 16384) throw new Error('保存的会话格式无效，请重新登录');
          let record;
          try { record = JSON.parse(result.value); }
          catch (_) { throw new Error('保存的会话格式无效，请重新登录'); }
          return validate(record);
        });
      },
    });
  }

  if (typeof module !== 'undefined' && module.exports) module.exports = {create};
  else global.saasSessionPersistence = {create};
})(typeof window === 'undefined' ? globalThis : window);
