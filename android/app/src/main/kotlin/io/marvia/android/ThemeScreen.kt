package io.marvia.android

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Canvas
import android.graphics.Outline
import android.graphics.Paint as CanvasPaint
import android.graphics.RectF
import android.graphics.drawable.GradientDrawable
import android.view.Gravity
import android.view.View
import android.view.ViewOutlineProvider
import android.view.inputmethod.EditorInfo
import android.widget.FrameLayout
import android.widget.GridLayout
import android.widget.LinearLayout
import android.widget.SeekBar
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import io.marvia.android.databinding.ScreenThemeBinding
import kotlin.random.Random

/**
 * ThemeScreen — выбор вида: готовые виды, акцент, свет, скругления,
 * плотность, карточки, кнопка, свечение, профили и код темы.
 *
 * Каждый готовый вид показан мини-макетом главного экрана, а не квадратиком
 * цвета: человек выбирает не цвет, а то, как будет выглядеть телефон, и
 * квадратик об этом не говорит. Всё применяется сразу — экран, на котором
 * выбирают, и есть предпросмотр.
 *
 * Ручки те же, что в панели и окне на компьютере, и код темы подходит всем
 * троим: движок один, internal/look.
 */
class ThemeScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenThemeBinding,
    private val store: Store,
    /** Выбор изменился: перекрасить всё приложение. */
    private val onChanged: () -> Unit,
) {

    private val dp = host.resources.displayMetrics.density

    /** Слот профиля, в который пишет «сохранить сюда». */
    private var slot = 0

    init {
        ui.themeReset.setOnClickListener { choose(Look.Choice()) }
        ui.profileSave.setOnClickListener {
            val list = store.profiles.toMutableList()
            list[slot] = Look.encode(store.look)
            store.profiles = list
            paint(Look.theme(store.look))
        }
        ui.themeRandom.setOnClickListener { choose(random()) }
        ui.themeCode.setOnClickListener {
            host.getSystemService(ClipboardManager::class.java)
                ?.setPrimaryClip(ClipData.newPlainText("marvia-look", Look.encode(store.look)))
            Toast.makeText(host, R.string.theme_code_copied, Toast.LENGTH_SHORT).show()
        }
        ui.codeInput.setOnEditorActionListener { v, action, _ ->
            if (action == EditorInfo.IME_ACTION_DONE) {
                applyCode(v.text.toString())
                true
            } else {
                false
            }
        }
        ui.depthSeek.setOnSeekBarChangeListener(object : SeekBar.OnSeekBarChangeListener {
            override fun onProgressChanged(bar: SeekBar, progress: Int, fromUser: Boolean) {
                if (fromUser) choose(store.look.copy(depth = (progress + 8) / 100.0))
            }
            override fun onStartTrackingTouch(bar: SeekBar) = Unit
            override fun onStopTrackingTouch(bar: SeekBar) = Unit
        })
    }

    private fun choose(next: Look.Choice) {
        store.look = Look.normalize(next)
        onChanged()
    }

    /** applyCode применяет код темы; чужой — говорит об этом, а не красит в серый. */
    private fun applyCode(text: String) {
        val next = Look.decode(text)
        ui.codeBad.isVisible = next == null
        if (next == null) return
        ui.codeInput.setText("")
        choose(next)
    }

    private fun random(): Look.Choice {
        val r = Random.Default
        fun <T> pick(list: List<T>): T = list[r.nextInt(list.size)]
        return Look.Choice(
            preset = pick(LookTable.presets).key,
            accent = pick(listOf(0) + LookTable.accents),
            kind = pick(listOf("linear", "radial", "aurora")),
            dir = pick(LookTable.dirs).key,
            depth = (20 + r.nextInt(76)) / 100.0,
            tint = pick(listOf(0, 0) + LookTable.accents),
            radius = pick(LookTable.radii).key,
            density = pick(LookTable.densities).key,
            btn = pick(LookTable.buttons),
            glow = pick(LookTable.glows).key,
            card = pick(LookTable.cards),
        )
    }

    /** paint перерисовывает выбор под текущую тему. Зовётся при каждой смене. */
    fun paint(t: Theme) {
        val choice = store.look
        paintLooks(t, choice)
        paintSwatches(ui.accentRows, t, LookTable.accents, choice.accent) { choose(choice.copy(accent = it)) }
        paintLight(t, choice)
        paintPills(ui.radiusRow, t, LookTable.radii.map { it.key }, choice.radius, ::radiusName) { choose(choice.copy(radius = it)) }
        paintPills(ui.densityRow, t, LookTable.densities.map { it.key }, choice.density, ::densityName) { choose(choice.copy(density = it)) }
        paintPills(ui.cardRow, t, LookTable.cards, choice.card, ::cardName) { choose(choice.copy(card = it)) }
        paintPills(ui.buttonRow, t, LookTable.buttons, choice.btn, ::btnName) { choose(choice.copy(btn = it)) }
        paintPills(ui.glowRow, t, LookTable.glows.map { it.key }, choice.glow, ::glowName) { choose(choice.copy(glow = it)) }
        paintProfiles(t)

        ui.themeCode.text = Look.encode(choice)
        ui.themeCode.background = GradientDrawable().apply {
            cornerRadius = t.r * dp
            setColor(t.surf2)
            setStroke((1 * dp).toInt(), t.acc, 4 * dp, 3 * dp)
        }
        ui.codeInput.background = GradientDrawable().apply {
            cornerRadius = t.r * dp
            setColor(t.surf)
            setStroke((1 * dp).toInt(), t.line)
        }
    }

    // ---------------------------------------------------------------- виды

    private fun paintLooks(t: Theme, choice: Look.Choice) {
        val grid = ui.looksGrid
        grid.removeAllViews()

        for (row in LookTable.looks.chunked(COLUMNS)) {
            val line = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                layoutParams = LinearLayout.LayoutParams(MATCH, WRAP).apply { topMargin = (8 * dp).toInt() }
            }
            for ((i, lk) in row.withIndex()) {
                line.addView(lookCard(t, lk, Look.same(choice, lk)), cell(i))
            }
            // Неполный ряд добивается пустыми ячейками, чтобы карточки не растянулись.
            repeat(COLUMNS - row.size) { i -> line.addView(View(host), cell(row.size + i)) }
            grid.addView(line)
        }
    }

    private fun cell(index: Int) = LinearLayout.LayoutParams(0, WRAP, 1f).apply {
        if (index > 0) marginStart = (8 * dp).toInt()
    }

    private fun lookCard(t: Theme, lk: LookTable.Look, on: Boolean): View {
        val preview = Look.theme(Look.ofLook(lk))
        val wrap = LinearLayout(host).apply {
            orientation = LinearLayout.VERTICAL
            val pad = (3 * dp).toInt()
            setPadding(pad, pad, pad, pad)
            background = GradientDrawable().apply {
                cornerRadius = (t.r + 3) * dp
                setColor(if (on) t.accSoft else 0)
                setStroke(((if (on) 2 else 1) * dp).toInt(), if (on) t.acc else t.line)
            }
            isClickable = true
            isFocusable = true
            // Вид задаёт всё разом; акцент и оттенок сбрасываются: у каждого свой.
            setOnClickListener { choose(Look.ofLook(lk)) }
        }

        val canvas = LookCanvas(host, preview, maxOf(t.r - 3, 5) * dp)
        wrap.addView(canvas, LinearLayout.LayoutParams(MATCH, (82 * dp).toInt()))

        val label = TextView(host).apply {
            text = lookName(lk)
            textSize = 11f
            gravity = Gravity.CENTER
            maxLines = 1
            setTextColor(if (on) t.acc else t.dim)
            setPadding(0, (5 * dp).toInt(), 0, (2 * dp).toInt())
        }
        wrap.addView(label, LinearLayout.LayoutParams(MATCH, WRAP))
        return wrap
    }

    /**
     * lookName — название вида: по ключу пресета из строк приложения. Пресет
     * встречается в видах дважды (серый мягкий и серый ровный) — второму
     * добавляется характер света, чтобы карточки не назывались одинаково.
     */
    private fun lookName(lk: LookTable.Look): String {
        val name = presetName(lk.preset)
        val twice = LookTable.looks.count { it.preset == lk.preset } > 1
        return if (twice && lk != LookTable.looks.first { it.preset == lk.preset }) name + " · " + kindName(lk.kind) else name
    }

    /**
     * presetName — название из строк приложения по ключу пресета.
     *
     * Ключ — рабочее слово из таблицы, а название переводится. Пресет без
     * названия покажет ключ: лучше «teal» на экране, чем падение из-за
     * пропущенной строки, но тест в internal/look до этого не допустит.
     */
    private fun presetName(key: String): String {
        val id = host.resources.getIdentifier("look_$key", "string", host.packageName)
        return if (id == 0) key else host.getString(id)
    }

    private fun named(prefix: String, key: String): String {
        val id = host.resources.getIdentifier(prefix + key, "string", host.packageName)
        return if (id == 0) key else host.getString(id)
    }

    private fun densityName(key: String) = named("density_", key)
    private fun radiusName(key: String) = named("radius_", key)
    private fun cardName(key: String) = named("card_", key)
    private fun btnName(key: String) = named("btn_", key)
    private fun glowName(key: String) = named("glow_", key)
    private fun kindName(key: String) = named("kind_", key)
    private fun dirName(key: String) = named("dir_", key)

    // ---------------------------------------------------------------- цвет

    /** paintSwatches — ряды плиток цвета; first — «как в пресете» (0), если он в списке. */
    private fun paintSwatches(rows: LinearLayout, t: Theme, colors: List<Int>, current: Int, onPick: (Int) -> Unit) {
        rows.removeAllViews()
        // Выбранный обведён цветом текста: обводка тем же цветом, что и
        // плитка, была бы невидима. Ноль — «как в пресете» — плитка с
        // градиентом акцента.
        for ((n, row) in colors.chunked(SWATCHES_PER_ROW).withIndex()) {
            val line = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                layoutParams = LinearLayout.LayoutParams(MATCH, WRAP).apply {
                    if (n > 0) topMargin = (8 * dp).toInt()
                }
            }
            for ((i, color) in row.withIndex()) {
                val on = color == current
                val swatch = View(host).apply {
                    background = GradientDrawable().apply {
                        cornerRadius = 10 * dp
                        if (color == 0) {
                            orientation = GradientDrawable.Orientation.TL_BR
                            setColors(intArrayOf(t.acc, Look.mix(t.acc, 0xFF000000.toInt(), 0.6)))
                        } else {
                            setColor(color)
                        }
                        if (on) setStroke((2 * dp).toInt(), t.fg)
                    }
                    isClickable = true
                    isFocusable = true
                    setOnClickListener { onPick(color) }
                }
                val size = (32 * dp).toInt()
                line.addView(swatch, LinearLayout.LayoutParams(size, size).apply {
                    if (i > 0) marginStart = (8 * dp).toInt()
                })
            }
            rows.addView(line)
        }
    }

    // ---------------------------------------------------------------- свет

    private fun paintLight(t: Theme, choice: Look.Choice) {
        // Лампа: экран, залитый настоящим фоном; нажатие в угол переносит свет.
        // Ровный фон света не показывает — лампа рисует мягкий, чтобы было видно, куда нажимать.
        val litKind = if (choice.kind == "flat") "linear" else choice.kind
        ui.lamp.background = Backdrop(Look.theme(choice.copy(kind = litKind)))
        ui.lamp.outlineProvider = rounded(t.r * dp)
        ui.lamp.clipToOutline = true
        ui.lamp.foreground = GradientDrawable().apply {
            cornerRadius = t.r * dp
            setColor(0)
            setStroke((1 * dp).toInt(), t.line)
        }

        val zones = ui.lampZones
        zones.removeAllViews()
        for (key in LAMP) {
            val on = choice.dir == key
            val sun = View(host).apply {
                background = GradientDrawable().apply {
                    shape = GradientDrawable.OVAL
                    setColor(if (on) t.acc else Look.withAlpha(t.fg, 0.22))
                    if (on) setStroke((5 * dp).toInt(), t.accSoft)
                }
            }
            val size = ((if (on) 30 else 8) * dp).toInt()
            val cellView = FrameLayout(host).apply {
                addView(sun, FrameLayout.LayoutParams(size, size, Gravity.CENTER))
                isClickable = true
                isFocusable = true
                setOnClickListener { choose(choice.copy(dir = key, kind = litKind)) }
            }
            zones.addView(cellView, GridLayout.LayoutParams().apply {
                width = 0
                height = 0
                columnSpec = GridLayout.spec(GridLayout.UNDEFINED, 1f)
                rowSpec = GridLayout.spec(GridLayout.UNDEFINED, 1f)
            })
        }
        ui.lightNote.text = if (choice.kind == "flat") host.getString(R.string.theme_light_off) else dirName(choice.dir)

        // Характер света: четыре мини-фона.
        val kinds = ui.kindsRow
        kinds.removeAllViews()
        for ((i, kind) in LookTable.kinds.withIndex()) {
            val on = choice.kind == kind
            val wrap = LinearLayout(host).apply {
                orientation = LinearLayout.VERTICAL
                val pad = (3 * dp).toInt()
                setPadding(pad, pad, pad, pad)
                background = GradientDrawable().apply {
                    cornerRadius = (t.r + 3) * dp
                    setColor(if (on) t.accSoft else 0)
                    setStroke(((if (on) 2 else 1) * dp).toInt(), if (on) t.acc else t.line)
                }
                isClickable = true
                isFocusable = true
                setOnClickListener { choose(choice.copy(kind = kind)) }
            }
            val canvas = View(host).apply {
                background = Backdrop(Look.theme(choice.copy(kind = kind)))
                outlineProvider = rounded(maxOf(t.r - 3, 5) * dp)
                clipToOutline = true
            }
            wrap.addView(canvas, LinearLayout.LayoutParams(MATCH, (44 * dp).toInt()))
            wrap.addView(
                TextView(host).apply {
                    text = kindName(kind)
                    textSize = 10f
                    gravity = Gravity.CENTER
                    setTextColor(if (on) t.acc else t.dim)
                    setPadding(0, (4 * dp).toInt(), 0, (1 * dp).toInt())
                },
                LinearLayout.LayoutParams(MATCH, WRAP),
            )
            kinds.addView(wrap, LinearLayout.LayoutParams(0, WRAP, 1f).apply {
                if (i > 0) marginStart = (6 * dp).toInt()
            })
        }

        // Густота тени: ползунок 8…98 — SeekBar считает от нуля, отсюда сдвиг.
        ui.depthSeek.progress = ((choice.depth * 100).toInt() - 8).coerceIn(0, 90)
        ui.depthSeek.progressTintList = android.content.res.ColorStateList.valueOf(t.acc)
        ui.depthSeek.thumbTintList = android.content.res.ColorStateList.valueOf(t.acc)
        ui.depthSeek.progressBackgroundTintList = android.content.res.ColorStateList.valueOf(t.line)
        val d = choice.depth
        ui.depthNote.text = host.getString(
            when {
                d <= 0.3 -> R.string.depth_0
                d <= 0.5 -> R.string.depth_1
                d <= 0.7 -> R.string.depth_2
                d <= 0.85 -> R.string.depth_3
                else -> R.string.depth_4
            },
        )

        paintSwatches(ui.tintRows, t, listOf(0) + LookTable.accents, choice.tint) { choose(choice.copy(tint = it)) }
    }

    private fun rounded(radius: Float) = object : ViewOutlineProvider() {
        override fun getOutline(view: View, outline: Outline) {
            outline.setRoundRect(0, 0, view.width, view.height, radius)
        }
    }

    // ------------------------------------------------------------- таблетки

    private fun paintPills(row: LinearLayout, t: Theme, keys: List<String>, current: String, name: (String) -> String, onPick: (String) -> Unit) {
        row.removeAllViews()
        for ((i, k) in keys.withIndex()) {
            val on = k == current
            val pill = TextView(host).apply {
                text = name(k)
                textSize = 12f
                gravity = Gravity.CENTER
                maxLines = 1
                setPadding((4 * dp).toInt(), (9 * dp).toInt(), (4 * dp).toInt(), (9 * dp).toInt())
                setTextColor(if (on) t.accFg else t.dim)
                background = Paint.rounded(if (on) t.acc else t.surf2, minOf(t.r, 14), dp)
                isClickable = true
                isFocusable = true
                setOnClickListener { onPick(k) }
            }
            row.addView(pill, LinearLayout.LayoutParams(0, WRAP, 1f).apply {
                if (i > 0) marginStart = (6 * dp).toInt()
            })
        }
    }

    // ------------------------------------------------------------- профили

    private fun paintProfiles(t: Theme) {
        val codes = store.profiles
        val row = ui.profileRow
        row.removeAllViews()
        for (i in 0 until 3) {
            val on = slot == i
            val code = codes[i]
            val pill = TextView(host).apply {
                text = host.getString(if (code != null) R.string.profile_full else R.string.profile_empty, i + 1)
                textSize = 12f
                gravity = Gravity.CENTER
                setPadding((4 * dp).toInt(), (9 * dp).toInt(), (4 * dp).toInt(), (9 * dp).toInt())
                setTextColor(if (on) t.accFg else t.dim)
                background = Paint.rounded(if (on) t.acc else t.surf2, minOf(t.r, 14), dp)
                isClickable = true
                isFocusable = true
                setOnClickListener {
                    slot = i
                    val next = code?.let { Look.decode(it) }
                    if (next != null) choose(next) else paint(t)
                }
            }
            row.addView(pill, LinearLayout.LayoutParams(0, WRAP, 1f).apply {
                if (i > 0) marginStart = (6 * dp).toInt()
            })
        }
        ui.profileSave.background = Paint.rounded(t.acc, minOf(t.r, 14), dp)
        ui.profileSave.setTextColor(t.accFg)
        ui.themeRandom.background = Paint.rounded(t.surf2, minOf(t.r, 14), dp)
        ui.themeRandom.setTextColor(t.fg)
    }

    /**
     * LookCanvas — мини-макет главного экрана в цветах вида: фон по свету,
     * шапка, кнопка питания и две карточки. Рисуется, а не собирается из
     * вьюх: в сетке их двадцать восемь, и по шесть вьюх на каждый — это
     * полторы сотни вьюх ради картинки размером с ноготь.
     */
    private class LookCanvas(context: Context, private val t: Theme, private val radius: Float) : View(context) {
        private val brush = CanvasPaint(CanvasPaint.ANTI_ALIAS_FLAG)
        private val box = RectF()

        init {
            background = Backdrop(t)
            outlineProvider = object : ViewOutlineProvider() {
                override fun getOutline(view: View, outline: Outline) {
                    outline.setRoundRect(0, 0, view.width, view.height, radius)
                }
            }
            clipToOutline = true
        }

        override fun onDraw(canvas: Canvas) {
            val w = width.toFloat()
            val h = height.toFloat()
            val dp = resources.displayMetrics.density

            // Шапка — полоска текста, полупрозрачная.
            brush.style = CanvasPaint.Style.FILL
            brush.color = Look.withAlpha(t.fg, 0.5)
            box.set(w * 0.09f, h * 0.08f, w * 0.63f, h * 0.14f)
            canvas.drawRoundRect(box, h, h, brush)

            // Кнопка питания: кольцо акцентом, диск — залитый.
            brush.style = CanvasPaint.Style.STROKE
            brush.strokeWidth = 3 * dp
            brush.color = t.acc
            canvas.drawCircle(w / 2, h * 0.36f, 12 * dp, brush)
            if (t.btn == "solid") {
                brush.style = CanvasPaint.Style.FILL
                canvas.drawCircle(w / 2, h * 0.36f, 12 * dp, brush)
            }

            // Две карточки внизу: поверхность и поверхность потемнее.
            brush.style = CanvasPaint.Style.FILL
            val r = t.r * dp * 0.3f
            brush.color = t.surf
            box.set(w * 0.09f, h * 0.66f, w * 0.91f, h * 0.79f)
            canvas.drawRoundRect(box, r, r, brush)
            brush.color = t.surf2
            box.set(w * 0.09f, h * 0.84f, w * 0.91f, h * 0.94f)
            canvas.drawRoundRect(box, r, r, brush)
        }
    }

    private companion object {
        const val COLUMNS = 3
        const val SWATCHES_PER_ROW = 7
        const val MATCH = LinearLayout.LayoutParams.MATCH_PARENT
        const val WRAP = LinearLayout.LayoutParams.WRAP_CONTENT
        val LAMP = listOf("nw", "n", "ne", "w", "c", "e", "sw", "s", "se")
    }
}
