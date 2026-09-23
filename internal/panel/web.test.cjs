const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const { Script, runInNewContext } = require('node:vm');

const page = readFileSync(join(__dirname, 'web/index.html'), 'utf8');

// Проверяем обещание диалога: любое закрытие заканчивает ожидание решения,
// а кнопка подтверждения не превращается в отмену при закрытии окна.
function confirmation() {
  let dialog;
  const ask = runInNewContext(page.match(/function ask\(title, text, danger\) \{[\s\S]*?\n\}/)[0] + '\nask;', {
    Promise,
    el: () => ({}),
    btn: (text, cls, click) => ({ text, click }),
    L: () => ({ act: { yes: 'Да', cancel: 'Отмена' } }),
    sheet: (title, body, buttons, onClose) => { dialog = { buttons, onClose }; },
    closeSheet: () => { if (dialog.onClose) dialog.onClose(); },
  });
  return { ask, dialog: () => dialog };
}

test('закрытие подтверждения без кнопки считается отменой', async () => {
  const ui = confirmation();
  const result = ui.ask('Удалить?', 'Это действие нельзя отменить');
  if (ui.dialog().onClose) ui.dialog().onClose();
  assert.equal(await Promise.race([result, Promise.resolve('ожидание не завершилось')]), false);
});

test('кнопки подтверждения и отмены возвращают выбранное решение', async () => {
  for (const [button, expected] of [[0, true], [1, false]]) {
    const ui = confirmation();
    const result = ui.ask('Удалить?', 'Это действие нельзя отменить');
    ui.dialog().buttons[button].click();
    assert.equal(await result, expected);
  }
});

test('вшитые скрипты панели и Windows разбираются движком JavaScript', () => {
  const look = readFileSync(join(__dirname, '../look/look.js'), 'utf8');
  for (const file of ['web/index.html', '../../cmd/marvia-windows/ui/app.html']) {
    const html = readFileSync(join(__dirname, file), 'utf8').replace('/*%LOOK%*/', look);
    const script = html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
    assert.doesNotThrow(() => new Script(script, { filename: file }));
  }
});

test('кнопка обновления не откатывает версию новее релиза', () => {
  const older = runInNewContext(page.match(/function older\(a, b\) \{[\s\S]*?\n\}/)[0] + '\nolder;');
  assert.equal(older('v0.11.0', 'v0.12.0'), true);
  assert.equal(older('v0.9.9', 'v0.10.0'), true);
  assert.equal(older('v0.12.0', 'v0.12.0'), false);
  assert.equal(older('v0.13.0', 'v0.12.0'), false);
  assert.equal(older('dev', 'v0.12.0'), false);
});
