package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View

/**
 * SignalBars — четыре полоски, как уровень связи у телефона.
 *
 * Отклик и так стоит числом рядом; полоски нужны, чтобы сравнивать строки
 * взглядом, не читая числа. Порог тот же, что красит число: до 50 мс —
 * четыре, до 80 — три, до 120 — две, дальше одна; молчащая нода — ноль.
 */
class SignalBars @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private var level = 0
    private var lit = 0
    private var off = 0

    fun show(ms: Long, alive: Boolean, t: Theme) {
        level = when {
            !alive || ms <= 0 -> 0
            ms < 50 -> 4
            ms < 80 -> 3
            ms < 120 -> 2
            else -> 1
        }
        lit = if (level == 0) t.dim else pingColor(t, ms)
        off = t.line
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val gap = 2 * dp
        val w = (width - gap * 3) / 4
        val h = height.toFloat()
        for (i in 0 until 4) {
            val x = i * (w + gap)
            val bh = h * (0.35f + 0.65f * i / 3f)
            brush.color = if (i < level) lit else off
            canvas.drawRoundRect(x, h - bh, x + w, h, 1.5f * dp, 1.5f * dp, brush)
        }
    }
}
