import { writeFileSync } from "node:fs";
import { HEAD, TAIL, tabs, header, MARK, SAFE_BOTTOM } from "./mobile-shell.mjs";

const frame = (inner) =>
  `<div style="display: flex; flex-direction: column; width: 390px; height: 844px; background: #f6f7f9; color: #1b1f24; overflow: hidden;">\n${inner}\n</div>\n`;

const card = "background: #ffffff; border: 1px solid #dfe3e8; border-radius: 12px;";
const chevron = '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#b9c2cd" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-left: auto; flex-shrink: 0;"><path d="m9.5 6 6 6-6 6"/></svg>';
const cut = "overflow: hidden; text-overflow: ellipsis; white-space: nowrap;";

/* ------------------------------------------------------------------ ноды */

const nodeRow = (dot, name, state, stateColor, detail, dim) => `
      <div style="${card} padding: 13px 15px; display: flex; align-items: center; gap: 11px; min-height: 60px; box-sizing: border-box;${dim ? " opacity: .6;" : ""}">
        <span style="width: 9px; height: 9px; border-radius: 50%; background: ${dot}; flex-shrink: 0;"></span>
        <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0;">
          <div style="display: flex; align-items: center; gap: 7px; min-width: 0;">
            <span style="font-weight: 600; ${cut}">${name}</span>
            <span style="flex-shrink: 0; padding: 1px 7px; border-radius: 999px; font-size: 11.5px; border: 1px solid ${stateColor}; color: ${stateColor};">${state}</span>
          </div>
          <span style="font-size: 13px; color: #6b7480; ${cut}">${detail}</span>
        </div>
        ${chevron}
      </div>`;

const nodes = frame(`${header(`      ${MARK}
      <div style="font-weight: 700; font-size: 17px;">Ноды</div>
      <div style="margin-left: auto; display: flex; flex-direction: column; align-items: flex-end;">
        <span style="font-size: 13px; font-weight: 600; color: #c0392b;">2 из 4 работают</span>
        <span style="font-size: 12px; color: #6b7480;">обновлено 8 секунд назад</span>
      </div>`)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 10px;">

    <!-- То, ради чего он открыл телефон. Кнопка внизу, здесь только суть. -->
    <div style="background: #ffffff; border: 1px solid #c0392b; border-radius: 12px; padding: 14px 15px; display: flex; align-items: flex-start; gap: 11px;">
      <span style="width: 9px; height: 9px; border-radius: 50%; background: #c0392b; flex-shrink: 0; margin-top: 6px;"></span>
      <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0;">
        <span style="font-size: 16px; font-weight: 700;">Германия · Франкфурт молчит</span>
        <span style="font-size: 13px; color: #6b7480;">Три часа не выходит на связь, но её всё ещё выдают 41 человеку.</span>
      </div>
    </div>
${nodeRow("#1a7f45", "ОАЭ · Дубай", "работает", "#1a7f45", "доходят все · 42 мс · 1,0 ТБ · связь 8 секунд назад")}
${nodeRow("#a86400", "Нидерланды · Амстердам-1", "барахлит", "#a86400", "12 из 87 не доходят · 480 ГБ · связь минуту назад")}
${nodeRow("#b9c2cd", "Финляндия · Хельсинки", "выключена", "#6b7480", "из подписок убрана · была вчера", true)}
  </div>

  <!--
    Действие прибито к низу: до верха большим пальцем не дотянуться, а это
    единственная кнопка, ради которой экран открыли. Рядом — цена решения и
    обещание, что оно отыгрывается: без него ночью не нажимают.
  -->
  <div style="padding: 12px 16px; background: #ffffff; border-top: 1px solid #dfe3e8; display: flex; flex-direction: column; gap: 7px; flex-shrink: 0;">
    <button style="font: inherit; font-size: 16px; cursor: pointer; border-radius: 11px; border: 1px solid #c0392b; background: #ffffff; color: #c0392b; padding: 14px 16px; font-weight: 600; width: 100%; min-height: 50px;">Выключить Германию</button>
    <span style="font-size: 12.5px; color: #6b7480; text-align: center;">41 человек перейдёт на другие ноды. Вернуть можно одним нажатием, статистика останется.</span>
  </div>

${tabs("nodes")}`);

