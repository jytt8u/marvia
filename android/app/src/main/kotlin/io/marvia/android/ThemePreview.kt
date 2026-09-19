package io.marvia.android

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.graphics.Typeface
import android.text.TextPaint
import android.text.TextUtils
import android.util.AttributeSet
import android.view.View

/**
 * ThemePreview — телефон с одним из трёх экранов в выбранной теме.
 *
 * Главная показана в подключённом виде: ради светящейся кнопки и строки
 * страны тема и выбирается. Настройки и серверы — чтобы увидеть, как тема
 * красит карточки, переключатели и списки. Всё берётся из той же [Theme],
 * что красит настоящие экраны, — фон, кнопка, цвета, — поэтому картинка не
 * может разойтись с тем, что человек увидит, вернувшись на вкладку.
 *
 * Кнопку рисует тот же [PowerButton], что и главная. Тексты — образцы: их
 * реальные значения зависят от продавца, а предпросмотру нужны узнаваемые.
 */
class ThemePreview @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {

    enum class Screen(val title: Int) {
        MAIN(R.string.theme_pv_main),
        SETTINGS(R.string.theme_pv_settings),
        SERVERS(R.string.theme_pv_servers),
    }

    private val power = PowerButton(context)
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    private val box = RectF()

    var theme: Theme = Look.theme(Look.Choice())
        set(value) {
            field = value
            power.theme = value
            invalidate()
        }

    var screen: Screen = Screen.MAIN
        private set

    /** target — куда переключаемся; screen догоняет на середине кроссфейда. */
    var target: Screen = Screen.MAIN
        private set

    /**
     * Frame — кадр: телефон целиком или его кусок крупно, как в макете.
     *
     * Целый телефон — для цвета, света и фона: их видно на всём экране.
     * Карточки — для формы: скругления, подача и плотность с ногтя не
     * читаются, а крупно видны сразу. Шапка — для шрифта: заголовок и
     * подписи почти в натуральную величину. Числа — из макета: там телефон
     * в 390px стоит в масштабе 0.34, а крупные кадры — 0.82 и 1.0 со сдвигом
     * на 118px и 46px; здесь то же в долях ширины кадра и в единицах vw.
     *
     * wide — кадр во всю карточку, а не телефон; fill — во сколько раз экран
     * шире кадра; dx, dy — что уходит за левый и верхний край, в единицах vw.
     */
    enum class Frame(val wide: Float, val fill: Float, val dx: Float, val dy: Float) {
        PHONE(0f, 1f, 0f, 0f),
        CARDS(1f, 1.06f, 3f, 52f),
        HEADER(1f, 1.29f, 0f, 23f),
    }

    var frame: Frame = Frame.PHONE
        private set

    // Кадр анимируется одним числом p между снимком «откуда» и целью:
    // рамка, масштаб и сдвиг едут вместе, и телефон не прыгает ни в одной
    // точке пути — даже если кадр сменили на полдороге: снимок берётся с
    // текущего места. Кривая та же, что в макете: cubic-bezier(.2,.8,.2,1).
    private var fromWide = 0f
    private var fromFill = 1f
    private var fromDx = 0f
    private var fromDy = 0f
    private var p = 1f
    private var fade = 1f
    private var frameAnim: ValueAnimator? = null
    private var fadeAnim: ValueAnimator? = null

    /** onFrame — кому сообщать о смене кадра: экран прячет список за кадром. */
    var onFrame: ((Frame, Long) -> Unit)? = null

    private fun lerp(a: Float, b: Float, k: Float) = a + (b - a) * k
    private val wideNow get() = lerp(fromWide, frame.wide, p)
    private val fillNow get() = lerp(fromFill, frame.fill, p)
    private val dxNow get() = lerp(fromDx, frame.dx, p)
    private val dyNow get() = lerp(fromDy, frame.dy, p)

