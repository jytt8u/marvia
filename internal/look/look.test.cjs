const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { runInNewContext } = require('node:vm');
const { join } = require('node:path');

function load(storage = new Map()) {
  return runInNewContext(readFileSync(join(__dirname, 'look.js'), 'utf8') + '\nLook;', {
    localStorage: {
      getItem: key => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
  });
}

test('новая установка нейтральна, выбранный вид переживает перезапуск', () => {
  const storage = new Map(), look = load(storage);
  assert.equal(look.load().preset, 'steel');
  const chosen = { ...look.LOOKS[4], acc: '#f08070', tint: '#778899' };
  look.save(chosen);
  assert.equal(load(storage).encode(load(storage).load()), look.encode(chosen));
});

test('все готовые виды переносятся кодом без потерь', () => {
  const look = load();
  for (const preset of look.LOOKS) {
    const code = look.encode(preset);
    assert.equal(look.encode(look.decode(code)), code);
    assert.equal(look.encode(look.decode('  ' + code.toLowerCase() + '  ')), code);
  }
});

test('повреждённая глубина не превращается в другой допустимый код', () => {
  const look = load(), parts = look.encode(look.DEFAULT).split('-');
  for (const bad of ['55junk', '55.9', '5e1', '0x37', '+55', '', '7', '99']) {
    parts[4] = bad;
    assert.equal(look.decode(parts.join('-')), null, bad);
  }
});

test('посторонние имена в сохранённой теме не ломают запуск', () => {
  const look = load();
  for (const key of ['preset', 'dir', 'radius', 'density', 'glow']) {
    for (const bad of ['constructor', '__proto__', 'toString']) {
      const stored = { [key]: bad, acc: '#abcdef' };
      assert.equal(look.normalize(stored)[key], look.DEFAULT[key]);
      assert.doesNotThrow(() => look.theme(stored));
      assert.equal(look.normalize(stored).acc, '#abcdef');
    }
  }
});

test('недоступное хранилище не мешает выбрать вид', () => {
  const look = runInNewContext(readFileSync(join(__dirname, 'look.js'), 'utf8') + '\nLook;', {
    localStorage: { getItem() { throw Error('denied'); }, setItem() { throw Error('denied'); } },
  });
  assert.equal(look.load().preset, 'steel');
  assert.doesNotThrow(() => look.save(look.LOOKS[1]));
});
