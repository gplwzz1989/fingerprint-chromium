// 仅验证独立 Web 的会话生命周期边界；这里的桥接探针不替代 AndroidKeyStore。
const {test} = require('node:test');
const assert = require('node:assert/strict');
const {create} = require('./session-persistence.js');

const serviceUrl = 'https://saas.example.test/api/v1/sessions/refresh';
const details = {available: true, capabilities: {secureStorage: true}};
function scenario() {
  const entries = new Map(), markers = new Map(), calls = [];
  const storage = {getItem: (key) => markers.get(key) ?? null, setItem: (key, value) => markers.set(key, value), removeItem: (key) => markers.delete(key)};
  const bridge = {secureStorage: {
    async get({key}) { calls.push('get'); return {value: entries.get(key) ?? null}; },
    async set({key, value}) { calls.push('set'); entries.set(key, value); return {ok: true}; },
    async remove({key}) { calls.push('remove'); entries.delete(key); return {ok: true}; },
  }};
  const store = create({bridge, serviceUrl, deviceId: () => 'test-device', storage});
  return {entries, markers, calls, storage, bridge, store};
}
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return {resolve, reject, promise}; };

test('未绑定安全能力不伪造持久化，启用后只保存刷新凭据与服务范围', async () => {
  const value = scenario();
  assert.equal(await value.store.save({refreshToken: '仅测试的刷新令牌'}), false);
  assert.equal(await value.store.restore(), null);
  assert.equal(value.calls.length, 0);
  value.store.configure(details);
  assert.equal(await value.store.save({refreshToken: '仅测试的刷新令牌', accessToken: '不能持久保存', password: '不能保存', user: {user_id: 'test'}}), true);
  assert.deepEqual(JSON.parse([...value.entries.values()][0]), {schemaVersion: 1, serviceUrl, refreshToken: '仅测试的刷新令牌', deviceId: 'test-device'});
  assert.equal((await value.store.restore()).refreshToken, '仅测试的刷新令牌');
});

test('退出排在已开始的写入之后，旧写入不能恢复退出状态', async () => {
  const value = scenario(); value.store.configure(details);
  const started = deferred(), finish = deferred();
  value.bridge.secureStorage.set = async ({key, value: data}) => { started.resolve(); await finish.promise; value.entries.set(key, data); return {ok: true}; };
  const saved = value.store.save({refreshToken: '仅测试的旧令牌'});
  await started.promise;
  const cleared = value.store.clear(); finish.resolve();
  assert.equal(await saved, false); assert.equal(await cleared, true);
  assert.equal(value.entries.size, 0); assert.equal(await value.store.restore(), null);
  assert.equal(value.markers.get('fingerprint-saas.native-session-revoked.v1'), '1');
});

test('恢复期间主动登录或退出，使旧读取结果失效', async () => {
  const value = scenario(); value.store.configure(details);
  await value.store.save({refreshToken: '仅测试的旧令牌'});
  const started = deferred(), finish = deferred();
  const existing = [...value.entries.values()][0];
  value.bridge.secureStorage.get = async () => { started.resolve(); await finish.promise; return {value: existing}; };
  const restored = value.store.restore(); await started.promise;
  const saved = value.store.save({refreshToken: '仅测试的新令牌'}); finish.resolve();
  assert.equal(await restored, null); assert.equal(await saved, true);
  assert.equal(JSON.parse([...value.entries.values()][0]).refreshToken, '仅测试的新令牌');
});

test('原生写入失败不降级明文，旧会话恢复保持禁止', async () => {
  const value = scenario(); value.store.configure(details);
  await value.store.save({refreshToken: '仅测试的旧令牌'});
  value.bridge.secureStorage.set = async () => { throw new Error('安全存储已撤权'); };
  await assert.rejects(value.store.save({refreshToken: '仅测试的新令牌'}), /已撤权/);
  value.store.configure({available: false, capabilities: {}});
  assert.equal(value.store.isEnabled(), true); assert.equal(await value.store.restore(), null);
});

test('退出时原生清除失败，跨页面的退出标志仍阻止旧令牌恢复', async () => {
  const value = scenario(); value.store.configure(details); await value.store.save({refreshToken: '仅测试的旧令牌'});
  value.bridge.secureStorage.remove = async () => { throw new Error('原生桥未连接'); };
  await assert.rejects(value.store.clear(), /未连接/);
  const reloaded = create({bridge: value.bridge, serviceUrl, deviceId: () => 'test-device', storage: value.storage});
  reloaded.configure(details); assert.equal(await reloaded.restore(), null);
});

test('其他服务、非法 JSON 和混入访问令牌的记录不能恢复', async () => {
  const value = scenario(); value.store.configure(details); await value.store.save({refreshToken: '仅测试的令牌'});
  const key = [...value.entries.keys()][0], record = JSON.parse(value.entries.get(key));
  for (const data of [JSON.stringify({...record, serviceUrl: 'https://other.test/refresh'}), '{broken',
    JSON.stringify({...record, accessToken: '仅测试的额外字段'}), JSON.stringify({...record, deviceId: ''})]) {
    value.entries.set(key, data); await assert.rejects(value.store.restore(), /会话/);
  }
});

test('尚未连接原生桥时退出，也不能在随后连接时恢复旧令牌', async () => {
  const value = scenario(); await value.store.clear(); value.store.configure(details);
  assert.equal(await value.store.restore(), null); assert.equal(value.calls.length, 0);
});

test('后一次保存覆盖前一次排队操作，保留认证请求的真实设备标识', async () => {
  const value = scenario(); value.store.configure(details);
  const first = value.store.save({refreshToken: '仅测试的旧令牌'});
  const last = value.store.save({refreshToken: '仅测试的新令牌', deviceId: 'authenticated-device'});
  assert.equal(await first, false); assert.equal(await last, true);
  assert.equal((await value.store.restore()).deviceId, 'authenticated-device'); assert.equal(value.calls.filter((call) => call === 'set').length, 1);
});

test('空令牌、超限内容和虚假成功不解除恢复保护', async () => {
  const value = scenario(); value.store.configure(details);
  await assert.rejects(value.store.save({refreshToken: ''}), /会话/);
  await assert.rejects(value.store.save({refreshToken: '中'.repeat(8192)}), /限制/);
  value.bridge.secureStorage.set = async () => ({ok: false});
  await assert.rejects(value.store.save({refreshToken: '仅测试的令牌'}), /未完成/);
  assert.equal(await value.store.restore(), null);
});

test('已使用安全存储的页面重载后，端口未连接也不能降级保存明文', async () => {
  const value = scenario(); value.store.configure(details); await value.store.save({refreshToken: '仅测试的令牌'});
  const reloaded = create({bridge: value.bridge, serviceUrl, deviceId: () => 'test-device', storage: value.storage});
  assert.equal(reloaded.requiresSecureStorage(), true); assert.equal(reloaded.isEnabled(), false);
  await assert.rejects(reloaded.save({refreshToken: '仅测试的新令牌'}), /尚未连接/);
  reloaded.configure(details); assert.equal((await reloaded.restore()).refreshToken, '仅测试的令牌');
});
