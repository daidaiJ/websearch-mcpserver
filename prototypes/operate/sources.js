(() => {
  const sources = [
    { name: 'Tavily', group: 'api', state: 'observed', average: '820 ms', p95: '2.4 s', timeout: 8000, quota: [842, 1000], quotaOrigin: 'official', attempts: 76, failures: { rate: 3 }, recent: { 6: 'rate', 13: 'rate', 17: 'rate' } },
    { name: 'DuckDuckGo', group: 'web', state: 'cooldown', average: '690 ms', p95: '1.8 s', timeout: 6000, attempts: 48, failures: { rate: 3 }, recent: { 17: 'rate', 18: 'rate', 19: 'rate' } },
    { name: 'Bing', group: 'web', state: 'healthy', average: '380 ms', p95: '810 ms', timeout: 5000, attempts: 105, failures: { empty: 2 }, recent: { 8: 'empty' } },
    { name: 'Exa', group: 'api', state: 'healthy', average: '410 ms', p95: '980 ms', timeout: 6000, quota: [384, 1000], quotaOrigin: 'local', attempts: 69, failures: { timeout: 1, empty: 1 }, recent: { 10: 'timeout', 16: 'empty' } },
    { name: 'arXiv', group: 'academic', state: 'healthy', average: '560 ms', p95: '1.1 s', timeout: 10000, attempts: 53, failures: { timeout: 1 }, recent: { 9: 'timeout' } },
    { name: 'OpenAlex', group: 'academic', state: 'healthy', average: '320 ms', p95: '720 ms', timeout: 6000, attempts: 57, failures: {}, recent: {} },
    { name: 'Crossref', group: 'academic', state: 'severe', average: '470 ms', p95: '930 ms', timeout: 6000, attempts: 64, failures: { empty: 16, timeout: 7, upstream: 2 }, recent: { 1: 'empty', 5: 'timeout', 6: 'empty', 9: 'empty', 11: 'upstream', 13: 'empty', 15: 'timeout', 16: 'empty', 19: 'empty' } },
    { name: 'Keenable MCP', group: 'hub', state: 'healthy', average: '610 ms', p95: '1.3 s', timeout: 8000, attempts: 28, failures: { upstream: 1 }, recent: { 12: 'upstream' } },
    { name: 'Google', group: 'web', state: 'healthy', average: '—', p95: '—', timeout: 6000, enabled: false, attempts: 0, failures: {}, recent: null },
  ].map(source => ({ enabled: true, ...source, id: source.name.toLowerCase().replaceAll(' ', '-') }));

  const groups = {
    all: ['全部', 'All', 'layers'], web: ['网页', 'Web', 'globe'], api: ['API', 'API', 'key'],
    academic: ['学术', 'Academic', 'book'], hub: ['MCP Hub', 'MCP Hub', 'box'],
  };
  const states = {
    any: ['全部状态', 'All states'], healthy: ['正常', 'Healthy'], observed: ['观测告警', 'Observed warning'],
    cooldown: ['路由冷却', 'Routing cooldown'], severe: ['严重异常', 'Severe degradation'], disabled: ['已禁用', 'Disabled'],
  };
  const outcomes = { success: ['成功', 'Success'], rate: ['限流', 'Rate limit'], empty: ['无结果', 'No results'], timeout: ['超时', 'Timeout'], upstream: ['上游错误', 'Upstream error'], missing: ['无样本', 'No sample'] };
  const settings = { enabled: ['启用搜索源', 'Enable source'], timeout: ['请求超时', 'Request timeout'] };
  let cooldownEndsAt = Date.now() + 134000;
  const drafts = new Map();
  let group = 'all';
  let state = 'any';
  let view = 'cards';
  let keyword = '';
  let inspector = null;

  const label = entry => tr(entry[0], entry[1]);
  const remaining = () => Math.max(0, Math.ceil((cooldownEndsAt - Date.now()) / 1000));
  const stateOf = source => {
    if (!source.enabled) return 'disabled';
    if (source.state === 'cooldown' && remaining() === 0) return 'healthy';
    return source.state;
  };
  const sourceState = source => `<span class="sources-status" data-state="${stateOf(source)}"><span></span>${label(states[stateOf(source)])}</span>`;
  const observed = () => tr('今日出现 3 次限流，当前仍可参与路由。', 'Three rate limits today. Routing remains available.');
  const severe = () => tr('今日 25 次失败，其中 16 次无结果。该源仍可参与路由。', '25 failed attempts today, including 16 empty results. The source remains eligible for routing.');
  const cooldownText = () => {
    const minutes = Math.floor(remaining() / 60);
    const seconds = remaining() % 60;
    return tr(`${minutes} 分 ${seconds} 秒`, `${minutes}m ${seconds}s`);
  };
  const cooldownTime = () => `<span data-source-countdown>${cooldownText()}</span>`;
  const cooldown = () => tr(`收到 HTTP 429。当前已暂停路由，约 ${cooldownTime()}后恢复。`, `HTTP 429 paused routing. Expected to resume in ${cooldownTime()}.`);
  const stateDetail = source => ({
    observed, cooldown, severe, healthy: () => source.state === 'cooldown' ? tr('冷却已解除，当前可参与路由。', 'Cooldown ended. Routing is available.') : tr('当前可参与路由。', 'Currently available for routing.'),
    disabled: () => tr('当前配置已禁用此搜索源。', 'This source is disabled in the current configuration.'),
  })[stateOf(source)]();

  const recentOutcomes = source => Array.from({ length: 20 }, (_, index) => source.recent ? source.recent[index] ?? 'success' : 'missing');

  function recentHealth(source, detail = false) {
    const last = recentOutcomes(source);
    const descriptions = last.map((kind, index) => `${index + 1}. ${label(outcomes[kind])}`);
    return `<span class="sources-spark" ${detail ? 'data-source-detail' : ''} role="${detail ? 'list' : 'img'}" aria-label="${detail ? tr('最近 20 次调用，从最早至最近', 'Last 20 attempts, oldest to newest') : descriptions.join(', ')}">${last.map((kind, index) => `<span data-sample="${kind}" ${detail ? 'role="listitem"' : 'aria-hidden="true"'} title="${descriptions[index]}">${detail ? `<span class="sources-sr-only">${descriptions[index]}</span>` : ''}</span>`).join('')}</span>`;
  }

  const successRate = source => source.attempts ? `${(100 * (source.attempts - Object.values(source.failures).reduce((sum, count) => sum + count, 0)) / source.attempts).toFixed(1)}%` : '—';
  const failureTags = counts => Object.entries(counts).map(([kind, count]) => `<span class="sources-error-tag" data-outcome="${kind}">${label(outcomes[kind])} ×${count}</span>`).join('');
  function recentFailureTags(source) {
    const counts = {};
    for (const kind of recentOutcomes(source)) {
      if (kind !== 'success' && kind !== 'missing') counts[kind] = (counts[kind] ?? 0) + 1;
    }
    return failureTags(counts);
  }
  const effectiveEnabled = source => drafts.get(source.id)?.enabled ?? source.enabled;
  const sourceToggle = source => `<label class="sources-switch" title="${label(settings.enabled)} · ${source.name}"><input type="checkbox" role="switch" data-source-toggle="${source.id}" aria-label="${label(settings.enabled)} · ${source.name}" ${effectiveEnabled(source) ? 'checked' : ''}></label>`;

  function quota(source, inline = false) {
    if (!source.quota) return inline ? '<span aria-hidden="true" class="sources-quiet">—</span>' : '';
    const [used, limit] = source.quota;
    const name = source.quotaOrigin === 'official' ? tr('官方月额度', 'Official monthly quota') : tr('本地月额度', 'Local monthly quota');
    const shortName = source.quotaOrigin === 'official' ? tr('官方', 'Official') : tr('本地', 'Local');
    return `<span class="sources-quota${inline ? ' is-inline' : ''}">
      <span><span>${inline ? shortName : name}</span><span>${used} / ${limit.toLocaleString('en-US')}</span></span>
      <progress aria-label="${name}" value="${used}" max="${limit}"></progress>
    </span>`;
  }

  const identity = source => `<span class="sources-identity"><span class="sources-emblem">${icon(groups[source.group][2], 17)}</span><span><strong>${source.name}</strong><small>${label(groups[source.group])}</small></span></span>`;

  function card(source) {
    return `<article class="sources-card" data-source-entry>
      <div class="sources-card-top"><button class="sources-open" data-source="${source.id}">${identity(source)}</button>${sourceToggle(source)}</div>
      <div class="sources-card-status">${sourceState(source)}${stateOf(source) === 'cooldown' ? `<small>${cooldownTime()}</small>` : ''}</div>
      <div class="sources-recent"><small>${tr('最近 20 次', 'Last 20')}</small>${recentHealth(source)}</div>
      ${today(source)}
      <div class="sources-card-errors">${failureTags(source.failures)}</div>
      <div class="sources-card-bottom"><div class="sources-latency"><small>${tr('平均 / P95', 'Avg / P95')}</small><strong>${source.average} / ${source.p95}</strong></div>${quota(source, true)}</div>
    </article>`;
  }

  const today = source => `<span class="sources-today"><span><small>${tr('今日尝试', 'Attempts today')}</small><strong>${source.attempts}</strong></span><span><small>${tr('成功率', 'Success rate')}</small><strong>${successRate(source)}</strong></span></span>`;

  function row(source) {
    const countdown = stateOf(source) === 'cooldown' ? `<small class="sources-table-countdown">${cooldownTime()}</small>` : '';
    return `<tr data-source-entry><th scope="row"><button class="sources-open" data-source="${source.id}">${identity(source)}</button></th><td>${sourceState(source)}${countdown}</td><td>${recentHealth(source)}<div class="sources-table-errors">${failureTags(source.failures)}</div></td><td>${today(source)}</td><td>${source.average}<br>${source.p95}</td><td>${quota(source, true)}</td><td>${sourceToggle(source)}</td></tr>`;
  }

  window.sourceNames = sources.map(source => source.name);
  window.getOverviewRouting = () => {
    const enabledCount = sources.filter(source => source.enabled).length;
    const coolingCount = sources.filter(source => stateOf(source) === 'cooldown').length;
    return {
      enabledCount, coolingCount, routableCount: enabledCount - coolingCount,
      cooldownRemaining: remaining(),
      duckDuckGoState: stateOf(sources.find(source => source.id === 'duckduckgo')),
      tavilyState: stateOf(sources.find(source => source.id === 'tavily')),
      alerts: sources.filter(source => source.state !== 'healthy').map(source => ({
        name: source.name, state: stateOf(source), kind: source.state, blocking: stateOf(source) === 'cooldown',
        detail: {
          observed: tr('今日 3 次限流', '3 rate limits today'),
          severe: tr('今日 25 次失败 · 16 次无结果', '25 failed attempts today · 16 empty results'),
          cooldown: tr('HTTP 429 触发冷却', 'HTTP 429 triggered cooldown'),
        }[source.state],
      })),
    };
  };
  window.renderSources = () => {
    return `<section class="sources-workspace" aria-label="${tr('搜索源目录', 'Source directory')}">
      <div class="sources-controls">
        <div class="sources-pills" role="group" aria-label="${tr('搜索源分类', 'Source categories')}">
          ${Object.entries(groups).map(([key, entry]) => `<button data-source-group="${key}" aria-pressed="${group === key}">${label(entry)}</button>`).join('')}
        </div>
        <div class="sources-actions">
          <label class="sources-keyword">${icon('search', 15)}<input id="sources-keyword" type="search" aria-label="${tr('按关键字筛选', 'Filter by keyword')}" placeholder="${tr('查找搜索源', 'Find sources')}"></label>
          <label class="sources-select">${icon('filter', 15)}<select id="sources-state" aria-label="${tr('按状态筛选', 'Filter by state')}">${Object.entries(states).map(([key, entry]) => `<option value="${key}" ${state === key ? 'selected' : ''}>${label(entry)}</option>`).join('')}</select></label>
          <div class="sources-view" role="group" aria-label="${tr('显示方式', 'View mode')}">
            <button data-source-view="cards" aria-label="${tr('卡片视图', 'Card view')}" aria-pressed="${view === 'cards'}">${icon('grid', 17)}</button>
            <button data-source-view="list" aria-label="${tr('紧凑列表', 'Compact list')}" aria-pressed="${view === 'list'}">${icon('list', 18)}</button>
          </div>
        </div>
      </div>
      <div class="sources-results">${renderResults()}</div>
    </section>`;
  };

  function renderResults() {
    const visible = sources.filter(source => (group === 'all' || source.group === group) && (state === 'any' || stateOf(source) === state) && `${source.name} ${label(groups[source.group])}`.toLowerCase().includes(keyword.trim().toLowerCase()));
    return `<div class="sources-context"><span role="status">${tr(`${visible.length} 个搜索源`, `${visible.length} ${visible.length === 1 ? 'source' : 'sources'}`)}</span><span>${tr('延迟取近 24 小时', 'Latency over the last 24 hours')}</span></div>
      ${visible.length === 0 ? `<div class="sources-empty"><h2>${tr('没有符合条件的搜索源', 'No matching sources')}</h2><button data-source-reset>${tr('清除筛选', 'Clear filters')}</button></div>` : view === 'cards' ? `<div class="sources-grid">${visible.map(card).join('')}</div>` : `<div class="sources-table-wrap"><table class="sources-table"><thead><tr><th scope="col">${tr('搜索源', 'Source')}</th><th scope="col">${tr('状态', 'Status')}</th><th scope="col">${tr('最近 20 次', 'Last 20')}</th><th scope="col">${tr('今日', 'Today')}</th><th scope="col">${tr('平均 / P95', 'Avg / P95')}</th><th scope="col">${tr('本月额度', 'Monthly quota')}</th><th scope="col">${tr('启用', 'Enabled')}</th></tr></thead><tbody>${visible.map(row).join('')}</tbody></table></div>`}`;
  }

  function refreshResults() {
    const results = document.querySelector('.sources-results');
    if (!results) return;
    const active = document.activeElement;
    results.innerHTML = renderResults();
    if (active?.dataset.source) document.querySelector(`[data-source="${active.dataset.source}"]`)?.focus();
    if (active?.dataset.sourceToggle) document.querySelector(`[data-source-toggle="${active.dataset.sourceToggle}"]`)?.focus();
  }

  window.wireSources = () => {
    const workspace = document.querySelector('.sources-workspace');
    if (!workspace) return;
    const refresh = selector => { render(); document.querySelector(selector)?.focus(); };
    const keywordInput = workspace.querySelector('#sources-keyword');
    keywordInput.value = keyword;
    keywordInput.addEventListener('input', () => { keyword = keywordInput.value; refreshResults(); });
    workspace.querySelectorAll('[data-source-group]').forEach(button => button.addEventListener('click', () => {
      group = button.dataset.sourceGroup;
      refresh(`[data-source-group="${group}"]`);
    }));
    workspace.querySelector('#sources-state').addEventListener('change', event => {
      state = event.target.value;
      refresh('#sources-state');
    });
    workspace.querySelectorAll('[data-source-view]').forEach(button => button.addEventListener('click', () => {
      view = button.dataset.sourceView;
      refresh(`[data-source-view="${view}"]`);
    }));
    workspace.addEventListener('click', event => {
      if (event.target.closest('[data-source-reset]')) {
        group = 'all';
        state = 'any';
        keyword = '';
        refresh('[data-source-group="all"]');
        return;
      }
      const button = event.target.closest('[data-source-entry]')?.querySelector('[data-source]');
      if (button && !event.target.closest('.sources-switch')) openSource(button.dataset.source);
    });
    workspace.addEventListener('change', event => {
      const toggle = event.target.closest('[data-source-toggle]');
      if (!toggle) return;
      const source = sources.find(entry => entry.id === toggle.dataset.sourceToggle);
      setDraft(source, 'enabled', toggle.checked);
    });
  };

  function showDialog(markup) {
    const previous = document.activeElement;
    const dialog = document.createElement('dialog');
    dialog.className = 'sources-dialog sources-confirm';
    dialog.innerHTML = markup;
    dialog.setAttribute('aria-labelledby', dialog.querySelector('h2').id);
    document.querySelector('#overlay').append(dialog);
    dialog.addEventListener('click', event => {
      if (event.target === dialog || event.target.closest('[data-source-close]')) dialog.close();
    });
    dialog.addEventListener('close', () => {
      dialog.remove();
      if (previous?.isConnected && !document.querySelector('.sources-confirm[open]')) previous.focus();
      else if (document.activeElement === document.body && !document.querySelector('.sources-dialog[open]')) {
        const element = previous?.dataset.source ? document.querySelector(`[data-source="${previous.dataset.source}"]`) : document.querySelector('[data-draft-review]');
        (element ?? document.querySelector('[data-source-group][aria-pressed="true"], [data-page][aria-current="page"]'))?.focus();
      }
    });
    dialog.showModal();
    return dialog;
  }

  const dialogClose = () => `<button type="button" data-source-close class="sources-close" autofocus aria-label="${tr('关闭', 'Close')}">${icon('close', 18)}</button>`;

  function setDraft(source, field, value) {
    const changes = { ...drafts.get(source.id) };
    if (value === source[field]) delete changes[field];
    else changes[field] = value;
    if (Object.keys(changes).length) drafts.set(source.id, changes);
    else drafts.delete(source.id);
    if (field === 'enabled') {
      document.querySelectorAll(`[data-source-toggle="${source.id}"]`).forEach(toggle => { toggle.checked = value; });
      if (inspector?.source === source) inspector.dialog.querySelector('[name="enabled"]').checked = value;
    }
    renderDraftBar();
  }

  function removeInspector(restoreFocus = true) {
    if (!inspector) return;
    const { source, dialog } = inspector;
    inspector = null;
    dialog.close();
    dialog.remove();
    if (restoreFocus) (document.querySelector(`[data-source="${source.id}"]`) ?? document.querySelector('#sources-keyword') ?? document.querySelector('#cmd'))?.focus();
  }

  function releaseInspector(onDiscard) {
    if (!inspector) return true;
    if (document.querySelector('.sources-confirm[open]')) return false;
    if (inspector.dialog.querySelector('[name="timeout"]').validity.valid) {
      removeInspector(!onDiscard);
      return true;
    }
    confirmAction('sources-discard', tr('放弃无效的超时输入？', 'Discard invalid timeout?'), tr('已加入草稿的有效设置会保留。', 'Valid staged changes will be kept.'), tr('放弃无效输入', 'Discard invalid input'), () => {
      removeInspector(!onDiscard);
      onDiscard?.();
    });
    return false;
  }

  window.closeSourceInspector = () => releaseInspector();

  window.openSource = name => {
    const source = sources.find(candidate => candidate.name === name || candidate.id === name);
    if (!source) return;
    if (inspector?.source === source) {
      inspector.dialog.querySelector('[data-source-close]').focus();
      return;
    }
    if (releaseInspector(() => openSource(name))) mountInspector(source);
  };

  const runtimeStatus = source => `<div data-source-runtime-status>${sourceState(source)}<p>${stateDetail(source)}</p></div>`;
  const runtimeActions = source => `<div class="sources-runtime-actions" data-source-runtime-actions>
    ${stateOf(source) === 'cooldown' ? `<button type="button" data-source-cooldown-clear>${tr('解除冷却', 'Clear cooldown')}</button>` : ''}
    ${source.group === 'api' ? `<button type="button" data-source-quota-refresh>${tr('刷新官方额度', 'Refresh official quota')}</button>` : ''}
    <button type="button" data-source-diagnose>${tr('去诊断试搜', 'Open search diagnostic')}${icon('arrow', 14)}</button>
  </div>`;

  function mountInspector(source) {
    const pending = drafts.get(source.id) ?? {};
    const dialog = document.createElement('dialog');
    dialog.className = 'sources-dialog sources-drawer';
    dialog.setAttribute('aria-labelledby', 'sources-inspector-title');
    dialog.dataset.sourceInspector = source.id;
    dialog.innerHTML = `<form class="sources-dialog-body">
      <header class="sources-dialog-header"><div><h2 id="sources-inspector-title">${source.name}</h2><p>${label(groups[source.group])}</p></div>${dialogClose()}</header>
      <div class="sources-drawer-content">
        <section aria-label="${tr('当前路由状态', 'Current routing state')}">${runtimeStatus(source)}${runtimeActions(source)}
          <p class="sources-operation-status" role="status" hidden></p>
          <dl class="sources-drawer-metrics"><div><dt>${tr('平均延迟 · 24h', 'Avg latency · 24h')}</dt><dd>${source.average}</dd></div><div><dt>P95 · 24h</dt><dd>${source.p95}</dd></div></dl><div data-source-quota>${quota(source)}</div>
        </section>
        <section class="sources-drawer-history"><h3>${tr('最近 20 次调用', 'Last 20 attempts')}</h3>${recentHealth(source, true)}<p>${tr('从早到晚排列。悬停可查看单次结果。', 'Oldest to newest. Hover to inspect each result.')}</p>
          <div class="sources-breakdown"><h4>${tr('近 20 次失败', 'Failures in last 20')}</h4>${recentFailureTags(source) || `<span>${source.recent ? tr('无失败', 'No failures') : tr('无样本', 'No samples')}</span>`}</div>
          ${today(source)}<div class="sources-breakdown"><h4>${tr('今日失败构成', 'Failures today')}</h4>${failureTags(source.failures) || `<span>${source.attempts ? tr('今日无失败', 'No failures today') : tr('今日无调用', 'No attempts today')}</span>`}</div></section>
        <section class="sources-parameters"><h3>${tr('路由参数', 'Routing settings')}</h3>
          <p>${tr('有效改动自动加入草稿，保存后生效。', 'Valid changes are staged automatically and apply after saving.')}</p>
          <label class="sources-setting"><strong>${label(settings.enabled)}</strong><span class="sources-switch"><input type="checkbox" role="switch" name="enabled" ${effectiveEnabled(source) ? 'checked' : ''}></span></label>
          <label class="sources-setting"><span><strong>${label(settings.timeout)}</strong><small>${tr('允许 1,000 至 60,000 毫秒', '1,000–60,000 milliseconds')}</small></span><span class="sources-number"><input type="number" name="timeout" required min="1000" max="60000" step="1" value="${pending.timeout ?? source.timeout}" aria-describedby="sources-timeout-error"><span aria-hidden="true">ms</span></span></label>
          <p id="sources-timeout-error" role="status" hidden>${tr('请输入 1,000 至 60,000 的整数。草稿保留上一个有效值。', 'Enter a whole number from 1,000 to 60,000. The draft keeps the last valid value.')}</p>
        </section>
      </div>
    </form>`;
    inspector = { dialog, source };
    const form = dialog.querySelector('form');
    const { enabled, timeout } = form.elements;
    const applyTimeout = () => {
      const valid = timeout.validity.valid;
      timeout.setAttribute('aria-invalid', String(!valid));
      dialog.querySelector('#sources-timeout-error').hidden = valid;
      if (valid) setDraft(source, 'timeout', timeout.valueAsNumber);
    };
    form.addEventListener('submit', event => { event.preventDefault(); applyTimeout(); });
    enabled.addEventListener('change', () => setDraft(source, 'enabled', enabled.checked));
    timeout.addEventListener('input', applyTimeout);
    dialog.addEventListener('click', event => {
      if (event.target.closest('[data-source-close]')) releaseInspector();
      if (event.target.closest('[data-source-cooldown-clear]')) confirmAction('sources-cooldown-confirm', tr('解除 DuckDuckGo 冷却？', 'Clear DuckDuckGo cooldown?'), tr('模拟恢复路由。近期 429 记录仍保留。', 'Simulate resuming routing. Recent 429 records remain visible.'), tr('确认解除', 'Clear cooldown'), () => {
        cooldownEndsAt = Date.now();
        updateCooldown();
        dialog.querySelector('[data-source-diagnose]')?.focus();
      });
      if (event.target.closest('[data-source-quota-refresh]')) refreshQuota(source, dialog);
      if (event.target.closest('[data-source-diagnose]')) openDiagnostic(source);
    });
    document.querySelector('#overlay').append(dialog);
    dialog.show();
    dialog.querySelector('[data-source-close]').focus();
  }

  function confirmAction(id, title, message, action, callback) {
    const dialog = showDialog(`<div class="sources-dialog-body"><header class="sources-dialog-header"><h2 id="${id}">${title}</h2>${dialogClose()}</header>
      <div class="sources-draft-content"><p>${message}</p></div><footer class="sources-dialog-footer"><button data-source-close>${tr('取消', 'Cancel')}</button><button class="sources-danger" data-run-confirm>${action}</button></footer></div>`);
    dialog.querySelector('[data-run-confirm]').addEventListener('click', () => { dialog.close(); callback(); });
  }

  function showRuntimeMessage(dialog, message) {
    const status = dialog.querySelector('.sources-operation-status');
    status.hidden = false;
    status.textContent = message;
  }

  function refreshQuota(source, dialog) {
    if (source.quotaOrigin !== 'official') {
      showRuntimeMessage(dialog, tr('模拟刷新完成：该源只提供本地计数。', 'Simulated refresh complete. This source provides local counts only.'));
      return;
    }
    source.quota = [844, source.quota[1]];
    dialog.querySelector('[data-source-quota]').innerHTML = quota(source);
    refreshResults();
    showRuntimeMessage(dialog, tr('官方额度已模拟刷新：844 / 1,000。', 'Official quota refresh simulated: 844 / 1,000.'));
  }

  function openDiagnostic(source) {
    const dialog = showDialog(`<form class="sources-dialog-body"><header class="sources-dialog-header"><h2 id="sources-diagnostic-title">${tr('诊断试搜', 'Search diagnostic')} · ${source.name}</h2>${dialogClose()}</header>
      <div class="sources-draft-content"><p>${tr('使用本地样本演示单源调用。输入只用于当前弹窗。', 'Run a sample source request locally. Input is used only in this dialog.')}</p>
      <label class="sources-query">${tr('试搜关键词', 'Test query')}<input data-sample-query type="search" required maxlength="120" value="WebSearch"></label>
      <p data-sample-result role="status" hidden></p></div>
      <footer class="sources-dialog-footer"><button type="button" data-source-close>${tr('关闭', 'Close')}</button><button class="sources-primary" type="submit" data-sample-run>${tr('运行试搜', 'Run search test')}</button></footer></form>`);
    const query = dialog.querySelector('[data-sample-query]');
    query.focus();
    dialog.querySelector('form').addEventListener('submit', event => {
      event.preventDefault();
      if (!query.value.trim()) {
        query.setCustomValidity(tr('请输入试搜关键词。', 'Enter a test query.'));
        query.reportValidity();
        return;
      }
      const result = dialog.querySelector('[data-sample-result]');
      const completeTest = () => {
        result.hidden = false;
        result.textContent = tr('模拟执行完成 · 返回 3 条样本结果。', 'Simulation complete · 3 sample results returned.');
      };
      if (source.group === 'api') {
        confirmAction('sources-billing-title', tr('确认试搜？', 'Run the test search?'), tr('正式服务试搜可能消耗供应商额度或产生费用。本次原型只执行本地模拟。', 'A live test may use provider quota or incur a charge. This prototype runs a local simulation.'), tr('确认模拟试搜', 'Run simulated search'), completeTest);
      } else completeTest();
    });
    query.addEventListener('input', () => query.setCustomValidity(''));
  }

  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape' || !inspector || document.querySelector('.sources-confirm[open], #palette[open], #prefs:popover-open')) return;
    event.preventDefault();
    releaseInspector();
  });

  function updateCooldown() {
    document.querySelectorAll('[data-source-countdown]').forEach(element => { element.textContent = cooldownText(); });
    document.dispatchEvent(new Event('sources:cooldown-tick'));
    if (remaining() > 0) return;
    clearInterval(tick);
    if (inspector?.source.id === 'duckduckgo') {
      const { dialog, source } = inspector;
      dialog.querySelector('[data-source-runtime-status]').outerHTML = runtimeStatus(source);
      const clear = dialog.querySelector('[data-source-cooldown-clear]');
      if (clear === document.activeElement) dialog.querySelector('[data-source-diagnose]').focus();
      clear?.remove();
    }
    refreshResults();
    document.dispatchEvent(new Event('sources:cooldown-expired'));
  }
  const tick = setInterval(updateCooldown, 1000);

  const formatValue = (key, value) => key === 'enabled' ? value ? tr('启用', 'Enabled') : tr('禁用', 'Disabled') : `${value.toLocaleString('en-US')} ms`;

  function openDraft(confirmSave = false) {
    const changes = sources.filter(source => drafts.has(source.id)).map(source => `<li><h3>${source.name}</h3>${Object.entries(drafts.get(source.id)).map(([key, value]) => `<div class="sources-diff-row"><span>${label(settings[key])}</span><span><del>${formatValue(key, source[key])}</del>${icon('arrow', 14)}<ins>${formatValue(key, value)}</ins></span></div>`).join('')}</li>`).join('');
    const dialog = showDialog(`<div class="sources-dialog-body"><header class="sources-dialog-header"><h2 id="${confirmSave ? 'sources-save-title' : 'sources-review-title'}">${confirmSave ? tr('保存并重启服务？', 'Save and restart the service?') : tr('检查待保存变更', 'Review pending changes')}</h2>${dialogClose()}</header>
      <div class="sources-draft-content">${confirmSave ? `<p>${tr('此操作仅演示保存和重启，配置变更只在当前页面会话中生效。', 'This simulates saving and restarting. Changes apply only in this page session.')}</p>` : ''}<ul>${changes}</ul></div>
      <footer class="sources-dialog-footer"><button data-source-close>${tr('取消', 'Cancel')}</button><button class="${confirmSave ? 'sources-danger' : 'sources-primary'}" data-draft-confirm>${confirmSave ? tr('确认模拟重启', 'Confirm simulated restart') : tr('保存并重启', 'Save and restart')}</button></footer></div>`);
    dialog.querySelector('[data-draft-confirm]').addEventListener('click', () => {
      dialog.close();
      if (!confirmSave) {
        document.querySelector('[data-draft-review]')?.focus();
        openDraft(true);
        return;
      }
      for (const source of sources) if (drafts.has(source.id)) Object.assign(source, drafts.get(source.id));
      drafts.clear();
      if (inspector) {
        const { dialog, source } = inspector;
        dialog.querySelector('[data-source-runtime-status]').outerHTML = runtimeStatus(source);
        dialog.querySelector('[data-source-runtime-actions]').outerHTML = runtimeActions(source);
      }
      render();
      renderDraftBar(tr('已模拟保存并重启', 'Save and restart simulated'));
    });
  }

  window.renderDraftBar = (notice = '') => {
    const host = document.querySelector('#draft');
    if (!host) return;
    const count = [...drafts.values()].reduce((sum, values) => sum + Object.keys(values).length, 0);
    host.innerHTML = count ? `<div class="sources-draftbar"><span class="sources-draft-count" role="status">${icon('sliders', 17)}${tr(`${count} 项待保存`, `${count} pending ${count === 1 ? 'change' : 'changes'}`)}</span><div><button data-draft-undo>${tr('撤销', 'Undo')}</button><button data-draft-review>${tr('查看差异', 'Review diff')}</button><button class="sources-primary" data-draft-save>${tr('保存并重启', 'Save &amp; restart')}</button></div></div>` : notice ? `<div class="sources-draftbar sources-notice" role="status">${icon('check', 17)}${notice}</div>` : '';
    host.querySelector('[data-draft-undo]')?.addEventListener('click', () => {
      drafts.clear();
      if (inspector) {
        const { source, dialog } = inspector;
        const { enabled, timeout } = dialog.querySelector('form').elements;
        enabled.checked = source.enabled;
        if (timeout.validity.valid) timeout.value = source.timeout;
      }
      refreshResults();
      renderDraftBar(tr('已撤销待保存变更', 'Pending changes undone'));
      (document.querySelector('[data-source-group][aria-pressed="true"]') ?? document.querySelector('[data-page][aria-current="page"]'))?.focus();
    });
    host.querySelector('[data-draft-review]')?.addEventListener('click', () => openDraft());
    host.querySelector('[data-draft-save]')?.addEventListener('click', () => openDraft(true));
    if (notice) {
      const message = host.querySelector('.sources-notice');
      setTimeout(() => message?.remove(), 4000);
    }
  };
})();
