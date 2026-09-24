package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View
import androidx.core.graphics.ColorUtils
import kotlin.math.cos
import kotlin.math.sin

/**
 * Тихие орбиты вокруг кнопки. Цветное пятно здесь дублировало свечение
 * кнопки и оставалось даже в теме без свечения. Статические кольца сохраняют
 * композицию и не требуют бесконечной перерисовки при подключённом VPN.
 */
class HaloView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) {
            if (field == value) return
            field = value
            invalidate()
        }

    var lit: Boolean = false
        set(value) {
            if (field == value) return
            field = value
            invalidate()
        }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val cx = width / 2f
        val cy = height / 2f
        val unit = resources.getDimension(R.dimen.hero_button_size) / 248f
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = .7f * dp
        for (i in 0..2) {
            brush.color = ColorUtils.setAlphaComponent(theme.acc, 28 - i * 7)
            canvas.drawCircle(cx, cy, (112 + i * 23) * unit, brush)
        }
        brush.style = Paint.Style.FILL
        val a1 = Math.toRadians(210.0)
        val a2 = Math.toRadians(30.0)
        brush.color = ColorUtils.setAlphaComponent(theme.acc, if (lit) 160 else 100)
        canvas.drawCircle(cx + (133 * unit * cos(a1)).toFloat(), cy + (133 * unit * sin(a1)).toFloat(), 2 * dp, brush)
        brush.color = ColorUtils.setAlphaComponent(theme.acc, if (lit) 85 else 55)
        canvas.drawCircle(cx + (135 * unit * cos(a2)).toFloat(), cy + (135 * unit * sin(a2)).toFloat(), 2 * dp, brush)
    }
}
