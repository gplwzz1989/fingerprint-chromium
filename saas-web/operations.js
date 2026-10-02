(function installConsoleOperations(global) {
  'use strict';
  let context, controller, key = null, operation = false;
  const find = (id) => document.getElementById(id);
  const status = (message) => { find('operation-status').textContent = message; };

  function lock() {
    key = null;
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
    const result = await controller[restore ? 'restore' : 'upload'](account);
    status(`账号 ${account.name} 已${restore ? '恢复' : '同步'}，云端版本 ${result.revision}`);
    await context.refreshAccounts();
  }

  async function batch(restore) {
    if (operation) return;
    if (!key) throw new Error('请先解锁加密快照');
    operation = true;
    const sessionUserId = context.getState().session?.user?.user_id;
    let success = 0;
    const failures = [];
    try {
      const tabs = restore ? [] : await context.bridge.tabs.list();
      const accounts = context.getState().accounts.filter((account) => restore || tabs.some((tab) => tab.account_id === account.account_id));
      for (const account of accounts) {
        if (context.getState().session?.user?.user_id !== sessionUserId) throw new Error('登录会话已变化，批量操作已停止');
        status(`正在处理 ${success + failures.length + 1}/${accounts.length}：${account.name}`);
        try { await controller[restore ? 'restore' : 'upload'](account); success++; }
        catch (error) { failures.push(`${account.name}：${context.userMessage(error)}`); }
      }
      status(`完成 ${success}/${accounts.length}。${failures.join('；')}`);
      await context.refreshAccounts();
    } finally { operation = false; }
  }

  async function loadTabs() {
    const body = find('tabs-table-body'); body.replaceChildren();
    find('tabs-status').textContent = '正在读取本机 Tab…';
    try {
      const allowed = new Set(context.getState().accounts.map((account) => account.account_id));
      const tabs = (await context.bridge.tabs.list()).filter((tab) => allowed.has(tab.account_id));
      for (const tab of tabs) {
        const row = document.createElement('tr');
        const name = document.createElement('td'), isolation = document.createElement('td'), actions = document.createElement('td');
        name.textContent = `${tab.account_id} · ${tab.title || tab.url || '空白页'}`;
        isolation.textContent = tab.storage_partition_persistent ? '独立持久化分区' : '临时分区';
        actions.className = 'table-actions';
        actions.append(actionButton('切换', () => context.bridge.tabs.activate(tab.id)),
          actionButton('指纹', () => editFingerprint(tab)),
          actionButton('关闭', async () => {
            if (await context.bridge.tabs.close(tab.id) === false) throw new Error('账号 Tab 未能关闭');
            await loadTabs();
          }));
        row.append(name, isolation, actions); body.append(row);
      }
      find('tabs-status').textContent = tabs.length ? `本工作区共 ${tabs.length} 个运行环境` : '当前工作区没有已打开的账号 Tab';
    } catch (error) { find('tabs-status').textContent = context.userMessage(error); }
  }

  async function loadSessions() {
    const body = find('sessions-table-body'); body.replaceChildren();
    find('sessions-status').textContent = '正在读取登录设备…';
    try {
      const sessions = await context.request('/api/v1/sessions');
      for (const session of sessions) {
        const row = document.createElement('tr');
        const name = document.createElement('td'), expiry = document.createElement('td'), actions = document.createElement('td');
        name.textContent = `${session.device_name || '未命名设备'}${session.current ? '（当前设备）' : ''}`;
        expiry.textContent = new Date(session.expires_at).toLocaleString('zh-CN');
        actions.append(actionButton('撤销会话', async () => {
          if (session.current) { await context.logout(); return; }
          await context.request('/api/v1/sessions/' + encodeURIComponent(session.session_id), {method: 'DELETE'});
          await loadSessions();
        }));
        row.append(name, expiry, actions); body.append(row);
      }
      find('sessions-status').textContent = `共 ${sessions.length} 个有效登录会话`;
    } catch (error) { find('sessions-status').textContent = context.userMessage(error); }
  }

  async function editFingerprint(tab) {
    const value = await context.bridge.fingerprint.get(tab.id);
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
      getSession: () => context.getState().session?.user?.user_id || null});
    find('snapshot-unlock-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        key = await global.saasSnapshotSync.passwordKey(find('snapshot-password').value);
        find('snapshot-password').value = '';
        find('snapshot-lock-state').textContent = '已解锁'; status('快照已解锁，可在账号操作中同步或恢复');
      } catch (error) { status(context.userMessage(error)); }
    });
    find('snapshot-lock-button').addEventListener('click', lock);
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
        await context.bridge.fingerprint.set(form.elements.tab_id.value, {
          user_agent: form.elements.user_agent.value, hardware_concurrency: Number(form.elements.hardware_concurrency.value),
        });
        find('fingerprint-modal').close(); await loadTabs();
      } catch (error) { find('fingerprint-status').textContent = context.userMessage(error); }
    });
    global.addEventListener('pagehide', lock);
  }

  global.saasConsoleOperations = {configure, lock,
    appendAccountActions(cell, account) {
      cell.className = 'table-actions';
      cell.append(actionButton('同步', () => runAccount(account, false)),
        actionButton('恢复', () => runAccount(account, true)));
    },
    loadView(view) { if (view === 'tabs') loadTabs(); if (view === 'security') loadSessions(); },
  };
})(window);
