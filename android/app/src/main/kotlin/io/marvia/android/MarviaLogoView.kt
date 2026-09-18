package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.AttributeSet
import android.util.LruCache
import android.view.View
import java.util.concurrent.Executors

/**
 * MarviaLogoView — знак «M», покрашенный в акцент темы.
 *
 * Вьюха не считает ни одного пикселя сама: готовые картинки живут в
 * [LogoAtlas], общем для всех экранов, а здесь только рисование и переход.
 * Так знак в шапке «Туннеля», «Стран» и «Темы» красится один раз на цвет,
 * а не трижды, и не тормозит прокрутку — в onDraw только drawBitmap.
 *
 * Смена темы — плавная: старая картинка растворяется в новой за четверть
 * секунды. Если человек выключил анимации в системе, переход мгновенный.
 */
class MarviaLogoView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG or Paint.FILTER_BITMAP_FLAG)
    private val box = RectF()

    private var current: Bitmap? = LogoAtlas.stock(context)
    private var previous: Bitmap? = null
    private var fade = 1f
    private var fader: ValueAnimator? = null

    /** Ключ последней запрошенной картинки: ответ на старый запрос выбрасывается. */
    private var wanted: Int = LogoAtlas.STOCK

    fun setTheme(theme: Theme) {
        val key = LogoAtlas.key(theme.acc)
        if (key == wanted) return
        wanted = key
        LogoAtlas.get(context, key) { k, bmp ->
            if (k != wanted) return@get
            show(bmp)
        }
    }

    private fun show(bmp: Bitmap) {
        if (bmp === current) return
        fader?.cancel()
        if (!animationsOn() || current == null || !isShown) {
            previous = null
            current = bmp
            fade = 1f
            invalidate()
            return
        }
        previous = current
        current = bmp
        fade = 0f
        fader = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = 250
            addUpdateListener {
                fade = it.animatedValue as Float
                invalidate()
            }
            start()
        }
    }

    private fun animationsOn(): Boolean =
        Settings.Global.getFloat(context.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) > 0f

    override fun onDraw(canvas: Canvas) {
        val cur = current ?: return
        // Вписываем по центру с сохранением пропорций — как fitCenter у ImageView.
        val w = width - paddingLeft - paddingRight
        val h = height - paddingTop - paddingBottom
        if (w <= 0 || h <= 0) return
        val scale = minOf(w.toFloat() / cur.width, h.toFloat() / cur.height)
        val bw = cur.width * scale
        val bh = cur.height * scale
        val left = paddingLeft + (w - bw) / 2
        val top = paddingTop + (h - bh) / 2
        box.set(left, top, left + bw, top + bh)

        previous?.let {
            brush.alpha = ((1f - fade) * 255).toInt()
            canvas.drawBitmap(it, null, box, brush)
        }
        brush.alpha = (fade * 255).toInt()
        canvas.drawBitmap(cur, null, box, brush)
        if (fade >= 1f) previous = null
    }

    override fun onDetachedFromWindow() {
        fader?.cancel()
        super.onDetachedFromWindow()
    }
}

/**
 * LogoAtlas — готовые картинки знака по цвету акцента.
 *
 * Один исходный PNG на процесс, один рабочий поток на перекраску и кэш на
 * дюжину цветов: человек листает темы туда-сюда, и второй раз тот же цвет
 * должен появляться мгновенно. Серый акцент вида из коробки хранится под
 * отдельным ключом и никогда не перекрашивается — это оригинал.
 */
object LogoAtlas {
    const val STOCK = 0

    private val cache = LruCache<Int, Bitmap>(12)
    private val worker = Executors.newSingleThreadExecutor { r -> Thread(r, "marvia-logo").apply { isDaemon = true } }
    private val main = Handler(Looper.getMainLooper())
    private var source: Bitmap? = null

    /** Ключ кэша: серый — оригинал, всё остальное — RGB акцента. */
    fun key(acc: Int): Int = if (LogoTint.isStock(acc)) STOCK else (acc and 0xFFFFFF).let { if (it == 0) 1 else it }

    /** Оригинал, синхронно: он нужен первым кадром, до того как тема прочитана. */
    fun stock(context: Context): Bitmap = cache.get(STOCK) ?: original(context).also { cache.put(STOCK, it) }

    /** Готовая картинка из кэша, если есть; иначе null и запрос в фоне. */
    fun peek(context: Context, acc: Int, then: () -> Unit): Bitmap? {
        val k = key(acc)
        return cache.get(k) ?: run { get(context, k) { _, _ -> then() }; null }
    }

    fun get(context: Context, key: Int, then: (Int, Bitmap) -> Unit) {
        cache.get(key)?.let { then(key, it); return }
        if (key == STOCK) { then(key, stock(context)); return }
        val app = context.applicationContext
        worker.execute {
            val done = cache.get(key) ?: paint(app, key).also { cache.put(key, it) }
            main.post { then(key, done) }
        }
    }

    private fun paint(context: Context, acc: Int): Bitmap {
        val src = original(context)
        val px = IntArray(src.width * src.height)
        src.getPixels(px, 0, src.width, 0, 0, src.width, src.height)
        val out = LogoTint.tint(px, acc)
        return Bitmap.createBitmap(out, src.width, src.height, Bitmap.Config.ARGB_8888)
    }

    @Synchronized
    private fun original(context: Context): Bitmap =
        source ?: BitmapFactory.decodeResource(
            context.applicationContext.resources,
            R.drawable.marvia_mark,
            BitmapFactory.Options().apply { inScaled = false; inPreferredConfig = Bitmap.Config.ARGB_8888 },
        ).also { source = it }
}
