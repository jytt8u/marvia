package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.SweepGradient
import android.util.AttributeSet
import android.view.animation.LinearInterpolator
import androidx.appcompat.widget.AppCompatButton
import androidx.core.graphics.ColorUtils

/**
 * PowerButton — кнопка питания из макета: объёмный диск и дуга вокруг.
 *
 * Дуга живёт отдельно от диска и говорит о состоянии: выключено — ровное
 * кольцо цветом линии; подключаемся — короткий отрезок, который крутится;
 * подключено — три четверти круга акцентом с разрывом внизу справа, как на
 * референсе. Диск при этом остаётся диском: у него свой объём и свои варианты
 * из темы (обычный, сплошной, стекло, без диска).
 *
 * Свечение рисуется здесь же, внутри вьюхи, а не отдельным слоем за ней:
 * тогда оно есть и на главной, и в предпросмотре темы, и выглядит одинаково.
 * Ради него у дуги запас до края вьюхи.
 */
class PowerButton @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : AppCompatButton(context, attrs) {
    enum class State { OFF, CONNECTING, ON, FAILED }

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    var state: State = State.OFF
        set(value) {
            if (field == value) return
            field = value
            spin(value == State.CONNECTING)
            invalidate()
        }

    /** Куда сейчас смотрит начало дуги, градусы; крутится только на подключении. */
    private var turn = 0f
    private var spinner: ValueAnimator? = null

    init { background = null; setAllCaps(false); isHapticFeedbackEnabled = true; text = "" }

