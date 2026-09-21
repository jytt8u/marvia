package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View

/**
 * SignalBars — четыре палочки качества, как уровень сигнала: сколько
 * горит, столько и хорош отклик. Число рядом точнее, но палочки читаются
 * с расстояния вытянутой руки, а число — нет.
 */
class SignalBars @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var lit: Int = 0
        set(value) { field = value.coerceIn(0, 4); invalidate() }
    var on: Int = 0xFFC3CCD6.toInt()
        set(value) { field = value; invalidate() }
    var off: Int = 0xFF4B4E50.toInt()
        set(value) { field = value; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val w = 3 * dp
        val gap = 2 * dp
        val h = height.toFloat()
        for (i in 0 until 4) {
            brush.color = if (i < lit) on else off
            val bh = (5 + 3 * i) * dp
            val x = i * (w + gap)
            canvas.drawRoundRect(x, h - bh, x + w, h, 2 * dp, 2 * dp, brush)
        }
    }

    companion object {
        /** Сколько палочек за такой отклик: пороги как в макете. */
        fun of(ms: Long, alive: Boolean): Int = when {
            !alive || ms <= 0 -> 0
            ms < 40 -> 4
            ms < 50 -> 3
            ms < 70 -> 2
            else -> 1
        }
    }
}
