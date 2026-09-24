package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Typeface
import android.util.AttributeSet
import android.view.View
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** Down/up samples from the running VPN. Zero is drawn as zero; no synthetic activity. */
class SpeedChartView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var data: List<SessionDetails.Sample> = emptyList()
    private var theme: Theme = Look.theme(Look.Choice())
    private val pen = Paint(Paint.ANTI_ALIAS_FLAG)
    private val path = Path()

    fun show(samples: List<SessionDetails.Sample>, t: Theme) { data = samples; theme = t; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val left = 2 * dp
        val right = width - 2 * dp
        val top = 15 * dp
        val bottom = height - 16 * dp
        pen.style = Paint.Style.STROKE
        pen.strokeWidth = dp
        pen.color = theme.line
        for (i in 0..3) {
            val y = top + (bottom - top) * i / 3f
            canvas.drawLine(left, y, right, y, pen)
        }
        val points = data.takeLast(120)
        val max = points.maxOfOrNull { maxOf(it.down, it.up) }?.coerceAtLeast(1.0) ?: 1.0
        fun trace(color: Int, value: (SessionDetails.Sample) -> Double) {
            if (points.isEmpty()) return
            path.reset()
            points.forEachIndexed { i, item ->
                val x = left + (right - left) * i / maxOf(119, points.size - 1)
                val y = bottom - ((bottom - top) * value(item) / max).toFloat()
                if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
            }
            pen.color = color
            pen.strokeWidth = 2.5f * dp
            pen.strokeJoin = Paint.Join.ROUND
            pen.strokeCap = Paint.Cap.ROUND
            canvas.drawPath(path, pen)
            val last = points.last()
            val x = left + (right - left) * (points.size - 1) / 119f
            val y = bottom - ((bottom - top) * value(last) / max).toFloat()
            pen.style = Paint.Style.FILL
            canvas.drawCircle(x, y, 3.5f * dp, pen)
            pen.style = Paint.Style.STROKE
        }
        trace(theme.acc) { it.down }
        trace(theme.fg) { it.up }
    }
}

/** Proportion of received and sent bytes for the current session. */
class SessionSplitView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var received = 0L
    private var sent = 0L
    private var theme: Theme = Look.theme(Look.Choice())
    private val pen = Paint(Paint.ANTI_ALIAS_FLAG)
    private val clip = Path()

    fun show(received: Long, sent: Long, t: Theme) { this.received = received; this.sent = sent; theme = t; invalidate() }

    override fun onDraw(canvas: Canvas) {
        val radius = height / 2f
        pen.color = theme.line
        canvas.drawRoundRect(0f, 0f, width.toFloat(), height.toFloat(), radius, radius, pen)
        val total = received + sent
        if (total <= 0) return
        val receivedWidth = (width.toDouble() * received / total).toFloat()
        if (receivedWidth > 0f) {
            clip.reset()
            clip.addRoundRect(0f, 0f, width.toFloat(), height.toFloat(), radius, radius, Path.Direction.CW)
            canvas.save()
            canvas.clipPath(clip)
            pen.color = theme.acc
            canvas.drawRect(0f, 0f, receivedWidth, height.toFloat(), pen)
            canvas.restore()
        }
    }
}

/** Last seven recorded sessions, with the active one at the right when present. */
class SessionBarsView @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : View(context, attrs) {
    private var data: List<SessionDetails.Session> = emptyList()
    private var active = false
    private var theme: Theme = Look.theme(Look.Choice())
    private val pen = Paint(Paint.ANTI_ALIAS_FLAG)
    private val date = SimpleDateFormat("d MMM", Locale.getDefault())

    fun show(rows: List<SessionDetails.Session>, active: Boolean, t: Theme) {
        data = rows.takeLast(7); this.active = active; theme = t
        contentDescription = data.joinToString(", ") { "${date.format(Date(it.began))}: ${Format.size(context, it.received + it.sent)}" }
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val bottom = height - 24 * dp
        val top = 6 * dp
        pen.color = theme.line
        pen.strokeWidth = dp
        canvas.drawLine(0f, bottom, width.toFloat(), bottom, pen)
        if (data.isEmpty()) return
        val maximum = data.maxOf { it.received + it.sent }.coerceAtLeast(1L)
        val step = width.toFloat() / data.size
        pen.textSize = 10 * resources.displayMetrics.scaledDensity
        pen.typeface = Typeface.DEFAULT
        data.forEachIndexed { index, s ->
            val x = step * (index + 0.5f)
            val barWidth = minOf(24 * dp, step * 0.38f)
            val h = ((s.received + s.sent).toDouble() / maximum * (bottom - top - 6 * dp)).toFloat().coerceAtLeast(2 * dp)
            pen.color = if (active && index == data.lastIndex) theme.acc else theme.dim
            canvas.drawRoundRect(x - barWidth / 2, bottom - h, x + barWidth / 2, bottom, 5 * dp, 5 * dp, pen)
            val label = date.format(Date(s.began))
            pen.color = theme.dim
            canvas.drawText(label, x - pen.measureText(label) / 2, height - 4 * dp, pen)
        }
    }
}
