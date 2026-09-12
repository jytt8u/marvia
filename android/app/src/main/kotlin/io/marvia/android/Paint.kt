package io.marvia.android

import android.content.res.ColorStateList
import android.graphics.drawable.GradientDrawable
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.ImageView
import android.widget.TextView
import androidx.core.widget.ImageViewCompat
import com.google.android.material.materialswitch.MaterialSwitch

/**
 * Paint красит дерево вьюх по теме.
 *
 * Вьюха говорит, кто она, тегом в разметке: `card`, `dim`, `icon:acc`. Здесь
 * по тегу ставится цвет, фон или тонировка. Так новый экран получает тему
 * тегами, а не полусотней строк кода, — и ни одна вьюха не знает, что такое
 * пресет.
 *
 * Почему не стили и не атрибуты темы Android: они читаются один раз при
 * надувании разметки и меняются только пересозданием экрана. Двадцать семь
 * пресетов с акцентом и плотностью, которые человек щёлкает и смотрит, как
 * меняется экран, через них не сделать — это статические стили, а у нас
 * значения считаются на лету.
 *
 * То, что зависит от состояния, — цвет кнопки питания, отметка выбранной
 * страны, цвет пинга, — красится кодом экрана по тому же объекту Theme.
 */
object Paint {

    /** Словарь тегов. Один тег на вьюху; составные роли — отдельными вьюхами. */
    private const val BG = "bg"
    private const val SURF = "surf"
    private const val CARD = "card"
    private const val CARD_PAD = "card.pad"
    private const val LINE = "line"
    private const val FG = "fg"
    private const val DIM = "dim"
    private const val ACC = "acc"
    private const val FAIL = "fail"
    private const val BTN = "btn"
    private const val CHIP = "chip"
    private const val TRACK = "track"
    private const val FILL = "fill"
    private const val GAP = "gap"
    private const val SWITCH = "switch"
    private const val ICON = "icon:"

    fun apply(root: View, t: Theme) {
        walk(root) { view -> paint(view, t) }
    }

    private fun walk(view: View, each: (View) -> Unit) {
        each(view)
        if (view is ViewGroup) {
            for (i in 0 until view.childCount) {
                walk(view.getChildAt(i), each)
            }
        }
    }

    private fun paint(v: View, t: Theme) {
        val tag = v.tag as? String ?: return
        val dp = v.resources.displayMetrics.density

        when {
            tag == BG -> v.background = Backdrop(t)
            tag == SURF -> v.setBackgroundColor(t.surf)
            tag == LINE -> v.setBackgroundColor(t.line)
            tag == CARD -> skin(v, t, dp)
            tag == CARD_PAD -> {
                skin(v, t, dp)
                val pad = (t.pad * dp).toInt()
                v.setPadding(pad, pad, pad, pad)
            }
            tag == FG -> text(v, t.fg, t.dim)
            tag == DIM -> text(v, t.dim, t.dim)
            tag == ACC -> text(v, t.acc, t.dim)
            tag == FAIL -> text(v, t.fail, t.dim)
            tag == BTN -> {
                v.background = rounded(t.acc, t.r, dp)
                text(v, t.accFg, t.accFg)
            }
            tag == CHIP -> {
                v.background = rounded(t.surf2, minOf(t.r, 10), dp)
                text(v, t.dim, t.dim)
            }
            tag == TRACK -> v.background = rounded(t.bg, 999, dp)
            tag == FILL -> v.setBackgroundColor(t.acc)
            tag == GAP -> (v.layoutParams as? ViewGroup.MarginLayoutParams)?.let {
                it.topMargin = (t.gap * dp).toInt()
                v.layoutParams = it
            }
            tag == SWITCH && v is MaterialSwitch -> paintSwitch(v, t)
            tag.startsWith(ICON) && v is ImageView -> {
                val color = when (tag.removePrefix(ICON)) {
                    "fg" -> t.fg
                    "acc" -> t.acc
                    "accFg" -> t.accFg
                    else -> t.dim
                }
                ImageViewCompat.setImageTintList(v, ColorStateList.valueOf(color))
            }
        }
    }

    private fun text(v: View, color: Int, hint: Int) {
        if (v is TextView) {
            v.setTextColor(color)
            if (v is EditText) {
                v.setHintTextColor(hint)
            }
        }
    }

    /**
     * Карточка: поверхность, рамка и скругление по теме. Рамка — по подаче
     * карточек: у плоских она прозрачная, но той же толщины, чтобы ничего не
     * прыгало при переключении. Выбранная карточка обводится акцентом при
     * любой подаче: отметка выбора важнее стиля.
     */
    fun card(t: Theme, dp: Float, stroke: Int = t.line): GradientDrawable {
        val line = if (stroke == t.line && t.card == "flat") 0 else stroke
        return rounded(t.surf, t.r, dp).apply { setStroke((1 * dp).toInt().coerceAtLeast(1), line) }
    }

    /**
     * skin красит карточку и ставит тень, если подача «с тенью». Тень —
     * системная, через elevation: своей у GradientDrawable нет, а рисовать
     * размытие руками ради карточки — дорого и на глаз не лучше.
     */
    private fun skin(v: View, t: Theme, dp: Float) {
        v.background = card(t, dp)
        v.elevation = if (t.card == "shadow") 6 * dp else 0f
        if (t.card == "shadow") {
            v.outlineProvider = android.view.ViewOutlineProvider.BACKGROUND
            v.clipToOutline = false
        }
    }

    fun rounded(color: Int, radiusDp: Int, dp: Float): GradientDrawable =
        GradientDrawable().apply {
            shape = GradientDrawable.RECTANGLE
            setColor(color)
            cornerRadius = radiusDp * dp
        }

    fun circle(color: Int): GradientDrawable =
        GradientDrawable().apply {
            shape = GradientDrawable.OVAL
            setColor(color)
        }

    /** Обод отметки выбора: пустой — линией, выбранный — акцентом. */
    fun ring(t: Theme, dp: Float, chosen: Boolean): GradientDrawable =
        GradientDrawable().apply {
            shape = GradientDrawable.OVAL
            setColor(if (chosen) t.acc else 0)
            setStroke((1.6f * dp).toInt().coerceAtLeast(1), if (chosen) t.acc else t.line)
        }

    /**
     * Переключатель красится руками: Material берёт цвет из атрибутов темы
     * Android, а те заданы один раз при надувании и наш пресет не видят.
     */
    private fun paintSwitch(s: MaterialSwitch, t: Theme) {
        val states = arrayOf(
            intArrayOf(android.R.attr.state_checked),
            intArrayOf(),
        )
        s.thumbTintList = ColorStateList(states, intArrayOf(t.accFg, t.dim))
        s.trackTintList = ColorStateList(states, intArrayOf(t.acc, t.surf2))
        s.trackDecorationTintList = ColorStateList(states, intArrayOf(t.acc, t.line))
    }
}
