package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View
import android.view.animation.LinearInterpolator
import androidx.core.graphics.ColorUtils
import kotlin.math.cos
import kotlin.math.sin

/**
 * HaloView — то, что в макете лежит за кнопкой на главной: мягкое пятно
 * акцента и тонкие кольца, расходящиеся от центра диска, с двумя искрами на
 * орбитах. Без них кнопка «приклеена» к фону; с ними у неё есть место на
 * экране. Рисуется отдельной вьюхой под кнопкой, чтобы кольца уходили за
 * её края.
 *
 * Пока туннель поднят, ореол живёт: пятно дышит, искры медленно идут по
 * орбитам. Выключено — всё стоит: кадры на пустом экране незачем жечь.
 * Шейдер пятна собирается на размер и тему, а не на каждый кадр.
 */
class HaloView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) {
            if (field == value) return
            field = value
            glow = null
            invalidate()
        }

    /** Подключено ли: живой ореол — только у поднятого туннеля. */
    var lit: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            live(value)
            invalidate()
        }

    /** Фаза жизни, 0..1 за круг; от неё дыхание пятна и положение искр. */
    private var phase = 0f
    private var pulse: ValueAnimator? = null
    private var glow: Shader? = null
    private var glowR = 0f

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        super.onSizeChanged(w, h, oldw, oldh)
        glow = null
    }

    private fun live(on: Boolean) {
        pulse?.cancel()
        pulse = null
        if (!on || !isAttachedToWindow || !isShown) return
        val scale = android.provider.Settings.Global.getFloat(context.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
        if (scale == 0f) return
        // Круг за 24 с: искры еле ползут, дыхание — вдох-выдох раз в 3,6 с,
        // в такт кнопке. Что-то быстрее читалось бы как загрузка.
        pulse = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 24_000
            repeatCount = ValueAnimator.INFINITE
            interpolator = LinearInterpolator()
            addUpdateListener { phase = it.animatedValue as Float; invalidate() }
            start()
        }
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (lit) live(true)
    }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        pulse?.cancel()
        pulse = null
    }

    override fun onVisibilityChanged(changedView: View, visibility: Int) {
        super.onVisibilityChanged(changedView, visibility)
        if (isShown) { if (lit && pulse == null) live(true) } else { pulse?.cancel(); pulse = null }
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        // Орбиты — по размеру кнопки, а не вьюхи: вьюха нарочно больше сцены,
        // чтобы кольца не резались по её краю.
        val unit = resources.getDimension(R.dimen.hero_button_size) / 248f
        val breath = if (lit) 0.86f + 0.14f * ((sin(phase * Math.PI * 2 * (24_000f / 3600f)).toFloat() + 1f) / 2f) else 0.8f

        var g = glow
        if (g == null) {
            glowR = 178 * unit
            g = RadialGradient(
                cx, cy, glowR,
                intArrayOf(ColorUtils.setAlphaComponent(theme.acc, if (theme.dark) 54 else 25), ColorUtils.setAlphaComponent(theme.acc, 17), ColorUtils.setAlphaComponent(theme.acc, 0)),
                floatArrayOf(0f, 0.55f, 1f), Shader.TileMode.CLAMP,
            )
            glow = g
        }
        brush.style = Paint.Style.FILL
        brush.shader = g
        brush.alpha = (255 * breath).toInt()
        canvas.drawCircle(cx, cy, glowR, brush)
        brush.shader = null
        brush.alpha = 255

        brush.style = Paint.Style.STROKE
        brush.strokeWidth = .7f * dp
        // Три тихих кольца: у стыка ореола с содержимым нет жёсткой границы.
        for (i in 0..2) {
            val r = (112 + i * 23) * unit
            brush.color = ColorUtils.setAlphaComponent(theme.acc, 28 - i * 7)
            canvas.drawCircle(cx, cy, r, brush)
        }

        // Две искры на первом и втором кольце; подключено — идут по орбите.
        brush.style = Paint.Style.FILL
        val a = phase * Math.PI * 2
        val r1 = 133 * unit
        val r2 = 135 * unit
        val a1 = Math.toRadians(210.0) + a
        val a2 = Math.toRadians(30.0) + a
        brush.color = ColorUtils.setAlphaComponent(theme.acc, 160)
        canvas.drawCircle(cx + (r1 * cos(a1)).toFloat(), cy + (r1 * sin(a1)).toFloat(), 2 * dp, brush)
        brush.color = ColorUtils.setAlphaComponent(theme.acc, 85)
        canvas.drawCircle(cx + (r2 * cos(a2)).toFloat(), cy + (r2 * sin(a2)).toFloat(), 2 * dp, brush)
    }
}
