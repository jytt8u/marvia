package io.marvia.android

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import androidx.core.app.NotificationCompat
import io.marvia.mobile.Mobile
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import io.marvia.mobile.Tunnel as Core

/**
 * MarviaVpnService — то, ради чего приложение существует.
 *
 * Обязанностей у него ровно две, и обе платформенные: выпросить у системы
 * сетевой интерфейс и не дать себя выгрузить из памяти. Всё остальное — выбор
 * ноды, рукопожатие, разбор пакетов, учёт — делает ядро на Go, то же самое,
 * что работает на сервере. Ни строчки сетевой логики здесь нет и быть не
 * должно: продублированная логика расходится, и расходится она молча.
 */
class MarviaVpnService : VpnService() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var worker: Job? = null

    /** Останавливались ли мы уже. См. [shutdown] — там объяснено, зачем. */
    private var finished = false

    @Volatile
    private var core: Core? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            shutdown(TunnelState.Off)
            return START_NOT_STICKY
        }

        // Служба могла уже один раз остановиться и не успеть разрушиться —
        // тогда система отдаёт запуск тому же объекту. Без сброса признака
        // остановки такая служба больше никогда бы не остановилась: [shutdown]
        // молча выходил бы, считая, что всё уже сделано.
        finished = false

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

        val store = Store(this)
        val link = store.accountLink
        if (link.isBlank()) {
            // Вид пустой намеренно. С видом account сюда подставилась бы фраза
            // «ключ не подошёл», а ключа просто нет — это разные вещи, и
            // человека они ведут в разные стороны.
            shutdown(TunnelState.Failed("", getString(R.string.detail_no_key)))
            return
        }

        Journal.add(getString(R.string.log_tunnel_on))
        MarviaState.set(TunnelState.Connecting)

        worker = scope.launch {
            val descriptor = try {
                openInterface(store.bypassed, store.bypassRussian)
            } catch (t: Throwable) {
                // Вид здесь известен без ядра: до ядра мы ещё не дошли.
                shutdown(TunnelState.Failed(Mobile.FailSystem, reasonOf(t)))
                return@launch
            }

            // Дескриптор отдаём насовсем. С этой строки он принадлежит ядру:
            // оно закроет его и при своей ошибке, и при остановке. Закрыть его
            // ещё и здесь означало бы закрыть номер дважды, а на Linux второй
            // раз попадёт уже по чужому сокету, успевшему этот номер занять.
            val fd = descriptor.detachFd()

            val started = try {
                // Каталог под кэш списка нод. Путь к своим файлам знает только
                // Context — ядру его взять неоткуда, поэтому передаём руками.
                Mobile.start(link, fd.toLong(), Mobile.DefaultDNS, store.cacheDir())
            } catch (t: Throwable) {
                shutdown(failureOf(t))
                return@launch
            }

            core = started
            val node = started.nodeName()
            val subscription = TunnelState.Subscription(
                until = started.until(),
                limitBytes = started.trafficLimit(),
                leftBytes = started.trafficLeft(),
            )
            Journal.add(getString(R.string.log_connected, node))
            MarviaState.set(TunnelState.On(node, subscription = subscription))
            goForeground(getString(R.string.status_on), getString(R.string.detail_node, node))

            watch(started, node, subscription)
        }
    }

    /** openInterface просит у системы интерфейс и описывает, что в него слать. */
    private fun openInterface(bypassed: Set<String>, bypassRu: Boolean): ParcelFileDescriptor {
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

        // Российские подсети мимо туннеля. Исключение маршрутов появилось в
        // Android 13; на старых остаётся исключение по приложениям, и обещать
        // человеку больше, чем умеет система, нельзя.
        if (bypassRu && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            var excluded = 0
            for (prefix in RuRoutes.load(this)) {
                try {
                    builder.excludeRoute(prefix)
                    excluded++
                } catch (t: IllegalArgumentException) {
                    // Одна битая строка в списке не повод остаться без туннеля.
                    Log.w(TAG, "подсеть не принята: " + t.message)
                }
            }
            Log.i(TAG, "мимо туннеля российских подсетей: " + excluded)
        }

        // Приложения, которые человек отправил мимо туннеля: госуслуги, банки,
        // всё, что не отвечает на запросы из-за границы. Система оставляет им
        // обычную сеть, и на сервер приходит их настоящий адрес.
        //
        // Пропавшее приложение не повод не подниматься: его могли удалить между
        // настройкой и запуском, а падать посреди включения VPN из-за этого —
        // худший из возможных ответов.
        for (pkg in bypassed) {
            try {
                builder.addDisallowedApplication(pkg)
            } catch (_: PackageManager.NameNotFoundException) {
                Log.w(TAG, "мимо туннеля просили $pkg, но оно не установлено")
            }
        }

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
     *
     * Имя ноды перечитываем на каждом круге, а не запоминаем при подключении.
     * Ядро меняет ноду само, когда прежняя замолчала, и раньше об этом здесь
     * не узнавали: на экране навсегда оставалась та нода, через которую трафик
     * давно не идёт. Удачный переезд выглядел как «ничего не произошло» — и
     * человек, глядя на мёртвое имя и на ошибки под ним, шёл переподключаться
     * руками. Ровно это и случилось на первой живой проверке.
     */
    private suspend fun watch(started: Core, node: String, subscription: TunnelState.Subscription) {
        var shown = ""
        var shownNode = node
        while (scope.isActive && started.running()) {
            delay(POLL_INTERVAL_MS)

            // Держащаяся беда важнее разовой ошибки и показывается вместо неё.
            //
            // Из ядра приезжает код, а не фраза: оно не знает языка интерфейса.
            // Раньше оттуда приходило русское предложение, и англоязычный
            // покупатель читал его как есть.
            val trouble = started.trouble()
            val last = if (trouble.isEmpty()) started.lastError() else troubleText(trouble)

            val now = started.nodeName()
            if (last != shown || now != shownNode) {
                shown = last
                if (now != shownNode) {
                    shownNode = now
                    Journal.add(getString(R.string.log_moved, now))
                    goForeground(getString(R.string.status_on), getString(R.string.detail_node, now))
                }
                MarviaState.set(TunnelState.On(shownNode, last, subscription))
            }
        }
    }

    /** troubleText подбирает фразу под код беды из ядра. */
    private fun troubleText(code: String): String = when (code) {
        "no-node" -> getString(R.string.trouble_no_node)
        // Незнакомый код — не повод молчать: покажем как есть, чтобы поломка
        // была видна, а не спрятана за пустой строкой.
        else -> code
    }

    /**
     * shutdown останавливает туннель и объявляет итоговое состояние.
     *
     * Останавливаемся ровно один раз, и вот почему. После stopSelf система
     * зовёт onDestroy, а тот тоже останавливается — и объявлял бы обычное
     * «отключено» поверх только что записанной причины обрыва. На экране
     * оставалось бы «Отключено. Трафик идёт напрямую», хотя подключение
     * только что провалилось. Это худший вид ошибки: человек видит, что не
     * работает, и не видит почему.
     */
    private fun shutdown(state: TunnelState) {
        if (finished) {
            return
        }
        finished = true

        if (state is TunnelState.Failed) {
            // В журнал — чтобы причину можно было достать с чужого телефона,
            // где экран уже закрыли и пересказывают по памяти.
            Log.w(TAG, "туннель не поднялся (${state.kind}): ${state.detail}")
            Journal.add(getString(R.string.log_failed, state.kind, state.detail))
        }

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

        MarviaState.set(state)
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
            Intent(this, MarviaVpnService::class.java).setAction(ACTION_STOP),
            flags,
        )

        val notification = NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_marvia)
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
        const val ACTION_STOP = "io.marvia.android.action.STOP"

        private const val TAG = "Veil"
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

        /**
         * failureOf разбирает ошибку ядра на вид и подробности.
         *
         * Ядро складывает их в одно сообщение — иначе не получится: наружу
         * gomobile отдаёт обычное исключение, и приложить к нему что-то ещё,
         * кроме текста, некуда. Вид едет первой строкой.
         */
        fun failureOf(t: Throwable): TunnelState.Failed {
            val raw = reasonOf(t)
            val cut = raw.indexOf('\n')
            if (cut <= 0) {
                // Вида нет — покажем как есть, это лучше, чем выдумывать.
                return TunnelState.Failed("", raw)
            }
            return TunnelState.Failed(raw.substring(0, cut), raw.substring(cut + 1).trim())
        }
    }
}
