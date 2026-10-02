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
