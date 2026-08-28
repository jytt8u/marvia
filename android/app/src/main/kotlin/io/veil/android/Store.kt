package io.veil.android

import android.content.Context
import io.veil.mobile.Mobile
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
            }
            prefs.edit().putString(KEY_ACCOUNT_LINK, link).apply()
        }

    /** Каталог, который приложение отдаёт ядру под кэш подписки. */
    fun cacheDir(): String = app.filesDir.absolutePath

    private fun dropSubscriptionCache() {
        // Не удалилось — не беда: ядро сверит отпечаток и просто не станет
        // этот кэш использовать, а первый удачный поход в панель его перезапишет.
        File(app.filesDir, Mobile.CacheName).delete()
    }

    private companion object {
        const val KEY_ACCOUNT_LINK = "account_link"
    }
}
