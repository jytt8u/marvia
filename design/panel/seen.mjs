import { readFileSync, writeFileSync } from "node:fs";
let s = readFileSync("content/subscribers.html", "utf8");

// Колонка протоколов заменяется на «был на связи»: тег vless в списке не
// нужен никому, а «подключался ли он вообще» — первый вопрос при жалобе.
const values = [
  '<div style="font-size: 13px;">4 минуты назад</div>',
  '<div style="font-size: 13px;">вчера в 21:40</div>',
  '<div style="font-size: 13px; color: #6b7480;">14 августа</div>',
  '<div style="font-size: 13px;">сейчас · 2 устройства</div>',
];

let i = 0;
s = s.replace(/ {10}<div style="display: flex; gap: 5px; flex-wrap: wrap;">[\s\S]*?\n {10}<\/div>/g, () => {
  const v = values[i] ?? values[0];
  i += 1;
  return "          " + v;
});

writeFileSync("content/subscribers.html", s, "utf8");
console.log("заменено блоков:", i);
