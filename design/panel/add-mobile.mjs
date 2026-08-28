import { readFileSync, writeFileSync } from "node:fs";
const c = JSON.parse(readFileSync("canvas.json", "utf8"));

c.pages.splice(2, 0, { id: "page-mobile", name: "Телефон" });

c.artboards.push(
  { file: "MobileStats.dc.html", x: 0, y: 0, w: 390, h: 844, title: "Сводка", page: "page-mobile" },
  { file: "MobileNodes.dc.html", x: 490, y: 0, w: 390, h: 844, title: "Ноды — что-то горит", page: "page-mobile" },
  { file: "MobileUser.dc.html", x: 980, y: 0, w: 390, h: 844, title: "Человек — продлить и ссылки", page: "page-mobile" },
);

c.annotations.push({
  id: "why-mobile",
  x: 0,
  y: -300,
  w: 470,
  page: "page-mobile",
  text: "Телефон — не «панель поменьше».\n\nС телефона в панель заходят по двум поводам: что-то сломалось, а человек не за компьютером, или кто-то написал «не работает, продли». Поэтому наверху каждого экрана стоит то, что горит, а действия прибиты к низу — до верха большим пальцем не дотянуться.\n\nЧего здесь нет намеренно: добавление ноды, протоколы, ключи для ботов, копия базы. Это делают за столом, и попытка втиснуть их сюда только удлинит список.",
});

writeFileSync("canvas.json", JSON.stringify(c, null, 2) + "\n", "utf8");
console.log("страница «Телефон» добавлена, артбордов всего:", c.artboards.length);
