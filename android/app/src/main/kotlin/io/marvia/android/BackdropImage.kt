package io.marvia.android

import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.drawable.Drawable
import kotlin.math.max
import kotlin.math.min

/**
 * BackdropImage — свой фон из фото: снимок под интерфейсом и пелена над ним.
 *
 * Пелена цветом фона темы, а не чёрным: под светлой темой чёрная пелена
 * сделала бы её тёмной, и текст, посчитанный под светлый фон, пропал бы.
 * Ровно то же делает панель в браузере — здесь та же геометрия, чтобы код
 * темы не врал про вид на разных клиентах.
 *
 * fit повторяет object-fit из CSS: cover заполняет экран, обрезая лишнее;
 * contain вписывает целиком, а поля закрывает фон темы.
 */
class BackdropImage(
    private val bitmap: Bitmap,
    private val fit: String,
    private val ground: Int,
    private val veil: Int,
) : Drawable() {

    private val brush = Paint(Paint.FILTER_BITMAP_FLAG)
    private val matrix = Matrix()

    override fun draw(canvas: Canvas) {
        val b = bounds
        if (b.isEmpty) return

        // Поля под «целиком» и любой промах — фоном темы, не чёрным.
        canvas.drawColor(ground)

        val vw = b.width().toFloat()
        val vh = b.height().toFloat()
        val bw = bitmap.width.toFloat()
        val bh = bitmap.height.toFloat()
        if (bw <= 0f || bh <= 0f) return

        val scale = if (fit == "contain") min(vw / bw, vh / bh) else max(vw / bw, vh / bh)
        val dw = bw * scale
        val dh = bh * scale
        matrix.setScale(scale, scale)
        matrix.postTranslate(b.left + (vw - dw) / 2f, b.top + (vh - dh) / 2f)
        canvas.drawBitmap(bitmap, matrix, brush)

        canvas.drawColor(veil)
    }

    override fun setAlpha(alpha: Int) = Unit
    override fun setColorFilter(colorFilter: ColorFilter?) = Unit
    @Deprecated("Deprecated in Java")
    override fun getOpacity(): Int = PixelFormat.OPAQUE
}
