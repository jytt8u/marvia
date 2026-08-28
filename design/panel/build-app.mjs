import { writeFileSync } from "node:fs";
import { HEAD, TAIL, frame, tabs, header, SAFE_TOP, SAFE_BOTTOM, BG, CARD, LINE, TEXT, MUTED, ACCENT, OK, WARN } from "./app-shell.mjs";

const card = `background: ${CARD}; border: 1px solid ${LINE}; border-radius: 14px;`;
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";

/* ------------------------------------------------------- главный экран */

// Кнопка одна и она огромная. Всё, что человек делает в приложении каждый
// день, — нажимает её; остальное он открывает раз в месяц или никогда.
const connect = frame(`${header(`<span style="margin-left: auto; font-size: 13px; color: ${MUTED};">до 27 сентября</span>`)}

  <div style="flex-grow: 1; display: flex; flex-direction: column; align-items: center; padding: 0 18px;">

    <div style="margin-top: 54px; width: 188px; height: 188px; border-radius: 50%; background: rgba(78, 194, 127, .07); display: flex; align-items: center; justify-content: center;">
      <div style="width: 152px; height: 152px; border-radius: 50%; background: rgba(78, 194, 127, .1); display: flex; align-items: center; justify-content: center;">
        <div style="width: 116px; height: 116px; border-radius: 50%; background: ${OK}; display: flex; align-items: center; justify-content: center;">
          <svg width="46" height="46" viewBox="0 0 24 24" fill="none" stroke="#0f1218" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.2v9.2"/><path d="M6.6 6.6a8 8 0 1 0 10.8 0"/></svg>
        </div>
      </div>
    </div>

    <div style="margin-top: 26px; font-size: 28px; font-weight: 700; letter-spacing: -.02em; color: ${OK};">Подключено</div>

    <!-- Страна, а не имя сервера: покупателю «ae-1» не говорит ничего. -->
    <div style="margin-top: 6px; display: flex; align-items: center; gap: 9px;">
      <span style="font-size: 16px;">ОАЭ · Дубай</span>
      <span style="padding: 2px 9px; border-radius: 999px; font-size: 12.5px; background: rgba(78, 194, 127, .12); color: ${OK};">42 мс</span>
    </div>
    <div style="margin-top: 3px; font-size: 13px; color: ${MUTED};">выбрано автоматически, самое быстрое</div>

    <!--
      Срок и остаток — прямо здесь. Это первое, что спрашивают у продавца, и
      каждый такой вопрос стоит ему времени: приложение знает ответ само.
    -->
    <div style="margin-top: auto; margin-bottom: 16px; width: 100%; ${card} padding: 15px 16px; display: flex; flex-direction: column; gap: 11px;">
      <div style="display: flex; align-items: baseline;">
        <span style="font-size: 14px; color: ${MUTED};">Осталось</span>
        <span style="margin-left: auto; font-size: 15px;">63 из 100 ГБ</span>
      </div>
      <div style="height: 7px; background: #0f1218; border-radius: 999px; overflow: hidden;"><i style="display: block; height: 100%; width: 63%; background: ${ACCENT};"></i></div>
      <div style="display: flex; align-items: center; gap: 8px;">
        <span style="font-size: 13px; color: ${MUTED};">Доступ до 27 сентября — это 30 дней</span>
        <span style="margin-left: auto; font-size: 13px; color: #6ea0ff;">Продлить</span>
      </div>
    </div>
  </div>

${tabs("power")}`);

writeFileSync("AppConnect.dc.html", HEAD + connect + TAIL, "utf8");

/* ------------------------------------------------------------- страны */

const country = (flag, name, ping, pingColor, note, selected) => `
      <div style="${card} ${selected ? `border-color: ${ACCENT};` : ""} padding: 13px 15px; display: flex; align-items: center; gap: 12px; min-height: 60px; box-sizing: border-box;">
        <span style="font-size: 24px; flex-shrink: 0; line-height: 1;">${flag}</span>
        <div style="display: flex; flex-direction: column; gap: 1px; min-width: 0;">
          <span style="font-size: 15.5px; font-weight: 600; ${cut}">${name}</span>
          <span style="font-size: 13px; color: ${MUTED}; ${cut}">${note}</span>
        </div>
        <div style="margin-left: auto; display: flex; align-items: center; gap: 11px; flex-shrink: 0;">
          <span style="font-size: 13.5px; color: ${pingColor};">${ping}</span>
          ${selected
            ? `<span style="width: 22px; height: 22px; border-radius: 50%; background: ${ACCENT}; display: flex; align-items: center; justify-content: center;"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="3.2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12.5 4.5 4.5L19 7.5"/></svg></span>`
            : `<span style="width: 22px; height: 22px; border-radius: 50%; border: 1.6px solid #3a424e; box-sizing: border-box;"></span>`}
        </div>
      </div>`;

const servers = frame(`${header(`<span style="margin-left: auto; font-size: 13px; color: #6ea0ff;">Обновить</span>`)}

  <div style="padding: 0 18px 8px; flex-shrink: 0;">
    <div style="font-size: 26px; font-weight: 700; letter-spacing: -.02em;">Страны</div>
    <div style="font-size: 13px; color: ${MUTED}; margin-top: 2px;">Скорость меряется с твоего телефона, а не с сервера продавца.</div>
  </div>

  <div style="flex-grow: 1; overflow: hidden; padding: 12px 18px; display: flex; flex-direction: column; gap: 10px;">
${country("⚡", "Автовыбор", "42 мс", OK, "самая быстрая прямо сейчас — ОАЭ", true)}

    <div style="font-size: 12.5px; color: ${MUTED}; padding: 6px 3px 0; text-transform: uppercase; letter-spacing: .04em;">Выбрать вручную</div>
${country("🇦🇪", "ОАЭ · Дубай", "42 мс", OK, "работает")}
${country("🇳🇱", "Нидерланды · Амстердам", "78 мс", WARN, "иногда не открывается")}
${country("🇫🇮", "Финляндия · Хельсинки", "—", MUTED, "сейчас недоступна")}
  </div>

  <div style="padding: 0 18px 14px; flex-shrink: 0;">
    <span style="font-size: 12.5px; color: ${MUTED};">Если страна перестала открываться, приложение уйдёт на другую само — выбирать вручную нужно редко.</span>
  </div>

${tabs("globe")}`);

writeFileSync("AppServers.dc.html", HEAD + servers + TAIL, "utf8");
console.log("собраны AppConnect.dc.html и AppServers.dc.html");
