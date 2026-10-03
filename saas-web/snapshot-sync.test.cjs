const {test} = require('node:test');
const assert = require('node:assert/strict');
const sync = require('./snapshot-sync.js');
const vm = require('node:vm');
const fs = require('node:fs');

const fixture = () => ({schema_version: 1, account_id: 'test-account', cookies: [],
  local_storage: {label: '中文测试'}, session_storage: {session: '测试'},
  storage_url: 'https://example.test/account', fingerprint_seed: '12345', proxy_rules: '',
  fingerprint: {user_agent: '', hardware_concurrency: 4}, sync_options: {...sync.defaults}});

test('真实加密往返、随机盐与账号绑定', async () => {
  const key = await sync.passwordKey('test-encryption-passphrase');
  const first = await sync.encrypt(key, 'test-account', fixture());
  const second = await sync.encrypt(key, 'test-account', fixture());
  assert.notEqual(first.salt, second.salt);
  assert.notEqual(first.nonce, second.nonce);
  assert.deepEqual(await sync.decrypt(key, 'test-account', first), fixture());
  await assert.rejects(sync.decrypt(key, 'other-account', first), /解密失败/);
  const wrongKey = await sync.passwordKey('another-encryption-passphrase');
  await assert.rejects(sync.decrypt(wrongKey, 'test-account', first), /解密失败/);
  await assert.rejects(sync.decrypt(key, 'test-account', {...first, tag: Buffer.alloc(16).toString('base64')}), /解密失败/);
});

test('网页存储强制携带来源，未选项不携带数据', () => {
  const options = {...sync.defaults, cookies: false, fingerprint: false, proxy: false, page: false};
  const filtered = sync.select(fixture(), 'test-account', options);
  assert.equal(filtered.sync_options.page, true);
  assert.deepEqual(filtered.cookies, []);
  assert.equal(filtered.fingerprint, undefined);
  assert.equal(filtered.proxy_rules, undefined);
  assert.equal(filtered.storage_url, fixture().storage_url);
  assert.throws(() => sync.select({...fixture(), storage_url: 'about:blank'}, 'test-account'), /来源页面/);
});

test('代理地址、认证引用和凭证密文分离，拒绝明文代理凭据', () => {
  const value = sync.select({cookies: [], local_storage: {}, session_storage: {}, storage_url: 'https://example.test/account',
    proxy_rules: 'proxy.example.test:8080', proxy_auth_ref: 'device.proxy', sync_options: {...sync.defaults}}, 'test-account');
  assert.deepEqual(value.proxy_config, {address: 'proxy.example.test:8080', auth_ref: 'device.proxy'});
  assert.equal(value.proxy_rules, 'proxy.example.test:8080');
  assert.throws(() => sync.validate({...fixture(), proxy_rules: 'user:password@proxy.example.test:8080'}, 'test-account'), /明文账号或密码/);
  assert.throws(() => sync.validate({...fixture(), proxy_config: {address: 'proxy:8080', credential_ciphertext: {algorithm: 'AES-GCM', ciphertext: 'x', password: '明文'}}}, 'test-account'), /凭证密文无效/);
});

test('拒绝异常加密参数、跨账号内容和不支持的存储结构', async () => {
  const key = await sync.passwordKey('test-encryption-passphrase');
  const envelope = await sync.encrypt(key, 'test-account', fixture());
  await assert.rejects(sync.decrypt(key, 'test-account', {...envelope, iterations: 2000001}), /加密参数/);
  await assert.rejects(sync.encrypt(key, 'other-account', fixture()), /标识/);
  assert.throws(() => sync.validate({...fixture(), local_storage: {invalid: 3}}, 'test-account'), /网页存储/);
  assert.throws(() => sync.validate({...fixture(), sync_options: {cookies: true}}, 'test-account'), /同步选项/);
});

test('兼容旧 WebUI 使用的 PBKDF2 与 AES-GCM 信封', async () => {
  const password = 'compatible-password-123';
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const nonce = crypto.getRandomValues(new Uint8Array(12));
  const baseKey = await crypto.subtle.importKey('raw', new TextEncoder().encode(password), 'PBKDF2', false, ['deriveKey']);
  const key = await crypto.subtle.deriveKey({name: 'PBKDF2', hash: 'SHA-256', iterations: 600000, salt}, baseKey,
    {name: 'AES-GCM', length: 256}, false, ['encrypt']);
  const encrypted = Buffer.from(await crypto.subtle.encrypt({name: 'AES-GCM', iv: nonce,
    additionalData: new TextEncoder().encode('fingerprint-manager:v1:test-account')}, key,
  new TextEncoder().encode(JSON.stringify(fixture()))));
  const envelope = {algorithm: 'AES-256-GCM', kdf: 'PBKDF2-HMAC-SHA-256', iterations: 600000,
    salt: Buffer.from(salt).toString('base64'), nonce: Buffer.from(nonce).toString('base64'),
    ciphertext: encrypted.subarray(0, -16).toString('base64'), tag: encrypted.subarray(-16).toString('base64')};
  assert.deepEqual(await sync.decrypt(await sync.passwordKey(password), 'test-account', envelope), fixture());
});

