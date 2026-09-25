package io.marvia.android

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.view.Gravity
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenPermsBinding

/**
 * PermsScreen — второй шаг первого запуска: разрешения от Android.
 *
 * Три карточки, каждая просит своё системное окно. Без VPN туннель
 * невозможен — так устроен Android, — поэтому «Продолжить» появляется
 * только после него. Уведомления и батарея — по желанию: без них туннель
 * работает, просто молча и с риском уснуть; человек вправе решить это
 * потом в настройках телефона.
 *
 * Раньше уведомления просились сразу при старте, поверх выбора языка, а
 * согласие на VPN — при первом нажатии кнопки. Теперь всё в одном месте и с
 * объяснением, зачем.
 */
class PermsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenPermsBinding,
    private val theme: () -> Theme,
    /** Разрешения даны (по крайней мере VPN); экран можно закрывать. */
    private val onDone: () -> Unit,
) {
    private val dp = host.resources.displayMetrics.density

    private val vpnConsent = host.registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { paint() }
    private val notify = host.registerForActivityResult(ActivityResultContracts.RequestPermission()) { paint() }

    init {
        ui.permsContinue.setOnClickListener { onDone() }
    }

    /** open зовётся при каждом показе и возврате из системных окон: состояние могло смениться. */
    fun open() = paint()

    private fun vpnGranted(): Boolean = VpnService.prepare(host) == null

    private fun notifyGranted(): Boolean =
        Build.VERSION.SDK_INT < 33 || ContextCompat.checkSelfPermission(host, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun batteryGranted(): Boolean =
        (host.getSystemService(PowerManager::class.java))?.isIgnoringBatteryOptimizations(host.packageName) == true

    fun paint() {
        val t = theme()
        Paint.apply(ui.root, t)
        ui.stepOne.background = Paint.rounded(t.line, 999, dp)
        ui.stepTwo.background = Paint.rounded(t.acc, 999, dp)

        val list = ui.permList
        list.removeAllViews()
        val cards = listOf(
            Triple(R.string.perms_vpn to R.string.perms_vpn_why, R.drawable.ic_nav_tunnel, vpnGranted()) to { askVpn() },
            Triple(R.string.perms_notify to R.string.perms_notify_why, R.drawable.ic_bell, notifyGranted()) to { askNotify() },
            Triple(R.string.perms_battery to R.string.perms_battery_why, R.drawable.ic_battery, batteryGranted()) to { askBattery() },
        )
        for ((i, card) in cards.withIndex()) {
            val (texts, icon, granted) = card.first
            list.addView(permCard(t, texts.first, texts.second, icon, granted, card.second), LinearLayout.LayoutParams(MATCH, WRAP).apply { if (i > 0) topMargin = (10 * dp).toInt() })
        }
        val ok = vpnGranted()
        ui.permsContinue.isVisible = ok
        ui.permsWait.isVisible = !ok
    }

    private fun permCard(t: Theme, name: Int, why: Int, icon: Int, granted: Boolean, ask: () -> Unit): LinearLayout {
        val row = LinearLayout(host).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            val pad = (14 * dp).toInt()
            setPadding(pad, pad, pad, pad)
            background = Paint.card(t, dp, stroke = if (granted) t.acc else t.line)
        }
        val box = FrameLayout(host).apply {
            background = Paint.rounded(if (granted) t.acc else t.accSoft, 12, dp)
            addView(ImageView(host).apply { setImageResource(icon); setColorFilter(if (granted) t.accFg else t.acc) }, FrameLayout.LayoutParams((20 * dp).toInt(), (20 * dp).toInt(), Gravity.CENTER))
        }
        row.addView(box, LinearLayout.LayoutParams((42 * dp).toInt(), (42 * dp).toInt()).apply { marginEnd = (12 * dp).toInt() })
        val texts = LinearLayout(host).apply { orientation = LinearLayout.VERTICAL }
        texts.addView(TextView(host).apply { setText(name); textSize = 14.5f; setTextColor(t.fg); typeface = android.graphics.Typeface.create(typeface, android.graphics.Typeface.BOLD) })
        texts.addView(TextView(host).apply { setText(why); textSize = 12f; setTextColor(t.dim); setPadding(0, (2 * dp).toInt(), 0, 0) })
        row.addView(texts, LinearLayout.LayoutParams(0, WRAP, 1f))
        if (granted) {
            row.addView(ImageView(host).apply {
                setImageResource(R.drawable.ic_check)
                setColorFilter(t.accFg)
                background = Paint.circle(t.acc)
                val inset = (6 * dp).toInt()
                setPadding(inset, inset, inset, inset)
            }, LinearLayout.LayoutParams((26 * dp).toInt(), (26 * dp).toInt()).apply { marginStart = (10 * dp).toInt() })
        } else {
            row.addView(TextView(host).apply {
                setText(R.string.perms_allow)
                textSize = 12.5f
                setTextColor(t.accFg)
                typeface = android.graphics.Typeface.create(typeface, android.graphics.Typeface.BOLD)
                background = Paint.rounded(t.acc, 999, dp)
                setPadding((14 * dp).toInt(), (8 * dp).toInt(), (14 * dp).toInt(), (8 * dp).toInt())
                isClickable = true
                isFocusable = true
                setOnClickListener { ask() }
            }, LinearLayout.LayoutParams(WRAP, WRAP).apply { marginStart = (10 * dp).toInt() })
        }
        return row
    }

    private fun askVpn() {
        if (vpnGranted()) return paint()
        ThemedDialogs.builder(host, theme())
            .setTitle(R.string.perms_vpn_disclosure_title)
            .setMessage(R.string.perms_vpn_disclosure)
            .setPositiveButton(R.string.perms_vpn_accept) { _, _ -> requestVpn() }
            .setNegativeButton(R.string.perms_vpn_decline, null)
            .show()
    }

    private fun requestVpn() {
        val intent = VpnService.prepare(host) ?: return paint()
        try {
            vpnConsent.launch(intent)
        } catch (_: ActivityNotFoundException) {
            paint()
        }
    }

    private fun askNotify() {
        if (Build.VERSION.SDK_INT >= 33) notify.launch(Manifest.permission.POST_NOTIFICATIONS) else paint()
    }

    /**
     * askBattery ведёт в системное «не оптимизировать батарею». Прямым
     * запросом, а не общим экраном: там ещё найти нас надо, а запрос
     * показывает одно окно с «да» и «нет».
     */
    private fun askBattery() {
        try {
            host.startActivity(
                Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).setData(Uri.parse("package:" + host.packageName)),
            )
        } catch (_: ActivityNotFoundException) {
            try {
                host.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
            } catch (_: ActivityNotFoundException) {
                paint()
            }
        }
    }

    private companion object {
        const val MATCH = LinearLayout.LayoutParams.MATCH_PARENT
        const val WRAP = LinearLayout.LayoutParams.WRAP_CONTENT
    }
}
