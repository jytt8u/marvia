package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.DashPathEffect
import android.graphics.LinearGradient
import android.graphics.Paint as Brush
import android.graphics.Path
import android.graphics.RectF
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View

/**
 * Диаграммы расхода из макета. Рисуются кодом, а не собираются из вьюх:
 * кольцо из тридцати сегментов, плавная линия и сетка в сто шестьдесят
 * восемь клеток отдельными вьюхами — это сотни вьюх на экран. Холст рисует
 * то же за один проход.
 *
 * Цвета приходят от темы через [paint]: у диаграмм нет своего представления
 * о том, какого цвета акцент.
 */

/**
 * UsageDial — кольцо из сегментов, по одному на день периода.
 *
 * Форма кольца не меняется — ни иголок, ни клякс; читается яркость: чем
 * больше трафика за день, тем светлее сегмент. Сегодняшний — белый и чуть
 * толще. Внутри кольца — тонкая окружность, за ней место под числа.
 */
class UsageDial @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var values: List<Long> = emptyList()
    private var acc = 0
    private var fg = 0
    private var line = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)
    private val box = RectF()

    fun show(values: List<Long>) {
        this.values = values
        invalidate()
    }

    fun paint(t: Theme) {
        acc = t.acc
        fg = t.fg
        line = t.line
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val r = minOf(cx, cy) - 22 * dp

        brush.style = Brush.Style.STROKE
        brush.strokeWidth = 1 * dp
        brush.color = line
        canvas.drawCircle(cx, cy, r - 12 * dp, brush)

        val n = values.size
        if (n == 0) return
        val peak = values.max().coerceAtLeast(1)
        val gap = if (n > 12) 1.6f else 4f
        val step = 240f / n
        brush.strokeCap = Brush.Cap.BUTT
        box.set(cx - r, cy - r, cx + r, cy + r)

        for ((i, v) in values.withIndex()) {
            // Дуга от −120° до +120° по часовой; в Android ноль — на три часа.
            val start = -210f + step * i + gap / 2
            val sweep = step - gap
            val today = i == n - 1
            val level = minOf(4, (5.0 * v / (peak + 0.001)).toInt())
            brush.strokeWidth = (if (today) 15f else 13f) * dp
            brush.color = if (today) fg else Look.withAlpha(acc, LEVELS[level])
            canvas.drawArc(box, start, sweep, false, brush)
        }
    }

    private companion object {
        val LEVELS = doubleArrayOf(0.14, 0.30, 0.48, 0.68, 0.90)
    }
}

/**
 * UsageLine — плавная линия по дням с заливкой под ней.
 *
 * Касательные по соседям (Catmull-Rom в кубические Безье): ломаная из
 * тридцати точек читается как шум, кривая — как форма. Точка на конце —
 * сегодня, точка с обводкой — пик.
 */
