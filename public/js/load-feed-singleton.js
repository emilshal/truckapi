/****************************************************************
 *  Load-feed singleton – keep one WebSocket pair per browser
 *
 *  – No SharedWorker needed (works in iOS Safari, Chrome exts, etc.)
 *  – Leader-election via BroadcastChannel “ping”
 *  – Public API:  subscribe(cb) → unsubscribe()
 ****************************************************************/

const CHROB_URL = "wss://chrob.hfield.net/ws";
const TRUCKSTOP_URL = "wss://chrob.hfield.net/ws/truckstop";
const CH = new BroadcastChannel("load-feed"); // intra-tab bus
const PING_MS = 1_000; // heartbeat
const LEADER_TTL_MS = 3_000; // takeover time
const SNAPSHOT_MS = 3 * 60_000; // 3-min snapshot

/* ────────── shared caches ─────────────────────────────────── */
const liveChrob = new Map(); // loadNumber → shipment
const liveTruckstop = new Map();
const subs = new Set(); // callbacks in **this** tab

/* ────────── leader-election state ─────────────────────────── */
let isLeader = false;
let lastPing = Date.now();

/* ============================================================
   Public API
   ========================================================== */
export function subscribe(cb) {
  subs.add(cb);
  return () => subs.delete(cb);
}

/* ============================================================
   Leader duties
   ========================================================== */
function becomeLeader() {
  if (isLeader) return;
  isLeader = true;
  console.log("[feed] 🟢 became leader");

  // 1️⃣  open persistent WebSockets
  makeWS(CHROB_URL, handleChrob);
  makeWS(TRUCKSTOP_URL, handleTruckstop);

  // 2️⃣  send heartbeat so others know a leader exists
  setInterval(() => CH.postMessage({ type: "ping" }), PING_MS);

  // 3️⃣  3-min snapshot loop
  setInterval(() => broadcastSnapshot("Chrob", liveChrob), SNAPSHOT_MS);
  setInterval(() => broadcastSnapshot("Truckstop", liveTruckstop), SNAPSHOT_MS);
}

/* ============================================================
   WebSocket helper (auto-reconnect)
   ========================================================== */
function makeWS(url, onMessage) {
  let ws;
  const open = () => {
    ws = new WebSocket(url);
    ws.onopen = () => console.log("[feed] WS open →", url);
    ws.onmessage = (e) => onMessage(JSON.parse(e.data));
    ws.onclose = () => setTimeout(open, 1_000);
    ws.onerror = (e) => console.error("[feed] WS error", url, e);
  };
  open();
}

/* ============================================================
   CH Robinson handler – already unified
   ========================================================== */
function handleChrob(msg) {
  msg.supplier = "Chrob";
  liveChrob.set(msg.loadNumber, msg);
  fanout({ type: "load", supplier: "Chrob", payload: msg });
}

/* ============================================================
   Truckstop handler – convert → cache
   ========================================================== */
function handleTruckstop(raw) {
  const meta = raw.additionalData ?? {};
  const load = {
    loadNumber: raw.load?.ID ?? raw.load?.LoadNumber ?? 0,
    origin: {
      city: raw.load?.OriginCity ?? "N/A",
      state: raw.load?.OriginState ?? "N/A",
      zip: raw.locationData?.address?.match(/\b\d{5}\b/)?.[0] ?? "N/A",
    },
    destination: {
      city: raw.load?.DestinationCity ?? "N/A",
      state: raw.load?.DestinationState ?? "N/A",
      zip: raw.destinationData?.zip ?? "N/A",
    },
    weight: { pounds: raw.load?.Weight ?? "N/A" },
    distance: { miles: raw.load?.Miles ?? "N/A" },
    deadHeadDistance: raw.load?.OriginDistance ?? "N/A",
    equipment: {
      length: { standard: raw.load?.Length || "N/A" },
      width: { standard: raw.load?.Width || "N/A" },
      height: { standard: "N/A" },
    },
    loadType: raw.load?.LoadType ?? "N/A",
    calculatedPickUpByDateTime: raw.locationData?.from ?? null,
    calculatedDeliverByDateTime: raw.locationData?.to ?? null,
    readyBy: raw.locationData?.from ?? null,
    deliverBy: raw.locationData?.to ?? null,
    AdditionalData: {
      UserName: meta.UserName ?? "N/A",
      DriverName: meta.DriverName ?? "N/A",
      DispatcherName: meta.DispatcherName ?? "N/A",
      PhoneNumber: meta.PhoneNumber ?? raw.load?.PointOfContactPhone ?? "N/A",
      TruckType: raw.load?.Equipment ?? "N/A",
      TelegramLink: raw.locationData?.TelegramLink ?? "#",
    },
    bookingContactPhoneNumber: raw.load?.PointOfContactPhone ?? "N/A",
    pieces: raw.load?.Pieces ?? "N/A",
    comment: "",
    supplier: "Truckstop",
  };

  liveTruckstop.set(load.loadNumber, load);
  fanout({ type: "load", supplier: "Truckstop", payload: load });
}

/* ============================================================
   Snapshot helper
   ========================================================== */
function broadcastSnapshot(supplier, cache) {
  fanout({
    type: "snapshot",
    supplier: supplier,
    payload: Array.from(cache.values()),
  });
  cache.clear();
}

/* ============================================================
   Message distribution helpers
   ========================================================== */
function fanout(msg) {
  // to other tabs
  CH.postMessage({ type: "feed", payload: msg });
  // to subscribers in this tab
  subs.forEach((cb) => cb(msg));
}

/* ============================================================
   BroadcastChannel plumbing
   ========================================================== */
CH.onmessage = ({ data }) => {
  switch (data.type) {
    case "ping":
      lastPing = Date.now();
      break;

    case "feed":
      // feed forwarded by leader → emit only locally
      subs.forEach((cb) => cb(data.payload));
      break;
  }
};

/* ============================================================
   Periodic leader election
   ========================================================== */
setInterval(() => {
  if (!isLeader && Date.now() - lastPing > LEADER_TTL_MS) {
    becomeLeader();
  }
}, PING_MS);

/* bootstrap immediately – whichever tab executes first is likely to
   become leader, the rest will become followers */
becomeLeader(); // harmless if another leader appears within TTL
