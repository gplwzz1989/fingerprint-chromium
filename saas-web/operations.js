(function installConsoleOperations(global) {
  'use strict';
  let context, controller, key = null, operation = false, conflictAccount = null;
  let unlockGeneration = 0;
  let fingerprintTarget = null;
  const find = (id) => document.getElementById(id);
  const status = (message) => { find('operation-status').textContent = message; };
  function batchProgress(completed, total, failures = []) {
    const panel = find('operation-progress');
    if (!panel) return;
    panel.hidden = false;
    const percent = total ? Math.round((completed / total) * 100) : 0;
    find('operation-progress-bar').style.width = `${percent}%`;
    find('operation-progress-label').textContent = `${completed}/${total} 个账号已处理`;
    const result = find('operation-results'); result.replaceChildren();
    for (const failure of failures) {
      const item = document.createElement('li'); item.textContent = failure; result.append(item);
    }
  }
  function leaseNotice(account, error) {
    if (!['lease_conflict', 'lease_required'].includes(error?.code)) return;
    const notice = find('lease-notice');
    if (!notice) return;
    notice.hidden = false;
    notice.textContent = error.code === 'lease_conflict'
      ? `${account.name} 正在被其他设备编辑，请等待租约释放后重试。`
      : `${account.name} 的编辑租约已失效，请重新发起同步或恢复。`;
  }
  function scopeToken() {
    const state = context.getState();
    return state.session ? `${state.sessionGeneration}:${state.workspaceId}` : null;
  }
  function checkScope(original) {
    if (!original || scopeToken() !== original) throw new Error('登录会话或工作区已变化，请重新操作');
  }
  function checkTabScope(original, tab) {
    checkScope(original);
    const state = context.getState();
    if (!state.accounts.some((account) => account.account_id === tab.account_id && account.workspace_id === state.workspaceId)) throw new Error('当前工作区没有该账号的访问权限');
  }

  function lock() {
    unlockGeneration++;
    key = null;
    find('operation-progress')?.setAttribute('hidden', '');
    controller?.clearConflicts(); conflictAccount = null;
    find('snapshot-conflict-modal').close();
    find('fingerprint-modal').close();
    fingerprintTarget = null;
    find('snapshot-password').value = '';
    find('snapshot-lock-state').textContent = '未解锁';
  }

  function syncOptions() {
    return Object.fromEntries([...find('sync-options').querySelectorAll('input')].map((input) => [input.name, input.checked]));
  }

  function actionButton(label, action) {
    const button = document.createElement('button');
    button.type = 'button'; button.className = 'table-action'; button.textContent = label;
    button.addEventListener('click', async () => {
      if (button.disabled) return;
      button.disabled = true;
      try { await action(); }
      catch (error) { context.showToast(context.userMessage(error), true); status(context.userMessage(error)); }
      finally { button.disabled = false; }
    });
    return button;
  }

  async function runAccount(account, restore) {
    if (!context.getState().session) throw new Error('请先登录');
    if (operation) throw new Error('已有批量操作正在执行');
    status(`正在${restore ? '恢复' : '同步'}账号：${account.name}`);
    let result;
    try { result = await controller[restore ? 'restore' : 'upload'](account); }
    catch (error) {
      leaseNotice(account, error);
      if (!restore && controller.getConflict(account.account_id)) showConflict(account);
      throw error;
    }
    status(`账号 ${account.name} 已${restore ? '恢复' : '同步'}，云端版本 ${result.revision}`);
    await context.refreshAccounts();
  }

  function showConflict(account) {
    const conflict = controller.getConflict(account.account_id);
    if (!conflict) return;
    conflictAccount = account;
    find('snapshot-conflict-title').textContent = `${account.name} · 同步冲突`;
    find('snapshot-conflict-detail').textContent = '云端数据在本次上传前发生了变化。本地快照尚未写入，请先确认要保留哪一份环境。';
    find('conflict-local-version').textContent = `本地目录版本 r${account.revision ?? '—'}`;
    find('conflict-cloud-version').textContent = `云端当前版本 r${conflict.revision ?? '—'}`;
    find('snapshot-conflict-status').textContent = '';
    if (!find('snapshot-conflict-modal').open) find('snapshot-conflict-modal').showModal();
  }

  async function resolveConflict(strategy) {
    const account = conflictAccount;
    if (!account || context.getState().workspaceId !== account.workspace_id) throw new Error('工作区已切换，请重新同步该账号');
    for (const id of ['conflict-keep-cloud', 'conflict-merge', 'conflict-overwrite']) find(id).disabled = true;
    try {
      const saved = await controller.resolveConflict(account, strategy);
      find('snapshot-conflict-modal').close(); conflictAccount = null;
      status(`冲突已${strategy === 'merge' ? '合并' : '明确覆盖'}，云端版本 ${saved.revision}`);
      await context.refreshAccounts();
    } catch (error) {
      const conflict = controller.getConflict(account.account_id);
      find('snapshot-conflict-status').textContent = context.userMessage(error);
      if (conflict) {
        find('snapshot-conflict-detail').textContent = '云端版本再次变化，本次处理没有写入。请重新确认版本后再操作。';
        find('conflict-cloud-version').textContent = `云端当前版本 r${conflict.revision ?? '—'}`;
      }
    } finally {
      for (const id of ['conflict-keep-cloud', 'conflict-merge', 'conflict-overwrite']) find(id).disabled = false;
    }
  }

  async function batch(restore, selectedIds = null) {
    if (operation) return;
    if (!key) throw new Error('请先解锁加密快照');
    operation = true;
    const originalState = context.getState();
    const sessionGeneration = originalState.sessionGeneration, workspaceId = originalState.workspaceId;
    let success = 0;
    const failures = [];
    try {
      const tabs = restore ? [] : await context.bridge.tabs.list();
      const selected = selectedIds ? new Set(selectedIds) : null;
      const accounts = context.getState().accounts.filter((account) =>
        (!selected || selected.has(account.account_id)) &&
        (restore || (account.role !== 'viewer' && tabs.some((tab) => tab.account_id === account.account_id))));
      if (!accounts.length) throw new Error(selected ? '所选账号没有可执行的同步任务' : '当前没有可执行的账号');
      batchProgress(0, accounts.length);
      for (const account of accounts) {
        if (!context.getState().session || context.getState().sessionGeneration !== sessionGeneration || context.getState().workspaceId !== workspaceId) throw new Error('登录会话或工作区已变化，批量操作已停止');
        status(`正在处理 ${success + failures.length + 1}/${accounts.length}：${account.name}`);
        try { await controller[restore ? 'restore' : 'upload'](account); success++; batchProgress(success + failures.length, accounts.length, failures); }
        catch (error) {
          leaseNotice(account, error);
          failures.push(`${account.name}：${context.userMessage(error)}`);
          batchProgress(success + failures.length, accounts.length, failures);
          if (!restore && controller.getConflict(account.account_id)) break;
        }
      }
      status(`完成 ${success}/${accounts.length}。${failures.join('；')}`);
      batchProgress(success + failures.length, accounts.length, failures);
      await context.refreshAccounts();
      const conflict = accounts.find((account) => controller.getConflict(account.account_id));
      if (conflict) showConflict(conflict);
    } finally { operation = false; }
  }

  async function loadTabs() {
    const body = find('tabs-table-body'); body.replaceChildren();
    find('tabs-status').textContent = '正在读取本机 Tab…';
    const original = scopeToken();
    try {
      checkScope(original);
      const state = context.getState();
      const allowed = new Set(state.accounts.filter((account) => account.workspace_id === state.workspaceId).map((account) => account.account_id));
      const tabs = (await context.bridge.tabs.list()).filter((tab) => allowed.has(tab.account_id));
      checkScope(original);
      for (const tab of tabs) {
        const row = document.createElement('tr');
        const name = document.createElement('td'), isolation = document.createElement('td'), actions = document.createElement('td');
        name.textContent = `${tab.account_id} · ${tab.title || tab.url || '空白页'}`;
        isolation.textContent = tab.storage_partition_persistent ? '独立持久化分区' : '临时分区';
        actions.className = 'table-actions';
        actions.append(actionButton('切换', () => { checkTabScope(original, tab); return context.bridge.tabs.activate(tab.id); }),
          actionButton('指纹', () => { checkTabScope(original, tab); return editFingerprint(tab); }),
          actionButton('关闭', async () => {
            checkTabScope(original, tab);
            if (await context.bridge.tabs.close(tab.id) === false) throw new Error('账号 Tab 未能关闭');
            await loadTabs();
          }));
        row.append(name, isolation, actions); body.append(row);
      }
      find('tabs-status').textContent = tabs.length ? `本工作区共 ${tabs.length} 个运行环境` : '当前工作区没有已打开的账号 Tab';
    } catch (error) { if (scopeToken() === original) find('tabs-status').textContent = context.userMessage(error); }
  }

  async function loadSessions() {
    const body = find('sessions-table-body'); body.replaceChildren();
    find('sessions-status').textContent = '正在读取登录设备…';
    const secureStorage = context.getState().bridge?.capabilities?.secureStorage === true;
    find('security-copy').textContent = secureStorage
      ? '当前客户端使用设备安全存储保护刷新会话；访问令牌只保留在当前页面内存。'
      : '当前客户端未提供设备安全存储，退出后不会自动恢复受保护会话。';
    const original = scopeToken();
    try {
      checkScope(original);
      const sessions = await context.request('/api/v1/sessions');
      checkScope(original);
      for (const session of sessions) {
        const row = document.createElement('tr');
        const name = document.createElement('td'), expiry = document.createElement('td'), actions = document.createElement('td');
        const nameWrap = document.createElement('div'); nameWrap.className = 'session-name-cell';
        const title = document.createElement('strong'); title.textContent = session.device_name || '未命名设备';
        const device = document.createElement('span'); device.textContent = session.device_id || '设备标识未提供';
        nameWrap.append(title, device);
        if (session.current) { const badge = document.createElement('span'); badge.className = 'role-badge role-editor'; badge.textContent = '当前设备'; nameWrap.append(badge); }
        name.append(nameWrap);
        expiry.textContent = new Date(session.expires_at).toLocaleString('zh-CN');
        actions.append(actionButton(session.current ? '退出当前设备' : '撤销会话', async () => {
          checkScope(original);
          if (session.current) { await context.logout(); return; }
          await context.request('/api/v1/sessions/' + encodeURIComponent(session.session_id), {method: 'DELETE'});
          await loadSessions();
        }));
        row.append(name, expiry, actions); body.append(row);
      }
      find('sessions-status').textContent = sessions.length ? `共 ${sessions.length} 个有效登录会话` : '当前没有有效设备会话';
    } catch (error) { if (scopeToken() === original) find('sessions-status').textContent = context.userMessage(error); }
  }

  async function editFingerprint(tab) {
    const original = scopeToken();
    checkTabScope(original, tab);
    const value = await context.bridge.fingerprint.get(tab.id);
    checkTabScope(original, tab);
    fingerprintTarget = {tab, original};
    const form = find('fingerprint-form');
    form.elements.tab_id.value = tab.id;
    form.elements.user_agent.value = value.user_agent || '';
    form.elements.hardware_concurrency.value = value.hardware_concurrency || 0;
    find('fingerprint-status').textContent = '';
    find('fingerprint-modal').showModal();
  }

  function configure(value) {
    context = value;
    controller = global.saasSnapshotSync.createController({request: context.request, bridge: context.bridge,
      deviceId: context.deviceId, getKey: () => context.getState().session ? key : null, getOptions: syncOptions,
      getSession: () => context.getState().session ? `${context.getState().sessionGeneration}:${context.getState().workspaceId}` : null});
    find('snapshot-unlock-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        const sessionGeneration = context.getState().sessionGeneration, generation = ++unlockGeneration;
        if (!context.getState().session) throw new Error('请先登录');
        const unlocked = await global.saasSnapshotSync.passwordKey(find('snapshot-password').value);
        if (!context.getState().session || context.getState().sessionGeneration !== sessionGeneration || unlockGeneration !== generation) throw new Error('登录会话或锁定状态已变化，请重新解锁');
        key = unlocked;
        find('snapshot-password').value = '';
        find('snapshot-lock-state').textContent = '已解锁'; status('快照已解锁，可在账号操作中同步或恢复');
      } catch (error) { status(context.userMessage(error)); }
    });
    find('snapshot-lock-button').addEventListener('click', lock);
    find('conflict-keep-cloud').addEventListener('click', async () => {
      if (conflictAccount) controller.discardConflict(conflictAccount.account_id);
      conflictAccount = null; find('snapshot-conflict-modal').close();
      status('已保留云端版本；本地环境未改动，可通过恢复按钮读取云端环境');
      try { await context.refreshAccounts(); } catch (error) { status(context.userMessage(error)); }
    });
    find('conflict-merge').addEventListener('click', () => resolveConflict('merge').catch((error) => status(context.userMessage(error))));
    find('conflict-overwrite').addEventListener('click', () => resolveConflict('overwrite').catch((error) => status(context.userMessage(error))));
    for (const [id, restore] of [['sync-all-button', false], ['restore-all-button', true]]) {
      find(id).addEventListener('click', async (event) => {
        event.currentTarget.disabled = true;
        try { await batch(restore); } catch (error) { status(context.userMessage(error)); }
        finally { find(id).disabled = false; }
      });
    }
    find('refresh-tabs-button').addEventListener('click', loadTabs);
    find('refresh-sessions-button').addEventListener('click', loadSessions);
    find('fingerprint-form').addEventListener('submit', async (event) => {
      if (event.submitter?.value === 'cancel') return;
      event.preventDefault();
      const form = event.currentTarget;
      try {
        if (!fingerprintTarget || form.elements.tab_id.value !== fingerprintTarget.tab.id) throw new Error('请重新打开账号指纹配置');
        checkTabScope(fingerprintTarget.original, fingerprintTarget.tab);
        const result = await context.bridge.fingerprint.set(form.elements.tab_id.value, {
          user_agent: form.elements.user_agent.value, hardware_concurrency: Number(form.elements.hardware_concurrency.value),
        });
        find('fingerprint-modal').close(); await loadTabs();
        if (result?.requires_reload) context.showToast('指纹配置已保存，需要重新加载账号页面');
      } catch (error) { find('fingerprint-status').textContent = context.userMessage(error); }
    });
    global.addEventListener('pagehide', lock);
  }

  global.saasConsoleOperations = {configure, lock, runBatch: batch,
    clear() {
      lock(); find('fingerprint-form').reset();
      for (const id of ['tabs-table-body', 'sessions-table-body']) find(id).replaceChildren();
      for (const id of ['tabs-status', 'sessions-status', 'operation-status']) find(id).textContent = '';
    },
    appendAccountActions(cell, account) {
      cell.className = 'table-actions';
      const sync = actionButton('同步', () => runAccount(account, false));
      sync.disabled = account.role === 'viewer';
      if (sync.disabled) sync.title = '该账号为只读权限，不能写入云端快照';
      cell.append(sync, actionButton('恢复', () => runAccount(account, true)));
      global.saasConsoleAdministration.appendAccountActions(cell, account);
    },
    loadView(view) { if (view === 'tabs') loadTabs(); if (view === 'security') loadSessions(); },
  };
})(window);
