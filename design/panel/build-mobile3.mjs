import { writeFileSync, readFileSync } from "node:fs";
import { HEAD, TAIL, tabs, header, MARK } from "./mobile-shell.mjs";

const frame = (inner) =>
  `<div style="display: flex; flex-direction: column; width: 390px; height: 844px; background: #f6f7f9; color: #1b1f24; overflow: hidden;">\n${inner}\n</div>\n`;
const card = "background: #ffffff; border: 1px solid #dfe3e8; border-radius: 12px;";
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";
const chevron = '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#b9c2cd" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-left: auto; flex-shrink: 0;"><path d="m9.5 6 6 6-6 6"/></svg>';

const round = (svg) =>
  `<div style="width: 44px; height: 44px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; margin: 0 -10px;">${svg}</div>`;
const back = round('<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#414954" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m14.5 6-6 6 6 6"/></svg>');
const dots = round('<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#414954" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="5" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="12" cy="19" r="1.7"/></svg>');

const qr = readFileSync(
  "C:/Users/Arseniy/AppData/Local/Temp/claude/D--AWIFI/32e44cec-9ffe-4d2d-b458-c06ec4f44b54/scratchpad/qr.svg",
  "utf8",
).trim();

// Строка «поле — значение»: на телефоне это читается лучше таблицы.
const line = (name, value, color) => `
      <div style="display: flex; align-items: baseline; gap: 12px;">
        <span style="font-size: 13px; color: #6b7480; flex-shrink: 0;">${name}</span>
        <span style="margin-left: auto; font-size: 14.5px; text-align: right; ${color ? `color: ${color};` : ""} ${cut}">${value}</span>
      </div>`;

/* ------------------------------------------------------------ карточка ноды */

const nodeHead = `      ${back}
      <div style="display: flex; flex-direction: column; min-width: 0;">
        <span style="font-weight: 700; font-size: 17px; ${cut}">Германия · Франкфурт</span>
        <span style="font-size: 13px; color: #6b7480; ${cut}">de-1 · 88.210.11.4:443</span>
      </div>
      <div style="margin-left: auto;">${dots}</div>`;

const node = frame(`${header(nodeHead)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 11px;">

    <div style="background: #ffffff; border: 1px solid #c0392b; border-radius: 12px; padding: 14px 15px; display: flex; align-items: flex-start; gap: 11px;">
      <span style="width: 9px; height: 9px; border-radius: 50%; background: #c0392b; flex-shrink: 0; margin-top: 6px;"></span>
      <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0;">
        <span style="font-size: 16px; font-weight: 700;">Молчит три часа</span>
        <span style="font-size: 13px; color: #6b7480;">Последний раз выходила на связь в 17:42. Сервер мог упасть, а мог попасть под блокировку — снаружи это не отличить.</span>
      </div>
    </div>

    <!--
      «Кого держит» — строка, ради которой карточку и открывают: она отвечает
      на «кому сейчас плохо» и ведёт прямо в отфильтрованный список.
    -->
    <div style="${card} padding: 13px 15px; display: flex; align-items: center; gap: 11px; min-height: 56px; box-sizing: border-box;">
      <div style="display: flex; flex-direction: column; gap: 1px; min-width: 0;">
        <span style="font-weight: 600;">Держит 41 человека</span>
        <span style="font-size: 13px; color: #6b7480;">пока она в подписках, они ходят через неё</span>
      </div>
      ${chevron}
    </div>

    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 12px;">
${line("Трафик за сутки", "68 ГБ")}
${line("За неделю", "412 ГБ")}
${line("Маскировка", "REALITY · www.bing.com")}
${line("Протоколы", "vp1")}
    </div>

    <div style="font-size: 12.5px; color: #6b7480; padding: 0 2px;">
      Протоколы и маскировку меняют с компьютера — на ходу такое решение не принимают.
    </div>
  </div>

  <!--
    Два действия, и они разной цены. «Выключить» — обратимо, поэтому оно
    открытое и с обещанием возврата. «Удалить» на телефоне нет вовсе: оно
    необратимо, а ошибиться одной рукой ночью проще всего.
  -->
  <div style="padding: 12px 16px; background: #ffffff; border-top: 1px solid #dfe3e8; display: flex; flex-direction: column; gap: 7px; flex-shrink: 0;">
    <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #c0392b; background: #ffffff; color: #c0392b; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Выключить ноду</button>
    <span style="font-size: 12.5px; color: #6b7480; text-align: center;">41 человек перейдёт на другие ноды. Вернуть можно одним нажатием, статистика останется.</span>
  </div>

${tabs("nodes")}`);

writeFileSync("MobileNode.dc.html", HEAD + node + TAIL, "utf8");

/* -------------------------------------------------------------- ссылки */

const linksHead = `      ${back}
      <div style="display: flex; flex-direction: column; min-width: 0;">
        <span style="font-weight: 700; font-size: 17px; ${cut}">Доступ — @mikhail_p</span>
        <span style="font-size: 13px; color: #6b7480; ${cut}">до 27 сентября · 3 устройства</span>
      </div>`;

const links = frame(`${header(linksHead)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 11px;">

    <div style="${card} padding: 15px; display: flex; flex-direction: column; align-items: center; gap: 11px;">
      <div style="padding: 6px; border: 1px solid #dfe3e8; border-radius: 9px;">${qr.replace('width="96" height="96"', 'width="150" height="150"')}</div>
      <span style="font-size: 13px; color: #6b7480; text-align: center;">Подписка. Дать отсканировать через стол — самый короткий путь на чужой телефон.</span>
      <button style="font: inherit; font-size: 15px; cursor: pointer; border-radius: 10px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 12px 16px; width: 100%; min-height: 46px;">Во весь экран</button>
    </div>

    <!--
      Здесь ломается ожидание «пришлите заново», и сказать об этом надо до
      того, как человек начнёт искать кнопку, которой нет.
    -->
    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 11px;">
      <div style="display: flex; gap: 10px;">
        <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="#6b7480" stroke-width="2" stroke-linecap="round" style="flex-shrink: 0; margin-top: 2px;"><rect x="4" y="10.5" width="16" height="10" rx="2.2"/><path d="M8 10.5V7.6a4 4 0 0 1 8 0v2.9"/></svg>
        <div style="display: flex; flex-direction: column; gap: 3px;">
          <span style="font-size: 14px; font-weight: 600;">Ключ приложения переслать нельзя</span>
          <span style="font-size: 13px; color: #6b7480;">Панель хранит только публичную половину — приватную показали один раз, при выдаче. Это же и защищает всех при утечке базы.</span>
        </div>
      </div>
      <button style="font: inherit; font-size: 15px; cursor: pointer; border-radius: 10px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 12px 14px; width: 100%; min-height: 46px;">Выдать новый ключ</button>
    </div>
  </div>

  <div style="padding: 12px 16px; background: #ffffff; border-top: 1px solid #dfe3e8; display: flex; flex-direction: column; gap: 9px; flex-shrink: 0;">
    <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #2f6fed; background: #2f6fed; color: #ffffff; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Скопировать для чата</button>
    <span style="font-size: 12.5px; color: #6b7480; text-align: center;">Ссылка подписки и ссылки на приложения одним сообщением.</span>
  </div>

${tabs("users")}`);

writeFileSync("MobileLinks.dc.html", HEAD + links + TAIL, "utf8");
console.log("собраны MobileNode.dc.html и MobileLinks.dc.html");
