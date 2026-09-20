package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View
import androidx.core.graphics.ColorUtils

/**
 * HourBars — расход за сутки по часам: двадцать четыре столбика.
 *
 * Только столбики: подпись «Сегодня», итог и ось «00 … 23» — обычные вьюхи
 * в карточке, их красит Paint тегами. Здесь один цвет и одна геометрия:
 * пустой час — линия, текущий — акцент, остальные — акцент вполсилы.
 */
class HourBars @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)

    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field = value; invalidate() }

    var hours: List<Long> = List(24) { 0L }
        set(value) { field = value; invalidate() }

    /** Какой столбик горит целиком; -1 — никакой (не сегодня). */
    var current: Int = Traffic.hourOf(System.currentTimeMillis())
        set(value) { field = value; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val n = hours.size.coerceAtLeast(1)
        val gap = 3 * dp
        val bw = (width - gap * (n - 1)) / n
        val max = (hours.maxOrNull() ?: 0L).coerceAtLeast(1)
        val h = height.toFloat()
        hours.forEachIndexed { i, v ->
            brush.color = when {
                v == 0L -> theme.line
                i == current -> theme.acc
                else -> ColorUtils.setAlphaComponent(theme.acc, 140)
            }
            val bh = if (v == 0L) 3 * dp else (h * v / max).coerceAtLeast(3 * dp)
            val x = i * (bw + gap)
            canvas.drawRoundRect(x, h - bh, x + bw, h, 2 * dp, 2 * dp, brush)
        }
    }
}
