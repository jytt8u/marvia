import { readFileSync, writeFileSync } from "node:fs";
const c = JSON.parse(readFileSync("canvas.json", "utf8"));

// Страница «Телефон» была про управление с телефона — это оказалось не тем.
// Телефон у покупателя нужен, чтобы подключаться, а не управлять.
c.pages = c.pages.map((p) => (p.id === "page-mobile" ? { id: "page-mobile", name: "Приложение" } : p));
c.artboards = c.artboards.filter((a) => !a.file.startsWith("Mobile"));
c.annotations = c.annotations.filter((a) => a.id !== "why-mobile" && a.id !== "why-no-chart");

c.artboards.push(
  { file: "AppStart.dc.html", x: 0, y: 0, w: 390, h: 844, title: "Первый запуск", page: "page-mobile" },
  { file: "AppConnect.dc.html", x: 490, y: 0, w: 390, h: 844, title: "Подключение", page: "page-mobile" },
  { file: "AppServers.dc.html", x: 980, y: 0, w: 390, h: 844, title: "Страны", page: "page-mobile" },
  { file: "AppSettings.dc.html", x: 1470, y: 0, w: 390, h: 844, title: "Настройки", page: "page-mobile" },
);

c.annotations.push({
  id: "why-app",
  x: 0,
  y: -330,
  w: 500,
  page: "page-mobile",
  text: "Приложение покупателя, а не панель на телефоне.\n\nОно тёмное, а панель светлая, и это намеренно: приложение открывают вечером и в метро, а панель — за столом. Это разные вещи для разных людей.\n\nВесь экран — одна кнопка. Всё, что человек делает каждый день, — нажимает её; остальное он трогает раз в месяц или никогда. Страна названа словами и с флагом, а не именем сервера: «ae-1» покупателю не говорит ничего.\n\nСрок и остаток трафика стоят на главном экране, потому что каждый такой вопрос иначе прилетает продавцу в чат.\n\nСверху 47 точек и снизу 34 отданы системе: там часы и полоса жеста «домой».",
});

writeFileSync("canvas.json", JSON.stringify(c, null, 2) + "\n", "utf8");
console.log("страница «Приложение»:", c.artboards.filter((a) => a.page === "page-mobile").map((a) => a.file).join(", "));
