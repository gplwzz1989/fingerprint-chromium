// 局部测试实际 app.js 会话函数；界面和网络探针仅用于边界测试，不替代业务服务或整体验收。
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {create} = require('./session-persistence.js');
const source = fs.readFileSync(require.resolve('./app.js'), 'utf8');
const names = ['readSession', 'writeSession', 'setStatus', 'request', 'refreshSession', 'performRefreshSession', 'login', 'getDeviceName', 'logout', 'inspectBridge'];
function actualFunction(name) {
  const start = source.search(new RegExp(`^  (?:async )?function ${name}\\(`, 'm'));
  assert(start >= 0, '未找到实际会话函数');
  const rest = source.slice(start), end = rest.indexOf('\n  }');
  assert(end >= 0, '实际会话函数边界变化，请更新测试提取');
  return rest.slice(0, end + 4);
}
const deferred = () => { let resolve; const promise = new Promise((done) => { resolve = done; }); return {resolve, promise}; };
const reply = (payload, status = 200) => ({ok: status >= 200 && status < 300, status,
  headers: new Headers({'Content-Type': 'application/json'}), json: async () => payload});
function harness({protectedMode = false, denySessionStorage = false, fetch = async () => reply({}), deviceId = () => 'test-device'} = {}) {
  const markers = new Map(), legacy = new Map(), native = new Map(), notices = [], events = [];
  if (protectedMode) markers.set('fingerprint-saas.native-session-protected.v1', '1');
  const storage = {getItem: (key) => markers.get(key) ?? null, setItem: (key, value) => markers.set(key, value), removeItem: (key) => markers.delete(key)};
  const bridge = {describe: async () => ({available: true, capabilities: {secureStorage: true}}), secureStorage: {
    async get({key}) { return {value: native.get(key) ?? null}; },
    async set({key, value}) { native.set(key, value); return {ok: true}; },
    async remove({key}) { native.delete(key); return {ok: true}; },
  }};
  const persistence = create({bridge, serviceUrl: 'https://saas.example.test/api/v1/sessions/refresh', deviceId, storage});
  const context = vm.createContext({state: {session: null, sessionGeneration: 0, workspaces: [], accounts: [], workspaceId: ''},
    sessionPersistence: persistence, sessionStorageKey: 'fingerprint-saas.session.v1', apiBase: 'https://saas.example.test',
    sessionStorage: {getItem: (key) => legacy.get(key) ?? null, setItem: (key, value) => legacy.set(key, value),
      removeItem: (key) => { if (denySessionStorage) throw new Error('仅测试存储拒绝'); legacy.delete(key); }},
    elements: {loginForm: {fields: {}, reset() { events.push('reset'); this.fields = {}; }},
      loginStatus: {classList: {toggle() {}}}}, getDeviceId: deviceId,
    render: () => events.push('render'), renderBridge() {}, loadWorkspaces: async () => events.push('workspaces'),
    showToast: (value) => notices.push(value), userMessage: (error) => error.message, fetch, Headers,
    global: {navigator: {platform: '测试设备'}, saasBridgeClient: bridge, saasConsoleOperations: {clear() {}}, saasConsoleAdministration: {clear() {}}}});
  const errorClass = source.slice(source.indexOf('  class ApiError'), source.indexOf('\n  const elements ='));
  vm.runInContext('let refreshFlight = null, sessionRecoveryAttempted = false;\n' + errorClass + names.map(actualFunction).join('\n'), context);
  const marker = "  elements.loginForm.addEventListener('submit', async (event) => {";
  const body = source.slice(source.indexOf(marker) + marker.length);
  const end = body.indexOf('\n  });'); assert(end >= 0);
  context.FormData = class {
    constructor(form) { this.data = new Map(Object.entries(form.fields)); }
    get(key) { return this.data.get(key) ?? null; }
  };
  vm.runInContext('global.submitLogin = async (event) => {' + body.slice(0, end) + '\n};', context);
  return {context, persistence, markers, legacy, native, notices, events, bridge};
}

test('保护模式启动忽略网页明文旧会话，端口未连接时也不写回明文', async () => {
  const value = harness({protectedMode: true});
  value.legacy.set('fingerprint-saas.session.v1', JSON.stringify({accessToken: '仅测试旧访问令牌'}));
  assert.equal(value.context.readSession(), null);
  await value.context.writeSession({accessToken: '仅测试访问令牌', refreshToken: '仅测试刷新令牌', user: {user_id: 'one'}});
  assert.equal(value.legacy.size, 0); assert.match(value.notices.at(-1), /尚未连接/);
});

