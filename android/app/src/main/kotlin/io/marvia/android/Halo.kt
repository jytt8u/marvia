package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View

/**
 * Halo — кольца и пятно света за кнопкой питания, как в макете.
 *
 * Кольца через каждые 40dp цветом текста на 3,5%: сами по себе невидимы,
 * но глаз читает их как глубину вокруг кнопки. Пятно света посередине —
 * цветом акцента на 10%, к 122dp сходит на нет. Всё это лежит отдельной
 * вьюхой во всю ширину, а не внутри кнопки: кольца уходят за её края.
 *
 * Пока туннель поднят, кольца медленно расходятся от кнопки — раз в четыре
 * секунды новое рождается у диска и тает у края. Так видно, что туннель
 * живой, не глядя на цифры. Выключено — кольца стоят.
 */
class Halo @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private var drift = 0f
    private var driftAnim: ValueAnimator? = null

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    /** alive — туннель поднят: кольца ползут наружу. */
    var alive: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            driftAnim?.cancel()
            driftAnim = null
            if (value && ValueAnimator.areAnimatorsEnabled()) {
                driftAnim = ValueAnimator.ofFloat(0f, 1f).apply {
                    duration = 4000
                    repeatCount = ValueAnimator.INFINITE
                    interpolator = null
                    addUpdateListener { drift = it.animatedValue as Float; invalidate() }
                    start()
                }
            } else {
                drift = 0f
                invalidate()
            }
        }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val t = theme

        brush.style = Paint.Style.FILL
        val glow = 122 * dp
        brush.shader = RadialGradient(
            cx, cy, glow,
            intArrayOf(Look.withAlpha(t.acc, 0.10), Look.withAlpha(t.acc, 0.06), t.acc and 0xFFFFFF),
            floatArrayOf(0f, 0.96f, 1f), Shader.TileMode.CLAMP,
        )
        canvas.drawCircle(cx, cy, glow, brush)
        brush.shader = null

        brush.style = Paint.Style.STROKE
        brush.strokeWidth = 1 * dp
        val step = 40 * dp
        val reach = maxOf(width, height).toFloat()
        // Сдвиг общий на все кольца: каждое проходит один шаг за цикл, и
        // картинка замыкается сама. У края кольцо тает, у диска — рождается.
        var r = step * (1 + drift)
        while (r < reach) {
            val fade = 1f - (r / reach).coerceIn(0f, 1f) * 0.6f
            brush.color = Look.withAlpha(t.fg, 0.035 * fade)
            canvas.drawCircle(cx, cy, r, brush)
            r += step
        }
    }

    override fun onDetachedFromWindow() {
        driftAnim?.cancel()
        super.onDetachedFromWindow()
    }
}
