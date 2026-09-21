package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View
import androidx.core.graphics.ColorUtils

/**
 * HaloView — то, что в макете лежит за кнопкой на главной: мягкое пятно
 * акцента и тонкие кольца через каждые 40 dp, расходящиеся от центра диска.
 * Без них кнопка «приклеена» к фону; с ними у неё есть место на экране.
 * Рисуется отдельной вьюхой под кнопкой, чтобы кольца уходили за её края.
 */
class HaloView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val unit = dp * (height / (262 * dp)).coerceAtMost(1f)
        val glow = minOf(width * .68f, height * .68f)
        brush.style = Paint.Style.FILL
        brush.shader = RadialGradient(
            cx, cy, glow,
            intArrayOf(ColorUtils.setAlphaComponent(theme.acc, if (theme.dark) 54 else 25), ColorUtils.setAlphaComponent(theme.acc, 17), ColorUtils.setAlphaComponent(theme.acc, 0)),
            floatArrayOf(0f, 0.55f, 1f), Shader.TileMode.CLAMP,
        )
        canvas.drawCircle(cx, cy, glow, brush)
        brush.shader = null

        brush.style = Paint.Style.STROKE
        brush.strokeWidth = .7f * dp
        // Three quiet orbital lines; no hard edge where the hero meets the content.
        for (i in 0..2) {
            val r = (112 + i * 23) * unit
            brush.color = ColorUtils.setAlphaComponent(theme.acc, 28 - i * 7)
            canvas.drawCircle(cx, cy, r, brush)
        }
        brush.style = Paint.Style.FILL
        brush.color = ColorUtils.setAlphaComponent(theme.acc, 160)
        canvas.drawCircle(cx - 116 * unit, cy - 66 * unit, 2 * dp, brush)
        brush.color = ColorUtils.setAlphaComponent(theme.acc, 85)
        canvas.drawCircle(cx + 117 * unit, cy + 66 * unit, 2 * dp, brush)
    }
}