    private fun spin(on: Boolean) {
        spinner?.cancel()
        spinner = null
        if (!on) { turn = 0f; return }
        // Скорость как в CSS: оборот за 1,4 с. Уважаем системный запрет на
        // анимации — тогда отрезок просто стоит.
        val scale = android.provider.Settings.Global.getFloat(context.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
        if (scale == 0f) return
        spinner = ValueAnimator.ofFloat(0f, 360f).apply {
            duration = 1400
            repeatCount = ValueAnimator.INFINITE
            interpolator = LinearInterpolator()
            addUpdateListener { turn = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        spinner?.cancel()
        spinner = null
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (state == State.CONNECTING) spin(true)
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        // Геометрия макета: дуга радиусом 104 при диске 86 — всё от центра.
        val arcR = minOf(width, height) / 2f - ROOM * dp
        val radius = arcR * (86f / 104f)
        val on = state == State.ON
        val scale = if (isPressed) .965f else 1f
        canvas.save()
        canvas.scale(scale, scale, cx, cy)

        brush.style = Paint.Style.FILL
        brush.shader = null
        // Свечение — только когда подключено и у диска есть тело: «контур» из
        // макета — прозрачный круг с одной линией, заливка под ним превращала
        // бы его в диск, которого человек не выбирал.
        if (theme.glowA > 0 && on && theme.btn != "bare") {
            val alpha = (theme.glowA * 210).toInt().coerceIn(0, 255)
            val reach = arcR + ROOM * dp
            brush.shader = RadialGradient(
                cx, cy, reach,
                intArrayOf((alpha shl 24) or (theme.acc and 0xFFFFFF), theme.acc and 0xFFFFFF),
                floatArrayOf(radius / reach * 0.9f, 1f),
                Shader.TileMode.CLAMP,
            )
            canvas.drawCircle(cx, cy, reach, brush)
            brush.shader = null
        }

        // Тень под диском: макет кладёт её вниз на 24px с размытием 48. На
        // светлой теме — вполсилы: чёрная тень на светлом фоне читалась грязью.
        if (theme.btn != "bare") {
            brush.shader = RadialGradient(cx, cy + radius * .28f, radius * 1.35f, intArrayOf(if (theme.dark) 0x73000000 else 0x30000000, 0x00000000), floatArrayOf(0.55f, 1f), Shader.TileMode.CLAMP)
            canvas.drawCircle(cx, cy + radius * .28f, radius * 1.35f, brush)
            brush.shader = null
        }

        val solid = theme.btn == "solid"
        when (theme.btn) {
            "bare" -> Unit
            "glass" -> {
                brush.color = theme.accSoft
                canvas.drawCircle(cx, cy, radius, brush)
            }
            else -> {
                // Блик сверху слева, тень к низу: диск объёмный, а не плоский круг.
                // Как в макете: блик светлее второй поверхности, к краю — заметно темнее; диск круглый на глаз, а не плоский.
                // Блик — всегда светом, не цветом текста: на светлой теме текст тёмный, и
                // «блик» им выходил тёмным пятном.
                val top = if (solid) ColorUtils.blendARGB(theme.acc, 0xFFFFFFFF.toInt(), 0.28f) else ColorUtils.blendARGB(theme.surf2, 0xFFFFFFFF.toInt(), if (theme.dark) 0.16f else 0.45f)
                val mid = if (solid) theme.acc else ColorUtils.blendARGB(theme.surf2, theme.acc, if (theme.dark) .19f else .08f)
                val low = if (solid) ColorUtils.blendARGB(theme.acc, 0xFF000000.toInt(), 0.18f) else ColorUtils.blendARGB(theme.surf, theme.bg, .55f)
                brush.shader = RadialGradient(
                    cx - radius * .32f, cy - radius * .58f, radius * 1.85f,
                    intArrayOf(top, mid, low), floatArrayOf(0f, 0.52f, 1f), Shader.TileMode.CLAMP,
                )
                canvas.drawCircle(cx, cy, radius, brush)
                brush.shader = null
                // Подсветка изнутри снизу, когда подключено: акцент отражается в диске.
                if (!solid) {
                    brush.shader = RadialGradient(cx + radius * .4f, cy + radius, radius * 1.4f, intArrayOf(ColorUtils.setAlphaComponent(theme.acc, if (on) 80 else 38), ColorUtils.setAlphaComponent(theme.acc, 0)), null, Shader.TileMode.CLAMP)
                    canvas.drawCircle(cx, cy, radius, brush)
                    brush.shader = null
                }
            }
        }

        // Тонкий обод по краю диска: линия, а подключено — акцент вполсилы.
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = 1 * dp
        brush.color = when {
            theme.btn == "bare" -> theme.acc
            on -> ColorUtils.setAlphaComponent(theme.acc, if (theme.btn == "glass") 115 else 56)
            else -> ColorUtils.setAlphaComponent(theme.fg, 20)
        }
        if (theme.btn == "bare") brush.strokeWidth = 1.5f * dp
        canvas.drawCircle(cx, cy, radius, brush)

        // A specular rim gives the disc a lit upper edge without another shadow layer.
        if (theme.btn != "bare") {
            brush.shader = android.graphics.LinearGradient(cx - radius, cy - radius, cx + radius, cy + radius,
                intArrayOf(ColorUtils.setAlphaComponent(0xFFFFFFFF.toInt(), if (theme.dark) 120 else 200), ColorUtils.setAlphaComponent(0xFFFFFFFF.toInt(), 0)),
                null, Shader.TileMode.CLAMP)
            canvas.drawArc(RectF(cx - radius + dp, cy - radius + dp, cx + radius - dp, cy + radius - dp), 190f, 150f, false, brush)
            brush.shader = null
        }

        // Дуга: начало внизу (90°), по часовой. Градиент от акцента к почти
        // прозрачному по ходу дуги — хвост растворяется, как в макете.
        brush.strokeWidth = 3 * dp
        brush.strokeCap = Paint.Cap.ROUND
        val box = RectF(cx - arcR, cy - arcR, cx + arcR, cy + arcR)
        when (state) {
            State.OFF, State.FAILED -> {
                brush.color = if (state == State.FAILED) ColorUtils.setAlphaComponent(theme.fail, 140) else theme.line
                canvas.drawCircle(cx, cy, arcR, brush)
                if (state == State.OFF) {
                    brush.strokeWidth = 2 * dp
                    brush.shader = sweep(cx, cy, 205f, 108f, ColorUtils.setAlphaComponent(theme.acc, 145))
                    canvas.drawArc(box, 205f, 108f, false, brush)
                    brush.shader = null
                }
            }
            State.CONNECTING -> {
                val start = 90f + turn
                brush.shader = sweep(cx, cy, start, 88f, theme.dim)
                canvas.drawArc(box, start, 88f, false, brush)
                brush.shader = null
            }
            State.ON -> {
                brush.shader = sweep(cx, cy, 90f, 270f, theme.acc)
                canvas.drawArc(box, 90f, 270f, false, brush)
                brush.shader = null
            }
        }

        // Знак питания: линия и разомкнутое кольцо, как в значке макета.
        brush.color = when {
            solid -> theme.accFg
            on -> if (theme.dark) ColorUtils.blendARGB(theme.fg, theme.acc, 0.35f) else theme.acc
            state == State.FAILED -> theme.fail
            else -> ColorUtils.blendARGB(theme.fg, theme.acc, .25f)
        }
        brush.strokeWidth = 2.6f * dp
        val r = radius * 0.235f
        canvas.drawArc(RectF(cx - r, cy - r, cx + r, cy + r), -55f, 290f, false, brush)
        canvas.drawLine(cx, cy - r - 3 * dp, cx, cy - r * 0.15f, brush)
        canvas.restore()
    }

    /** Градиент вдоль дуги: полный цвет в начале, 12 % в конце. */
    private fun sweep(cx: Float, cy: Float, start: Float, sweepDeg: Float, color: Int): Shader {
        val shader = SweepGradient(
            cx, cy,
            intArrayOf(color, ColorUtils.setAlphaComponent(color, 30), ColorUtils.setAlphaComponent(color, 30)),
            floatArrayOf(0f, sweepDeg / 360f, 1f),
        )
        val m = android.graphics.Matrix()
        m.setRotate(start, cx, cy)
        shader.setLocalMatrix(m)
        return shader
    }

    override fun drawableStateChanged() { super.drawableStateChanged(); invalidate() }

    override fun performClick(): Boolean {
        performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK)
        return super.performClick()
    }

    private companion object {
        /** Запас от дуги до края вьюхи под свечение и тень, dp. */
        const val ROOM = 16
    }
}
