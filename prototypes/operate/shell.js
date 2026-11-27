const pref = {
  theme: "system",
  language: "auto",
  density: "normal",
  brand: "green",
};
let page = "overview",
  mobileOpen = false;
const systemTheme = matchMedia("(prefers-color-scheme: dark)");
function tr(zh, en) {
  return pref.language === "en" ||
    (pref.language === "auto" && !navigator.language.startsWith("zh"))
    ? en
    : zh;
}
const paths = {
  grid: "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z",
  list: "M4 6h16M4 12h16M4 18h16",
  search: "M20 20l-5-5 M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0",
  layers: "m3 8 9-5 9 5-9 5z M3 12l9 5 9-5 M3 16l9 5 9-5",
  activity: "M2 12h5l3-8 4 16 3-8h5",
  sliders: "M3 6h8m5 0h5M3 17h3m5 0h10 M11 3v6m-5 5v6",
  settings:
    "M10 3h4l.7 2.4 2 .9 2.3-.7 2 3.5-1.7 1.7v2.4l1.7 1.7-2 3.5-2.3-.7-2 .9L14 21h-4l-.7-2.4-2-.9-2.3.7-2-3.5 1.7-1.7v-2.4L3 9.1l2-3.5 2.3.7 2-.9z M15 12a3 3 0 1 1-6 0 3 3 0 1 1 6 0",
  box: "m3 7 9-5 9 5v10l-9 5-9-5z M3 7l9 5 9-5m-9 5v10",
  chevron: "m9 5 7 7-7 7",
  arrow: "M4 12h15m-6-6 6 6-6 6",
  close: "m6 6 12 12M18 6 6 18",
  filter: "M3 5h18l-7 8v6l-4 2v-8z",
  check: "m4 12 5 5L20 6",
  clock: "M21 12a9 9 0 1 1-18 0 9 9 0 1 1 18 0M12 6v6l4 2",
  globe:
    "M21 12a9 9 0 1 1-18 0 9 9 0 1 1 18 0M3 12h18M12 3q-8 9 0 18 8-9 0-18z",
  book: "M3 4h6q3 0 3 3 0-3 3-3h6v14h-6q-3 0-3 3 0-3-3-3H3z",
  key: "M13 11a5 5 0 1 0-3 3l2 2h3v3h4v-4z",
  undo: "M8 4 3 9l5 5 M4 9h10a6 6 0 0 1 0 12",
  shield: "m12 2 8 3v6q0 7-8 11-8-4-8-11V5z M8 12l3 3 5-6",
  refresh: "M20 8a8 8 0 1 0 .3 7M20 3v6h-6",
  menu: "M3 5h18M3 12h18M3 19h18",
  brand: "M3 6.5 7.7 18 12 8l4.3 10L21 6.5",
  lab: "M9 3h6m-5 0v6L4 19q-.3 2 2 2h12q2.3 0 2-2l-6-10V3M7 15h10",
};
function icon(name, size = 18) {
  return (
    '<svg aria-hidden="true" width="' +
    size +
    '" height="' +
    size +
    '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round"><path d="' +
    (paths[name] || paths.box) +
    '"/></svg>'
  );
}
const navGroups = [
  [
    ["监控", "Monitor"],
    [
      ["overview", "总览", "Overview", "grid"],
      ["traces", "调用追踪", "Traces", "activity"],
    ],
  ],
  [
    ["治理", "Manage"],
    [
      ["sources", "搜索源", "Sources", "layers"],
      ["capabilities", "工具与能力", "Capabilities", "box"],
      ["diagnostics", "诊断实验室", "Diagnostics", "lab"],
    ],
  ],
  [["系统", "System"], [["settings", "设置", "Settings", "sliders"]]],
];
const title = () =>
  page === "overview" ? tr("总览", "Overview") : tr("搜索源", "Sources");