test('HTTP 无 WebCrypto 环境通过原生契约加密，密钥对象不暴露密码', async () => {
  // 仅测试环境将原生边界交给真实 Node WebCrypto，验证调用参数和兼容信封。
  const nativeBridge = {
    describe: async () => ({available: true, capabilities: {crypto: true}}),
    crypto: {
      encryptSnapshot: async ({accountId, password, snapshot}) => sync.encrypt(await sync.passwordKey(password), accountId, snapshot),
      decryptSnapshot: async ({accountId, password, envelope}) => sync.decrypt(await sync.passwordKey(password), accountId, envelope),
    },
  };
  const context = vm.createContext({window: {crypto: {}, saasBridgeClient: nativeBridge}, URL, TextEncoder, TextDecoder});
  vm.runInContext(fs.readFileSync(require.resolve('./snapshot-sync.js'), 'utf8'), context);
  const client = context.window.saasSnapshotSync;
  const key = await client.passwordKey('native-password-test-123');
  assert.equal(key.password, undefined);
  const envelope = await client.encrypt(key, 'test-account', fixture());
  assert.deepEqual(await client.decrypt(key, 'test-account', envelope), fixture());
  nativeBridge.describe = async () => ({available: false, capabilities: {}});
  await assert.rejects(client.passwordKey('native-password-test-123'), /加密能力/);
});

test('合并区分 Cookie 分区和域，保留两端键并拒绝混合来源', () => {
  const local = fixture(), remote = fixture();
  const cookie = {name: 'token', value: '云端测试', domain: '.example.test', path: '/'};
  remote.cookies = [cookie, {...cookie, domain: 'other.example.test', value: '其他域'}];
  local.cookies = [{...cookie, value: '本地测试'}, {...cookie, partition_key: {top_level_site: 'https://example.test', has_cross_site_ancestor: true}}];
  remote.local_storage = {remote: '云端键', shared: '云端值'};
  local.local_storage = {local: '本地键', shared: '本地值'};
  const result = sync.mergeSnapshots(local, remote, 'test-account');
  assert.equal(result.cookies.length, 3);
  assert.equal(result.cookies.find((value) => value.domain === '.example.test' && !value.partition_key).value, '本地测试');
  assert.deepEqual(result.local_storage, {remote: '云端键', shared: '本地值', local: '本地键'});
  assert.throws(() => sync.mergeSnapshots(local, {...remote, storage_url: 'https://other.test/'}, 'test-account'), /不能合并/);
});

test('冲突覆盖必须使用刚读取的版本，云端再次更新时继续拒绝覆盖', async () => {
  // 测试专用接口边界模拟版本竞争；快照加解密仍使用真实 WebCrypto。
  const key = await sync.passwordKey('conflict-password-test');
  const cloud = fixture(); cloud.local_storage.cloud = '云端键';
  let revision = 1, envelope = await sync.encrypt(key, 'test-account', cloud), race = false;
  const puts = [], deletes = [];
  const controller = sync.createController({deviceId: () => 'test-device', getKey: () => key,
    getSession: () => 'test-user', getOptions: () => sync.defaults,
    bridge: {tabs: {list: async () => [{id: 'test-tab', account_id: 'test-account'}]},
      storage: {getSnapshot: async () => fixture()}},
    request: async (path, options = {}) => {
      if (options.method === 'POST') return {lease_id: 'test-lease'};
      if (options.method === 'DELETE') { deletes.push(path); return null; }
      if (!options.method) return {account_id: 'test-account', schema_version: 1, revision, envelope};
      puts.push(options);
      if (race) { revision++; race = false; }
      if (String(revision) !== options.headers['If-Match']) {
        const error = new Error('测试版本冲突'); error.code = 'snapshot_revision_conflict'; error.currentRevision = revision; throw error;
      }
      envelope = options.body.envelope; revision++; return {revision};
    }});
  const account = {account_id: 'test-account', revision: 0};
  await assert.rejects(controller.upload(account), /版本冲突/);
  assert.equal(controller.getConflict('test-account').revision, 1);
  race = true;
  await assert.rejects(controller.resolveConflict(account, 'overwrite'), /版本冲突/);
  assert.equal(puts.at(-1).headers['If-Match'], '1');
  assert.equal(puts.at(-1).body.overwrite, true);
  assert.equal(controller.getConflict('test-account').revision, 2);
  assert.equal((await controller.resolveConflict(account, 'merge')).revision, 3);
  assert.equal(puts.at(-1).headers['If-Match'], '2');
  assert.equal(puts.at(-1).body.overwrite, undefined);
  assert.equal((await sync.decrypt(key, 'test-account', envelope)).local_storage.cloud, '云端键');
  assert.equal(deletes.length, 3);
  assert.equal(controller.getConflict('test-account'), null);
});

