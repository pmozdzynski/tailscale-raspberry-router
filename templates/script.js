const itemsPerPage = 10;
let currentPage = 1;
let exitNodes = [];
let friendlyNames = {}; // Store friendly names

// Utility function to fetch friendly names
function getFriendlyName(node) {
  if (!node) return "Unknown"; // Fallback if null or undefined
  let cleanNode = node
    .trim()
    .toLowerCase()
    .replace(/\s*\(.*?\)\s*$/, "");
  return friendlyNames[cleanNode] || node;
}

// Fetch friendly names JSON
async function loadFriendlyNames() {
  try {
    const response = await fetch("/friendly-names.json");
    if (!response.ok) throw new Error("Failed to load friendly names");

    const data = await response.json();

    // Normalize and clean up keys
    friendlyNames = Object.fromEntries(
      Object.entries(data).map(([key, value]) => [
        key
          .trim()
          .toLowerCase()
          .replace(/\s*\(.*?\)\s*$/, ""),
        value,
      ])
    );
  } catch (error) {
    console.error("Error loading friendly names:", error);
  }
}

// Fetch status and update UI
async function fetchStatus() {
  try {
    const response = await fetch("/status");
    if (!response.ok) throw new Error("Failed to fetch status");

    const data = await response.json();

    // Extract raw hostname (remove "tailscale:" prefix if exists)
    let currentModeRaw = data.mode
      .replace(/^tailscale:/, "")
      .trim()
      .toLowerCase();

    // Remove any location info inside parentheses
    currentModeRaw = currentModeRaw.replace(/\s*\(.*?\)\s*$/, "");

    // ✅ Properly match with friendly name
    const currentModeFriendly = friendlyNames[currentModeRaw] || currentModeRaw;

    // ✅ Fix: Display the friendly name correctly in "Current Mode"
    document.getElementById(
      "currentMode"
    ).innerHTML = `<span class="active-node">${currentModeFriendly}</span>`;

    exitNodes = [];
    let friendlyPrivateNodes = [];
    let friendlyMullvadNodes = [];
    let genericNodes = [];

    // Categorize nodes, but **exclude offline nodes**
    Object.entries(data.exitNodes || {}).forEach(([node, details]) => {
      if (!details?.Active) return; // Skip offline nodes 🚀

      const button = document.createElement("button");
      let cleanNode = node
        .trim()
        .toLowerCase()
        .replace(/\s*\(.*?\)\s*$/, "");
      const displayName = friendlyNames[cleanNode] || node;

      button.textContent = `${displayName}`;
      button.className = node.includes(".mullvad.")
        ? "exit-node"
        : "private-node";

      // ✅ Fix: Highlight the currently active node
      if (cleanNode === currentModeRaw) {
        button.classList.add("active-node");
        button.disabled = true;
      } else {
        button.onclick = () => switchMode("tailscale", node);
      }

      // **Prioritize friendly names first**
      if (friendlyNames[cleanNode]) {
        if (!node.includes(".mullvad.")) {
          friendlyPrivateNodes.push(button); // Private (Green)
        } else {
          friendlyMullvadNodes.push(button); // Mullvad (Blue)
        }
      } else {
        genericNodes.push(button); // Generic
      }
    });

    // Arrange exit nodes (Private & Friendly on top)
    exitNodes = [
      ...friendlyPrivateNodes,
      "separator",
      ...friendlyMullvadNodes,
      "separator",
      ...genericNodes,
    ];

    // Render the first page
    currentPage = 1;
    renderPage();
  } catch (error) {
    console.error("Error fetching status:", error);
  }
}

// Render paginated exit nodes
function renderPage() {
  const nodeList = document.getElementById("exitNodesList");
  nodeList.innerHTML = "";

  const startIndex = (currentPage - 1) * itemsPerPage;
  const endIndex = startIndex + itemsPerPage;
  const paginatedItems = exitNodes.slice(startIndex, endIndex);

  paginatedItems.forEach((item) => {
    if (item === "separator") {
      const separator = document.createElement("div");
      separator.className = "separator";
      nodeList.appendChild(separator);
    } else {
      const listItem = document.createElement("div");
      listItem.appendChild(item);
      nodeList.appendChild(listItem);
    }
  });

  document.getElementById(
    "pageInfo"
  ).textContent = `Page ${currentPage} of ${Math.ceil(
    exitNodes.length / itemsPerPage
  )}`;
  document.getElementById("prevPage").disabled = currentPage === 1;
  document.getElementById("nextPage").disabled =
    currentPage === Math.ceil(exitNodes.length / itemsPerPage);
}