const el = (id) => document.getElementById(id);
let refreshed = "";
function gotoPage(next) {
  if (!window.closeSourceInspector()) return false;
  page = next;
  mobileOpen = false;
  render();
  window.scrollTo(0, 0);
  return true;
}
function goSources(name) {
  if (!gotoPage("sources")) return;
  if (name) {
    el("cmd").focus();
    openSource(name);
  }
}
function render() {
  document.documentElement.dataset.theme =
    pref.theme === "system"
      ? systemTheme.matches
        ? "dark"
        : "light"
      : pref.theme;
  document.documentElement.dataset.brand = pref.brand;
  document.documentElement.dataset.density = pref.density;
  document.documentElement.lang = tr("zh-CN", "en");
  el("sidebar").classList.toggle("mobile", mobileOpen);
  el("sidebar").innerHTML =
    '<div class="brand"><span class="mark">' +
    icon("brand", 28) +
    "</span><div><strong>WebSearch</strong><small>" +
    tr("本地控制中心", "Local control center") +
    "</small></div></div>" +
    navGroups
      .map(
        ([g, items]) =>
          '<nav class="navgroup" aria-label="' +
          tr(...g) +
          '"><h2>' +
          tr(...g) +
          "</h2>" +
          items
            .map(
              ([id, z, e, i]) =>
                '<button data-page="' +
                id +
                '" ' +
                (page === id ? 'aria-current="page"' : "") +
                (["sources", "overview"].includes(id)
                  ? ""
                  : ' disabled title="' +
                    tr("后续阶段", "Future phase") +
                    '"') +
                ">" +
                icon(i, 17) +
                tr(z, e) +
                "</button>",
            )
            .join("") +
          "</nav>",
      )
      .join("") +
    '<div class="privacy">' +
    icon("shield", 20) +
    "<div>" +
    tr("仅本地 · 原型预览", "Local prototype") +
    "<small>WebSearch · Phase 0</small></div></div>";
  el("sidebar")
    .querySelectorAll("button:enabled")
    .forEach((b) => (b.onclick = () => gotoPage(b.dataset.page)));
  el("top").innerHTML =
    '<div class="context"><button id="mobileNav" class="round" aria-label="' +
    tr("导航菜单", "Navigation menu") +
    '">' +
    icon("menu") +
    '</button><span class="contextgroup">' +
    tr("工作区", "Workspace") +
    '　/</span><span class="current">' +
    title() +
    '</span></div><button id="cmd" class="searchpill">' +
    icon("search", 15) +
    ' Search <kbd>⌘ K</kbd></button><div class="actions"><span class="refreshtext" aria-live="polite">' +
    (refreshed || tr("样本快照", "Sample snapshot")) +
    '</span><button class="round" id="refresh" aria-label="' +
    tr("刷新模拟数据", "Refresh sample") +
    '">' +
    icon("refresh", 15) +
    '</button><button class="round" id="gear" popovertarget="prefs" aria-label="' +
    tr("偏好设置", "Preferences") +
    '">' +
    icon("settings", 17) +
    "</button></div>";
  el("mobileNav").onclick = () => {
    mobileOpen = !mobileOpen;
    render();
    if (mobileOpen) el("sidebar").querySelector("button:enabled").focus();
  };
  document.querySelector(".mobileveil")?.remove();
  if (mobileOpen) {
    let veil = document.createElement("button");
    veil.className = "mobileveil";
    veil.ariaLabel = tr("关闭导航", "Close navigation");
    veil.onclick = () => {
      mobileOpen = false;
      render();
      el("mobileNav").focus();
    };
    document.body.append(veil);
  }
  el("gear").onclick = event => {
    if (!window.closeSourceInspector()) event.preventDefault();
  };
  el("cmd").onclick = openPalette;
  el("refresh").onclick = () => {
    refreshed = tr("刚刚刷新", "Just refreshed");
    render();
    el("refresh").focus();
  };
  el("main").innerHTML =
    '<div class="headingline"><h1>' +
    title() +
    '</h1><span class="heading-meta"><i class="live"></i>' +
    tr("本地实例", "Local instance") +
    " · " +
    tr("模拟数据", "Sample data") +
    "</span></div>" +
    (page === "overview" ? renderOverview() : renderSources());
  if (page === "overview") wireOverview();
  else wireSources();
  renderDraftBar();
  renderPrefs();
}
const groups = [
  [
    "theme",
    "外观",
    "Appearance",
    [
      ["light", "浅色", "Light"],
      ["dark", "暗色", "Dark"],
      ["system", "系统", "System"],
    ],
  ],
  [
    "language",
    "语言",
    "Language",
    [
      ["auto", "自动", "Auto"],
      ["zh", "中文", "中文"],
      ["en", "English", "English"],
    ],
  ],
  [
    "density",
    "密度",
    "Density",
    [
      ["normal", "舒适", "Comfortable"],
      ["compact", "紧凑", "Compact"],
    ],
  ],
  [
    "brand",
    "品牌点缀",
    "Brand accent",
    [
      ["green", "松绿", "Pine"],
      ["blue", "雾蓝", "Blue"],
      ["mono", "石墨", "Graphite"],
    ],
  ],
];
function renderPrefs() {
  const box = el("prefs");
  box.innerHTML =
    "<h2>" +
    tr("偏好设置", "Preferences") +
    "</h2>" +
    groups
      .map(
        ([key, z, e, options]) =>
          '<fieldset data-pref="' +
          key +
          '"><legend>' +
          tr(z, e) +
          '</legend><div class="segments">' +
          options
            .map(
              ([v, a, b]) =>
                '<button data-value="' +
                v +
                '" aria-pressed="' +
                (pref[key] === v) +
                '">' +
                (key === "brand" ? '<i class="swatch ' + v + '"></i>' : "") +
                tr(a, b) +
                "</button>",
            )
            .join("") +
          "</div></fieldset>",
      )
      .join("");
  box.querySelectorAll("button").forEach(
    (button) =>
      (button.onclick = () => {
        let key = button.closest("fieldset").dataset.pref,
          v = button.dataset.value;
        pref[key] = v;
        refreshed = "";
        render();
        box
          .querySelector('[data-pref="' + key + '"] [data-value="' + v + '"]')
          .focus();
      }),
  );
}
document.addEventListener("keydown", event => {
  const prefs = el("prefs");
  if (event.key !== "Escape" || !prefs.matches(":popover-open")) return;
  event.preventDefault();
  prefs.hidePopover();
  el("gear").focus();
}, true);
el("prefs").addEventListener("toggle", (event) => {
  if (event.newState === "closed" && document.activeElement === document.body)
    el("gear").focus();
});
systemTheme.addEventListener("change", () => {
  if (pref.theme === "system")
    document.documentElement.dataset.theme = systemTheme.matches
      ? "dark"
      : "light";
});
function openPalette() {
  if (el("palette").open || !window.closeSourceInspector()) return;
  if (el("prefs").matches(":popover-open")) el("prefs").hidePopover();
  const d = el("palette");
  d.setAttribute("aria-label", tr("命令面板", "Command palette"));
  d.innerHTML =
    '<div class="queryrow">' +
    icon("search") +
    '<input aria-label="' +
    tr("搜索命令", "Search commands") +
    '" placeholder="' +
    tr("搜索页面或搜索源…", "Search pages or sources…") +
    '"><button aria-label="' +
    tr("关闭", "Close") +
    '"><kbd>Esc</kbd></button></div><div class="results"></div><div class="cmdfoot">↑ ↓ ' +
    tr("移动", "Move") +
    "　·　↵ " +
    tr("打开", "Open") +
    "</div>";
  let idx = 0;
  const q = d.querySelector("input"),
    res = d.querySelector(".results");
  const items = [
    [
      "overview",
      tr("总览", "Overview"),
      "grid",
      tr("工作区", "Workspace"),
      "总览 overview",
      () => gotoPage("overview"),
    ],
    [
      "sources",
      tr("搜索源", "Sources"),
      "layers",
      tr("工作区", "Workspace"),
      "搜索源 sources",
      () => gotoPage("sources"),
    ],
    ...(window.sourceNames || []).map((n) => [
      n,
      n,
      "globe",
      tr("搜索源", "Sources"),
      n,
      () => goSources(n),
    ]),
  ];
  function show() {
    let rows = items.filter((r) =>
      (r[1] + r[4]).toLowerCase().includes(q.value.toLowerCase().trim()),
    );
    idx = Math.max(0, Math.min(idx, rows.length - 1));
    let last = "";
    res.innerHTML =
      rows
        .map((r, i) => {
          let group = last === r[3] ? "" : "<h3>" + r[3] + "</h3>";
          last = r[3];
          return (
            group +
            '<button class="' +
            (i === idx ? "active" : "") +
            '" data-index="' +
            i +
            '">' +
            icon(r[2], 14) +
            r[1] +
            "</button>"
          );
        })
        .join("") ||
      '<p class="empty">' + tr("没有匹配项", "No matching commands") + "</p>";
    res.querySelectorAll("button").forEach(
      (b, i) =>
        (b.onclick = () => {
          d.close();
          rows[i][5]();
        }),
    );
    return rows;
  }
  q.oninput = () => {
    idx = 0;
    show();
  };
  d.onkeydown = (e) => {
    if (["ArrowDown", "ArrowUp"].includes(e.key)) {
      e.preventDefault();
      idx += e.key === "ArrowDown" ? 1 : -1;
      show();
      res.querySelector(".active")?.scrollIntoView({ block: "nearest" });
    }
    if (e.key === "Enter" && document.activeElement === q) {
      e.preventDefault();
      res.querySelector(".active")?.click();
    }
  };
  d.querySelector(".queryrow button").onclick = () => d.close();
  d.onclick = (e) => {
    if (e.target === d) d.close();
  };
  d.onclose = () => {
    if (!document.querySelector("dialog[open]")) el("cmd").focus();
  };
  show();
  d.showModal();
  q.focus();
}
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    if (!document.querySelector("dialog:modal")) openPalette();
  }
  if (e.key === "Escape" && mobileOpen) {
    mobileOpen = false;
    render();
    el("mobileNav").focus();
  }
});
render();