test('获取租约期间锁定快照后停止写入，仍释放已取得的租约', async () => {
  // 测试专用接口边界注入锁定时机，不替代生产业务；加解密使用真实 WebCrypto。
  let key = await sync.passwordKey('lock-during-lease-test');
  const methods = [];
  const controller = sync.createController({deviceId: () => 'test-device', getKey: () => key,
    getSession: () => 'test-session', getOptions: () => sync.defaults,
    bridge: {tabs: {list: async () => [{id: 'test-tab', account_id: 'test-account'}]},
      storage: {getSnapshot: async () => fixture()}},
    request: async (_path, options) => {
      methods.push(options.method);
      if (options.method === 'POST') { key = null; return {lease_id: 'test-lease'}; }
      return null;
    }});
  await assert.rejects(controller.upload({account_id: 'test-account', revision: 0}), /操作已停止/);
  assert.deepEqual(methods, ['POST', 'DELETE']);
});

test('恢复读取 Tab 期间会话变化后不能写入本地或创建新 Tab', async () => {
  const key = await sync.passwordKey('restore-session-switch-test');
  const envelope = await sync.encrypt(key, 'test-account', fixture());
  let session = 'original-session', writes = 0;
  const controller = sync.createController({deviceId: () => 'test-device', getKey: () => key,
    getSession: () => session, getOptions: () => sync.defaults,
    request: async () => ({account_id: 'test-account', schema_version: 1, revision: 1, envelope}),
    bridge: {tabs: {
      list: async () => { session = 'new-session'; return []; },
      create: async () => { writes++; },
    }, storage: {writeSnapshot: async () => { writes++; }}}});
  await assert.rejects(controller.restore({account_id: 'test-account'}), /操作已停止/);
  assert.equal(writes, 0);
});

test('原生读取使用本次选项，读取期间修改选项不会扩大云端同步范围', async () => {
  // 仅注入接口边界时机；使用真实快照加密，不替代生产存储或服务端。
  const key = await sync.passwordKey('selection-during-read-test');
  const options = {...sync.defaults, local_storage: false, session_storage: false,
    fingerprint: false, proxy: false, page: false};
  let requested, uploaded;
  const controller = sync.createController({deviceId: () => 'test-device', getKey: () => key,
    getSession: () => 'test-session', getOptions: () => options,
    bridge: {tabs: {list: async () => [{id: 'test-tab', account_id: 'test-account'}]},
      storage: {getSnapshot: async (_id, selection) => {
        requested = {...selection};
        options.local_storage = true;
        return fixture();
      }}},
    request: async (_path, request = {}) => {
      if (request.method === 'POST') return {lease_id: 'test-lease'};
      if (request.method === 'PUT') { uploaded = request.body.envelope; return {revision: 1}; }
      return null;
    }});
  await controller.upload({account_id: 'test-account', revision: 0});
  const saved = await sync.decrypt(key, 'test-account', uploaded);
  assert.equal(requested.local_storage, false);
  assert.equal(saved.sync_options.local_storage, false);
  assert.deepEqual(saved.local_storage, {});
  assert.equal(saved.storage_url, undefined);
});

test('恢复先创建空白环境并应用指纹，再发起网站导航', async () => {
  // 仅核对调用顺序；快照加解密真实执行，不模拟浏览器指纹结果。
  const key = await sync.passwordKey('restore-before-navigation-test');
  const data = fixture();
  const envelope = await sync.encrypt(key, 'test-account', data);
  const events = [];
  let tab;
  const controller = sync.createController({deviceId: () => 'test-device', getKey: () => key,
    getSession: () => 'test-session', getOptions: () => sync.defaults,
    request: async () => ({account_id: 'test-account', schema_version: 1, revision: 1, envelope}),
    bridge: {tabs: {
      list: async () => tab ? [tab] : [],
      create: async (options) => {
        events.push(['create', options.url]); assert.deepEqual(options.fingerprint, data.fingerprint);
        tab = {id: 'test-tab', account_id: 'test-account', url: options.url, load_progress: 1}; return tab;
      },
      navigate: async (_id, url) => { events.push(['navigate', url]); tab.url = url; return true; },
      activate: async () => { events.push(['activate']); },
    }, fingerprint: {set: async () => { events.push(['fingerprint']); }},
      storage: {writeSnapshot: async () => { events.push(['storage']); return true; }}}});
  await controller.restore({account_id: 'test-account'});
  assert.deepEqual(events, [['create', 'about:blank'], ['fingerprint'], ['navigate', data.storage_url], ['storage'], ['activate']]);
});
