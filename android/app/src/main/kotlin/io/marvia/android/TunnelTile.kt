package io.marvia.android

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Intent
import android.graphics.drawable.Icon
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/**
 * TunnelTile — кнопка Marvia в шторке, рядом с Wi-Fi и фонариком.
 *
 * Туннель включают чаще, чем открывают приложение: зашёл в банк — выключил,
 * вышел — включил. Раскрыть шторку и нажать быстрее, чем найти значок среди
 * других. Так делают Hiddify, AmneziaWG и Happ, и человек, пришедший от них,
 * ищет её там же.
 *
 * Плитка ничего не решает сама: включает ту же службу, что кнопка в
 * приложении, и показывает то же состояние. Если включить без приложения
 * нельзя — нет ключа или система ещё не давала разрешения на VPN, — плитка
 * открывает приложение, а не молчит: спросить разрешение умеет только экран.
 */
class TunnelTile : TileService() {

    private var scope: CoroutineScope? = null
    private var watcher: Job? = null

    override fun onStartListening() {
        super.onStartListening()
        val s = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        scope = s
        watcher = s.launch { MarviaState.state.collect { paint(it) } }
    }

    override fun onStopListening() {
        watcher?.cancel()
        scope?.cancel()
        scope = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        val state = MarviaState.state.value
        if (state is TunnelState.On || state is TunnelState.Connecting) {
            startService(Intent(this, MarviaVpnService::class.java).setAction(MarviaVpnService.ACTION_STOP))
            return
        }
        val store = Store(this)
        if (store.accountLink.isBlank() || VpnService.prepare(this) != null) {
            openApp()
            return
        }
        try {
            ContextCompat.startForegroundService(this, Intent(this, MarviaVpnService::class.java))
        } catch (_: Throwable) {
            // Часть прошивок не даёт поднять службу из шторки. Тогда — через
            // приложение: человек нажал кнопку и должен увидеть, что будет.
            openApp()
        }
    }

    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(
                PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT),
            )
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    private fun paint(state: TunnelState) {
        val tile = qsTile ?: return
        tile.icon = Icon.createWithResource(this, R.drawable.ic_stat_marvia)
        tile.label = getString(R.string.app_name)
        when (state) {
            is TunnelState.On -> {
                tile.state = Tile.STATE_ACTIVE
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) tile.subtitle = state.node.substringBefore('·').trim()
            }
            is TunnelState.Connecting -> {
                tile.state = Tile.STATE_ACTIVE
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) tile.subtitle = getString(R.string.status_connecting)
            }
            else -> {
                tile.state = Tile.STATE_INACTIVE
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) tile.subtitle = getString(R.string.status_off)
            }
        }
        tile.updateTile()
    }
}