test('网页缓存清理被拒绝，仍执行真实安全存储接口的保存和退出清理', async () => {
  const value = harness({denySessionStorage: true}); value.persistence.configure({available: true, capabilities: {secureStorage: true}});
  await value.context.writeSession({accessToken: '仅测试访问令牌', refreshToken: '仅测试刷新令牌', user: {user_id: 'one'}, deviceId: 'authenticated-device'});
  const saved = JSON.parse([...value.native.values()][0]);
  assert.equal(saved.deviceId, 'authenticated-device'); assert.equal(saved.accessToken, undefined);
  await value.context.writeSession(null); assert.equal(value.native.size, 0); assert.equal(value.context.state.session, null);
  assert(value.notices.every((notice) => /[\u4e00-\u9fff]/.test(notice)));
});

test('两个不同用户的刷新互不复用旧请求，旧回复不能覆盖新会话', async () => {
  const old = deferred(), current = deferred(), calls = [];
  const value = harness({fetch: async (_url, options) => { calls.push(JSON.parse(options.body)); return calls.length === 1 ? old.promise : current.promise; }});
  const one = {refreshToken: '仅测试旧令牌', user: {user_id: 'one'}, deviceId: 'one-device'};
  await value.context.writeSession(one); const previous = value.context.refreshSession();
  await value.context.writeSession({refreshToken: '仅测试新令牌', user: {user_id: 'two'}, deviceId: 'two-device'});
  const latest = value.context.refreshSession(); assert.equal(calls.length, 2);
  old.resolve(reply({access_token: '仅测试过期回复', user: {user_id: 'one'}})); assert.equal(await previous, false);
  const shared = value.context.refreshSession(); assert.equal(calls.length, 2);
  current.resolve(reply({access_token: '仅测试新访问令牌', refresh_token: '仅测试轮换令牌', user: {user_id: 'two'}}));
  assert.equal(await latest, true); assert.equal(await shared, true); assert.equal(value.context.state.session.user.user_id, 'two');
  assert.equal(value.context.state.session.refreshToken, '仅测试轮换令牌');
  assert.equal(calls[1].device_id, 'two-device');
});

test('恢复读取期间退出，旧凭据不触发刷新或改变会话', async () => {
  const read = deferred(), started = deferred(); let requests = 0;
  const value = harness({fetch: async () => { requests++; return reply({}); }});
  value.bridge.secureStorage.get = async () => { started.resolve(); return read.promise; };
  const restored = value.context.inspectBridge(); await started.promise;
  const cleared = value.context.writeSession(null);
  read.resolve({value: JSON.stringify({schemaVersion: 1, serviceUrl: 'https://saas.example.test/api/v1/sessions/refresh', refreshToken: '仅测试旧令牌', deviceId: 'old-device'})});
  await restored; await cleared; assert.equal(requests, 0); assert.equal(value.context.state.session, null);
});

test('登录与刷新使用认证时捕获的设备标识，不随本地标识变化', async () => {
  const calls = []; let count = 0;
  const value = harness({deviceId: () => `device-${++count}`, fetch: async (_url, options) => {
    calls.push(JSON.parse(options.body)); return reply({access_token: '仅测试访问令牌', refresh_token: '仅测试刷新令牌', user: {user_id: 'one'}});
  }});
  await value.context.login('unit@example.test', '仅测试密码'); await value.context.refreshSession();
  assert.equal(calls[0].device_id, 'device-1'); assert.equal(calls[1].device_id, 'device-1');
  assert.equal(value.context.state.session.deviceId, 'device-1');
});

test('断网保留刷新凭据，401 则清除本地与原生会话', async () => {
  let unauthorized = false;
  const value = harness({fetch: async () => { if (!unauthorized) throw new Error('仅测试断网'); return reply({}, 401); }});
  value.persistence.configure({available: true, capabilities: {secureStorage: true}});
  await value.context.writeSession({refreshToken: '仅测试令牌', user: {user_id: 'one'}, deviceId: 'test-device'});
  const original = value.context.state.session;
  assert.equal(await value.context.refreshSession(), false); assert.equal(value.context.state.session, original); assert.equal(value.native.size, 1);
  unauthorized = true; assert.equal(await value.context.refreshSession(), false);
  await value.persistence.restore(); assert.equal(value.context.state.session, null); assert.equal(value.native.size, 0);
});

test('实际登录提交先捕获表单，再清理候选会话，不能把用户名密码清空后发送', async () => {
  let submitted;
  const value = harness({fetch: async (_url, options) => {
    submitted = JSON.parse(options.body); return reply({access_token: '仅测试访问令牌', refresh_token: '仅测试刷新令牌', user: {user_id: 'one'}});
  }});
  value.context.elements.loginForm.fields = {email: ' unit@example.test ', password: '仅测试密码'};
  await value.context.global.submitLogin({preventDefault() {}});
  assert.equal(submitted.email, 'unit@example.test'); assert.equal(submitted.password, '仅测试密码');
  assert.equal(value.context.state.session.user.user_id, 'one');
});
