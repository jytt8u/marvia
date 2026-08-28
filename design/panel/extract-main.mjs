import { readFileSync, writeFileSync } from "node:fs";
const html = readFileSync("Main.dc.html", "utf8");
const from = html.indexOf('  <div style="flex-grow: 1;');
const to = html.lastIndexOf("</div>\n</x-dc>");
writeFileSync("content/stats.html", html.slice(from, to).replace(/\s+$/, "") + "\n", "utf8");
console.log("вынуто content/stats.html");
