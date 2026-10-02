'use strict';

const {spawnSync} = require('node:child_process');
const {webcrypto, pbkdf2Sync, randomBytes, createCipheriv} = require('node:crypto');
const path = require('node:path');

// 使用真实网页模块和 Node WebCrypto；测试数据只通过内存及子进程管道传递。
Object.defineProperty(globalThis, 'crypto', {value: webcrypto, configurable: true});
const sync = require('../../saas-web/snapshot-sync.js');
const [java, classes] = process.argv.slice(2);
if (!java || !classes || !path.win32.isAbsolute(classes) || !/^d:/i.test(classes)) {
  throw new Error('请指定 Java 路径和 D 盘编译输出目录');
}
let passed = 0;
const check = (condition, label) => {
  if (!condition) throw new Error(label + '未通过');
  passed++;
};

function jvm(request, rejected = false) {
  const result = spawnSync(java, ['-Djava.io.tmpdir=' + classes, '-Dfile.encoding=UTF-8', '-cp', classes,
    'com.fingerprint.saas.bridge.SnapshotCryptoSelfTest', '--stdio'], {
    input: JSON.stringify(request) + '\n', encoding: 'utf8', maxBuffer: 24 * 1024 * 1024,
    timeout: 120000, windowsHide: true,
  });
  if (rejected) {
    check(result.status === 2 && /[\u4e00-\u9fff]/u.test(result.stderr) && result.stdout === '', 'JVM 安全拒绝');
    check(!result.stderr.includes(request.password), '错误不泄露密码');
    return;
  }
  if (result.status !== 0 || result.error) {
    const safeMessage = result.stderr?.match(/(?:快照|账号|加密)[^\r\n]*/u)?.[0] || '子进程未正常完成';
    throw new Error('JVM ' + (request.action === 'encrypt' ? '加密' : '解密') + '互操作调用失败：' + safeMessage);
  }
  return JSON.parse(result.stdout);
}

function fixture(accountId) {
  return {schema_version: 1, account_id: accountId, cookies: [
    {name: '会话😀', value: '真实算法测试内容', domain: 'example.com', path: '/'},
  ], local_storage: {'中文😀': '汉字-é-e\u0301-😀-\u0000-\ud800'}, session_storage: {会话: '内容'},
  storage_url: 'https://example.com/测试?q=😀', fingerprint: {user_agent: '测试浏览器', hardware_concurrency: 8},
  extra: {escape: '\n\t"\\', bool: true, nil: null, list: [1, -1.5, 1e-8]}};
}

function same(left, right) {
  // 按对象结构比较，避免 Java 和 JS 的属性排序规则影响验证。
  const {isDeepStrictEqual} = require('node:util');
  return isDeepStrictEqual(left, right);
}

function seal(accountId, password, plaintext, iterations = 600000, saltLength = 16) {
  const salt = randomBytes(saltLength), nonce = randomBytes(12);
  const key = pbkdf2Sync(Buffer.from(password, 'utf8'), salt, iterations, 32, 'sha256');
  try {
    const cipher = createCipheriv('aes-256-gcm', key, nonce, {authTagLength: 16});
    cipher.setAAD(Buffer.from('fingerprint-manager:v1:' + accountId, 'utf8'));
    const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
    return {algorithm: 'AES-256-GCM', kdf: 'PBKDF2-HMAC-SHA-256', iterations,
      salt: salt.toString('base64'), nonce: nonce.toString('base64'),
      ciphertext: ciphertext.toString('base64'), tag: cipher.getAuthTag().toString('base64')};
  } finally { key.fill(0); }
}

