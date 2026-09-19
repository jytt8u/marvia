package io.marvia.android

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
 * цветом акцента на 10%, к 118dp сходит на нет. Всё это лежит отдельной
 * вьюхой во всю ширину, а не внутри кнопки: кольца уходят за её края.
 */
class Halo @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

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
        brush.color = Look.withAlpha(t.fg, 0.035)
        val reach = maxOf(width, height).toFloat()
        var r = 40 * dp
        while (r < reach) {
            canvas.drawCircle(cx, cy, r, brush)
            r += 40 * dp
        }
    }
}
