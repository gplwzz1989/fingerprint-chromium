// 仅检查真实页面状态契约和无障碍挂点，不提供账号、成员、设备或原生桥假数据。
const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');

const read = (name) => fs.readFileSync(require.resolve(`./${name}`), 'utf8');
const html = read('index.html');
const app = read('app.js');
const operations = read('operations.js');
const administration = read('administration.js');

test('账号工作台保留真实加载、详情和运行状态入口', () => {
  for (const id of ['account-table', 'account-detail-modal', 'account-runtime-status', 'account-empty']) {
    assert.match(html, new RegExp(`id="${id}"`));
  }
  assert.match(app, /accountsLoadState/);
  assert.match(app, /openAccountDetail/);
  assert.match(app, /saasBridgeClient\.tabs\.list\(\)/);
  assert.match(app, /accountOpenFlights/);
  assert.match(app, /已切换到账号/);
  assert.match(app, /refreshRuntimeTabs/);
  assert.match(app, /runtimeTabsLoadError/);
  assert.match(app, /renderRuntimeTabStrip/);
  assert.match(app, /runtime-badge/);
  assert.match(html, /运行状态/);
  assert.match(html, /workspace-tab-strip/);
});

test('批量同步保留租约分类和可感知进度', () => {
  assert.match(html, /role="progressbar"/);
  assert.match(html, /aria-valuenow="0"/);
  assert.match(operations, /leaseNotices = new Map/);
  assert.match(operations, /lease_conflict/);
  assert.match(operations, /lease_required/);
  assert.match(operations, /aria-valuenow/);
  assert.match(operations, /downloadSnapshot/);
  assert.match(operations, /controller\.import/);
  assert.match(html, /id="snapshot-import-file"/);
});

test('成员和设备页面暴露加载忙状态及真实错误文案入口', () => {
  for (const id of ['members-table', 'sessions-table', 'security-device-id', 'account-access-role-help']) {
    assert.match(html, new RegExp(`id="${id}"`));
  }
  assert.match(administration, /成员列表格式无效/);
  assert.match(administration, /账号授权数据格式无效/);
  assert.match(administration, /删除账号/);
  assert.match(operations, /security-protection-state/);
  assert.match(operations, /refresh-sessions-button.*disabled = true/);
});
