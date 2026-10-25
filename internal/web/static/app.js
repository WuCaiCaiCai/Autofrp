/* ============================================================
   Autofrp WebUI
   ============================================================ */
"use strict";

const state = {
  data: null,
  role: "both",
  view: "overview",
  logTarget: "frps",
  logTimer: null,
  imported: null,
  wizardStep: 1,
};

const VIEW_META = {
  overview: { title: "总览", sub: "服务状态、网络环境与下一步操作" },
  frps: { title: "穿透服务端", sub: "frps 参数、端口白名单与穿透服务" },
  frpc: { title: "穿透客户端", sub: "连接远程 VPS 与本地隧道" },
  caddy: { title: "Caddy 反代", sub: "规则管理、证书策略与全局设置" },
  scanner: { title: "原服务迁移", sub: "探测 Nginx Proxy Manager / Nginx / 原版 Caddy 与 frp" },
  system: { title: "日志与设置", sub: "服务日志、面板安全与运行信息" },
};

const $ = (id) => document.getElementById(id);

/* ------------------------------------------------------------ theme */
function applyTheme(theme, persist = true) {
  document.documentElement.dataset.theme = theme;
  const btn = $("btnTheme");
  if (btn) {
    btn.innerHTML = `<svg class="i" width="17" height="17" aria-hidden="true"><use href="#i-${
      theme === "light" ? "sun" : "moon"
    }"/></svg>`;
    btn.setAttribute("aria-label", theme === "light" ? "切换到夜间模式" : "切换到日间模式");
    btn.title = theme === "light" ? "切换到夜间模式" : "切换到日间模式";
  }
  if (persist) localStorage.setItem("autof.theme", theme);
}

function toggleTheme() {
  applyTheme(document.documentElement.dataset.theme === "light" ? "dark" : "light");
}
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

function icon(name, size = 16) {
  return `<svg class="i" width="${size}" height="${size}" aria-hidden="true"><use href="#i-${name}"/></svg>`;
}

function esc(v) {
  if (v === null || v === undefined) return "";
  return String(v)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function pick(obj, path, fallback) {
  const val = path.split(".").reduce((acc, key) => (acc == null ? undefined : acc[key]), obj);
  return val === undefined || val === null ? fallback : val;
}

/* ------------------------------------------------------------ api */
class AuthError extends Error {}

async function api(url, options = {}) {
  const headers = Object.assign({ "Content-Type": "application/json" }, options.headers || {});
  const res = await fetch(url, Object.assign({}, options, { headers }));
  if (res.status === 401) {
    openLogin();
    throw new AuthError("未登录");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok || data.error) throw new Error(data.error || `请求失败（${res.status}）`);
  return data;
}

/* ------------------------------------------------------------ feedback */
function toast(message, type = "info") {
  const box = $("toasts");
  const el = document.createElement("div");
  el.className = `toast ${type}`;
  const ic = type === "ok" ? "check" : type === "err" ? "alert" : "info";
  el.innerHTML = `${icon(ic, 16)}<span>${esc(message)}</span>`;
  box.appendChild(el);
  setTimeout(() => {
    el.style.opacity = "0";
    el.style.transform = "translateX(12px)";
    setTimeout(() => el.remove(), 200);
  }, 4200);
}

async function withBusy(btn, task) {
  if (btn && btn.dataset.busy === "1") return undefined;
  if (btn) {
    btn.dataset.busy = "1";
    btn.classList.add("loading");
    btn.disabled = true;
  }
  try {
    return await task();
  } finally {
    if (btn) {
      btn.dataset.busy = "0";
      btn.classList.remove("loading");
      btn.disabled = false;
    }
  }
}

/* ------------------------------------------------------------ modal */
function openModal(id) {
  const el = $(id);
  if (!el) return;
  el.hidden = false;
  document.body.style.overflow = "hidden";
  setTimeout(() => {
    const focusable = el.querySelector("input:not([type=hidden]), select, textarea, button");
    if (focusable) focusable.focus({ preventScroll: true });
  }, 30);
}

function closeModal(id) {
  const el = $(id);
  if (!el) return;
  el.hidden = true;
  if (!$$(".modal-backdrop").some((m) => !m.hidden)) document.body.style.overflow = "";
}

function anyModalOpen() {
  return $$(".modal-backdrop").some((m) => !m.hidden);
}

let confirmResolver = null;

function confirmDialog({ title = "确认操作", text = "", okText = "确认", danger = true }) {
  $("confirmTitle").textContent = title;
  $("confirmText").textContent = text;
  $("confirmOk").textContent = okText;
  $("confirmOk").className = `btn ${danger ? "btn-danger" : "btn-primary"}`;
  openModal("confirmModal");
  return new Promise((resolve) => {
    confirmResolver = resolve;
  });
}

function settleConfirm(result) {
  closeModal("confirmModal");
  if (confirmResolver) {
    const fn = confirmResolver;
    confirmResolver = null;
    fn(result);
  }
}

/* ------------------------------------------------------------ clipboard */
async function copyText(text, label = "已复制到剪贴板") {
  try {
    await navigator.clipboard.writeText(text);
    toast(label, "ok");
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
      toast(label, "ok");
    } catch {
      toast("复制失败，请手动选择文本", "err");
    }
    ta.remove();
  }
}

/* ------------------------------------------------------------ login */
function openLogin() {
  openModal("loginModal");
  setTimeout(() => $("loginPass").focus(), 40);
}

async function submitLogin() {
  const username = $("loginUser").value.trim();
  const password = $("loginPass").value;
  try {
    await api("/api/auth/login", { method: "POST", body: JSON.stringify({ username, password }) });
    closeModal("loginModal");
    $("loginPass").value = "";
    toast("登录成功", "ok");
    await loadOverview();
  } catch (err) {
    if (!(err instanceof AuthError)) toast(err.message, "err");
  }
}

/* ------------------------------------------------------------ load */
async function loadOverview(btn) {
  try {
    const data = await withBusy(btn, () => api("/api/overview"));
    if (!data) return;
    state.data = data;
    render();
  } catch (err) {
    if (!(err instanceof AuthError)) toast(err.message, "err");
  }
}

/* ------------------------------------------------------------ render */
function render() {
  const d = state.data;
  if (!d) return;
  const cfg = d.config || {};
  state.role = cfg.nodeRole || "both";

  $("appVersion").textContent = `v${cfg.version || "1.0.2"}`;
  const sys = d.systemInfo || {};
  $("sidebarDistro").textContent = `${sys.prettyName || "Linux"} · ${sys.arch || ""}`.trim();
  $("sidebarStateDir").textContent = d.stateDir || "/etc/autof";

  renderBanners();
  renderRole();
  renderStats();
  renderNetwork();
  renderChecklist();
  renderFrps();
  renderFrpc();
  renderCaddy();
  renderScanner();
  renderSystem();
  renderPresets();

  const counts = {
    navFrpsCount: (pick(cfg, "frps.services", []) || []).length,
    navFrpcCount: (pick(cfg, "frpc.tunnels", []) || []).length,
    navCaddyCount: (pick(cfg, "caddy.rules", []) || []).filter((r) => r.enabled).length,
  };
  Object.entries(counts).forEach(([id, n]) => {
    const el = $(id);
    if (el) el.textContent = n;
  });

  const external = (d.detectedApps || []).filter((a) => !a.managedByAutof);
  const scannerCount = $("navScannerCount");
  scannerCount.textContent = external.length;
  scannerCount.classList.toggle("hidden", external.length === 0);
}

