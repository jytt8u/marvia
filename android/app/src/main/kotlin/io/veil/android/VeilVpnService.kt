package io.veil.android

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.core.app.NotificationCompat
import io.veil.mobile.Mobile
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import io.veil.mobile.Tunnel as Core

/**
 * VeilVpnService — то, ради чего приложение существует.
 *
 * Обязанностей у него ровно две, и обе платформенные: выпросить у системы
 * сетевой интерфейс и не дать себя выгрузить из памяти. Всё остальное — выбор
 * ноды, рукопожатие, разбор пакетов, учёт — делает ядро на Go, то же самое,
 * что работает на сервере. Ни строчки сетевой логики здесь нет и быть не
 * должно: продублированная логика расходится, и расходится она молча.
 */
class VeilVpnService : VpnService() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var worker: Job? = null

    @Volatile
    private var core: Core? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            shutdown(TunnelState.Off)
            return START_NOT_STICKY
        }

        // Постоянное уведомление надо показать в первые секунды после запуска,
        // иначе система убьёт службу за нарушение правил. Подключение занимает
        // куда больше — поэтому уведомление сначала, работа потом.
        goForeground(getString(R.string.status_connecting), getString(R.string.detail_connecting))
        connect()

        // Не START_STICKY: система перезапускала бы службу с пустым намерением
        // после каждого убийства, и неудачное подключение превратилось бы в
        // бесконечный круг попыток, который человек не может остановить.
        return START_NOT_STICKY
    }

    private fun connect() {
        if (worker?.isActive == true) {
            return
        }

        val link = Store(this).accountLink
        if (link.isBlank()) {
            shutdown(TunnelState.Failed(getString(R.string.detail_no_key)))
            return
        }

        VeilState.set(TunnelState.Connecting)

        worker = scope.launch {
            val descriptor = try {
                openInterface()
            } catch (t: Throwable) {
                shutdown(TunnelState.Failed(reasonOf(t)))
                return@launch
            }

            // Дескриптор отдаём насовсем. С этой строки он принадлежит ядру:
            // оно закроет его и при своей ошибке, и при остановке. Закрыть его
            // ещё и здесь означало бы закрыть номер дважды, а на Linux второй
            // раз попадёт уже по чужому сокету, успевшему этот номер занять.
            val fd = descriptor.detachFd()

            val started = try {
                Mobile.start(link, fd.toLong(), Mobile.DefaultDNS)
            } catch (t: Throwable) {
                shutdown(TunnelState.Failed(reasonOf(t)))
                return@launch
            }

            core = started
            val node = started.nodeName()
            VeilState.set(TunnelState.On(node))
            goForeground(getString(R.string.status_on), getString(R.string.detail_node, node))

            watch(started, node)
        }
    }

    /** openInterface просит у системы интерфейс и описывает, что в него слать. */
    private fun openInterface(): ParcelFileDescriptor {
        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(MTU)
            .addAddress(ADDRESS_V4, PREFIX_V4)
            .addRoute("0.0.0.0", 0)
            // IPv6 заворачиваем в туннель, даже если у ноды его нет. Оставить
            // его снаружи — это утечка: часть трафика пойдёт мимо, и цензор
            // увидит именно ту часть, которую мы прячем. Пусть лучше
            // соединение не состоится внутри туннеля: приложения после этого
            // сами переходят на IPv4.
            .addAddress(ADDRESS_V6, PREFIX_V6)
            .addRoute("::", 0)
            .addDnsServer(DNS)

        // Свой трафик в собственный туннель не заворачиваем. Иначе соединение
        // до ноды пошло бы через интерфейс, который сам же и ведёт к ноде.
        builder.addDisallowedApplication(packageName)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            builder.setMetered(false)
        }

        return builder.establish()
            ?: throw IllegalStateException(getString(R.string.error_no_interface))
    }

    /**
     * watch показывает ошибки отдельных соединений, не трогая туннель.
     *
     * Разница между «ничего не работает» и «не открывается один сайт» для
     * человека огромна, а изнутри ядра она видна сразу.
     */
    private suspend fun watch(started: Core, node: String) {
        var shown = ""
        while (scope.isActive && started.running()) {
            delay(POLL_INTERVAL_MS)
            val last = started.lastError()
            if (last != shown) {
                shown = last
                VeilState.set(TunnelState.On(node, last))
            }
        }
    }

    private fun shutdown(state: TunnelState) {
        worker?.cancel()
        worker = null

        val started = core
        core = null
        if (started != null) {
            try {
                started.stop()
            } catch (_: Throwable) {
                // Останавливаемся в любом случае: жаловаться уже некому.
            }
        }

        VeilState.set(state)
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    /** Система отобрала право на VPN — обычно потому, что включили другой. */
    override fun onRevoke() {
        shutdown(TunnelState.Off)
    }

    override fun onDestroy() {
        shutdown(TunnelState.Off)
        scope.cancel()
        super.onDestroy()
    }

    private fun goForeground(title: String, text: String?) {
        val manager = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL,
                getString(R.string.notification_channel),
                NotificationManager.IMPORTANCE_LOW,
            )
            channel.description = getString(R.string.notification_channel_desc)
            manager.createNotificationChannel(channel)
        }

        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            flags,
        )
        val stop = PendingIntent.getService(
            this,
            1,
            Intent(this, VeilVpnService::class.java).setAction(ACTION_STOP),
            flags,
        )

        val notification = NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_veil)
            .setContentTitle(title)
            .setContentText(text)
            .setContentIntent(open)
            .setOngoing(true)
            .setShowWhen(false)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .addAction(0, getString(R.string.notification_stop), stop)
            .build()

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE,
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    companion object {
        const val ACTION_STOP = "io.veil.android.action.STOP"

        private const val CHANNEL = "veil.tunnel"
        private const val NOTIFICATION_ID = 1
        private const val POLL_INTERVAL_MS = 5_000L

        // Адреса внутри туннеля. Наружу они не выходят и ни с чем не спорят:
        // это частные диапазоны, видимые только сетевому стеку телефона.
        private const val ADDRESS_V4 = "10.19.84.2"
        private const val PREFIX_V4 = 32
        private const val ADDRESS_V6 = "fdfe:dcba:9876::2"
        private const val PREFIX_V6 = 126
        private const val MTU = 1500

        // Адрес не важен: ядро перехватывает любой запрос имён по порту 53 и
        // отправляет его в туннель по TCP. Указываем осмысленный на случай,
        // если система решит показать его человеку в настройках сети.
        private const val DNS = "1.1.1.1"

        fun reasonOf(t: Throwable): String {
            val message = t.message
            return if (message.isNullOrBlank()) t.toString() else message
        }
    }
}
