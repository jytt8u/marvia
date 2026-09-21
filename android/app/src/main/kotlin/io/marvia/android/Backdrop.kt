package io.marvia.android

import android.graphics.Bitmap
import android.graphics.BitmapShader
import android.graphics.Canvas
import android.graphics.ColorFilter
import android.graphics.LinearGradient
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.RadialGradient
import android.graphics.Shader
import android.graphics.drawable.Drawable
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.sin

/**
 * Backdrop — фон экрана по свету темы: ровный, мягкий, пятном или сияние.
 *
 * Рисуется шейдерами, а не GradientDrawable: тому доступны восемь направлений
 * и круглый градиент без формы, а в look.js фон — это CSS с углом, эллипсом
 * 130%×100% и пятном у «сияния». Повторяем ту же геометрию, иначе
 * телефон светил бы не оттуда, откуда панель.
 *
 * Угол CSS считается от «вверх» по часовой; линия градиента у прямоугольника
 * длиной |w·sin| + |h·cos|, цвета — от начала к концу. Так работает
 * linear-gradient, и так же здесь.
 */
class Backdrop(private val t: Theme, private val pattern: String = io.marvia.android.Paint.style.pattern, private val ink: Int = 12) : Drawable() {

    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private var w = 0f
    private var h = 0f

    override fun onBoundsChange(bounds: android.graphics.Rect) {
        w = bounds.width().toFloat()
        h = bounds.height().toFloat()
        rebuild()
    }

    private fun rebuild() {
        if (w <= 0f || h <= 0f) return
        when (t.kind) {
            "flat" -> {
                brush.shader = null
                brush.color = t.bg
            }
            "radial" -> brush.shader = ellipse(
                intArrayOf(t.lit, t.mid, t.shade), floatArrayOf(0f, 0.45f, 1f), 1.3f, 1.0f,
            )
            // Сияние: пятно света, подкрашенное оттенком, к 62% эллипса
            // сходит в тень, и дальше — тень. В CSS под ним лежит ещё линейный
            // слой, но его не видно: последний цвет градиента тянется до края.
            "aurora" -> brush.shader = ellipse(
                intArrayOf(Look.mix(t.lit, t.tint, 0.25), t.shade), floatArrayOf(0f, 0.62f), 0.9f, 0.6f,
            )
            else -> brush.shader = if (t.dir == "c") {
                linear(intArrayOf(t.shade, t.lit, t.shade), floatArrayOf(0f, 0.5f, 1f))
            } else {
                linear(intArrayOf(t.lit, Look.mix(t.lit, t.shade, 0.55), t.shade), floatArrayOf(0f, 0.52f, 1f))
            }
        }
    }

    /** linear — градиент вдоль угла CSS через весь прямоугольник. */
    private fun linear(colors: IntArray, stops: FloatArray): Shader {
        val deg = (LookTable.dirs.firstOrNull { it.key == t.dir }?.deg ?: 315).toDouble()
        val rad = Math.toRadians(deg)
        val dx = sin(rad).toFloat()
        val dy = -cos(rad).toFloat()
        val len = abs(w * dx) + abs(h * dy)
        val cx = w / 2
        val cy = h / 2
        return LinearGradient(
            cx - dx * len / 2, cy - dy * len / 2, cx + dx * len / 2, cy + dy * len / 2,
            colors, stops, Shader.TileMode.CLAMP,
        )
    }

    /**
     * ellipse — radial-gradient с полуосями в долях ширины и высоты, из точки,
     * куда указывает свет. Круглый шейдер растянут матрицей: своего эллипса
     * у Android нет.
     */
    private fun ellipse(colors: IntArray, stops: FloatArray, rx: Float, ry: Float): Shader {
        val (fx, fy) = when (t.dir) {
            "nw" -> 0f to 0f
            "n" -> 0.5f to 0f
            "ne" -> 1f to 0f
            "w" -> 0f to 0.5f
            "e" -> 1f to 0.5f
            "sw" -> 0f to 1f
            "s" -> 0.5f to 1f
            "se" -> 1f to 1f
            else -> 0.5f to 0.5f
        }
        val shader = RadialGradient(0f, 0f, 1f, colors, stops, Shader.TileMode.CLAMP)
        val m = Matrix()
        m.setScale(w * rx, h * ry)
        m.postTranslate(w * fx, h * fy)
        shader.setLocalMatrix(m)
        return shader
    }

