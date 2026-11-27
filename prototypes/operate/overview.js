(() => {
  // Day of November, successful calls, failed calls, cached successful calls.
  const data = [
    [13, 1247, 18, 257], [14, 1397, 22, 296], [15, 1087, 19, 226],
    [16, 981, 16, 191], [17, 1510, 31, 318], [18, 1410, 32, 274],
    [19, 1617, 19, 352], [20, 1151, 14, 243], [21, 1283, 19, 266],
    [22, 1150, 13, 218], [23, 1528, 23, 312], [24, 1392, 22, 295],
    [25, 1701, 29, 368], [26, 1461, 21, 292]
  ];
  const chartMaximum = 1800;
  const tools = [
    ['smartsearch', 1128],
    ['cleanfetch', 176],
    ['academicsearch', 146],
    ['pdf_parser', 12],
    ['file_search', 12],
    ['search_history', 8]
  ];
  const recentTotal = tools.reduce((sum, [, calls]) => sum + calls, 0);
  let attribution = 'client';
  const format = value => value.toLocaleString(tr('zh-CN', 'en-US'));
  const date = day => tr(`11 月 ${day} 日`, `Nov ${day}`);
  const total = ([, success, failures]) => success + failures;
  const series = () => [
    ['success', tr('成功', 'Success')],
    ['failure', tr('失败', 'Failure')],
    ['cache', tr('缓存命中（计入成功）', 'Cache hits (included in success)')]
  ];
  const arrow = '<svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8h9m-4-4 4 4-4 4"/></svg>';
  const warning = '<svg aria-hidden="true" width="17" height="17" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><path d="M8.6 3.8a1.6 1.6 0 0 1 2.8 0l5.6 10a1.55 1.55 0 0 1-1.4 2.3H4.4A1.55 1.55 0 0 1 3 13.8l5.6-10ZM10 7.5v3.4m0 2.4h.01"/></svg>';
  const cooldown = '<svg aria-hidden="true" width="17" height="17" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="6.6"/><path d="M10 6.6v3.7l2.1 1.2"/></svg>';
  const severe = '<svg aria-hidden="true" width="17" height="17" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.45" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="6.6"/><path d="m8 8 4 4m0-4-4 4"/></svg>';

  function routingDetail({ coolingCount }) {
    const label = coolingCount
      ? tr(`当前 · ${coolingCount} 个冷却中`, `Now · ${coolingCount} cooling down`)
      : tr('当前 · 无冷却', 'Now · no cooldowns');
    return `<span class="ov-routing-count" data-state="${coolingCount ? 'cooldown' : 'healthy'}"><i aria-hidden="true"></i>${label}</span>`;
  }

  function cooldownDetail(seconds) {
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return tr(`HTTP 429 · 剩余 ${minutes} 分 ${remainder} 秒`, `HTTP 429 · ${minutes}m ${remainder}s remaining`);
  }

  function statusRows(routing) {
    const kinds = {
      cooldown: { glyph: cooldown, label: tr('冷却记录', 'Past cooldown') },
      observed: { glyph: warning, label: tr('观测告警', 'Observed warning') },
      severe: { glyph: severe, label: tr('严重异常', 'Severe issue') }
    };
    const priority = { cooldown: 0, severe: 1, observed: 2, healthy: 3, disabled: 4 };
    return routing.alerts.toSorted((a, b) => priority[a.state] - priority[b.state]).map(({ name, state, kind, detail }) => {
      const { glyph, label } = kinds[kind];
      const cooling = state === 'cooldown';
      let status;
      if (state === 'disabled') status = tr(`${label} · 搜索源已禁用`, `${label} · source disabled`);
      else if (cooling) status = tr('冷却中 · 暂停路由', 'Cooling · routing paused');
      else if (kind === 'cooldown') status = tr('冷却已解除 · 当前可用', 'Cooldown cleared · currently routable');
      else status = tr(`${label} · 当前可用`, `${label} · currently routable`);
      return `<button type="button" class="ov-status-entry" data-overview-source="${name}" data-state="${state}">
        <span class="ov-status-icon">${glyph}</span>
        <span class="ov-status-copy"><span class="ov-status-name">${name}</span><span class="ov-status-state">${status}</span><span class="ov-status-description"${cooling ? ' data-cooldown-detail' : ''}>${cooling ? cooldownDetail(routing.cooldownRemaining) : detail}</span></span>
        <span class="ov-status-link">${arrow}</span>
      </button>`;
    }).join('');
  }

  function attributionRows() {
    const rows = attribution === 'client'
      ? [['Codex', 894], ['Claude Code', 421], ['WorkBuddy', 167]]
      : [['WebSearch', 948], [tr('文献检索', 'Literature search'), 383], [tr('未标注项目', 'Untagged project'), 151]];
    return rows.map(([name, calls]) => `<li><span>${name}</span><strong>${format(calls)}</strong><span class="ov-overview-share">${(100 * calls / recentTotal).toFixed(1)}%</span></li>`).join('');
  }

  function chart(days) {
    const keys = series();
    const x = index => 1000 * (index + 0.5) / days.length;
    const y = count => 200 * (1 - count / chartMaximum);
    const dateButtons = days.map((day, index) => {
      const counts = keys.map(([, name], key) => `${name} ${format(day[key + 1])}`);
      return `<button class="ov-date" type="button" data-chart-day="${index}" tabindex="${index === days.length - 1 ? 0 : -1}" aria-label="${[date(day[0]), ...counts, tr('合计', 'Total') + ' ' + format(total(day))].join(' · ')}"></button>`;
    }).join('');
    const paths = keys.map(([key], index) => {
      const points = days.map((day, i) => `${x(i)},${y(day[index + 1])}`).join(' ');
      return `<polyline class="ov-line ov-${key}" points="${points}"/>`;
    }).join('');
    const guideValues = [chartMaximum, chartMaximum * 2 / 3, chartMaximum / 3, 0];
    const guides = guideValues.map(value => `<line x1="0" x2="1000" y1="${y(value)}" y2="${y(value)}"/>`).join('');
    return `<div class="ov-chart-wrap" data-ov-chart>
      <div class="ov-chart-main">
        <div class="ov-axis-y" aria-hidden="true">${guideValues.map(value => `<span>${format(value)}</span>`).join('')}</div>
        <div class="ov-plot" style="--ov-days:${days.length}">
          <svg class="ov-chart-svg" viewBox="0 0 1000 200" preserveAspectRatio="none" aria-hidden="true">
            <g class="ov-chart-grid">${guides}</g>
            ${paths}
          </svg>
          <div class="ov-date-buttons" role="group" aria-label="${tr('每日调用量，左右方向键切换日期', 'Daily calls. Use Left and Right to browse dates.')}">${dateButtons}</div>
          <div class="ov-chart-markers" aria-hidden="true">${keys.map(([key]) => `<i class="ov-dot ov-${key}"></i>`).join('')}</div>
          <div class="ov-tooltip" hidden aria-hidden="true"></div>
        </div>
      </div>
      <div class="ov-axis-x" style="--ov-days:${days.length}" aria-hidden="true">${days.map(([day], index) => `<span${days.length > 7 && index % 2 === 0 ? ' class="ov-minor-date"' : ''}>11/${day}</span>`).join('')}</div>
    </div>`;
  }

  window.renderOverview = function () {
    const routing = getOverviewRouting();
    const metrics = [
      [tr('调用量', 'Calls'), format(recentTotal), tr('最近 24 小时', 'Last 24 hours'), ''],
      [tr('成功率', 'Success rate'), `${(100 * data.at(-1)[1] / recentTotal).toFixed(1)}%`, tr('最近 24 小时', 'Last 24 hours'), 'success'],
      [tr('P95 延迟', 'P95 latency'), '840 ms', tr('最近 24 小时', 'Last 24 hours'), ''],
      [tr('路由可用', 'Routable sources'), `${routing.routableCount} / ${routing.enabledCount}`, routingDetail(routing), 'routing']
    ];
    return `<div class="operate-overview">
      <dl class="ov-metrics">${metrics.map(([label, value, detail, tone]) => `<div class="ov-metric">
        <dt>${label}</dt><dd class="ov-metric-value${tone === 'success' ? ' ov-metric-success' : ''}"${tone === 'routing' ? ' data-overview-routable' : ''}>${value}</dd><dd class="ov-metric-detail"${tone === 'routing' ? ' data-overview-routing-detail' : ''}>${detail}</dd>
      </div>`).join('')}</dl>
      <div class="ov-body">
        <section class="ov-panel ov-trend" aria-labelledby="ov-trend-title">
          <header class="ov-panel-header">
            <div><h2 id="ov-trend-title">${tr('调用趋势', 'Traffic over time')}</h2><p>${tr('按日统计 · 调用次数', 'Daily · call count')}</p></div>
            <div class="ov-range" aria-label="${tr('时间范围', 'Date range')}">
              <button type="button" data-overview-range="7" aria-pressed="true">${tr('7 天', '7 days')}</button>
              <button type="button" data-overview-range="14" aria-pressed="false">${tr('14 天', '14 days')}</button>
            </div>
          </header>
          <div class="ov-legend">${series().map(([key, name]) => `<span><i class="ov-${key}"></i>${name}</span>`).join('')}</div>
          <div data-overview-chart-host>${chart(data.slice(-7))}</div>
        </section>
        <section class="ov-panel ov-status" aria-labelledby="ov-status-title">
          <header class="ov-panel-header">
            <div><h2 id="ov-status-title">${tr('异常与路由状态', 'Issues & routing')}</h2><p>${tr('当前路由与今日异常', 'Current routing and today’s issues')}</p></div>
          </header>
          <div class="ov-status-list">${statusRows(routing)}</div>
        </section>
      </div>
      <section class="ov-panel ov-tools" aria-labelledby="ov-tools-title">
        <header class="ov-panel-header">
          <div><h2 id="ov-tools-title">${tr('已注册工具', 'Registered tools')}</h2><p>${tr('6 个工具 · 最近 24 小时调用分布', '6 tools · call distribution over the past 24 hours')}</p></div>
        </header>
        <div class="ov-tool-grid">${tools.map(([name, calls]) => {
          const percent = 100 * calls / recentTotal;
          return `<div class="ov-tool">
            <div class="ov-tool-label"><code>${name}</code><strong>${format(calls)}</strong></div>
            <div class="ov-tool-bar" aria-hidden="true"><i style="width:${percent}%"></i></div>
            <div class="ov-tool-value">${percent.toFixed(1)}%</div>
          </div>`;
        }).join('')}</div>
      </section>
      <div class="ov-body ov-context">
        <section class="ov-panel" aria-labelledby="ov-usage-title">
          <header class="ov-panel-header">
            <div><h2 id="ov-usage-title">${tr('调用归属', 'Usage attribution')}</h2><p>${tr('最近 24 小时 · 调用量与占比', 'Last 24 hours · calls and share')}</p></div>
            <div class="ov-range" role="group" aria-label="${tr('归属维度', 'Attribution category')}">
              <button type="button" data-overview-attribution="client" aria-pressed="${attribution === 'client'}">${tr('应用', 'App')}</button>
              <button type="button" data-overview-attribution="project" aria-pressed="${attribution === 'project'}">${tr('项目', 'Project')}</button>
            </div>
          </header>
          <ul class="ov-context-list ov-attribution-list" data-overview-attribution-list>${attributionRows()}</ul>
        </section>
        <section class="ov-panel" aria-labelledby="ov-system-title">
          <header class="ov-panel-header">
            <div><h2 id="ov-system-title">${tr('服务状态', 'Service status')}</h2><p>${tr('本机子系统 · 样本快照', 'Local subsystems · sample snapshot')}</p></div>
          </header>
          <ul class="ov-context-list ov-system-list">
            <li><span>MCP HTTP</span><span><i></i>${tr('运行中', 'Running')}</span></li>
            <li><span>${tr('SQLite 遥测', 'SQLite telemetry')}</span><span><i></i>${tr('写入正常', 'Writing')}</span></li>
            <li><span>${tr('Everything 文件索引', 'Everything file index')}</span><span><i></i>${tr('已连接', 'Connected')}</span></li>
          </ul>
        </section>
      </div>
    </div>`;
  };

  function wireChart(host, days) {
    const plot = host.querySelector('.ov-plot');
    const markers = plot.querySelector('.ov-chart-markers');
    const tooltip = plot.querySelector('.ov-tooltip');
    const dots = Array.from(markers.children);
    const buttons = Array.from(plot.querySelectorAll('.ov-date'));
    const keys = series();
    const hide = () => {
      tooltip.hidden = true;
      markers.classList.remove('ov-markers-visible');
    };
    const show = index => {
      const day = days[index];
      plot.style.setProperty('--ov-crosshair', `${100 * (index + 0.5) / days.length}%`);
      dots.forEach((dot, i) => dot.style.top = `${100 * (1 - day[i + 1] / chartMaximum)}%`);
      tooltip.innerHTML = `<strong>${date(day[0])}</strong>${keys.map(([key, name], i) => `<div><span><i class="ov-${key}"></i>${name}</span><b>${format(day[i + 1])}</b></div>`).join('')}<div class="ov-tooltip-total"><span>${tr('合计', 'Total')}</span><b>${format(total(day))}</b></div>`;
      tooltip.hidden = false;
      markers.classList.add('ov-markers-visible');
      const maxLeft = plot.clientWidth - tooltip.offsetWidth - 3;
      const x = buttons[index].offsetLeft + buttons[index].offsetWidth / 2;
      const right = x + 12;
      const left = right <= maxLeft ? right : x - tooltip.offsetWidth - 12;
      tooltip.style.left = `${Math.max(3, Math.min(maxLeft, left))}px`;
    };
    buttons.forEach((button, index) => {
      button.addEventListener('pointerenter', () => show(index));
      button.addEventListener('focus', () => {
        buttons.forEach(item => { item.tabIndex = item === button ? 0 : -1; });
        show(index);
      });
      button.addEventListener('click', () => show(index));
      button.addEventListener('keydown', event => {
        const next = { ArrowLeft: index - 1, ArrowRight: index + 1, Home: 0, End: days.length - 1 }[event.key];
        if (event.key === 'Escape') {
          hide();
        } else if (next !== undefined) {
          event.preventDefault();
          buttons[Math.max(0, Math.min(days.length - 1, next))].focus();
        }
      });
    });
    plot.addEventListener('pointerleave', () => {
      const activeIndex = buttons.indexOf(document.activeElement);
      if (activeIndex === -1) hide();
      else show(activeIndex);
    });
    plot.addEventListener('focusout', event => {
      if (!plot.contains(event.relatedTarget)) hide();
    });
  }

  window.wireOverview = function () {
    const overview = document.querySelector('.operate-overview');
    const chartHost = overview.querySelector('[data-overview-chart-host]');
    const rangeButtons = overview.querySelectorAll('[data-overview-range]');
    wireChart(chartHost, data.slice(-7));
    rangeButtons.forEach(button => button.addEventListener('click', () => {
      if (button.getAttribute('aria-pressed') === 'true') return;
      rangeButtons.forEach(item => item.setAttribute('aria-pressed', String(item === button)));
      const days = data.slice(-Number(button.dataset.overviewRange));
      chartHost.innerHTML = chart(days);
      wireChart(chartHost, days);
    }));
    const attributionButtons = overview.querySelectorAll('[data-overview-attribution]');
    const attributionList = overview.querySelector('[data-overview-attribution-list]');
    attributionButtons.forEach(button => button.addEventListener('click', () => {
      if (button.dataset.overviewAttribution === attribution) return;
      attribution = button.dataset.overviewAttribution;
      attributionButtons.forEach(item => item.setAttribute('aria-pressed', String(item === button)));
      attributionList.innerHTML = attributionRows();
    }));
    overview.querySelector('.ov-status-list').addEventListener('click', event => {
      const button = event.target.closest('[data-overview-source]');
      if (button) goSources(button.dataset.overviewSource);
    });
  };

  document.addEventListener('sources:cooldown-tick', () => {
    const detail = document.querySelector('.operate-overview [data-cooldown-detail]');
    if (detail) detail.textContent = cooldownDetail(getOverviewRouting().cooldownRemaining);
  });

  document.addEventListener('sources:cooldown-expired', () => {
    const overview = document.querySelector('.operate-overview');
    if (!overview) return;
    const routing = getOverviewRouting();
    const list = overview.querySelector('.ov-status-list');
    const focusedSource = document.activeElement?.dataset.overviewSource;
    list.innerHTML = statusRows(routing);
    if (focusedSource) Array.from(list.children).find(button => button.dataset.overviewSource === focusedSource)?.focus();
    overview.querySelector('[data-overview-routable]').textContent = `${routing.routableCount} / ${routing.enabledCount}`;
    const detail = overview.querySelector('[data-overview-routing-detail]');
    detail.innerHTML = routingDetail(routing);
  });
})();