    /** focus показывает экран в кадре: с кроссфейдом экрана и наездом кадра. */
    fun focus(to: Screen, frame: Frame) {
        if (to == this.target && frame == this.frame) return
        this.target = to
        val animate = ValueAnimator.areAnimatorsEnabled()
        if (to != screen) {
            fadeAnim?.cancel()
            if (animate) {
                fadeAnim = ValueAnimator.ofFloat(1f, 0f, 1f).apply {
                    duration = 360
                    addUpdateListener {
                        val v = it.animatedValue as Float
                        // На середине — подмена экрана: в темноте её не видно.
                        if (it.animatedFraction >= 0.5f && screen != to) screen = to
                        fade = v
                        invalidate()
                    }
                    start()
                }
            } else screen = to
        }
        if (frame != this.frame) {
            fromWide = wideNow; fromFill = fillNow; fromDx = dxNow; fromDy = dyNow
            this.frame = frame
            frameAnim?.cancel()
            onFrame?.invoke(frame, if (animate) FRAME_MS else 0L)
            if (animate) {
                p = 0f
                frameAnim = ValueAnimator.ofFloat(0f, 1f).apply {
                    duration = FRAME_MS
                    interpolator = Motion.ease
                    addUpdateListener { p = it.animatedValue as Float; invalidate() }
                    start()
                }
            } else { p = 1f; invalidate() }
        }
    }

    /** Условная ширина экрана внутри телефона, dp; всё ниже — в ней. */
    private val vw = 200f

    companion object {
        /** FRAME_MS — сколько едет кадр; столько же прячется список экранов. */
        const val FRAME_MS = 560L
    }

    override fun onDraw(canvas: Canvas) {
        val dp = resources.displayMetrics.density
        val t = theme
        val h = height.toFloat()
        val wide = wideNow

        // Два кадра, между которыми едем: телефон 9:19 по центру и широкое
        // окно во всю карточку, чуть ниже телефона, как в макете.
        val pw = minOf(width.toFloat(), h * 9f / 19f)
        val phone = RectF((width - pw) / 2, 0f, (width + pw) / 2, h)
        val window = RectF(0f, 16 * dp, width.toFloat(), h - 16 * dp)
        val body = RectF(
            lerp(phone.left, window.left, wide), lerp(phone.top, window.top, wide),
            lerp(phone.right, window.right, wide), lerp(phone.bottom, window.bottom, wide),
        )
        val corner = lerp(22f, 18f, wide) * dp
        // Корпус тает в тонкую обводку: у крупного кадра телефона нет, есть окно.
        val bezel = lerp(3f, 1f, wide) * dp

        brush.style = Paint.Style.FILL
        brush.color = if (t.dark) 0xFF0B0C0E.toInt() else 0xFFD8DADC.toInt()
        canvas.drawRoundRect(body, corner, corner, brush)

        val screenBox = RectF(body.left + bezel, body.top + bezel, body.right - bezel, body.bottom - bezel)
        canvas.save()
        canvas.clipPath(Path().apply { addRoundRect(screenBox, corner - bezel, corner - bezel, Path.Direction.CW) })

        // Масштаб — от ширины кадра: экран шире его на fill и уехал вверх и
        // влево на dx, dy. Высота экрана в единицах vw — от целого телефона:
        // у крупных кадров она та же, просто низ за краем.
        val scale = screenBox.width() * fillNow / (vw * dp)
        val vh = (phone.height() - 6 * dp) / ((phone.width() - 6 * dp) / (vw * dp)) / dp
        canvas.translate(screenBox.left - dxNow * dp * scale, screenBox.top - dyNow * dp * scale)
        canvas.scale(scale, scale)

        // Фон — на весь экран телефона, а не на окно: крупный кадр — это
        // кусок того же экрана, и свет в нём должен лежать там же, где на
        // телефоне. Окно, залитое своим градиентом, внизу чернело.
        Backdrop(t).apply {
            setBounds(0, 0, (vw * dp).toInt(), (vh * dp).toInt())
            draw(canvas)
        }

        when (screen) {
            Screen.MAIN -> drawMain(canvas, dp, vh)
            Screen.SETTINGS -> drawSettings(canvas, dp)
            Screen.SERVERS -> drawServers(canvas, dp)
        }
        drawNav(canvas, dp, vh)
        canvas.restore()

        // Кроссфейд: пелена цветом корпуса поверх экрана, пока он подменяется.
        if (fade < 1f) {
            brush.style = Paint.Style.FILL
            brush.color = Look.withAlpha(if (t.dark) 0xFF0B0C0E.toInt() else 0xFFD8DADC.toInt(), (1f - fade).toDouble())
            canvas.drawRoundRect(screenBox, corner - bezel, corner - bezel, brush)
        }
    }

    // ------------------------------------------------------------ главная

