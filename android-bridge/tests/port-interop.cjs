// 仅测试消息端口边界：端口、JVM 路由和加密均为真实实现；初始化事件在隔离测试上下文注入。
const assert = require('node:assert/strict');
const {spawn} = require('node:child_process');
const {MessageChannel} = require('node:worker_threads');
const readline = require('node:readline');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const sync = require('../../saas-web/snapshot-sync.js');

(async () => {
  const [java, classes] = process.argv.slice(2);
  assert(java && classes, '请指定已有 JDK 与测试类目录');
  const child = spawn(java, [`-Djava.io.tmpdir=${classes}`, '-Dfile.encoding=UTF-8', '-cp', classes,
    'com.fingerprint.saas.bridge.SaasBridgeDispatcherSelfTest', '--wire'], {stdio: ['pipe', 'pipe', 'pipe']});
  const callbacks = new Map(), queue = [];
  let childError = '', tests = 0;
  child.stderr.setEncoding('utf8'); child.stderr.on('data', (value) => { childError += value.slice(0, 2048); });
  readline.createInterface({input: child.stdout}).on('line', (value) => queue.shift()?.resolve(value));
  child.on('error', (error) => { for (const value of queue.splice(0)) value.reject(error); });
  child.on('exit', () => { for (const value of queue.splice(0)) value.reject(new Error('JVM 消息测试进程已结束')); });
  const send = (message) => new Promise((resolve, reject) => {
    queue.push({resolve, reject}); child.stdin.write(message + '\n');
  });
  const global = {location: {origin: 'https://saas.example.test'}, setTimeout, clearTimeout,
    addEventListener: (name, action) => { callbacks.set(name, action); }, dispatchEvent: () => true};
  global.parent = global;
  const context = vm.createContext({window: global, URL, Event, console});
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../../saas-web/bridge-contract.js'), 'utf8'), context);
  const channel = new MessageChannel();
  let forward = true;
  channel.port2.on('message', async (message) => {
    if (!forward) return;
    try { channel.port2.postMessage(await send(message)); }
    catch (_) { channel.port2.postMessage(JSON.stringify({type: 'fingerprint-saas-bridge:revoked'})); }
  });
  const init = {isTrusted: true, source: null, origin: 'https://fingerprint-native.invalid',
    data: JSON.stringify({type: 'fingerprint-saas-bridge:init', version: '1.0', origin: global.location.origin + ':443'}), ports: [channel.port1]};
  const check = (condition, message) => { assert(condition, message); tests++; };
  const deadline = setTimeout(() => {
    channel.port1.close(); channel.port2.close(); child.kill();
    process.exitCode = 1; console.error('端口互操作测试超时');
  }, 45000);
  try {
    for (const change of [{isTrusted: false}, {source: global}, {origin: 'https://other.test'},
      {data: JSON.stringify({...JSON.parse(init.data), origin: 'https://other.test'})},
      {data: JSON.stringify({...JSON.parse(init.data), version: '2.0'})}, {data: JSON.parse(init.data)}]) {
      callbacks.get('message')({...init, ...change}); check(!global.saasBridge, '不可信初始化事件被接受');
    }
    callbacks.get('message')(init);
    check(global.saasBridgeClient.isAvailable(), '原生端口未接入客户端');
    const details = await global.saasBridgeClient.describe();
    check(details.available && details.capabilities.crypto === true && details.capabilities.tabs === false && details.capabilities.http === false, '能力声明与实际路由不一致');
    const snapshot = {schema_version: 1, account_id: 'wire-account', cookies: [],
      local_storage: {标签: '网页与 JVM 消息'}, session_storage: {}, storage_url: 'https://example.test/'};
    const password = 'message-wire-test-123';
    const envelope = await global.saasBridgeClient.crypto.encryptSnapshot({accountId: 'wire-account', password, snapshot});
    check(envelope.algorithm === 'AES-256-GCM', '消息未返回真实信封');
    assert.deepEqual(await sync.decrypt(await sync.passwordKey(password), 'wire-account', envelope), snapshot); tests++;
    const webEnvelope = await sync.encrypt(await sync.passwordKey(password), 'wire-account', snapshot);
    const result = await global.saasBridgeClient.crypto.decryptSnapshot({accountId: 'wire-account', password, envelope: webEnvelope});
    assert.deepEqual(JSON.parse(JSON.stringify(result)), snapshot); tests++;
    await assert.rejects(global.saasBridgeClient.crypto.decryptSnapshot({accountId: 'wire-account', password: 'wrong-password-123', envelope}), /解密失败/); tests++;
    await assert.rejects(global.saasBridgeClient.tabs.create({accountId: 'wire-account'}), /尚未提供/); tests++;
    forward = false;
    const pending = global.saasBridgeClient.crypto.encryptSnapshot({accountId: 'wire-account', password, snapshot});
    channel.port2.postMessage(JSON.stringify({type: 'fingerprint-saas-bridge:revoked'}));
    await assert.rejects(pending, /授权已失效/); tests++;
    check(!global.saasBridgeClient.isAvailable() && !global.saasBridge, '撤销后保留了旧原生桥');
    check(!childError, 'JVM 输出了异常');
    console.log(`真实端口／网页／JVM 互操作通过：${tests} 项`);
  } finally {
    clearTimeout(deadline); callbacks.get('pagehide')(); channel.port1.close(); channel.port2.close();
    child.stdin.end();
  }
})().catch(() => { console.error('端口互操作验证失败，请检查测试环境和消息协议'); process.exitCode = 1; });