(async () => {
  for (const [accountId, password] of [
    ['普通账号', 'test-password-123456'],
    ['账号-中文-😀-e\u0301', '中文密码😀-e\u0301-\u0000-安全足够长'],
    ['账号-未配对-\ud800', '未配对-\ud800-\udfff-密码123456'],
  ]) {
    const snapshot = fixture(accountId), key = await sync.passwordKey(password);
    const native = jvm({action: 'encrypt', accountId, password, snapshot});
    check(same(await sync.decrypt(key, accountId, native), snapshot), 'JVM 加密到网页解密');
    const web = await sync.encrypt(key, accountId, snapshot);
    check(same(jvm({action: 'decrypt', accountId, password, envelope: web}), snapshot), '网页加密到 JVM 解密');
    jvm({action: 'decrypt', accountId: accountId + '其他', password, envelope: web}, true);
    jvm({action: 'decrypt', accountId, password: password + '错误', envelope: web}, true);
    let rejected = false;
    try { await sync.decrypt(key, accountId + '其他', native); } catch (_) { rejected = true; }
    check(rejected, '网页拒绝跨账号 JVM 信封');
  }
  const accountId = '上限测试账号', password = '边界测试密码-123456', snapshot = fixture(accountId);
  const key = await sync.passwordKey(password);
  const maximum = jvm({action: 'encrypt', accountId, password, snapshot, iterations: 2000000});
  check(same(await sync.decrypt(key, accountId, maximum), snapshot), '2000000 次派生兼容');
  const salt64 = seal(accountId, password, Buffer.from(JSON.stringify(snapshot)), 2000000, 64);
  check(same(jvm({action: 'decrypt', accountId, password, envelope: salt64}), snapshot), '64 字节盐及派生上限');
  const whitespace = {...salt64, salt: salt64.salt.replace(/=+$/, '') + '\n', tag: salt64.tag.replace(/=+$/, '')};
  check(same(jvm({action: 'decrypt', accountId, password, envelope: whitespace}), snapshot), 'atob 格式兼容');
  for (const plaintext of [
    Buffer.from('{"account_id":"上限测试账号","schema_version":1,"cookies":[],"local_storage":{},"storage_url":"https://example.com/","extra":01}'),
    Buffer.from(JSON.stringify({...snapshot, account_id: '不同账号'})),
    Buffer.from(JSON.stringify({...snapshot, cookies: [null]})),
    Buffer.from([0xc0, 0xaf, ...Buffer.from(' '.repeat(20))]),
  ]) {
    jvm({action: 'decrypt', accountId, password, envelope: seal(accountId, password, plaintext)}, true);
  }
  const collisionAccount = '账号-\ud800', collisionSnapshot = fixture(collisionAccount);
  const collisionEnvelope = jvm({action: 'encrypt', accountId: collisionAccount, password, snapshot: collisionSnapshot});
  jvm({action: 'decrypt', accountId: '账号-\ufffd', password, envelope: collisionEnvelope}, true);
  const large = {...snapshot, extra: ''};
  const overhead = Buffer.byteLength(JSON.stringify(large));
  large.extra = 'a'.repeat(14 * 1024 * 1024 - overhead);
  const boundary = jvm({action: 'encrypt', accountId, password, snapshot: large});
  check(same(await sync.decrypt(key, accountId, boundary), large), '14 MiB 加密明文边界');
  const webBoundary = await sync.encrypt(key, accountId, large);
  check(same(jvm({action: 'decrypt', accountId, password, envelope: webBoundary}), large), '14 MiB 网页信封边界');
  large.extra = 'a'.repeat(16 * 1024 * 1024 - overhead);
  const maximumCiphertext = seal(accountId, password, Buffer.from(JSON.stringify(large)));
  check(same(jvm({action: 'decrypt', accountId, password, envelope: maximumCiphertext}), large), '16 MiB 解密密文边界');
  jvm({action: 'decrypt', accountId, password,
    envelope: {...maximumCiphertext, ciphertext: Buffer.alloc(16 * 1024 * 1024 + 1).toString('base64')}}, true);
  console.log('Node／JVM 交叉兼容通过：' + passed + ' 项');
})().catch((error) => {
  console.error('快照交叉兼容验证失败：' + error.message);
  process.exitCode = 1;
});