    private fun drawMain(canvas: Canvas, dp: Float, vh: Float) {
        val t = theme
        // Знак — та же покрашенная в акцент картинка, что в настоящей шапке:
        // если её ещё нет в кэше, рисуем оригинал и перерисуемся, когда придёт.
        val bitmap = LogoAtlas.peek(context, t.acc) { invalidate() } ?: LogoAtlas.stock(context)
        val mh = 18 * dp
        val mw = mh * bitmap.width / bitmap.height
        brush.alpha = 255
        canvas.drawBitmap(bitmap, null, RectF(14 * dp, 18 * dp, 14 * dp + mw, 18 * dp + mh), brush)

        val cx = vw / 2 * dp
        val cy = 116 * dp
        power.snap(PowerButton.Phase.ON)
        val size = (2 * (40 + 22 + 18) * dp).toInt()
        power.layout(0, 0, size, size)
        canvas.save()
        canvas.translate(cx - size / 2f, cy - size / 2f)
        power.draw(canvas)
        canvas.restore()

        text(canvas, context.getString(R.string.status_on), cx, 192 * dp, 15f * dp, t.fg, Fonts.textBold(context), Paint.Align.CENTER)
        text(canvas, sample(), cx, 206 * dp, 8f * dp, t.dim, Fonts.mono(context), Paint.Align.CENTER)

        // Сегодня по часам, сессия и скорость — как на главной, вполовину.
        val top = vh - 42 - 108
        card(canvas, dp, 10f, top, vw - 10f, top + 58f)
        text(canvas, context.getString(R.string.stats_today), 18 * dp, (top + 14) * dp, 7f * dp, t.dim, Fonts.text(context))
        text(canvas, "4,2 ГБ", (vw - 18) * dp, (top + 14) * dp, 8f * dp, t.fg, Fonts.monoBold(context), Paint.Align.RIGHT)
        val bars = intArrayOf(1, 0, 0, 0, 1, 2, 4, 6, 7, 6, 5, 3, 4, 5, 7, 8, 8, 7, 9, 8, 6, 4, 3, 2)
        val bw = (vw - 36) / 24
        for ((i, v) in bars.withIndex()) {
            val bh = maxOf(1.5f, 24f * v / 9)
            brush.color = if (v == 0) t.line else if (i == 18) t.acc else Look.withAlpha(t.acc, 0.55)
            canvas.drawRoundRect(
                (18 + i * bw) * dp, (top + 50 - bh) * dp, (18 + (i + 1) * bw - 1.2f) * dp, (top + 50) * dp,
                1 * dp, 1 * dp, brush,
            )
        }
        val row = top + 64
        card(canvas, dp, 10f, row, vw / 2 - 3, row + 38f)
        card(canvas, dp, vw / 2 + 3, row, vw - 10f, row + 38f)
        text(canvas, context.getString(R.string.stats_session), 18 * dp, (row + 13) * dp, 7.5f * dp, t.dim, Fonts.text(context))
        text(canvas, "01:12:34", 18 * dp, (row + 29) * dp, 10f * dp, t.fg, Fonts.monoBold(context))
        text(canvas, context.getString(R.string.stats_speed), (vw / 2 + 11) * dp, (row + 13) * dp, 7.5f * dp, t.dim, Fonts.text(context))
        text(canvas, context.getString(R.string.stats_mbps, "48"), (vw / 2 + 11) * dp, (row + 29) * dp, 10f * dp, t.fg, Fonts.monoBold(context))
    }

    // ---------------------------------------------------------- настройки

