let stateData = null;

async function apiFetch(url, options = {}) {
  const headers = Object.assign({ "Content-Type": "application/json" }, options.headers || {});
  const resp = await fetch(url, Object.assign({}, options, { headers }));
  if (resp.status === 401) {
    document.getElementById("loginModal").classList.remove("hidden");
    throw new Error("请先登录控制台");
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok || data.error) {
    throw new Error(data.error || `请求失败 (${resp.status})`);
  }
  return data;
}

function showToast(msg, type = "info") {
  const wrap = document.getElementById("toastContainer");
  const el = document.createElement("div");
  el.className = `toast ${type}`;
  el.textContent = msg;
  wrap.appendChild(el);
  setTimeout(() => el.remove(), 3400);
}

async function submitLogin() {
  const username = document.getElementById("loginUsername").value.trim();
  const password = document.getElementById("loginPassword").value;
  try {
    await apiFetch("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    document.getElementById("loginModal").classList.add("hidden");
    showToast("登录成功", "success");
    await loadOverview();
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function loadOverview() {
  try {
    const data = await apiFetch("/api/overview");
    stateData = data;
    renderAll(data);
  } catch (e) {
    if (!e.message.includes("登录")) {
      showToast(e.message, "error");
    }
  }
}

function renderAll(data) {
  const cfg = data.config;
  const svcs = data.services;
  const sys = data.systemInfo || {};

  document.getElementById("appVersion").textContent = "v" + (cfg.version || "2.0.0");
  if (sys.prettyName) {
    document.getElementById("distroBadge").textContent = `${sys.prettyName} (${sys.arch || ""})`;
  }

  // 渲染探测到的原服务列表与顶部提示
  renderDetectedApps(data.detectedApps || []);

  // 角色切换高亮
  applyRoleView(cfg.nodeRole || "both", false);

  // 网络卡片
  document.getElementById("v4Status").textContent = cfg.network.v4Status || "未探测";
  document.getElementById("pubIPv4").textContent = cfg.network.publicIPv4 || "无";
  document.getElementById("v6Status").textContent = cfg.network.v6Status || "未探测";
  document.getElementById("pubIPv6").textContent = cfg.network.publicIPv6 || "无";
  document.getElementById("localIPv4").textContent = cfg.network.localIPv4 || "127.0.0.1";
  document.getElementById("ipPrefSelect").value = cfg.network.ipPref || "4";

  // 服务状态卡
  renderServiceCard("frps", svcs.frps);
  document.getElementById("frpsPortMeta").textContent = cfg.frps.controlPort || 7000;

  renderServiceCard("frpc", svcs.frpc);
  document.getElementById("frpcServerMeta").textContent = cfg.frpc.serverAddr
    ? `${cfg.frpc.serverAddr}:${cfg.frpc.serverPort}`
    : "未设置";

  renderServiceCard("caddy", svcs.caddy);
  const enabledCaddyCount = (cfg.caddy.rules || []).filter((r) => r.enabled).length;
  document.getElementById("caddyRulesCount").textContent = `${enabledCaddyCount} 条`;

  // Tab 1: frps 参数与列表
  document.getElementById("frpsControlPort").value = cfg.frps.controlPort || 7000;
  document.getElementById("frpsToken").value = cfg.frps.token || "";
  document.getElementById("frpsAllowMode").value = cfg.frps.allowPortsMode || "unlimited";
  document.getElementById("frpsAllowRange").value = cfg.frps.allowPortsRange || "";
  toggleAllowRangeInput();
  renderFrpsServices(cfg);

  // Tab 2: frpc 参数与列表
  document.getElementById("frpcServerAddr").value = cfg.frpc.serverAddr || "";
  document.getElementById("frpcServerPort").value = cfg.frpc.serverPort || 7000;
  document.getElementById("frpcToken").value = cfg.frpc.token || "";
  document.getElementById("frpcTLSEnable").checked = cfg.frpc.tlsEnable !== false;
  document.getElementById("frpcAdminPortLabel").textContent = `127.0.0.1:${cfg.frpc.adminPort || 7400}`;
  renderFrpcTunnels(cfg);

  // Tab 3: Caddy 规则与参数
  document.getElementById("caddyHTTPPort").value = cfg.caddy.httpPort || 80;
  document.getElementById("caddyHTTPSPort").value = cfg.caddy.httpsPort || 443;
  document.getElementById("caddyEmail").value = cfg.caddy.email || "";
  document.getElementById("caddyDNSProvider").value = cfg.caddy.dnsProvider || "";
  document.getElementById("caddyDNSToken").value = cfg.caddy.dnsToken || "";
  document.getElementById("caddyDNSSecret").value = cfg.caddy.dnsSecret || "";
  renderCaddyRules(cfg);

  // Tab 4: WebUI 设置
  document.getElementById("webListenAddr").value = cfg.web.listenAddr || "0.0.0.0:7500";
  document.getElementById("webUsername").value = cfg.web.username || "admin";
  document.getElementById("webAuthEnabled").checked = cfg.web.authEnabled !== false;

  // 预设按钮渲染
  renderPresets(data.presets || []);
}

function renderServiceCard(key, st) {
  const badge = document.getElementById(`${key}Badge`);
  const meta = document.getElementById(`${key}Meta`);
  if (!st || !st.installed) {
    badge.className = "badge badge-warning";
    badge.textContent = "未安装";
    meta.textContent = "点击启动自动安装";
    return;
  }
  if (st.running) {
    badge.className = "badge badge-success";
    badge.textContent = "● 运行中";
    meta.textContent = `${st.version ? "v" + st.version : "已安装"}${st.pid ? " / PID " + st.pid : ""}`;
  } else {
    badge.className = "badge badge-danger";
    badge.textContent = "● 已停止";
    meta.textContent = st.version ? `v${st.version}` : "已安装";
  }
}

function publicAddr(cfg) {
  if (cfg.network.ipPref === "6") {
    return cfg.network.publicIPv6 || cfg.network.publicIPv4 || "VPS_IP";
  }
  return cfg.network.publicIPv4 || cfg.network.publicIPv6 || "VPS_IP";
}

function renderFrpsServices(cfg) {
  const tbody = document.getElementById("frpsServicesBody");
  const list = cfg.frps.services || [];
  if (list.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="empty-state">暂无穿透服务，点击右上角「+ 添加穿透服务」（内置 Minecraft、Web、NAS、SSH 等预设）</td></tr>`;
    return;
  }
  const pub = publicAddr(cfg);
  tbody.innerHTML = list
    .map((s) => {
      const accessHtml = s.caddyDomain
        ? `<span class="badge badge-purple">Caddy 反代</span> <span class="mono">${escapeHtml(s.caddyDomain)}</span> <span class="sub-hint">(→ :${s.remotePort})</span>`
        : `<span class="mono">${escapeHtml(pub)}:${s.remotePort}</span>`;
      const statusBadge = s.enabled
        ? `<span class="badge badge-success">启用</span>`
        : `<span class="badge badge-danger">停用</span>`;
      const typeBadge =
        s.type === "udp"
          ? `<span class="badge badge-warning">UDP</span>`
          : `<span class="badge badge-info">TCP</span>`;
      return `<tr>
        <td><strong>${escapeHtml(s.name)}</strong>${s.remark ? `<div class="sub-hint">${escapeHtml(s.remark)}</div>` : ""}</td>
        <td>${typeBadge}</td>
        <td class="mono">${escapeHtml(s.localIP || "127.0.0.1")}:${s.localPort}</td>
        <td class="mono">${s.remotePort}</td>
        <td>${accessHtml}</td>
        <td>${statusBadge}</td>
        <td>
          <div class="btn-group">
            <button class="btn btn-sm" onclick='openTunnelModal("frps", ${JSON.stringify(s)})'>编辑</button>
            <button class="btn btn-sm btn-danger" onclick='deleteTunnel("frps", "${s.id}")'>删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderFrpcTunnels(cfg) {
  const tbody = document.getElementById("frpcTunnelsBody");
  const list = cfg.frpc.tunnels || [];
  if (list.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="empty-state">暂无本地穿透隧道，可点击「一键导入连接码 / frpc.toml」或手动添加</td></tr>`;
    return;
  }
  const srv = cfg.frpc.serverAddr || "VPS_IP";
  tbody.innerHTML = list
    .map((s) => {
      const accessHtml = s.caddyDomain
        ? `<span class="badge badge-purple">本地反代</span> <span class="mono">${escapeHtml(s.caddyDomain)}</span>`
        : `<span class="mono">${escapeHtml(srv)}:${s.remotePort}</span>`;
      const statusBadge = s.enabled
        ? `<span class="badge badge-success">启用</span>`
        : `<span class="badge badge-danger">停用</span>`;
      const typeBadge =
        s.type === "udp"
          ? `<span class="badge badge-warning">UDP</span>`
          : `<span class="badge badge-info">TCP</span>`;
      return `<tr>
        <td><strong>${escapeHtml(s.name)}</strong>${s.remark ? `<div class="sub-hint">${escapeHtml(s.remark)}</div>` : ""}</td>
        <td>${typeBadge}</td>
        <td class="mono">${escapeHtml(s.localIP || "127.0.0.1")}:${s.localPort}</td>
        <td class="mono">${s.remotePort}</td>
        <td>${accessHtml}</td>
        <td>${statusBadge}</td>
        <td>
          <div class="btn-group">
            <button class="btn btn-sm" onclick='openTunnelModal("frpc", ${JSON.stringify(s)})'>编辑</button>
            <button class="btn btn-sm btn-danger" onclick='deleteTunnel("frpc", "${s.id}")'>删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderCaddyRules(cfg) {
  const tbody = document.getElementById("caddyRulesBody");
  const list = cfg.caddy.rules || [];
  if (list.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="empty-state">暂无 Caddy 反代规则，点击右上角「+ 添加反代规则」或在穿透服务中开启域名联动</td></tr>`;
    return;
  }
  const tlsLabels = {
    auto: `<span class="badge badge-success">自动公网 HTTPS</span>`,
    dns: `<span class="badge badge-purple">DNS API 证书</span>`,
    custom: `<span class="badge badge-info">已迁移原证书</span>`,
    internal: `<span class="badge badge-warning">内网自签 HTTPS</span>`,
    http: `<span class="badge badge-info">纯 HTTP</span>`,
  };
  tbody.innerHTML = list
    .map((r) => {
      const originTag = r.linkedTunnelId
        ? `<span class="badge badge-info">穿透联动</span>`
        : `<span class="badge badge-purple">独立反代</span>`;
      const features = [];
      if (r.enableEncode) features.push("压缩");
      if (r.skipTlsVerify) features.push("跳过后端证书校验");
      if (r.extraDirectives) features.push("自定义指令");
      const statusBadge = r.enabled
        ? `<span class="badge badge-success">启用</span>`
        : `<span class="badge badge-danger">停用</span>`;
      return `<tr>
        <td><strong>${escapeHtml(r.name)}</strong> <div style="margin-top:3px;">${originTag}</div></td>
        <td class="mono">${escapeHtml(r.listen)}</td>
        <td class="mono">${escapeHtml(r.upstream)}</td>
        <td>${tlsLabels[r.tlsMode] || tlsLabels.auto}</td>
        <td>${features.length ? features.join(" · ") : "-"}</td>
        <td>${statusBadge}</td>
        <td>
          <div class="btn-group">
            <button class="btn btn-sm" onclick='openCaddyRuleModal(${JSON.stringify(r)})'>编辑</button>
            <button class="btn btn-sm btn-danger" onclick='deleteCaddyRule("${r.id}")'>删除</button>
          </div>
        </td>
      </tr>`;
    })
    .join("");
}

function renderPresets(presets) {
  const wrap = document.getElementById("presetChips");
  wrap.innerHTML = presets
    .map(
      (p) =>
        `<span class="preset-chip" title="${escapeHtml(p.description)}" onclick='applyPreset(${JSON.stringify(p)})'>${escapeHtml(p.label)}</span>`
    )
    .join("");
}

function applyPreset(p) {
  document.getElementById("tunName").value = p.defaultName;
  document.getElementById("tunType").value = p.type;
  document.getElementById("tunLocalPort").value = p.localPort;
  document.getElementById("tunRemotePort").value = p.remotePort;
  document.getElementById("tunRemark").value = p.label;
  if (p.suggestCaddy && !document.getElementById("tunCaddyDomain").value) {
    document.getElementById("tunCaddyDomain").focus();
  }
}

function syncDefaultRemotePort() {
  const lp = document.getElementById("tunLocalPort").value;
  const rpInput = document.getElementById("tunRemotePort");
  if (!rpInput.dataset.touched && lp) {
    rpInput.value = lp;
  }
}

function switchTab(tab) {
  ["frps", "frpc", "caddy", "scanner", "system"].forEach((t) => {
    const btn = document.getElementById("tabBtn" + capitalize(t));
    const panel = document.getElementById("tabPanel" + capitalize(t));
    if (!btn || !panel) return;
    if (t === tab) {
      btn.classList.add("active");
      panel.classList.remove("hidden");
    } else {
      btn.classList.remove("active");
      panel.classList.add("hidden");
    }
  });
}

function switchRole(role) {
  applyRoleView(role, true);
  apiFetch("/api/net/pref", {
    method: "POST",
    body: JSON.stringify({ nodeRole: role }),
  }).catch(() => {});
}

function applyRoleView(role, autoSwitchTab) {
  document.querySelectorAll(".role-btn").forEach((b) => {
    b.classList.toggle("active", b.dataset.role === role);
  });
  const cardFrps = document.getElementById("cardFrps");
  const cardFrpc = document.getElementById("cardFrpc");
  const tabFrps = document.getElementById("tabBtnFrps");
  const tabFrpc = document.getElementById("tabBtnFrpc");

  cardFrps.classList.remove("hidden");
  cardFrpc.classList.remove("hidden");
  tabFrps.classList.remove("hidden");
  tabFrpc.classList.remove("hidden");

  if (role === "vps") {
    cardFrpc.classList.add("hidden");
    tabFrpc.classList.add("hidden");
    if (autoSwitchTab) switchTab("frps");
  } else if (role === "local") {
    cardFrps.classList.add("hidden");
    tabFrps.classList.add("hidden");
    if (autoSwitchTab) switchTab("frpc");
  }
}

async function refreshNetwork() {
  showToast("正在探测公网 IPv4 / IPv6...");
  try {
    const data = await apiFetch("/api/net/refresh", { method: "POST" });
    stateData = data;
    renderAll(data);
    showToast("网络环境探测完成", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function changeIPPref(ipPref) {
  try {
    const data = await apiFetch("/api/net/pref", {
      method: "POST",
      body: JSON.stringify({ ipPref }),
    });
    stateData = data;
    renderAll(data);
    showToast("网络偏好已更新", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function serviceAction(target, action, extra = {}) {
  const actionNames = { start: "启动/安装", restart: "重启", reload: "热重载", stop: "停止", install: "安装" };
  showToast(`正在执行 ${target} ${actionNames[action] || action}...`);
  try {
    const data = await apiFetch("/api/service/action", {
      method: "POST",
      body: JSON.stringify(Object.assign({ target, action }, extra)),
    });
    stateData = data;
    renderAll(data);
    showToast(`${target} ${actionNames[action] || action}成功`, "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

function toggleAllowRangeInput() {
  const mode = document.getElementById("frpsAllowMode").value;
  document.getElementById("frpsAllowRangeWrap").style.opacity = mode === "range" ? "1" : "0.45";
}

function randomToken(inputId) {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 24; i++) {
    out += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  document.getElementById(inputId).value = out;
}

async function saveFrpsSettings() {
  const controlPort = parseInt(document.getElementById("frpsControlPort").value, 10);
  const token = document.getElementById("frpsToken").value.trim();
  const allowPortsMode = document.getElementById("frpsAllowMode").value;
  const allowPortsRange = document.getElementById("frpsAllowRange").value.trim();
  try {
    const data = await apiFetch("/api/frps/settings", {
      method: "POST",
      body: JSON.stringify({ controlPort, token, allowPortsMode, allowPortsRange }),
    });
    stateData = data;
    renderAll(data);
    showToast("frps 参数已保存并生效", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function saveFrpcSettings() {
  const serverAddr = document.getElementById("frpcServerAddr").value.trim();
  const serverPort = parseInt(document.getElementById("frpcServerPort").value, 10);
  const token = document.getElementById("frpcToken").value.trim();
  const tlsEnable = document.getElementById("frpcTLSEnable").checked;
  try {
    const data = await apiFetch("/api/frpc/settings", {
      method: "POST",
      body: JSON.stringify({ serverAddr, serverPort, token, tlsEnable }),
    });
    stateData = data;
    renderAll(data);
    showToast("frpc 连接配置已保存并热重载", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

function openTunnelModal(scope, item) {
  document.getElementById("tunTargetScope").value = scope;
  document.getElementById("tunnelModalTitle").textContent = item
    ? `编辑穿透服务 (${item.name})`
    : scope === "frps"
    ? "添加穿透服务 (远程 VPS 端)"
    : "添加本地穿透隧道 (本地客户端)";

  const rpInput = document.getElementById("tunRemotePort");
  rpInput.dataset.touched = item ? "1" : "";
  rpInput.oninput = () => {
    rpInput.dataset.touched = "1";
  };

  document.getElementById("tunID").value = item ? item.id : "";
  document.getElementById("tunName").value = item ? item.name : "";
  document.getElementById("tunType").value = item ? item.type : "tcp";
  document.getElementById("tunLocalIP").value = item ? item.localIP || "127.0.0.1" : "127.0.0.1";
  document.getElementById("tunLocalPort").value = item ? item.localPort : "";
  document.getElementById("tunRemotePort").value = item ? item.remotePort : "";
  document.getElementById("tunRemark").value = item ? item.remark || "" : "";
  document.getElementById("tunCaddyDomain").value = item ? item.caddyDomain || "" : "";
  document.getElementById("tunCaddyTLSMode").value = item ? item.caddyTlsMode || "auto" : "auto";
  document.getElementById("tunEnabled").checked = item ? item.enabled !== false : true;

  document.getElementById("tunnelModal").classList.remove("hidden");
}

async function submitTunnelForm() {
  const scope = document.getElementById("tunTargetScope").value;
  const payload = {
    id: document.getElementById("tunID").value,
    name: document.getElementById("tunName").value.trim(),
    type: document.getElementById("tunType").value,
    localIP: document.getElementById("tunLocalIP").value.trim() || "127.0.0.1",
    localPort: parseInt(document.getElementById("tunLocalPort").value, 10),
    remotePort: parseInt(document.getElementById("tunRemotePort").value, 10),
    remark: document.getElementById("tunRemark").value.trim(),
    caddyDomain: document.getElementById("tunCaddyDomain").value.trim(),
    caddyTlsMode: document.getElementById("tunCaddyTLSMode").value,
    enabled: document.getElementById("tunEnabled").checked,
  };
  const endpoint = scope === "frps" ? "/api/frps/service" : "/api/frpc/tunnel";
  try {
    const data = await apiFetch(endpoint, {
      method: "POST",
      body: JSON.stringify(payload),
    });
    stateData = data;
    renderAll(data);
    closeModal("tunnelModal");
    showToast("穿透规则已保存并应用", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function deleteTunnel(scope, id) {
  if (!confirm("确定要删除这条穿透规则吗？关联的 Caddy 反代也会一并移除。")) return;
  const endpoint = scope === "frps" ? `/api/frps/service?id=${encodeURIComponent(id)}` : `/api/frpc/tunnel?id=${encodeURIComponent(id)}`;
  try {
    const data = await apiFetch(endpoint, { method: "DELETE" });
    stateData = data;
    renderAll(data);
    showToast("已删除穿透规则", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

function openCaddyRuleModal(rule) {
  document.getElementById("caddyModalTitle").textContent = rule ? `编辑反代规则 (${rule.name})` : "添加 Caddy 反代规则";
  document.getElementById("caddyRuleID").value = rule ? rule.id : "";
  document.getElementById("caddyRuleName").value = rule ? rule.name : "";
  document.getElementById("caddyRuleListen").value = rule ? rule.listen : "";
  document.getElementById("caddyRuleUpstream").value = rule ? rule.upstream : "";
  document.getElementById("caddyRuleTLSMode").value = rule ? rule.tlsMode || "auto" : "auto";
  document.getElementById("caddyRuleCertFile").value = rule ? rule.certFile || "" : "";
  document.getElementById("caddyRuleKeyFile").value = rule ? rule.keyFile || "" : "";
  toggleCustomCertInputs();
  document.getElementById("caddyRuleEncode").checked = rule ? rule.enableEncode !== false : true;
  document.getElementById("caddyRuleSkipVerify").checked = rule ? !!rule.skipTlsVerify : false;
  document.getElementById("caddyRuleEnabled").checked = rule ? rule.enabled !== false : true;
  document.getElementById("caddyRuleExtra").value = rule ? rule.extraDirectives || "" : "";

  // 填充已有穿透服务下拉列表供快捷选择
  const sel = document.getElementById("caddyQuickTunnelSelect");
  const allTunnels = [];
  if (stateData && stateData.config) {
    (stateData.config.frps.services || []).forEach((s) =>
      allTunnels.push({ label: `[VPS 穿透] ${s.name} (127.0.0.1:${s.remotePort})`, name: s.name, upstream: `127.0.0.1:${s.remotePort}` })
    );
    (stateData.config.frpc.tunnels || []).forEach((s) =>
      allTunnels.push({ label: `[本地隧道] ${s.name} (${s.localIP || "127.0.0.1"}:${s.localPort})`, name: s.name, upstream: `${s.localIP || "127.0.0.1"}:${s.localPort}` })
    );
  }
  sel.innerHTML =
    `<option value="">-- 手动输入本机或局域网目标地址 --</option>` +
    allTunnels.map((t, idx) => `<option value="${idx}">${escapeHtml(t.label)}</option>`).join("");
  sel.dataset.tunnels = JSON.stringify(allTunnels);

  document.getElementById("caddyModal").classList.remove("hidden");
}

function applyQuickTunnelToCaddy(idxStr) {
  if (idxStr === "") return;
  const sel = document.getElementById("caddyQuickTunnelSelect");
  const list = JSON.parse(sel.dataset.tunnels || "[]");
  const item = list[parseInt(idxStr, 10)];
  if (!item) return;
  document.getElementById("caddyRuleName").value = item.name;
  document.getElementById("caddyRuleUpstream").value = item.upstream;
}

function quickProxySelfWebUI() {
  const listenAddr = (stateData && stateData.config && stateData.config.web.listenAddr) || "0.0.0.0:7500";
  const port = listenAddr.split(":").pop() || "7500";
  openCaddyRuleModal({
    id: "",
    name: "Autofrp 控制台",
    listen: "",
    upstream: `127.0.0.1:${port}`,
    tlsMode: "auto",
    enableEncode: true,
    enabled: true,
  });
}

function toggleCustomCertInputs() {
  const mode = document.getElementById("caddyRuleTLSMode").value;
  document.getElementById("caddyCustomCertBox").classList.toggle("hidden", mode !== "custom");
}

async function submitCaddyRuleForm() {
  const payload = {
    id: document.getElementById("caddyRuleID").value,
    name: document.getElementById("caddyRuleName").value.trim(),
    listen: document.getElementById("caddyRuleListen").value.trim(),
    upstream: document.getElementById("caddyRuleUpstream").value.trim(),
    tlsMode: document.getElementById("caddyRuleTLSMode").value,
    certFile: document.getElementById("caddyRuleCertFile").value.trim(),
    keyFile: document.getElementById("caddyRuleKeyFile").value.trim(),
    enableEncode: document.getElementById("caddyRuleEncode").checked,
    skipTlsVerify: document.getElementById("caddyRuleSkipVerify").checked,
    enabled: document.getElementById("caddyRuleEnabled").checked,
    extraDirectives: document.getElementById("caddyRuleExtra").value.trim(),
  };
  try {
    const data = await apiFetch("/api/caddy/rule", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    stateData = data;
    renderAll(data);
    closeModal("caddyModal");
    showToast("Caddy 反代规则已保存并热重载", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function deleteCaddyRule(id) {
  if (!confirm("确定要删除这条 Caddy 反代规则吗？")) return;
  try {
    const data = await apiFetch(`/api/caddy/rule?id=${encodeURIComponent(id)}`, { method: "DELETE" });
    stateData = data;
    renderAll(data);
    showToast("已删除 Caddy 规则", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function saveCaddySettings() {
  const payload = {
    httpPort: parseInt(document.getElementById("caddyHTTPPort").value, 10) || 80,
    httpsPort: parseInt(document.getElementById("caddyHTTPSPort").value, 10) || 443,
    email: document.getElementById("caddyEmail").value.trim(),
    dnsProvider: document.getElementById("caddyDNSProvider").value,
    dnsToken: document.getElementById("caddyDNSToken").value.trim(),
    dnsSecret: document.getElementById("caddyDNSSecret").value.trim(),
  };
  try {
    const data = await apiFetch("/api/caddy/settings", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    stateData = data;
    renderAll(data);
    showToast("Caddy 全局设置已保存", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function installCaddyWithDNS() {
  const dnsProvider = document.getElementById("caddyDNSProvider").value;
  await saveCaddySettings();
  await serviceAction("caddy", "install", { dnsProvider });
}

function openExportModal() {
  if (!stateData || !stateData.exports) return;
  document.getElementById("exportConnCodeInput").value = stateData.exports.connCode || "";
  document.getElementById("exportFrpcTomlBox").textContent = stateData.exports.frpcExportToml || "";
  document.getElementById("exportModal").classList.remove("hidden");
}

function openImportModal() {
  document.getElementById("importRawInput").value = "";
  document.getElementById("importModal").classList.remove("hidden");
}

async function submitImportConfig() {
  const input = document.getElementById("importRawInput").value.trim();
  const merge = document.getElementById("importMergeTunnels").checked;
  if (!input) {
    showToast("请先粘贴连接码或 frpc.toml 内容", "error");
    return;
  }
  try {
    const data = await apiFetch("/api/frpc/import", {
      method: "POST",
      body: JSON.stringify({ input, merge }),
    });
    stateData = data;
    renderAll(data);
    closeModal("importModal");
    showToast("导入成功！已自动更新本地 frpc 配置", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

function openPreviewModal(type) {
  if (!stateData || !stateData.exports) return;
  const titles = {
    frps: "frps.toml（远程 VPS 服务端配置）",
    frpcClient: "frpc.toml（本地服务器客户端运行配置）",
    caddy: "Caddyfile（Caddy 反向代理完整配置）",
  };
  const contents = {
    frps: stateData.exports.frpsToml,
    frpcClient: stateData.exports.frpcClientToml,
    caddy: stateData.exports.caddyfile,
  };
  document.getElementById("previewModalTitle").textContent = titles[type] || "配置预览";
  document.getElementById("previewCodeBox").textContent = contents[type] || "";
  document.getElementById("previewModal").classList.remove("hidden");
}

function openLogsTab(target) {
  switchTab("system");
  loadServiceLogs(target);
}

async function loadServiceLogs(target) {
  const box = document.getElementById("serviceLogBox");
  box.textContent = `正在加载 ${target} 日志...`;
  try {
    const data = await apiFetch(`/api/service/logs?target=${encodeURIComponent(target)}&lines=120`);
    box.textContent = data.logs || "暂无日志";
    box.scrollTop = box.scrollHeight;
  } catch (e) {
    box.textContent = "读取日志失败: " + e.message;
  }
}

async function saveWebSettings() {
  const listenAddr = document.getElementById("webListenAddr").value.trim();
  const username = document.getElementById("webUsername").value.trim();
  const password = document.getElementById("webPassword").value;
  const authEnabled = document.getElementById("webAuthEnabled").checked;
  try {
    const data = await apiFetch("/api/system/web", {
      method: "POST",
      body: JSON.stringify({ listenAddr, username, password, authEnabled }),
    });
    stateData = data;
    renderAll(data);
    document.getElementById("webPassword").value = "";
    showToast("WebUI 设置已保存", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

function closeModal(id) {
  document.getElementById(id).classList.add("hidden");
}

async function copyTextById(id, isPre = false) {
  const el = document.getElementById(id);
  const text = isPre ? el.textContent : el.value;
  try {
    await navigator.clipboard.writeText(text);
    showToast("已复制到剪贴板", "success");
  } catch (_) {
    const ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
    showToast("已复制到剪贴板", "success");
  }
}

function capitalize(s) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function escapeHtml(str) {
  if (!str) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

window.addEventListener("DOMContentLoaded", () => {
  loadOverview();
});

// ==================== 原服务探测、在线编辑与一键吞并迁移 ====================
function renderDetectedApps(apps) {
  const externalApps = apps.filter((a) => !a.managedByAutof);
  const banner = document.getElementById("detectedBanner");
  const bannerText = document.getElementById("detectedBannerText");
  if (externalApps.length > 0) {
    banner.classList.remove("hidden");
    bannerText.textContent = externalApps.map((a) => `${a.name}${a.running ? " (运行中)" : ""}`).join("、");
  } else {
    banner.classList.add("hidden");
  }

  const tbody = document.getElementById("detectedAppsBody");
  if (!apps || apps.length === 0) {
    tbody.innerHTML = `<tr><td colspan="6" class="empty-state">当前未在服务器上检测到其他 NPM、Nginx、原版 Caddy 或 frp 实例</td></tr>`;
    return;
  }

  tbody.innerHTML = apps
    .map((app) => {
      const stBadge = app.running
        ? `<span class="badge badge-success">● 运行中</span>`
        : `<span class="badge badge-warning">○ 未运行</span>`;
      const meta = app.container
        ? `Docker: <span class="mono">${escapeHtml(app.container)}</span>`
        : app.pid
        ? `PID: <span class="mono">${app.pid}</span>`
        : escapeHtml(app.binaryOrImage || "-");
      const ports = (app.listenPorts || []).length ? `<span class="mono">${escapeHtml(app.listenPorts.join(", "))}</span>` : "-";
      const cfgFiles = app.configFiles || [];
      const primaryCfg = app.primaryConfig ? `<span class="mono">${escapeHtml(app.primaryConfig)}</span>` : "未找到配置文件";
      const managedTag = app.managedByAutof
        ? `<span class="badge badge-info">Autofrp 已托管</span>`
        : `<span class="badge badge-warning">外部 / 原生服务</span>`;

      const editBtn =
        cfgFiles.length > 0
          ? `<button class="btn btn-sm" onclick='openExtEditorModal(${JSON.stringify(app)})'>✏️ 查看/编辑配置</button>`
          : "";
      const takeoverBtn =
        !app.managedByAutof && (app.canImport || app.running || cfgFiles.length > 0)
          ? `<button class="btn btn-sm btn-primary" onclick='openTakeoverModal("${escapeHtml(app.id)}")'>🚀 一键吞并迁移</button>`
          : "";

      return `<tr>
        <td><strong>${escapeHtml(app.name)}</strong><div style="margin-top:4px;">${managedTag}</div></td>
        <td>${stBadge}<div class="sub-hint" style="margin-top:3px;">${meta}</div></td>
        <td>${ports}</td>
        <td>${primaryCfg}${cfgFiles.length > 1 ? `<div class="sub-hint">共 ${cfgFiles.length} 个配置文件</div>` : ""}</td>
        <td class="sub-hint">${escapeHtml(app.note || "-")}</td>
        <td><div class="btn-group">${editBtn}${takeoverBtn}</div></td>
      </tr>`;
    })
    .join("");
}

async function openExtEditorModal(app) {
  document.getElementById("extEditorTitle").textContent = `在线编辑配置文件 · ${app.name}`;
  document.getElementById("extEditorAppID").value = app.id;
  const sel = document.getElementById("extEditorFileSelect");
  const files = app.configFiles || [];
  sel.innerHTML = files.map((f) => `<option value="${escapeHtml(f)}">${escapeHtml(f)}</option>`).join("");
  document.getElementById("extEditorModal").classList.remove("hidden");
  if (files.length > 0) {
    await loadExternalFileContent(files[0]);
  }
}

async function loadExternalFileContent(path) {
  const ta = document.getElementById("extEditorTextarea");
  ta.value = "正在读取文件...";
  try {
    const data = await apiFetch(`/api/scanner/file?path=${encodeURIComponent(path)}`);
    ta.value = data.content || "";
  } catch (e) {
    ta.value = "";
    showToast(e.message, "error");
  }
}

async function saveExternalFileContent(reload) {
  const path = document.getElementById("extEditorFileSelect").value;
  const content = document.getElementById("extEditorTextarea").value;
  const appId = document.getElementById("extEditorAppID").value;
  try {
    const data = await apiFetch("/api/scanner/file", {
      method: "POST",
      body: JSON.stringify({ path, content, reload, appId }),
    });
    stateData = data;
    renderAll(data);
    closeModal("extEditorModal");
    showToast(reload ? "配置已保存并重载服务（已生成 .autof.bak 备份）" : "配置文件已保存（已生成 .autof.bak 备份）", "success");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function openTakeoverModal(appId) {
  showToast("正在扫描并分析原服务配置与 SSL 证书...");
  try {
    const plan = await apiFetch(`/api/scanner/takeover-preview?appId=${encodeURIComponent(appId)}`);
    document.getElementById("takeoverAppID").value = plan.appId;
    document.getElementById("takeoverModalTitle").textContent = `🚀 确认吞并迁移 · ${plan.appName}`;

    const rules = plan.discoveredRules || [];
    const certs = plan.discoveredCerts || [];
    const tunnels = plan.discoveredTunnels || [];
    const steps = plan.cleanupSteps || [];

    let html = `<div style="margin-bottom:10px;">
      即将把 <strong>${escapeHtml(plan.appName)}</strong> 的配置与证书迁移至 <span class="mono">${escapeHtml(plan.targetStateDir)}</span>：
    </div>`;

    if (rules.length > 0) {
      html += `<div style="margin-bottom:10px;">
        <strong>识别到的反向代理站点 (${rules.length} 条)：</strong>
        <div class="code-box" style="max-height:140px;margin-top:4px;">${rules
          .map((r) => `${r.hasSsl ? "🔒 [HTTPS]" : "🌐 [HTTP] "} ${escapeHtml(r.domain)}  ──►  ${escapeHtml(r.upstream)}`)
          .join("\n")}</div>
      </div>`;
    }

    if (certs.length > 0) {
      html += `<div style="margin-bottom:10px;">
        <strong>识别到的 SSL 证书 (${certs.length} 套，将自动复制到 <span class="mono">${escapeHtml(plan.certsTargetDir)}</span>)：</strong>
        <div class="code-box" style="max-height:110px;margin-top:4px;">${certs
          .map((c) => `📜 ${escapeHtml(c.domain)}: ${escapeHtml(c.srcCertPath)} ──► ${escapeHtml(c.destCertPath)}`)
          .join("\n")}</div>
      </div>`;
    }

    if (tunnels.length > 0 || plan.discoveredFrpsPort) {
      html += `<div style="margin-bottom:10px;">
        <strong>识别到的 FRP 穿透配置：</strong>
        <div class="code-box" style="max-height:110px;margin-top:4px;">${
          plan.discoveredFrpsPort ? `frps 控制端口: ${plan.discoveredFrpsPort}\n` : ""
        }${tunnels.map((t) => `[${t.type}] ${t.name}: ${t.localIP}:${t.localPort} -> :${t.remotePort}`).join("\n")}</div>
      </div>`;
    }

    html += `<div>
      <strong>将执行的吞并步骤：</strong>
      <ul style="margin:6px 0 0 18px;color:var(--text-muted);font-size:13px;">
        ${steps.map((s) => `<li>${escapeHtml(s)}</li>`).join("")}
      </ul>
    </div>`;

    document.getElementById("takeoverSummaryBox").innerHTML = html;
    document.getElementById("takeoverModal").classList.remove("hidden");
  } catch (e) {
    showToast(e.message, "error");
  }
}

async function confirmAndExecuteTakeover() {
  const payload = {
    appId: document.getElementById("takeoverAppID").value,
    useImportedCerts: document.getElementById("tkUseImportedCerts").checked,
    stopOldService: document.getElementById("tkStopOldService").checked,
    removeOldService: document.getElementById("tkRemoveOldService").checked,
    startAutofService: document.getElementById("tkStartAutof").checked,
  };
  showToast("正在执行配置与证书迁移并接管服务...");
  try {
    const data = await apiFetch("/api/scanner/takeover", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    stateData = data;
    renderAll(data);
    closeModal("takeoverModal");
    const res = data.takeoverResult || {};
    showToast(
      `吞并完成！已导入反代 ${res.importedRules || 0} 条、迁移证书 ${res.copiedCerts || 0} 套`,
      "success"
    );
    if (res.importedRules > 0) {
      switchTab("caddy");
    }
  } catch (e) {
    showToast(e.message, "error");
  }
}