writeFileSync("MobileNodes.dc.html", HEAD + nodes + TAIL, "utf8");

/* ----------------------------------------------------------------- люди */

const person = (name, sub, right, rightColor, dim) => `
      <div style="${card} padding: 12px 15px; display: flex; align-items: center; gap: 11px; min-height: 58px; box-sizing: border-box;${dim ? " opacity: .6;" : ""}">
        <div style="display: flex; flex-direction: column; gap: 2px; min-width: 0;">
          <span style="font-weight: 600; ${cut}">${name}</span>
          <span style="font-size: 13px; color: #6b7480; ${cut}">${sub}</span>
        </div>
        <div style="margin-left: auto; display: flex; align-items: center; gap: 8px; flex-shrink: 0;">
          <span style="font-size: 13px; color: ${rightColor}; text-align: right;">${right}</span>
          ${chevron}
        </div>
      </div>`;

const users = frame(`${header(`      ${MARK}
      <div style="font-weight: 700; font-size: 17px;">Люди</div>
      <div style="margin-left: auto; font-size: 13px; color: #6b7480;">147</div>`)}

  <div style="flex-grow: 1; overflow: hidden; padding: 14px 16px; display: flex; flex-direction: column; gap: 10px;">

    <!-- Фильтры теми же словами, что на компьютере: «кончаются» — то, зачем сюда заходят чаще всего. -->
    <div style="display: flex; align-items: center; gap: 8px;">
      <span style="padding: 8px 14px; border-radius: 999px; font-size: 13.5px; background: #1b1f24; color: #ffffff; min-height: 36px; box-sizing: border-box;">Все</span>
      <span style="padding: 8px 14px; border-radius: 999px; font-size: 13.5px; background: #ffffff; border: 1px solid #dfe3e8; color: #414954; min-height: 36px; box-sizing: border-box;">Кончаются · 6</span>
      <span style="padding: 8px 14px; border-radius: 999px; font-size: 13.5px; background: #ffffff; border: 1px solid #dfe3e8; color: #414954; min-height: 36px; box-sizing: border-box;">Отключённые · 8</span>
    </div>
${person("заказ 1043", "#146 · 94 из 100 ГБ", "осталось<br>2 дня", "#a86400")}
${person("@mikhail_p", "#147 · был на связи 4 минуты назад", "до 27<br>сентября", "#6b7480")}
${person("@dmitry", "#145 · был на связи сейчас", "до 3<br>марта", "#6b7480")}
${person("@anna_k", "#131 · отключён", "вышла<br>14 августа", "#c0392b", true)}
${person("@sergey_ivanov_1990", "#144 · был вчера в 21:40", "до 12<br>октября", "#6b7480")}
  </div>

  <!--
    Поиск внизу, а не под часами: набирают его большим пальцем, и клавиатура
    выезжает снизу — поле должно оказаться прямо над ней, а не за ней.
  -->
  <div style="padding: 10px 16px 12px; background: #ffffff; border-top: 1px solid #dfe3e8; flex-shrink: 0;">
    <div style="display: flex; align-items: center; gap: 10px; background: #f0f2f5; border: 1px solid #dfe3e8; border-radius: 11px; padding: 12px 14px; min-height: 48px; box-sizing: border-box;">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#6b7480" stroke-width="2" stroke-linecap="round" style="flex-shrink: 0;"><circle cx="11" cy="11" r="6.5"/><path d="m20 20-4.2-4.2"/></svg>
      <span style="font-size: 15px; color: #6b7480;">Имя, @ или номер из чата</span>
    </div>
  </div>

${tabs("users")}`);

writeFileSync("MobileUsers.dc.html", HEAD + users + TAIL, "utf8");
console.log("собраны MobileNodes.dc.html и MobileUsers.dc.html");