    private fun drawSettings(canvas: Canvas, dp: Float) {
        val t = theme
        text(canvas, context.getString(R.string.theme_pv_settings), 14 * dp, 36 * dp, 15f * dp, t.fg, Fonts.textBold(context))
        text(canvas, "Marvia · Android", 14 * dp, 48 * dp, 7.5f * dp, t.dim, Fonts.text(context))

        card(canvas, dp, 10f, 58f, vw - 10f, 94f)
        tile(canvas, dp, 17f, 65f, 22f)
        text(canvas, context.getString(R.string.more_apps_title), 46 * dp, 73 * dp, 9f * dp, t.fg, Fonts.textBold(context))
        text(canvas, context.getString(R.string.theme_pv_apps_note), 46 * dp, 85 * dp, 7f * dp, t.dim, Fonts.text(context))

        text(canvas, context.getString(R.string.conn_group_protect).uppercase(), 14 * dp, 110 * dp, 5.5f * dp, t.dim, Fonts.mono(context), letter = 0.18f)
        card(canvas, dp, 10f, 116f, vw - 10f, 116f + 3 * 30f)
        val rows = listOf(
            Triple(R.string.settings_autostart, R.string.settings_autostart_sub, true),
            Triple(R.string.conn_lan, R.string.theme_pv_lan_note, false),
            Triple(R.string.settings_russian, R.string.settings_russian_sub, true),
        )
        for ((i, r) in rows.withIndex()) {
            val y = 116f + i * 30f
            if (i > 0) { brush.color = t.line; canvas.drawRect(10 * dp, y * dp, (vw - 10) * dp, (y + 0.5f) * dp, brush) }
            tile(canvas, dp, 17f, y + 6f, 18f)
            text(canvas, context.getString(r.first), 41 * dp, (y + 13) * dp, 8f * dp, t.fg, Fonts.textBold(context), width = (vw - 85) * dp)
            text(canvas, context.getString(r.second), 41 * dp, (y + 23) * dp, 6.5f * dp, t.dim, Fonts.text(context), width = (vw - 85) * dp)
            switchAt(canvas, dp, vw - 36f, y + 9f, r.third)
        }
    }

    // ------------------------------------------------------------ серверы

    private fun drawServers(canvas: Canvas, dp: Float) {
        val t = theme
        text(canvas, context.getString(R.string.servers_title), 14 * dp, 36 * dp, 15f * dp, t.fg, Fonts.textBold(context))
        text(canvas, context.getString(R.string.theme_pv_servers_note), 14 * dp, 48 * dp, 7.5f * dp, t.dim, Fonts.text(context))

        card(canvas, dp, 10f, 58f, vw - 10f, 92f, stroke = t.acc)
        text(canvas, context.getString(R.string.servers_auto), 18 * dp, 72 * dp, 9f * dp, t.fg, Fonts.textBold(context))
        text(canvas, context.getString(R.string.theme_pv_auto_note), 18 * dp, 84 * dp, 7f * dp, t.dim, Fonts.text(context))
        brush.color = t.acc
        canvas.drawCircle((vw - 22) * dp, 75 * dp, 6 * dp, brush)

        card(canvas, dp, 10f, 100f, vw - 10f, 100f + 34f + 3 * 30f)
        text(canvas, context.getString(R.string.theme_pv_provider), 18 * dp, 114 * dp, 9f * dp, t.fg, Fonts.textBold(context))
        text(canvas, context.getString(R.string.theme_pv_provider_note), 18 * dp, 125 * dp, 7f * dp, t.dim, Fonts.text(context))
        brush.color = t.line
        canvas.drawRect(10 * dp, 134 * dp, (vw - 10) * dp, 134.5f * dp, brush)

        val nodes = listOf(
            Triple(context.getString(R.string.theme_preview_country), context.getString(R.string.theme_pv_node_current), 33),
            Triple("Нидерланды", "Амстердам", 41),
            Triple("Германия", "Франкфурт", 48),
        )
        for ((i, n) in nodes.withIndex()) {
            val y = 134f + i * 30f
            if (i == 0) { brush.color = Look.withAlpha(t.fg, 0.04); canvas.drawRect(10 * dp, y * dp, (vw - 10) * dp, (y + 30) * dp, brush) }
            if (i > 0) { brush.color = t.line; canvas.drawRect(10 * dp, y * dp, (vw - 10) * dp, (y + 0.5f) * dp, brush) }
            text(canvas, n.first, 18 * dp, (y + 13) * dp, 8.5f * dp, t.fg, Fonts.textBold(context))
            text(canvas, n.second, 18 * dp, (y + 23) * dp, 6.5f * dp, t.dim, Fonts.text(context))
            text(canvas, context.getString(R.string.node_ping, n.third), (vw - 18) * dp, (y + 18) * dp, 7f * dp, if (i == 0) t.fg else t.dim, Fonts.monoBold(context), Paint.Align.RIGHT)
        }
    }

    // ---------------------------------------------------------------- общее

