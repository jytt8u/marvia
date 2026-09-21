package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint as Brush
import android.graphics.Path
import android.graphics.RectF
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View
import android.view.animation.DecelerateInterpolator
import androidx.core.graphics.ColorUtils
import kotlin.math.max
import kotlin.math.min

/**
 * Диаграммы расхода. Рисуются кодом, а не собираются из вьюх.
 *
 * В макете это кольцо из тридцати сегментов, плавная линия по дням и сто
 * шестьдесят восемь клеток недели. Каждая такая мелочь отдельной вьюхой —
 * это сотни вьюх на экран, которые ещё и надо красить обходом дерева. Холст
 * рисует то же самое за один проход.
 *
 * Цвета приходят от темы через [paint]: у диаграмм нет своего представления
 * о том, какого цвета акцент.
 */

/**
 * Кольцо периода: по сегменту на день, чем больше трафика — тем светлее.
 *
 * Форма кольца не меняется: ни иголок, ни клякс, читается только яркость.
 * Сегодняшний сегмент — цветом текста, чтобы видеть, где конец периода.
 * Раскрыто на 240°, разрыв внизу — как на референсе.
 */
class RangeDial @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var values: List<Long> = emptyList()
    private var acc = 0
    private var fg = 0
    private var line = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG).apply { style = Brush.Style.STROKE; strokeCap = Brush.Cap.BUTT }
    private val reveal = Reveal(this)

    fun show(values: List<Long>) { val changed = values != this.values; this.values = values; if (changed) reveal.start() else invalidate() }
    fun paint(t: Theme) { acc = t.acc; fg = t.fg; line = t.line; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val r = min(cx, cy) - 14 * dp
        val box = RectF(cx - r, cy - r, cx + r, cy + r)

        // Тонкое внутреннее кольцо — ободок шкалы, как в макете.
        brush.strokeWidth = 1 * dp
        brush.color = line
        canvas.drawCircle(cx, cy, r - 12 * dp, brush)

        val n = values.size
        if (n == 0) return
        val peak = (values.maxOrNull() ?: 0L).coerceAtLeast(1)
        val gap = if (n > 12) 1.6f else 4f
        val step = 240f / n
        brush.strokeWidth = 14 * dp
        // Сегменты зажигаются по кругу, от первого дня к сегодняшнему.
        val lit = reveal.value * n
        for (i in 0 until n) {
            if (i > lit) break
            val fade = (lit - i).coerceIn(0f, 1f)
            val share = values[i].toFloat() / peak
            val last = i == n - 1
            val color = when {
                last -> fg
                values[i] == 0L -> ColorUtils.setAlphaComponent(acc, 26)
                else -> ColorUtils.setAlphaComponent(acc, (60 + 195 * share).toInt().coerceIn(40, 255))
            }
            brush.color = ColorUtils.setAlphaComponent(color, (android.graphics.Color.alpha(color) * fade).toInt())
            // Ноль градусов у Android — три часа; −120° от двенадцати = −210°.
            val start = -210f + step * i + gap / 2
            canvas.drawArc(box, start, step - gap, false, brush)
        }
    }
}

/**
 * Линия по дням: плавная кривая через точки, под ней — тающая заливка,
 * последняя точка кружком, пик — кружком с обводкой. Касательные по соседям
 * (Catmull-Rom → Безье), как в макете; острые изломы дневного расхода
 * глазу читаются хуже, чем волна.
 */
class TrendLine @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var values: List<Long> = emptyList()
    private var acc = 0
    private var fg = 0
    private var surf = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)
    private val path = Path()
    private val area = Path()
    private val reveal = Reveal(this)

    fun show(values: List<Long>) { val changed = values != this.values; this.values = values; if (changed) reveal.start() else invalidate() }
    fun paint(t: Theme) { acc = t.acc; fg = t.fg; surf = t.surf; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val n = values.size
        if (n < 2) return
        val dp = resources.displayMetrics.density
        val w = width.toFloat()
        val h = height.toFloat()
        val top = 12 * dp
        val bottom = h - 12 * dp
        val peak = (values.maxOrNull() ?: 0L).coerceAtLeast(1)
        // Крайние точки не у самого края: у последней кружок, ему нужно место.
        val inset = 8 * dp
        val xs = FloatArray(n) { inset + it * (w - 2 * inset) / (n - 1) }
        val ys = FloatArray(n) { bottom - (bottom - top) * values[it] / peak }

        path.reset()
        path.moveTo(xs[0], ys[0])
        for (i in 0 until n - 1) {
            val p0 = max(i - 1, 0); val p3 = min(i + 2, n - 1)
            val c1x = xs[i] + (xs[i + 1] - xs[p0]) / 6; val c1y = ys[i] + (ys[i + 1] - ys[p0]) / 6
            val c2x = xs[i + 1] - (xs[p3] - xs[i]) / 6; val c2y = ys[i + 1] - (ys[p3] - ys[i]) / 6
            path.cubicTo(c1x, c1y, c2x, c2y, xs[i + 1], ys[i + 1])
        }
        area.set(path)
        area.lineTo(xs[n - 1], h); area.lineTo(xs[0], h); area.close()

        // Линия прочерчивается слева направо: срез холста едет по ширине.
        canvas.save()
        canvas.clipRect(0f, 0f, inset + (w - inset) * reveal.value, h)

        brush.style = Brush.Style.FILL
        brush.shader = LinearGradient(0f, 0f, 0f, h, ColorUtils.setAlphaComponent(acc, 115), 0, Shader.TileMode.CLAMP)
        canvas.drawPath(area, brush)
        brush.shader = null

        brush.style = Brush.Style.STROKE
        brush.strokeWidth = 2.5f * dp
        brush.strokeCap = Brush.Cap.ROUND
        brush.strokeJoin = Brush.Join.ROUND
        brush.color = acc
        canvas.drawPath(path, brush)

        // Пик: маленький кружок с обводкой. Последняя точка: кружок побольше.
        // Пока трафика не было, пика нет: peak подтянут к единице, и indexOf
        // вернул бы -1 — так приложение и падало на пустой статистике.
        val peakI = values.indexOf(peak)
        if (peakI >= 0) {
            brush.style = Brush.Style.FILL
            brush.color = surf
            canvas.drawCircle(xs[peakI], ys[peakI], 3.5f * dp, brush)
            brush.style = Brush.Style.STROKE
            brush.strokeWidth = 2 * dp
            brush.color = acc
            canvas.drawCircle(xs[peakI], ys[peakI], 3.5f * dp, brush)
        }

        brush.style = Brush.Style.FILL
        brush.color = surf
        canvas.drawCircle(xs[n - 1], ys[n - 1], 6.5f * dp, brush)
        brush.color = fg
        canvas.drawCircle(xs[n - 1], ys[n - 1], 4.5f * dp, brush)
        canvas.restore()
    }
}

