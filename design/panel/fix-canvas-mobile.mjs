import { readFileSync, writeFileSync } from "node:fs";
const c = JSON.parse(readFileSync("canvas.json", "utf8"));

const has = (f) => c.artboards.some((a) => a.file === f);
if (!has("MobileUsers.dc.html")) {
  const x = Math.max(...c.artboards.filter((a) => a.page === "page-mobile").map((a) => a.x)) + 490;
  c.artboards.push({ file: "MobileUsers.dc.html", x, y: 0, w: 390, h: 844, title: "Люди — поиск снизу", page: "page-mobile" });
}
for (const a of c.artboards) {
  if (a.file === "MobileStats.dc.html") a.title = "Статистика";
  if (a.file === "MobileUser.dc.html") a.title = "Человек — продлить и ссылки";
}

const note = c.annotations.find((a) => a.id === "why-mobile");
note.text = "Телефон — не «панель поменьше».\n\nС телефона заходят по двум поводам: что-то сломалось, а человек не за компьютером, или пришло «не работает, продли». Поэтому наверху стоит то, что горит, а действия прибиты к низу: до верха большим пальцем не дотянуться.\n\nСверху 47 точек и снизу 34 отданы системе — там часы и полоса жеста «домой». Рисовать их нельзя, занимать своим содержимым тоже.\n\nОпасное сюда не переехало: «Отключить» и «Удалить» живут в «⋯», как и на компьютере. Промахнуться одной рукой ночью дороже, чем мышью за столом.";

c.annotations.push({
  id: "why-no-chart",
  x: 490,
  y: -300,
  w: 460,
  page: "page-mobile",
  text: "Кривая трафика на телефон не переехала.\n\nНаведения тут не бывает, а без него с графика не снять ни одного значения — на компьютере подсказку рисовали специально. Осталось то, ради чего на неё смотрели: сколько за сутки, сколько за месяц и куда растёт.\n\nНоды называются одинаково на всех экранах: «Германия» на сводке и «Германия · Франкфурт» на списке — это два разных места для того, кто сверяет со словами покупателя.",
});

writeFileSync("canvas.json", JSON.stringify(c, null, 2) + "\n", "utf8");
console.log("артбордов:", c.artboards.length);
