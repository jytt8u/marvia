// Врезаем QR рядом с главной ссылкой в оба окна выдачи.
import { readFileSync, writeFileSync } from "node:fs";
const qr = readFileSync("C:/Users/Arseniy/AppData/Local/Temp/claude/D--AWIFI/32e44cec-9ffe-4d2d-b458-c06ec4f44b54/scratchpad/qr.svg", "utf8").trim();

const block = (title, hint) => `
      <div style="display: flex; gap: 14px; padding: 14px; background: #ffffff; border: 1px solid #dfe3e8; border-radius: 9px;">
        <div style="flex-shrink: 0; padding: 5px; border: 1px solid #dfe3e8; border-radius: 7px;">${qr}</div>
        <div style="display: flex; flex-direction: column; gap: 6px; min-width: 0;">
          <span style="font-size: 13.5px; font-weight: 600;">${title}</span>
          <span style="font-size: 12.5px; color: #6b7480;">${hint}</span>
          <button style="align-self: flex-start; margin-top: 2px; font: inherit; font-size: 13px; cursor: pointer; border-radius: 7px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 5px 11px;">Показать во весь экран</button>
        </div>
      </div>
`;

let links = readFileSync("LinksModal.dc.html", "utf8");
links = links.replace(
  '      <div style="display: flex; flex-direction: column; gap: 5px;">\n        <div style="display: flex; align-items: baseline; gap: 8px;">\n          <span style="font-size: 13px; font-weight: 600;">Подписка</span>',
  block("Наведи телефоном покупателя", "Приложение поставится и настроится само — переносить ссылку с компьютера на телефон руками не придётся.") +
  '\n      <div style="display: flex; flex-direction: column; gap: 5px;">\n        <div style="display: flex; align-items: baseline; gap: 8px;">\n          <span style="font-size: 13px; font-weight: 600;">Подписка</span>');
writeFileSync("LinksModal.dc.html", links, "utf8");

let user = readFileSync("UserLinks.dc.html", "utf8");
user = user.replace(
  '      <div style="display: flex; flex-direction: column; gap: 5px;">\n        <div style="display: flex; align-items: baseline; gap: 8px;">\n          <span style="font-size: 13px; font-weight: 600;">Для чужих клиентов</span>',
  block("Подписка кодом", "Дать отсканировать через стол проще, чем диктовать адрес. Ключ приложения так передать нельзя — его больше нет.") +
  '\n      <div style="display: flex; flex-direction: column; gap: 5px;">\n        <div style="display: flex; align-items: baseline; gap: 8px;">\n          <span style="font-size: 13px; font-weight: 600;">Для других приложений</span>');
writeFileSync("UserLinks.dc.html", user, "utf8");

console.log("QR врезан в оба окна");
