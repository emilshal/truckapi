/* eslint-disable no-unused-vars */
/* --------------------------------------------------------------
   Truck API — main client script  (ES-module version, 2024-05-31)
   -------------------------------------------------------------- */
import { subscribe } from "./load-feed-singleton.js"; // ⬅️ NEW

// -----------------------------------------------------------------------------
// mapping table (unchanged)
// -------------------4----------------------------------------------------------
const truckTypeMapping = {
  /* --------------------------------------------------------------
   supplier‑display map – controls how supplier names appear
-------------------------------------------------------------- */

  "2F": "Two 24 or 28 Foot Flatbeds",
  ANIM: "Animal Carrier",
  AUTO: "Auto Carrier",
  "B-TR": "B-Train/Supertrain (Canada Only)",
  BELT: "Conveyor Belt",
  BOAT: "Boat Hauling Trailer",
  CH: "Convertible Hopper",
  CONT: "Container Trailer",
  DD: "Double Drop",
  DUMP: "Dump Truck",
  ENDP: "End Dump",
  F: "Flatbed",
  FEXT: "Stretch/Extendable Flatbed",
  FINT: "Flatbed Intermodal",
  FO: "Flatbed (Over-Dimensional)",
  FSD: "Flatbed or Step Deck",
  FVR: "Flatbed, Van, or Reefer",
  FWS: "Flatbed with Sides",
  HOPP: "Hopper Bottom",
  HS: "Hot Shot",
  HTU: "Haul and Tow Unit",
  LAF: "Landoll Flatbed",
  LB: "Lowboy",
  LBO: "Lowboy Over Dimensional",
  LDOT: "Load-Out Trailer",
  LIVE: "Live Bottom Trailer",
  MAXI: "Maxi or Double Flat",
  MBHM: "Mobile Home",
  PNEU: "Pneumatic",
  PO: "Power Only",
  R: "Reefer",
  RGN: "Removable Gooseneck (RGN)",
  RGNE: "RGN Extendable",
  RINT: "Refrigerated Intermodal",
  ROLL: "Roll Top Conestoga",
  RPD: "Reefer w/ Plant Decking",
  SD: "Step Deck",
  SDL: "Step Deck w/ Loading Ramps",
  SDO: "Step Deck Over-Dimensional",
  SPEC: "Unspecified Specialized Trailer",
  SV: "Straight Van",
  TANK: "Tanker",
  V: "Van",
  "V-OT": "Open Top Van",
  VB: "Blanket Wrap Van",
  CV: "Curtain Van",
  VCAR: "Cargo Van (1 Ton)",
  VF: "Flatbed or Van",
  VINT: "Van Intermodal",
  VIV: "Vented Insulated Van",
  VLG: "Van with Liftgate",
  VM: "Moving Van",
  VR: "Van or Reefer",
  VV: "Vented Van",
  WALK: "Walking Floor",
  VVR: "Vented Van or Reefer",
  VIVR: "Vented Insulated Van or Reefer",
  VA: "Van Air-Ride",
  FA: "Flatbed Air-Ride",
  FV: "Van or Flatbed",
  FRV: "Flatbed, Van, or Reefer",
  FSDV: "Flatbed, Step Deck, or Van",
  FVVR: "Flatbed, Vented Van, or Reefer",
  VRDD: "Van, Reefer, or Double Drop",
  FVV: "Flatbed or Vented Van",
  SDRG: "Step Deck or Removable Gooseneck",
  VRF: "Van, Flatbed, or Reefer",
  RVF: "Van, Flatbed, or Reefer",
  RFV: "Van, Flatbed, or Reefer",
  RV: "Van or Reefer",
  SPV: "Cargo/Sprinter Van",
  SDC: "Step Deck Conestoga",
  SDE: "Step Deck Extendable",
  DA: "Drive Away",
  DDE: "Double Drop Extendable",
  BEAM: "Beam Trailer",
  CONG: "Conestoga",
  BDMP: "Belly Dump",
};

const supplierDisplayMap = {
  CHRobinson: "CHRob", // nicer label in the dropdown
  CHRob: "CHRob",
  Truckstop: "Truckstop",
};

