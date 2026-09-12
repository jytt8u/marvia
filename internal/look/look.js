/* ──────────────────────────────────────────────────────────────────────────
   Тема, общая для панели и клиентов.

   Этот файл вшивается в страницу на месте метки look.Marker при отдаче —
   см. look.go. Единственный источник цветов: правка здесь красит и панель,
   и окно на компьютере, а генератор переписывает таблицу в Kotlin для
   телефона. Копии таблицы в страницах быть не должно.

   Взято из дизайна дословно: 27 пресетов, свет (откуда светит, характер,
   густота тени, оттенок), скругления, плотность, кнопка, свечение, карточки
   и арифметика контраста, которая подбирает приглушённый цвет так, чтобы он
   оставался читаемым на всех фонах, где им пишут.

   Названий тем здесь намеренно нет: они переводятся, а словари живут в
   страницах. Ключи пресетов — единственное, что связывает таблицу и
   словарь, и тест в каждой странице проверяет, что ни один не осиротел.
   ────────────────────────────────────────────────────────────────────────── */

const Look = (() => {
  const PRESETS = {
    emerald:  { bg: "#06100c", fg: "#e8f6ef", acc: "#1fd18d" },
    jade:     { bg: "#071310", fg: "#e3f5f0", acc: "#33d6c0" },
    teal:     { bg: "#04100f", fg: "#dff5f3", acc: "#00d4c8" },
    ice:      { bg: "#060c14", fg: "#e2f0fb", acc: "#54c8ff" },
    cobalt:   { bg: "#070a18", fg: "#e6eaff", acc: "#5c7cff" },
    ultra:    { bg: "#05061a", fg: "#e4e7ff", acc: "#3d5bff" },
    plum:     { bg: "#0b0714", fg: "#efe7fb", acc: "#a86dff" },
    orchid:   { bg: "#100716", fg: "#f7e8fb", acc: "#e46ce0" },
    fuchsia:  { bg: "#120616", fg: "#fbe6f6", acc: "#ff5ecd" },
    rose:     { bg: "#120709", fg: "#fbe8ec", acc: "#ff5f7e" },
    crimson:  { bg: "#130508", fg: "#fbe3e6", acc: "#ff3b52" },
    ember:    { bg: "#120902", fg: "#fbeadc", acc: "#ff7a3d" },
    amber:    { bg: "#100c04", fg: "#f8f0dd", acc: "#ffc247" },
    gold:     { bg: "#0f0d05", fg: "#f7f1dc", acc: "#e8c25a" },
    citrus:   { bg: "#0c0f05", fg: "#f0f5dd", acc: "#c9e64b" },
    lime:     { bg: "#080f06", fg: "#ecf7e2", acc: "#8ff04f" },
    olive:    { bg: "#0a0d07", fg: "#eaf1de", acc: "#96bf5c" },
    sand:     { bg: "#100d09", fg: "#f5ede1", acc: "#d9a97a" },
    copper:   { bg: "#110a06", fg: "#f8e9dd", acc: "#e08b4c" },
    steel:    { bg: "#0c0e11", fg: "#f1f4f7", acc: "#c3ccd6" },
    slate:    { bg: "#0e1013", fg: "#eef1f4", acc: "#8fa3b8" },
    mono:     { bg: "#0a0a0a", fg: "#fafafa", acc: "#fafafa" },
    oled:     { bg: "#000000", fg: "#ffffff", acc: "#3dffc0" },
    midnight: { bg: "#03050a", fg: "#dfe8f5", acc: "#4d8cff" },
    daylight: { bg: "#f3f5f7", fg: "#0d1013", acc: "#046b4a" },
    paper:    { bg: "#f7f4ee", fg: "#14120e", acc: "#8a4b1f" },
    linen:    { bg: "#f4f1ea", fg: "#171512", acc: "#3f6b4a" },
  };

  // Плотность — отступ карточки и зазор между ними. Скругление живёт отдельно
  // (RADII): в дизайне это разные ручки, и «просторно с острыми углами» —
  // законный вид.
  const DENSITY = {
    compact: { pad: 12, gap: 8 },
    normal:  { pad: 16, gap: 12 },
    roomy:   { pad: 20, gap: 16 },
  };

  const RADII = { sharp: 6, soft: 14, round: 22, pill: 30 };

  // Свечение акцента — сила тени под кнопкой питания и главной кнопкой.
  const GLOWS = { none: 0, soft: 0.2, mid: 0.42, neon: 0.72 };

  // Свет: характер фона, откуда он светит, и как это записывается в CSS.
  // «c» — из центра: у линейного света это свет посередине и тень по краям.
  const KINDS = ["flat", "linear", "radial", "aurora"];
  const DIRS = {
    n: [0, "top"], ne: [45, "top right"], e: [90, "right"], se: [135, "bottom right"],
    s: [180, "bottom"], sw: [225, "bottom left"], w: [270, "left"], nw: [315, "top left"],
    c: [315, "center"],
  };

  // Кнопка питания: диск, кольцо, стекло, контур.
  const BUTTONS = ["solid", "ring", "glass", "bare"];
  // Карточки: без обводки, с обводкой, с тенью.
  const CARDS = ["flat", "outline", "shadow"];

  // Готовые виды: пресет и все ручки разом. Порядок — часть договора:
  // названия лежат в словарях страниц под теми же номерами, и перестановка
  // здесь подпишет «Изумруд» «Нефритом».
  const LOOKS = [
    { preset: "steel",    kind: "linear", dir: "nw", depth: 0.55, radius: "soft",  density: "normal",  btn: "ring",  glow: "soft", card: "outline" },
    { preset: "emerald",  kind: "linear", dir: "nw", depth: 0.62, radius: "soft",  density: "normal",  btn: "ring",  glow: "mid",  card: "outline" },
    { preset: "jade",     kind: "radial", dir: "n",  depth: 0.58, radius: "round", density: "normal",  btn: "ring",  glow: "mid",  card: "outline" },
    { preset: "teal",     kind: "aurora", dir: "ne", depth: 0.66, radius: "round", density: "normal",  btn: "solid", glow: "mid",  card: "flat" },
    { preset: "ice",      kind: "aurora", dir: "ne", depth: 0.80, radius: "round", density: "normal",  btn: "solid", glow: "neon", card: "flat" },
    { preset: "cobalt",   kind: "linear", dir: "nw", depth: 0.70, radius: "soft",  density: "normal",  btn: "ring",  glow: "mid",  card: "outline" },
    { preset: "ultra",    kind: "radial", dir: "c",  depth: 0.82, radius: "round", density: "roomy",   btn: "solid", glow: "neon", card: "shadow" },
    { preset: "plum",     kind: "radial", dir: "se", depth: 0.80, radius: "round", density: "roomy",   btn: "glass", glow: "mid",  card: "shadow" },
    { preset: "orchid",   kind: "aurora", dir: "w",  depth: 0.72, radius: "pill",  density: "normal",  btn: "glass", glow: "mid",  card: "flat" },
    { preset: "fuchsia",  kind: "aurora", dir: "sw", depth: 0.70, radius: "round", density: "normal",  btn: "solid", glow: "neon", card: "flat" },
    { preset: "rose",     kind: "linear", dir: "s",  depth: 0.60, radius: "pill",  density: "roomy",   btn: "solid", glow: "mid",  card: "outline" },
    { preset: "crimson",  kind: "radial", dir: "se", depth: 0.85, radius: "sharp", density: "compact", btn: "ring",  glow: "mid",  card: "flat" },
    { preset: "ember",    kind: "aurora", dir: "s",  depth: 0.75, radius: "soft",  density: "normal",  btn: "solid", glow: "mid",  card: "shadow" },
    { preset: "amber",    kind: "linear", dir: "s",  depth: 0.45, radius: "pill",  density: "roomy",   btn: "solid", glow: "soft", card: "flat" },
    { preset: "gold",     kind: "radial", dir: "n",  depth: 0.55, radius: "round", density: "roomy",   btn: "glass", glow: "soft", card: "outline" },
    { preset: "citrus",   kind: "linear", dir: "ne", depth: 0.50, radius: "soft",  density: "normal",  btn: "solid", glow: "mid",  card: "flat" },
    { preset: "lime",     kind: "aurora", dir: "nw", depth: 0.50, radius: "round", density: "normal",  btn: "solid", glow: "mid",  card: "flat" },
    { preset: "olive",    kind: "linear", dir: "w",  depth: 0.52, radius: "soft",  density: "normal",  btn: "ring",  glow: "soft", card: "outline" },
    { preset: "sand",     kind: "linear", dir: "nw", depth: 0.42, radius: "pill",  density: "roomy",   btn: "glass", glow: "soft", card: "outline" },
    { preset: "copper",   kind: "linear", dir: "e",  depth: 0.55, radius: "pill",  density: "roomy",   btn: "glass", glow: "soft", card: "outline" },
    { preset: "steel",    kind: "flat",   dir: "n",  depth: 0.25, radius: "sharp", density: "compact", btn: "bare",  glow: "none", card: "outline" },
    { preset: "slate",    kind: "linear", dir: "n",  depth: 0.40, radius: "soft",  density: "compact", btn: "ring",  glow: "none", card: "outline" },
    { preset: "mono",     kind: "flat",   dir: "n",  depth: 0.30, radius: "sharp", density: "compact", btn: "bare",  glow: "none", card: "flat" },
    { preset: "oled",     kind: "flat",   dir: "n",  depth: 0.95, radius: "soft",  density: "compact", btn: "ring",  glow: "soft", card: "outline" },
    { preset: "midnight", kind: "radial", dir: "c",  depth: 0.85, radius: "soft",  density: "normal",  btn: "ring",  glow: "mid",  card: "shadow" },
    { preset: "daylight", kind: "linear", dir: "nw", depth: 0.40, radius: "soft",  density: "normal",  btn: "ring",  glow: "soft", card: "outline" },
    { preset: "paper",    kind: "linear", dir: "nw", depth: 0.40, radius: "soft",  density: "roomy",   btn: "ring",  glow: "none", card: "outline" },
    { preset: "linen",    kind: "flat",   dir: "n",  depth: 0.30, radius: "pill",  density: "roomy",   btn: "glass", glow: "none", card: "flat" },
  ];

  // Вид из коробки — первый из готовых. Серый, а не изумрудный: первое
  // впечатление должно быть нейтральным, а цвет человек выбирает сам — для
  // того вкладка и есть. acc и tint — null: «из пресета».
  const DEFAULT = Object.assign({ acc: null, tint: null }, LOOKS[0]);

  // Ключи выбора — то, что хранится, кодируется и сравнивается.
  const KEYS = ["preset", "acc", "kind", "dir", "depth", "tint", "radius", "density", "btn", "glow", "card"];

  function hex(h) {
    const v = h.replace("#", "");
    const n = v.length === 3 ? v.split("").map((c) => c + c).join("") : v;
    return [parseInt(n.slice(0, 2), 16), parseInt(n.slice(2, 4), 16), parseInt(n.slice(4, 6), 16)];
  }
  function mix(a, b, t) {
    const A = hex(a), B = hex(b);
    return "#" + A.map((c, i) => Math.round(c + (B[i] - c) * t).toString(16).padStart(2, "0")).join("");
  }
  function lum(h) {
    const [r, g, b] = hex(h);
    return (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  }
  // Относительная яркость и контраст по WCAG: приглушённый текст подбирается
  // не на глаз, а до порога 4.5, ниже которого его перестают читать.
  function rl(h) {
    return hex(h).map((c) => {
      const s = c / 255;
      return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    }).reduce((a, v, i) => a + v * [0.2126, 0.7152, 0.0722][i], 0);
  }
  function ratio(a, b) {
    const x = rl(a), y = rl(b);
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
  }
  function bestOn(bg) {
    return ratio("#08110d", bg) >= ratio("#ffffff", bg) ? "#08110d" : "#ffffff";
  }
  function readableDim(fg, grounds, start) {
    const list = [].concat(grounds), tone = list[list.length - 1];
    const ok = (c) => Math.min.apply(null, list.map((g) => ratio(c, g))) >= 4.5;
    let t = Math.min(start == null ? 0.55 : start, 0.55), out = mix(fg, tone, t);
    while (t > 0 && !ok(out)) { t = Math.max(0, t - 0.04); out = mix(fg, tone, t); }
    return out;
  }

  const isHex = (v) => typeof v === "string" && /^#[0-9a-f]{6}$/i.test(v);

  // normalize приводит любой выбор к полному и допустимому: чужое поле —
  // из вида по умолчанию, поле по полю, чтобы одна битая плотность не
  // сбросила ещё и цвет.
  function normalize(st) {
    const v = st || {};
    const out = Object.assign({}, DEFAULT);
    if (PRESETS[v.preset]) out.preset = v.preset;
    if (isHex(v.acc)) out.acc = v.acc.toLowerCase();
    if (KINDS.indexOf(v.kind) >= 0) out.kind = v.kind;
    if (DIRS[v.dir]) out.dir = v.dir;
    if (typeof v.depth === "number" && v.depth >= 0.08 && v.depth <= 0.98) out.depth = Math.round(v.depth * 100) / 100;
    if (isHex(v.tint)) out.tint = v.tint.toLowerCase();
    if (RADII[v.radius] != null) out.radius = v.radius;
    if (DENSITY[v.density]) out.density = v.density;
    if (BUTTONS.indexOf(v.btn) >= 0) out.btn = v.btn;
    if (GLOWS[v.glow] != null) out.glow = v.glow;
    if (CARDS.indexOf(v.card) >= 0) out.card = v.card;
    return out;
  }

  // background собирает CSS фона по свету. Отдельно от theme, потому что его
  // же рисуют мини-макеты видов и лампа «откуда светит».
  function background(kind, dir, lit, mid, shade, bg, tint) {
    if (kind === "flat") return bg;
    const centered = dir === "c";
    const [deg, pos] = DIRS[dir] || DIRS.nw;
    if (centered && kind === "linear") {
      return "linear-gradient(" + deg + "deg," + shade + " 0%," + lit + " 50%," + shade + " 100%)";
    }
    if (kind === "radial") {
      return "radial-gradient(130% 100% at " + pos + "," + lit + " 0%," + mid + " 45%," + shade + " 100%)";
    }
    if (kind === "aurora") {
      return "radial-gradient(90% 60% at " + pos + "," + mix(lit, tint, 0.25) + " 0%," + shade + " 62%),linear-gradient("
        + deg + "deg," + lit + "," + shade + ")";
    }
    return "linear-gradient(" + deg + "deg," + lit + " 0%," + mix(lit, shade, 0.55) + " 52%," + shade + " 100%)";
  }

  // Ключ, под которым вид хранится в браузере. Общий: панель и окно живут в
  // разных origin, и мешать друг другу не могут, а одно имя проще помнить.
  const KEY = "marvia-look";
  // Профили — три сохранённых вида, кодами.
  const KEY_PROFILES = "marvia-look-profiles";

  // theme считает цвета по выбору. Возвращает плоский набор токенов; страница
  // раскладывает их в CSS-переменные через vars.
  function theme(raw) {
    const st = normalize(raw);
    const base = PRESETS[st.preset];
    const acc = st.acc || base.acc;
    const bg = base.bg, dark = lum(bg) < 0.5;
    const tint = st.tint || acc;

    // Свет: освещённая сторона подкрашена оттенком, тёмная — уведена в
    // чёрный на глубину тени. Ровный фон — без света: поверхности тогда
    // считаются от самого фона, как в пресете.
    const flat = st.kind === "flat";
    const lit = flat ? bg : mix(bg, tint, dark ? 0.30 : 0.16);
    const shade = flat ? bg : mix(bg, "#000000", dark ? st.depth : st.depth * 0.22);
    const mid = flat ? bg : mix(lit, shade, 0.5);

    const surf = dark ? mix(mid, "#ffffff", 0.06) : mix(mid, "#ffffff", 0.55);
    const surf2 = dark ? mix(mid, "#ffffff", 0.13) : mix(mid, "#000000", 0.07);
    const [r, g, b] = hex(acc);
    const glowA = GLOWS[st.glow];
    const d = DENSITY[st.density];

    return {
      bg, lit, mid, shade, acc, surf, surf2, dark,
      bgcss: background(st.kind, st.dir, lit, mid, shade, bg, tint),
      line: dark ? mix(mid, "#ffffff", 0.18) : mix(mid, "#000000", 0.14),
      fg: base.fg,
      dim: readableDim(base.fg, [lit, mid, shade, surf, surf2]),
      accFg: bestOn(acc),
      accSoft: "rgba(" + r + "," + g + "," + b + ",0.14)",
      accRgb: r + "," + g + "," + b,
      // Предупреждение и отказ одни на все темы: янтарный и красный должны
      // читаться как «внимание» независимо от акцента, иначе в розовой теме
      // ошибка сольётся с кнопкой.
      warn: "#f2a03d",
      fail: dark ? "#ff6b7a" : "#c8323f",
      glowA,
      // Тень свечения под акцентной кнопкой. Сила — из GLOWS, цвет — акцент.
      glow: glowA <= 0 ? "none"
        : "0 0 " + Math.round(30 * glowA) + "px rgba(" + r + "," + g + "," + b + "," + (0.30 + glowA * 0.5).toFixed(2) + ")",
      r: RADII[st.radius], pad: d.pad, gap: d.gap,
      btn: st.btn, card: st.card,
      // Обводка и тень карточки по выбранной подаче. Страницы пишут
      // border: 1px solid var(--cardLine) — и плоские карточки получают
      // прозрачную обводку той же толщины, ничего не прыгает.
      cardLine: st.card === "flat" ? "transparent" : (dark ? mix(mid, "#ffffff", 0.18) : mix(mid, "#000000", 0.14)),
      cardShadow: st.card === "shadow" ? "0 10px 24px rgba(0,0,0," + (dark ? "0.45" : "0.12") + ")" : "none",
    };
  }

  // vars собирает токены темы в строку CSS-переменных.
  function vars(t) {
    return Object.entries(t)
      .filter(([, v]) => typeof v === "string")
      .map(([k, v]) => "--" + k + ":" + v).join(";")
      + ";--r:" + t.r + "px;--pad:" + t.pad + "px;--gap:" + t.gap + "px;--glowA:" + t.glowA;
  }

  // Код темы — весь вид одной строкой, чтобы передать другу или в другой
  // клиент. Первые три буквы пресета уникальны, остальное — по две.
  const short = (s, n) => s.slice(0, n).toUpperCase();
  const find = (list, code, n) => list.find((k) => short(k, n) === code);

  function encode(raw) {
    const st = normalize(raw);
    return ["MV",
      short(st.preset, 3),
      st.acc ? st.acc.slice(1).toUpperCase() : "0",
      short(st.kind, 3) + st.dir.toUpperCase(),
      String(Math.round(st.depth * 100)),
      short(st.radius, 2) + short(st.density, 2),
      short(st.btn, 2) + short(st.glow, 2) + short(st.card, 2),
      st.tint ? st.tint.slice(1).toUpperCase() : "0",
    ].join("-");
  }

  // decode разбирает код. Возвращает null на чужом или битом: показать
  // человеку «код не наш» честнее, чем молча перекрасить в серый.
  function decode(code) {
    const p = String(code || "").trim().toUpperCase().split("-");
    if (p.length !== 8 || p[0] !== "MV") return null;
    const preset = find(Object.keys(PRESETS), p[1], 3);
    const kind = find(KINDS, p[3].slice(0, 3), 3);
    const dir = Object.keys(DIRS).find((k) => k.toUpperCase() === p[3].slice(3));
    const depth = parseInt(p[4], 10) / 100;
    const radius = find(Object.keys(RADII), p[5].slice(0, 2), 2);
    const density = find(Object.keys(DENSITY), p[5].slice(2), 2);
    const btn = find(BUTTONS, p[6].slice(0, 2), 2);
    const glow = find(Object.keys(GLOWS), p[6].slice(2, 4), 2);
    const card = find(CARDS, p[6].slice(4), 2);
    const color = (s) => (s === "0" ? null : /^[0-9A-F]{6}$/.test(s) ? "#" + s.toLowerCase() : undefined);
    const acc = color(p[2]), tint = color(p[7]);
    if (!preset || !kind || !dir || !(depth >= 0.08 && depth <= 0.98) || !radius || !density
      || !btn || !glow || !card || acc === undefined || tint === undefined) return null;
    return { preset, acc, kind, dir, depth, tint, radius, density, btn, glow, card };
  }

  // same — совпадает ли выбор с готовым видом (акцент и оттенок не в счёт:
  // вид задаёт форму, а цвет поверх него — уже своё).
  function same(st, lk) {
    const a = normalize(st);
    return Object.keys(lk).every((k) => a[k] === lk[k]);
  }

  // load и save — вид в localStorage. Чужое или испорченное значение не
  // роняет страницу: normalize берёт вид из коробки поле по полю.
  function load() {
    try {
      return normalize(JSON.parse(localStorage.getItem(KEY)));
    } catch (_) { return normalize(null); /* приватное окно или мусор */ }
  }
  function save(st) {
    const v = normalize(st), out = {};
    KEYS.forEach((k) => { out[k] = v[k]; });
    try { localStorage.setItem(KEY, JSON.stringify(out)); } catch (_) { /* приватное окно — вид просто не запомнится */ }
  }
  // Профили: массив из трёх кодов, пустой слот — null.
  function loadProfiles() {
    try {
      const v = JSON.parse(localStorage.getItem(KEY_PROFILES));
      return [0, 1, 2].map((i) => (Array.isArray(v) && decode(v[i]) ? v[i] : null));
    } catch (_) { return [null, null, null]; }
  }
  function saveProfiles(list) {
    try { localStorage.setItem(KEY_PROFILES, JSON.stringify(list)); } catch (_) { /* не запомнится */ }
  }

  // Все акценты, какие есть в пресетах, без повторов: из них выбирают цвет.
  const ACCENTS = Object.values(PRESETS).map((p) => p.acc).filter((c, i, a) => a.indexOf(c) === i);

  return { PRESETS, DENSITY, RADII, GLOWS, KINDS, DIRS, BUTTONS, CARDS, LOOKS, DEFAULT, KEYS, ACCENTS, KEY,
    hex, mix, lum, ratio, bestOn, readableDim, normalize, background, theme, vars,
    encode, decode, same, load, save, loadProfiles, saveProfiles };
})();
