// Первый запуск: та же колонка, но счётчиков ещё нет.
import { readFileSync, writeFileSync } from "node:fs";
let shell = readFileSync("Main.dc.html", "utf8");
const from = shell.indexOf('  <div style="flex-grow: 1;');
const to = shell.lastIndexOf("</div>\n</x-dc>");
const head = shell.slice(0, from);
const content = readFileSync("content/firstrun.html", "utf8");
let out = head + content + shell.slice(to);
// Счётчики убираем: нод и покупателей ещё нет.
out = out.replace(/\n\s*<span style="margin-left: auto; font-size: 12px; font-weight: 600; color: #6b7480;">\d+<\/span>/g, "");
out = out.replace("Копия базы снята вчера", "Копия базы ещё не снималась");
out = out.replace('background: #1a7f45;"></span>\n        Копия', 'background: #b9c2cd;"></span>\n        Копия');
writeFileSync("FirstRun.dc.html", out, "utf8");
console.log("собран FirstRun.dc.html");
