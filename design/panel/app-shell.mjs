// Обвязка экранов приложения покупателя.
//
// Приложение тёмное, а не светлое, как панель: им пользуются вечером, в
// метро и в постели, и это единственный экран продукта, который человек
// открывает каждый день. Панель и приложение — разные вещи для разных людей,
// и попытка сделать их одинаковыми испортила бы обе.
export const SAFE_TOP = 47;
export const SAFE_BOTTOM = 34;

export const BG = "#0f1218";
export const CARD = "#191d24";
export const LINE = "#272d36";
export const TEXT = "#f2f5f9";
export const MUTED = "#8b94a3";
export const ACCENT = "#2f6fed";
export const OK = "#4ec27f";
export const WARN = "#e0a33a";

const ICONS = {
  power: '<path d="M12 3.2v9.2"/><path d="M6.6 6.6a8 8 0 1 0 10.8 0"/>',
  globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17"/><path d="M12 3.5c2.2 2.4 3.3 5.3 3.3 8.5S14.2 18.1 12 20.5c-2.2-2.4-3.3-5.3-3.3-8.5S9.8 5.9 12 3.5z"/>',
  gear: '<circle cx="12" cy="12" r="3.2"/><path d="M12 3v2.2M12 18.8V21M21 12h-2.2M5.2 12H3M18.4 5.6l-1.6 1.6M7.2 16.8l-1.6 1.6M18.4 18.4l-1.6-1.6M7.2 7.2 5.6 5.6"/>',
};

export function tabs(active) {
  const item = (id, label) => {
    const on = id === active;
    return `    <div style="flex-grow: 1; display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 8px 0; color: ${on ? TEXT : MUTED}; min-height: 52px; box-sizing: border-box;">
      <svg width="23" height="23" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">${ICONS[id]}</svg>
      <span style="font-size: 12px;${on ? " font-weight: 600;" : ""}">${label}</span>
    </div>`;
  };

  return `  <div style="display: flex; align-items: stretch; background: ${CARD}; border-top: 1px solid ${LINE}; padding: 6px 6px ${SAFE_BOTTOM}px; flex-shrink: 0;">
${item("power", "Подключение")}
${item("globe", "Страны")}
${item("gear", "Настройки")}
  </div>`;
}

export const HEAD = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <script src="./support.js"></script>
</head>
<body>
<x-dc>
<helmet>
  <style>
    body { margin: 0; font-family: -apple-system, "Segoe UI", Roboto, Ubuntu, sans-serif; font-size: 15px; line-height: 1.5; }
    a { color: #6ea0ff; } a:hover { color: #9dbcff; }
  </style>
</helmet>
`;

export const TAIL = `</x-dc>
</body>
</html>
`;

export const frame = (inner) =>
  `<div style="display: flex; flex-direction: column; width: 390px; height: 844px; background: ${BG}; color: ${TEXT}; overflow: hidden; position: relative;">\n${inner}\n</div>\n`;

// Шапка приложения: имя продавца, а не наше. Покупатель платил ему.
export const header = (right) => `  <div style="padding: ${SAFE_TOP}px 18px 10px; flex-shrink: 0; display: flex; align-items: center; gap: 10px; min-height: 44px;">
    <span style="font-size: 12px; letter-spacing: .2em; color: ${MUTED};">БЫСТРЫЙ ИНТЕРНЕТ</span>
${right ? `    ${right}` : ""}
  </div>`;