// Pagination event listeners
document.getElementById("prevPage").addEventListener("click", () => {
  if (currentPage > 1) {
    currentPage--;
    renderPage();
  }
});

document.getElementById("nextPage").addEventListener("click", () => {
  if (currentPage < Math.ceil(exitNodes.length / itemsPerPage)) {
    currentPage++;
    renderPage();
  }
});

// Show notification popup
function showNotification(message) {
  const infoBox = document.getElementById("infoBox");
  infoBox.textContent = message;
  infoBox.style.display = "block";
  infoBox.style.opacity = "1";

  setTimeout(() => {
    infoBox.style.opacity = "0";
    setTimeout(() => {
      infoBox.style.display = "none";
    }, 500);
  }, 3000);
}

// Switch mode and notify
async function switchMode(mode, node = "") {
  let url = `/set-mode?mode=${mode}`;
  if (mode === "tailscale") {
    url += `&node=${node}`;
  }
  try {
    await fetch(url, { method: "POST" });
    const displayName = getFriendlyName(node);
    showNotification(
      `Switched to ${mode} ${displayName ? "(" + displayName + ")" : ""}`
    );
    fetchStatus();
  } catch (error) {
    showNotification("Error switching mode");
    console.error(error);
  }
}

// Load friendly names first, then fetch status
window.onload = async () => {
  await loadFriendlyNames();
  fetchStatus();
  bindDiagnosticsUI();
};

function bindDiagnosticsUI() {
  document.getElementById("runDiagnosticsBtn").addEventListener("click", () => {
    runDiagnosticStream("/diagnostics/run?stream=1", "runDiagnosticsBtn", "Run Diagnostics");
  });
  document.getElementById("repairRoutingBtn").addEventListener("click", () => {
    runDiagnosticStream("/diagnostics/repair?stream=1", "repairRoutingBtn", "Repair Routing & DNS");
  });
  document.getElementById("copyDiagBtn").addEventListener("click", async () => {
    const text = document.getElementById("diagLog").textContent;
    try {
      await navigator.clipboard.writeText(text);
      showNotification("Log copied to clipboard");
    } catch {
      showNotification("Copy failed — select log text manually");
    }
  });
}

function appendDiagLine(text) {
  const log = document.getElementById("diagLog");
  const panel = document.getElementById("diagLogPanel");
  panel.hidden = false;
  log.textContent += text + "\n";
  log.scrollTop = log.scrollHeight;
  document.getElementById("copyDiagBtn").disabled = false;
}

async function runDiagnosticStream(url, btnId, btnLabel) {
  const btn = document.getElementById(btnId);
  const repairBtn = document.getElementById("repairRoutingBtn");
  const diagBtn = document.getElementById("runDiagnosticsBtn");
  btn.disabled = true;
  diagBtn.disabled = true;
  repairBtn.disabled = true;
  document.getElementById("diagLog").textContent = "";
  document.getElementById("copyDiagBtn").disabled = true;

  try {
    const response = await fetch(url, {
      method: "POST",
      headers: { Accept: "text/event-stream" },
    });

    if (!response.ok || !response.body) {
      const text = await response.text();
      throw new Error(text || "Request failed");
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n");
      buffer = chunks.pop() || "";

      for (const chunk of chunks) {
        const line = chunk.split("\n").find((l) => l.startsWith("data: "));
        if (!line) {
          continue;
        }
        let evt;
        try {
          evt = JSON.parse(line.slice(6));
        } catch {
          continue;
        }
        if (evt.status === "line") {
          appendDiagLine(evt.detail || "");
        } else if (evt.status === "done") {
          appendDiagLine("✓ " + (evt.detail || "done"));
          showNotification(evt.detail || "Complete");
          fetchStatus();
        } else if (evt.status === "error") {
          appendDiagLine("✗ " + (evt.detail || "error"));
          showNotification(evt.detail || "Failed");
        }
      }
    }
  } catch (error) {
    appendDiagLine(error.message);
    showNotification(error.message);
  } finally {
    btn.disabled = false;
    diagBtn.disabled = false;
    repairBtn.disabled = false;
    btn.textContent = btnLabel;
  }
}
