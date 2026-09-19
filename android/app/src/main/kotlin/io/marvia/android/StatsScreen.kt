package io.marvia.android

import android.content.res.ColorStateList
import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenStatsBinding
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * StatsScreen — расход этого телефона: кольцо и итог, линия по дням, доли
 * стран, неделя по часам.
 *
 * Всё считает сам телефон ([Traffic]), поэтому экран работает и без сети, и
 * без подписки, и показывает ровно то, что прошло через туннель здесь. Панель
 * этих чисел не знает и знать не будет: подённый расход каждого покупателя —
 * это история его жизни, а для выдачи доступа она не нужна.
 *
 * Периода два — неделя и тридцать дней. «Весь срок» из макета не перенесён:
 * телефон помнит тридцать суток, и «всё» было бы теми же тридцатью днями под
 * другим именем.
 */
class StatsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenStatsBinding,
    private val theme: () -> Theme,
    private val traffic: Traffic,
) {

    private val dp = host.resources.displayMetrics.density

    /** Сколько дней показываем: 7 или 30. Живёт, пока открыт экран. */
    private var days = Traffic.KEEP_DAYS

    init {
        ui.rangeWeek.setOnClickListener { days = 7; paint(theme()) }
        ui.rangeMonth.setOnClickListener { days = Traffic.KEEP_DAYS; paint(theme()) }
    }

    /** open зовётся при каждом показе: за время на других экранах трафик шёл. */
    fun open() {
        paint(theme())
    }

    fun paint(t: Theme) {
        val rows = traffic.lastDays(days)
        val values = rows.map { it.bytes }
        val total = values.sum()
        val peak = values.max()
        val peakAt = values.indexOf(peak)
        val ago = values.size - 1 - peakAt

        paintRange(t)

        ui.dial.paint(t)
        ui.dial.show(values)
        ui.dialLabel.text = host.getString(
            if (days == 7) R.string.stats_dial_week else R.string.stats_dial_month,
        ).uppercase()
        ui.dialTotal.text = Format.size(host, total)
        ui.dialAverage.text = host.getString(R.string.stats_average, Format.size(host, total / values.size))

        ui.peakValue.text = Format.size(host, peak)
        ui.peakWhen.text = host.getString(R.string.stats_peak, when_(ago))
        paintLimit()
        ui.daysValue.text = values.size.toString()

        ui.lineTitle.setText(if (days == 7) R.string.stats_line_week else R.string.stats_line_month)
        ui.lineNote.text = host.getString(R.string.stats_line_note, when_(ago), Format.size(host, peak))
        ui.lineTotal.text = Format.size(host, total)
        ui.line.paint(t)
        ui.line.show(values)
        paintAxis(t, rows)

        paintPlaces(t)
        paintWeek(t)
    }

    /** when_ — «сегодня» или «N дн. назад»: подпись к пику. */
    private fun when_(ago: Int): String =
        if (ago == 0) host.getString(R.string.stats_today_word) else host.getString(R.string.stats_days_ago, ago)

    private fun paintRange(t: Theme) {
        ui.rangeBox.backgroundTintList = ColorStateList.valueOf(t.bg)
        for ((view, on) in listOf(ui.rangeWeek to (days == 7), ui.rangeMonth to (days != 7))) {
            view.backgroundTintList = ColorStateList.valueOf(if (on) t.acc else 0)
            view.setTextColor(if (on) t.accFg else t.dim)
        }
    }

    /**
     * paintLimit — средняя ячейка: лимит подписки. Известен только внутри
     * поднятого туннеля — подписка приходит при подключении. Иначе ячейка
     * прячется: прочерк на её месте выглядел бы как «лимита нет».
     */
    private fun paintLimit() {
        val sub = (MarviaState.state.value as? TunnelState.On)?.subscription
        val show = sub != null && sub.known
        ui.limitCell.isVisible = show
        ui.limitRule.isVisible = show
        if (!show || sub == null) return

        val until = if (sub.until.isEmpty()) "" else Format.day(host, sub.until)
        if (sub.limitBytes > 0) {
            ui.limitValue.text = host.getString(R.string.traffic_of, Format.size(host, sub.leftBytes), Format.size(host, sub.limitBytes))
            val left = Format.daysLeft(sub.until)
            ui.limitWhen.text = if (left == null) host.getString(R.string.stats_left_word) else host.getString(R.string.stats_left_days, left)
        } else {
            ui.limitValue.text = "∞"
            ui.limitWhen.text = if (until.isEmpty()) host.getString(R.string.stats_no_limit) else host.getString(R.string.stats_no_limit_until, until)
        }
    }

    /** paintAxis — четыре даты под линией: начало, две трети и «сегодня». */
    private fun paintAxis(t: Theme, rows: List<Traffic.DayTotal>) {
        ui.lineAxis.removeAllViews()
        val marks = if (rows.size >= 4) listOf(0, rows.size / 3, rows.size * 2 / 3) else listOf(0)
        val labels = marks.map { label(rows[it].day) } + host.getString(R.string.stats_today_word).uppercase()
        for ((i, text) in labels.withIndex()) {
            val v = TextView(host).apply {
                this.text = text
                textSize = 10f
                letterSpacing = 0.08f
                typeface = Fonts.mono(host)
                setTextColor(t.dim)
                gravity = when (i) {
                    0 -> Gravity.START
                    labels.size - 1 -> Gravity.END
                    else -> Gravity.CENTER
                }
            }
            ui.lineAxis.addView(v, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        }
    }

    /**
     * paintPlaces раскладывает полосу по странам, до трёх крупнейших.
     *
     * Округляем вниз и отдаём остаток самой большой: иначе сумма процентов
     * на экране не сходится в сто, и человек это замечает.
     */
    private fun paintPlaces(t: Theme) {
        val places = traffic.places().take(3)
        val sum = places.sumOf { it.bytes }
        ui.placeList.removeAllViews()
        ui.sharesCard.isVisible = sum > 0
        if (sum <= 0) return

        val shares = places.map { ((it.bytes * 100) / sum).toInt() }.toMutableList()
        shares[0] += 100 - shares.sum()
        val colors = listOf(t.fg, Look.mix(t.fg, t.mid, 0.45), Look.mix(t.fg, t.mid, 0.72))
        ui.shares.show(shares.mapIndexed { i, pct -> pct / 100f to colors[i] })

        for ((i, place) in places.withIndex()) {
            val column = LinearLayout(host).apply { orientation = LinearLayout.VERTICAL }

            val head = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
            }
            head.addView(
                TextView(host).apply { background = Paint.circle(colors[i]) },
                LinearLayout.LayoutParams((8 * dp).toInt(), (8 * dp).toInt()),
            )
            val flag = Flags.of(place.name)
            if (flag.isNotEmpty()) {
                head.addView(
                    TextView(host).apply { text = flag; textSize = 14f; setPadding((6 * dp).toInt(), 0, 0, 0) },
                )
            }
            column.addView(head)

            column.addView(
                TextView(host).apply {
                    text = place.name
                    textSize = 13f
                    typeface = Fonts.textBold(host)
                    maxLines = 1
                    setTextColor(t.fg)
                    setPadding(0, (6 * dp).toInt(), (6 * dp).toInt(), 0)
                },
            )
            column.addView(
                TextView(host).apply {
                    text = host.getString(R.string.stats_share, shares[i], Format.size(host, place.bytes))
                    textSize = 12f
                    typeface = Fonts.mono(host)
                    maxLines = 1
                    setTextColor(t.dim)
                    setPadding(0, (4 * dp).toInt(), 0, 0)
                },
            )
            ui.placeList.addView(column, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        }
    }

    /**
     * paintWeek — семь строк по 24 часа и ось.
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

        ui.weekPeak.text = if (peak > 0) {
            host.getString(R.string.stats_week_peak, names.getOrElse(peakDay) { "" }.lowercase(), peakHour)
        } else ""

        ui.weekRows.removeAllViews()
        for ((i, row) in week.withIndex()) {
            val line = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
            }
            line.addView(
                TextView(host).apply {
                    text = names.getOrElse(i) { "" }.lowercase()
                    textSize = 10f
                    typeface = Fonts.mono(host)
                    setTextColor(t.dim)
                },
                LinearLayout.LayoutParams((28 * dp).toInt(), LinearLayout.LayoutParams.WRAP_CONTENT),
            )
            val cells = WeekHours(host).apply {
                paint(t)
                show(row.toList(), peak, if (i == peakDay) peakHour else -1)
            }
            line.addView(cells, LinearLayout.LayoutParams(0, (13 * dp).toInt(), 1f))
            val lp = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT)
            if (i > 0) lp.topMargin = (4 * dp).toInt()
            ui.weekRows.addView(line, lp)
        }

        // Ось часов под сеткой, с отступом под подписи дней.
        // baselineAligned выключен: с ним пустая распорка тянет подписи вниз,
        // и строка обрезает их по половине.
        val axis = LinearLayout(host).apply { orientation = LinearLayout.HORIZONTAL; isBaselineAligned = false }
        axis.addView(android.view.View(host), LinearLayout.LayoutParams((28 * dp).toInt(), 1))
        val marks = listOf("0", "6", "12", "18", "23")
        for ((i, m) in marks.withIndex()) {
            axis.addView(
                TextView(host).apply {
                    text = m
                    textSize = 10f
                    typeface = Fonts.mono(host)
                    setTextColor(t.dim)
                    gravity = when (i) { 0 -> Gravity.START; marks.size - 1 -> Gravity.END; else -> Gravity.CENTER }
                },
                LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f),
            )
        }
        val lp = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT)
        lp.topMargin = (6 * dp).toInt()
        ui.weekRows.addView(axis, lp)
    }

    /** label — «20 АВГ» под графиком: число и месяц, без года. */
    private fun label(day: Long): String {
        val at = day * 86_400_000L - java.util.TimeZone.getDefault().rawOffset
        val locale = host.resources.configuration.locales[0]
        return SimpleDateFormat("d MMM", locale).format(Date(at)).uppercase(locale).replace(".", "")
    }
}
