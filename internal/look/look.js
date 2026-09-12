/* ──────────────────────────────────────────────────────────────────────────
   Тема, общая для панели и клиентов.

   Этот файл вшивается в страницу на месте метки look.Marker при отдаче —
   см. look.go. Единственный источник цветов: правка здесь красит и панель,
   и окно на компьютере. Копии таблицы в страницах быть не должно.

   Взято из дизайна дословно: 27 пресетов, три плотности и арифметика
   контраста, которая подбирает приглушённый цвет так, чтобы он оставался
   читаемым на всех фонах, где им пишут.

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

  const DENSITY = {
    compact: { r: 12, pad: 12, gap: 8 },
    normal:  { r: 16, pad: 16, gap: 12 },
    roomy:   { r: 22, pad: 20, gap: 16 },
  };

  // Готовые виды: пресет плюс плотность. Порядок — часть договора: названия
  // лежат в словарях страниц под теми же номерами, и перестановка здесь
  // подпишет «Изумруд» «Нефритом».
  const LOOKS = [
    ["steel", "normal"],
    ["emerald", "normal"], ["jade", "normal"], ["teal", "normal"],
    ["ice", "normal"], ["cobalt", "normal"], ["ultra", "roomy"],
    ["plum", "roomy"], ["orchid", "normal"], ["fuchsia", "normal"],
    ["rose", "roomy"], ["crimson", "compact"], ["ember", "normal"],
    ["amber", "roomy"], ["gold", "roomy"], ["citrus", "normal"],
    ["lime", "normal"], ["olive", "normal"], ["sand", "roomy"],
    ["copper", "roomy"], ["steel", "compact"], ["slate", "compact"],
    ["mono", "compact"], ["oled", "compact"], ["midnight", "normal"],
    ["daylight", "normal"], ["paper", "roomy"], ["linen", "roomy"],
  ];

  // Вид из коробки. Серый, а не изумрудный: первое впечатление должно быть
  // нейтральным, а цвет человек выбирает сам — для того вкладка и есть.
  const DEFAULT = { preset: "steel", acc: null, density: "normal" };

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

  // Ключ, под которым вид хранится в браузере. Общий: панель и окно живут в
  // разных origin, и мешать друг другу не могут, а одно имя проще помнить.
  const KEY = "marvia-look";

  // theme считает цвета по выбору: пресет, акцент (или null — из пресета).
  // Возвращает плоский набор токенов; страница раскладывает их в CSS-переменные.
  function theme(st) {
    const base = PRESETS[st && st.preset] || PRESETS[DEFAULT.preset];
    const acc = (st && st.acc) || base.acc;
    const bg = base.bg, dark = lum(bg) < 0.5;
    const [r, g, b] = hex(acc);
    const surf = mix(bg, dark ? "#ffffff" : "#000000", 0.05);
    const surf2 = mix(bg, dark ? "#ffffff" : "#000000", 0.10);
    return {
      bg, acc, surf, surf2, dark,
      line: mix(bg, dark ? "#ffffff" : "#000000", 0.16),
      fg: base.fg,
      dim: readableDim(base.fg, [surf, surf2]),
      accFg: bestOn(acc),
      accSoft: "rgba(" + r + "," + g + "," + b + ",0.13)",
      accRgb: r + "," + g + "," + b,
      // Предупреждение и отказ одни на все темы: янтарный и красный должны
      // читаться как «внимание» независимо от акцента, иначе в розовой теме
      // ошибка сольётся с кнопкой.
      warn: "#f2a03d",
      fail: dark ? "#ff6b7a" : "#c8323f",
    };
  }

  // vars собирает токены темы и плотности в строку CSS-переменных.
  function vars(t, d) {
    const dd = DENSITY[d] || DENSITY[DEFAULT.density];
    return Object.entries(t)
      .filter(([, v]) => typeof v === "string")
      .map(([k, v]) => "--" + k + ":" + v).join(";")
      + ";--r:" + dd.r + "px;--pad:" + dd.pad + "px;--gap:" + dd.gap + "px";
  }

  // load и save — вид в localStorage. Чужое или испорченное значение не
  // роняет страницу: берётся вид из коробки, а поле по полю проверяется,
  // чтобы одна битая плотность не сбросила ещё и цвет.
  function load() {
    const out = Object.assign({}, DEFAULT);
    try {
      const raw = localStorage.getItem(KEY);
      if (!raw) return out;
      const v = JSON.parse(raw);
      if (v && PRESETS[v.preset]) out.preset = v.preset;
      if (v && DENSITY[v.density]) out.density = v.density;
      if (v && typeof v.acc === "string" && /^#[0-9a-f]{6}$/i.test(v.acc)) out.acc = v.acc;
    } catch (_) { /* приватное окно или мусор — вид из коробки */ }
    return out;
  }
  function save(st) {
    try {
      localStorage.setItem(KEY, JSON.stringify({ preset: st.preset, acc: st.acc, density: st.density }));
    } catch (_) { /* приватное окно — вид просто не запомнится */ }
  }

  // Все акценты, какие есть в пресетах, без повторов: из них выбирают цвет.
  const ACCENTS = Object.values(PRESETS).map((p) => p.acc).filter((c, i, a) => a.indexOf(c) === i);

  return { PRESETS, DENSITY, LOOKS, DEFAULT, ACCENTS, KEY,
    hex, mix, lum, ratio, bestOn, readableDim, theme, vars, load, save };
})();
