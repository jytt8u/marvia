package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.SweepGradient
import android.util.AttributeSet
import androidx.appcompat.widget.AppCompatButton

/**
 * PowerButton — кнопка питания из макета: диск с мягким объёмом, вокруг —
 * дуга с разрывом, внутри — знак. Никакой подписи: что делает кнопка,
 * говорит заголовок под ней.
 *
 * Три состояния рисуются по-разному, и это единственное, что их различает:
 * выключено — дуга замкнута и тусклая; подключаемся — короткая дуга крутится;
 * включено — дуга на три четверти акцентом, диск светится.
 *
 * Свечение живёт внутри вьюхи, а не отдельным слоем за ней: тогда оно есть
 * и на главной, и в предпросмотре темы, и выглядит одинаково. Ради него у
 * диска запас до края вьюхи.
 */
class PowerButton @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : AppCompatButton(context, attrs) {

    enum class Phase { OFF, CONNECTING, ON }

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private val box = RectF()

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    var phase: Phase = Phase.OFF
        set(value) {
            field = value
            spin = 0f
            invalidate()
        }

    /** Угол дуги, пока подключаемся: крутится сама, пока фаза не сменится. */
    private var spin = 0f

    init { background = null; setAllCaps(false); isHapticFeedbackEnabled = true }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val outer = minOf(width, height) / 2f - ROOM * dp
        val disc = outer - 22 * dp
        val on = phase == Phase.ON
        val t = theme

        val scale = if (isPressed) .965f else 1f
        canvas.save()
        canvas.scale(scale, scale, cx, cy)

        // Свечение: сила из темы, ноль означает «без свечения».
        brush.style = Paint.Style.FILL
        if (on && t.glowA > 0) {
            val reach = disc + ROOM * dp + 18 * dp
            val alpha = (t.glowA * 0.32 * 255).toInt().coerceIn(0, 255)
            brush.shader = RadialGradient(
                cx, cy, reach,
                intArrayOf((alpha shl 24) or (t.acc and 0xFFFFFF), t.acc and 0xFFFFFF),
                floatArrayOf(disc / reach * 0.85f, 1f), Shader.TileMode.CLAMP,
            )
            canvas.drawCircle(cx, cy, reach, brush)
            brush.shader = null
        }

        // Диск: объём от света в верхнем левом углу, как в макете.
        when (t.btn) {
            "bare" -> Unit
            "solid" -> { brush.color = t.acc; canvas.drawCircle(cx, cy, disc, brush) }
            "glass" -> { brush.color = t.accSoft; canvas.drawCircle(cx, cy, disc, brush) }
            else -> {
                val hi = if (on) Look.mix(t.surf2, t.fg, 0.08) else t.surf2
                brush.shader = RadialGradient(
                    cx - disc * 0.24f, cy - disc * 0.4f, disc * 1.5f,
                    intArrayOf(hi, t.surf, Look.mix(t.surf, t.shade, 0.5)),
                    floatArrayOf(0f, 0.55f, 1f), Shader.TileMode.CLAMP,
                )
                canvas.drawCircle(cx, cy, disc, brush)
                brush.shader = null
            }
        }
        if (t.btn != "bare") {
            // Внутренний обод в 1dp: заметный, когда включено, едва — когда нет.
            brush.style = Paint.Style.STROKE
            brush.strokeWidth = 1 * dp
            brush.color = if (on) Look.withAlpha(t.acc, 0.22) else Look.withAlpha(t.fg, 0.08)
            canvas.drawCircle(cx, cy, disc - 0.5f * dp, brush)
        }

        // Дуга вокруг: от трёх часов по часовой стрелке, с растяжкой в
        // прозрачность на конце.
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = 3 * dp
        brush.strokeCap = Paint.Cap.ROUND
        box.set(cx - outer, cy - outer, cx + outer, cy + outer)
        val arcColor = when (phase) {
            Phase.ON -> t.acc
            Phase.CONNECTING -> t.dim
            Phase.OFF -> t.line
        }
        val sweep = when (phase) {
            Phase.ON -> 270f
            Phase.CONNECTING -> 88f
            Phase.OFF -> 359.9f
        }
        val start = if (phase == Phase.CONNECTING) spin else 0f
        brush.shader = SweepGradient(
            cx, cy,
            intArrayOf(arcColor, Look.withAlpha(arcColor, 0.12)),
            floatArrayOf(0f, sweep / 360f),
        ).also { sh ->
            val m = android.graphics.Matrix()
            m.setRotate(start, cx, cy)
            sh.setLocalMatrix(m)
        }
        canvas.drawArc(box, start, sweep, false, brush)
        brush.shader = null

        // Знак питания: круг с разрывом сверху и черта. В макете значок 56 из
        // 216, кружок — радиусом 8 из 24 единиц значка.
        val g = outer * (56f / 216f) * (8f / 12f)
        brush.color = when {
            t.btn == "solid" -> t.accFg
            on -> t.fg
            phase == Phase.CONNECTING -> t.dim
            else -> Look.mix(t.dim, t.fg, 0.2)
        }
        brush.strokeWidth = 2 * dp * (outer / (108 * dp))
        box.set(cx - g, cy - g, cx + g, cy + g)
        canvas.drawArc(box, -55f, 290f, false, brush)
        canvas.drawLine(cx, cy - g * 1.125f, cx, cy, brush)

        canvas.restore()

        if (phase == Phase.CONNECTING) {
            spin = (spin + 4f) % 360f
            postInvalidateOnAnimation()
        }
    }

    override fun drawableStateChanged() { super.drawableStateChanged(); invalidate() }

    override fun performClick(): Boolean {
        performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK)
        return super.performClick()
    }

    private companion object {
        /** Запас от дуги до края вьюхи под свечение, dp. */
        const val ROOM = 18
    }
}
