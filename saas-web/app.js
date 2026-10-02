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
    bridge: null,
    currentView: 'overview',
  };
  let refreshFlight = null;

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
      const value = sessionStorage.getItem(sessionStorageKey);
      return value ? JSON.parse(value) : null;
    } catch (_error) {
      return null;
    }
  }

  function writeSession(session) {
    if (!session || !state.session || session.user?.user_id !== state.session.user?.user_id) state.sessionGeneration++;
    state.session = session;
    if (session) {
      sessionStorage.setItem(sessionStorageKey, JSON.stringify(session));
    } else {
      sessionStorage.removeItem(sessionStorageKey);
      state.workspaces = []; state.accounts = []; state.workspaceId = '';
      elements.loginForm.reset();
      global.saasConsoleOperations.clear();
      global.saasConsoleAdministration.clear();
      render();
    }
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
    if (refreshFlight) return refreshFlight;
    refreshFlight = performRefreshSession(state.session);
    try { return await refreshFlight; }
    finally { refreshFlight = null; }
  }

  async function performRefreshSession(originalSession) {
    try {
      const response = await fetch(`${apiBase}/api/v1/sessions/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'omit',
        body: JSON.stringify({ refresh_token: originalSession.refreshToken, device_id: getDeviceId() }),
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
      });
      return true;
    } catch (_error) {
      // 暂时断网时保留刷新凭据，恢复网络后可重试，不能把网络错误当成撤销会话。
      return false;
    }
  }

  async function login(email, password) {
    const payload = await request('/api/v1/sessions', {
      method: 'POST',
      body: {
        email,
        password,
        device_id: getDeviceId(),
        device_name: getDeviceName(),
      },
    }, false);
    writeSession({
      accessToken: payload.access_token,
      refreshToken: payload.refresh_token,
      user: payload.user,
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
      render();
      return;
    }
    const workspaceId = state.workspaceId;
    const accounts = [], seenTokens = new Set();
    let pageToken = '';
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
    render();
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

  async function logout() {
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
    elements.accountTableBody.replaceChildren();
    elements.accountEmpty.hidden = state.accounts.length > 0;
    elements.metricAccounts.textContent = String(state.accounts.length);
    const revisionTotal = state.accounts.reduce((total, account) => total + Number(account.revision || 0), 0);
    elements.metricRevisions.textContent = state.accounts.length ? String(revisionTotal) : '—';
    for (const account of state.accounts) {
      const row = document.createElement('tr');
      const nameCell = document.createElement('td');
      const nameWrap = document.createElement('div');
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
      nameCell.append(nameWrap);

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
      const action = document.createElement('button');
      action.type = 'button';
      action.className = 'table-action';
      action.textContent = '打开隔离 Tab';
      action.addEventListener('click', () => openAccount(account));
      actionCell.append(action);
      global.saasConsoleOperations.appendAccountActions(actionCell, account);
      row.append(nameCell, labelsCell, revisionCell, dateCell, actionCell);
      elements.accountTableBody.append(row);
    }
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
      button.classList.toggle('is-active', button.dataset.view === state.currentView);
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

  elements.loginForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(elements.loginForm);
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
    state.workspaceId = event.target.value;
    state.accounts = [];
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
  global.addEventListener('saas-native-bridge-ready', () => inspectBridge().catch(() => {
    showToast('原生桥状态更新失败，请重新加载控制台', true);
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
  }
})(window);
