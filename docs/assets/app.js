// The feed is one JSON file filtered in the browser. Filters live in the URL,
// so a filtered view is a link you can send or bookmark.
(function () {
  "use strict";

  var base = document.documentElement.getAttribute("data-base") || "";

  // ------------------------------------------------------------ relative time
  function plural(n, one, few, many) {
    var m = n % 100;
    if (m >= 11 && m <= 14) return many;
    m = n % 10;
    if (m === 1) return one;
    if (m >= 2 && m <= 4) return few;
    return many;
  }
  var MONTHS = ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"];
  function ago(ts) {
    var s = Math.max(0, Date.now() / 1000 - ts);
    if (s < 3600) { var m = Math.max(1, Math.round(s / 60)); return m + " " + plural(m, "минуту", "минуты", "минут") + " назад"; }
    if (s < 86400) { var h = Math.round(s / 3600); return h + " " + plural(h, "час", "часа", "часов") + " назад"; }
    var d = Math.floor(s / 86400);
    if (d === 1) return "вчера";
    if (d < 30) return d + " " + plural(d, "день", "дня", "дней") + " назад";
    var dt = new Date(ts * 1000);
    return dt.getDate() + " " + MONTHS[dt.getMonth()] + " " + dt.getFullYear();
  }
  function fillTimes(root) {
    var nodes = (root || document).querySelectorAll("time[data-ts]");
    for (var i = 0; i < nodes.length; i++) {
      var ts = +nodes[i].getAttribute("data-ts");
      if (ts > 0) nodes[i].textContent = ago(ts);
    }
  }
  fillTimes();

  var list = document.getElementById("list");
  var form = document.getElementById("filters");
  if (!list || !form) return;

  var countEl = document.getElementById("count");
  var countWord = document.getElementById("count-word");
  var emptyEl = document.getElementById("empty");
  var moreBtn = document.getElementById("more");
  var sortEl = document.getElementById("sort");
  var filterCount = document.getElementById("filter-count");
  var details = document.getElementById("filters-details");

  var PAGE = 40;
  var jobs = [];
  var shown = 0;
  var matched = [];

  if (window.matchMedia("(max-width: 860px)").matches && details) details.open = false;

  // ------------------------------------------------------------ rendering
  var GRADES = { intern: "Стажёр", junior: "Junior", middle: "Middle", senior: "Senior", lead: "Lead" };
  var FORMATS = { remote: "Удалёнка", hybrid: "Гибрид", office: "Офис" };
  var CONTACTS = { email: "Email", telegram: "Telegram", url: "Ссылка" };

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  function cardHTML(j) {
    var meta = "";
    if (j.c) meta += '<span class="company">' + esc(j.c) + "</span>";
    if (j.loc) meta += "<span>" + esc(j.loc) + "</span>";
    meta += '<time data-ts="' + j.d + '">' + ago(j.d) + "</time>";

    var chips = "";
    if (j.gm) chips += '<span class="chip chip-go">Go — основной</span>';
    if (j.rg && j.rg[0] === "world") chips += '<span class="chip chip-format">Из любой страны</span>';
    (j.f || []).forEach(function (f) { chips += '<span class="chip chip-format">' + FORMATS[f] + "</span>"; });
    (j.g || []).forEach(function (g) { chips += '<span class="chip chip-grade">' + GRADES[g] + "</span>"; });
    if (j.r) chips += '<span class="chip chip-format">Релокация</span>';
    (j.k || []).forEach(function (k) { chips += '<span class="chip">' + esc(k) + "</span>"; });

    var contacts = (j.ct || []).map(function (c) {
      return '<span class="contact contact-' + c + '">' + CONTACTS[c] + "</span>";
    }).join("");

    return '<article class="card">' +
      '<div class="card-top"><h3 class="card-title"><a href="' + base + "/jobs/" + esc(j.s) + '/">' + esc(j.t) + "</a></h3>" +
      (j.sal ? '<span class="card-salary">' + esc(j.sal) + "</span>" : "") + "</div>" +
      '<p class="card-meta">' + meta + "</p>" +
      (j.m ? '<p class="card-sum">' + esc(j.m) + "</p>" : "") +
      '<div class="chips">' + chips + "</div>" +
      '<div class="card-foot"><span class="contacts">' + contacts + '</span><span class="src">' + esc(j.sn) +
      (j.n ? " и ещё " + j.n : "") + "</span></div>" +
      "</article>";
  }

  function renderMore() {
    var next = matched.slice(shown, shown + PAGE);
    list.insertAdjacentHTML("beforeend", next.map(cardHTML).join(""));
    shown += next.length;
    moreBtn.hidden = shown >= matched.length;
  }

  // ------------------------------------------------------------ filtering
  function readForm() {
    var data = new FormData(form);
    return {
      q: (data.get("q") || "").trim().toLowerCase(),
      direct: data.get("direct") === "1",
      ct: data.getAll("ct"),
      gm: data.get("gm") === "1",
      rg: data.getAll("rg"),
      f: data.getAll("f"),
      g: data.getAll("g"),
      sal: +data.get("sal") || 0,
      hassal: data.get("hassal") === "1",
      src: data.getAll("src"),
      l: data.getAll("l"),
      r: data.get("r") === "1",
      days: +data.get("days") || 0
    };
  }

  function any(have, want) {
    if (!want.length) return true;
    if (!have) return false;
    for (var i = 0; i < want.length; i++) if (have.indexOf(want[i]) >= 0) return true;
    return false;
  }

  function matches(j, f, now) {
    if (f.q) {
      var hay = (j.t + " " + (j.c || "") + " " + (j.m || "") + " " + (j.k || []).join(" ") + " " + (j.loc || "")).toLowerCase();
      var words = f.q.split(/\s+/);
      for (var i = 0; i < words.length; i++) if (hay.indexOf(words[i]) < 0) return false;
    }
    if (f.direct && !any(j.ct, ["email", "telegram"])) return false;
    if (!any(j.ct, f.ct)) return false;
    if (f.gm && !j.gm) return false;
    if (!any(j.rg, f.rg)) return false;
    if (!any(j.f, f.f)) return false;
    if (!any(j.g, f.g)) return false;
    if (!any([j.src], f.src)) return false;
    if (!any([j.l], f.l)) return false;
    if (f.r && !j.r) return false;
    if ((f.hassal || f.sal) && !(j.u0 || j.u1)) return false;
    if (f.sal && Math.max(j.u0 || 0, j.u1 || 0) < f.sal) return false;
    if (f.days && now - j.d > f.days * 86400) return false;
    return true;
  }

  function apply(pushURL) {
    var f = readForm();
    var now = Date.now() / 1000;
    matched = jobs.filter(function (j) { return matches(j, f, now); });
    if (sortEl.value === "salary") {
      matched.sort(function (a, b) { return Math.max(b.u0 || 0, b.u1 || 0) - Math.max(a.u0 || 0, a.u1 || 0) || b.d - a.d; });
    }

    list.innerHTML = "";
    shown = 0;
    renderMore();
    countEl.textContent = matched.length;
    countWord.textContent = plural(matched.length, "вакансия", "вакансии", "вакансий");
    emptyEl.hidden = matched.length > 0;

    var active = activeCount(f);
    filterCount.hidden = active === 0;
    filterCount.textContent = active;
    if (pushURL) writeURL();
  }

  function activeCount(f) {
    return (f.q ? 1 : 0) + (f.gm ? 1 : 0) + f.rg.length + (f.direct ? 1 : 0) + f.ct.length + f.f.length + f.g.length + (f.sal ? 1 : 0) +
      (f.hassal ? 1 : 0) + f.src.length + f.l.length + (f.r ? 1 : 0) + (f.days ? 1 : 0);
  }

  // ------------------------------------------------------------ URL state
  function writeURL() {
    var params = new URLSearchParams();
    new FormData(form).forEach(function (v, k) { if (v !== "") params.append(k, v); });
    if (sortEl.value !== "new") params.set("sort", sortEl.value);
    var qs = params.toString();
    history.replaceState(null, "", location.pathname + (qs ? "?" + qs : ""));
  }

  function readURL() {
    var params = new URLSearchParams(location.search);
    var els = form.elements;
    for (var i = 0; i < els.length; i++) {
      var el = els[i];
      if (!el.name) continue;
      var vals = params.getAll(el.name);
      if (el.type === "checkbox") el.checked = vals.indexOf(el.value) >= 0;
      else if (vals.length) el.value = vals[0];
    }
    if (params.get("sort")) sortEl.value = params.get("sort");
  }

  // ------------------------------------------------------------ wiring
  var timer;
  form.addEventListener("input", function (e) {
    clearTimeout(timer);
    timer = setTimeout(function () { apply(true); }, e.target.type === "search" || e.target.type === "number" ? 200 : 0);
  });
  form.addEventListener("reset", function () { setTimeout(function () { apply(true); }, 0); });
  sortEl.addEventListener("change", function () { apply(true); });
  moreBtn.addEventListener("click", renderMore);

  fetch(base + "/data/jobs.json")
    .then(function (r) { if (!r.ok) throw new Error(r.status); return r.json(); })
    .then(function (data) {
      jobs = data.jobs || [];
      readURL();
      apply(false);
    })
    .catch(function () {
      // The server-rendered first page stays; only filtering is unavailable.
      moreBtn.hidden = true;
    });
})();
