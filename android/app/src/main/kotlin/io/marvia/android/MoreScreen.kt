package io.marvia.android

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Typeface
import android.provider.Settings as AndroidSettings
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import io.marvia.android.databinding.ItemAppBinding
import io.marvia.android.databinding.ScreenMoreBinding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * MoreScreen — вкладка «Ещё»: соединение, приложения, логи.
 *
 * Три раздела под одним заголовком, как в макете. Здесь нет ручек протокола:
 * ни размера фрагментов, ни числа соединений, ни выбора мультиплексора.
 * Такие настройки бывают у оболочек над чужим движком, которому надо
 * объяснить, к какому серверу он подключается. У нас клиент и нода — одна
 * система, и все эти решения приняты внутри; вынести их на экран значило бы
 * дать человеку сломать себе связь, не понимая чем.
 *
 * Остаётся то, что зависит от него, а не от сети: когда включаться, кто
 * отвечает на запросы имён, что пускать мимо, что показать продавцу, когда
 * что-то пошло не так.
 *
 * Из макета намеренно нет: kill switch (это системный «постоянный VPN», и
 * туда ведёт строка), MTU и IPv6 (решает ядро), «обфускации» (она не
 * выключается — это и есть протокол), уровня «только TCP».
 */
class MoreScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenMoreBinding,
    private val store: Store,
    private val theme: () -> Theme,
    /** Открыть экран ключа: он общий с первым запуском. */
    private val onKey: () -> Unit,
    /** Открыть выбор языка: он тоже общий с первым запуском. */
    private val onLanguage: () -> Unit,
    /** Что-то из этого применяется только на следующем подключении. */
    private val onRoutesChanged: () -> Unit,
) {

    enum class Section { CONN, APPS, LOGS }

    private var section = Section.CONN

    /** Фильтр логов: null — все уровни. */
    private var logFilter: Journal.Level? = null

    private val apps = AppsAdapter()
    private val dp = host.resources.displayMetrics.density

    init {
        ui.pillConn.setOnClickListener { show(Section.CONN) }
        ui.pillApps.setOnClickListener { show(Section.APPS) }
        ui.pillLogs.setOnClickListener { show(Section.LOGS) }

        wireConnection()
        wireApps()
        wireLogs()
    }

    /** open зовётся при каждом показе: настройки меняются и из других мест. */
    fun open() {
        show(section)
    }

    /** paint перекрашивает то, что красится кодом: таблетки и строки логов. */
    fun paint() {
        paintPills()
        paintModes()
        if (section == Section.LOGS) {
            renderLogs()
        }
        apps.notifyDataSetChanged()
    }

    private fun show(next: Section) {
        section = next
        ui.sectionConn.isVisible = next == Section.CONN
        ui.sectionApps.isVisible = next == Section.APPS
        ui.sectionLogs.isVisible = next == Section.LOGS

        val (title, sub) = when (next) {
            Section.CONN -> R.string.more_conn_title to R.string.more_conn_sub
            Section.APPS -> R.string.more_apps_title to 0
            Section.LOGS -> R.string.more_logs_title to 0
        }
        ui.moreTitle.setText(title)
        ui.moreSub.text = when (next) {
            Section.CONN -> host.getString(sub)
            Section.APPS -> appsSummary()
            Section.LOGS -> logsSub()
        }
        paintPills()

        when (next) {
            Section.CONN -> renderConnection()
            Section.APPS -> openApps()
            Section.LOGS -> renderLogs()
        }
    }

    private fun paintPills() {
        val t = theme()
        pill(ui.pillConn, t, section == Section.CONN)
        pill(ui.pillApps, t, section == Section.APPS)
        pill(ui.pillLogs, t, section == Section.LOGS)
    }

    /** pill красит таблетку: активная — акцентом, остальные — второй поверхностью. */
    private fun pill(view: TextView, t: Theme, on: Boolean) {
        view.background = Paint.rounded(if (on) t.acc else t.surf2, minOf(t.r, 14), dp)
        view.setTextColor(if (on) t.accFg else t.dim)
    }

    // ---------------------------------------------------------- соединение

    private fun wireConnection() {
        ui.rowAutostart.setOnClickListener {
            store.autoStart = !store.autoStart
            renderConnection()
            Toast.makeText(
                host,
                if (store.autoStart) {
                    R.string.settings_autostart_saved
                } else {
                    R.string.settings_autostart_off_saved
                },
                Toast.LENGTH_SHORT,
            ).show()
        }

        ui.rowAlwaysOn.setOnClickListener { openAlwaysOn() }
        ui.rowDns.setOnClickListener { chooseDns() }

        ui.rowLan.setOnClickListener {
            store.lanOutside = !store.lanOutside
            renderConnection()
            onRoutesChanged()
        }

        ui.rowRussian.setOnClickListener {
            store.bypassRussian = !store.bypassRussian
            renderConnection()
            onRoutesChanged()
        }

        ui.rowLanguage.setOnClickListener { onLanguage() }
        ui.rowKey.setOnClickListener { onKey() }
        ui.rowAbout.setOnClickListener { about() }

        // Исключать маршруты умеет только Android 13 и новее. На старых
        // строк нет вовсе: переключатель обещал бы то, чего система не
        // сделает, а человек считал бы, что госуслуги уже починены.
        val routes = RuRoutes.supported()
        ui.rowLan.isVisible = routes
        ui.dividerLan.isVisible = routes
        ui.rowRussian.isVisible = routes
        ui.dividerRu.isVisible = routes
    }

    private fun renderConnection() {
        ui.switchAutostart.isChecked = store.autoStart
        ui.switchLan.isChecked = store.lanOutside
        ui.switchRussian.isChecked = store.bypassRussian
        ui.dnsValue.text = dnsName(store.dns)
        ui.languageValue.text = host.getString(
            if (store.language == Store.LANG_EN) R.string.language_en else R.string.language_ru,
        )

        val saved = store.accountSavedAt
        ui.keySummary.isVisible = saved > 0
        if (saved > 0) {
            ui.keySummary.text = host.getString(R.string.settings_key_added, Format.day(host, saved))
        }
    }

    /**
     * chooseDns — кто отвечает на запросы имён. Список короткий и известный:
     * своё вписать нельзя, опечатка в адресе резолвера — это «интернет не
     * работает» без единой подсказки, почему.
     */
    private fun chooseDns() {
        val choices = Store.DNS_CHOICES
        val labels = choices.map { dnsName(it) + "  ·  " + it }.toTypedArray()
        val current = choices.indexOf(store.dns)

        AlertDialog.Builder(host)
            .setTitle(R.string.conn_dns)
            .setSingleChoiceItems(labels, current) { dialog, which ->
                store.dns = choices[which]
                renderConnection()
                onRoutesChanged()
                dialog.dismiss()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun dnsName(address: String): String = when (address) {
        "1.1.1.1" -> "Cloudflare"
        "8.8.8.8" -> "Google"
        "9.9.9.9" -> "Quad9"
        "94.140.14.14" -> "AdGuard"
        else -> address
    }

    /**
     * openAlwaysOn отправляет в системный «постоянный VPN».
     *
     * Он надёжнее нашего автозапуска: система поднимает туннель раньше нас,
     * переживает наше падение и умеет не пускать трафик мимо, пока туннель не
     * встал, — это и есть kill switch, только настоящий. Прятать это от
     * человека ради того, чтобы наша галочка выглядела главной, — нечестно.
     */
    private fun openAlwaysOn() {
        try {
            host.startActivity(Intent(AndroidSettings.ACTION_VPN_SETTINGS))
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(host, R.string.settings_always_on_missing, Toast.LENGTH_LONG).show()
        }
    }

    /** about отвечает на «какая у тебя версия» — первый вопрос продавца. */
    private fun about() {
        val version = try {
            host.packageManager.getPackageInfo(host.packageName, 0).versionName.orEmpty()
        } catch (_: PackageManager.NameNotFoundException) {
            ""
        }

        AlertDialog.Builder(host)
            .setTitle(R.string.settings_about)
            .setMessage(host.getString(R.string.about_body, host.getString(R.string.app_name), version))
            .setPositiveButton(android.R.string.ok, null)
            .show()
    }

    // ---------------------------------------------------------- приложения

    private fun wireApps() {
        ui.appList.layoutManager = LinearLayoutManager(host)
        ui.appList.adapter = apps
        ui.appList.itemAnimator = null

        ui.modeExclude.setOnClickListener { setMode(Store.BYPASS_EXCLUDE) }
        ui.modeInclude.setOnClickListener { setMode(Store.BYPASS_INCLUDE) }
        ui.modeOff.setOnClickListener { setMode(Store.BYPASS_OFF) }

        // Набор одной кнопкой: отмечает госуслуги и банки из тех, что стоят.
        // Только добавляет — снимать чужие отметки за человека нельзя.
        ui.presetChip.setOnClickListener {
            val chosen = store.bypassed.toMutableSet()
            val installed = apps.entries.map { it.pkg }.toSet()
            val added = Bypass.PRESET.filter { it in installed && chosen.add(it) }
            if (added.isEmpty()) {
                Toast.makeText(host, R.string.apps_preset_none, Toast.LENGTH_SHORT).show()
                return@setOnClickListener
            }
            store.bypassed = chosen
            apps.notifyDataSetChanged()
            afterAppsChanged()
        }
    }

    private fun setMode(mode: String) {
        if (store.bypassMode == mode) {
            return
        }
        store.bypassMode = mode
        afterAppsChanged()
    }

    private fun afterAppsChanged() {
        paintModes()
        ui.moreSub.text = appsSummary()
        onRoutesChanged()
    }

    private fun paintModes() {
        val t = theme()
        val mode = store.bypassMode
        pill(ui.modeExclude, t, mode == Store.BYPASS_EXCLUDE)
        pill(ui.modeInclude, t, mode == Store.BYPASS_INCLUDE)
        pill(ui.modeOff, t, mode == Store.BYPASS_OFF)
        ui.modeNote.setText(
            when (mode) {
                Store.BYPASS_INCLUDE -> R.string.apps_note_include
                Store.BYPASS_OFF -> R.string.apps_note_off
                else -> R.string.apps_note_exclude
            },
        )
        ui.presetChip.isVisible = mode != Store.BYPASS_OFF
    }

    /** appsSummary — подпись под заголовком: сколько и в какую сторону. */
    private fun appsSummary(): String {
        val n = store.bypassed.size
        return when (store.bypassMode) {
            Store.BYPASS_OFF -> host.getString(R.string.apps_summary_off)
            Store.BYPASS_INCLUDE ->
                if (n == 0) host.getString(R.string.apps_summary_include_none)
                else host.resources.getQuantityString(R.plurals.apps_summary_include, n, n)
            else ->
                if (n == 0) host.getString(R.string.bypass_none)
                else host.resources.getQuantityString(R.plurals.apps_summary_exclude, n, n)
        }
    }

    /**
     * openApps читает список установленного в фоне и показывает отмеченные
     * сверху: человек открывает этот экран второй раз, чтобы посмотреть или
     * снять то, что уже отметил, а не искать заново.
     */
    private fun openApps() {
        paintModes()
        if (apps.entries.isNotEmpty()) {
            apps.notifyDataSetChanged()
            return
        }
        ui.appsLoading.isVisible = true
        host.lifecycleScope.launch {
            val entries = withContext(Dispatchers.IO) { Bypass.installed(host) }
            val chosen = store.bypassed
            apps.entries = entries.sortedWith(
                compareByDescending<Bypass.Entry> { it.pkg in chosen }.thenBy { it.label.lowercase() },
            )
            ui.appsLoading.isVisible = false
            apps.notifyDataSetChanged()
        }
    }

    private inner class AppsAdapter : RecyclerView.Adapter<AppHolder>() {
        var entries: List<Bypass.Entry> = emptyList()

        override fun getItemCount() = entries.size

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): AppHolder {
            val binding = ItemAppBinding.inflate(LayoutInflater.from(parent.context), parent, false)
            return AppHolder(binding)
        }

        override fun onBindViewHolder(holder: AppHolder, position: Int) {
            holder.bind(entries[position])
        }
    }

    private inner class AppHolder(private val b: ItemAppBinding) : RecyclerView.ViewHolder(b.root) {
        fun bind(entry: Bypass.Entry) {
            val t = theme()
            Paint.apply(b.root, t)

            b.appName.text = entry.label
            b.appPackage.text = entry.pkg
            b.appInitial.text = entry.label.firstOrNull()?.uppercase().orEmpty()
            b.appInitial.setTextColor(t.acc)
            b.appInitial.background = Paint.rounded(t.accSoft, 10, dp)

            val on = entry.pkg in store.bypassed
            b.appSwitch.isChecked = on
            // Список остаётся и в выключенном режиме, но не применяется —
            // переключатели гаснут, чтобы это было видно.
            val enabled = store.bypassMode != Store.BYPASS_OFF
            b.root.isEnabled = enabled
            b.root.alpha = if (enabled) 1f else 0.5f

            b.root.setOnClickListener {
                val chosen = store.bypassed.toMutableSet()
                if (!chosen.remove(entry.pkg)) {
                    chosen.add(entry.pkg)
                }
                store.bypassed = chosen
                b.appSwitch.isChecked = entry.pkg in chosen
                afterAppsChanged()
            }
        }
    }

    // ---------------------------------------------------------------- логи

    private fun wireLogs() {
        ui.logAll.setOnClickListener { filterLogs(null) }
        ui.logInfo.setOnClickListener { filterLogs(Journal.Level.INFO) }
        ui.logWarn.setOnClickListener { filterLogs(Journal.Level.WARN) }
        ui.logErr.setOnClickListener { filterLogs(Journal.Level.ERROR) }

        ui.logsCopy.setOnClickListener {
            val text = visibleLogs().joinToString("\n") { it.time + "  " + it.text }
            if (text.isEmpty()) {
                Toast.makeText(host, R.string.logs_empty, Toast.LENGTH_SHORT).show()
                return@setOnClickListener
            }
            host.getSystemService(ClipboardManager::class.java)
                ?.setPrimaryClip(ClipData.newPlainText("marvia", text))
            Toast.makeText(host, R.string.logs_copied, Toast.LENGTH_SHORT).show()
        }

        // Одна кнопка «отправить» заменяет переписку «а что у тебя написано
        // на экране» — самую бесполезную часть любой поддержки. Уходит
        // отчёт целиком, с моделью телефона, а не отфильтрованный кусок.
        ui.logsSend.setOnClickListener {
            val send = Intent(Intent.ACTION_SEND).apply {
                type = "text/plain"
                putExtra(Intent.EXTRA_TEXT, Journal.report(host))
            }
            host.startActivity(Intent.createChooser(send, host.getString(R.string.journal_send)))
        }
    }

    private fun logsSub(): String {
        val n = Journal.entries().size
        return if (n == 0) host.getString(R.string.more_logs_sub_empty) else host.resources.getQuantityString(R.plurals.more_logs_sub, n, n)
    }

    private fun filterLogs(level: Journal.Level?) {
        logFilter = level
        renderLogs()
    }

    private fun visibleLogs(): List<Journal.Entry> {
        val all = Journal.entries()
        val level = logFilter ?: return all
        return all.filter { it.level == level }
    }

    /**
     * renderLogs собирает строки вьюхами, а не одним TextView: у каждой
     * строки своя метка уровня своим цветом, и предупреждение должно
     * читаться как предупреждение, а не как ещё одна серая строка.
     */
    private fun renderLogs() {
        val t = theme()
        pill(ui.logAll, t, logFilter == null)
        pill(ui.logInfo, t, logFilter == Journal.Level.INFO)
        pill(ui.logWarn, t, logFilter == Journal.Level.WARN)
        pill(ui.logErr, t, logFilter == Journal.Level.ERROR)

        val rows = ui.logRows
        rows.removeAllViews()
        val shown = visibleLogs()
        ui.logsCount.text = host.resources.getQuantityString(R.plurals.logs_count, shown.size, shown.size)
        ui.moreSub.text = logsSub()

        if (shown.isEmpty()) {
            rows.addView(
                TextView(host).apply {
                    setText(R.string.logs_empty)
                    setTextColor(t.dim)
                    textSize = 13f
                },
            )
            return
        }

        for ((i, e) in shown.withIndex()) {
            rows.addView(logRow(t, e), LinearLayout.LayoutParams(MATCH, WRAP).apply {
                if (i > 0) topMargin = (9 * dp).toInt()
            })
        }
        ui.logsScroll.post { ui.logsScroll.fullScroll(View.FOCUS_DOWN) }
    }

    private fun logRow(t: Theme, e: Journal.Entry): View {
        val row = LinearLayout(host).apply { orientation = LinearLayout.HORIZONTAL }

        row.addView(
            TextView(host).apply {
                text = e.time
                typeface = Typeface.MONOSPACE
                textSize = 10f
                setTextColor(t.dim)
            },
            LinearLayout.LayoutParams(WRAP, WRAP),
        )

        val (tag, color) = when (e.level) {
            Journal.Level.INFO -> "INF" to t.acc
            Journal.Level.WARN -> "WRN" to t.warn
            Journal.Level.ERROR -> "ERR" to t.fail
        }
        row.addView(
            TextView(host).apply {
                text = tag
                typeface = Typeface.MONOSPACE
                textSize = 9f
                gravity = Gravity.CENTER
                setTextColor(color)
                background = Paint.rounded(Look.withAlpha(color, 0.14), 4, dp)
                setPadding((5 * dp).toInt(), (1 * dp).toInt(), (5 * dp).toInt(), (1 * dp).toInt())
            },
            LinearLayout.LayoutParams(WRAP, WRAP).apply { marginStart = (8 * dp).toInt() },
        )

        row.addView(
            TextView(host).apply {
                text = e.text
                typeface = Typeface.MONOSPACE
                textSize = 11f
                setTextColor(t.fg)
            },
            LinearLayout.LayoutParams(0, WRAP, 1f).apply { marginStart = (8 * dp).toInt() },
        )
        return row
    }

    private companion object {
        const val MATCH = LinearLayout.LayoutParams.MATCH_PARENT
        const val WRAP = LinearLayout.LayoutParams.WRAP_CONTENT
    }
}
