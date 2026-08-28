import { writeFileSync } from "node:fs";
import { HEAD, TAIL, tabs, header, MARK, SAFE_BOTTOM } from "./mobile-shell.mjs";

const frame = (inner) =>
  `<div style="display: flex; flex-direction: column; width: 390px; height: 844px; background: #f6f7f9; color: #1b1f24; overflow: hidden; position: relative;">\n${inner}\n</div>\n`;
const card = "background: #ffffff; border: 1px solid #dfe3e8; border-radius: 12px;";
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";
const chevron = '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#b9c2cd" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-left: auto; flex-shrink: 0;"><path d="m9.5 6 6 6-6 6"/></svg>';

/* --------------------------------------------------- подтверждение шторкой */

// Шторка снизу, а не окно посередине: до середины экрана большим пальцем не
// дотянуться, а решение принимают на ходу. Кнопки в порядке «мягкое сверху,
// опасное снизу» — промах вверх безопаснее промаха вниз.
const sheet = frame(`  <!-- Затемнённый экран нод под шторкой: видно, откуда пришли. -->
  <div style="position: absolute; inset: 0; background: #f6f7f9;"></div>
  <div style="position: absolute; inset: 0; background: rgba(27, 31, 36, .55);"></div>

  <div style="position: absolute; left: 0; right: 0; bottom: 0; background: #ffffff; border-radius: 20px 20px 0 0; padding: 10px 16px ${SAFE_BOTTOM + 10}px; display: flex; flex-direction: column; gap: 14px;">

    <div style="width: 40px; height: 4px; border-radius: 999px; background: #dfe3e8; align-self: center;"></div>

    <div style="display: flex; flex-direction: column; gap: 5px;">
      <span style="font-size: 19px; font-weight: 700;">Выключить Германию?</span>
      <span style="font-size: 14px; color: #6b7480;">41 человек сразу перестанет её получать и перейдёт на ОАЭ и Нидерланды. Скорость у них может просесть — эти двое возьмут нагрузку на себя.</span>
    </div>

    <div style="display: flex; gap: 10px; padding: 12px 14px; background: #f6f7f9; border-radius: 10px;">
      <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="#1a7f45" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; margin-top: 2px;"><path d="M20 11.5A8 8 0 1 0 18.6 16"/><path d="M20 5v6h-6"/></svg>
      <span style="font-size: 13px; color: #414954;">Это отыгрывается: включить обратно — одно нажатие, статистика и расход останутся на месте.</span>
    </div>

    <div style="display: flex; flex-direction: column; gap: 9px;">
      <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #dfe3e8; background: #f0f2f5; color: #1b1f24; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Оставить как есть</button>
      <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #c0392b; background: #ffffff; color: #c0392b; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Выключить</button>
    </div>
  </div>`);

writeFileSync("MobileConfirm.dc.html", HEAD + sheet + TAIL, "utf8");

/* ------------------------------------------------------------------ ещё */

const row = (title, sub, right, danger) => `
      <div style="${card} padding: 13px 15px; display: flex; align-items: center; gap: 11px; min-height: 58px; box-sizing: border-box;">
        <div style="display: flex; flex-direction: column; gap: 1px; min-width: 0;">
          <span style="font-weight: 600; ${danger ? "color: #c0392b;" : ""} ${cut}">${title}</span>
          ${sub ? `<span style="font-size: 13px; color: #6b7480; ${cut}">${sub}</span>` : ""}
        </div>
        <div style="margin-left: auto; display: flex; align-items: center; gap: 8px; flex-shrink: 0;">
          ${right ? `<span style="font-size: 13px; color: #6b7480;">${right}</span>` : ""}
          ${chevron}
        </div>
      </div>`;

const more = frame(`${header(`      ${MARK}
      <div style="font-weight: 700; font-size: 17px;">Ещё</div>`)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 14px;">

    <!--
      Здесь лежит то, что с телефона нужно смотреть, но не делать. Копия базы
      — про «насколько всё плохо», и думают о ней ровно тогда, когда что-то
      упало. Ключ бота — единственное действие в разделе: отзывают его не по
      расписанию, а в тот вечер, когда бот начал раздавать не то.
    -->
    <div style="display: flex; flex-direction: column; gap: 9px;">
      <span style="font-size: 12.5px; color: #6b7480; padding: 0 3px; text-transform: uppercase; letter-spacing: .04em;">Хозяйство</span>
${row("Копия базы", "снята вчера в 03:00 · хранится 7 штук", "")}
${row("Ключи для ботов", "два ключа, оба живые", "")}
${row("Приложения", "Android и Windows выложены", "")}
    </div>

    <div style="display: flex; flex-direction: column; gap: 9px;">
      <span style="font-size: 12.5px; color: #6b7480; padding: 0 3px; text-transform: uppercase; letter-spacing: .04em;">Вид</span>
${row("Язык", "", "Русский")}
${row("Тема", "", "Светлая")}
    </div>

    <div style="display: flex; flex-direction: column; gap: 9px;">
      <span style="font-size: 12.5px; color: #6b7480; padding: 0 3px; text-transform: uppercase; letter-spacing: .04em;">Marvia</span>
${row("Как это работает", "", "")}
${row("Поддержка", "", "")}
${row("Выйти из панели", "телефон теряют и дают в руки", "", true)}
    </div>

    <div style="font-size: 12.5px; color: #6b7480; text-align: center; padding-top: 2px;">
      Marvia v0.5.0 · всё работает на твоём сервере
    </div>
  </div>

${tabs("more")}`);

writeFileSync("MobileMore.dc.html", HEAD + more + TAIL, "utf8");
console.log("собраны MobileConfirm.dc.html и MobileMore.dc.html");
