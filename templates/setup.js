const ROUTER_PORT = "5000";

function loginURL() {
  return `http://${window.location.hostname}:${ROUTER_PORT}/login`;
}

function showNotification(message, isError = false) {
  const box = document.getElementById("infoBox");
  box.textContent = message;
  box.style.display = "block";
  box.style.opacity = "1";
  box.style.backgroundColor = isError ? "#c62828" : "#4caf50";
  setTimeout(() => {
    box.style.opacity = "0";
    setTimeout(() => { box.style.display = "none"; }, 500);
  }, 5000);
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function loadSetupStatus() {
  const r = await fetch("/setup/status", { cache: "no-store" });
  if (!r.ok) throw new Error("Failed to load network status");
  return r.json();
}

async function loadSetupProgress() {
  const r = await fetch("/setup/progress", { cache: "no-store" });
  if (!r.ok) throw new Error("Failed to load progress");
  return r.json();
}

function renderNetworkSummary(snapshot) {
  const routes = snapshot.routing || {};
  const wan = snapshot.interfaces?.find((i) => i.name === routes.default_interface);
  const wanIPs = wan?.ipv4?.length ? wan.ipv4.join(", ") : "DHCP / not assigned yet";

  document.getElementById("networkSummary").innerHTML = [
    `Default route: ${routes.default_interface || "none"} via ${routes.default_gateway || "n/a"}`,
    `WAN: ${wanIPs}`,
  ].map((l) => `<div>${l}</div>`).join("");

  const ips = snapshot.management_ips || [];
  document.getElementById("accessUrls").innerHTML = ips.length
    ? `Setup: <code>${window.location.origin}/setup</code><br>Dashboard: <code>${loginURL()}</code>`
    : "Connect network, then open http://&lt;device-ip&gt;:4000/setup";
}

function populateInterfaceSelects(snapshot) {
  const wanSelect = document.getElementById("wanInterface");
  const lanSelect = document.getElementById("lanInterface");
  wanSelect.innerHTML = "";
  lanSelect.innerHTML = "";

  const defaultWAN =
    snapshot.config?.wan_interface ||
    snapshot.routing?.default_interface ||
    snapshot.interfaces?.find((i) => i.is_default_route)?.name ||
    "";
  const defaultLAN =
    snapshot.config?.lan_interface ||
    snapshot.interfaces?.find((i) => i.name !== defaultWAN)?.name ||
    "";

  for (const iface of snapshot.interfaces || []) {
    for (const sel of [wanSelect, lanSelect]) {
      const opt = document.createElement("option");
      opt.value = iface.name;
      const ips = iface.ipv4?.length ? iface.ipv4.join(", ") : "no IP";
      opt.textContent = `${iface.name}: ${ips}`;
      if (sel === wanSelect && iface.name === defaultWAN) opt.selected = true;
      if (sel === lanSelect && iface.name === defaultLAN) opt.selected = true;
      sel.appendChild(opt);
    }
  }

  const suggested = snapshot.suggested_lan || {};
  if (suggested.address) document.getElementById("lanAddress").value = suggested.address;
  if (suggested.prefix) document.getElementById("lanPrefix").value = suggested.prefix;
  if (suggested.dhcp_start) document.getElementById("dhcpStart").value = suggested.dhcp_start;
  if (suggested.dhcp_end) document.getElementById("dhcpEnd").value = suggested.dhcp_end;
  if (snapshot.hostname) document.getElementById("tailscaleHost").value = snapshot.hostname;

  wanSelect.onchange = async () => {
    const r = await fetch(`/setup/status?wan=${encodeURIComponent(wanSelect.value)}`);
    const data = await r.json();
    const s = data.suggested_lan || {};
    if (s.address) document.getElementById("lanAddress").value = s.address;
    if (s.prefix) document.getElementById("lanPrefix").value = s.prefix;
    if (s.dhcp_start) document.getElementById("dhcpStart").value = s.dhcp_start;
    if (s.dhcp_end) document.getElementById("dhcpEnd").value = s.dhcp_end;
  };
}

let logLineCount = 0;

function renderLog(progress) {
  const lines = progress.lines || [];
  const log = document.getElementById("setupLog");
  document.getElementById("setupLogPanel").hidden = false;

  for (let i = logLineCount; i < lines.length; i++) {
    const line = lines[i];
    const span = document.createElement("span");
    const ts = line.at ? ` [${line.at}]` : "";
    const text = line.status === "ok" ? `✓ [${line.step}] ${line.detail}${ts}`
      : line.status === "error" ? `✗ [${line.step}] ${line.detail}${ts}`
      : `[${line.step}] ${line.detail || line.status}${ts}`;
    if (line.status === "ok") span.className = "log-ok";
    else if (line.status === "error") span.className = "log-error";
    else if (line.status === "warn") span.className = "log-warn";
    else span.className = "log-running";
    span.textContent = text + "\n";
    log.appendChild(span);
  }
  logLineCount = lines.length;
  log.scrollTop = log.scrollHeight;
}

function showDone(message) {
  document.getElementById("setupForm").hidden = true;
  document.getElementById("completedPanel").hidden = false;
  document.getElementById("completedMessage").textContent = message || "Setup complete.";
  const link = document.getElementById("dashboardLink");
  link.href = loginURL();
  link.textContent = loginURL();
}

async function pollUntilDone() {
  const deadline = Date.now() + 20 * 60 * 1000;
  while (Date.now() < deadline) {
    let progress;
    try {
      progress = await loadSetupProgress();
    } catch {
      await sleep(2000);
      continue;
    }
    renderLog(progress);
    if (progress.error) throw new Error(progress.error);
    if (progress.complete) return progress;
    await sleep(2000);
  }
  throw new Error("Setup timed out");
}

async function initSetup() {
  const snapshot = await loadSetupStatus();
  renderNetworkSummary(snapshot);

  if (snapshot.configured) {
    showDone("Router is configured.");
    const progress = await loadSetupProgress().catch(() => null);
    if (progress?.lines?.length) renderLog(progress);
    return;
  }

  populateInterfaceSelects(snapshot);

  const progress = await loadSetupProgress().catch(() => null);
  if (progress?.running) {
    document.getElementById("applyBtn").disabled = true;
    document.getElementById("applyBtn").textContent = "Installing...";
    document.getElementById("setupForm").style.opacity = "0.6";
    try {
      const done = await pollUntilDone();
      showDone(done.message);
      showNotification("Setup complete");
    } catch (e) {
      showNotification(e.message, true);
      document.getElementById("applyBtn").disabled = false;
      document.getElementById("applyBtn").textContent = "Install & Configure";
      document.getElementById("setupForm").style.opacity = "1";
    }
  }
}

document.getElementById("setupForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const btn = document.getElementById("applyBtn");
  btn.disabled = true;
  btn.textContent = "Installing...";
  document.getElementById("setupLog").textContent = "";
  logLineCount = 0;

  const payload = {
    wan_interface: document.getElementById("wanInterface").value,
    lan_interface: document.getElementById("lanInterface").value,
    lan_address: document.getElementById("lanAddress").value.trim(),
    lan_prefix: parseInt(document.getElementById("lanPrefix").value, 10),
    dhcp_range_start: document.getElementById("dhcpStart").value.trim(),
    dhcp_range_end: document.getElementById("dhcpEnd").value.trim(),
    dhcp_lease_hours: 12,
    tailscale_hostname: document.getElementById("tailscaleHost").value.trim(),
    tailscale_auth_key: document.getElementById("tailscaleAuthKey").value.trim(),
    admin_username: document.getElementById("adminUser").value.trim(),
    admin_password: document.getElementById("adminPass").value,
  };

  if (payload.wan_interface === payload.lan_interface) {
    showNotification("WAN and LAN must differ", true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
    return;
  }

  const r = await fetch("/setup/apply", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!r.ok) {
    showNotification(await r.text() || "Failed to start setup", true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
    return;
  }

  try {
    const done = await pollUntilDone();
    showDone(done.message);
    showNotification("Setup complete");
  } catch (err) {
    showNotification(err.message, true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
  }
});

document.getElementById("rebootstrapBtn")?.addEventListener("click", async () => {
  if (!confirm("Remove config and run setup again?")) return;
  const r = await fetch("/setup/reset", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ rebootstrap: true }),
  });
  if (!r.ok) {
    showNotification(await r.text(), true);
    return;
  }
  location.reload();
});

document.getElementById("clearSetupBtn")?.addEventListener("click", async () => {
  await fetch("/setup/reset", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
  location.reload();
});

window.onload = initSetup;