class UsageLine @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var values: List<Long> = emptyList()
    private var acc = 0
    private var fg = 0
    private var line = 0
    private var surf = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)
    private val path = Path()
    private val area = Path()

    fun show(values: List<Long>) {
        this.values = values
        invalidate()
    }

    fun paint(t: Theme) {
        acc = t.acc
        fg = t.fg
        line = t.line
        surf = t.surf
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val w = width.toFloat()
        val h = height.toFloat()
        val top = 12 * dp
        val bottom = h - 12 * dp

        // Три пунктирные линии сетки, как в макете.
        brush.style = Brush.Style.STROKE
        brush.strokeWidth = 1 * dp
        brush.color = line
        brush.pathEffect = DashPathEffect(floatArrayOf(2 * dp, 4 * dp), 0f)
        for (k in 1..3) {
            val y = h * k / 4
            canvas.drawLine(0f, y, w, y, brush)
        }
        brush.pathEffect = null

        if (values.size < 2) return
        val peak = values.max().coerceAtLeast(1)
        val pts = values.mapIndexed { i, v ->
            floatArrayOf(i * w / (values.size - 1), bottom - (bottom - top) * v / peak)
        }

        path.reset()
        path.moveTo(pts[0][0], pts[0][1])
        for (i in 0 until pts.size - 1) {
            val p0 = pts.getOrElse(i - 1) { pts[i] }
            val p1 = pts[i]
            val p2 = pts[i + 1]
            val p3 = pts.getOrElse(i + 2) { p2 }
            path.cubicTo(
                p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6,
                p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6,
                p2[0], p2[1],
            )
        }

        area.set(path)
        area.lineTo(w, h)
        area.lineTo(0f, h)
        area.close()
        brush.style = Brush.Style.FILL
        brush.shader = LinearGradient(
            0f, 0f, 0f, h,
            Look.withAlpha(acc, 0.45), acc and 0xFFFFFF, Shader.TileMode.CLAMP,
        )
        canvas.drawPath(area, brush)
        brush.shader = null

        brush.style = Brush.Style.STROKE
        brush.strokeWidth = 2.2f * dp
        brush.strokeCap = Brush.Cap.ROUND
        brush.strokeJoin = Brush.Join.ROUND
        brush.color = fg
        canvas.drawPath(path, brush)

        val last = pts.last()
        brush.style = Brush.Style.FILL
        brush.color = fg
        canvas.drawCircle(last[0], last[1], 5 * dp, brush)
        brush.style = Brush.Style.STROKE
        brush.strokeWidth = 3 * dp
        brush.color = surf
        canvas.drawCircle(last[0], last[1], 5 * dp, brush)

        val peakAt = values.indexOf(values.max())
        if (peakAt != values.size - 1) {
            val p = pts[peakAt]
            brush.style = Brush.Style.FILL
            brush.color = surf
            canvas.drawCircle(p[0], p[1], 3.5f * dp, brush)
            brush.style = Brush.Style.STROKE
            brush.strokeWidth = 2 * dp
            brush.color = acc
            canvas.drawCircle(p[0], p[1], 3.5f * dp, brush)
        }
    }
}

/** UsageShares — полоса долей стран: сегменты через зазор, скруглённая. */
class UsageShares @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var slices: List<Pair<Float, Int>> = emptyList()
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)
    private val clip = Path()

    /** slices — пары «доля от единицы · цвет», по порядку слева направо. */
    fun show(slices: List<Pair<Float, Int>>) {
        this.slices = slices
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val w = width.toFloat()
        val h = height.toFloat()
        val gap = 3 * dp
        clip.reset()
        clip.addRoundRect(0f, 0f, w, h, h / 2, h / 2, Path.Direction.CW)
        canvas.save()
        canvas.clipPath(clip)
        var x = 0f
        val usable = w - gap * (slices.size - 1).coerceAtLeast(0)
        for ((share, color) in slices) {
            val sw = usable * share
            brush.color = color
            canvas.drawRect(x, 0f, x + sw, h, brush)
            x += sw + gap
        }
        canvas.restore()
    }
}

/**
 * WeekHours — одна строка недели: 24 клетки, яркость — объём за час.
 *
 * Пик недели — белая клетка со свечением; про него сказано и словами над
 * сеткой, но глазу нужна точка, к которой эти слова относятся.
 */
class WeekHours @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private var cells: List<Long> = emptyList()
    private var peak = 1L
    private var peakAt = -1
    private var acc = 0
    private var fg = 0
    private var line = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    /** cells — 24 значения; peak — вершина всей недели; peakAt — час пика в этой строке или −1. */
    fun show(cells: List<Long>, peak: Long, peakAt: Int) {
        this.cells = cells
        this.peak = peak.coerceAtLeast(1)
        this.peakAt = peakAt
        invalidate()
    }

    fun paint(t: Theme) {
        acc = t.acc
        fg = t.fg
        line = t.line
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        if (cells.isEmpty()) return
        val dp = resources.displayMetrics.density
        val gap = 3 * dp
        val cw = (width - gap * (cells.size - 1)) / cells.size
        val h = height.toFloat()
        val r = 4 * dp

        for (i in cells.indices) {
            val x = i * (cw + gap)
            val share = cells[i].toDouble() / peak
            if (i == peakAt) {
                brush.color = Look.withAlpha(fg, 0.35)
                canvas.drawRoundRect(x - 2 * dp, -2 * dp, x + cw + 2 * dp, h + 2 * dp, r, r, brush)
                brush.color = fg
            } else {
                brush.color = if (cells[i] == 0L) Look.withAlpha(line, 0.8) else Look.withAlpha(acc, 0.12 + 0.88 * share)
            }
            canvas.drawRoundRect(x, 0f, x + cw, h, r, r, brush)
        }
    }
}
