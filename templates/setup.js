function showNotification(message, isError = false) {
  const infoBox = document.getElementById("infoBox");
  infoBox.textContent = message;
  infoBox.style.display = "block";
  infoBox.style.opacity = "1";
  infoBox.style.backgroundColor = isError ? "#c62828" : "#4caf50";

  setTimeout(() => {
    infoBox.style.opacity = "0";
    setTimeout(() => {
      infoBox.style.display = "none";
    }, 500);
  }, 5000);
}

function renderInterfaceOption(select, iface, selectedName) {
  const option = document.createElement("option");
  option.value = iface.name;
  const ips = iface.ipv4?.length ? iface.ipv4.join(", ") : "DHCP / none yet";
  const route = iface.is_default_route ? ", default route" : "";
  option.textContent = `${iface.name} (${iface.kind}, ${iface.state}${route}) — ${ips}`;
  if (iface.name === selectedName) {
    option.selected = true;
  }
  select.appendChild(option);
}

function renderNetworkSummary(snapshot) {
  const el = document.getElementById("networkSummary");
  const routes = snapshot.routing || {};
  const ts = snapshot.tailscale || {};
  const pkgs = snapshot.packages || {};
  const wan = snapshot.interfaces?.find((i) => i.name === routes.default_interface);
  const wanIPs = wan?.ipv4?.length ? wan.ipv4.join(", ") : "not assigned yet (DHCP)";

  const lines = [
    `Default route: ${routes.default_interface || "none — connect WAN cable"} via ${routes.default_gateway || "n/a"}`,
    `WAN address: ${wanIPs}`,
    `IP forwarding: ${routes.ip_forwarding ? "enabled" : "will be enabled"}`,
    `Packages: dnsmasq=${pkgs.dnsmasq ? "yes" : "will install"}, tailscale=${pkgs.tailscale ? "yes" : "will install"}, apt=${pkgs.apt_available ? "yes" : "no"}`,
    `Tailscale: ${ts.installed ? ts.status || "unknown" : "will install on apply"}`,
  ];

  if (snapshot.interfaces?.length) {
    lines.push("", "Interfaces:");
    snapshot.interfaces.forEach((iface) => {
      const ips = iface.ipv4?.length ? iface.ipv4.join(", ") : "none";
      const marker = iface.is_default_route ? " [WAN candidate]" : "";
      lines.push(`• ${iface.name}${marker}: ${iface.mac || "no mac"}, ${ips}`);
    });
  }

  el.innerHTML = lines.map((line) => `<div>${line || "&nbsp;"}</div>`).join("");

  const access = document.getElementById("accessUrls");
  if (snapshot.management_ips?.length) {
    access.innerHTML =
      "Open setup at: " +
      snapshot.management_ips.map((ip) => `<code>http://${ip}:5000/setup</code>`).join(" or ");
  } else {
    access.textContent =
      "No IP detected yet. Connect WAN/LAN, wait for DHCP, then open http://<device-ip>:5000/setup";
  }

  const pkg = document.getElementById("packageStatus");
  if (pkgs.apt_available) {
    pkg.textContent =
      "Apply will run apt-get to install missing packages (dnsmasq, iptables, curl, Tailscale).";
  } else {
    pkg.textContent =
      "Warning: apt-get not found. You must install dnsmasq, iptables, curl, and Tailscale manually before apply.";
  }
}

function applySuggestedLAN(snapshot, force = false) {
  const suggested = snapshot.suggested_lan || {};
  const setValue = (id, value) => {
    const el = document.getElementById(id);
    if (value && (force || !el.value)) {
      el.value = value;
    }
  };

  setValue("lanAddress", suggested.address);
  setValue("lanPrefix", suggested.prefix);
  setValue("dhcpStart", suggested.dhcp_start);
  setValue("dhcpEnd", suggested.dhcp_end);

  const hint = document.getElementById("lanSuggestion");
  if (suggested.reason) {
    hint.textContent = suggested.reason;
  }
}

