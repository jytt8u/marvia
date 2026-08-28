// Общая обвязка телефонных экранов: безопасные зоны и нижняя полоса.
//
// Сверху 47 точек и снизу 34 отданы системе: там часы, батарея и полоса
// жеста «домой». Рисовать их нельзя — они настоящие и лягут поверх, — но и
// занимать их своим содержимым тоже.
export const SAFE_TOP = 47;
export const SAFE_BOTTOM = 34;

const ICONS = {
  stats: '<path d="M4 19V5"/><path d="M4 19h16"/><path d="m7.5 14.5 3.5-4 3 2.5 4.5-6"/>',
  nodes: '<rect x="3" y="4" width="18" height="6" rx="1.6"/><rect x="3" y="14" width="18" height="6" rx="1.6"/><path d="M7 7h.01M7 17h.01"/>',
  users: '<path d="M16 20v-1.6a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4V20"/><circle cx="9.5" cy="7.5" r="3.5"/><path d="M17 11.2a3.5 3.5 0 0 0 0-6.9"/><path d="M21 20v-1.6a4 4 0 0 0-3-3.8"/>',
  more: '<circle cx="5" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="19" cy="12" r="1.6"/>',
};

// Красная метка на «Ноды» несёт число: голый кружок читается как «что-то
// новое», а не как «две ноды из четырёх лежат».
export function tabs(active) {
  const item = (id, label, alarm) => {
    const on = id === active;
    const color = on ? "#2f6fed" : "#6b7480";
    const mark = alarm
      ? `\n      <span style="position: absolute; top: 2px; left: 55%; min-width: 17px; height: 17px; border-radius: 999px; background: #c0392b; color: #ffffff; font-size: 11px; font-weight: 700; display: flex; align-items: center; justify-content: center; padding: 0 4px; box-sizing: border-box;">2</span>`
      : "";
    return `    <div style="flex-grow: 1; display: flex; flex-direction: column; align-items: center; gap: 4px; padding: 8px 0; color: ${color}; position: relative; min-height: 52px; box-sizing: border-box;">
      <svg width="23" height="23" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${ICONS[id]}</svg>${mark}
      <span style="font-size: 12px;${on ? " font-weight: 600;" : ""}">${label}</span>
    </div>`;
  };

  return `  <div style="display: flex; align-items: stretch; background: #ffffff; border-top: 1px solid #dfe3e8; padding: 6px 6px ${SAFE_BOTTOM}px; flex-shrink: 0;">
${item("stats", "Статистика")}
${item("nodes", "Ноды", true)}
${item("users", "Люди")}
${item("more", "Ещё")}
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
    a { color: #2f6fed; } a:hover { color: #1d55c9; }
  </style>
</helmet>
`;

export const TAIL = `</x-dc>
</body>
</html>
`;

// Шапка экрана. Отступ сверху — безопасная зона, туда ничего не кладём.
export function header(inner) {
  return `  <div style="padding: ${SAFE_TOP}px 16px 12px; background: #ffffff; border-bottom: 1px solid #dfe3e8; flex-shrink: 0;">
    <div style="display: flex; align-items: center; gap: 10px; min-height: 44px;">
${inner}
    </div>
  </div>`;
}

export const MARK = `<div style="width: 28px; height: 28px; border-radius: 8px; background: #2f6fed; display: flex; align-items: center; justify-content: center; flex-shrink: 0;">
        <svg width="17" height="17" viewBox="0 0 32 32" fill="none" stroke="#ffffff" stroke-width="3.9" stroke-linecap="round" stroke-linejoin="round"><path d="M6 25 10 7l5.1 10.2"/><path d="M16.9 17.2 22 7l4 18"/></svg>
      </div>`;
