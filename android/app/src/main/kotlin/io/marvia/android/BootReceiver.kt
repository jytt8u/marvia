package io.marvia.android

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.util.Log
import androidx.core.content.ContextCompat

/**
 * BootReceiver поднимает туннель после перезагрузки телефона.
 *
 * Телефон перезагружается сам — от обновления системы, от разряда, от
 * зависания. Человек этого не замечает и обнаруживает, что «интернет опять
 * не работает», уже когда открывает нужный сайт. Просить его каждый раз
 * заходить в приложение и нажимать кнопку — значит требовать помнить о нашей
 * работе.
 *
 * Есть и более надёжный путь: у Android свой «постоянный VPN» в системных
 * настройках, он поднимает туннель раньше нас и переживает наше падение. Мы
 * говорим о нём в приложении, а этот приёмник — для тех, кто туда не пошёл.
 */
class BootReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED &&
            intent.action != "android.intent.action.QUICKBOOT_POWERON"
        ) {
            return
        }

        val store = Store(context)
        if (!store.autoStart || store.accountLink.isBlank()) {
            return
        }

        // Разрешение на VPN человек уже давал: система помнит его до переустановки
        // приложения. Если не давал — тихо выходим: спрашивать согласие в момент
        // включения телефона нельзя, экрана ещё нет.
        if (VpnService.prepare(context) != null) {
            Log.i("Marvia", "автозапуск: разрешение на VPN не выдано, жду человека")
            return
        }

        try {
            ContextCompat.startForegroundService(context, Intent(context, MarviaVpnService::class.java))
        } catch (t: Throwable) {
            // На части прошивок запуск службы сразу после загрузки запрещён.
            // Это не повод падать: человек включит руками, как раньше.
            Log.w("Marvia", "автозапуск не вышел: ${t.message}")
        }
    }
}
