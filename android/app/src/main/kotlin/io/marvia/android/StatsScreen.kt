package io.marvia.android

import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import io.marvia.android.databinding.ScreenStatsBinding
import io.marvia.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.text.SimpleDateFormat
import java.util.Date

/**
 * StatsScreen — расход этого телефона: период, страны, неделя по часам.
 *
 * Всё считает сам телефон ([Traffic]), поэтому экран работает и без сети, и
 * без подписки, и показывает ровно то, что прошло через туннель здесь. Панель
 * этих чисел не знает и знать не будет: подённый расход каждого покупателя —
 * это история его жизни, а для выдачи доступа она не нужна.
 *
 * Период — неделя или тридцать дней. «Весь срок» из макета не показываем:
 * телефон помнит тридцать дней, и кнопка «всё» показывала бы те же тридцать
 * под другим именем.
 */
class StatsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenStatsBinding,
    private val theme: () -> Theme,
    private val traffic: Traffic,
    /** Рабочая подписка: срок и остаток идут в факты под кольцом. */
    private val store: Store,
) {

    private val dp = host.resources.displayMetrics.density
    private var week = false

    /** Срок и остаток рабочей подписки из кэша; пусто — не читали или нет. */
    private var quotaLimit = 0L
    private var quotaLeft = 0L
    private var quotaUntil = ""

    init {
        ui.rangeWeek.setOnClickListener { week = true; paint(theme()) }
        ui.rangeMonth.setOnClickListener { week = false; paint(theme()) }
    }

    /** open зовётся при каждом показе: за время на других экранах трафик шёл. */
    fun open() {
        paint(theme())
        val link = store.accountLink
        if (link.isEmpty()) return
        host.lifecycleScope.launch {
            val json = withContext(Dispatchers.IO) { runCatching { Mobile.subscription(link, store.cacheDir(), false) }.getOrDefault("") }
            if (json.isEmpty()) return@launch
            val o = runCatching { JSONObject(json) }.getOrNull() ?: return@launch
            quotaLimit = o.optLong("limit", 0)
            quotaLeft = o.optLong("left", 0)
            quotaUntil = o.optString("until", "")
            paintFacts(theme())
        }
    }

    fun paint(t: Theme) {
        val count = if (week) 7 else Traffic.KEEP_DAYS
        val days = traffic.lastDays(count)
        val values = days.map { it.bytes }
        val total = values.sum()
        val peak = values.maxOrNull() ?: 0L
        val peakBack = if (peak > 0) values.size - 1 - values.indexOf(peak) else -1

        // Период — таблетки: активная залита акцентом, остальные тихие.
        ui.rangeBox.background = Paint.rounded(t.surf, 999, dp)
        for ((v, on) in listOf(ui.rangeWeek to week, ui.rangeMonth to !week)) {
            v.background = if (on) Paint.rounded(t.acc, 999, dp) else null
            v.setTextColor(if (on) t.accFg else t.dim)
        }

        ui.rangeDial.paint(t)
        ui.rangeDial.show(values)
        ui.dialLabel.setText(if (week) R.string.stats_dial_week else R.string.stats_dial_month)
        ui.dialTotal.text = Format.size(host, total)
        ui.dialPerDay.text = host.getString(R.string.stats_per_day, Format.size(host, total / count))

        ui.factPeak.text = Format.size(host, peak)
        ui.factPeakWhen.text = host.getString(R.string.stats_peak, ago(peakBack))
        ui.factDays.text = count.toString()
        paintFacts(t)

        ui.trendTitle.setText(if (week) R.string.stats_range_title_week else R.string.stats_range_title_month)
        ui.trendPeak.text = if (peak > 0) host.getString(R.string.stats_peak_line, ago(peakBack), Format.size(host, peak)) else host.getString(R.string.stats_no_traffic)
        ui.trendTotal.text = Format.size(host, total)
        ui.trendLine.paint(t)
        ui.trendLine.show(values)
        ui.daysFrom.text = label(days.first().day)
        ui.daysMid.text = label(days[days.size / 2].day)

        paintPlaces(t)
        paintWeek(t)
    }

    /** Факты под кольцом: остаток подписки — из кэша, без похода в панель. */
    private fun paintFacts(t: Theme) {
        val days = if (quotaUntil.isEmpty()) null else Format.daysLeft(quotaUntil)
        val until = if (quotaUntil.isEmpty()) "" else Format.day(host, quotaUntil)
        if (quotaLimit > 0) {
            ui.factQuota.text = compactQuota(quotaLeft, quotaLimit)
            ui.factQuotaNote.text = if (days != null) host.getString(R.string.stats_quota_left_days, host.resources.getQuantityString(R.plurals.days_left, days, days))
            else host.getString(R.string.stats_quota_left)
        } else {
            ui.factQuota.text = host.getString(R.string.servers_unlimited)
            ui.factQuotaNote.text = if (until.isNotEmpty()) host.getString(R.string.stats_unlimited_until, until) else host.getString(R.string.stats_unlimited)
        }
    }

    /**
     * paintPlaces — страны: одна полоса долями и список с флагами. Проценты
     * округляем вниз и остаток отдаём самой большой: иначе сумма на экране
     * не сходится в сто, и человек это замечает.
     */
    private fun paintPlaces(t: Theme) {
        val places = traffic.places().take(4)
        val sum = places.sumOf { it.bytes }
        ui.placeList.removeAllViews()
        ui.placesCard.isVisible = sum > 0
        if (sum <= 0) return

        val shares = places.map { ((it.bytes * 100) / sum).toInt() }.toMutableList()
        shares[0] += 100 - shares.sum()
        val colors = listOf(t.acc, Look.mix(t.acc, t.mid, 0.35), Look.mix(t.acc, t.mid, 0.6), t.dim)
        ui.placesBar.show(places.mapIndexed { i, p -> (p.bytes.toFloat() / sum) to colors[i % colors.size] }, t.line)

        for ((i, place) in places.withIndex()) {
            val row = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
                setPadding(0, (6 * dp).toInt(), 0, (6 * dp).toInt())
            }
            val dot = TextView(host).apply { background = Paint.rounded(colors[i % colors.size], 2, dp) }
            row.addView(dot, LinearLayout.LayoutParams((8 * dp).toInt(), (8 * dp).toInt()))
            val flag = TextView(host).apply { text = Flags.of(place.name.substringBefore('·').trim()); textSize = 15f; setPadding((10 * dp).toInt(), 0, 0, 0) }
            row.addView(flag)
            val title = TextView(host).apply {
                text = place.name.substringBefore('·').trim()
                textSize = 13f
                setTextColor(t.fg)
                maxLines = 1
                setPadding((9 * dp).toInt(), 0, (9 * dp).toInt(), 0)
            }
            row.addView(title, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
            val share = TextView(host).apply {
                text = host.getString(R.string.stats_place_share, shares[i], Format.size(host, place.bytes))
                textSize = 12f
                typeface = androidx.core.content.res.ResourcesCompat.getFont(host, R.font.mono)
                setTextColor(t.dim)
            }
            row.addView(share)
            ui.placeList.addView(row)
        }
    }

    /**
     * paintWeek рисует семь строк по 24 часа.
     *
     * Вершина общая на все дни, а не своя у каждого: со своей вершиной тихое
     * воскресенье выглядело бы таким же плотным, как рабочий вторник.
     */
    private fun paintWeek(t: Theme) {
        val week = traffic.week()
        var peak = 0L
        var peakDay = -1
        var peakHour = -1
        for ((d, row) in week.withIndex()) for ((h, v) in row.withIndex()) if (v > peak) { peak = v; peakDay = d; peakHour = h }
        val names = host.resources.getStringArray(R.array.weekdays_short)
        ui.weekPeak.text = if (peak > 0) host.getString(R.string.stats_week_peak, names[peakDay].lowercase(), peakHour) else ""

        ui.weekRows.removeAllViews()
        for ((i, row) in week.withIndex()) {
            val line = LinearLayout(host).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL }
            val day = TextView(host).apply {
                text = names.getOrElse(i) { "" }
                textSize = 9.5f
                letterSpacing = 0.1f
                typeface = androidx.core.content.res.ResourcesCompat.getFont(host, R.font.mono)
                setTextColor(t.dim)
            }
            line.addView(day, LinearLayout.LayoutParams((30 * dp).toInt(), LinearLayout.LayoutParams.WRAP_CONTENT))
            val cells = WeekHours(host).apply {
                paint(t)
                show(row.toList(), peak, if (i == peakDay) peakHour else -1)
            }
            line.addView(cells, LinearLayout.LayoutParams(0, (13 * dp).toInt(), 1f))
            val lp = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT)
            lp.topMargin = (4 * dp).toInt()
            ui.weekRows.addView(line, lp)
        }
    }

    /**
     * compactQuota — «41 из 100 ГБ»: единица один раз, без десятых. Две полные
     * величины в узкую колонку не влезают, а до десятых остаток здесь незачем.
     */
    private fun compactQuota(left: Long, limit: Long): String {
        val gb = 1L shl 30
        // Предел — тоже целыми: Format.size даёт «100,0 ГБ», и строка
        // «41 из 100,0 ГБ» переносилась в узкой колонке на две.
        return if (limit >= gb) host.getString(R.string.stats_quota, (left / gb).toString(), host.getString(R.string.size_gb, (limit / gb).toString()))
        else host.getString(R.string.stats_quota, Format.size(host, left), Format.size(host, limit))
    }

    /** ago — «сегодня» или «N дн. назад». */
    private fun ago(back: Int): String = when {
        back <= 0 -> host.getString(R.string.stats_today_low)
        else -> host.getString(R.string.stats_days_ago, back)
    }

    /** label — «20 АВГ» под графиком: число и месяц, без года. */
    private fun label(day: Long): String {
        val at = day * 86_400_000L - java.util.TimeZone.getDefault().rawOffset
        val locale = host.resources.configuration.locales[0]
        return SimpleDateFormat("d MMM", locale).format(Date(at)).uppercase(locale).replace(".", "")
    }
}
