package io.marvia.android

import android.content.Context
import io.marvia.mobile.Mobile
import java.io.File

/**
 * Store — то немногое, что приложение помнит между запусками.
 *
 * Ключ доступа лежит в обычных настройках приложения. Их защищает песочница
 * Android: другое приложение файл не прочитает, а на заблокированном телефоне
 * он зашифрован ключом владельца. От root-доступа это не спасает, но и
 * androidx.security ничего к этому не добавил бы — там тот же аппаратный
 * ключ, только через прослойку, которую сама Google перестала развивать.
 *
 * Рядом с настройками ядро держит кэш списка нод. Сам файл пишет и читает Go,
 * приложение знает про него ровно одно: когда ключ меняется, старый кэш надо
 * убрать. Ядро от чужого кэша защищено и само — оно сверяет отпечаток адреса
 * подписки, — но оставлять на диске адреса нод прошлого продавца незачем.
 */
class Store(context: Context) {

    private val app = context.applicationContext

    private val prefs = app.getSharedPreferences("veil", Context.MODE_PRIVATE)

    /** Ссылка вида marvia://… — её выдаёт бот продавца при оплате. */
    var accountLink: String
        get() = prefs.getString(KEY_ACCOUNT_LINK, "").orEmpty()
        set(value) {
            val link = value.trim()
            if (link != accountLink) {
                dropSubscriptionCache()
                // Запоминаем день, а не саму ссылку: в настройках человек
                // видит, когда ключ появился, и понимает, тот ли он, что
                // прислал продавец на прошлой неделе.
                prefs.edit().putLong(KEY_ACCOUNT_SAVED, System.currentTimeMillis()).apply()
            }
            prefs.edit().putString(KEY_ACCOUNT_LINK, link).apply()
        }

    /** Когда ключ положили сюда. Ноль означает, что он появился до этой записи. */
    val accountSavedAt: Long
        get() = prefs.getLong(KEY_ACCOUNT_SAVED, 0)

    /**
     * bypassed — приложения, которые ходят мимо туннеля.
     *
     * Госуслуги, банки и всё государственное не отвечают на запросы с
     * зарубежных адресов, а нода стоит за границей — отсюда и берётся «включил
     * VPN, и госуслуги не открываются». Исключённому приложению система
     * оставляет обычную сеть, и на сервер приходит нормальный российский адрес.
     *
     * Значок VPN в шторке при этом остаётся: его рисует Android, пока поднят
     * туннель, и убрать его нельзя ничем. Но ломает госуслуги не значок, а
     * адрес, и вот его исключение чинит.
     */
    var bypassed: Set<String>
        // Копию отдаём намеренно: SharedPreferences возвращает своё множество,
        // и правка его на месте молча не сохранилась бы.
        get() = prefs.getStringSet(KEY_BYPASSED, emptySet())?.toSet().orEmpty()
        set(value) {
            prefs.edit().putStringSet(KEY_BYPASSED, value.toSet()).apply()
        }

    /**
     * bypassRussian — вести ли российские сайты мимо туннеля.
     *
     * Отдельно от списка приложений: приложения человек выбирает сам, а это
     * один переключатель на всю страну. Работает с Android 13, где система
     * научилась исключать маршруты.
     */
    var bypassRussian: Boolean
        get() = prefs.getBoolean(KEY_BYPASS_RU, false)
        set(value) {
            prefs.edit().putBoolean(KEY_BYPASS_RU, value).apply()
        }

    /** Спрашивали ли уже про российские сайты. Второй раз не навязываемся. */
    var bypassAsked: Boolean
        get() = prefs.getBoolean(KEY_BYPASS_ASKED, false)
        set(value) {
            prefs.edit().putBoolean(KEY_BYPASS_ASKED, value).apply()
        }

    /**
     * autoStart — поднимать ли туннель после перезагрузки телефона.
     *
     * Телефон перезагружается сам: от обновления, от разряда, от зависания.
     * Человек замечает это не сразу, а когда открывает нужный сайт, — и
     * винит не телефон, а нас.
     */
    var autoStart: Boolean
        get() = prefs.getBoolean(KEY_AUTOSTART, false)
        set(value) {
            prefs.edit().putBoolean(KEY_AUTOSTART, value).apply()
        }

    /**
     * look — выбранная тема: пресет, акцент, плотность.
     *
     * До этого тем было две — светлая и тёмная, — и выбор лежал под ключом
     * theme числом режима AppCompatDelegate. Тот, кто выбирал светлую, получает
     * «Дневную»: это ближайший пресет, и человек не должен обнаружить, что его
     * выбор молча стёрли обновлением.
     */
    var look: Look.Choice
        get() {
            val preset = prefs.getString(KEY_PRESET, null)
                ?: if (prefs.getInt(KEY_THEME, -1) == LEGACY_LIGHT) "daylight" else LookTable.DEFAULT_PRESET
            return Look.Choice(
                preset = preset,
                accent = prefs.getInt(KEY_ACCENT, 0),
                density = prefs.getString(KEY_DENSITY, null) ?: LookTable.DEFAULT_DENSITY,
            )
        }
        set(value) {
            prefs.edit()
                .putString(KEY_PRESET, value.preset)
                .putInt(KEY_ACCENT, value.accent)
                .putString(KEY_DENSITY, value.density)
                .apply()
        }

    /**
     * language — язык приложения: ru, en или пусто, пока не выбран.
     *
     * Пустота означает первый запуск: тогда первым открывается экран выбора.
     * Сам язык применяет AppCompat и хранит его отдельно; здесь запоминается
     * только факт выбора и то, что показать отмеченным в настройках.
     */
    var language: String
        get() = prefs.getString(KEY_LANGUAGE, "").orEmpty()
        set(value) {
            prefs.edit().putString(KEY_LANGUAGE, value).apply()
        }

    /** Каталог, который приложение отдаёт ядру под кэш подписки. */
    fun cacheDir(): String = app.filesDir.absolutePath

    private fun dropSubscriptionCache() {
        // Не удалилось — не беда: ядро сверит отпечаток и просто не станет
        // этот кэш использовать, а первый удачный поход в панель его перезапишет.
        File(app.filesDir, Mobile.CacheName).delete()
    }

    companion object {
        const val LANG_RU = "ru"
        const val LANG_EN = "en"

        private const val KEY_ACCOUNT_LINK = "account_link"
        private const val KEY_ACCOUNT_SAVED = "account_saved_at"
        private const val KEY_BYPASSED = "bypassed_apps"
        private const val KEY_BYPASS_RU = "bypass_russian"
        private const val KEY_BYPASS_ASKED = "bypass_asked"
        private const val KEY_AUTOSTART = "autostart"
        private const val KEY_THEME = "theme"
        private const val KEY_LANGUAGE = "language"
        private const val KEY_PRESET = "look_preset"
        private const val KEY_ACCENT = "look_accent"
        private const val KEY_DENSITY = "look_density"

        /** AppCompatDelegate.MODE_NIGHT_NO — так хранилась светлая тема. */
        private const val LEGACY_LIGHT = 1
    }
}