function renderBanners() {
  const d = state.data;
  const cfg = d.config || {};
  const items = [];

  const legacy = d.legacy || {};
  if (legacy.detected) {
    items.push({
      cls: "banner-info",
      icon: "box",
      text: `已从旧版 v1.x 继承配置：${legacy.serviceCount || 0} 条穿透规则${
        legacy.legacyPort ? `，控制端口 ${legacy.legacyPort}` : ""
      }。原文件保留于 ${esc(legacy.stateFile || "/etc/autof/state.conf")}。`,
      actions: [{ label: "查看详情", view: "scanner" }],
    });
  }

  const external = (d.detectedApps || []).filter((a) => !a.managedByAutof);
  if (external.length) {
    items.push({
      cls: "",
      icon: "alert",
      text: `检测到 ${external.length} 个原有反代/穿透服务：${esc(
        external.map((a) => a.name).join("、")
      )}`,
      actions: [{ label: "查看 / 吞并迁移", view: "scanner" }],
    });
  }

  if (cfg.setupCompleted === false) {
    items.push({
      cls: "banner-info",
      icon: "rocket",
      text: "尚未完成初始化，建议先选择部署角色与网络偏好。",
      actions: [{ label: "开始初始化", wizard: true }],
    });
  }

  const box = $("banners");
  box.innerHTML = items
    .map(
      (b, i) => `<div class="banner ${b.cls}">
        ${icon(b.icon, 17)}
        <div style="flex:1;min-width:200px">${b.text}</div>
        <div class="banner-actions">
          ${(b.actions || [])
            .map(
              (a, j) =>
                `<button class="btn btn-sm ${j === 0 ? "btn-primary" : ""}" data-banner="${i}:${j}">${esc(
                  a.label
                )}</button>`
            )
            .join("")}
        </div>
      </div>`
    )
    .join("");

  box.querySelectorAll("[data-banner]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const [i, j] = btn.dataset.banner.split(":").map(Number);
      const action = items[i].actions[j];
      if (action.wizard) openWizard();
      else if (action.view) switchView(action.view);
    });
  });
}

function renderRole() {
  $$(".segmented [data-role]").forEach((b) => {
    b.setAttribute("aria-pressed", String(b.dataset.role === state.role));
  });
  const modeTag = $("modeTag");
  if (modeTag) modeTag.classList.toggle("hidden", state.role !== "both");
  const cardFrps = $("statFrps");
  const cardFrpc = $("statFrpc");
  if (cardFrps) cardFrps.classList.toggle("hidden", state.role === "local");
  if (cardFrpc) cardFrpc.classList.toggle("hidden", state.role === "vps");
  $$(".nav-item[data-roles]").forEach((item) => {
    const roles = item.dataset.roles.split(" ");
    item.classList.toggle("hidden", !roles.includes(state.role));
  });
  $$("[data-quick]").forEach((b) => {
    const k = b.dataset.quick;
    b.classList.toggle("hidden", (k === "frps" && state.role === "local") || (k === "import" && state.role === "vps"));
  });
  if (state.role === "vps" && state.view === "frpc") switchView("overview");
  if (state.role === "local" && state.view === "frps") switchView("overview");
}

function statusBadge(svc) {
  if (!svc || !svc.installed) return `<span class="badge badge-warn">${icon("download", 12)}未安装</span>`;
  if (svc.running) return `<span class="badge badge-ok"><span class="dot"></span>运行中</span>`;
  return `<span class="badge badge-off"><span class="dot"></span>已停止</span>`;
}

function renderStats() {
  const d = state.data;
  const cfg = d.config || {};
  const svcs = d.services || {};

  const map = [
    ["Frps", svcs.frps, pick(cfg, "frps.controlPort", 7000)],
    ["Frpc", svcs.frpc, pick(cfg, "frpc.serverPort", 7000)],
    ["Caddy", svcs.caddy, pick(cfg, "caddy.httpsPort", 443)],
  ];

  map.forEach(([key, svc, port]) => {
    const badge = $(`stat${key}Badge`);
    const meta = $(`stat${key}Meta`);
    if (!badge || !meta) return;
    badge.outerHTML = `<span id="stat${key}Badge" style="margin-left:auto">${statusBadge(svc)}</span>`;
    const running = svc && svc.running;
    meta.textContent = !svc || !svc.installed
      ? "未安装 · 点击启动自动安装"
      : `${svc.version ? "v" + svc.version : "已安装"}${running && svc.pid ? ` · PID ${svc.pid}` : ""}`;
  });

  $("statFrpsPort").textContent = `控制端口 ${pick(cfg, "frps.controlPort", 7000)} · ${
    (pick(cfg, "frps.services", []) || []).length
  } 条规则`;
  const addr = pick(cfg, "frpc.serverAddr", "");
  $("statFrpcServer").textContent = addr
    ? `远端 ${addr}:${pick(cfg, "frpc.serverPort", 7000)} · ${(pick(cfg, "frpc.tunnels", []) || []).length} 条隧道`
    : "远端未配置";
  const caddyRules = (pick(cfg, "caddy.rules", []) || []).filter((r) => r.enabled).length;
  $("statCaddyRules").textContent = `${caddyRules} 条启用规则 · HTTPS :${pick(cfg, "caddy.httpsPort", 443)}`;
}

function renderNetwork() {
  const cfg = state.data.config || {};
  const net = cfg.network || {};
  const setText = (id, text) => {
    const el = $(id);
    if (el) el.textContent = text || "-";
  };
  setText("netV4", net.publicIPv4 || "未探测");
  setText("netV6", net.publicIPv6 || "未探测");
  setText("netLocal", net.localIPv4 || "-");
  $("netV4Tag").textContent = net.v4Status || "-";
  $("netV6Tag").textContent = net.v6Status || "-";

  const host = publicAddr(cfg);
  const port = String(pick(cfg, "web.listenAddr", "0.0.0.0:7500")).split(":").pop();
  setText("netAccess", host ? `${host}:${port}` : "-");

  $("ipPrefSelect").value = net.ipPref || "4";
}

function publicAddr(cfg) {
  const net = cfg.network || {};
  if (net.ipPref === "6") return net.publicIPv6 || net.publicIPv4 || "";
  return net.publicIPv4 || net.publicIPv6 || net.localIPv4 || "";
}

function renderChecklist() {
  const d = state.data;
  const cfg = d.config || {};
  const svcs = d.services || {};
  const role = cfg.nodeRole || "both";
  const isVps = role === "vps" || role === "both";
  const isLocal = role === "local" || role === "both";
  const items = [];

  items.push({
    text: "完成初始化设置",
    done: cfg.setupCompleted === true,
    action: () => openWizard(),
  });

  if (isVps) {
    items.push({
      text: "启动 frps 服务端",
      done: !!(svcs.frps && svcs.frps.running),
      action: () => switchView("frps"),
    });
    items.push({
      text: "添加至少一条穿透服务",
      done: (pick(cfg, "frps.services", []) || []).length > 0,
      action: () => switchView("frps"),
    });
  }

  if (isLocal) {
    if (!pick(cfg, "frpc.serverAddr", "")) {
      items.push({ text: "导入远程连接码 / 填写 VPS 地址", done: false, action: () => switchView("frpc") });
    }
    items.push({
      text: "启动 frpc 客户端",
      done: !!(svcs.frpc && svcs.frpc.running),
      action: () => switchView("frpc"),
    });
  }

  items.push({
    text: "配置 Caddy 反代并启动",
    done: !!(svcs.caddy && svcs.caddy.running) && (pick(cfg, "caddy.rules", []) || []).length > 0,
    action: () => switchView("caddy"),
  });

  const doneCount = items.filter((i) => i.done).length;
  $("setupBadge").textContent = `${doneCount}/${items.length} 已完成`;
  $("setupBadge").className = `badge ${doneCount === items.length ? "badge-ok" : "badge-neutral"}`;

  $("checklist").innerHTML = items
    .map(
      (item, i) => `<button class="checklist-item ${item.done ? "done" : ""}" data-check="${i}">
        <span class="step-mark">${item.done ? icon("check", 13) : i + 1}</span>
        <span>${esc(item.text)}</span>
        ${icon("chevron", 15)}
      </button>`
    )
    .join("");

  $$("#checklist [data-check]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const item = items[Number(btn.dataset.check)];
      if (item && item.action) item.action();
    });
  });
}

/* ------------------------------------------------------------ tables */
function emptyRow(cols, text, actionHtml = "") {
  return `<tr><td colspan="${cols}" style="padding:0">
    <div class="empty">
      <div class="empty-icon">${icon("box", 22)}</div>
      <p>${text}</p>
      ${actionHtml}
    </div>
  </td></tr>`;
}

