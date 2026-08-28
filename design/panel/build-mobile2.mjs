import { writeFileSync, readFileSync } from "node:fs";
import { HEAD, TAIL, tabs, header, MARK } from "./mobile-shell.mjs";

const frame = (inner) =>
  `<div style="display: flex; flex-direction: column; width: 390px; height: 844px; background: #f6f7f9; color: #1b1f24; overflow: hidden;">\n${inner}\n</div>\n`;
const card = "background: #ffffff; border: 1px solid #dfe3e8; border-radius: 12px;";
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";

// Иконка в кружке 44×44: попасть в 22 пикселя большим пальцем нельзя.
const round = (svg) =>
  `<div style="width: 44px; height: 44px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; margin: 0 -10px;">${svg}</div>`;

const back = round('<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#414954" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m14.5 6-6 6 6 6"/></svg>');
const dots = round('<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#414954" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="5" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="12" cy="19" r="1.7"/></svg>');

const qr = readFileSync(
  "C:/Users/Arseniy/AppData/Local/Temp/claude/D--AWIFI/32e44cec-9ffe-4d2d-b458-c06ec4f44b54/scratchpad/qr.svg",
  "utf8",
).trim().replace('width="96" height="96"', 'width="118" height="118"');

/* --------------------------------------------------------- карточка человека */

const userHeader = `      ${back}
      <div style="display: flex; flex-direction: column; min-width: 0;">
        <span style="font-weight: 700; font-size: 17px; ${cut}">@mikhail_p</span>
        <span style="font-size: 13px; color: #6b7480; ${cut}">#147 · tg:584930221</span>
      </div>
      <div style="margin-left: auto;">${dots}</div>`;

const user = frame(`${header(userHeader)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 11px;">

    <!-- Первое, что спрашивают при «не работает»: он вообще заходил? -->
    <div style="${card} padding: 14px 15px; display: flex; align-items: center; gap: 11px;">
      <span style="width: 9px; height: 9px; border-radius: 50%; background: #1a7f45; flex-shrink: 0;"></span>
      <div style="display: flex; flex-direction: column; gap: 1px; min-width: 0;">
        <span style="font-weight: 600;">Был на связи 4 минуты назад</span>
        <span style="font-size: 13px; color: #6b7480; ${cut}">через ОАЭ · Дубай, с двух устройств</span>
      </div>
    </div>

    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 13px;">
      <div style="display: flex; flex-direction: column; gap: 6px;">
        <div style="display: flex; align-items: baseline;">
          <span style="font-size: 13px; color: #6b7480;">Трафик</span>
          <span style="margin-left: auto; font-size: 15px;">37 из 100 ГБ</span>
        </div>
        <div style="height: 7px; background: #f0f2f5; border-radius: 999px; overflow: hidden;"><i style="display: block; height: 100%; width: 37%; background: #2f6fed;"></i></div>
      </div>
      <div style="display: flex; align-items: baseline;">
        <span style="font-size: 13px; color: #6b7480;">Доступ до</span>
        <span style="margin-left: auto; font-size: 15px;">27 сентября · 30 дней</span>
      </div>
      <div style="display: flex; align-items: baseline;">
        <span style="font-size: 13px; color: #6b7480;">Устройств</span>
        <span style="margin-left: auto; font-size: 15px;">2 из 3</span>
      </div>
    </div>

    <!--
      Ссылка на телефоне — это и код показать через стол, и строка, которую
      вставляют в чат. Нужны обе, поэтому обе на виду.
    -->
    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 12px;">
      <div style="display: flex; align-items: center; gap: 14px;">
        <div style="flex-shrink: 0; padding: 5px; border: 1px solid #dfe3e8; border-radius: 8px;">${qr}</div>
        <div style="display: flex; flex-direction: column; gap: 8px; min-width: 0;">
          <span style="font-size: 14px; font-weight: 600;">Подписка кодом</span>
          <button style="font: inherit; font-size: 14px; cursor: pointer; border-radius: 9px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 11px 12px; min-height: 44px;">Во весь экран</button>
        </div>
      </div>
      <button style="font: inherit; font-size: 15px; cursor: pointer; border-radius: 10px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 12px 14px; width: 100%; min-height: 46px;">Скопировать ссылку для чата</button>
    </div>
  </div>

  <!--
    Внизу только то, зачем сюда зашли. «Отключить» убрано в «⋯»: рядом со
    «Ссылками», в самой удобной для пальца точке экрана, им отключают
    платящего человека — ровно та ошибка, от которой на компьютере уже
    защитились, спрятав её в меню.
  -->
  <div style="padding: 12px 16px; background: #ffffff; border-top: 1px solid #dfe3e8; display: flex; flex-direction: column; gap: 9px; flex-shrink: 0;">
    <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #2f6fed; background: #2f6fed; color: #ffffff; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Продлить на 30 дней</button>
    <button style="font: inherit; font-size: 15px; cursor: pointer; border-radius: 11px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 13px 16px; width: 100%; min-height: 48px;">Выдать доступ на новое устройство</button>
  </div>

${tabs("users")}`);