$(document).ready(function () {
  const resultsPerPage = 20;

  /* ------------------------------------------------------------------
     shared (mutable) page-state
  ------------------------------------------------------------------ */
  let searchResults = [];
  let expiredResults = [];
  let selectedDriver = null;
  let selectedLoadType = null;
  let selectedOwner = null;
  let selectedSupplier = null;
  let selectedDispatcher = null;
  let currentLivePage = 1;
  let currentExpiredPage = 1;

  /* ------------------------------------------------------------------
     sticky-header clone (unchanged)
  ------------------------------------------------------------------ */
  const tableHead = $("#mainTableHead");
  const tableContainer = $(".table-responsive");
  const stickyHeader = tableHead
    .clone()
    .addClass("sticky-header")
    .appendTo(tableContainer);

  /* ------------------------------------------------------------------
     helper: stable string id  (still used everywhere)
  ------------------------------------------------------------------ */
  function id(x) {
    return x === undefined || x === null ? "" : x.toString();
  }

  /* ------------------------------------------------------------------
     helper: see if the user is actively filtering via search box
  ------------------------------------------------------------------ */
  function isSearchActive() {
    return $("#searchQuery").val().trim() !== "";
  }

  /* ------------------------------------------------------------------
     RENDER THROTTLE – coalesce many data pushes into one paint
  ------------------------------------------------------------------ */
  let uiDirty = false;
  let uiTimer = null;

  function scheduleUIRender(force = false) {
    if (force) {
      uiDirty = false;
      updateUI();
      return;
    }
    uiDirty = true;
    if (uiTimer) return; // already scheduled
    uiTimer = setTimeout(() => {
      uiTimer = null;
      if (uiDirty && !isSearchActive()) updateUI();
      uiDirty = false;
    }, 500); // paint at most every 500 ms
  }

  /* ------------------------------------------------------------------
     🆕  singleton-feed hook-up – replaces the old SharedWorker logic
  ------------------------------------------------------------------ */
  function handleFeedMessage(msg) {
    console.log(
      `[WS FEED] ${msg.supplier || "Unknown Supplier"}:`,
      msg.payload
    );
    /* 0️⃣  single load during warm-up */
    if (msg.type === "load") {
      const supplier = msg.supplier;
      const load = msg.payload;
      load.ID = load.loadNumber ?? 0;
      load.AdditionalData = load.AdditionalData || {};
      const i = searchResults.findIndex(
        (l) =>
          id(l.loadNumber) === id(load.loadNumber) && l.supplier === supplier
      );
      if (i === -1) searchResults.push(load);
      else searchResults[i] = load;

      scheduleUIRender(); // coalesced repaint
      return;
    }

    /* 1️⃣  three-minute snapshot */
    if (msg.type === "snapshot") {
      const supplier = msg.supplier;
      const snapshot = msg.payload;
      const keep = new Set(snapshot.map((l) => id(l.loadNumber)));

      searchResults = searchResults.filter((l) => {
        if (l.supplier !== supplier) return true;
        if (keep.has(id(l.loadNumber))) return false; // will be replaced
        if (
          !expiredResults.some(
            (x) => x.loadNumber === l.loadNumber && x.supplier === supplier
          )
        ) {
          expiredResults.push(l);
        }
        return false;
      });

      snapshot.forEach((load) => {
        load.AdditionalData = load.AdditionalData || {};

        const j = searchResults.findIndex(
          (l) =>
            id(l.loadNumber) === id(load.loadNumber) && l.supplier === supplier
        );
        if (j === -1) searchResults.push(load);
        else searchResults[j] = load;
      });

      scheduleUIRender();
      updateExpiredUI(); // expired tab can refresh freely
    }
  }

  /* start listening (the singleton keeps both WebSockets alive) */
  subscribe(handleFeedMessage);

  // ── STICKY HEADER LOGIC (unchanged) ─────────────────────────────────
  function setStickyHeaderWidth() {
    const originalHeader = $("#mainTable thead tr").children();
    const stickyHeaderColumns = stickyHeader.children().children();
    stickyHeader.css("width", $("#mainTable").outerWidth());
    stickyHeaderColumns.each(function (index) {
      $(this).width($(originalHeader[index]).outerWidth());
    });
  }
  function syncStickyHeaderScroll() {
    stickyHeader.css("left", -tableContainer.scrollLeft());
  }
  const offset = 22;
  $(window).on("scroll", function () {
    const scrollTop = $(this).scrollTop();
    const tableOffsetTop = $("#mainTable").offset().top + offset;
    const tableHeight = $("#mainTable").height();
    if (
      scrollTop > tableOffsetTop &&
      scrollTop < tableOffsetTop + tableHeight
    ) {
      setStickyHeaderWidth();
      syncStickyHeaderScroll();
      stickyHeader.css({
        visibility: "visible",
        opacity: 1,
        transform: "translateY(0)",
        transition: "opacity 0.3s ease, transform 0.3s ease, visibility 0s",
      });
    } else {
      stickyHeader.css({
        opacity: 0,
        transform: "translateY(-10px)",
        visibility: "hidden",
        transition:
          "opacity 0.3s ease, transform 0.3s ease, visibility 0s 0.3s",
      });
    }
  });
  $(window).on("resize", setStickyHeaderWidth);
  tableContainer.on("scroll", syncStickyHeaderScroll);

  const ownerDropdown = $("#ownerDropdown");
  const ownerHeader = $("#ownerNameHeader");
  const selectedOwnerLabel = $("#selectedOwnerLabel");

  const dispatcherDropdown = $("#dispatcherDropdown");
  const dispatcherHeader = $("#dispatcherNameHeader");
  const selectedDispatcherLabel = $("#selectedDispatcherLabel");

  ownerHeader.on("click", function (e) {
    e.stopPropagation();
    ownerDropdown.toggle();
  });

  function populateOwnerDropdown(ownerNames) {
    ownerDropdown.empty();
    ownerNames.forEach((owner) => {
      const item = $("<a>")
        .addClass("dropdown-item")
        .text(owner)
        .on("click", () => {
          selectedOwner = owner;
          selectedOwnerLabel.text(`(${owner})`);
          updateUI();
          ownerDropdown.hide();
        });
      ownerDropdown.append(item);
    });
    $('<div class="dropdown-divider">').appendTo(ownerDropdown);
    $("<a>")
      .addClass("dropdown-item text-muted")
      .text("Show All")
      .on("click", () => {
        selectedOwner = null;
        selectedOwnerLabel.text("");
        updateUI();
        ownerDropdown.hide();
      })
      .appendTo(ownerDropdown);
  }

  const supplierDropdown = $("#supplierDropdown");
  const supplierHeader = $("#supplierHeader");
  const selectedSupplierLabel = $("#selectedSupplierLabel");

  supplierHeader.on("click", function (e) {
    e.stopPropagation();
    supplierDropdown.toggle();
  });

  // Refactored dispatcher dropdown logic to ensure proper event handling
  dispatcherHeader.off("click").on("click", function (e) {
    e.stopPropagation();
    // Only populate and show the dropdown on first click, not on subsequent clicks
    dispatcherDropdown.toggle();
  });

  // Remove any previous click handlers on dispatcherDropdown items to prevent double-handling
  dispatcherDropdown.off("click", ".dropdown-item");

  function populateSupplierDropdown(suppliers) {
    supplierDropdown.empty();
    suppliers.forEach((supplier) => {
      const label = supplierDisplayMap[supplier] || supplier;
      const item = $("<a>")
        .addClass("dropdown-item")
        .attr("href", "#")
        .attr("data-supplier", supplier)
        .text(label)
        .on("click", () => {
          selectedSupplier = supplier;
          selectedSupplierLabel.text(`(${label})`);
          updateUI();
          supplierDropdown.hide();
        });
      supplierDropdown.append(item);
    });
    $('<div class="dropdown-divider">').appendTo(supplierDropdown);
    $("<a>")
      .addClass("dropdown-item text-muted")
      .attr("href", "#")
      .text("Show All")
      .on("click", () => {
        selectedSupplier = null;
        selectedSupplierLabel.text("");
        updateUI();
        supplierDropdown.hide();
      })
      .appendTo(supplierDropdown);
  }
  // clicking *anywhere else* hides all dropdowns
  $(document).on("click", function () {
    $(
      "#driverDropdown, #loadTypeDropdown, #ownerDropdown, #supplierDropdown, #dispatcherDropdown"
    ).hide();
  });
  // prevent clicks inside dropdowns from closing them
  $("#ownerDropdown").on("click", function (e) {
    e.stopPropagation();
  });
  $("#supplierDropdown").on("click", function (e) {
    e.stopPropagation();
  });

  function populateDispatcherDropdown(dispatchers) {
    dispatcherDropdown.empty();
    dispatchers
      .filter(
        (disp) =>
          disp &&
          typeof disp === "string" &&
          disp.trim().length > 0 &&
          disp !== "N/A"
      )
      .forEach((disp) => {
        $("<a>")
          .addClass("dropdown-item dispatcher-item")
          .attr("href", "#")
          .text(disp)
          .appendTo(dispatcherDropdown);
      });
    $('<div class="dropdown-divider">').appendTo(dispatcherDropdown);
    $("<a>")
      .addClass("dropdown-item text-muted dispatcher-item-all")
      .attr("href", "#")
      .text("Show All")
      .appendTo(dispatcherDropdown);
  }

  // Delegate click handler for dispatcher dropdown items
  dispatcherDropdown
    .off("click")
    .on("click", ".dispatcher-item", function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      const dispatcherName = $(this).text();
      selectedDispatcher = dispatcherName;
      $("#selectedDispatcherLabel").text(`(${dispatcherName})`);
      currentLivePage = 1;
      updateUI();
      dispatcherDropdown.hide();
    });
  dispatcherDropdown.on("click", ".dispatcher-item-all", function (ev) {
    ev.preventDefault();
    ev.stopPropagation();
    selectedDispatcher = null;
    $("#selectedDispatcherLabel").text("");
    currentLivePage = 1;
    updateUI();
    dispatcherDropdown.hide();
  });

  // ── DATE FORMATTING HELPERS ─────────────────────────────────────────
  function formatLocalDate(utcString, city, state) {
    if (!utcString) return "N/A";
    const stateTimezones = {
      AL: "America/Chicago",
      AK: "America/Anchorage",
      AZ: "America/Phoenix",
      AR: "America/Chicago",
      CA: "America/Los_Angeles",
      CO: "America/Denver",
      CT: "America/New_York",
      DE: "America/New_York",
      FL: "America/New_York",
      GA: "America/New_York",
      HI: "Pacific/Honolulu",
      ID: "America/Boise",
      IL: "America/Chicago",
      IN: "America/Indiana/Indianapolis",
      IA: "America/Chicago",
      KS: "America/Chicago",
      KY: "America/New_York",
      LA: "America/Chicago",
      ME: "America/New_York",
      MD: "America/New_York",
      MA: "America/New_York",
      MI: "America/Detroit",
      MN: "America/Chicago",
      MS: "America/Chicago",
      MO: "America/Chicago",
      MT: "America/Denver",
      NE: "America/Chicago",
      NV: "America/Los_Angeles",
      NH: "America/New_York",
      NJ: "America/New_York",
      NM: "America/Denver",
      NY: "America/New_York",
      NC: "America/New_York",
      ND: "America/Chicago",
      OH: "America/New_York",
      OK: "America/Chicago",
      OR: "America/Los_Angeles",
      PA: "America/New_York",
      RI: "America/New_York",
      SC: "America/New_York",
      SD: "America/Chicago",
      TN: "America/Chicago",
      TX: "America/Chicago",
      UT: "America/Denver",
      VT: "America/New_York",
      VA: "America/New_York",
      WA: "America/Los_Angeles",
      WV: "America/New_York",
      WI: "America/Chicago",
      WY: "America/Denver",
    };
    const zone = stateTimezones[state.toUpperCase()] || "America/New_York";
    return luxon.DateTime.fromISO(utcString, { zone: "utc" })
      .setZone(zone)
      .toLocaleString(luxon.DateTime.DATETIME_MED);
  }
  function formatDateTime(dateStr) {
    if (!dateStr) return "N/A";
    const dt = new Date(dateStr);
    return dt.toLocaleString("en-US", { timeZone: "America/New_York" });
  }
  // (add this helper next to your existing formatDateTime / formatLocalDate)
  function clipboardFormatDate(load) {
    if (load.supplier === "Truckstop") {
      // Truckstop uses plain New York formatting
      return formatDateTime(load.calculatedPickUpByDateTime || load.readyBy);
    } else {
      // CHRob uses local‐per‐state formatting
      return formatLocalDate(
        load.calculatedPickUpByDateTime || load.readyBy,
        load.origin?.city || "N/A",
        load.origin?.state || "N/A"
      );
    }
  }
  function clipboardFormatDelivery(load) {
    if (load.supplier === "Truckstop") {
      return formatDateTime(load.calculatedDeliverByDateTime || load.deliverBy);
    } else {
      return formatLocalDate(
        load.calculatedDeliverByDateTime || load.deliverBy,
        load.destination?.city || "N/A",
        load.destination?.state || "N/A"
      );
    }
  }

  // ── RENDER / UPDATE UI ────────────────────────────────────────────────
  function updateUI() {
    const filter = $("#searchQuery").val().trim().toLowerCase();
    const dataToRender =
      filter === ""
        ? searchResults
        : searchResults.filter((load) => {
            const fieldsToSearch = [
              load.loadNumber,
              load.loadType,
              load.comment,
              load.supplier,
              load.bookingContactPhoneNumber,
              load.AdditionalData?.UserName,
              load.AdditionalData?.DriverName,
              load.AdditionalData?.DispatcherName,
              load.AdditionalData?.PhoneNumber,
              load.AdditionalData?.TruckType,
              load.TruckType,
              load.TruckData?.TruckType,
              load.specializedEquipment?.description,
              load.origin?.city,
              load.origin?.state,
              load.origin?.zip,
              load.destination?.city,
              load.destination?.state,
              load.destination?.zip,
            ];
            const searchValueLower = filter.toLowerCase().trim();
            return fieldsToSearch.some(
              (field) =>
                typeof field === "string" &&
                (field.toLowerCase().trim().includes(searchValueLower) ||
                  (searchValueLower === "hot shot" &&
                    field.toLowerCase().includes("hot") &&
                    field.toLowerCase().includes("shot")))
            );
          });

    // ─── apply driver + loadType dropdown filters ─────────────────
    const filtered = dataToRender.filter((load) => {
      // Exclude Truckstop loads with standard length > 26
      if (
        load.supplier === "Truckstop" &&
        load.equipment?.length?.standard &&
        Number(load.equipment.length.standard) > 26
      )
        return false;

      if (
        selectedDispatcher &&
        load.AdditionalData.DispatcherName !== selectedDispatcher
      )
        return false;

      if (selectedDriver && load.AdditionalData.DriverName !== selectedDriver)
        return false;

      if (
        selectedLoadType &&
        !(
          load.loadType === selectedLoadType ||
          (selectedLoadType === "Full" &&
            (!load.loadType || load.loadType.toUpperCase() === "N/A"))
        )
      )
        return false;

      if (selectedOwner && load.AdditionalData.UserName !== selectedOwner)
        return false;

      if (selectedSupplier && load.supplier !== selectedSupplier) return false;

      return true;
    });

    /* in updateUI() use currentLivePage …*/
    const maxPage = Math.ceil(filtered.length / resultsPerPage) || 1;
    currentLivePage = Math.min(currentLivePage, maxPage);
    const start = (currentLivePage - 1) * resultsPerPage;
    const paginatedResults = filtered.slice(start, start + resultsPerPage);

    // const maxPage = Math.ceil(dataToRender.length / resultsPerPage) || 1;
    // currentPage = Math.min(currentPage, maxPage);
    // const start = (currentPage - 1) * resultsPerPage;
    // const paginatedResults = dataToRender.slice(start, start + resultsPerPage);

    requestAnimationFrame(() => {
      const fragment = document.createDocumentFragment();
      (Array.isArray(paginatedResults) ? paginatedResults : []).forEach(
        (load) => {
          let pickupDate, deliveryDate;
          if (load.supplier === "Truckstop") {
            pickupDate = formatDateTime(
              load.calculatedPickUpByDateTime || load.readyBy
            );
            deliveryDate = formatDateTime(
              load.calculatedDeliverByDateTime || load.deliverBy
            );
          } else {
            pickupDate = formatLocalDate(
              load.calculatedPickUpByDateTime || load.readyBy,
              load.origin?.city || "N/A",
              load.origin?.state || "N/A"
            );
            deliveryDate = formatLocalDate(
              load.calculatedDeliverByDateTime || load.deliverBy,
              load.destination?.city || "N/A",
              load.destination?.state || "N/A"
            );
          }
          const truckType = load.AdditionalData?.TruckType || "N/A";
          const bookingContactPhoneNumber =
            load.bookingContactPhoneNumber || "—";
          const ownerPhoneNumber = load.AdditionalData?.PhoneNumber || "";
          const row = document.createElement("tr");
          row.innerHTML = `
            <td>${load.loadNumber}</td>
            <td>${load.origin?.city || "N/A"}, ${
            load.origin?.state || "N/A"
          }, ${load.origin?.zip || "N/A"}
                <br>Pick Up By: ${pickupDate}</td>
            <td>${load.destination?.city || "N/A"}, ${
            load.destination?.state || "N/A"
          }, ${load.destination?.zip || "N/A"}
                <br>Deliver By: ${deliveryDate}</td>
            <td>${load.weight?.pounds ?? "N/A"}</td>
            <td>${Math.floor(load.deadHeadDistance) || "N/A"} mi</td>
            <td>${load.distance?.miles ?? "N/A"} mi</td>
            <td>${load.AdditionalData.UserName || "N/A"}
                <br><a href="${
                  load.AdditionalData.TelegramLink
                }" target="_blank" class="telegram-button">Telegram</a>
                <br>${ownerPhoneNumber}</td>
            <td>${load.AdditionalData.DriverName || "N/A"}</td>
            <td>${truckTypeMapping[truckType] || truckType || "N/A"}</td>
            <td>${load.equipment?.length?.standard ?? "N/A"} ft</td>
            <td>${load.AdditionalData.DispatcherName || "N/A"}</td>
            <td>${bookingContactPhoneNumber}</td>
            <td>${load.supplier}</td>
            <td>${load.loadType || "N/A"}</td>
            <td>
              <button class="btn btn-success" onclick="bookLoad(${
                load.loadNumber
              })">Book</button>
              <button class="btn btn-warning" onclick="offerLoad(${
                load.loadNumber
              })">Offer</button>
              <span class="clipboard-icon"
                data-load-number="${load.loadNumber}"
                data-pickup-location="${load.destination?.city || "N/A"}, ${
            load.destination?.state || "N/A"
          }"
                data-pickup-date="${load.readyBy}"
                data-comment="${load.comment || "N/A"}">&#x1F4CB;</span>
            </td>
          `;
          fragment.appendChild(row);
        }
      );
      const tableBody = document.getElementById("resultsTableBody");
      tableBody.innerHTML = "";
      tableBody.appendChild(fragment);
    });
    // auto-update owner/supplier/dispatcher dropdowns from current searchResults
    const uniqueOwners = Array.from(
      new Set(
        searchResults
          .map((l) => l.AdditionalData.UserName)
          .filter(
            (value) =>
              value && typeof value === "string" && value.trim().length > 0
          )
      )
    ).sort();

    populateOwnerDropdown(uniqueOwners);

    const uniqueSuppliers = Array.from(
      new Set(
        searchResults
          .map((l) => l.supplier)
          .filter(
            (value) => typeof value === "string" && value.trim().length > 0
          )
      )
    ).sort();

    populateSupplierDropdown(uniqueSuppliers);

    const uniqueDispatchers = Array.from(
      new Set(
        searchResults
          .map((l) => l.AdditionalData.DispatcherName)
          .filter(
            (value) =>
              value && typeof value === "string" && value.trim().length > 0
          )
      )
    ).sort();
    populateDispatcherDropdown(uniqueDispatchers);
    // ─── update footer & buttons ──────────────────────────────────
    $("#loadsFound").text(`Loads found: ${filtered.length}`);
    $("#currentPage").text(`Page ${currentLivePage} of ${maxPage}`);

    /* adjust previous/next buttons for live … */
    $("#previousPage").prop("disabled", currentLivePage === 1);
    $("#nextPage").prop("disabled", currentLivePage === maxPage);

    // PAGINATION: Render numbered page buttons
    const $pagination = $("#pageButtons");
    if ($pagination.length) {
      $pagination.empty();
      for (let i = 0; i < maxPage; i++) {
        const button = $("<button>")
          .addClass("btn btn-sm btn-light mx-1 page-number-btn")
          .text(i + 1)
          .on("click", function () {
            currentLivePage = i + 1;
            updateUI();
          });
        // Ensure the active page uses currentLivePage directly
        button
          .toggleClass("active", i === currentLivePage - 1)
          .attr("aria-current", i === currentLivePage - 1 ? "page" : null);
        $pagination.append(button);
      }
    }
  }

  // ── PAGINATION BUTTONS ──────────────────────────────────────────────
  $("#previousPage").on("click", function () {
    if (currentLivePage > 1) {
      currentLivePage--;
      updateUI();
    }
  });
  $("#nextPage").on("click", function () {
    const search = $("#searchQuery").val().trim().toLowerCase();

    // rebuild the *real* filtered list (same predicate as in updateUI)
    const filtered =
      search === ""
        ? searchResults
        : searchResults.filter((load) => {
            const fieldsToSearch = [
              load.loadNumber,
              load.loadType,
              load.comment,
              load.supplier,
              load.bookingContactPhoneNumber,
              load.AdditionalData?.UserName,
              load.AdditionalData?.DriverName,
              load.AdditionalData?.DispatcherName,
              load.AdditionalData?.PhoneNumber,
              load.AdditionalData?.TruckType,
              load.origin?.city,
              load.origin?.state,
              load.origin?.zip,
              load.destination?.city,
              load.destination?.state,
              load.destination?.zip,
            ];
            return fieldsToSearch.some((field) =>
              (field || "").toString().toLowerCase().includes(search)
            );
          });

    const max = Math.ceil(filtered.length / resultsPerPage) || 1;
    if (currentLivePage < max) {
      currentLivePage++; // ← increment the live counter
      updateUI();
    }
  });

  // ── EXPIRED TAB PAGINATION + FILTERS ────────────────────────────────
  $("#previousExpiredPage").on("click", function () {
    if (currentExpiredPage > 1) {
      currentExpiredPage--;
      updateExpiredUI();
    }
  });

  $("#nextExpiredPage").on("click", function () {
    const max = Math.ceil(expiredResults.length / resultsPerPage) || 1;
    if (currentExpiredPage < max) {
      currentExpiredPage++;
      updateExpiredUI();
    }
  });

  $("#expiredQuery").on("input", function () {
    currentExpiredPage = 1;
    updateExpiredUI();
  });

  $("#clearExpiredFilters").on("click", function () {
    $("#expiredQuery").val("");
    currentExpiredPage = 1;
    updateExpiredUI();
  });

  // ── FILTER INPUT ───────────────────────────────────────────────────
  $("#searchQuery").on("input", function () {
    currentLivePage = 1;
    updateUI(); // instant feedback while typing
    if (!isSearchActive()) {
      // search just cleared → paint any queued loads immediately
      scheduleUIRender(true);
    }
  });

  // ── HOVER / CLIPBOARD ICONS ────────────────────────────────────────
  function findLoadByNumber(loadNumber) {
    // force both to strings so "123" and 123 match
    const key = loadNumber.toString();
    return searchResults.find((load) => load.loadNumber.toString() === key);
  }
  function formatClipboardData(load) {
    // Use the exact same logic as updateUI() to format pick‐up / delivery times:
    const pickUpDate = clipboardFormatDate(load);
    const deliveryDate = clipboardFormatDelivery(load);

    return `
Pick-up at: ${load.origin?.city || "N/A"}, ${load.origin?.state || "N/A"} ${
      load.origin?.zip || "N/A"
    }
Pick-up date (local): ${pickUpDate}

Deliver to: ${load.destination?.city || "N/A"}, ${
      load.destination?.state || "N/A"
    } ${load.destination?.zip || "N/A"}
Delivery date (local): ${deliveryDate}

Notes: ${load.comment || "N/A"}
Miles: ${load.distance.miles}
Pieces: ${load.pieces || "N/A"}
Weight: ${load.weight.pounds} lb.
Dims: ${load.equipment.length.standard}x${load.equipment.width.standard}x${
      load.equipment.height.standard
    } in.
Suggested Truck Size: ${
      truckTypeMapping[load.AdditionalData.TruckType] ||
      load.AdditionalData.TruckType ||
      "N/A"
    }

Driver Name: ${load.AdditionalData.DriverName || "N/A"}

Outmile: ${Math.floor(load.deadHeadDistance) || "N/A"}
  `.trim();
  }

  $(document).on("mouseenter", ".clipboard-icon", function () {
    const load = findLoadByNumber($(this).data("load-number"));
    const details = formatClipboardData(load);
    $("#hoverDetails")
      .html(details)
      .css({
        display: "block",
        top: $(this).offset().top + 20,
        left: Math.max(
          $(this).offset().left - $("#hoverDetails").outerWidth() - 20,
          0
        ),
      });
  });
  $(document).on("mouseleave", ".clipboard-icon", () => {
    $("#hoverDetails").hide();
  });
  $(document).on("click", ".clipboard-icon", function () {
    const load = findLoadByNumber($(this).data("load-number"));
    const formattedData = formatClipboardData(load);
    const dummy = $("<textarea>").val(formattedData).appendTo("body").select();
    document.execCommand("copy");
    dummy.remove();
    alert("Shipment details copied to clipboard");
  });

  function updateExpiredUI() {
    const filter = $("#expiredQuery").val().trim().toLowerCase();

    const filtered = (
      Array.isArray(expiredResults) ? expiredResults : []
    ).filter((load) => {
      return (
        (typeof load?.loadNumber === "string" ||
        typeof load?.loadNumber === "number"
          ? load.loadNumber.toString()
          : ""
        ).includes(filter) ||
        (load?.AdditionalData?.DriverName || "").toLowerCase().includes(filter)
      );
    });

    /* in updateExpiredUI() use currentExpiredPage …*/
    const maxPageE = Math.ceil(filtered.length / resultsPerPage) || 1;
    currentExpiredPage = Math.min(currentExpiredPage, maxPageE);
    const startE = (currentExpiredPage - 1) * resultsPerPage;
    const paginated = filtered.slice(startE, startE + resultsPerPage); // ← use startE

    $("#expiredTableBody").empty();
    (Array.isArray(paginated) ? paginated : []).forEach((load) => {
      // Defensive checks for all chained properties
      const originCity = load?.origin?.city || "N/A";
      const originState = load?.origin?.state || "N/A";
      const originZip = load?.origin?.zip || "N/A";
      const destinationCity = load?.destination?.city || "Unknown";
      const destinationState = load?.destination?.state || "N/A";
      const destinationZip = load?.destination?.zip || "N/A";
      const weight = load?.weight?.pounds ?? 0;
      const deadHeadDistance = Math.floor(load?.deadHeadDistance ?? 0) || "N/A";
      const miles = load?.distance?.miles ?? "N/A";
      const userName = load?.AdditionalData?.UserName || "N/A";
      const driverName = load?.AdditionalData?.DriverName || "N/A";
      const truckType = load?.AdditionalData?.TruckType || "N/A";
      const equipmentLength = load?.equipment?.length?.standard ?? "N/A";
      const dispatcherName = load?.AdditionalData?.DispatcherName || "N/A";
      const bookingContactPhoneNumber = load?.bookingContactPhoneNumber || "";
      const ownerPhoneNumber = load?.AdditionalData?.PhoneNumber || "N/A";
      const supplier = load?.supplier || "N/A";
      const loadType = load?.loadType || "N/A";
      const loadNumber = load?.loadNumber ?? "N/A";

      $("#expiredTableBody").append(`
      <tr>
        <td>${loadNumber}</td> 
        <td>${originCity}, ${originState}, ${originZip}</td>
        <td>${destinationCity}, ${destinationState}, ${destinationZip}</td>
        <td>${weight !== 0 ? weight : "N/A"}</td>
        <td>${deadHeadDistance} mi</td>
        <td>${miles} mi</td>
        <td>${userName}</td>
        <td>${driverName}</td>
        <td>${truckType}</td>
        <td>${equipmentLength} ft</td>
        <td>${dispatcherName}</td>
        <td>${bookingContactPhoneNumber}</td>
        <td>${ownerPhoneNumber}</td>
        <td>${supplier}</td>
        <td>${loadType}</td>
        <td><button class="btn btn-secondary" disabled>Expired</button></td>
      </tr>
    `);
    });

    $("#expiredLoadsFound").text(`Expired Loads: ${filtered.length}`);
    $("#expiredCurrentPage").text(`Page ${currentExpiredPage} of ${maxPageE}`);
    /* buttons for expired … */
    $("#previousExpiredPage").prop("disabled", currentExpiredPage === 1);
    $("#nextExpiredPage").prop("disabled", currentExpiredPage === maxPageE);

    // PAGINATION: Render numbered page buttons for expired
    const $expiredPagination = $("#expiredPageButtons");
    if ($expiredPagination.length) {
      $expiredPagination.empty();
      for (let i = 0; i < maxPageE; i++) {
        const button = $("<button>")
          .addClass("btn btn-sm btn-light mx-1 page-number-btn")
          .text(i + 1)
          .on("click", function () {
            currentExpiredPage = i + 1;
            updateExpiredUI();
          });
        button
          .toggleClass("active", i === currentExpiredPage - 1)
          .attr("aria-current", i === currentExpiredPage - 1 ? "page" : null);
        $expiredPagination.append(button);
      }
    }
  }

  $('button[data-bs-target="#expired"]').on("click", function () {
    updateExpiredUI();
  });

  // ── PASSWORD PROTECTION (unchanged) ─────────────────────────────────
  function handlePasswordAuthentication() {
    const correctPassword = "truckapi3";
    const enteredPassword = $("#passwordInput").val();
    if (enteredPassword === correctPassword) {
      localStorage.setItem("lastAuthTime", new Date().getTime());
      $("#passwordPrompt").hide();
      $("#mainContent").show();
    } else {
      $("#passwordError").show();
    }
  }
  function checkPasswordTimeout() {
    const lastAuthTime = localStorage.getItem("lastAuthTime");
    const fiveHoursInMillis = 5 * 60 * 60 * 1000;
    if (
      lastAuthTime &&
      new Date().getTime() - lastAuthTime < fiveHoursInMillis
    ) {
      $("#passwordPrompt").hide();
      $("#mainContent").show();
    } else {
      $("#passwordPrompt").show();
      $("#mainContent").hide();
    }
  }
  checkPasswordTimeout();
  $("#submitPassword").on("click", handlePasswordAuthentication);
  $("#passwordInput").on("keypress", (e) => {
    if (e.which === 13) handlePasswordAuthentication();
  });
  // ── DRIVER NAME FILTER DROPDOWN ────────────────────────────────────
  $("#driverNameHeader").on("click", function (e) {
    e.stopPropagation();
    const $menu = $("#driverDropdown");

    // 1) grab unique, non-empty driver names and sort them
    const names = Array.from(
      new Set(
        searchResults
          .map((l) => l.AdditionalData.DriverName)
          .filter(
            (value) =>
              value && typeof value === "string" && value.trim().length > 0
          )
      )
    ).sort();

    // 2) build menu items
    $menu.empty();
    names.forEach((name) => {
      $("<a>")
        .addClass("dropdown-item")
        .attr("href", "#")
        .text(name)
        .on("click", function (ev) {
          ev.preventDefault();
          selectedDriver = name;
          $("#selectedDriverLabel").text(`(${name})`);
          updateUI();
          $menu.hide();
        })
        .appendTo($menu);
    });

    // ← INSERT the “Show All” option right here:
    $('<div class="dropdown-divider">').appendTo($menu);
    $("<a>")
      .addClass("dropdown-item text-muted")
      .attr("href", "#")
      .text("Show All")
      .on("click", function (ev) {
        ev.preventDefault();
        selectedDriver = null;
        $("#selectedDriverLabel").text("");
        updateUI();
        $menu.hide();
      })
      .appendTo($menu);

    // 3) toggle visibility
    $menu.toggle();
  });

  // clicking *anywhere else* hides it
  $(document).on("click", () => $("#driverDropdown").hide());

  // ── LOAD TYPE FILTER DROPDOWN ───────────────────────────────────────
  $("#loadTypeHeader").on("click", function (e) {
    e.stopPropagation();
    const $menu = $("#loadTypeDropdown");
    const types = ["Full", "Partial"];

    $menu.empty();
    types.forEach((type) => {
      $("<a>")
        .addClass("dropdown-item")
        .attr("href", "#")
        .text(type)
        .on("click", (ev) => {
          ev.preventDefault();
          selectedLoadType = type;
          $("#selectedLoadTypeLabel").text(`(${type})`);
          updateUI();
          $menu.hide();
        })
        .appendTo($menu);
    });
    // also add a “Clear” option
    $('<div class="dropdown-divider">').appendTo($menu);
    $("<a>")
      .addClass("dropdown-item text-muted")
      .attr("href", "#")
      .text("Show All")
      .on("click", (ev) => {
        ev.preventDefault();
        selectedLoadType = null;
        $("#selectedLoadTypeLabel").text("");
        updateUI();
        $menu.hide();
      })
      .appendTo($menu);

    $menu.toggle();
  });

  $("#clearFilters").on("click", function () {
    // reset in‑memory filters
    selectedDriver = null;
    selectedLoadType = null;
    selectedOwner = null;
    selectedSupplier = null;
    selectedDispatcher = null;

    // reset the little labels
    $(
      "#selectedDriverLabel, #selectedLoadTypeLabel, #selectedOwnerLabel, #selectedSupplierLabel, #selectedDispatcherLabel"
    ).text("");

    // hide any open dropdown menus
    $(
      "#driverDropdown, #loadTypeDropdown, #ownerDropdown, #supplierDropdown, #dispatcherDropdown"
    ).hide();

    // reset pagination
    currentLivePage = 1;
    currentExpiredPage = 1;

    // re‑render
    updateUI();
    updateExpiredUI();
  });
  // hide loadType dropdown on outside click
  $(document).on("click", () => $("#loadTypeDropdown").hide());
  // hide dispatcher dropdown on outside click
  $(document).on("click", function () {
    $("#dispatcherDropdown").hide();
  });
});

// Remove any block that overwrites AdditionalData.PhoneNumber from bookingContactPhoneNumber for Truckstop:
// Example (DO NOT add this line to code, just for clarity):
// if (supplier === "Truckstop") {
//   load.AdditionalData.PhoneNumber = load.bookingContactPhoneNumber;
// }