/**
 * Reveal — проявление диаграммы за 0,9 с при новых данных: кольцо зажигается
 * по кругу, линия прочерчивается. Один аниматор на вьюху; пока экран не
 * виден, ничего не крутится — данные просто встают на место.
 */
private class Reveal(private val view: View) {
    var value = 1f
        private set
    private var animator: ValueAnimator? = null

    fun start() {
        animator?.cancel()
        if (!view.isShown) { value = 1f; view.invalidate(); return }
        value = 0f
        animator = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 900
            interpolator = DecelerateInterpolator(2f)
            addUpdateListener { value = it.animatedValue as Float; view.invalidate() }
            start()
        }
    }
}

/** Полоса долей: страны по порядку, каждая своим оттенком акцента. */
class ShareBar @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var shares: List<Pair<Float, Int>> = emptyList()
    private var idle = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    /** shares — доли от нуля до единицы с цветом; idle — цвет пустой полосы. */
    fun show(shares: List<Pair<Float, Int>>, idle: Int) { this.shares = shares; this.idle = idle; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val w = width.toFloat()
        val h = height.toFloat()
        val gap = 3 * dp
        brush.color = idle
        canvas.drawRoundRect(0f, 0f, w, h, h / 2, h / 2, brush)
        if (shares.isEmpty()) return
        var x = 0f
        val usable = w - gap * (shares.size - 1)
        for ((share, color) in shares) {
            val sw = usable * share
            brush.color = color
            canvas.drawRoundRect(x, 0f, x + sw, h, h / 2, h / 2, brush)
            x += sw + gap
        }
    }
}

/**
 * Неделя по часам: клетка — час, яркость — объём за него. Пик недели —
 * цветом текста и со свечением, чтобы найти его, не разглядывая.
 */
class WeekHours @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var cells: List<Long> = emptyList()
    private var peakHour = -1
    private var peak = 1L
    private var acc = 0
    private var fg = 0
    private var line = 0
    private val brush = Brush(Brush.ANTI_ALIAS_FLAG)

    /** cells — 24 значения одной строки-дня; peak — вершина всей недели; peakHour — если она в этой строке. */
    fun show(cells: List<Long>, peak: Long, peakHour: Int) {
        this.cells = cells; this.peak = peak.coerceAtLeast(1); this.peakHour = peakHour; invalidate()
    }
    fun paint(t: Theme) { acc = t.acc; fg = t.fg; line = t.line; invalidate() }

    override fun onDraw(canvas: Canvas) {
        if (cells.isEmpty()) return
        val dp = resources.displayMetrics.density
        val gap = 2 * dp
        val cw = (width - gap * (cells.size - 1)) / cells.size
        val h = height.toFloat()
        for (i in cells.indices) {
            val x = i * (cw + gap)
            val share = cells[i].toFloat() / peak
            when {
                i == peakHour -> {
                    brush.color = ColorUtils.setAlphaComponent(fg, 70)
                    canvas.drawRoundRect(x - 2 * dp, -2 * dp, x + cw + 2 * dp, h + 2 * dp, 5 * dp, 5 * dp, brush)
                    brush.color = fg
                }
                cells[i] == 0L -> brush.color = ColorUtils.setAlphaComponent(line, 120)
                else -> brush.color = ColorUtils.setAlphaComponent(acc, (40 + 215 * share).toInt().coerceIn(30, 255))
            }
            canvas.drawRoundRect(x, 0f, x + cw, h, 4 * dp, 4 * dp, brush)
        }
    }
}
