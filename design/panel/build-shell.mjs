// Сборка экранов панели из общей колонки и содержимого.
//
// Колонка одинаковая на всех экранах, и рассинхрон в ней — первое, что
// заметит глаз на холсте. Поэтому она живёт в одном месте, а не копией в
// каждом файле.
import { readFileSync, writeFileSync } from "node:fs";

const NAV = [
  { id: "stats", label: "Статистика", icon: '<path d="M4 19V5"/><path d="M4 19h16"/><path d="m7.5 14.5 3.5-4 3 2.5 4.5-6"/>' },
  { id: "nodes", label: "Ноды", count: "2 из 4", alarm: true, icon: '<rect x="3" y="4" width="18" height="6" rx="1.6"/><rect x="3" y="14" width="18" height="6" rx="1.6"/><path d="M7 7h.01M7 17h.01"/>' },
  { id: "users", label: "Люди", count: "147", icon: '<path d="M16 20v-1.6a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4V20"/><circle cx="9.5" cy="7.5" r="3.5"/><path d="M17 11.2a3.5 3.5 0 0 0 0-6.9"/><path d="M21 20v-1.6a4 4 0 0 0-3-3.8"/>' },
  { id: "look", label: "Кастомизация", icon: '<circle cx="12" cy="12" r="3.2"/><path d="M12 3v2.2M12 18.8V21M21 12h-2.2M5.2 12H3M18.4 5.6l-1.6 1.6M7.2 16.8l-1.6 1.6M18.4 18.4l-1.6-1.6M7.2 7.2 5.6 5.6"/>' },
  { id: "household", label: "Обслуживание", icon: '<circle cx="8" cy="14" r="4"/><path d="M11 11.5 20 3l1.5 1.5-1.8 1.8 1.4 1.4-2 2-1.4-1.4-2.6 2.6"/>' },
];

function sidebar(active) {
  const items = NAV.map((n) => {
    const on = n.id === active;
    const row = on
      ? 'padding: 9px 10px; border-radius: 8px; background: #eef3ff; color: #2f6fed; font-weight: 600;'
      : 'padding: 9px 10px; border-radius: 8px; color: #414954;';
    // Тревога в колонке: пока одна нода молчит, это видно с любого экрана,
    // а не только с того, куда ещё надо догадаться зайти.
    const badge = n.alarm
      ? `\n        <span style="margin-left: auto; display: flex; align-items: center; gap: 6px;"><span style="width: 7px; height: 7px; border-radius: 50%; background: #c0392b;"></span><span style="font-size: 12px; font-weight: 600; color: #c0392b;">${n.count}</span></span>`
      : n.count
        ? `\n        <span style="margin-left: auto; font-size: 12px; font-weight: 600; color: #6b7480;">${n.count}</span>`
        : "";
    return `      <div style="display: flex; align-items: center; gap: 10px; ${row}">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${n.icon}</svg>
        ${n.label}${badge}
      </div>`;
  }).join("\n");

  return `  <div style="display: flex; flex-direction: column; width: 232px; flex-shrink: 0; background: #ffffff; border-right: 1px solid #dfe3e8;">
    <div style="display: flex; align-items: center; gap: 10px; padding: 20px 18px 18px;">
      <div style="width: 26px; height: 26px; border-radius: 7px; background: #2f6fed; display: flex; align-items: center; justify-content: center;">
        <svg width="16" height="16" viewBox="0 0 32 32" fill="none" stroke="#ffffff" stroke-width="3.9" stroke-linecap="round" stroke-linejoin="round"><path d="M6 25 10 7l5.1 10.2"/><path d="M16.9 17.2 22 7l4 18"/></svg>
      </div>
      <div style="font-weight: 700; font-size: 16px; letter-spacing: .01em;">Marvia</div>
    </div>

    <div style="display: flex; flex-direction: column; gap: 2px; padding: 0 10px;">
${items}
    </div>

    <div style="margin-top: auto; display: flex; flex-direction: column;">

      <div style="padding: 13px 18px; border-top: 1px solid #dfe3e8; display: flex; align-items: center; gap: 7px; font-size: 12.5px; color: #6b7480;">
        <span style="width: 7px; height: 7px; border-radius: 50%; background: #1a7f45; flex-shrink: 0;"></span>
        Копия базы снята вчера
      </div>

      <!--
        О нас живёт в колонке, а не подвалом одного экрана: «что это вообще
        такое и кому уходят мои данные» спрашивают из любого места, а читают
        ответ один раз.
      -->
      <div style="padding: 13px 18px 15px; border-top: 1px solid #dfe3e8; display: flex; flex-direction: column; gap: 9px;">
        <div style="font-size: 12.5px; color: #6b7480; line-height: 1.55;">
          Всё работает на твоём сервере. Мы не видим ни людей, ни их трафика — и не можем их у тебя отобрать.
        </div>
        <div style="display: flex; align-items: center; gap: 9px; font-size: 12.5px;">
          <span style="color: #2f6fed;">Как это работает</span>
          <span style="color: #dfe3e8;">·</span>
          <span style="color: #2f6fed;">Поддержка</span>
        </div>
        <div style="display: flex; align-items: center; justify-content: space-between; font-size: 12.5px; color: #6b7480;">
          <span>Marvia v0.5.0</span>
          <span style="color: #2f6fed;">Выйти</span>
        </div>
      </div>
    </div>
  </div>`;
}

const HEAD = `<!doctype html>
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

<div style="display: flex; width: 1440px; height: 900px; background: #f6f7f9; color: #1b1f24;">
`;

const TAIL = `
</div>
</x-dc>
</body>
</html>
`;

const [, , active, contentPath, outPath] = process.argv;
const content = readFileSync(contentPath, "utf8");
writeFileSync(outPath, HEAD + sidebar(active) + "\n\n" + content + TAIL, "utf8");
console.log("собран", outPath, "— активный раздел", active);