    override fun draw(canvas: Canvas) {
        val b = bounds
        canvas.drawRect(b, brush)
        if (t.kind != "flat") canvas.drawRect(b, grain)
        // Узор из цвета текста, едва заметный: фон перестаёт быть пустым, но
        // не спорит с содержимым. Плитка, повторённая шейдером: рисовать
        // тысячи точек на каждый кадр дорого, а плитка — один вызов. Точки,
        // сетка, кольца или штрих — как в макете; «ровный» — без узора.
        if (pattern != Store.PATTERN_NONE) canvas.drawRect(b, dots)
    }

    /** Плотность экрана — у Drawable своей нет, берём системную. Объявлена до узора: он её читает при создании. */
    private val density = android.content.res.Resources.getSystem().displayMetrics.density

    // Плитка собирается лениво: у «ровного» фона и у карточек-превью узора
    // нет, а их на «Теме» десятки — по битмапу на каждую было бы зря.
    private val dots: Paint by lazy { Paint(Paint.ANTI_ALIAS_FLAG).apply {
        val ink = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = androidx.core.graphics.ColorUtils.setAlphaComponent(t.fg, ink) }
        val size = ((when (pattern) { "grid" -> 28; "rings" -> 82; "lines" -> 20; else -> 22 }) * density).toInt().coerceAtLeast(2)
        val tile = android.graphics.Bitmap.createBitmap(size, size, android.graphics.Bitmap.Config.ARGB_8888)
        val c = Canvas(tile)
        when (pattern) {
            "grid" -> { c.drawRect(0f, 0f, size.toFloat(), density, ink); c.drawRect(0f, 0f, density, size.toFloat(), ink) }
            "rings" -> {
                ink.style = Paint.Style.STROKE; ink.strokeWidth = density
                c.drawCircle(size / 2f, size / 2f, size * 0.49f, ink); c.drawCircle(size / 2f, size / 2f, size * 0.24f, ink)
            }
            "lines" -> { ink.strokeWidth = density; c.drawLine(0f, size.toFloat(), size.toFloat(), 0f, ink) }
            else -> c.drawCircle(size / 2f, size / 2f, density, ink)
        }
        shader = android.graphics.BitmapShader(tile, Shader.TileMode.REPEAT, Shader.TileMode.REPEAT)
    } }

    /**
     * grain — зерно поверх градиента: плёночный шум в пару единиц яркости.
     *
     * Тёмный градиент в восемь бит на цвет идёт кольцами: соседние ступени
     * различаются на единицу, а глаз видит границу между ними. DITHER_FLAG
     * на аппаратном холсте шейдеры не трогает, и кольца оставались — на
     * эмуляторе особенно. Шум ломает границы ступеней, и глаз перестаёт их
     * собирать в линии; сам он ниже порога заметности. Плитка 128×128
     * общая на все фоны и повторяется, а не считается на каждый пиксель:
     * фон рисуется на каждом кадре анимаций.
     */
    private val grain = Paint().apply {
        shader = BitmapShader(grainTile, Shader.TileMode.REPEAT, Shader.TileMode.REPEAT)
    }

    companion object {
        private val grainTile: Bitmap by lazy {
            val n = 128
            val px = IntArray(n * n)
            val rnd = java.util.Random(7)
            for (i in px.indices) {
                // Половина точек чуть светлее, половина чуть темнее: среднее не
                // сдвигается, и цвет фона остаётся тем, что задан темой.
                val a = 4 + rnd.nextInt(4)
                px[i] = if (rnd.nextBoolean()) (a shl 24) or 0xFFFFFF else (a shl 24)
            }
            Bitmap.createBitmap(px, n, n, Bitmap.Config.ARGB_8888)
        }
    }

    override fun setAlpha(alpha: Int) {
        brush.alpha = alpha
    }

    override fun setColorFilter(filter: ColorFilter?) {
        brush.colorFilter = filter
    }

    @Deprecated("Deprecated in Java")
    override fun getOpacity(): Int = PixelFormat.OPAQUE
}
