package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.graphics.Typeface
import android.util.AttributeSet
import android.view.View
import androidx.core.content.ContextCompat
import androidx.core.graphics.drawable.DrawableCompat

/**
 * ThemePreview — телефон с главным экраном в подключённом виде.
 *
 * Показываем не пустой «Отключено», а то, ради чего тема и выбирается:
 * светящуюся кнопку и строку страны. Всё берётся из той же [Theme], что
 * красит настоящий экран, — фон, кнопка, цвета текста, — поэтому картинка
 * не может разойтись с тем, что человек увидит, вернувшись на «Туннель».
 *
 * Кнопку рисует тот же [PowerButton], что и главная. Страна и пинг —
 * образец: их реальные значения зависят от продавца, а предпросмотру нужна
 * узнаваемая строка.
 */
class ThemePreview @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val power = PowerButton(context)
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private val wordmark = ContextCompat.getDrawable(context, R.drawable.ic_wordmark)?.mutate()

    var theme: Theme = Look.theme(Look.Choice())
        set(value) {
            field = value
            power.theme = value
            invalidate()
        }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val t = theme

        // Корпус по центру, шириной чуть меньше половины карточки; низ уходит
        // за край и обрезается — как в макете. Так экран внутри достаточно
        // крупный, чтобы прочесть страну, а не только увидеть цвета.
        val w = minOf(width * 0.42f, 230 * dp)
        val left = (width - w) / 2
        val body = RectF(left, 0f, left + w, w * 19f / 9f)
        val corner = 26 * dp

        brush.style = Paint.Style.FILL
        brush.color = if (t.dark) 0xFF0B0C0E.toInt() else 0xFFD8DADC.toInt()
        canvas.drawRoundRect(body, corner, corner, brush)

        val bezel = 4 * dp
        val screen = RectF(body.left + bezel, body.top + bezel, body.right - bezel, body.bottom - bezel)
        canvas.save()
        val clip = Path().apply { addRoundRect(screen, corner - bezel, corner - bezel, Path.Direction.CW) }
        canvas.clipPath(clip)

        // Фон — тот же рисовальщик, что за настоящим экраном.
        Backdrop(t).apply {
            setBounds(screen.left.toInt(), screen.top.toInt(), screen.right.toInt(), screen.bottom.toInt())
            draw(canvas)
        }

        // Масштаб: внутри «телефона» живёт условная ширина 200dp.
        val scale = screen.width() / (200 * dp)
        canvas.translate(screen.left, screen.top)
        canvas.scale(scale, scale)

        // Надпись слева, шестерёнка справа — как в шапке главной.
        wordmark?.let {
            DrawableCompat.setTint(it, t.fg)
            val wh = 9 * dp
            val ww = wh * 610f / 136f
            it.setBounds((18 * dp).toInt(), (20 * dp).toInt(), (18 * dp + ww).toInt(), (20 * dp + wh).toInt())
            it.draw(canvas)
        }
        drawGear(canvas, 200 * dp - 24 * dp, 24 * dp, 6 * dp, t.dim)

        // «Подключено» — пилюля с точкой.
        val label = context.getString(R.string.status_on)
        brush.textSize = 8.5f * dp
        brush.typeface = Typeface.DEFAULT
        val tw = brush.measureText(label)
        val pillW = tw + 24 * dp
        val pill = RectF(100 * dp - pillW / 2, 42 * dp, 100 * dp + pillW / 2, 58 * dp)
        brush.color = t.accSoft
        canvas.drawRoundRect(pill, 8 * dp, 8 * dp, brush)
        brush.color = t.acc
        canvas.drawCircle(pill.left + 9 * dp, pill.centerY(), 2.5f * dp, brush)
        brush.color = if (t.dark) t.fg else t.acc
        canvas.drawText(label, pill.left + 16 * dp, pill.centerY() + 3 * dp, brush)

        // Кнопка — та же, что на главной, вместе со своим свечением.
        val cx = 100 * dp
        val cy = 116 * dp
        val r = 40 * dp
        power.glowing = true
        val size = (r * 2 + 60 * dp).toInt()
        power.layout(0, 0, size, size)
        canvas.save()
        canvas.translate(cx - size / 2f, cy - size / 2f)
        power.draw(canvas)
        canvas.restore()

        // Строка страны: флаг, название, город и пинг.
        val rowY = 178 * dp
        brush.textSize = 13 * dp
        canvas.drawText(Flags.of("Финляндия"), 40 * dp, rowY + 5 * dp, brush)
        brush.color = t.fg
        brush.textSize = 9 * dp
        brush.typeface = Typeface.DEFAULT_BOLD
        canvas.drawText(context.getString(R.string.theme_preview_country), 62 * dp, rowY, brush)
        brush.color = t.dim
        brush.textSize = 7.5f * dp
        brush.typeface = Typeface.DEFAULT
        canvas.drawText(context.getString(R.string.theme_preview_place), 62 * dp, rowY + 11 * dp, brush)

        canvas.restore()
    }

    /** drawGear — шестерёнка настроек: круг с восемью зубцами. */
    private fun drawGear(canvas: Canvas, cx: Float, cy: Float, r: Float, color: Int) {
        brush.style = Paint.Style.STROKE
        brush.strokeWidth = r * 0.28f
        brush.strokeCap = Paint.Cap.ROUND
        brush.color = color
        canvas.drawCircle(cx, cy, r * 0.55f, brush)
        for (i in 0 until 8) {
            canvas.save()
            canvas.rotate(i * 45f, cx, cy)
            canvas.drawLine(cx, cy - r * 0.8f, cx, cy - r * 1.05f, brush)
            canvas.restore()
        }
        brush.style = Paint.Style.FILL
    }
}
