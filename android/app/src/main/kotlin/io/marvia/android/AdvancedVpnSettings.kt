package io.marvia.android

import android.view.View
import android.os.Build
import android.widget.HorizontalScrollView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity

/** Параметры VPN внутри Marvia: каждое значение здесь меняет работу службы или ядра. */
class AdvancedVpnSettings(
    private val host: AppCompatActivity,
    private val scroll: ScrollView,
    private val rows: LinearLayout,
    private val store: Store,
    private val theme: () -> Theme,
    private val onNextConnect: () -> Unit,
) {
    private val dp = host.resources.displayMetrics.density

    fun render() {
        val position = scroll.scrollY
        val t = theme()
        rows.removeAllViews()

        group(R.string.advanced_network_group)
        choice(
            R.string.advanced_mtu, R.string.advanced_mtu_sub,
            Store.MTU_CHOICES.map(Int::toString), Store.MTU_CHOICES.indexOf(store.vpnMtu),
        ) { store.vpnMtu = Store.MTU_CHOICES[it]; onNextConnect() }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            choice(
                R.string.advanced_metered, R.string.advanced_metered_sub,
                listOf(R.string.advanced_metered_auto, R.string.advanced_metered_always).map { host.getString(it) },
                if (store.meteredVpn) 1 else 0,
            ) { store.meteredVpn = it == 1; onNextConnect() }
        }
        choice(
            R.string.advanced_reports, R.string.advanced_reports_sub,
            listOf(R.string.advanced_reports_on, R.string.advanced_reports_off).map { host.getString(it) },
            if (store.sendNodeReports) 0 else 1,
        ) { store.sendNodeReports = it == 0; onNextConnect() }
        choice(
            R.string.advanced_notification, R.string.advanced_notification_sub,
            listOf(R.string.advanced_notification_full, R.string.advanced_notification_compact).map { host.getString(it) },
            if (store.compactNotification) 1 else 0,
        ) { store.compactNotification = it == 1 }

        group(R.string.advanced_monitor_group)
        val presets = listOf(Triple(10, 60, 60), Triple(2, 30, 30), Triple(2, 10, 10))
        val current = Triple(store.liveRefreshSeconds, store.idleRefreshSeconds, store.pingIntervalSeconds)
        choice(
            R.string.advanced_preset, R.string.advanced_preset_sub,
            listOf(R.string.advanced_preset_battery, R.string.advanced_preset_balanced, R.string.advanced_preset_detailed).map { host.getString(it) },
            presets.indexOf(current),
        ) {
            val (live, idle, ping) = presets[it]
            store.liveRefreshSeconds = live
            store.idleRefreshSeconds = idle
            store.pingIntervalSeconds = ping
        }
        choice(
            R.string.advanced_live, R.string.advanced_live_sub,
            Store.LIVE_REFRESH_CHOICES.map { host.getString(R.string.advanced_seconds, it) },
            Store.LIVE_REFRESH_CHOICES.indexOf(store.liveRefreshSeconds),
        ) { store.liveRefreshSeconds = Store.LIVE_REFRESH_CHOICES[it] }
        choice(
            R.string.advanced_idle, R.string.advanced_idle_sub,
            Store.IDLE_REFRESH_CHOICES.map { host.getString(R.string.advanced_seconds, it) },
            Store.IDLE_REFRESH_CHOICES.indexOf(store.idleRefreshSeconds),
        ) { store.idleRefreshSeconds = Store.IDLE_REFRESH_CHOICES[it] }
        choice(
            R.string.advanced_ping, R.string.advanced_ping_sub,
            Store.PING_INTERVAL_CHOICES.map {
                if (it == 0) host.getString(R.string.advanced_off_short)
                else host.getString(R.string.advanced_seconds, it)
            },
            Store.PING_INTERVAL_CHOICES.indexOf(store.pingIntervalSeconds),
        ) { store.pingIntervalSeconds = Store.PING_INTERVAL_CHOICES[it] }

        rows.addView(TextView(host).apply {
            setText(R.string.advanced_next_connection)
            textSize = 12f
            tag = "dim"
            setPadding(px(5), px(12), px(5), px(4))
        })
        Paint.apply(rows, t)
        scroll.post { scroll.scrollTo(0, position) }
    }

    private fun group(label: Int) {
        rows.addView(TextView(host).apply {
            setText(label)
            textSize = 10f
            letterSpacing = 0.16f
            tag = "dim"
            setPadding(px(5), px(18), 0, px(8))
        })
    }

    private fun choice(title: Int, description: Int, labels: List<String>, selected: Int, save: (Int) -> Unit) {
        val t = theme()
        val card = LinearLayout(host).apply {
            orientation = LinearLayout.VERTICAL
            tag = "card"
            setPadding(px(15), px(14), px(15), px(14))
        }
        rows.addView(card, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = px(9) })
        card.addView(TextView(host).apply {
            setText(title)
            textSize = 16f
            setTypeface(typeface, android.graphics.Typeface.BOLD)
            tag = "fg"
        })
        card.addView(TextView(host).apply {
            setText(description)
            textSize = 12f
            tag = "dim"
        }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = px(4) })

        val strip = LinearLayout(host).apply { orientation = LinearLayout.HORIZONTAL }
        labels.forEachIndexed { index, label ->
            strip.addView(TextView(host).apply {
                text = label
                textSize = 12f
                isSelected = index == selected
                isClickable = true
                isFocusable = true
                contentDescription = host.getString(title) + ": " + label
                setPadding(px(12), px(9), px(12), px(9))
                background = Paint.rounded(if (index == selected) t.acc else t.surf2, 12, dp)
                setTextColor(if (index == selected) t.accFg else t.fg)
                setOnClickListener { save(index); render() }
            }, LinearLayout.LayoutParams(-2, -2).apply { if (index > 0) marginStart = px(7) })
        }
        card.addView(HorizontalScrollView(host).apply {
            isHorizontalScrollBarEnabled = false
            overScrollMode = View.OVER_SCROLL_NEVER
            addView(strip)
        }, LinearLayout.LayoutParams(-1, -2).apply { topMargin = px(12) })
    }

    private fun px(value: Int) = (value * dp).toInt()
}
