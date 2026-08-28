// «О нас» уезжает в левую колонку: там оно на всех экранах, а не только на одном.
import { readFileSync, writeFileSync } from "node:fs";
let s = readFileSync("content/stats.html", "utf8");
const start = s.indexOf("      <!--\n        О нас — внизу");
const marker = '<button style="font: inherit; font-size: 13.5px; cursor: pointer; border-radius: 8px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 7px 13px;">Поддержка</button>';
const end = s.indexOf(marker) + marker.length;
const tail = s.indexOf("</div>\n      </div>\n", end) + "</div>\n      </div>\n".length;
s = s.slice(0, start) + s.slice(tail);
writeFileSync("content/stats.html", s, "utf8");
console.log("блок «О нас» вырезан, осталось строк:", s.split("\n").length);
