(function installAdministration(global) {
  'use strict';
  let context, activeAccount = null;
  let inviteGeneration = 0;
  const find = (id) => document.getElementById(id);
  const roles = {owner: '所有者', admin: '管理员', editor: '编辑者', viewer: '查看者'};
  const accountPath = (id) => '/api/v1/accounts/' + encodeURIComponent(id);
  const workspacePath = (id) => '/api/v1/workspaces/' + encodeURIComponent(id);
  function scope() {
    const state = context.getState();
    if (!state.session?.user || !state.workspaceId) throw new Error('请先登录并选择工作区');
    return {workspaceId: state.workspaceId, userId: state.session.user.user_id, generation: state.sessionGeneration,
      role: state.workspaces.find((item) => item.workspace_id === state.workspaceId)?.role};
  }
  function checkScope(original) {
    const current = scope();
    if (current.workspaceId !== original.workspaceId || current.userId !== original.userId || current.generation !== original.generation) {
      throw new Error('工作区或登录会话已变化，请重新操作');
    }
  }
  const isManager = (value) => ['owner', 'admin'].includes(value.role);
  function button(label, action, statusId) {
    const value = document.createElement('button');
    value.type = 'button'; value.className = 'table-action'; value.textContent = label;
    value.addEventListener('click', async () => {
      value.disabled = true;
      try { await action(); }
      catch (error) {
        const message = context.userMessage(error);
        if (statusId) find(statusId).textContent = message;
        context.showToast(message, true);
      } finally { value.disabled = false; }
    });
    return value;
  }
  function roleSelect(options, selected) {
    const value = document.createElement('select'); value.className = 'select-input'; value.setAttribute('aria-label', '成员角色');
    for (const role of options) value.add(new Option(roles[role], role, false, role === selected));
    return value;
  }
  async function loadMembers() {
    find('members-table-body').replaceChildren();
    find('members-table').setAttribute('aria-busy', 'true');
    find('members-status').textContent = '正在读取工作区成员…';
    try {
      const original = scope();
      const values = await context.request(workspacePath(original.workspaceId) + '/members');
      checkScope(original);
      find('invite-member-button').disabled = !isManager(original);
      find('members-copy').textContent = isManager(original)
        ? '所有者和管理员可以调整成员角色；移除成员会立即撤销其工作区访问。'
        : '当前账号只能查看成员列表，角色变更由工作区管理员执行。';
      if (!Array.isArray(values)) throw new Error('成员列表格式无效，请刷新后重试');
      if (!values.length) {
        find('members-status').textContent = '当前工作区暂无其他成员，可邀请成员加入协作。';
        return;
      }
      for (const member of values) {
        const row = document.createElement('tr'), name = document.createElement('td'), role = document.createElement('td'), actions = document.createElement('td');
        name.textContent = `${member.display_name || member.email} · ${member.email}`;
        const canManage = member.user_id !== original.userId && member.role !== 'owner' &&
          (original.role === 'owner' || (original.role === 'admin' && ['editor', 'viewer'].includes(member.role)));
        if (canManage) {
          const select = roleSelect(original.role === 'owner' ? ['admin', 'editor', 'viewer'] : ['editor', 'viewer'], member.role);
          role.append(select); actions.className = 'table-actions';
          const path = workspacePath(original.workspaceId) + '/members/' + encodeURIComponent(member.user_id);
          actions.append(button('保存角色', async () => {
            checkScope(original); await context.request(path, {method: 'PATCH', body: {role: select.value}}); await loadMembers();
          }, 'members-status'), button('移除成员', async () => {
            checkScope(original); await context.request(path, {method: 'DELETE'}); await loadMembers();
          }, 'members-status'));
        } else {
          const badge = document.createElement('span');
          badge.className = `role-badge role-${member.role || 'member'}`;
          badge.textContent = roles[member.role] || '成员';
          role.append(badge);
          actions.textContent = member.user_id === original.userId ? '当前账号' : '不可修改';
        }
        row.append(name, role, actions); find('members-table-body').append(row);
      }
      find('members-status').textContent = `共 ${values.length} 位成员。角色变更由服务端逐次校验。`;
    } catch (error) { find('members-status').textContent = context.userMessage(error); }
    finally { find('members-table').setAttribute('aria-busy', 'false'); }
  }
  function openInvite() {
    const original = scope();
    if (!isManager(original)) throw new Error('没有邀请成员的权限');
    inviteGeneration++;
    find('invite-form').reset(); find('invite-status').textContent = '';
    find('invite-role').querySelector('[value="admin"]').disabled = original.role !== 'owner';
    find('invite-modal').showModal();
  }
  async function openAccountAccess(account) {
    const original = scope();
    if (!isManager(original)) throw new Error('没有管理账号权限的权限');
    if (account.workspace_id !== original.workspaceId) throw new Error('账号不属于当前工作区');
    const members = await context.request(workspacePath(original.workspaceId) + '/members');
    checkScope(original);
    activeAccount = {account, original};
    find('account-access-title').textContent = `${account.name} · 访问权限`;
    find('account-access-user').replaceChildren();
    for (const member of members.filter((value) => !['owner', 'admin'].includes(value.role))) {
      find('account-access-user').add(new Option(`${member.email} · ${roles[member.role]}`, member.user_id));
    }
    find('account-access-status').textContent = '';
    await loadAccountAccess();
    if (!find('account-access-modal').open) find('account-access-modal').showModal();
  }
  async function loadAccountAccess() {
    const selected = activeAccount;
    if (!selected) return;
    checkScope(selected.original);
    find('account-access-status').textContent = '正在读取账号授权…';
    const result = await context.request(accountPath(selected.account.account_id) + '/members');
    checkScope(selected.original);
    if (selected !== activeAccount) return;
    find('account-access-scope').textContent = result.restricted
      ? '当前仅显式授权成员可访问。移除最后一位成员会恢复为工作区开放。'
      : '当前对工作区成员开放。添加第一位显式成员后，其他普通成员将无法访问。';
    find('account-access-list').replaceChildren();
    for (const member of result.members) {
      const row = document.createElement('div'), name = document.createElement('span'); row.className = 'admin-row';
      name.textContent = member.email;
      const select = roleSelect(['editor', 'viewer'], member.role);
      const path = accountPath(selected.account.account_id) + '/members/' + encodeURIComponent(member.user_id);
      row.append(name, select, button('保存', async () => {
        checkScope(selected.original); await context.request(path, {method: 'PATCH', body: {role: select.value}}); await loadAccountAccess();
      }, 'account-access-status'), button(result.members.length === 1 ? '恢复工作区开放' : '移除授权', async () => {
        checkScope(selected.original); await context.request(path, {method: 'DELETE'}); await loadAccountAccess();
      }, 'account-access-status'));
      find('account-access-list').append(row);
    }
    find('account-access-status').textContent = result.members.length ? `已加载 ${result.members.length} 位显式授权成员` : '当前没有显式授权成员';
  }
  async function openAudit(account) {
    const original = scope();
    const values = await context.request(accountPath(account.account_id) + '/audit-events');
    checkScope(original);
    const names = {account_created: '创建账号', account_updated: '更新账号资料', snapshot_read: '读取环境快照',
      snapshot_written: '同步环境快照', snapshot_overwritten: '明确覆盖云端快照', lease_acquired: '获取编辑租约',
      lease_released: '释放编辑租约', account_member_role_updated: '更新账号授权', account_member_removed: '移除账号授权'};
    find('audit-title').textContent = `${account.name} · 操作记录`;
    find('audit-list').replaceChildren();
    for (const value of values) {
      const row = document.createElement('div'); row.className = 'admin-row';
      row.textContent = `${new Date(value.created_at).toLocaleString('zh-CN')} · ${names[value.action] || '账号操作'} · ${value.device_id || '未记录设备'}`;
      find('audit-list').append(row);
    }
    find('audit-status').textContent = values.length ? '显示最近的账号操作记录，最多 200 条' : '暂无账号操作记录';
    find('audit-modal').showModal();
  }
  function configure(value) {
    context = value;
    find('invite-member-button').addEventListener('click', () => {
      try { openInvite(); } catch (error) { find('members-status').textContent = context.userMessage(error); }
    });
    find('accept-invite-button').addEventListener('click', () => {
      find('accept-invite-form').reset(); find('accept-invite-status').textContent = ''; find('accept-invite-modal').showModal();
    });
    find('invite-form').addEventListener('submit', async (event) => {
      if (event.submitter?.value === 'cancel') return;
      event.preventDefault(); const submit = event.submitter;
      if (submit?.disabled) return; if (submit) submit.disabled = true;
      const generation = inviteGeneration;
      try {
        const original = scope(), form = new FormData(event.currentTarget);
        const result = await context.request(workspacePath(original.workspaceId) + '/invitations', {
          method: 'POST', body: {email: String(form.get('email')).trim(), role: form.get('role')},
        });
        checkScope(original);
        if (find('invite-modal').open && inviteGeneration === generation) {
          find('invite-result').value = result.invite_token;
          find('invite-status').textContent = `邀请已创建，有效期至 ${new Date(result.expires_at).toLocaleString('zh-CN')}`;
        }
      } catch (error) { if (inviteGeneration === generation) find('invite-status').textContent = context.userMessage(error); }
      finally { if (submit) submit.disabled = false; }
    });
    find('accept-invite-form').addEventListener('submit', async (event) => {
      if (event.submitter?.value === 'cancel') return;
      event.preventDefault(); const submit = event.submitter;
      if (submit?.disabled) return; if (submit) submit.disabled = true;
      try {
        const form = new FormData(event.currentTarget);
        const result = await context.request('/api/v1/invitations/accept', {method: 'POST',
          body: {invite_token: String(form.get('invite_token')).trim(), password: form.get('password'), display_name: String(form.get('display_name')).trim()}}, false);
        find('accept-invite-form').reset(); find('accept-invite-modal').close();
        context.showToast(`已加入工作区，请使用 ${result.email} 登录并刷新目录`);
      } catch (error) { find('accept-invite-status').textContent = context.userMessage(error); }
      finally { if (submit) submit.disabled = false; }
    });
    find('account-access-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      try {
        const selected = activeAccount;
        if (!selected) throw new Error('请重新打开账号权限');
        checkScope(selected.original);
        const form = new FormData(event.currentTarget);
        await context.request(accountPath(selected.account.account_id) + '/members/' + encodeURIComponent(String(form.get('user_id'))), {
          method: 'PATCH', body: {role: form.get('role')},
        });
        await loadAccountAccess(); find('account-access-status').textContent = '账号授权已保存';
      } catch (error) { find('account-access-status').textContent = context.userMessage(error); }
    });
    find('close-account-access').addEventListener('click', () => find('account-access-modal').close());
    find('close-audit').addEventListener('click', () => find('audit-modal').close());
    for (const id of ['invite-modal', 'accept-invite-modal']) {
      find(id).addEventListener('close', () => {
        if (id === 'invite-modal') inviteGeneration++;
        find(id === 'invite-modal' ? 'invite-form' : 'accept-invite-form').reset();
      });
    }
    find('account-access-modal').addEventListener('close', () => { activeAccount = null; });
  }
  global.saasConsoleAdministration = {configure, loadMembers,
    clear() {
      activeAccount = null;
      for (const id of ['invite-modal', 'accept-invite-modal', 'account-access-modal', 'audit-modal']) find(id).close();
      find('invite-form').reset(); find('accept-invite-form').reset();
      for (const id of ['members-table-body', 'account-access-list', 'audit-list']) find(id).replaceChildren();
    },
    appendAccountActions(cell, account) {
      cell.append(button('记录', () => openAudit(account), 'operation-status'));
      const state = context.getState();
      const current = state.workspaces.find((item) => item.workspace_id === state.workspaceId);
      if (current && isManager(current)) cell.append(button('权限', () => openAccountAccess(account), 'operation-status'));
    },
  };
})(window);
