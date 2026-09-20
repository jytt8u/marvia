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
        val glow = 120 * dp
        brush.style = Paint.Style.FILL
        brush.shader = RadialGradient(
            cx, cy, glow,
            intArrayOf(ColorUtils.setAlphaComponent(theme.acc, 26), ColorUtils.setAlphaComponent(theme.acc, 15), 0),
            floatArrayOf(0f, 0.97f, 1f), Shader.TileMode.CLAMP,
        )
        canvas.drawCircle(cx, cy, glow, brush)
        brush.shader = null

        brush.style = Paint.Style.STROKE
        brush.strokeWidth = 1 * dp
        brush.color = ColorUtils.setAlphaComponent(theme.fg, 9)
        val reach = maxOf(width, height).toFloat()
        var r = 40 * dp
        while (r < reach) {
            canvas.drawCircle(cx, cy, r, brush)
            r += 40 * dp
        }
    }
}
