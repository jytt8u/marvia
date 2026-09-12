package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint as CanvasPaint
import android.graphics.RectF
import android.graphics.drawable.GradientDrawable
import android.view.Gravity
import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import io.marvia.android.databinding.ScreenThemeBinding

/**
 * ThemeScreen — выбор вида: пресет, акцент, плотность.
 *
 * Каждый пресет показан мини-макетом главного экрана, а не квадратиком
 * цвета: человек выбирает не цвет, а то, как будет выглядеть телефон, и
 * квадратик об этом не говорит. Всё применяется сразу — экран, на котором
 * выбирают, и есть предпросмотр.
 *
 * Чего здесь нет из макета: света, оттенка, стиля кнопки, свечения, шрифта,
 * профилей и кода темы. Движок общий с панелью и окном на компьютере, и
 * этих ручек в нём пока нет; появятся там — появятся и здесь.
 */
class ThemeScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenThemeBinding,
    private val store: Store,
    /** Выбор изменился: перекрасить всё приложение. */
    private val onChanged: () -> Unit,
) {

    private val dp = host.resources.displayMetrics.density

    init {
        ui.themeReset.setOnClickListener { choose(Look.Choice()) }
    }

    private fun choose(next: Look.Choice) {
        store.look = next
        onChanged()
    }

    /** paint перерисовывает выбор под текущую тему. Зовётся при каждой смене. */
    fun paint(t: Theme) {
        val choice = store.look
        paintLooks(t, choice)
        paintAccents(t, choice)
        paintDensities(t, choice)
    }

    private fun paintLooks(t: Theme, choice: Look.Choice) {
        val grid = ui.looksGrid
        grid.removeAllViews()

        for (row in LookTable.presets.chunked(COLUMNS)) {
            val line = LinearLayout(host).apply {
                orientation = LinearLayout.HORIZONTAL
                layoutParams = LinearLayout.LayoutParams(MATCH, WRAP).apply { topMargin = (8 * dp).toInt() }
            }
            for ((i, p) in row.withIndex()) {
                line.addView(lookCard(t, p, p.key == choice.preset), cell(i))
            }
            // Неполный ряд добивается пустыми ячейками, чтобы карточки не растянулись.
            repeat(COLUMNS - row.size) { i -> line.addView(View(host), cell(row.size + i)) }
            grid.addView(line)
        }
    }

    private fun cell(index: Int) = LinearLayout.LayoutParams(0, WRAP, 1f).apply {
        if (index > 0) marginStart = (8 * dp).toInt()
    }

    private fun lookCard(t: Theme, p: LookTable.Preset, on: Boolean): View {
        val preview = Look.theme(Look.Choice(preset = p.key, density = store.look.density))
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
            setOnClickListener { choose(store.look.copy(preset = p.key, accent = 0)) }
        }

        val canvas = LookCanvas(host, preview, maxOf(t.r - 3, 5) * dp)
        wrap.addView(canvas, LinearLayout.LayoutParams(MATCH, (82 * dp).toInt()))

        val label = TextView(host).apply {
            text = presetName(p.key)
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

    private fun paintAccents(t: Theme, choice: Look.Choice) {
        val rows = ui.accentRows
        rows.removeAllViews()
        val current = t.acc

        for ((n, row) in LookTable.accents.chunked(ACCENTS_PER_ROW).withIndex()) {
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
                        setColor(color)
                        // Выбранный обведён цветом текста: обводка тем же
                        // цветом, что и плитка, была бы невидима.
                        if (on) setStroke((2 * dp).toInt(), t.fg)
                    }
                    isClickable = true
                    isFocusable = true
                    setOnClickListener { choose(choice.copy(accent = color)) }
                }
                val size = (32 * dp).toInt()
                line.addView(swatch, LinearLayout.LayoutParams(size, size).apply {
                    if (i > 0) marginStart = (8 * dp).toInt()
                })
            }
            rows.addView(line)
        }
    }

    private fun paintDensities(t: Theme, choice: Look.Choice) {
        val row = ui.densityRow
        row.removeAllViews()
        for ((i, d) in LookTable.densities.withIndex()) {
            val on = d.key == choice.density
            val pill = TextView(host).apply {
                text = densityName(d.key)
                textSize = 12f
                gravity = Gravity.CENTER
                setPadding(0, (9 * dp).toInt(), 0, (9 * dp).toInt())
                setTextColor(if (on) t.accFg else t.dim)
                background = Paint.rounded(if (on) t.acc else t.surf2, minOf(t.r, 14), dp)
                isClickable = true
                isFocusable = true
                setOnClickListener { choose(choice.copy(density = d.key)) }
            }
            row.addView(pill, LinearLayout.LayoutParams(0, WRAP, 1f).apply {
                if (i > 0) marginStart = (6 * dp).toInt()
            })
        }
    }

    private fun densityName(key: String): String = when (key) {
        "compact" -> host.getString(R.string.density_compact)
        "roomy" -> host.getString(R.string.density_roomy)
        else -> host.getString(R.string.density_normal)
    }

    /**
     * LookCanvas — мини-макет главного экрана в цветах пресета: шапка, круг
     * питания и две карточки. Рисуется, а не собирается из вьюх: в сетке их
     * двадцать семь, и по шесть вьюх на каждый — это полторы сотни вьюх ради
     * картинки размером с ноготь.
     */
    private class LookCanvas(context: Context, private val t: Theme, private val radius: Float) : View(context) {
        private val brush = CanvasPaint(CanvasPaint.ANTI_ALIAS_FLAG)
        private val box = RectF()

        override fun onDraw(canvas: Canvas) {
            val w = width.toFloat()
            val h = height.toFloat()
            val dp = resources.displayMetrics.density

            brush.style = CanvasPaint.Style.FILL
            brush.color = t.bg
            box.set(0f, 0f, w, h)
            canvas.drawRoundRect(box, radius, radius, brush)

            // Шапка — полоска текста, полупрозрачная.
            brush.color = Look.withAlpha(t.fg, 0.5)
            box.set(w * 0.09f, h * 0.08f, w * 0.63f, h * 0.14f)
            canvas.drawRoundRect(box, h, h, brush)

            // Круг питания — кольцо акцентом.
            brush.style = CanvasPaint.Style.STROKE
            brush.strokeWidth = 3 * dp
            brush.color = t.acc
            canvas.drawCircle(w / 2, h * 0.36f, 12 * dp, brush)

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
        const val ACCENTS_PER_ROW = 7
        const val MATCH = LinearLayout.LayoutParams.MATCH_PARENT
        const val WRAP = LinearLayout.LayoutParams.WRAP_CONTENT
    }
}
