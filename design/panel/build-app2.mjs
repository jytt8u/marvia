import { writeFileSync } from "node:fs";
import { HEAD, TAIL, frame, tabs, header, SAFE_TOP, SAFE_BOTTOM, CARD, LINE, TEXT, MUTED, ACCENT, OK } from "./app-shell.mjs";

const card = `background: ${CARD}; border: 1px solid ${LINE}; border-radius: 14px;`;
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";

const toggle = (on) => `<span style="width: 44px; height: 26px; border-radius: 999px; background: ${on ? ACCENT : "#39414d"}; flex-shrink: 0; display: flex; align-items: center; ${on ? "justify-content: flex-end;" : ""} padding: 0 3px; box-sizing: border-box;"><span style="width: 20px; height: 20px; border-radius: 50%; background: #ffffff;"></span></span>`;

const row = (title, sub, right) => `
      <div style="padding: 13px 15px; display: flex; align-items: center; gap: 12px; min-height: 58px; box-sizing: border-box; border-bottom: 1px solid ${LINE};">
        <div style="display: flex; flex-direction: column; gap: 1px; min-width: 0;">
          <span style="font-size: 15px; ${cut}">${title}</span>
          ${sub ? `<span style="font-size: 12.5px; color: ${MUTED};">${sub}</span>` : ""}
        </div>
        <div style="margin-left: auto; flex-shrink: 0; display: flex; align-items: center; gap: 9px;">${right}</div>
      </div>`;

const chev = `<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="#4d5766" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9.5 6 6 6-6 6"/></svg>`;

/* --------------------------------------------------------- настройки */

const settings = frame(`${header("")}

  <div style="padding: 0 18px 8px; flex-shrink: 0;">
    <div style="font-size: 26px; font-weight: 700; letter-spacing: -.02em;">Настройки</div>
  </div>

  <div style="flex-grow: 1; overflow: hidden; padding: 12px 18px; display: flex; flex-direction: column; gap: 14px;">

    <!--
      Первым — то, что чинит «интернет тормозит»: не всё нужно вести через
      туннель. Дальше вид, и только потом ключ доступа, который трогают раз.
    -->
    <div style="${card} overflow: hidden;">
${row("Включаться при запуске телефона", "", toggle(true))}
${row("Российские сайты — мимо туннеля", "банки и госуслуги иначе ругаются", toggle(true))}
${row("Показывать в шторке", "", toggle(false))}
    </div>

    <div style="${card} overflow: hidden;">
${row("Цвет", "", `<span style="display: flex; gap: 7px;"><span style="width: 22px; height: 22px; border-radius: 7px; background: ${ACCENT}; box-shadow: 0 0 0 2px ${CARD}, 0 0 0 3.6px ${ACCENT};"></span><span style="width: 22px; height: 22px; border-radius: 7px; background: ${OK};"></span><span style="width: 22px; height: 22px; border-radius: 7px; background: #9333ea;"></span><span style="width: 22px; height: 22px; border-radius: 7px; background: #e0a33a;"></span></span>`)}
${row("Тема", "", `<span style="font-size: 14px; color: ${MUTED};">Тёмная</span>${chev}`)}
${row("Язык", "", `<span style="font-size: 14px; color: ${MUTED};">Русский</span>${chev}`)}
    </div>

    <div style="${card} overflow: hidden;">
${row("Ключ доступа", "выдан 28 августа", chev)}
${row("Написать продавцу", "@bystry_support", chev)}
${row("О приложении", "", chev)}
    </div>
  </div>

${tabs("gear")}`);

writeFileSync("AppSettings.dc.html", HEAD + settings + TAIL, "utf8");

/* ------------------------------------------------------- первый запуск */

// Первый экран после установки. Покупатель только что заплатил и получил в
// чате ссылку: нажатие на неё настраивает всё само, а поле и код — запасные
// пути для тех, у кого ссылка потерялась в переписке.
const onboarding = frame(`  <div style="padding: ${SAFE_TOP + 40}px 22px 0; display: flex; flex-direction: column; align-items: center; flex-grow: 1;">

    <div style="width: 62px; height: 62px; border-radius: 17px; background: ${ACCENT}; display: flex; align-items: center; justify-content: center;">
      <svg width="36" height="36" viewBox="0 0 32 32" fill="none" stroke="#ffffff" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round"><path d="M6 25 10 7l5.1 10.2"/><path d="M16.9 17.2 22 7l4 18"/></svg>
    </div>

    <div style="margin-top: 22px; font-size: 25px; font-weight: 700; letter-spacing: -.02em; text-align: center;">Вставь ключ доступа</div>
    <div style="margin-top: 8px; font-size: 14.5px; color: ${MUTED}; text-align: center; line-height: 1.5;">
      Продавец прислал его в чате — ссылку вида <span style="color: ${TEXT};">marvia://…</span>
      Нажми её прямо в переписке, и приложение настроится само.
    </div>

    <div style="margin-top: 26px; width: 100%; ${card} padding: 15px 16px; display: flex; align-items: center; gap: 12px; min-height: 56px; box-sizing: border-box;">
      <span style="font-size: 15px; color: ${MUTED};">marvia://…</span>
      <span style="margin-left: auto; font-size: 14px; color: #6ea0ff; flex-shrink: 0;">Вставить</span>
    </div>

    <div style="margin-top: 12px; width: 100%; display: flex; gap: 10px;">
      <button style="flex-grow: 1; font: inherit; font-size: 15px; cursor: pointer; border-radius: 12px; border: 1px solid ${LINE}; background: ${CARD}; color: ${TEXT}; padding: 14px 12px; min-height: 50px; display: flex; align-items: center; justify-content: center; gap: 9px;">
        <svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="7" height="7" rx="1.4"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.4"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.4"/><path d="M13.5 13.5h3v3M20.5 20.5h-3v-3"/></svg>
        Сканировать код
      </button>
    </div>

    <div style="margin-top: auto; margin-bottom: ${SAFE_BOTTOM + 14}px; width: 100%; display: flex; flex-direction: column; gap: 12px;">
      <button style="font: inherit; font-size: 16.5px; cursor: pointer; border-radius: 13px; border: 1px solid ${ACCENT}; background: ${ACCENT}; color: #ffffff; padding: 16px; font-weight: 600; width: 100%; min-height: 54px;">Подключиться</button>
      <span style="font-size: 12.5px; color: ${MUTED}; text-align: center; line-height: 1.5;">
        Ключ хранится только на этом телефоне. Мы его не видим и восстановить не сможем — если потеряешь, попроси у продавца новый.
      </span>
    </div>
  </div>`);

writeFileSync("AppStart.dc.html", HEAD + onboarding + TAIL, "utf8");
console.log("собраны AppSettings.dc.html и AppStart.dc.html");
