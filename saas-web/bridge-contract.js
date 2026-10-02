(function installSaasBridgeClient(global) {
  'use strict';

  const expectedMajorVersion = 1;

  class BridgeUnavailableError extends Error {
    constructor(message) {
      super(message);
      this.name = 'BridgeUnavailableError';
      this.code = 'BRIDGE_UNAVAILABLE';
    }
  }

  let embeddedBridge = null;
  let embeddedRequestSequence = 0;
  const embeddedRequests = new Map();

  function getEmbeddedBridge() {
    if (global.parent === global) return null;
    if (embeddedBridge) return embeddedBridge;

    global.addEventListener('message', (event) => {
      if (event.source !== global.parent) return;
      const message = event.data;
      if (!message || message.type !== 'fingerprint-saas-bridge:response' ||
          typeof message.requestId !== 'string') {
        return;
      }
      const request = embeddedRequests.get(message.requestId);
      if (!request) return;
      embeddedRequests.delete(message.requestId);
      global.clearTimeout(request.timeout);
      if (message.ok) {
        request.resolve(message.result);
      } else {
        request.reject(new Error(message.error || '原生桥请求失败'));
      }
    });

    const request = (method, args) => new Promise((resolve, reject) => {
      const requestId = `bridge-${++embeddedRequestSequence}`;
      const timeout = global.setTimeout(() => {
        embeddedRequests.delete(requestId);
        reject(new BridgeUnavailableError('原生桥响应超时，请确认已在客户端中加载控制台'));
      }, method === 'storage.writeSnapshot' ? 60000 : 40000);
      embeddedRequests.set(requestId, {resolve, reject, timeout});
      global.parent.postMessage({
        type: 'fingerprint-saas-bridge:request',
        requestId,
        method,
        args: args || {},
      }, '*');
    });

    embeddedBridge = {
      version: '1.0',
      origin: global.location.origin,
      getCapabilities() { return request('getCapabilities', {}); },
      tabs: {
        list() { return request('tabs.list', {}); },
        create(options) { return request('tabs.create', options); },
        activate(options) { return request('tabs.activate', options); },
        navigate(options) { return request('tabs.navigate', options); },
        close(options) { return request('tabs.close', options); },
      },
      storage: {
        getSnapshot(options) { return request('storage.getSnapshot', options); },
        writeSnapshot(options) { return request('storage.writeSnapshot', options); },
      },
      fingerprint: {
        get(options) { return request('fingerprint.get', options); },
        set(options) { return request('fingerprint.set', options); },
      },
      files: {
        list(options) { return request('files.list', options); },
        read(options) { return request('files.read', options); },
        write(options) { return request('files.write', options); },
      },
      http: {
        request(options) { return request('http.request', options); },
      },
      crypto: {
        encryptSnapshot(options) { return request('crypto.encryptSnapshot', options); },
        decryptSnapshot(options) { return request('crypto.decryptSnapshot', options); },
      },
    };
    return embeddedBridge;
  }

  function getRawBridge() {
    const bridge = global.saasBridge || getEmbeddedBridge();
    if (!bridge || typeof bridge !== 'object') {
      throw new BridgeUnavailableError('当前页面没有连接受支持的浏览器原生桥');
    }
    const version = String(bridge.version || '');
    const majorVersion = Number.parseInt(version.split('.')[0], 10);
    if (!Number.isInteger(majorVersion) || majorVersion !== expectedMajorVersion) {
      throw new BridgeUnavailableError('浏览器原生桥版本不兼容，请更新客户端');
    }
    return bridge;
  }

  function call(path, args) {
    const bridge = getRawBridge();
    let target = bridge;
    for (const part of path) {
      target = target && target[part];
    }
    if (typeof target !== 'function') {
      throw new BridgeUnavailableError(`浏览器原生桥未提供 ${path.join('.')} 能力`);
    }
    return Promise.resolve(target.call(path.length > 1 ? bridge[path[0]] : bridge, args));
  }

  const capabilityNames = [
    ['tabs', 'Tab 管理'],
    ['storage', 'Cookie 与网页存储'],
    ['fingerprint', '指纹配置'],
    ['files', '本地文件'],
    ['http', '原生 HTTP'],
    ['crypto', '原生快照加密'],
  ];

  const client = Object.freeze({
    expectedMajorVersion,
    isAvailable() {
      try {
        getRawBridge();
        return true;
      } catch (_error) {
        return false;
      }
    },
    describe() {
      try {
        const bridge = getRawBridge();
        const rawCapabilities = typeof bridge.getCapabilities === 'function'
          ? bridge.getCapabilities()
          : bridge.capabilities;
        return Promise.resolve(rawCapabilities).then((details) => {
          const capabilities = details && details.capabilities ?
            details.capabilities : details;
          return {
            available: true,
            version: String(details?.version || bridge.version),
            origin: String(details?.origin || bridge.origin || '客户端编译白名单'),
            capabilities: capabilities && typeof capabilities === 'object' ? capabilities : {},
          };
        }).catch(() => ({available: false, version: '', origin: '', capabilities: {},
          reason: '原生桥未连接或客户端拒绝授权'}));
      } catch (error) {
        return Promise.resolve({
          available: false,
          version: '',
          origin: '',
          capabilities: {},
          reason: error instanceof Error ? error.message : '原生桥不可用',
        });
      }
    },
    capabilityNames,
    tabs: {
      list() { return call(['tabs', 'list']); },
      create(options) { return call(['tabs', 'create'], options || {}); },
      activate(tabId) { return call(['tabs', 'activate'], { tabId }); },
      navigate(tabId, url) { return call(['tabs', 'navigate'], { tabId, url }); },
      close(tabId) { return call(['tabs', 'close'], { tabId }); },
    },
    storage: {
      getSnapshot(tabId, options) { return call(['storage', 'getSnapshot'], { tabId, ...(options || {}) }); },
      writeSnapshot(tabId, snapshot, options) { return call(['storage', 'writeSnapshot'], { tabId, snapshot, ...(options || {}) }); },
    },
    fingerprint: {
      get(tabId) { return call(['fingerprint', 'get'], { tabId }); },
      set(tabId, fingerprint) { return call(['fingerprint', 'set'], { tabId, fingerprint }); },
    },
    files: {
      list(options) { return call(['files', 'list'], options || {}); },
      read(options) { return call(['files', 'read'], options || {}); },
      write(options) { return call(['files', 'write'], options || {}); },
    },
    http: {
      request(options) { return call(['http', 'request'], options || {}); },
    },
    crypto: {
      encryptSnapshot(options) { return call(['crypto', 'encryptSnapshot'], options); },
      decryptSnapshot(options) { return call(['crypto', 'decryptSnapshot'], options); },
    },
  });

  global.saasBridgeClient = client;
})(window);