    /** drawNav — панель вкладок внизу; активная — по показанному экрану. */
    private fun drawNav(canvas: Canvas, dp: Float, vh: Float) {
        val t = theme
        val top = vh - 36
        brush.color = Look.withAlpha(t.surf, 0.92)
        canvas.drawRect(0f, top * dp, vw * dp, vh * dp, brush)
        brush.color = t.line
        canvas.drawRect(0f, top * dp, vw * dp, (top + 0.5f) * dp, brush)
        val names = listOf(R.string.nav_connect, R.string.nav_servers, R.string.nav_stats, R.string.nav_theme, R.string.nav_more)
        val active = when (screen) { Screen.MAIN -> 0; Screen.SERVERS -> 1; Screen.SETTINGS -> 4 }
        val cell = vw / 5
        for ((i, n) in names.withIndex()) {
            val cx = (i + 0.5f) * cell * dp
            if (i == active) {
                brush.color = t.accSoft
                canvas.drawRoundRect(cx - 11 * dp, (top + 6) * dp, cx + 11 * dp, (top + 20) * dp, 7 * dp, 7 * dp, brush)
            }
            brush.color = if (i == active) t.fg else t.dim
            canvas.drawCircle(cx, (top + 13) * dp, 2.5f * dp, brush)
            text(canvas, context.getString(n), cx, (top + 30) * dp, 5f * dp, if (i == active) t.fg else t.dim, if (i == active) Fonts.textBold(context) else Fonts.text(context), Paint.Align.CENTER)
        }
    }

    /** card — карточка в цветах и форме темы: скругление и подача — из неё. */
    private fun card(canvas: Canvas, dp: Float, l: Float, tp: Float, r: Float, b: Float, stroke: Int = theme.line) {
        val t = theme
        box.set(l * dp, tp * dp, r * dp, b * dp)
        val rad = t.r * dp * 0.7f
        if (t.card == "shadow") {
            brush.style = Paint.Style.FILL
            brush.color = 0x40000000
            canvas.drawRoundRect(box.left, box.top + 3 * dp, box.right, box.bottom + 3 * dp, rad, rad, brush)
        }
        brush.style = Paint.Style.FILL
        brush.color = t.surf
        canvas.drawRoundRect(box, rad, rad, brush)
        if (t.card != "flat" || stroke != t.line) {
            brush.style = Paint.Style.STROKE
            brush.strokeWidth = 0.6f * dp
            brush.color = stroke
            canvas.drawRoundRect(box, rad, rad, brush)
        }
        brush.style = Paint.Style.FILL
    }

    /** tile — квадратик под значок в строке: мягкий акцент, скругление темы. */
    private fun tile(canvas: Canvas, dp: Float, x: Float, y: Float, size: Float) {
        val t = theme
        box.set(x * dp, y * dp, (x + size) * dp, (y + size) * dp)
        val rad = minOf(t.r, 14) * dp * 0.7f
        brush.style = Paint.Style.FILL
        brush.color = t.accSoft
        canvas.drawRoundRect(box, rad, rad, brush)
        brush.color = t.acc
        val c = size / 2
        canvas.drawRoundRect((x + c - 3.5f) * dp, (y + c - 3.5f) * dp, (x + c + 3.5f) * dp, (y + c + 3.5f) * dp, 1.5f * dp, 1.5f * dp, brush)
    }

    private fun switchAt(canvas: Canvas, dp: Float, x: Float, y: Float, on: Boolean) {
        val t = theme
        box.set(x * dp, y * dp, (x + 22) * dp, (y + 12) * dp)
        brush.color = if (on) t.acc else t.surf2
        canvas.drawRoundRect(box, 6 * dp, 6 * dp, brush)
        brush.color = if (on) t.accFg else t.dim
        canvas.drawCircle((if (on) x + 16 else x + 6) * dp, (y + 6) * dp, 4 * dp, brush)
    }

    /** text — строка холстом; width — предел в пикселях холста, дальше многоточие. */
    private fun text(
        canvas: Canvas, s: String, x: Float, y: Float, size: Float, color: Int, face: Typeface,
        align: Paint.Align = Paint.Align.LEFT, letter: Float = 0f, width: Float = 0f,
    ) {
        brush.style = Paint.Style.FILL
        brush.color = color
        brush.textSize = size
        brush.typeface = face
        brush.textAlign = align
        brush.letterSpacing = letter
        val shown = if (width > 0) TextUtils.ellipsize(s, TextPaint(brush), width, TextUtils.TruncateAt.END).toString() else s
        canvas.drawText(shown, x, y, brush)
        brush.letterSpacing = 0f
        brush.textAlign = Paint.Align.LEFT
    }

    /** sample — строка страны как на главной: страна · город · отклик. */
    private fun sample(): String =
        context.getString(R.string.theme_preview_country) + " · " + context.getString(R.string.theme_preview_place)
}
