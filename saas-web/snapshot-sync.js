(function installSnapshotSync(global) {
  'use strict';

  const iterations = 600000;
  const optionKeys = ['cookies', 'local_storage', 'session_storage', 'fingerprint', 'proxy', 'page'];
  const defaults = Object.freeze(Object.fromEntries(optionKeys.map((key) => [key, true])));
  const nativePasswords = new WeakMap();
  const fail = (message) => { throw new Error(message); };
  const record = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

  function optionsFor(snapshot) {
    const value = snapshot.sync_options ?? defaults;
    if (!record(value) || !optionKeys.every((key) => typeof value[key] === 'boolean')) {
      fail('账号快照的同步选项无效');
    }
    const options = Object.fromEntries(optionKeys.map((key) => [key, value[key]]));
    if (options.local_storage || options.session_storage) options.page = true;
    return options;
  }

  function validate(snapshot, accountId) {
    if (!record(snapshot) || snapshot.account_id !== accountId || snapshot.schema_version !== 1 ||
        !Array.isArray(snapshot.cookies) || !record(snapshot.local_storage) ||
        (snapshot.session_storage !== undefined && !record(snapshot.session_storage))) {
      fail('账号快照的版本、标识或存储结构无效');
    }
    for (const values of [snapshot.local_storage, snapshot.session_storage || {}]) {
      if (!Object.values(values).every((value) => typeof value === 'string')) fail('网页存储内容无效');
    }
    if (snapshot.cookies.length > 5000 || !snapshot.cookies.every((cookie) => record(cookie) &&
        ['name', 'value', 'domain', 'path'].every((name) => typeof cookie[name] === 'string'))) {
      fail('Cookie 内容无效或数量超过支持范围');
    }
    const options = optionsFor(snapshot);
    if (options.page && snapshot.storage_url) {
      let url;
      try { url = new URL(snapshot.storage_url); } catch (_) { fail('账号页面地址无效'); }
      if (!['http:', 'https:', 'about:'].includes(url.protocol) ||
          (url.protocol === 'about:' && url.href !== 'about:blank') || url.username || url.password) {
        fail('账号页面地址无效');
      }
    }
    if ((options.local_storage || options.session_storage) && !/^https?:\/\//i.test(snapshot.storage_url || '')) {
      fail('网页存储快照缺少有效的来源页面');
    }
    if (options.fingerprint && snapshot.fingerprint !== undefined &&
        (!record(snapshot.fingerprint) || typeof snapshot.fingerprint.user_agent !== 'string' ||
         snapshot.fingerprint.user_agent.length > 512 ||
         !Number.isInteger(snapshot.fingerprint.hardware_concurrency) ||
         snapshot.fingerprint.hardware_concurrency < 0 || snapshot.fingerprint.hardware_concurrency > 64)) {
      fail('账号指纹配置无效');
    }
    return options;
  }

  function select(raw, accountId, requested = defaults) {
    const options = optionsFor({sync_options: requested});
    // 未选择的数据只影响本次快照，恢复时不能清空本地未选中的类别。
    const snapshot = {
      schema_version: 1, account_id: accountId, sync_options: options,
      cookies: options.cookies ? raw.cookies : [],
      local_storage: options.local_storage ? raw.local_storage : {},
      session_storage: options.session_storage ? (raw.session_storage || {}) : {},
    };
    if (options.page) snapshot.storage_url = raw.storage_url;
    if (options.proxy) snapshot.proxy_rules = raw.proxy_rules || '';
    if (options.fingerprint) {
      snapshot.fingerprint_seed = raw.fingerprint_seed || '';
      if (raw.fingerprint) snapshot.fingerprint = raw.fingerprint;
    }
    validate(snapshot, accountId);
    return snapshot;
  }

  function mergeSnapshots(local, remote, accountId) {
    validate(local, accountId); validate(remote, accountId);
    const localOptions = optionsFor(local), remoteOptions = optionsFor(remote);
    const options = Object.fromEntries(optionKeys.map((name) => [name, localOptions[name] || remoteOptions[name]]));
    const localHasStorage = localOptions.local_storage || localOptions.session_storage;
    const remoteHasStorage = remoteOptions.local_storage || remoteOptions.session_storage;
    if (localHasStorage && remoteHasStorage && new URL(local.storage_url).origin !== new URL(remote.storage_url).origin) {
      fail('本地与云端网页存储来源不同，不能合并；可保留云端或明确覆盖');
    }
    const storageUrl = localHasStorage ? local.storage_url : remoteHasStorage ? remote.storage_url :
      localOptions.page ? local.storage_url : remote.storage_url;
    const merged = {schema_version: 1, account_id: accountId, sync_options: options, storage_url: storageUrl};
    const cookies = new Map();
    // 同名 Cookie 仍按域、路径和分区身份区分，只有完全相同的身份才以本地覆盖。
    for (const cookie of [...(remoteOptions.cookies ? remote.cookies : []), ...(localOptions.cookies ? local.cookies : [])]) {
      const partition = cookie.partition_key;
      const identity = JSON.stringify([cookie.name, cookie.domain, cookie.path,
        partition?.top_level_site || '', partition?.has_cross_site_ancestor === true]);
      cookies.set(identity, cookie);
    }
    merged.cookies = [...cookies.values()];
    for (const name of ['local_storage', 'session_storage']) {
      merged[name] = {...(remoteOptions[name] ? remote[name] || {} : {}), ...(localOptions[name] ? local[name] || {} : {})};
    }
    for (const [name, option] of [['fingerprint', 'fingerprint'], ['fingerprint_seed', 'fingerprint'], ['proxy_rules', 'proxy']]) {
      const selected = localOptions[option] ? local : remote;
      if (selected[name] !== undefined) merged[name] = selected[name];
    }
    if (!options.page) delete merged.storage_url;
    validate(merged, accountId);
    return merged;
  }

  function encode(bytes) {
    let binary = '';
    for (let offset = 0; offset < bytes.length; offset += 8192) {
      binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
    }
    return global.btoa(binary);
  }

  function decode(value, max) {
    if (typeof value !== 'string' || value.length > Math.ceil(max / 3) * 4 + 4) fail('加密快照格式无效');
    let binary;
    try { binary = global.atob(value); } catch (_) { fail('加密快照格式无效'); }
    if (binary.length > max) fail('加密快照超过大小限制');
    return Uint8Array.from(binary, (char) => char.charCodeAt(0));
  }

  async function passwordKey(password) {
    if (typeof password !== 'string' || password.length < 12) fail('快照密码至少需要 12 位');
    if (!global.crypto?.subtle) {
      const details = await global.saasBridgeClient?.describe();
      if (!details?.available || details.capabilities?.crypto !== true) {
        fail('当前页面没有可用的快照加密能力，请在支持原生加密的客户端中打开');
      }
      const key = Object.freeze({native: true});
      nativePasswords.set(key, password);
      return key;
    }
    return global.crypto.subtle.importKey('raw', new TextEncoder().encode(password), 'PBKDF2', false, ['deriveKey']);
  }

  function derive(key, salt, count) {
    return global.crypto.subtle.deriveKey({name: 'PBKDF2', salt, iterations: count, hash: 'SHA-256'},
      key, {name: 'AES-GCM', length: 256}, false, ['encrypt', 'decrypt']);
  }

  async function encrypt(key, accountId, snapshot) {
    if (!key) fail('请先解锁加密快照');
    validate(snapshot, accountId);
    if (nativePasswords.has(key)) {
      return global.saasBridgeClient.crypto.encryptSnapshot({accountId, snapshot, password: nativePasswords.get(key)});
    }
    const plaintext = new TextEncoder().encode(JSON.stringify(snapshot));
    if (plaintext.length > 14 * 1024 * 1024) fail('账号快照过大，请减少同步内容');
    const salt = global.crypto.getRandomValues(new Uint8Array(16));
    const nonce = global.crypto.getRandomValues(new Uint8Array(12));
    const encrypted = new Uint8Array(await global.crypto.subtle.encrypt({name: 'AES-GCM', iv: nonce,
      additionalData: new TextEncoder().encode('fingerprint-manager:v1:' + accountId)},
    await derive(key, salt, iterations), plaintext));
    return {algorithm: 'AES-256-GCM', kdf: 'PBKDF2-HMAC-SHA-256', iterations,
      salt: encode(salt), nonce: encode(nonce), ciphertext: encode(encrypted.subarray(0, -16)),
      tag: encode(encrypted.subarray(-16))};
  }

  async function decrypt(key, accountId, envelope) {
    if (!key) fail('请先解锁加密快照');
    if (!record(envelope) || envelope.algorithm !== 'AES-256-GCM' ||
        envelope.kdf !== 'PBKDF2-HMAC-SHA-256' || !Number.isInteger(envelope.iterations) ||
        envelope.iterations < iterations || envelope.iterations > 2000000) fail('快照加密参数不受支持');
    if (nativePasswords.has(key)) {
      const snapshot = await global.saasBridgeClient.crypto.decryptSnapshot({accountId, envelope, password: nativePasswords.get(key)});
      validate(snapshot, accountId);
      return snapshot;
    }
    const salt = decode(envelope.salt, 64), nonce = decode(envelope.nonce, 12);
    const ciphertext = decode(envelope.ciphertext, 16 * 1024 * 1024), tag = decode(envelope.tag, 16);
    if (salt.length < 16 || nonce.length !== 12 || tag.length !== 16 || ciphertext.length < 16) fail('加密快照格式无效');
    const encrypted = new Uint8Array(ciphertext.length + tag.length);
    encrypted.set(ciphertext); encrypted.set(tag, ciphertext.length);
    let snapshot;
    try {
      const plaintext = await global.crypto.subtle.decrypt({name: 'AES-GCM', iv: nonce,
        additionalData: new TextEncoder().encode('fingerprint-manager:v1:' + accountId)},
      await derive(key, salt, envelope.iterations), encrypted);
      snapshot = JSON.parse(new TextDecoder('utf-8', {fatal: true}).decode(plaintext));
    } catch (_) { fail('快照解密失败，请核对快照密码与账号'); }
    validate(snapshot, accountId);
    return snapshot;
  }

  function createController({request, bridge, deviceId, getKey, getOptions, getSession = () => null}) {
    const busy = new Set();
    const conflicts = new Map();
    const path = (id) => '/api/v1/accounts/' + encodeURIComponent(id);
    function checkContext(key, session) {
      if (getSession() !== session || getKey() !== key) fail('会话或快照密钥已变化，操作已停止');
    }
    async function exclusive(id, action) {
      if (busy.has(id)) fail('该账号正在同步或恢复，请等待完成');
      busy.add(id);
      try { return await action(); } finally { busy.delete(id); }
    }
    async function tabs() {
      const values = await bridge.tabs.list();
      if (!Array.isArray(values)) fail('客户端返回的 Tab 列表无效');
      return values;
    }
    async function waitForPage(tabId, url, key, session) {
      const expected = new URL(url).origin;
      const deadline = Date.now() + 15000;
      while (Date.now() < deadline) {
        checkContext(key, session);
        const tab = (await tabs()).find((value) => value.id === tabId);
        checkContext(key, session);
        if (!tab) fail('账号 Tab 已关闭，恢复已停止');
        let origin = '';
        try { origin = new URL(tab.url).origin; } catch (_) { /* 页面正在导航。 */ }
        if (origin === expected && tab.load_progress >= 1) return;
        await new Promise((resolve) => global.setTimeout(resolve, 200));
      }
      fail('账号页面未能及时完成加载，请检查网络后重试恢复');
    }
    async function save(account, snapshot, key, session, revision, overwrite = false) {
      const envelope = await encrypt(key, account.account_id, snapshot);
      checkContext(key, session);
      const lease = await request(path(account.account_id) + '/leases', {method: 'POST', body: {device_id: deviceId()}});
      try {
        checkContext(key, session);
        const saved = await request(path(account.account_id) + '/snapshot', {
          method: 'PUT', headers: {'If-Match': String(revision)},
          body: {schema_version: 1, device_id: deviceId(), envelope, ...(overwrite ? {overwrite: true} : {})},
        });
        conflicts.delete(account.account_id);
        return saved;
      } catch (error) {
        if (error?.code === 'snapshot_revision_conflict' && getSession() === session && getKey() === key) conflicts.set(account.account_id, {snapshot, key, session,
          revision: error.currentRevision});
        throw error;
      } finally {
        // 租约清理失败不能掩盖已成功上传的结果；租约会由服务端自动到期。
        try { await request(path(account.account_id) + '/leases/' + encodeURIComponent(lease.lease_id), {method: 'DELETE'}); }
        catch (_) { /* 清理失败由短期租约到期回收。 */ }
      }
    }
    return Object.freeze({
      clearConflicts() { conflicts.clear(); },
      discardConflict(accountId) { conflicts.delete(accountId); },
      getConflict(accountId) {
        const value = conflicts.get(accountId);
        return value ? {accountId, revision: value.revision} : null;
      },
      resolveConflict(account, strategy) {
        return exclusive(account.account_id, async () => {
          const pending = conflicts.get(account.account_id);
          if (!pending) fail('待处理的冲突已清除，请重新同步');
          if (getKey() !== pending.key || getSession() !== pending.session) fail('会话或快照密钥已变化，请重新同步');
          if (!['merge', 'overwrite'].includes(strategy)) fail('冲突处理方式无效');
          const response = await request(path(account.account_id) + '/snapshot');
          if (response.account_id !== account.account_id || response.schema_version !== 1 || !Number.isSafeInteger(response.revision)) {
            fail('云端快照标识或版本无效');
          }
          const remote = await decrypt(pending.key, account.account_id, response.envelope);
          const snapshot = strategy === 'merge' ? mergeSnapshots(pending.snapshot, remote, account.account_id) : pending.snapshot;
          // 使用刚读取的版本进行条件写入，不用通配符覆盖尚未查看的新版本。
          return save(account, snapshot, pending.key, pending.session, response.revision, strategy === 'overwrite');
        });
      },
      upload(account) {
        return exclusive(account.account_id, async () => {
          const session = getSession(), key = getKey();
          if (!key) fail('请先解锁加密快照');
          const tab = (await tabs()).find((value) => value.account_id === account.account_id);
          checkContext(key, session);
          if (!tab) fail('请先打开该账号的隔离 Tab');
          const requested = optionsFor({sync_options: getOptions()});
          const raw = await bridge.storage.getSnapshot(tab.id, requested);
          checkContext(key, session);
          const snapshot = select(raw, account.account_id, requested);
          return save(account, snapshot, key, session, account.revision);
        });
      },
      restore(account) {
        return exclusive(account.account_id, async () => {
          const session = getSession(), key = getKey();
          if (!key) fail('请先解锁加密快照');
          const response = await request(path(account.account_id) + '/snapshot');
          if (response.account_id !== account.account_id || response.schema_version !== 1) fail('云端快照标识或版本无效');
          const snapshot = await decrypt(key, account.account_id, response.envelope);
          checkContext(key, session);
          const options = optionsFor(snapshot);
          let tab = (await tabs()).find((value) => value.account_id === account.account_id);
          checkContext(key, session);
          if (tab && ((options.proxy && tab.proxy_rules !== (snapshot.proxy_rules || '')) ||
              (options.fingerprint && tab.fingerprint_seed !== (snapshot.fingerprint_seed || '')))) {
            fail('当前 Tab 的代理或指纹种子与云端不同，请先关闭该账号 Tab 再恢复');
          }
          const url = options.page ? snapshot.storage_url || 'about:blank' : tab?.url || 'about:blank';
          if (!tab) tab = await bridge.tabs.create({accountId: account.account_id, url,
            ...(options.proxy ? {proxyRules: snapshot.proxy_rules || ''} : {}),
            ...(options.fingerprint ? {fingerprintSeed: snapshot.fingerprint_seed || ''} : {})});
          checkContext(key, session);
          if (options.fingerprint && snapshot.fingerprint) await bridge.fingerprint.set(tab.id, snapshot.fingerprint);
          checkContext(key, session);
          if (options.page && snapshot.storage_url) {
            if (await bridge.tabs.navigate(tab.id, url) === false) fail('无法导航到账号页面');
            if (/^https?:/i.test(url)) await waitForPage(tab.id, url, key, session);
          }
          checkContext(key, session);
          if (await bridge.storage.writeSnapshot(tab.id, snapshot) === false) fail('账号快照写入失败');
          checkContext(key, session);
          await bridge.tabs.activate(tab.id);
          return {tab, revision: response.revision};
        });
      },
    });
  }

  const api = Object.freeze({defaults, passwordKey, encrypt, decrypt, select, validate, mergeSnapshots, createController});
  global.saasSnapshotSync = api;
  if (typeof module === 'object' && module.exports) module.exports = api;
})(typeof window === 'undefined' ? globalThis : window);
