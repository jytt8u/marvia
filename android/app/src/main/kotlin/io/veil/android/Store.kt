package io.veil.android

import android.content.Context

/**
 * Store — то немногое, что приложение помнит между запусками.
 *
 * Ключ доступа лежит в обычных настройках приложения. Их защищает песочница
 * Android: другое приложение файл не прочитает, а на заблокированном телефоне
 * он зашифрован ключом владельца. От root-доступа это не спасает, но и
 * androidx.security ничего к этому не добавил бы — там тот же аппаратный
 * ключ, только через прослойку, которую сама Google перестала развивать.
 */
class Store(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("veil", Context.MODE_PRIVATE)

    /** Ссылка вида veil-account://… — её выдаёт бот продавца при оплате. */
    var accountLink: String
        get() = prefs.getString(KEY_ACCOUNT_LINK, "").orEmpty()
        set(value) {
            prefs.edit().putString(KEY_ACCOUNT_LINK, value.trim()).apply()
        }

    private companion object {
        const val KEY_ACCOUNT_LINK = "account_link"
    }
}
