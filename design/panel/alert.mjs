// Красная точка у «Ноды», когда одна из них молчит: узнавать о поломке от
// покупателей — то, из-за чего продавцы теряют лицо.
import { readFileSync, writeFileSync } from "node:fs";
let s = readFileSync("build-shell.mjs", "utf8");

s = s.replace(
  `    const count = n.count
      ? \`\n        <span style="margin-left: auto; font-size: 12px; font-weight: 600; color: #6b7480;">\${n.count}</span>\`
      : "";`,
  `    const badge = n.alarm
      ? \`\n        <span style="margin-left: auto; display: flex; align-items: center; gap: 6px;"><span style="width: 7px; height: 7px; border-radius: 50%; background: #c0392b;"></span><span style="font-size: 12px; font-weight: 600; color: #c0392b;">\${n.count}</span></span>\`
      : n.count
        ? \`\n        <span style="margin-left: auto; font-size: 12px; font-weight: 600; color: #6b7480;">\${n.count}</span>\`
        : "";`);
s = s.replace("${n.label}${count}", "${n.label}${badge}");
s = s.replace(`{ id: "nodes", label: "Ноды", count: "4"`, `{ id: "nodes", label: "Ноды", count: "4", alarm: true`);

writeFileSync("build-shell.mjs", s, "utf8");
console.log("тревога в колонке включена");
