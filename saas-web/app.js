(function startSaasConsole(global) {
  'use strict';

  const apiBase = (document.querySelector('meta[name="saas-api-base"]')?.content || '').replace(/\/$/, '');
  const sessionStorageKey = 'fingerprint-saas.session.v1';
  const deviceStorageKey = 'fingerprint-saas.device-id.v1';
  const state = {
    session: null,
    sessionGeneration: 0,
    workspaces: [],
    workspaceId: '',
    accounts: [],
    accountsLoadState: 'idle',
    accountsLoadError: '',
    bridge: null,
    currentView: 'overview',
    selectedAccountIds: new Set(),
  };
  let refreshFlight = null;
  let sessionRecoveryAttempted = false;
  let sessionMarkerStorage;
  try { sessionMarkerStorage = global.localStorage; } catch (_) { sessionMarkerStorage = null; }
  const sessionPersistence = global.saasSessionPersistence.create({bridge: global.saasBridgeClient,
    serviceUrl: new URL(`${apiBase}/api/v1/sessions/refresh`, global.location.href).href,
    deviceId: getDeviceId, storage: sessionMarkerStorage});

  class ApiError extends Error {
    constructor(status, message, code = '', currentRevision = null) {
      super(message || '请求失败，请稍后重试');
      this.name = 'ApiError';
      this.status = status;
      this.code = code;
      this.currentRevision = currentRevision;
    }
  }

  const elements = {
    loginView: document.querySelector('#login-view'),
    appView: document.querySelector('#app-view'),
    loginForm: document.querySelector('#login-form'),
    loginStatus: document.querySelector('#login-status'),
    accountForm: document.querySelector('#account-form'),
    accountModal: document.querySelector('#account-modal'),
    accountStatus: document.querySelector('#account-status'),
    accountEditForm: document.querySelector('#account-edit-form'),
    accountEditModal: document.querySelector('#account-edit-modal'),
    accountEditStatus: document.querySelector('#account-edit-status'),
    workspaceForm: document.querySelector('#workspace-form'),
    workspaceModal: document.querySelector('#workspace-modal'),
    workspaceStatus: document.querySelector('#workspace-status'),
    workspaceSelect: document.querySelector('#workspace-select'),
    workspaceBreadcrumb: document.querySelector('#workspace-breadcrumb'),
    userChip: document.querySelector('#user-chip'),
    userAvatar: document.querySelector('#user-avatar'),
    userName: document.querySelector('#user-name'),
    accountTableBody: document.querySelector('#account-table-body'),
    accountEmpty: document.querySelector('#account-empty'),
    accountEmptyTitle: document.querySelector('#account-empty-title'),
    accountEmptyCopy: document.querySelector('#account-empty-copy'),
    accountSearch: document.querySelector('#account-search'),
    accountFilter: document.querySelector('#account-filter'),
    metricAccounts: document.querySelector('#metric-accounts'),
    metricRevisions: document.querySelector('#metric-revisions'),
    metricBridge: document.querySelector('#metric-bridge'),
    metricBridgeMeta: document.querySelector('#metric-bridge-meta'),
    bridgeMiniStatus: document.querySelector('#bridge-mini-status'),
    bridgeStatusBlock: document.querySelector('#bridge-status-block'),
    bridgeStatusDot: document.querySelector('#bridge-status-dot'),
    bridgeStatusTitle: document.querySelector('#bridge-status-title'),
    bridgeStatusDetail: document.querySelector('#bridge-status-detail'),
    bridgeOriginPolicy: document.querySelector('#bridge-origin-policy'),
    capabilityList: document.querySelector('#capability-list'),
    accountTable: document.querySelector('#account-table'),
    accountDetailModal: document.querySelector('#account-detail-modal'),
    accountDetailTitle: document.querySelector('#account-detail-title'),
    accountDetailSummary: document.querySelector('#account-detail-summary'),
    accountDetailMeta: document.querySelector('#account-detail-meta'),
    accountRuntimeBadge: document.querySelector('#account-runtime-badge'),
    accountRuntimeStatus: document.querySelector('#account-runtime-status'),
    accountRuntimeCard: document.querySelector('#account-runtime-card'),
    toast: document.querySelector('#toast'),
  };

  function getDeviceId() {
    const existing = localStorage.getItem(deviceStorageKey);
    if (existing) return existing;
    const value = typeof crypto?.randomUUID === 'function'
      ? crypto.randomUUID()
      : `device-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
    localStorage.setItem(deviceStorageKey, value);
    return value;
  }

  function readSession() {
    try {
      if (sessionPersistence.requiresSecureStorage()) return null;
      const value = sessionStorage.getItem(sessionStorageKey);
      return value ? JSON.parse(value) : null;
    } catch (_error) {
      return null;
    }
  }

  function writeSession(session) {
    if (!session || !state.session || session.user?.user_id !== state.session.user?.user_id) state.sessionGeneration++;
    state.session = session;
    if (session && !sessionPersistence.requiresSecureStorage()) {
      try { sessionStorage.setItem(sessionStorageKey, JSON.stringify(session)); }
      catch (_) { showToast('本地会话保存不可用，本次登录仅在当前页面有效', true); }
    } else {
      try { sessionStorage.removeItem(sessionStorageKey); }
      catch (_) { showToast('网页会话缓存不可用，请检查存储权限', true); }
    }
    if (!session) {
      state.workspaces = []; state.accounts = []; state.workspaceId = '';
      state.accountsLoadState = 'idle'; state.accountsLoadError = '';
      elements.accountDetailModal?.close();
      elements.loginForm.reset();
      global.saasConsoleOperations.clear();
      global.saasConsoleAdministration.clear();
      render();
    }
    const saved = session ? sessionPersistence.save(session) : sessionPersistence.clear();
    return saved.catch((error) => {
      if (state.session === session) showToast(userMessage(error), true);
      return false;
    });
  }

  function setStatus(element, message, success = false) {
    element.textContent = message || '';
    element.classList.toggle('is-success', Boolean(success));
  }

  function showToast(message, isError = false) {
    elements.toast.textContent = message;
    elements.toast.classList.toggle('is-error', isError);
    elements.toast.classList.add('is-visible');
    clearTimeout(showToast.timeout);
    showToast.timeout = setTimeout(() => elements.toast.classList.remove('is-visible'), 3200);
  }

  function userMessage(error) {
    if (error instanceof ApiError) return error.message;
    if (error instanceof Error && /[\u4e00-\u9fff]/.test(error.message)) return error.message;
    return '操作失败，请稍后重试';
  }

  async function request(path, options = {}, allowRefresh = true) {
    const userId = state.session?.user?.user_id || null;
    const generation = state.sessionGeneration;
    const headers = new Headers(options.headers || {});
    let body = options.body;
    if (body !== undefined && body !== null && typeof body !== 'string') {
      headers.set('Content-Type', 'application/json');
      body = JSON.stringify(body);
    }
    if (state.session?.accessToken) {
      headers.set('Authorization', `Bearer ${state.session.accessToken}`);
    }
    const response = await fetch(`${apiBase}${path}`, {
      ...options,
      body,
      headers,
      credentials: 'omit',
    });
    if (state.sessionGeneration !== generation || (state.session?.user?.user_id || null) !== userId) throw new ApiError(401, '登录会话已变化，请重试');
    if (response.status === 401 && allowRefresh && state.session?.refreshToken) {
      const refreshed = await refreshSession();
      if (refreshed) return request(path, options, false);
    }
    const contentType = response.headers.get('content-type') || '';
    const payload = contentType.includes('application/json')
      ? await response.json().catch(() => null)
      : null;
    if (state.sessionGeneration !== generation) throw new ApiError(401, '登录会话已变化，请重试');
    if (!response.ok) {
      throw new ApiError(response.status, payload?.message || `服务返回 ${response.status}`, payload?.code || '', payload?.current_revision ?? null);
    }
    return payload;
  }

  async function refreshSession() {
    if (!state.session?.refreshToken) return false;
    if (refreshFlight?.session === state.session) return refreshFlight.promise;
    const flight = {session: state.session, promise: performRefreshSession(state.session)};
    refreshFlight = flight;
    try { return await flight.promise; }
    finally { if (refreshFlight === flight) refreshFlight = null; }
  }

  async function performRefreshSession(originalSession) {
    try {
      const response = await fetch(`${apiBase}/api/v1/sessions/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'omit',
        body: JSON.stringify({ refresh_token: originalSession.refreshToken, device_id: originalSession.deviceId || getDeviceId() }),
      });
      const payload = await response.json().catch(() => null);
      if (state.session !== originalSession) return false;
      if (!response.ok || !payload?.access_token) {
        if (response.status === 401) writeSession(null);
        return false;
      }
      writeSession({
        accessToken: payload.access_token,
        refreshToken: payload.refresh_token || originalSession.refreshToken,
        user: payload.user,
        deviceId: originalSession.deviceId || getDeviceId(),
      });
      return true;
    } catch (_error) {
      // 暂时断网时保留刷新凭据，恢复网络后可重试，不能把网络错误当成撤销会话。
      return false;
    }
  }

  async function login(email, password) {
    const deviceId = getDeviceId();
    const payload = await request('/api/v1/sessions', {
      method: 'POST',
      body: {
        email,
        password,
        device_id: deviceId,
        device_name: getDeviceName(),
      },
    }, false);
    writeSession({
      accessToken: payload.access_token,
      refreshToken: payload.refresh_token,
      user: payload.user,
      deviceId,
    });
    elements.loginForm.reset();
  }

  function getDeviceName() {
    const platform = global.navigator.userAgentData?.platform || global.navigator.platform || '桌面设备';
    return `SaaS Web · ${platform}`.slice(0, 128);
  }

  async function loadWorkspaces() {
    state.workspaces = await request('/api/v1/workspaces');
    const storedWorkspaceId = sessionStorage.getItem('fingerprint-saas.workspace-id.v1');
    state.workspaceId = state.workspaces.some((item) => item.workspace_id === storedWorkspaceId)
      ? storedWorkspaceId
      : state.workspaces[0]?.workspace_id || '';
    if (state.workspaceId) sessionStorage.setItem('fingerprint-saas.workspace-id.v1', state.workspaceId);
    await loadAccounts();
  }

  async function loadAccounts() {
    if (!state.workspaceId) {
      state.accounts = [];
      state.accountsLoadState = 'ready';
      state.accountsLoadError = '';
      render();
      return;
    }
    const workspaceId = state.workspaceId;
    const accounts = [], seenTokens = new Set();
    let pageToken = '';
    state.accountsLoadState = 'loading';
    state.accountsLoadError = '';
    render();
    try {
      do {
        const query = new URLSearchParams({page_size: '200'});
        if (pageToken) query.set('page_token', pageToken);
        const payload = await request(`/api/v1/workspaces/${encodeURIComponent(workspaceId)}/accounts?${query}`);
        accounts.push(...(Array.isArray(payload) ? payload : (payload?.items || [])));
        pageToken = payload?.next_page_token || '';
        if (pageToken && seenTokens.has(pageToken)) throw new ApiError(502, '账号分页响应无效，请重试');
        if (pageToken) seenTokens.add(pageToken);
      } while (pageToken);
      if (state.workspaceId !== workspaceId || !state.session) return;
      state.accounts = accounts;
      state.accountsLoadState = 'ready';
      state.accountsLoadError = '';
      const available = new Set(accounts.map((account) => account.account_id));
      for (const accountId of state.selectedAccountIds) if (!available.has(accountId)) state.selectedAccountIds.delete(accountId);
      render();
    } catch (error) {
      if (state.workspaceId === workspaceId && state.session) {
        state.accountsLoadState = 'error';
        state.accountsLoadError = userMessage(error);
        render();
      }
      throw error;
    }
  }

  async function createWorkspace(name) {
    const workspace = await request('/api/v1/workspaces', { method: 'POST', body: { name } });
    await loadWorkspaces();
    state.workspaceId = workspace.workspace_id;
    sessionStorage.setItem('fingerprint-saas.workspace-id.v1', state.workspaceId);
    await loadAccounts();
  }

  async function createAccount(accountId, name, labels) {
    await request(`/api/v1/workspaces/${encodeURIComponent(state.workspaceId)}/accounts`, {
      method: 'POST',
      body: { account_id: accountId, name, labels },
    });
    await loadAccounts();
  }

  async function updateAccount(account, name, labels, scope) {
    if (!state.session || state.sessionGeneration !== scope.generation || state.workspaceId !== scope.workspaceId ||
        !state.accounts.some((value) => value.account_id === account.account_id && value.workspace_id === scope.workspaceId)) {
      throw new Error('登录会话、工作区或账号权限已变化，请重新操作');
    }
    await request(`/api/v1/accounts/${encodeURIComponent(account.account_id)}`, {
      method: 'PATCH', body: {name, labels},
    });
    await loadAccounts();
  }

  async function logout() {
    sessionRecoveryAttempted = true;
    const session = state.session;
    writeSession(null);
    try {
      if (session?.accessToken) await fetch(`${apiBase}/api/v1/sessions/revoke`, {
        method: 'POST', headers: {Authorization: `Bearer ${session.accessToken}`}, credentials: 'omit',
      });
    } catch (_error) {
      // 服务端不可达时仍然清除当前设备会话。
    }
  }

  async function inspectBridge() {
    state.bridge = await global.saasBridgeClient.describe();
    renderBridge();
    const wasEnabled = sessionPersistence.isEnabled();
    if (!sessionPersistence.configure(state.bridge) || wasEnabled) return;
    try { sessionStorage.removeItem(sessionStorageKey); }
    catch (_) { showToast('网页会话缓存清理失败，请检查存储权限', true); }
    if (state.session?.accessToken) {
      sessionRecoveryAttempted = true;
      await writeSession(state.session);
    } else if (!sessionRecoveryAttempted) {
      sessionRecoveryAttempted = true;
      const generation = state.sessionGeneration;
      const stored = await sessionPersistence.restore();
      if (!stored || generation !== state.sessionGeneration || state.session) return;
      state.session = {refreshToken: stored.refreshToken, deviceId: stored.deviceId};
      if (await refreshSession()) { await loadWorkspaces(); render(); }
    }
  }

  async function openAccount(account) {
    if (!state.bridge?.available) {
      showToast('当前页面没有可用的浏览器原生桥，请在受支持的客户端中打开', true);
      return;
    }
    const generation = state.sessionGeneration, workspaceId = state.workspaceId;
    const checkAccountScope = () => {
      if (!state.session || state.sessionGeneration !== generation || state.workspaceId !== workspaceId ||
          account.workspace_id !== workspaceId || !state.accounts.some((value) => value.account_id === account.account_id && value.workspace_id === workspaceId)) {
        throw new Error('登录会话、工作区或账号权限已变化，请重新操作');
      }
    };
    try {
      checkAccountScope();
      const tabs = await global.saasBridgeClient.tabs.list();
      checkAccountScope();
      const existing = tabs.find((tab) => tab.account_id === account.account_id);
      if (existing) await global.saasBridgeClient.tabs.activate(existing.id);
      else await global.saasBridgeClient.tabs.create({ accountId: account.account_id });
      showToast(`已请求创建账号 ${account.name} 的隔离 Tab`);
    } catch (error) {
      showToast(userMessage(error), true);
    }
  }

  function formatDate(value) {
    if (!value) return '—';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { dateStyle: 'short', timeStyle: 'short' });
  }

  function renderSession() {
    const loggedIn = Boolean(state.session?.accessToken && state.session?.user);
    elements.loginView.hidden = loggedIn;
    elements.appView.hidden = !loggedIn;
    elements.userChip.hidden = !loggedIn;
    if (!loggedIn) {
      elements.workspaceBreadcrumb.textContent = '未登录';
      return;
    }
    const user = state.session.user;
    const displayName = user.display_name || user.email || '当前用户';
    elements.userName.textContent = displayName;
    elements.userAvatar.textContent = displayName.slice(0, 1).toUpperCase();
  }

  function renderWorkspaces() {
    elements.workspaceSelect.replaceChildren();
    if (!state.workspaces.length) {
      const option = new Option('暂无工作区', '');
      option.disabled = true;
      option.selected = true;
      elements.workspaceSelect.add(option);
      elements.workspaceBreadcrumb.textContent = '暂无工作区';
      return;
    }
    for (const workspace of state.workspaces) {
      const role = {owner: '所有者', admin: '管理员', editor: '编辑者', viewer: '查看者'}[workspace.role] || '成员';
      const option = new Option(`${workspace.name} · ${role}`, workspace.workspace_id);
      option.selected = workspace.workspace_id === state.workspaceId;
      elements.workspaceSelect.add(option);
    }
    const current = state.workspaces.find((item) => item.workspace_id === state.workspaceId);
    elements.workspaceBreadcrumb.textContent = current?.name || '工作区';
  }

  function renderAccounts() {
    const visibleAccounts = getVisibleAccounts();
    const loading = state.accountsLoadState === 'loading';
    const failed = state.accountsLoadState === 'error';
    elements.accountTable?.setAttribute('aria-busy', String(loading));
    elements.accountTableBody.replaceChildren();
    elements.accountEmpty.hidden = loading || (!failed && visibleAccounts.length > 0);
    const hasFilter = visibleAccounts.length !== state.accounts.length;
    elements.accountEmptyTitle.textContent = loading ? '正在读取账号目录…' : failed ? '账号目录读取失败' : hasFilter ? '没有符合条件的账号' : '当前工作区还没有账号';
    elements.accountEmptyCopy.textContent = loading ? '正在从服务端读取当前工作区的真实账号，请稍候。' : failed
      ? `${state.accountsLoadError || '服务暂时不可用'}。可使用右上角刷新按钮重试。`
      : hasFilter
      ? '请调整搜索或筛选条件。账号目录保持真实服务端数据，不会填充演示内容。'
      : '添加真实账号目录后，才能从浏览器创建隔离 Tab。这里不会填充演示数据。';
    document.querySelector('#empty-new-account-button').hidden = loading || failed || hasFilter;
    elements.metricAccounts.textContent = String(state.accounts.length);
    const revisionTotal = state.accounts.reduce((total, account) => total + Number(account.revision || 0), 0);
    elements.metricRevisions.textContent = state.accounts.length ? String(revisionTotal) : '—';
    for (const account of visibleAccounts) {
      const row = document.createElement('tr');
      const nameCell = document.createElement('td');
      const nameWrap = document.createElement('div');
      const checkbox = document.createElement('input');
      checkbox.type = 'checkbox'; checkbox.className = 'account-select';
      checkbox.checked = state.selectedAccountIds.has(account.account_id);
      checkbox.setAttribute('aria-label', `选择账号 ${account.name}`);
      checkbox.addEventListener('change', () => {
        if (checkbox.checked) state.selectedAccountIds.add(account.account_id);
        else state.selectedAccountIds.delete(account.account_id);
        renderAccountSelection();
      });
      nameWrap.className = 'account-name-cell';
      const avatar = document.createElement('span');
      avatar.className = 'account-avatar';
      avatar.textContent = account.account_id.slice(0, 2).toUpperCase();
      const nameBlock = document.createElement('div');
      const name = document.createElement('div');
      name.className = 'account-name';
      name.textContent = account.name;
      const id = document.createElement('div');
      id.className = 'account-id';
      id.textContent = account.account_id;
      nameBlock.append(name, id);
      nameWrap.append(avatar, nameBlock);
      nameCell.append(checkbox, nameWrap);

      const labelsCell = document.createElement('td');
      const labels = document.createElement('div');
      labels.className = 'tag-list';
      if (Array.isArray(account.labels) && account.labels.length) {
        for (const labelValue of account.labels.slice(0, 3)) {
          const label = document.createElement('span');
          label.className = 'tag';
          label.textContent = labelValue;
          labels.append(label);
        }
      } else {
        labels.textContent = '—';
        labels.className = 'tag-empty';
      }
      labelsCell.append(labels);

      const revisionCell = document.createElement('td');
      revisionCell.className = 'revision';
      revisionCell.textContent = `r${account.revision ?? 0}`;
      const dateCell = document.createElement('td');
      dateCell.textContent = formatDate(account.updated_at);
      const actionCell = document.createElement('td');
      const detail = document.createElement('button');
      detail.type = 'button'; detail.className = 'table-action'; detail.textContent = '详情';
      detail.addEventListener('click', () => openAccountDetail(account));
      const action = document.createElement('button');
      action.type = 'button';
      action.className = 'table-action';
      action.textContent = '打开隔离 Tab';
      action.addEventListener('click', () => openAccount(account));
      actionCell.append(detail, action);
      if (account.role !== 'viewer') {
        const edit = document.createElement('button');
        edit.type = 'button'; edit.className = 'table-action'; edit.textContent = '编辑';
        edit.addEventListener('click', () => openEditAccount(account));
        actionCell.append(edit);
      }
      global.saasConsoleOperations.appendAccountActions(actionCell, account);
      row.append(nameCell, labelsCell, revisionCell, dateCell, actionCell);
      elements.accountTableBody.append(row);
    }
    renderAccountSelection(visibleAccounts);
  }

  function getVisibleAccounts() {
    const query = String(elements.accountSearch?.value || '').trim().toLocaleLowerCase();
    const filter = elements.accountFilter?.value || 'all';
    return state.accounts.filter((account) => {
      const haystack = [account.name, account.account_id, ...(Array.isArray(account.labels) ? account.labels : [])]
        .filter(Boolean).join(' ').toLocaleLowerCase();
      const matchesQuery = !query || haystack.includes(query);
      const matchesFilter = filter === 'all' || (filter === 'editable' && account.role !== 'viewer') ||
        (filter === 'viewer' && account.role === 'viewer');
      return matchesQuery && matchesFilter;
    });
  }

  async function openAccountDetail(account) {
    const generation = state.sessionGeneration;
    const workspaceId = state.workspaceId;
    if (!state.accounts.some((value) => value.account_id === account.account_id && value.workspace_id === workspaceId)) {
      showToast('账号已从当前工作区移除，请刷新后重试', true);
      return;
    }
    elements.accountDetailTitle.textContent = account.name;
    elements.accountDetailSummary.textContent = `${account.account_id} · ${account.role === 'viewer' ? '只读账号' : '可编辑账号'}`;
    elements.accountDetailMeta.replaceChildren();
    const metadata = [
      ['稳定标识', account.account_id],
      ['账号角色', {owner: '所有者', admin: '管理员', editor: '编辑者', viewer: '查看者'}[account.role] || '成员'],
      ['云端修订', `r${account.revision ?? 0}`],
      ['最后更新', formatDate(account.updated_at)],
      ['标签', Array.isArray(account.labels) && account.labels.length ? account.labels.join('、') : '未设置'],
    ];
    for (const [label, value] of metadata) {
      const item = document.createElement('div'); item.className = 'detail-item';
      const title = document.createElement('span'); title.textContent = label;
      const content = document.createElement('strong'); content.textContent = value;
      item.append(title, content); elements.accountDetailMeta.append(item);
    }
    elements.accountRuntimeBadge.textContent = '读取中';
    elements.accountRuntimeStatus.textContent = state.bridge?.available ? '正在读取当前客户端的真实 Tab 状态…' : '当前页面未连接原生桥，无法读取本机运行状态。';
    elements.accountRuntimeCard.hidden = true;
    elements.accountRuntimeCard.replaceChildren();
    elements.accountDetailModal.showModal();
    if (!state.bridge?.available) {
      elements.accountRuntimeBadge.textContent = '未连接';
      return;
    }
    try {
      const tabs = await global.saasBridgeClient.tabs.list();
      if (state.sessionGeneration !== generation || state.workspaceId !== workspaceId || !elements.accountDetailModal.open) return;
      const matches = tabs.filter((tab) => tab.account_id === account.account_id);
      elements.accountRuntimeBadge.textContent = matches.length ? '运行中' : '未运行';
      elements.accountRuntimeStatus.textContent = matches.length ? `当前账号有 ${matches.length} 个运行中的原生 Tab。` : '当前账号没有已打开的原生 Tab。';
      for (const tab of matches) {
        const item = document.createElement('div'); item.className = 'runtime-item';
        const title = document.createElement('strong'); title.textContent = tab.title || tab.url || '空白页';
        const detail = document.createElement('span'); detail.textContent = `${tab.storage_partition_persistent ? '独立持久化分区' : '临时分区'} · ${tab.id}`;
        item.append(title, detail); elements.accountRuntimeCard.append(item);
      }
      elements.accountRuntimeCard.hidden = !matches.length;
    } catch (error) {
      if (state.sessionGeneration === generation && state.workspaceId === workspaceId && elements.accountDetailModal.open) {
        elements.accountRuntimeBadge.textContent = '读取失败';
        elements.accountRuntimeStatus.textContent = userMessage(error);
      }
    }
  }

  function renderAccountSelection(visibleAccounts = getVisibleAccounts()) {
    const total = visibleAccounts.length;
    const selected = state.selectedAccountIds.size;
    const toolbar = document.querySelector('#account-selection-toolbar');
    const selectAll = document.querySelector('#account-select-all');
    const count = document.querySelector('#account-selection-count');
    if (!toolbar || !selectAll || !count) return;
    toolbar.hidden = selected === 0;
    count.textContent = `已选择 ${selected} 个账号`;
    const visibleSelected = visibleAccounts.filter((account) => state.selectedAccountIds.has(account.account_id)).length;
    selectAll.checked = total > 0 && visibleSelected === total;
    selectAll.indeterminate = visibleSelected > 0 && visibleSelected < total;
  }

  function renderBridge() {
    const bridge = state.bridge || { available: false, capabilities: {} };
    const available = bridge.available === true;
    const capabilities = bridge.capabilities || {};
    elements.metricBridge.textContent = available ? '已连接' : '未连接';
    elements.metricBridgeMeta.textContent = available ? `桥接版本 ${bridge.version}` : '等待浏览器桥接';
    elements.bridgeMiniStatus.replaceChildren();
    const miniDot = document.createElement('span');
    miniDot.className = `status-dot ${available ? '' : 'is-muted'}`;
    elements.bridgeMiniStatus.append(miniDot, document.createTextNode(available ? '原生桥已连接' : '原生桥未连接'));
    elements.bridgeStatusDot.className = `status-dot ${available ? '' : 'is-muted'}`;
    elements.bridgeStatusTitle.textContent = available ? `已连接 · v${bridge.version}` : '未连接';
    elements.bridgeStatusDetail.textContent = available
      ? `当前页面来源：${bridge.origin || '客户端编译白名单'}。能力调用将由客户端逐次校验。`
      : '在普通浏览器中，账号目录仍可通过 SaaS API 管理；打开受支持的 Chromium 客户端后才会出现本地 Tab 能力。';
    elements.bridgeOriginPolicy.textContent = available ? (bridge.origin || '客户端编译白名单') : '由客户端编译配置';
    elements.capabilityList.replaceChildren();
    for (const [key, label] of global.saasBridgeClient.capabilityNames) {
      const item = document.createElement('div');
      item.className = 'capability-item';
      const name = document.createElement('span');
      name.textContent = label;
      const value = document.createElement('span');
      const enabled = available && capabilities[key] === true;
      value.className = `capability-state ${enabled ? 'is-ready' : ''}`;
      value.textContent = enabled ? '可用' : '不可用';
      item.append(name, value);
      elements.capabilityList.append(item);
    }
  }

  function renderNavigation() {
    document.querySelectorAll('.nav-item').forEach((button) => {
      const active = button.dataset.view === state.currentView;
      button.classList.toggle('is-active', active);
      if (active) button.setAttribute('aria-current', 'page');
      else button.removeAttribute('aria-current');
    });
    document.querySelector('#overview-content').hidden = state.currentView !== 'overview';
    document.querySelector('#tabs-content').hidden = state.currentView !== 'tabs';
    document.querySelector('#security-content').hidden = state.currentView !== 'security';
    document.querySelector('#members-content').hidden = state.currentView !== 'members';
    document.querySelector('#view-heading').textContent = {overview: '账号总览', tabs: '运行中 Tab', security: '设备与安全', members: '成员与权限'}[state.currentView];
  }

  function render() {
    renderSession();
    renderWorkspaces();
    renderAccounts();
    renderBridge();
    renderNavigation();
  }

  function openModal(modal, status) {
    setStatus(status, '');
    modal.showModal();
  }

  function openEditAccount(account) {
    if (account.role === 'viewer') { showToast('只读账号不能修改目录资料', true); return; }
    elements.accountEditForm.reset();
    elements.accountEditForm.elements.account_id.value = account.account_id;
    elements.accountEditForm.elements.name.value = account.name;
    elements.accountEditForm.elements.labels.value = Array.isArray(account.labels) ? account.labels.join(', ') : '';
    elements.accountEditStatus.textContent = '';
    elements.accountEditModal.showModal();
  }

  elements.loginForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(elements.loginForm);
    // 用户主动登录优先，不能让后台恢复请求在随后覆盖新的会话。
    sessionRecoveryAttempted = true;
    if (!state.session?.accessToken) writeSession(null);
    setStatus(elements.loginStatus, '正在建立安全会话…');
    try {
      await login(String(form.get('email') || '').trim(), String(form.get('password') || ''));
      await loadWorkspaces();
      await inspectBridge();
      elements.loginForm.reset();
      setStatus(elements.loginStatus, '');
      showToast('登录成功');
    } catch (error) {
      setStatus(elements.loginStatus, userMessage(error));
    }
    render();
  });

  elements.accountForm.addEventListener('submit', async (event) => {
    if (event.submitter?.value === 'cancel') return;
    event.preventDefault();
    const form = new FormData(elements.accountForm);
    const accountId = String(form.get('account_id') || '').trim();
    const name = String(form.get('name') || '').trim();
    const labels = String(form.get('labels') || '').split(',').map((item) => item.trim()).filter(Boolean);
    setStatus(elements.accountStatus, '正在创建账号目录…');
    try {
      await createAccount(accountId, name, labels);
      elements.accountModal.close();
      elements.accountForm.reset();
      showToast('账号目录已创建');
    } catch (error) {
      setStatus(elements.accountStatus, userMessage(error));
    }
  });

  elements.accountEditForm.addEventListener('submit', async (event) => {
    if (event.submitter?.value === 'cancel') return;
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const account = state.accounts.find((value) => value.account_id === String(form.get('account_id')));
    const scope = {generation: state.sessionGeneration, workspaceId: state.workspaceId};
    if (!account) { elements.accountEditStatus.textContent = '账号已从当前工作区移除，请刷新后重试'; return; }
    const name = String(form.get('name') || '').trim();
    const labels = String(form.get('labels') || '').split(',').map((item) => item.trim()).filter(Boolean);
    elements.accountEditStatus.textContent = '正在保存账号资料…';
    const submit = event.submitter; if (submit) submit.disabled = true;
    try {
      await updateAccount(account, name, labels, scope);
      elements.accountEditModal.close(); showToast('账号资料已保存');
    } catch (error) { elements.accountEditStatus.textContent = userMessage(error); }
    finally { if (submit) submit.disabled = false; }
  });

  elements.workspaceForm.addEventListener('submit', async (event) => {
    if (event.submitter?.value === 'cancel') return;
    event.preventDefault();
    const form = new FormData(elements.workspaceForm);
    setStatus(elements.workspaceStatus, '正在创建工作区…');
    try {
      await createWorkspace(String(form.get('name') || '').trim());
      elements.workspaceModal.close();
      elements.workspaceForm.reset();
      showToast('工作区已创建');
    } catch (error) {
      setStatus(elements.workspaceStatus, userMessage(error));
    }
  });

  elements.workspaceSelect.addEventListener('change', async (event) => {
    global.saasConsoleOperations.clear();
    global.saasConsoleAdministration.clear();
    elements.accountDetailModal.close();
    state.workspaceId = event.target.value;
    state.accounts = [];
    state.accountsLoadState = 'loading'; state.accountsLoadError = '';
    state.selectedAccountIds.clear();
    render();
    sessionStorage.setItem('fingerprint-saas.workspace-id.v1', state.workspaceId);
    try {
      await loadAccounts();
      if (state.currentView === 'members') await global.saasConsoleAdministration.loadMembers();
    } catch (error) {
      showToast(userMessage(error), true);
    }
  });

  document.querySelector('#new-account-button').addEventListener('click', () => openModal(elements.accountModal, elements.accountStatus));
  document.querySelector('#empty-new-account-button').addEventListener('click', () => openModal(elements.accountModal, elements.accountStatus));
  document.querySelector('#close-account-detail').addEventListener('click', () => elements.accountDetailModal.close());
  elements.accountDetailModal.addEventListener('close', () => {
    elements.accountRuntimeCard.replaceChildren();
    elements.accountRuntimeCard.hidden = true;
  });
  document.querySelector('#new-workspace-button').addEventListener('click', () => openModal(elements.workspaceModal, elements.workspaceStatus));
  document.querySelector('#logout-button').addEventListener('click', () => logout());
  document.querySelector('#refresh-button').addEventListener('click', async () => {
    if (!state.session) return;
    try {
      await loadWorkspaces();
      await inspectBridge();
      showToast('数据已刷新');
    } catch (error) {
      showToast(userMessage(error), true);
    }
  });
  document.querySelector('#account-select-all').addEventListener('change', (event) => {
    const visibleAccounts = getVisibleAccounts();
    if (event.currentTarget.checked) for (const account of visibleAccounts) state.selectedAccountIds.add(account.account_id);
    else for (const account of visibleAccounts) state.selectedAccountIds.delete(account.account_id);
    renderAccounts();
  });
  elements.accountSearch.addEventListener('input', () => renderAccounts());
  elements.accountFilter.addEventListener('change', () => renderAccounts());
  document.querySelector('#selected-sync-button').addEventListener('click', async () => {
    try { await global.saasConsoleOperations.runBatch(false, [...state.selectedAccountIds]); }
    catch (error) { showToast(userMessage(error), true); }
  });
  document.querySelector('#selected-restore-button').addEventListener('click', async () => {
    try { await global.saasConsoleOperations.runBatch(true, [...state.selectedAccountIds]); }
    catch (error) { showToast(userMessage(error), true); }
  });
  document.querySelectorAll('.nav-item').forEach((button) => button.addEventListener('click', () => {
    state.currentView = button.dataset.view;
    renderNavigation();
    global.saasConsoleOperations.loadView(state.currentView);
    if (state.currentView === 'members') global.saasConsoleAdministration.loadMembers();
  }));

  global.saasConsoleOperations.configure({request, bridge: global.saasBridgeClient,
    deviceId: getDeviceId, getState: () => state, refreshAccounts: loadAccounts,
    showToast, userMessage, logout});
  global.saasConsoleAdministration.configure({request, getState: () => state,
    refreshAccounts: loadAccounts, showToast, userMessage});
  global.addEventListener('saas-native-bridge-ready', () => inspectBridge().catch((error) => {
    showToast(userMessage(error), true);
  }));
  state.session = readSession();
  render();
  if (state.session) {
    const generation = state.sessionGeneration;
    Promise.all([loadWorkspaces(), inspectBridge()]).catch((error) => {
      if (state.sessionGeneration !== generation) return;
      if (error instanceof ApiError && error.status === 401) writeSession(null);
      showToast(userMessage(error), true);
      render();
    });
  } else {
    inspectBridge().catch((error) => showToast(userMessage(error), true));
  }
})(window);
