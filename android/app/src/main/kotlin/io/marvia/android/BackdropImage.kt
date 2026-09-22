package io.marvia.android

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.drawable.Drawable
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min

/**
 * BackdropFrame — как лежит снимок на экране: приближение, точка, которая
 * стоит в середине, и поворот.
 *
 * Без этого фото вписывалось только одним способом — по центру, — и лицо
 * на портретном снимке уходило за край, а человеку нечем было его вернуть.
 *
 * zoom ≥ 1 — во сколько раз ближе, чем «заполнить». fx, fy — доли ширины и
 * высоты повёрнутого снимка, 0,5 — середина. rot — четверти оборота по
 * часовой.
 */
data class BackdropFrame(val zoom: Float = 1f, val fx: Float = 0.5f, val fy: Float = 0.5f, val rot: Int = 0) {

    fun encode(): String = "$zoom,$fx,$fy,$rot"

    companion object {
        const val MAX_ZOOM = 5f

        fun decode(raw: String?): BackdropFrame {
            val p = raw?.split(',') ?: return BackdropFrame()
            if (p.size != 4) return BackdropFrame()
            return BackdropFrame(
                (p[0].toFloatOrNull() ?: 1f).coerceIn(1f, MAX_ZOOM),
                (p[1].toFloatOrNull() ?: 0.5f).coerceIn(0f, 1f),
                (p[2].toFloatOrNull() ?: 0.5f).coerceIn(0f, 1f),
                ((p[3].toIntOrNull() ?: 0) % 4 + 4) % 4,
            )
        }
    }
}

/**
 * BackdropImage — свой фон из фото: снимок под интерфейсом и пелена над ним.
 *
 * Пелена цветом фона темы, а не чёрным: под светлой темой чёрная пелена
 * сделала бы её тёмной, и текст, посчитанный под светлый фон, пропал бы.
 *
 * fit повторяет object-fit из CSS: cover заполняет экран, обрезая лишнее;
 * contain вписывает целиком, а поля закрывает фон темы. Поверх — кадр,
 * выбранный человеком: приближение, сдвиг и поворот.
 */
class BackdropImage(
    private val bitmap: Bitmap,
    private val fit: String,
    private val ground: Int,
    private val veil: Int,
    private val frame: BackdropFrame = BackdropFrame(),
) : Drawable() {

    private val brush = Paint(Paint.FILTER_BITMAP_FLAG)
    private val matrix = Matrix()

    override fun draw(canvas: Canvas) {
        val b = bounds
        if (b.isEmpty) return

        // Поля под «целиком» и любой промах — фоном темы, не чёрным.
        canvas.drawColor(ground)
        place(bitmap, b.width().toFloat(), b.height().toFloat(), fit, frame, matrix)
        matrix.postTranslate(b.left.toFloat(), b.top.toFloat())
        canvas.drawBitmap(bitmap, matrix, brush)
        canvas.drawColor(veil)
    }

    override fun setAlpha(alpha: Int) = Unit
    override fun setColorFilter(colorFilter: ColorFilter?) = Unit
    @Deprecated("Deprecated in Java")
    override fun getOpacity(): Int = PixelFormat.OPAQUE

    companion object {
        /**
         * place собирает матрицу снимка под экран vw×vh. Общая для фона и для
         * редактора кадра: то, что человек видел, выбирая, и то, что потом
         * лежит под приложением, обязано совпадать до точки.
         *
         * Сдвиг ограничен: снимок в режиме «заполнить» не может отъехать так,
         * чтобы из-под него показался фон, — такой кадр никто не выбирает
         * нарочно, а пальцем получить его легко.
         */
        fun place(bitmap: Bitmap, vw: Float, vh: Float, fit: String, frame: BackdropFrame, out: Matrix) {
            val bw = bitmap.width.toFloat()
            val bh = bitmap.height.toFloat()
            out.reset()
            if (bw <= 0f || bh <= 0f) return
            val turned = frame.rot % 2 == 1
            val rw = if (turned) bh else bw
            val rh = if (turned) bw else bh
            val base = if (fit == "contain") min(vw / rw, vh / rh) else max(vw / rw, vh / rh)
            val s = base * frame.zoom
            val sw = rw * s
            val sh = rh * s
            var dx = (0.5f - frame.fx) * sw
            var dy = (0.5f - frame.fy) * sh
            val mx = max(0f, (sw - vw) / 2f)
            val my = max(0f, (sh - vh) / 2f)
            dx = dx.coerceIn(-mx, mx)
            dy = dy.coerceIn(-my, my)
            if (abs(sw - vw) < 0.5f) dx = 0f
            if (abs(sh - vh) < 0.5f) dy = 0f

            out.postTranslate(-bw / 2f, -bh / 2f)
            out.postRotate(90f * frame.rot)
            out.postScale(s, s)
            out.postTranslate(vw / 2f + dx, vh / 2f + dy)
        }

        /**
         * focus переводит сдвиг в пикселях экрана обратно в точку снимка —
         * для редактора: палец двигает картинку, а хранится точка.
         */
        fun focus(bitmap: Bitmap, vw: Float, vh: Float, fit: String, frame: BackdropFrame, dxPx: Float, dyPx: Float): BackdropFrame {
            val turned = frame.rot % 2 == 1
            val rw = if (turned) bitmap.height.toFloat() else bitmap.width.toFloat()
            val rh = if (turned) bitmap.width.toFloat() else bitmap.height.toFloat()
            if (rw <= 0f || rh <= 0f) return frame
            val base = if (fit == "contain") min(vw / rw, vh / rh) else max(vw / rw, vh / rh)
            val s = base * frame.zoom
            val sw = rw * s
            val sh = rh * s
            // Точка, которая сейчас в середине, с учётом того же ограничения,
            // что в place: иначе палец «копил» бы сдвиг за краем.
            val mx = max(0f, (sw - vw) / 2f)
            val my = max(0f, (sh - vh) / 2f)
            val curX = ((0.5f - frame.fx) * sw).coerceIn(-mx, mx)
            val curY = ((0.5f - frame.fy) * sh).coerceIn(-my, my)
            val nx = (curX + dxPx).coerceIn(-mx, mx)
            val ny = (curY + dyPx).coerceIn(-my, my)
            return frame.copy(fx = (0.5f - nx / sw).coerceIn(0f, 1f), fy = (0.5f - ny / sh).coerceIn(0f, 1f))
        }
    }
}