async function loadSetupStatus() {
  const response = await fetch("/setup/status");
  if (!response.ok) {
    throw new Error("Failed to load network status");
  }
  return response.json();
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
    snapshot.interfaces?.find((i) => i.name !== defaultWAN && i.kind === "ethernet")?.name ||
    snapshot.interfaces?.find((i) => i.name !== defaultWAN)?.name ||
    "";

  snapshot.interfaces.forEach((iface) => {
    renderInterfaceOption(wanSelect, iface, defaultWAN);
    renderInterfaceOption(lanSelect, iface, defaultLAN);
  });

  if (snapshot.config?.lan_address) {
    document.getElementById("lanAddress").value = snapshot.config.lan_address;
  }
  if (snapshot.config?.lan_prefix) {
    document.getElementById("lanPrefix").value = snapshot.config.lan_prefix;
  }
  if (snapshot.config?.dhcp_range_start) {
    document.getElementById("dhcpStart").value = snapshot.config.dhcp_range_start;
  }
  if (snapshot.config?.dhcp_range_end) {
    document.getElementById("dhcpEnd").value = snapshot.config.dhcp_range_end;
  }
  if (snapshot.config?.tailscale_hostname) {
    document.getElementById("tailscaleHost").value = snapshot.config.tailscale_hostname;
  } else if (snapshot.hostname) {
    document.getElementById("tailscaleHost").value = snapshot.hostname;
  }

  applySuggestedLAN(snapshot);

  wanSelect.onchange = async () => {
    const wan = wanSelect.value;
    const refreshed = await fetch(`/setup/status?wan=${encodeURIComponent(wan)}`).then((r) => r.json());
    applySuggestedLAN(refreshed, true);
  };
}

async function initSetup() {
  try {
    const snapshot = await loadSetupStatus();
    if (snapshot.configured) {
      window.location.href = "/login";
      return;
    }
    renderNetworkSummary(snapshot);
    populateInterfaceSelects(snapshot);
  } catch (error) {
    showNotification(error.message, true);
  }
}

function escapeHtml(text) {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function appendLogLine(text, cssClass = "") {
  const log = document.getElementById("setupLog");
  const panel = document.getElementById("setupLogPanel");
  panel.hidden = false;
  const span = document.createElement("span");
  if (cssClass) {
    span.className = cssClass;
  }
  span.textContent = text + "\n";
  log.appendChild(span);
  log.scrollTop = log.scrollHeight;
}

function formatLogEvent(evt) {
  const step = evt.step ? `[${evt.step}] ` : "";
  return `${step}${evt.detail || evt.status}`;
}

async function applyWithStream(payload) {
  const response = await fetch("/setup/apply?stream=1", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "text/event-stream",
    },
    body: JSON.stringify(payload),
  });

  if (!response.ok && !response.body) {
    const text = await response.text();
    throw new Error(text || "Setup failed");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let failed = false;

  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const chunks = buffer.split("\n\n");
    buffer = chunks.pop() || "";

    for (const chunk of chunks) {
      const line = chunk
        .split("\n")
        .find((l) => l.startsWith("data: "));
      if (!line) {
        continue;
      }
      let evt;
      try {
        evt = JSON.parse(line.slice(6));
      } catch {
        continue;
      }

      if (evt.status === "running") {
        appendLogLine(formatLogEvent(evt), "log-running");
      } else if (evt.status === "ok") {
        appendLogLine("✓ " + formatLogEvent(evt), "log-ok");
      } else if (evt.status === "error") {
        appendLogLine("✗ " + (evt.detail || evt.step || "error"), "log-error");
        failed = true;
      } else if (evt.status === "done") {
        appendLogLine("✓ " + evt.detail, "log-done");
        return;
      }
    }
  }

  if (failed) {
    throw new Error("Setup failed — see log above");
  }
}

document.getElementById("setupForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const btn = document.getElementById("applyBtn");
  const form = document.getElementById("setupForm");
  btn.disabled = true;
  btn.textContent = "Installing...";
  document.getElementById("setupLog").textContent = "";
  form.style.opacity = "0.55";

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
    showNotification("WAN and LAN must be different interfaces", true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
    return;
  }

  if (!payload.tailscale_auth_key) {
    showNotification("Tailscale auth key is required on fresh installs", true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
    return;
  }

  try {
    await applyWithStream(payload);
    showNotification("Setup complete — redirecting to login");
    setTimeout(() => {
      window.location.href = "/login";
    }, 2000);
  } catch (error) {
    appendLogLine(error.message, "log-error");
    showNotification(error.message, true);
    btn.disabled = false;
    btn.textContent = "Install & Configure";
    form.style.opacity = "1";
  }
});

window.onload = initSetup;
