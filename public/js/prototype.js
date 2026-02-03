(() => {
  const state = {
    source: "CHROBINSON",
    page: 1,
    pageSize: 100,
    total: 0,
  };

  const els = {
    status: document.getElementById("status"),
    list: document.getElementById("list"),
    pageLabel: document.getElementById("pageLabel"),
    countLabel: document.getElementById("countLabel"),
    prevBtn: document.getElementById("prevBtn"),
    nextBtn: document.getElementById("nextBtn"),
    btnCHROB: document.getElementById("btnCHROB"),
    btnTS: document.getElementById("btnTS"),
  };

  function setActiveSource(source) {
    state.source = source;
    state.page = 1;

    els.btnCHROB.classList.toggle("active", source === "CHROBINSON");
    els.btnTS.classList.toggle("active", source === "TRUCKSTOP");

    render();
  }

  function totalPages() {
    return Math.max(1, Math.ceil(state.total / state.pageSize));
  }

  function fmtTime(value) {
    if (!value) return "—";
    const d = new Date(value);
    if (isNaN(d.getTime())) return String(value);
    return d.toLocaleString();
  }

  function escapeHtml(str) {
    return String(str)
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;");
  }

  async function fetchPage() {
    const url = `/api/orders?source=${encodeURIComponent(state.source)}&page=${state.page}&pageSize=${state.pageSize}`;
    const resp = await fetch(url);
    if (!resp.ok) {
      const text = await resp.text();
      throw new Error(text || `HTTP ${resp.status}`);
    }
    return resp.json();
  }

  function renderCards(items) {
    if (!items || items.length === 0) {
      els.list.innerHTML = `<div class="card"><div class="mono" style="color: var(--muted); font-size: 13px;">No items yet.</div></div>`;
      return;
    }

    els.list.innerHTML = items
      .map((it) => {
        const order = it.order || {};
        const orderNumber = order.orderNumber || "—";
        const receivedAt = it.receivedAt;
        const pretty = escapeHtml(JSON.stringify(order, null, 2));
        return `
          <div class="card">
            <div class="meta">
              <div class="left">
                <span class="chip mono">${escapeHtml(state.source)}</span>
                <span class="chip mono">orderNumber=${escapeHtml(orderNumber)}</span>
              </div>
              <div class="chip">${escapeHtml(fmtTime(receivedAt))}</div>
            </div>
            <pre class="mono">${pretty}</pre>
          </div>
        `;
      })
      .join("");
  }

  async function render() {
    els.status.textContent = `Source=${state.source} • page=${state.page} • pageSize=${state.pageSize}`;

    try {
      const data = await fetchPage();
      state.total = data.total || 0;

      const pages = totalPages();
      if (state.page > pages) state.page = pages;

      els.pageLabel.textContent = `Page ${state.page} / ${pages}`;
      els.countLabel.textContent = `${state.total} total`;

      els.prevBtn.disabled = state.page <= 1;
      els.nextBtn.disabled = state.page >= pages;

      renderCards(data.items || []);
    } catch (e) {
      els.status.textContent = `Error: ${e && e.message ? e.message : e}`;
      els.list.innerHTML = "";
    }
  }

  els.btnCHROB.addEventListener("click", () => setActiveSource("CHROBINSON"));
  els.btnTS.addEventListener("click", () => setActiveSource("TRUCKSTOP"));

  els.prevBtn.addEventListener("click", () => {
    state.page = Math.max(1, state.page - 1);
    render();
  });

  els.nextBtn.addEventListener("click", () => {
    state.page = Math.min(totalPages(), state.page + 1);
    render();
  });

  setActiveSource("CHROBINSON");
  render();
})();
