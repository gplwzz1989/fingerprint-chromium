'use strict';
// 仅使用有界存储探针测试实际 C++ 生成脚本的读写/错误行为。
// 不模拟原生 CookieManager，不据此宣称 Android 或真实浏览器生命周期验收通过。
const assert = require('node:assert/strict');
const vm = require('node:vm');
const {spawnSync} = require('node:child_process');
const path = require('node:path');
const result = spawnSync(process.argv[2], [], {
  encoding: 'utf8', maxBuffer: 1024 * 1024,
  cwd: process.argv[3], env: {...process.env, PATH: process.argv[3] + path.delimiter + process.env.PATH},
});
assert.equal(result.status, 0, '实际原生脚本生成程序未运行成功');
const scripts = JSON.parse(result.stdout);
let checks = 0;
function check(value) { checks++; assert.ok(value); }
class StorageProbe {
  constructor(values = {}) { this.values = new Map(Object.entries(values)); this.failures = 0; this.failAlways = false; }
  get length() { return this.values.size; }
  key(index) { return [...this.values.keys()][index] ?? null; }
  getItem(key) { return this.values.get(key) ?? null; }
  setItem(key, value) {
    if (this.failAlways || this.failures-- > 0) throw new Error('仅测试配额失败');
    this.values.set(String(key), String(value));
  }
  clear() { this.values.clear(); }
}
function context(local = {old: '原数据'}, session = {old: '原会话'}) {
  const state = vm.createContext({
    localStorage: new StorageProbe(local), sessionStorage: new StorageProbe(session),
    location: {href: 'https://storage.example.test/account'}, TextEncoder,
  });
  vm.runInContext(scripts.prepare, state);
  return state;
}
let state = context();
let read = vm.runInContext(scripts.read, state);
check(read.ok && read.local_storage.old === '原数据' && read.session_storage.old === '原会话');
let written = vm.runInContext(scripts.write, state);
check(written.ok);
check(state.localStorage.getItem('__proto__') === '真实键');
check(state.localStorage.getItem('constructor') === '保留键');
check(state.localStorage.getItem('测试') === '中文与换行\n内容');
check(state.localStorage.getItem('old') === null && state.sessionStorage.getItem('session') === '新会话');
read = vm.runInContext(scripts.read, state);
check(Object.prototype.hasOwnProperty.call(read.local_storage, '__proto__'));
state = context();
vm.runInContext(scripts.localOnly, state);
check(state.sessionStorage.getItem('old') === '原会话');
state = context();
vm.runInContext(scripts.neither, state);
check(state.localStorage.getItem('old') === '原数据' && state.sessionStorage.getItem('old') === '原会话');
state = context();
state.location.href = 'https://other.example.test/';
check(vm.runInContext(scripts.read, state).ok === false);
check(vm.runInContext(scripts.write, state).ok === false && state.localStorage.getItem('old') === '原数据');
state = context();
// 不能覆盖已建立的文档标记；新文档的独立全局环境没有旧标记。
check(vm.runInContext("Reflect.set(globalThis,'__fingerprint_saas_storage_document','其他标记')", state) === false);
const newDocument = vm.createContext({localStorage: state.localStorage, sessionStorage: state.sessionStorage,
  location: state.location, TextEncoder});
check(vm.runInContext(scripts.write, newDocument).ok === false);
state = context();
state.localStorage.failures = 1;
written = vm.runInContext(scripts.write, state);
check(written.ok === false && written.rollback_failed === false);
check(state.localStorage.getItem('old') === '原数据' && state.sessionStorage.getItem('old') === '原会话');
state = context();
state.sessionStorage.failures = 1;
written = vm.runInContext(scripts.write, state);
check(written.ok === false && written.rollback_failed === false);
check(state.localStorage.getItem('old') === '原数据' && state.sessionStorage.getItem('old') === '原会话');
state = context();
state.localStorage.failAlways = true;
written = vm.runInContext(scripts.write, state);
check(written.ok === false && written.rollback_failed === true);
check(state.sessionStorage.getItem('old') === '原会话');
state = context({huge: '中'.repeat(400000)});
check(vm.runInContext(scripts.read, state).ok === false);
state = context({['中'.repeat(400)]: '值'});
check(vm.runInContext(scripts.read, state).ok === false);
state = context();
state.localStorage.getItem = () => { throw new Error('仅测试拒绝访问'); };
check(vm.runInContext(scripts.readNeither, state).ok === true);
written = vm.runInContext(scripts.write, state);
check(written.ok === false && state.localStorage.values.get('old') === '原数据');
console.log('实际原生网页存储脚本边界测试通过：' + checks + ' 项；不代表 Android 设备验收');