writeFileSync("MobileUser.dc.html", HEAD + user + TAIL, "utf8");

/* --------------------------------------------------------------- сводка */

const bar = (name, color, width, value, dim) => `
      <div style="display: flex; align-items: center; gap: 10px;">
        <span style="width: 118px; flex-shrink: 0; font-size: 13px; color: ${dim ? "#6b7480" : "#414954"}; ${cut}">${name}</span>
        <div style="flex-grow: 1; height: 10px; background: #f0f2f5; border-radius: 4px; overflow: hidden;"><i style="display: block; height: 100%; width: ${width}; background: ${color}; border-radius: 0 4px 4px 0;"></i></div>
        <span style="width: 52px; text-align: right; font-size: 13px; ${dim ? "color: #6b7480;" : "font-weight: 600;"}">${value}</span>
      </div>`;

const statsHeader = `      ${MARK}
      <div style="font-weight: 700; font-size: 17px;">Marvia</div>
      <span style="margin-left: auto; font-size: 12.5px; color: #6b7480;">копия базы — вчера</span>`;

const stats = frame(`${header(statsHeader)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 11px;">

    <div style="background: #ffffff; border: 1px solid #c0392b; border-radius: 12px; padding: 13px 15px; display: flex; align-items: center; gap: 11px; min-height: 56px; box-sizing: border-box;">
      <span style="width: 9px; height: 9px; border-radius: 50%; background: #c0392b; flex-shrink: 0;"></span>
      <span style="font-size: 14px; min-width: 0;">Германия молчит три часа — её выдают 41 человеку</span>
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#c0392b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-left: auto; flex-shrink: 0;"><path d="m9.5 6 6 6-6 6"/></svg>
    </div>

    <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 10px;">
      <div style="${card} padding: 13px 15px; display: flex; flex-direction: column; gap: 2px;">
        <span style="font-size: 13px; color: #6b7480;">Люди</span>
        <span style="font-size: 25px; font-weight: 700; letter-spacing: -.02em;">147</span>
        <span style="font-size: 13px; color: #6b7480;">у 139 работает</span>
      </div>
      <div style="${card} padding: 13px 15px; display: flex; flex-direction: column; gap: 2px;">
        <span style="font-size: 13px; color: #6b7480;">Устройств сейчас</span>
        <span style="font-size: 25px; font-weight: 700; letter-spacing: -.02em;">84</span>
        <span style="font-size: 13px; color: #6b7480;">за сутки было 112</span>
      </div>
    </div>

    <!--
      Кривой здесь нет. На 390 точках с неё не снять ни одного значения —
      наведения на телефоне не бывает, — а места она съедала под четверть
      экрана. Осталось то, ради чего на неё смотрели: два числа и куда растёт.
    -->
    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 6px;">
      <div style="display: flex; align-items: baseline;">
        <span style="font-size: 14px; font-weight: 600;">Трафик за сутки</span>
        <span style="margin-left: auto; font-size: 20px; font-weight: 700; letter-spacing: -.02em;">215 ГБ</span>
      </div>
      <div style="display: flex; align-items: baseline; gap: 10px;">
        <span style="font-size: 13px; color: #6b7480; flex-shrink: 0;">За месяц</span>
        <span style="margin-left: auto; font-size: 13.5px; color: #6b7480; text-align: right;">5,9 ТБ · на четверть больше, чем неделю назад</span>
      </div>
    </div>

    <div style="${card} padding: 14px 15px; display: flex; flex-direction: column; gap: 11px;">
      <span style="font-size: 14px; font-weight: 600;">Куда идёт трафик</span>
${bar("ОАЭ · Дубай", "#2f6fed", "84%", "1,0 ТБ")}
${bar("Нидерланды · Амстердам-1", "#1a7f45", "41%", "480 ГБ")}
${bar("Германия · Франкфурт", "#9333ea", "6%", "68 ГБ", true)}
    </div>
  </div>

${tabs("stats")}`);

writeFileSync("MobileStats.dc.html", HEAD + stats + TAIL, "utf8");
console.log("собраны MobileUser.dc.html и MobileStats.dc.html");
