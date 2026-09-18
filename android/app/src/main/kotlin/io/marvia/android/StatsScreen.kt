package io.marvia.android

import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import io.marvia.android.databinding.ScreenStatsBinding
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * StatsScreen — расход этого телефона: тридцать дней, страны, неделя по часам.
 *
 * Всё считает сам телефон ([Traffic]), поэтому экран работает и без сети, и
 * без подписки, и показывает ровно то, что прошло через туннель здесь. Панель
 * этих чисел не знает и знать не будет: подённый расход каждого покупателя —
 * это история его жизни, а для выдачи доступа она не нужна.
 */
class StatsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenStatsBinding,
    private val theme: () -> Theme,
    private val traffic: Traffic,
) {

    private val dp = host.resources.displayMetrics.density

    /** open зовётся при каждом показе: за время на других экранах трафик шёл. */
    fun open() {
        paint(theme())
    }

    fun paint(t: Theme) {
        val days = traffic.lastDays(Traffic.KEEP_DAYS)
        val total = days.sumOf { it.bytes }

        ui.statsTotal.text = host.getString(
            R.string.stats_total,
            Traffic.KEEP_DAYS,
            Format.size(host, total),
        )

        ui.daysChart.paint(t)
        ui.daysChart.show(
            days.map { it.bytes },
            days.map { Traffic.weekdayOf(it.day) >= 5 },
        )
        ui.daysFrom.text = label(days.first().day)
        ui.daysMid.text = label(days[days.size / 2].day)
        ui.daysTo.text = label(days.last().day)

        paintPlaces(t, total)
        paintWeek(t)
    }

    /**
     * paintPlaces раскладывает сто засечек по странам.
     *
     * Округляем вниз и отдаём остаток самой большой стране: иначе сумма
     * процентов на экране не сходится в сто, и человек это замечает.
     */
    private fun paintPlaces(t: Theme, total: Long) {
        val places = traffic.places().take(4)
        val sum = places.sumOf { it.bytes }
        ui.placeList.removeAllViews()

        if (sum <= 0) {
            ui.sharesDial.show(emptyList(), t.line)
            ui.dialNumber.setTextColor(t.dim)
            return
        }
        ui.dialNumber.setTextColor(t.fg)

        val shares = places.map { ((it.bytes * 100) / sum).toInt() }.toMutableList()
        val slack = 100 - shares.sum()
        if (shares.isNotEmpty()) shares[0] += slack

        val colors = listOf(
            t.acc,
            Look.mix(t.acc, t.mid, 0.35),
            Look.mix(t.acc, t.mid, 0.6),
            t.dim,
        )
        ui.sharesDial.show(shares.mapIndexed { i, pct -> pct to colors[i % colors.size] }, t.line)

        for ((i, place) in places.withIndex()) {
            ui.placeList.addView(placeRow(place.name, shares[i], colors[i % colors.size], t))
        }
    }

    private fun placeRow(name: String, pct: Int, color: Int, t: Theme): LinearLayout {
        val row = LinearLayout(host).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, (5 * dp).toInt(), 0, (5 * dp).toInt())
        }

        val dot = TextView(host).apply {
            background = Paint.rounded(color, 2, dp)
        }
        row.addView(dot, LinearLayout.LayoutParams((8 * dp).toInt(), (8 * dp).toInt()))

        val title = TextView(host).apply {
            text = name
            textSize = 13f
            setTextColor(t.fg)
            maxLines = 1
            setPadding((9 * dp).toInt(), 0, (9 * dp).toInt(), 0)
        }
        row.addView(
            title,
            LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f),
        )

        val share = TextView(host).apply {
            text = host.getString(R.string.stats_percent, pct)
            textSize = 12f
            setTextColor(t.dim)
        }
        row.addView(share)
        return row
    }

    /**
     * paintWeek рисует семь строк по 24 часа.
     *
     * Вершина общая на все дни, а не своя у каждого: со своей вершиной тихое
     * воскресенье выглядело бы таким же плотным, как рабочий вторник.
     */
    private fun paintWeek(t: Theme) {
        val week = traffic.week()
        val peak = week.maxOfOrNull { row -> row.maxOrNull() ?: 0 } ?: 0
        val names = host.resources.getStringArray(R.array.weekdays_short)

        ui.weekRows.removeAllViews()
        for ((i, row) in week.withIndex()) {
            val line = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
            }

            val day = TextView(host).apply {
                text = names.getOrElse(i) { "" }
                textSize = 9f
                letterSpacing = 0.1f
                setTextColor(t.dim)
            }
            line.addView(day, LinearLayout.LayoutParams((30 * dp).toInt(), LinearLayout.LayoutParams.WRAP_CONTENT))

            val cells = WeekHours(host).apply {
                paint(t)
                show(row.toList(), peak)
            }
            line.addView(
                cells,
                LinearLayout.LayoutParams(0, (18 * dp).toInt(), 1f),
            )

            val lp = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT,
            )
            lp.topMargin = (5 * dp).toInt()
            ui.weekRows.addView(line, lp)
        }
    }

    /** label — «05 АВГ» под графиком: число и месяц, без года. */
    private fun label(day: Long): String {
        val at = day * 86_400_000L - java.util.TimeZone.getDefault().rawOffset
        val locale = host.resources.configuration.locales[0]
        return SimpleDateFormat("dd MMM", locale).format(Date(at)).uppercase(locale)
    }
}
