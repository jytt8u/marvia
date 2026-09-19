package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.graphics.SweepGradient
import android.util.AttributeSet
import android.view.animation.DecelerateInterpolator
import android.view.animation.OvershootInterpolator
import androidx.appcompat.widget.AppCompatButton
import kotlin.math.sin

/**
 * PowerButton — кнопка питания из макета: диск с мягким объёмом, вокруг —
 * дуга с разрывом, внутри — знак. Никакой подписи: что делает кнопка,
 * говорит заголовок под ней.
 *
 * Три состояния различаются только рисунком: выключено — дуга замкнута и
 * тусклая; подключаемся — короткая дуга крутится; включено — дуга на три
 * четверти акцентом, диск светится и дышит.
 *
 * Между состояниями кнопка не прыгает, а перетекает: свечение разгорается,
 * дуга укорачивается, цвета сходятся — полсекунды. Резкая смена картинки
 * читается как сбой, плавная — как ответ на нажатие. Всё через ValueAnimator,
 * поэтому системный масштаб анимаций и «без анимаций» соблюдаются сами.
 *
 * Свечение живёт внутри вьюхи, а не отдельным слоем за ней: тогда оно есть
 * и на главной, и в предпросмотре темы, и выглядит одинаково.
 */
class PowerButton @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : AppCompatButton(context, attrs) {

    enum class Phase { OFF, CONNECTING, ON }

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private val box = RectF()
    private val turn = Matrix()

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    /** on — насколько «включено», 0…1; busy — насколько «подключаемся». */
    private var on = 0f
    private var busy = 0f
    private var press = 1f
    private var spin = 0f
    private var breath = 0f

    private var onAnim: ValueAnimator? = null
    private var busyAnim: ValueAnimator? = null
    private var pressAnim: ValueAnimator? = null
    private var breathAnim: ValueAnimator? = null

    var phase: Phase = Phase.OFF
        set(value) {
            if (field == value) return
            field = value
            onAnim = glide(onAnim, on, if (value == Phase.ON) 1f else 0f) { on = it }
            busyAnim = glide(busyAnim, busy, if (value == Phase.CONNECTING) 1f else 0f) { busy = it }
            breathe(value == Phase.ON)
            invalidate()
        }

    /** snap — без перехода: предпросмотру нужен готовый кадр. */
    fun snap(to: Phase) {
        phase = to
        onAnim?.cancel(); busyAnim?.cancel()
        on = if (to == Phase.ON) 1f else 0f
        busy = if (to == Phase.CONNECTING) 1f else 0f
        breath = 0.25f
        invalidate()
    }

    init { background = null; setAllCaps(false); isHapticFeedbackEnabled = true }

    private fun glide(prev: ValueAnimator?, from: Float, to: Float, set: (Float) -> Unit): ValueAnimator {
        prev?.cancel()
        return ValueAnimator.ofFloat(from, to).apply {
            duration = 520
            interpolator = DecelerateInterpolator(1.6f)
            addUpdateListener { set(it.animatedValue as Float); invalidate() }
            start()
        }
    }

    /** breathe — медленный вдох-выдох свечения, пока туннель поднят. */
    private fun breathe(go: Boolean) {
        breathAnim?.cancel()
        breathAnim = null
        if (!go || !ValueAnimator.areAnimatorsEnabled()) return
        breathAnim = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 3200
            repeatCount = ValueAnimator.INFINITE
            interpolator = null
            addUpdateListener { breath = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val outer = minOf(width, height) / 2f - ROOM * dp
        val disc = outer - 22 * dp
        val t = theme
        // Дыхание: синус за цикл, от 0,85 до 1 силы свечения.
        val pulse = 0.85f + 0.15f * ((sin(breath * Math.PI * 2).toFloat() + 1f) / 2f)

        canvas.save()
        canvas.scale(press, press, cx, cy)

        // Свечение: сила из темы, ноль означает «без свечения».
        brush.style = Paint.Style.FILL
        if (on > 0f && t.glowA > 0) {
            val reach = disc + ROOM * dp + 18 * dp
            val alpha = (t.glowA * 0.32 * 255 * on * pulse).toInt().coerceIn(0, 255)
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
                val hi = Look.mix(t.surf2, t.fg, 0.08 * on)
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
            brush.color = Look.withAlpha(Look.mix(t.fg, t.acc, on.toDouble()), 0.08 + 0.14 * on)
            canvas.drawCircle(cx, cy, disc - 0.5f * dp, brush)
        }

        // Дуга вокруг: цвет и длина перетекают между состояниями; пока
        // подключаемся — короткая и крутится.
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = 3 * dp
        brush.strokeCap = Paint.Cap.ROUND
        box.set(cx - outer, cy - outer, cx + outer, cy + outer)
        val calm = Look.mix(t.line, t.acc, on.toDouble())
        val arcColor = Look.mix(calm, t.dim, busy.toDouble())
        val calmSweep = 359.9f - 89.9f * on
        val sweep = calmSweep + (88f - calmSweep) * busy
        val start = spin * busy
        brush.shader = SweepGradient(
            cx, cy,
            intArrayOf(arcColor, Look.withAlpha(arcColor, 0.12)),
            floatArrayOf(0f, sweep / 360f),
        ).also { sh ->
            turn.setRotate(start, cx, cy)
            sh.setLocalMatrix(turn)
        }
        canvas.drawArc(box, start, sweep, false, brush)
        brush.shader = null

        // Знак питания: круг с разрывом сверху и черта. В макете значок 56 из
        // 216, кружок — радиусом 8 из 24 единиц значка.
        val g = outer * (56f / 216f) * (8f / 12f)
        val idle = Look.mix(t.dim, t.fg, 0.2)
        brush.color = if (t.btn == "solid") t.accFg else Look.mix(Look.mix(idle, t.fg, on.toDouble()), t.dim, busy.toDouble())
        brush.strokeWidth = 2 * dp * (outer / (108 * dp))
        box.set(cx - g, cy - g, cx + g, cy + g)
        canvas.drawArc(box, -55f, 290f, false, brush)
        canvas.drawLine(cx, cy - g * 1.125f, cx, cy, brush)

        canvas.restore()

        if (busy > 0f && phase == Phase.CONNECTING) {
            spin = (spin + 4f) % 360f
            postInvalidateOnAnimation()
        }
    }

    /** Нажатие — пружина: вжалась на 4 %, отпустили — вернулась с перелётом. */
    override fun drawableStateChanged() {
        super.drawableStateChanged()
        val to = if (isPressed) 0.96f else 1f
        if (to == press) return
        pressAnim?.cancel()
        pressAnim = ValueAnimator.ofFloat(press, to).apply {
            duration = if (isPressed) 90 else 260
            interpolator = if (isPressed) DecelerateInterpolator() else OvershootInterpolator(2.2f)
            addUpdateListener { press = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    override fun performClick(): Boolean {
        performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK)
        return super.performClick()
    }

    override fun onDetachedFromWindow() {
        breathAnim?.cancel()
        super.onDetachedFromWindow()
    }

    private companion object {
        /** Запас от дуги до края вьюхи под свечение, dp. */
        const val ROOM = 18
    }
}
