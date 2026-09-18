package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint as Brush
import android.util.AttributeSet
import android.view.View

/**
 * Диаграммы статистики. Рисуются кодом, а не собираются из вьюх.
 *
 * В макете это сотня засечек циферблата, тридцать столбиков с точками и сто
 * шестьдесят восемь кружков разного размера. Каждая такая мелочь отдельной
 * вьюхой — это триста вьюх на экран, которые ещё и надо красить обходом
 * дерева. Холст рисует то же самое за один проход.
 *
 * Цвета приходят от темы через [paint]: у диаграмм нет своего представления
 * о том, какого цвета акцент.
 */

/** Столбики за тридцать дней: волосинка — день, точка — его вершина. */
class DaysChart @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var values: List<Long> = emptyList()
    private var weekend: List<Boolean> = emptyList()
    private var fg = 0
    private var dim = 0
    private var acc = 0
    private var line = 0

    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    fun show(values: List<Long>, weekend: List<Boolean>) {
        this.values = values
        this.weekend = weekend
        invalidate()
    }

    fun paint(t: Theme) {
        fg = t.fg
        dim = t.dim
        acc = t.acc
        line = t.line
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        if (values.isEmpty()) return

        val dp = resources.displayMetrics.density
        val top = 8 * dp
        val bottom = height.toFloat()
        val peak = values.max().coerceAtLeast(1)

        val step = width.toFloat() / values.size
        val stem = 1 * dp

        for (i in values.indices) {
            val share = values[i].toDouble() / peak
            val x = step * i + step / 2

            // Пустой день — одна точка у самого низа: столбик высотой в ноль
            // выглядит как пропуск в данных, а это не пропуск.
            val h = ((bottom - top) * share).toFloat()
            val dotY = bottom - h
            val big = share > 0.85
            val r = (if (big) 3.5f else 2.5f) * dp

            brush.color = line
            canvas.drawRect(x - stem / 2, dotY + r, x + stem / 2, bottom, brush)

            brush.color = if (big) acc else fg
            if (!big && weekend.getOrElse(i) { false }) {
                // Выходные полые: так в неделе видно ритм, а не только высоту.
                brush.style = Brush.Style.STROKE
                brush.strokeWidth = 1 * dp
            } else {
                brush.style = Brush.Style.FILL
            }
            canvas.drawCircle(x, dotY, r, brush)
            brush.style = Brush.Style.FILL
        }
    }
}

/**
 * Циферблат: сто засечек по кругу, каждая — процент расхода.
 *
 * Не круговая диаграмма: доли вроде «41 %» на ней читаются глазом плохо, а
 * сто засечек можно пересчитать. Каждая десятая длиннее — чтобы считать
 * десятками, а не по одной.
 */
class SharesDial @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var slices: List<Pair<Int, Int>> = emptyList()
    private var idle = 0

    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    /** slices — пары «сколько засечек · каким цветом», по порядку от двенадцати часов. */
    fun show(slices: List<Pair<Int, Int>>, idle: Int) {
        this.slices = slices
        this.idle = idle
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val radius = minOf(cx, cy) - 12 * dp

        brush.style = Brush.Style.FILL
        var tick = 0
        for ((count, color) in slices) {
            for (i in 0 until count) {
                drawTick(canvas, cx, cy, radius, tick, color, dp)
                tick++
            }
        }
        while (tick < 100) {
            drawTick(canvas, cx, cy, radius, tick, idle, dp)
            tick++
        }
    }

    private fun drawTick(canvas: Canvas, cx: Float, cy: Float, radius: Float, i: Int, color: Int, dp: Float) {
        val long = i % 10 == 0
        val len = (if (long) 11f else 7.5f) * dp
        val w = 1.5f * dp

        canvas.save()
        canvas.rotate(i * 3.6f, cx, cy)
        brush.color = color
        canvas.drawRoundRect(cx - w / 2, cy - radius, cx + w / 2, cy - radius + len, w / 2, w / 2, brush)
        canvas.restore()
    }
}

/** Неделя по часам: площадь точки — объём за этот час. */
class WeekHours @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var cells: List<Long> = emptyList()
    private var acc = 0
    private var mid = 0
    private var line = 0

    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    /** cells — 24 значения одной строки-дня. */
    fun show(cells: List<Long>, peak: Long) {
        this.cells = cells
        this.peak = peak.coerceAtLeast(1)
        invalidate()
    }

    private var peak = 1L

    fun paint(t: Theme) {
        acc = t.acc
        mid = t.mid
        line = t.line
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        if (cells.isEmpty()) return

        val dp = resources.displayMetrics.density
        val step = width.toFloat() / cells.size
        val cy = height / 2f

        brush.style = Brush.Style.FILL
        for (i in cells.indices) {
            val share = cells[i].toDouble() / peak
            val x = step * i + step / 2

            if (cells[i] == 0L) {
                brush.color = line
                canvas.drawCircle(x, cy, 1.5f * dp, brush)
                continue
            }

            // Площадь, а не радиус: глаз сравнивает пятна по площади, и
            // радиус, взятый напрямую, преувеличил бы разницу вчетверо.
            val r = (1.5f + 4.5f * Math.sqrt(share).toFloat()) * dp
            brush.color = if (share > 0.85) acc else Look.mix(acc, mid, 0.75 - share * 0.55)
            canvas.drawCircle(x, cy, r, brush)
        }
    }
}
