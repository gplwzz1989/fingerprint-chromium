// 仅测试 iframe 消息安全边界；不提供生产原生能力或模拟账号数据。
const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');

test('桌面 iframe 只向原生控制台发送，并拒绝其他父来源的回复', async () => {
  const listeners = new Map();
  let sent;
  const parent = {postMessage: (message, origin) => { sent = {message, origin}; }};
  const window = {parent, location: {origin: 'https://saas.example.test'},
    setTimeout, clearTimeout, addEventListener: (name, handler) => listeners.set(name, handler)};
  vm.runInContext(fs.readFileSync(require.resolve('./bridge-contract.js'), 'utf8'), vm.createContext({window, URL, Event}));
  let resolved = false;
  const result = window.saasBridgeClient.tabs.list().then((value) => { resolved = true; return value; });
  assert.equal(sent.origin, 'chrome://fingerprint-manager');
  assert.equal(sent.message.method, 'tabs.list');
  const response = {type: 'fingerprint-saas-bridge:response', requestId: sent.message.requestId, ok: true, result: []};
  listeners.get('message')({source: parent, origin: 'https://attacker.example.test', data: response});
  await new Promise(setImmediate);
  assert.equal(resolved, false);
  listeners.get('message')({source: {}, origin: 'chrome://fingerprint-manager', data: response});
  await new Promise(setImmediate);
  assert.equal(resolved, false);
  listeners.get('message')({source: parent, origin: 'chrome://fingerprint-manager', data: response});
  assert.deepEqual(JSON.parse(JSON.stringify(await result)), []);
});

test('宿主发送失败不会遗留待处理请求或计时器', async () => {
  let active = 0;
  const window = {parent: {postMessage: () => { throw new Error('仅用于测试的无效宿主'); }},
    location: {origin: 'https://saas.example.test'}, addEventListener() {},
    setTimeout: (handler, delay) => { active++; return setTimeout(handler, delay); },
    clearTimeout: (timer) => { active--; clearTimeout(timer); }};
  vm.runInContext(fs.readFileSync(require.resolve('./bridge-contract.js'), 'utf8'), vm.createContext({window, URL, Event}));
  await assert.rejects(window.saasBridgeClient.tabs.list(), /没有可用的原生桥/);
  assert.equal(active, 0);
});
