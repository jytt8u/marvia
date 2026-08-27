package bot

// ExampleConfig — образец файла настроек.
//
// Печатается по -example и служит единственной инструкцией: продавец правит
// три строки сверху и цены, остальное работает как есть. Всё, что можно было
// решить за него, уже решено.
const ExampleConfig = `{
  "telegram_token": "укажи токен от @BotFather или задай VEIL_BOT_TOKEN",
  "panel": "https://panel.example.com",
  "panel_key": "vk_ключ_с_правом_users",

  "admins": [0],
  "support": "@имя_в_телеграме",

  "payment": "stars",
  "manual_note": "Переведи на карту 0000 0000 0000 0000 и пришли скриншот.",

  "downloads": {
    "android": "https://github.com/jytt8u/marvia-releases/releases/latest/download/veil-android.apk",
    "windows": "https://github.com/jytt8u/marvia-releases/releases/latest/download/veil-windows.exe"
  },

  "tariffs": [
    {"id": "trial", "title": "Пробные 3 дня", "days": 3,  "price": 0,   "traffic_gb": 5,   "devices": 1},
    {"id": "m1",    "title": "1 месяц",       "days": 30, "price": 150, "traffic_gb": 100, "devices": 3},
    {"id": "m3",    "title": "3 месяца",      "days": 90, "price": 390, "traffic_gb": 300, "devices": 3},
    {"id": "y1",    "title": "Год",           "days": 365,"price": 1290,"traffic_gb": 0,   "devices": 5}
  ]
}
`