function renderFrps() {
  const cfg = state.data.config || {};
  const list = pick(cfg, "frps.services", []) || [];
  const body = $("frpsServicesBody");
  const addr = publicAddr(cfg) || "VPS_IP";

  $("frpsControlPort").value = pick(cfg, "frps.controlPort", 7000);
  $("frpsToken").value = pick(cfg, "frps.token", "");
  const mode = pick(cfg, "frps.allowPortsMode", "unlimited");
  $("frpsAllowMode").value = mode;
  $("frpsAllowRange").value = pick(cfg, "frps.allowPortsRange", "");
  $("frpsAllowRange").disabled = mode !== "range";
  $("frpsAllowHint").textContent =
    mode === "strict"
      ? "仅放行列表内端口，新增规则需重启 frps。"
      : mode === "range"
      ? "NAT VPS 推荐：把服务商给的端口段直接填入。"
      : "本地新增穿透无需再改 VPS 配置。";

  if (!list.length) {
    body.innerHTML = emptyRow(
      7,
      "还没有穿透服务。可从 Minecraft、Web、NAS、SSH 等预设快速添加。",
      `<button class="btn btn-sm btn-primary" data-empty="add-frps">${icon("plus", 14)}添加穿透服务</button>`
    );
    bindEmptyActions();
    return;
  }

  body.innerHTML = list
    .map((s) => {
      const access = s.caddyDomain
        ? `<span class="badge badge-purple">${icon("globe", 12)}${esc(s.caddyDomain)}</span>`
        : `<span class="mono">${esc(addr)}:${s.remotePort}</span>`;
      return `<tr>
        <td data-label="名称">
          <div class="cell-main">${esc(s.name)}</div>
          ${s.remark ? `<div class="cell-sub">${esc(s.remark)}</div>` : ""}
        </td>
        <td data-label="协议"><span class="tag">${esc((s.type || "tcp").toUpperCase())}</span></td>
        <td data-label="内网目标"><span class="mono">${esc(s.localIP || "127.0.0.1")}:${s.localPort}</span></td>
        <td data-label="对外端口"><span class="mono">${s.remotePort}</span></td>
        <td data-label="访问方式">${access}</td>
        <td data-label="状态">${
          s.enabled ? `<span class="badge badge-ok">已启用</span>` : `<span class="badge badge-neutral">已停用</span>`
        }</td>
        <td data-label="操作">
          <div class="cell-actions">
            <button class="btn btn-sm" data-edit-frps="${esc(s.id)}">${icon("edit", 13)}编辑</button>
            <button class="btn btn-sm btn-danger" data-del-frps="${esc(s.id)}">${icon("trash", 13)}删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderFrpc() {
  const cfg = state.data.config || {};
  const list = pick(cfg, "frpc.tunnels", []) || [];

  $("frpcServerAddr").value = pick(cfg, "frpc.serverAddr", "");
  $("frpcServerPort").value = pick(cfg, "frpc.serverPort", 7000);
  $("frpcToken").value = pick(cfg, "frpc.token", "");
  $("frpcTLSEnable").checked = pick(cfg, "frpc.tlsEnable", true) !== false;
  $("frpcAdminPortLabel").textContent = `127.0.0.1:${pick(cfg, "frpc.adminPort", 7400)}`;

  const body = $("frpcTunnelsBody");
  if (!list.length) {
    body.innerHTML = emptyRow(
      7,
      "还没有本地隧道。可导入 VPS 连接码，或手动添加本机 / 局域网服务。",
      `<div class="row"><button class="btn btn-sm btn-primary" data-empty="import">${icon("upload", 14)}导入连接码</button>
       <button class="btn btn-sm" data-empty="add-frpc">${icon("plus", 14)}添加隧道</button></div>`
    );
    bindEmptyActions();
    return;
  }

  const srv = pick(cfg, "frpc.serverAddr", "") || "VPS_IP";
  body.innerHTML = list
    .map((s) => {
      const access = s.caddyDomain
        ? `<span class="badge badge-purple">${icon("globe", 12)}${esc(s.caddyDomain)}</span>`
        : `<span class="mono">${esc(srv)}:${s.remotePort}</span>`;
      return `<tr>
        <td data-label="名称">
          <div class="cell-main">${esc(s.name)}</div>
          ${s.remark ? `<div class="cell-sub">${esc(s.remark)}</div>` : ""}
        </td>
        <td data-label="协议"><span class="tag">${esc((s.type || "tcp").toUpperCase())}</span></td>
        <td data-label="本地目标"><span class="mono">${esc(s.localIP || "127.0.0.1")}:${s.localPort}</span></td>
        <td data-label="远端端口"><span class="mono">${s.remotePort}</span></td>
        <td data-label="访问方式">${access}</td>
        <td data-label="状态">${
          s.enabled ? `<span class="badge badge-ok">已启用</span>` : `<span class="badge badge-neutral">已停用</span>`
        }</td>
        <td data-label="操作">
          <div class="cell-actions">
            <button class="btn btn-sm" data-edit-frpc="${esc(s.id)}">${icon("edit", 13)}编辑</button>
            <button class="btn btn-sm btn-danger" data-del-frpc="${esc(s.id)}">${icon("trash", 13)}删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderCaddy() {
  const cfg = state.data.config || {};
  const list = pick(cfg, "caddy.rules", []) || [];
  const body = $("caddyRulesBody");

  $("caddyHTTPPort").value = pick(cfg, "caddy.httpPort", 80);
  $("caddyHTTPSPort").value = pick(cfg, "caddy.httpsPort", 443);
  $("caddyEmail").value = pick(cfg, "caddy.email", "");
  $("caddyDNSProvider").value = pick(cfg, "caddy.dnsProvider", "");
  $("caddyDNSToken").value = pick(cfg, "caddy.dnsToken", "");
  $("caddyDNSSecret").value = pick(cfg, "caddy.dnsSecret", "");

  if (!list.length) {
    body.innerHTML = emptyRow(
      7,
      "还没有反代规则。可绑定域名自动 HTTPS，或把局域网端口反代出来。",
      `<button class="btn btn-sm btn-primary" data-empty="add-caddy">${icon("plus", 14)}添加反代规则</button>`
    );
    bindEmptyActions();
    return;
  }

  const tls = {
    auto: `<span class="badge badge-ok">自动 HTTPS</span>`,
    dns: `<span class="badge badge-purple">DNS 证书</span>`,
    custom: `<span class="badge badge-info">自定义证书</span>`,
    internal: `<span class="badge badge-warn">内网自签</span>`,
    http: `<span class="badge badge-neutral">纯 HTTP</span>`,
  };

  body.innerHTML = list
    .map((r) => {
      const features = [];
      if (r.enableEncode) features.push("压缩");
      if (r.skipTlsVerify) features.push("跳过校验");
      if (r.extraDirectives) features.push("自定义指令");
      const origin = r.linkedTunnelId
        ? `<span class="badge badge-info">穿透联动</span>`
        : `<span class="badge badge-neutral">独立规则</span>`;
      return `<tr>
        <td data-label="名称"><div class="cell-main">${esc(r.name)}</div><div class="cell-sub">${origin}</div></td>
        <td data-label="前端入口"><span class="mono">${esc(r.listen)}</span></td>
        <td data-label="后端目标"><span class="mono">${esc(r.upstream)}</span></td>
        <td data-label="证书模式">${tls[r.tlsMode] || tls.auto}</td>
        <td data-label="特性">${features.length ? esc(features.join(" · ")) : "-"}</td>
        <td data-label="状态">${
          r.enabled ? `<span class="badge badge-ok">已启用</span>` : `<span class="badge badge-neutral">已停用</span>`
        }</td>
        <td data-label="操作">
          <div class="cell-actions">
            <button class="btn btn-sm" data-edit-caddy="${esc(r.id)}">${icon("edit", 13)}编辑</button>
            <button class="btn btn-sm btn-danger" data-del-caddy="${esc(r.id)}">${icon("trash", 13)}删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderScanner() {
  // 只看外部/原生服务；由 Autofrp 托管的组件已在总览与配置页展示
  const apps = (state.data.detectedApps || []).filter((a) => !a.managedByAutof);
  const body = $("detectedAppsBody");

  if (!apps.length) {
    body.innerHTML = emptyRow(6, "未检测到外部 NPM、Nginx、原版 Caddy 或 frp 实例。");
  } else {
    body.innerHTML = apps
      .map((a) => {
        const running = a.running
          ? `<span class="badge badge-ok"><span class="dot"></span>运行中</span>`
          : `<span class="badge badge-warn">未运行</span>`;
        const meta = a.container
          ? `Docker · ${esc(a.container)}`
          : a.pid
          ? `PID ${a.pid}`
          : esc(a.binaryOrImage || "-");
        const ports = (a.listenPorts || []).length
          ? `<span class="mono">${esc((a.listenPorts || []).join(", "))}</span>`
          : "-";
        const cfgPath = a.primaryConfig
          ? `<span class="mono">${esc(a.primaryConfig)}</span>`
          : `<span class="faint">未找到</span>`;
        const managed = a.managedByAutof
          ? `<span class="badge badge-info">Autofrp 托管</span>`
          : `<span class="badge badge-neutral">外部服务</span>`;
        const actions = [
          (a.configFiles || []).length
            ? `<button class="btn btn-sm" data-edit-app="${esc(a.id)}">${icon("edit", 13)}编辑配置</button>`
            : "",
          !a.managedByAutof
            ? `<button class="btn btn-sm btn-primary" data-takeover="${esc(a.id)}">${icon("rocket", 13)}吞并迁移</button>`
            : "",
        ]
          .filter(Boolean)
          .join("");
        return `<tr>
          <td data-label="服务"><div class="cell-main">${esc(a.name)}</div><div class="cell-sub">${managed}</div></td>
          <td data-label="状态">${running}<div class="cell-sub">${meta}</div></td>
          <td data-label="端口">${ports}</td>
          <td data-label="主配置">${cfgPath}</td>
          <td data-label="说明" class="muted" style="font-size:12.5px">${esc(a.note || "-")}</td>
          <td data-label="操作"><div class="cell-actions">${actions || "-"}</div></td>
        </tr>`;
      })
      .join("");
  }

  const legacy = state.data.legacy || {};
  const badge = $("legacyBadge");
  const text = $("legacyBody");
  if (legacy.detected) {
    badge.className = "badge badge-ok";
    badge.textContent = "已继承";
    text.innerHTML = `检测到旧版 <span class="mono">state.conf</span> / <span class="mono">services.conf</span>：已迁移 <strong>${
      legacy.serviceCount || 0
    }</strong> 条穿透规则${legacy.legacyPort ? `，原控制端口 <span class="mono">${legacy.legacyPort}</span>` : ""}。
      原文件保留在 <span class="mono">${esc(legacy.stateFile || "")}</span>，可随时回滚；当前配置已写入 <span class="mono">config.json</span>。`;
  } else {
    badge.className = "badge badge-neutral";
    badge.textContent = "未检测到";
    text.innerHTML = `未发现 v1.x 的 <span class="mono">state.conf</span> / <span class="mono">services.conf</span>。若需要从旧版升级，直接把旧配置目录放到 <span class="mono">/etc/autof/</span> 后重启服务即可自动迁移。`;
  }
}

function renderSystem() {
  const d = state.data;
  const cfg = d.config || {};
  $("webListenAddr").value = pick(cfg, "web.listenAddr", "0.0.0.0:7500");
  $("webUsername").value = pick(cfg, "web.username", "admin");
  $("webAuthEnabled").checked = pick(cfg, "web.authEnabled", true) !== false;
  $("webTLSListen").value = pick(cfg, "web.tlsListen", "");
  $("webTLSCert").value = pick(cfg, "web.tlsCertFile", "");
  $("webTLSKey").value = pick(cfg, "web.tlsKeyFile", "");

  const sys = d.systemInfo || {};
  const svcs = d.services || {};
  const rows = [
    ["系统", `${esc(sys.prettyName || "Linux")}`],
    ["架构 / 初始化", `${esc(sys.arch || "-")} · ${esc(sys.initSystem || "-")}`],
    ["包管理器", esc(sys.pkgManager || "-")],
    ["Autofrp", `v${esc(cfg.version || "1.0.2")} · 面板 ${statusInline(svcs.autof)}`],
    ["配置目录", `<span class="mono">${esc(d.stateDir || "/etc/autof")}</span>`],
    ["初始化状态", cfg.setupCompleted ? "已完成" : "待初始化"],
  ];
  $("sysInfoKv").innerHTML = rows
    .map(([k, v]) => `<div class="kv-row"><span>${esc(k)}</span><span class="kv-value">${v}</span></div>`)
    .join("");
}

function statusInline(svc) {
  if (!svc || !svc.installed) return "未安装";
  return svc.running ? "运行中" : "已停止";
}

function renderPresets() {
  const presets = state.data.presets || [];
  $("presetChips").innerHTML = presets
    .map(
      (p) =>
        `<button class="btn btn-sm btn-ghost" data-preset="${esc(p.key)}" title="${esc(p.description || "")}">${esc(
          p.label
        )}</button>`
    )
    .join("");
  $$("#presetChips [data-preset]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const p = presets.find((x) => x.key === btn.dataset.preset);
      if (!p) return;
      $("tunName").value = p.defaultName;
      $("tunType").value = p.type;
      $("tunLocalPort").value = p.localPort;
      $("tunRemotePort").value = p.remotePort;
      $("tunRemark").value = p.label;
      if (p.suggestCaddy && !$("tunEnableCaddy").checked) {
        $("tunEnableCaddy").checked = true;
        $("tunCaddyFields").hidden = false;
      }
    });
  });
}

function bindEmptyActions() {
  $$("[data-empty]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const k = btn.dataset.empty;
      if (k === "add-frps") openTunnelModal("frps", null);
      else if (k === "add-frpc") openTunnelModal("frpc", null);
      else if (k === "import") openImportModal();
      else if (k === "add-caddy") openCaddyModal(null);
    });
  });
}

/* ------------------------------------------------------------ routing */
function switchView(view, pushHash = true) {
  if (!VIEW_META[view]) view = "overview";
  state.view = view;
  $$(".nav-item").forEach((b) => b.setAttribute("aria-current", b.dataset.view === view ? "page" : "false"));
  $$(".section").forEach((s) => s.classList.toggle("active", s.id === `view-${view}`));
  const meta = VIEW_META[view] || VIEW_META.overview;
  $("pageTitle").textContent = meta.title;
  $("pageSub").textContent = meta.sub;
  if (pushHash && location.hash !== `#/${view}`) history.replaceState(null, "", `#/${view}`);
  if (view === "system") loadLogs();
  else stopLogAuto();
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function viewFromHash() {
  const view = (location.hash || "").replace(/^#\/?/, "");
  return VIEW_META[view] ? view : "overview";
}

/* ------------------------------------------------------------ service actions */
async function serviceAction(target, action, btn, extra = {}) {
  const names = { start: "启动", restart: "重启", reload: "热重载", stop: "停止", install: "安装" };
  const label = `${target} ${names[action] || action}`;
  await withBusy(btn, async () => {
    try {
      toast(`正在${label}…`);
      const data = await api("/api/service/action", {
        method: "POST",
        body: JSON.stringify(Object.assign({ target, action }, extra)),
      });
      state.data = data;
      render();
      toast(`${label}成功`, "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
      throw err;
    }
  }).catch(() => {});
}

/* ------------------------------------------------------------ forms */
async function saveFrps(btn) {
  const payload = {
    controlPort: Number($("frpsControlPort").value),
    token: $("frpsToken").value.trim(),
    allowPortsMode: $("frpsAllowMode").value,
    allowPortsRange: $("frpsAllowRange").value.trim(),
  };
  if (!payload.controlPort || payload.controlPort < 1 || payload.controlPort > 65535) {
    toast("控制端口需在 1 ~ 65535 之间", "err");
    return;
  }
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/frps/settings", { method: "POST", body: JSON.stringify(payload) });
      render();
      toast("frps 参数已保存并生效", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function saveFrpc(btn) {
  const payload = {
    serverAddr: $("frpcServerAddr").value.trim(),
    serverPort: Number($("frpcServerPort").value),
    token: $("frpcToken").value.trim(),
    tlsEnable: $("frpcTLSEnable").checked,
  };
  if (!payload.serverPort || payload.serverPort < 1 || payload.serverPort > 65535) {
    toast("控制端口需在 1 ~ 65535 之间", "err");
    return;
  }
  if (!payload.serverAddr) {
    toast("请填写远程 VPS 地址，或使用导入连接码", "err");
    return;
  }
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/frpc/settings", { method: "POST", body: JSON.stringify(payload) });
      render();
      toast("frpc 配置已保存并热重载", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function saveCaddy(btn) {
  const payload = {
    httpPort: Number($("caddyHTTPPort").value) || 80,
    httpsPort: Number($("caddyHTTPSPort").value) || 443,
    email: $("caddyEmail").value.trim(),
    dnsProvider: $("caddyDNSProvider").value,
    dnsToken: $("caddyDNSToken").value.trim(),
    dnsSecret: $("caddyDNSSecret").value.trim(),
  };
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/caddy/settings", { method: "POST", body: JSON.stringify(payload) });
      render();
      toast("Caddy 全局设置已保存", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function saveWeb(btn) {
  const pwd = $("webPassword").value;
  const payload = {
    listenAddr: $("webListenAddr").value.trim(),
    tlsListen: $("webTLSListen").value.trim(),
    tlsCertFile: $("webTLSCert").value.trim(),
    tlsKeyFile: $("webTLSKey").value.trim(),
    username: $("webUsername").value.trim(),
    password: pwd,
    authEnabled: $("webAuthEnabled").checked,
  };
  const tlsWas = pick(state.data.config, "web.tlsListen", "");
  const tlsNow = payload.tlsListen;
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/system/web", { method: "POST", body: JSON.stringify(payload) });
      render();
      $("webPassword").value = "";
      if (tlsWas !== tlsNow) {
        toast("面板设置已保存，HTTPS 监听变更需重启面板服务后生效", "ok");
      } else {
        toast("面板设置已保存", "ok");
      }
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ tunnel modal */
function openTunnelModal(scope, item) {
  $("tunScope").value = scope;
  $("tunID").value = item ? item.id : "";
  $("tunnelTitle").textContent = item
    ? `编辑${scope === "frps" ? "穿透服务" : "本地隧道"}`
    : scope === "frps"
    ? "添加穿透服务"
    : "添加本地隧道";
  $("tunnelSub").textContent =
    scope === "frps" ? "登记在 VPS 端，并编入导出给客户端的配置" : "在本机运行 frpc，把内网服务暴露到 VPS";

  $("tunName").value = item ? item.name : "";
  $("tunType").value = item ? item.type : "tcp";
  $("tunLocalIP").value = item ? item.localIP || "127.0.0.1" : "127.0.0.1";
  $("tunLocalPort").value = item ? item.localPort : "";
  $("tunRemotePort").value = item ? item.remotePort : "";
  $("tunRemark").value = item ? item.remark || "" : "";
  $("tunEnabled").checked = item ? item.enabled !== false : true;

  const hasCaddy = item && item.caddyDomain;
  $("tunEnableCaddy").checked = !!hasCaddy;
  $("tunCaddyFields").hidden = !hasCaddy;
  $("tunCaddyDomain").value = item ? item.caddyDomain || "" : "";
  $("tunCaddyTLSMode").value = item ? item.caddyTlsMode || "auto" : "auto";

  openModal("tunnelModal");
}

async function submitTunnel(btn) {
  const scope = $("tunScope").value;
  const payload = {
    id: $("tunID").value,
    name: $("tunName").value.trim(),
    type: $("tunType").value,
    localIP: $("tunLocalIP").value.trim() || "127.0.0.1",
    localPort: Number($("tunLocalPort").value),
    remotePort: Number($("tunRemotePort").value) || Number($("tunLocalPort").value),
    remark: $("tunRemark").value.trim(),
    caddyDomain: $("tunEnableCaddy").checked ? $("tunCaddyDomain").value.trim() : "",
    caddyTlsMode: $("tunCaddyTLSMode").value,
    enabled: $("tunEnabled").checked,
  };

  if (!payload.name) return toast("请填写规则名称", "err");
  if (!payload.localPort || payload.localPort < 1 || payload.localPort > 65535) return toast("内网端口不合法", "err");
  if (!payload.remotePort || payload.remotePort < 1 || payload.remotePort > 65535) return toast("对外端口不合法", "err");
  if (payload.caddyDomain && !/^[a-zA-Z0-9*.:_-]+$/.test(payload.caddyDomain)) {
    return toast("域名格式不正确", "err");
  }

  await withBusy(btn, async () => {
    try {
      const url = scope === "frps" ? "/api/frps/service" : "/api/frpc/tunnel";
      state.data = await api(url, { method: "POST", body: JSON.stringify(payload) });
      render();
      closeModal("tunnelModal");
      toast("规则已保存并应用", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function deleteTunnel(scope, id, btn) {
  const ok = await confirmDialog({
    title: "删除穿透规则",
    text: "删除后关联的 Caddy 联动反代也会一并移除，确定继续？",
    okText: "删除",
  });
  if (!ok) return;
  await withBusy(btn, async () => {
    try {
      const url =
        scope === "frps"
          ? `/api/frps/service?id=${encodeURIComponent(id)}`
          : `/api/frpc/tunnel?id=${encodeURIComponent(id)}`;
      state.data = await api(url, { method: "DELETE" });
      render();
      toast("已删除", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ caddy modal */
function openCaddyModal(rule) {
  $("caddyRuleID").value = rule ? rule.id : "";
  $("caddyTitle").textContent = rule ? `编辑反代规则 · ${rule.name}` : "添加反代规则";
  $("caddyRuleName").value = rule ? rule.name : "";
  $("caddyRuleListen").value = rule ? rule.listen : "";
  $("caddyRuleUpstream").value = rule ? rule.upstream : "";
  $("caddyRuleTLSMode").value = rule ? rule.tlsMode || "auto" : "auto";
  $("caddyRuleCertFile").value = rule ? rule.certFile || "" : "";
  $("caddyRuleKeyFile").value = rule ? rule.keyFile || "" : "";
  $("caddyRuleEncode").checked = rule ? rule.enableEncode !== false : true;
  $("caddyRuleSkipVerify").checked = rule ? !!rule.skipTlsVerify : false;
  $("caddyRuleEnabled").checked = rule ? rule.enabled !== false : true;
  $("caddyRuleExtra").value = rule ? rule.extraDirectives || "" : "";
  syncCaddyCertFields();

  const cfg = state.data.config || {};
  const options = [`<option value="">手动填写后端地址</option>`];
  (pick(cfg, "frps.services", []) || []).forEach((s, i) => {
    options.push(
      `<option value="frps:${i}">[VPS 穿透] ${esc(s.name)} → 127.0.0.1:${s.remotePort}</option>`
    );
  });
  (pick(cfg, "frpc.tunnels", []) || []).forEach((s, i) => {
    options.push(
      `<option value="frpc:${i}">[本地隧道] ${esc(s.name)} → ${esc(s.localIP || "127.0.0.1")}:${s.localPort}</option>`
    );
  });
  const listenAddr = pick(cfg, "web.listenAddr", "0.0.0.0:7500");
  options.push(
    `<option value="self">[本面板] Autofrp WebUI → 127.0.0.1:${esc(String(listenAddr).split(":").pop())}</option>`
  );
  $("caddyQuickTunnel").innerHTML = options.join("");
  openModal("caddyModal");
}

function syncCaddyCertFields() {
  $("caddyCertFields").hidden = $("caddyRuleTLSMode").value !== "custom";
}

function applyQuickTunnel(key) {
  if (!key) return;
  const cfg = state.data.config || {};
  if (key === "self") {
    const port = String(pick(cfg, "web.listenAddr", "0.0.0.0:7500")).split(":").pop();
    $("caddyRuleName").value = "Autofrp 控制台";
    $("caddyRuleUpstream").value = `127.0.0.1:${port}`;
    return;
  }
  const [scope, idx] = key.split(":");
  const item =
    scope === "frps"
      ? (pick(cfg, "frps.services", []) || [])[Number(idx)]
      : (pick(cfg, "frpc.tunnels", []) || [])[Number(idx)];
  if (!item) return;
  $("caddyRuleName").value = item.name;
  $("caddyRuleUpstream").value =
    scope === "frps" ? `127.0.0.1:${item.remotePort}` : `${item.localIP || "127.0.0.1"}:${item.localPort}`;
}

async function submitCaddy(btn) {
  const payload = {
    id: $("caddyRuleID").value,
    name: $("caddyRuleName").value.trim(),
    listen: $("caddyRuleListen").value.trim(),
    upstream: $("caddyRuleUpstream").value.trim(),
    tlsMode: $("caddyRuleTLSMode").value,
    certFile: $("caddyRuleCertFile").value.trim(),
    keyFile: $("caddyRuleKeyFile").value.trim(),
    enableEncode: $("caddyRuleEncode").checked,
    skipTlsVerify: $("caddyRuleSkipVerify").checked,
    enabled: $("caddyRuleEnabled").checked,
    extraDirectives: $("caddyRuleExtra").value.trim(),
  };
  if (!payload.listen) return toast("请填写前端监听入口", "err");
  if (!payload.upstream) return toast("请填写后端目标地址", "err");
  if (payload.tlsMode === "custom" && (!payload.certFile || !payload.keyFile)) {
    return toast("自定义证书模式需要填写证书与私钥路径", "err");
  }
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/caddy/rule", { method: "POST", body: JSON.stringify(payload) });
      render();
      closeModal("caddyModal");
      toast("规则已保存并热重载", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function deleteCaddy(id, btn) {
  const ok = await confirmDialog({ title: "删除反代规则", text: "删除后该域名将立即停止反代，确定继续？", okText: "删除" });
  if (!ok) return;
  await withBusy(btn, async () => {
    try {
      state.data = await api(`/api/caddy/rule?id=${encodeURIComponent(id)}`, { method: "DELETE" });
      render();
      toast("已删除", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ import */
function openImportModal() {
  $("importInput").value = "";
  $("importPreview").hidden = true;
  $("btnApplyImport").disabled = true;
  state.imported = null;
  openModal("importModal");
}

async function parseImport(btn) {
  const input = $("importInput").value.trim();
  if (!input) return toast("请先粘贴连接码或 frpc.toml", "err");
  await withBusy(btn, async () => {
    try {
      const res = await api("/api/frpc/parse", { method: "POST", body: JSON.stringify({ input }) });
      state.imported = { input, res };
      $("importPreview").hidden = false;
      $("importSummary").innerHTML = `服务器 <span class="mono">${esc(res.serverAddr)}:${res.serverPort}</span> · 隧道 <strong>${
        res.tunnelCount
      }</strong> 条 · Token ${res.hasToken ? "已包含" : "未包含"} · TLS ${res.tlsEnable ? "开启" : "关闭"}`;
      $("importTunnels").textContent = (res.tunnels || [])
        .map((t) => `[${(t.type || "tcp").toUpperCase()}] ${t.name}  ${t.localIP || "127.0.0.1"}:${t.localPort} -> :${t.remotePort}`)
        .join("\n") || "（无隧道规则，仅更新服务器连接信息）";
      $("btnApplyImport").disabled = false;
      toast("解析成功，请确认后导入", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function applyImport(btn) {
  if (!state.imported) return;
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/frpc/import", {
        method: "POST",
        body: JSON.stringify({ input: state.imported.input, merge: $("importMerge").checked }),
      });
      render();
      closeModal("importModal");
      toast("导入成功，本地 frpc 配置已更新", "ok");
      switchView("frpc");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ preview / export */
function showPreview(title, content, sub = "") {
  $("previewTitle").textContent = title;
  $("previewSub").textContent = sub;
  $("previewCode").textContent = content || "";
  openModal("previewModal");
}

function openExport() {
  const ex = state.data.exports || {};
  $("exportCode").value = ex.connCode || "";
  $("exportToml").textContent = ex.frpcExportToml || "";
  openModal("exportModal");
}

/* ------------------------------------------------------------ logs */
function setLogTarget(target) {
  state.logTarget = target;
  $$(".segmented [data-log]").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset.log === target)));
}

async function loadLogs(btn) {
  if (state.view !== "system") return;
  const target = state.logTarget;
  try {
    const data = await api(`/api/service/logs?target=${encodeURIComponent(target)}&lines=${$("logLines").value}`);
    const view = $("logView");
    view.textContent = data.logs || "暂无日志";
    view.scrollTop = view.scrollHeight;
  } catch (err) {
    if (!(err instanceof AuthError)) $("logView").textContent = `读取失败：${err.message}`;
  }
}

function startLogAuto() {
  stopLogAuto();
  state.logTimer = setInterval(() => loadLogs(), 5000);
}

function stopLogAuto() {
  if (state.logTimer) clearInterval(state.logTimer);
  state.logTimer = null;
}

/* ------------------------------------------------------------ scanner actions */
function findApp(id) {
  return (state.data.detectedApps || []).find((a) => a.id === id);
}

async function openEditor(appId) {
  const app = findApp(appId);
  if (!app) return;
  $("editorTitle").textContent = `编辑配置 · ${app.name}`;
  $("editorAppId").value = app.id;
  const sel = $("editorFile");
  sel.innerHTML = (app.configFiles || []).map((f) => `<option value="${esc(f)}">${esc(f)}</option>`).join("");
  openModal("editorModal");
  if (app.configFiles && app.configFiles.length) await loadEditorFile(app.configFiles[0]);
}

async function loadEditorFile(path) {
  const ta = $("editorContent");
  ta.value = "读取中…";
  try {
    const res = await api(`/api/scanner/file?path=${encodeURIComponent(path)}`);
    ta.value = res.content || "";
  } catch (err) {
    ta.value = "";
    if (!(err instanceof AuthError)) toast(err.message, "err");
  }
}

async function saveEditor(btn, reload) {
  const path = $("editorFile").value;
  const content = $("editorContent").value;
  const appId = $("editorAppId").value;
  if (!path) return;
  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/scanner/file", {
        method: "POST",
        body: JSON.stringify({ path, content, reload, appId }),
      });
      render();
      closeModal("editorModal");
      toast(reload ? "已保存并重载原服务（已生成 .autof.bak 备份）" : "已保存（已生成 .autof.bak 备份）", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

async function openTakeover(appId) {
  try {
    const plan = await api(`/api/scanner/takeover-preview?appId=${encodeURIComponent(appId)}`);
    $("takeoverAppId").value = plan.appId;
    $("takeoverTitle").textContent = `吞并迁移 · ${plan.appName}`;
    $("takeoverSub").textContent = `目标目录 ${plan.targetStateDir} · 备份 ${plan.backupTargetDir}`;

    const rules = plan.discoveredRules || [];
    const certs = plan.discoveredCerts || [];
    const tunnels = plan.discoveredTunnels || [];
    const steps = plan.cleanupSteps || [];

    let html = "";
    if (rules.length) {
      html += `<div class="card tight"><div class="card-title" style="margin-bottom:8px">识别到的反代站点（${rules.length}）</div>
        <pre class="code" style="max-height:150px">${rules
          .map((r) => `[${r.hasSsl ? "HTTPS" : "HTTP "}] ${r.domain}  →  ${r.upstream}`)
          .join("\n")}</pre></div>`;
    }
    if (certs.length) {
      html += `<div class="card tight"><div class="card-title" style="margin-bottom:8px">可迁移的 SSL 证书（${certs.length}）</div>
        <pre class="code" style="max-height:130px">${certs
          .map((c) => `${c.domain}: ${c.srcCertPath}`)
          .join("\n")}</pre></div>`;
    }
    if (tunnels.length || plan.discoveredFrpsPort) {
      html += `<div class="card tight"><div class="card-title" style="margin-bottom:8px">FRP 配置</div>
        <pre class="code" style="max-height:120px">${
          plan.discoveredFrpsPort ? `控制端口: ${plan.discoveredFrpsPort}\n` : ""
        }${tunnels.map((t) => `[${(t.type || "tcp").toUpperCase()}] ${t.name} ${t.localIP}:${t.localPort} -> :${t.remotePort}`).join("\n")}</pre></div>`;
    }
    html += `<div class="card tight"><div class="card-title" style="margin-bottom:8px">执行步骤</div>
      <ol style="margin:0 0 0 18px;color:var(--muted);font-size:12.8px;display:grid;gap:4px">
        ${steps.map((s) => `<li>${esc(s)}</li>`).join("")}
      </ol></div>`;

    $("takeoverSummary").innerHTML = html;
    $("tkCerts").checked = true;
    $("tkStopOld").checked = true;
    $("tkRemoveOld").checked = false;
    $("tkStart").checked = true;
    openModal("takeoverModal");
  } catch (err) {
    if (!(err instanceof AuthError)) toast(err.message, "err");
  }
}

async function confirmTakeover(btn) {
  const appId = $("takeoverAppId").value;
  const ok = await confirmDialog({
    title: "确认吞并迁移",
    text: "将迁移规则与证书，并按勾选项停用老服务。该操作会影响正在运行的线上服务，确认继续？",
    okText: "确认执行",
  });
  if (!ok) return;
  await withBusy(btn, async () => {
    try {
      const data = await api("/api/scanner/takeover", {
        method: "POST",
        body: JSON.stringify({
          appId,
          useImportedCerts: $("tkCerts").checked,
          stopOldService: $("tkStopOld").checked,
          removeOldService: $("tkRemoveOld").checked,
          startAutofService: $("tkStart").checked,
        }),
      });
      state.data = data;
      render();
      closeModal("takeoverModal");
      const r = data.takeoverResult || {};
      toast(`迁移完成：反代 ${r.importedRules || 0} 条，证书 ${r.copiedCerts || 0} 套`, "ok");
      switchView("caddy");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ wizard */
function openWizard() {
  const cfg = state.data.config || {};
  state.wizardStep = 1;
  const role = cfg.nodeRole || "both";
  const radio = document.querySelector(`input[name="wizRole"][value="${role}"]`);
  if (radio) radio.checked = true;
  $("wizIpPref").value = pick(cfg, "network.ipPref", "4");
  $("wizControlPort").value = pick(cfg, "frps.controlPort", 7000);
  $("wizToken").value = pick(cfg, "frps.token", "");
  $("wizFrpcAddr").value = pick(cfg, "frpc.serverAddr", "");
  $("wizFrpcPort").value = pick(cfg, "frpc.serverPort", 7000);
  $("wizStartFrps").checked = role !== "local";
  $("wizStartCaddy").checked = true;
  $("wizStartFrpc").checked = role === "local" && !!pick(cfg, "frpc.serverAddr", "");
  $("wizProgress").hidden = true;
  $("wizProgress").innerHTML = "";
  syncWizardStep();
  openModal("wizardModal");
}

function wizardRole() {
  const checked = document.querySelector('input[name="wizRole"]:checked');
  return checked ? checked.value : "both";
}

function syncWizardStep() {
  const step = state.wizardStep;
  $$("#wizardModal .step").forEach((el) => {
    const n = Number(el.dataset.step);
    el.classList.toggle("active", n === step);
    el.classList.toggle("done", n < step);
  });
  $$("#wizardModal [data-panel]").forEach((el) => {
    el.hidden = Number(el.dataset.panel) !== step;
  });
  const role = wizardRole();
  $("wizVpsFields").hidden = role === "local";
  $("wizLocalFields").hidden = role === "vps";
  $("wizPrev").hidden = step === 1;
  $("wizSkip").hidden = step === 3;
  $("wizNext").textContent = step === 3 ? "完成初始化" : "下一步";
}

async function wizardNext(btn) {
  if (state.wizardStep < 3) {
    if (state.wizardStep === 1) syncWizardStep();
    state.wizardStep += 1;
    syncWizardStep();
    return;
  }
  await finishWizard(btn);
}

async function finishWizard(btn) {
  const role = wizardRole();
  const payload = {
    nodeRole: role,
    ipPref: $("wizIpPref").value,
    controlPort: Number($("wizControlPort").value),
    token: $("wizToken").value.trim(),
    frpcAddr: $("wizFrpcAddr").value.trim(),
    frpcPort: Number($("wizFrpcPort").value),
  };

  const progress = $("wizProgress");
  progress.hidden = false;
  const tasks = [];
  if (role !== "local" && $("wizStartFrps").checked) tasks.push({ target: "frps", label: "启动 frps 服务端" });
  if ($("wizStartCaddy").checked) tasks.push({ target: "caddy", label: "启动 Caddy 反代" });
  if (role !== "vps" && $("wizStartFrpc").checked) tasks.push({ target: "frpc", label: "启动 frpc 客户端" });

  progress.innerHTML = tasks
    .map(
      (t, i) =>
        `<div class="progress-item" data-task="${i}">${icon("activity", 15)}<span>${esc(t.label)}</span>
         <span class="state">等待中</span></div>`
    )
    .join("");

  await withBusy(btn, async () => {
    try {
      state.data = await api("/api/system/init", { method: "POST", body: JSON.stringify(payload) });
      render();
      for (let i = 0; i < tasks.length; i++) {
        const row = progress.querySelector(`[data-task="${i}"]`);
        const st = row.querySelector(".state");
        st.textContent = "进行中…";
        row.querySelector("svg").style.color = "var(--brand)";
        try {
          state.data = await api("/api/service/action", {
            method: "POST",
            body: JSON.stringify({ target: tasks[i].target, action: "start" }),
          });
          st.textContent = "完成";
          row.querySelector("svg").style.color = "var(--cta)";
        } catch (err) {
          st.textContent = "失败";
          row.querySelector("svg").style.color = "var(--danger)";
          if (!(err instanceof AuthError)) toast(`${tasks[i].label}：${err.message}`, "err");
        }
      }
      render();
      toast("初始化完成", "ok");
      localStorage.removeItem("autof.skipWizard");
      if (location.hash === "#/setup") switchView("overview");
      setTimeout(() => closeModal("wizardModal"), 500);
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });
}

/* ------------------------------------------------------------ events */
function bindEvents() {
  $$(".nav-item").forEach((b) => b.addEventListener("click", () => switchView(b.dataset.view)));
  $$(".segmented [data-role]").forEach((b) =>
    b.addEventListener("click", async () => {
      const role = b.dataset.role;
      state.role = role;
      renderRole();
      try {
        state.data = await api("/api/net/pref", { method: "POST", body: JSON.stringify({ nodeRole: role }) });
        render();
      } catch (err) {
        if (!(err instanceof AuthError)) toast(err.message, "err");
      }
    })
  );

  $("btnTheme").addEventListener("click", toggleTheme);
  $("btnRefreshAll").addEventListener("click", (e) => loadOverview(e.currentTarget));
  $$("[data-quick]").forEach((btn) =>
    btn.addEventListener("click", () => {
      const k = btn.dataset.quick;
      if (k === "frps") {
        switchView("frps");
        openTunnelModal("frps", null);
      } else if (k === "import") {
        switchView("frpc");
        openImportModal();
      } else if (k === "export") {
        openExport();
      } else if (k === "scan") {
        switchView("scanner");
        loadOverview();
      }
    })
  );
  window.addEventListener("hashchange", () => {
    if (location.hash === "#/setup") {
      openWizard();
      return;
    }
    switchView(viewFromHash(), false);
  });
  $("btnDetectNet").addEventListener("click", async (e) => {
    await withBusy(e.currentTarget, async () => {
      try {
        state.data = await api("/api/net/refresh", { method: "POST" });
        render();
        toast("网络探测完成", "ok");
      } catch (err) {
        if (!(err instanceof AuthError)) toast(err.message, "err");
      }
    });
  });
  $("ipPrefSelect").addEventListener("change", async (e) => {
    try {
      state.data = await api("/api/net/pref", {
        method: "POST",
        body: JSON.stringify({ ipPref: e.target.value }),
      });
      render();
      toast("网络偏好已更新", "ok");
    } catch (err) {
      if (!(err instanceof AuthError)) toast(err.message, "err");
    }
  });

  $("btnSaveFrps").addEventListener("click", (e) => saveFrps(e.currentTarget));
  $("btnPreviewFrps").addEventListener("click", () =>
    showPreview("frps.toml", (state.data.exports || {}).frpsToml, "远程 VPS 服务端配置")
  );
  $("btnOpenExport").addEventListener("click", openExport);
  $("btnRandomToken").addEventListener("click", () => {
    $("frpsToken").value = randomToken(24);
  });
  $("frpsAllowMode").addEventListener("change", (e) => {
    const mode = e.target.value;
    $("frpsAllowRange").disabled = mode !== "range";
    $("frpsAllowHint").textContent =
      mode === "strict"
        ? "仅放行列表内端口，新增规则需重启 frps。"
        : mode === "range"
        ? "NAT VPS 推荐：把服务商给的端口段直接填入。"
        : "本地新增穿透无需再改 VPS 配置。";
  });
  $("btnAddFrpsService").addEventListener("click", () => openTunnelModal("frps", null));

  $("btnSaveFrpc").addEventListener("click", (e) => saveFrpc(e.currentTarget));
  $("btnPreviewFrpc").addEventListener("click", () =>
    showPreview("frpc.toml", (state.data.exports || {}).frpcClientToml, "本机客户端运行配置")
  );
  $("btnOpenImport").addEventListener("click", openImportModal);
  $("btnAddFrpcTunnel").addEventListener("click", () => openTunnelModal("frpc", null));

  $("btnSaveCaddy").addEventListener("click", (e) => saveCaddy(e.currentTarget));
  $("btnPreviewCaddy").addEventListener("click", () =>
    showPreview("Caddyfile", (state.data.exports || {}).caddyfile, "由结构化规则生成，保存前会自动校验")
  );
  $("btnAddCaddyRule").addEventListener("click", () => openCaddyModal(null));
  $("btnProxySelf").addEventListener("click", () => {
    openCaddyModal(null);
    applyQuickTunnel("self");
  });
  $("btnInstallCaddyDNS").addEventListener("click", async (e) => {
    await saveCaddy(null);
    await serviceAction("caddy", "install", e.currentTarget, { dnsProvider: $("caddyDNSProvider").value });
  });
  $("caddyRuleTLSMode").addEventListener("change", syncCaddyCertFields);
  $("caddyQuickTunnel").addEventListener("change", (e) => applyQuickTunnel(e.target.value));

  $("btnSubmitTunnel").addEventListener("click", (e) => submitTunnel(e.currentTarget));
  $("btnSubmitCaddy").addEventListener("click", (e) => submitCaddy(e.currentTarget));
  $("tunEnableCaddy").addEventListener("change", (e) => {
    $("tunCaddyFields").hidden = !e.target.checked;
  });

  $("btnParseImport").addEventListener("click", (e) => parseImport(e.currentTarget));
  $("btnApplyImport").addEventListener("click", (e) => applyImport(e.currentTarget));

  $("btnRescan").addEventListener("click", (e) => loadOverview(e.currentTarget));
  $("btnSaveWeb").addEventListener("click", (e) => saveWeb(e.currentTarget));
  $("btnRandomPwd").addEventListener("click", () => {
    $("webPassword").value = randomToken(14);
  });
  $("btnGenTLS").addEventListener("click", async (e) => {
    await withBusy(e.currentTarget, async () => {
      try {
        state.data = await api("/api/system/tls/generate", { method: "POST" });
        render();
        const info = state.data.tlsGenerated || {};
        toast(`自签证书已生成，HTTPS 监听 ${info.listen || ""}，重启面板后生效`, "ok");
      } catch (err) {
        if (!(err instanceof AuthError)) toast(err.message, "err");
      }
    });
  });
  $("btnReloadWeb").addEventListener("click", async (e) => {
    const ok = await confirmDialog({
      title: "重启面板服务",
      text: "重启后面板会短暂断开，稍等几秒刷新页面即可。",
      okText: "重启",
      danger: false,
    });
    if (ok) await serviceAction("autof", "restart", e.currentTarget);
  });

  $("btnOpenWizard").addEventListener("click", openWizard);
  $("wizNext").addEventListener("click", (e) => wizardNext(e.currentTarget));
  $("wizPrev").addEventListener("click", () => {
    if (state.wizardStep > 1) state.wizardStep -= 1;
    syncWizardStep();
  });
  $("wizSkip").addEventListener("click", () => {
    localStorage.setItem("autof.skipWizard", "1");
    closeModal("wizardModal");
    if (location.hash === "#/setup") switchView("overview");
  });
  $$('input[name="wizRole"]').forEach((r) => r.addEventListener("change", syncWizardStep));
  $("btnWizToken").addEventListener("click", () => {
    $("wizToken").value = randomToken(24);
  });

  $("logLines").addEventListener("change", () => loadLogs());
  $$(".segmented [data-log]").forEach((b) =>
    b.addEventListener("click", () => {
      setLogTarget(b.dataset.log);
      loadLogs();
    })
  );
  $("btnLoadLogs").addEventListener("click", (e) => withBusy(e.currentTarget, () => loadLogs()));
  $("logAutoRefresh").addEventListener("change", (e) => (e.target.checked ? startLogAuto() : stopLogAuto()));

  $("btnLogin").addEventListener("click", submitLogin);
  $("loginPass").addEventListener("keydown", (e) => {
    if (e.key === "Enter") submitLogin();
  });

  $("confirmOk").addEventListener("click", () => settleConfirm(true));
  $("confirmCancel").addEventListener("click", () => settleConfirm(false));

  $("btnSaveEditor").addEventListener("click", (e) => saveEditor(e.currentTarget, false));
  $("btnSaveEditorReload").addEventListener("click", (e) => saveEditor(e.currentTarget, true));
  $("editorFile").addEventListener("change", (e) => loadEditorFile(e.target.value));
  $("btnTakeoverConfirm").addEventListener("click", (e) => confirmTakeover(e.currentTarget));

  // delegated row actions
  document.addEventListener("click", (e) => {
    const t = e.target.closest("[data-svc],[data-edit-frps],[data-del-frps],[data-edit-frpc],[data-del-frpc],[data-edit-caddy],[data-del-caddy],[data-edit-app],[data-takeover],[data-close],[data-copy],[data-copy-input],[data-copy-code]");
    if (!t) return;

    if (t.dataset.close) {
      closeModal(t.dataset.close);
      return;
    }
    if (t.dataset.copy) {
      const el = $(t.dataset.copy);
      if (el) copyText(el.textContent.trim());
      return;
    }
    if (t.dataset.copyInput) {
      copyText($(t.dataset.copyInput).value);
      return;
    }
    if (t.dataset.copyCode) {
      copyText($(t.dataset.copyCode).textContent);
      return;
    }
    if (t.dataset.svc) {
      if (t.dataset.action === "logs") {
        setLogTarget(t.dataset.svc);
        switchView("system");
        loadLogs();
        return;
      }
      serviceAction(t.dataset.svc, t.dataset.action, t);
      return;
    }
    if (t.dataset.editFrps || t.dataset.delFrps) {
      const id = t.dataset.editFrps || t.dataset.delFrps;
      const list = pick(state.data.config, "frps.services", []) || [];
      if (t.dataset.delFrps) deleteTunnel("frps", id, t);
      else openTunnelModal("frps", list.find((x) => x.id === id));
      return;
    }
    if (t.dataset.editFrpc || t.dataset.delFrpc) {
      const id = t.dataset.editFrpc || t.dataset.delFrpc;
      const list = pick(state.data.config, "frpc.tunnels", []) || [];
      if (t.dataset.delFrpc) deleteTunnel("frpc", id, t);
      else openTunnelModal("frpc", list.find((x) => x.id === id));
      return;
    }
    if (t.dataset.editCaddy || t.dataset.delCaddy) {
      const id = t.dataset.editCaddy || t.dataset.delCaddy;
      const list = pick(state.data.config, "caddy.rules", []) || [];
      if (t.dataset.delCaddy) deleteCaddy(id, t);
      else openCaddyModal(list.find((x) => x.id === id));
      return;
    }
    if (t.dataset.editApp) {
      openEditor(t.dataset.editApp);
      return;
    }
    if (t.dataset.takeover) openTakeover(t.dataset.takeover);
  });

  // backdrop / escape
  $$(".modal-backdrop").forEach((bd) => {
    bd.addEventListener("mousedown", (e) => {
      if (e.target !== bd) return;
      if (bd.id === "confirmModal" || bd.id === "loginModal") return;
      closeModal(bd.id);
    });
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (!$("confirmModal").hidden) return settleConfirm(false);
    const open = $$(".modal-backdrop").filter((m) => !m.hidden).pop();
    if (open && open.id !== "loginModal") closeModal(open.id);
  });
}

function randomToken(len) {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  const arr = new Uint8Array(len);
  crypto.getRandomValues(arr);
  return Array.from(arr, (b) => chars[b % chars.length]).join("");
}

/* ------------------------------------------------------------ boot */
window.addEventListener("DOMContentLoaded", async () => {
  applyTheme(document.documentElement.dataset.theme || "dark", false);
  bindEvents();
  setLogTarget("frps");
  switchView(viewFromHash(), false);
  await loadOverview();
  if (location.hash === "#/setup") openWizard();
  const cfg = state.data && state.data.config;
  if (cfg && cfg.setupCompleted === false && !localStorage.getItem("autof.skipWizard")) {
    openWizard();
  }
});
