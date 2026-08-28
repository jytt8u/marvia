import { readFileSync, writeFileSync } from "node:fs";
for (const [src, out] of [["Subscribers.dc.html", "content/subscribers.html"], ["Household.dc.html", "content/household.html"]]) {
  const html = readFileSync(src, "utf8");
  const from = html.indexOf('  <div style="flex-grow: 1;');
  const to = html.lastIndexOf("</div>\n</x-dc>");
  writeFileSync(out, html.slice(from, to).replace(/\s+$/, "") + "\n", "utf8");
  console.log("вынуто", out, html.slice(from, to).length, "символов");
}
