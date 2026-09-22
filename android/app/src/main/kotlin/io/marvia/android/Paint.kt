package io.marvia.android

import android.content.res.ColorStateList
import android.graphics.drawable.GradientDrawable
import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.ImageView
import android.widget.TextView
import androidx.core.widget.ImageViewCompat
import androidx.core.graphics.ColorUtils
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
    private const val NAVBAR = "navbar"
    private const val CARD = "card"
    private const val CARD_PAD = "card.pad"
    private const val LINE = "line"
    private const val FG = "fg"
    private const val DIM = "dim"
    private const val ACC = "acc"
    private const val FAIL = "fail"
    private const val BTN = "btn"
    private const val CHIP = "chip"
    private const val ICON_BOX = "icon.box"
    private const val TRACK = "track"
    private const val FILL = "fill"
    private const val GAP = "gap"
    private const val SWITCH = "switch"
    private const val ICON = "icon:"

    /**
     * Style — ручки, которых нет в теме: узор фона и шрифт. Одна на всё
     * приложение: её ставит MainActivity перед покраской, и экраны, что
     * собирают строки позже, красят их тем же.
     */
    data class Style(val pattern: String = Store.PATTERN_DOTS, val font: String = Store.FONT_ONEST, val photo: Boolean = false)

    var style = Style()

    fun apply(root: View, t: Theme) {
        walk(root) { view -> paint(view, t) }
    }

    /**
     * typeface — шрифт по ключу с нужной жирностью. Моноширинные и знак
     * MARVIA не трогаем: у них своя роль, и «Manrope» в цифрах отклика был
     * бы не сменой шрифта, а поломкой таблицы.
     */
    private val families = HashMap<String, android.graphics.Typeface?>()

    private fun font(v: TextView) {
        val old = v.typeface
        val p = v.paint
        // Моноширинный узнаём по ширине: у него «i» и «W» одинаковы.
        if (p.measureText("i") == p.measureText("W")) return
        if (v.getTag(R.id.keep_font) == true) return
        val key = style.font
        val family = families.getOrPut(key) {
            val id = when (key) {
                "manrope" -> R.font.manrope
                "geologica" -> R.font.geologica
                else -> R.font.onest
            }
            androidx.core.content.res.ResourcesCompat.getFont(v.context, id)
        } ?: return
        val bold = old?.isBold == true
        v.typeface = android.graphics.Typeface.create(family, if (bold) android.graphics.Typeface.BOLD else android.graphics.Typeface.NORMAL)
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
        if (v.isClickable && v !is android.widget.EditText && v !is MaterialSwitch && v !is PowerButton) {
            v.foreground = android.graphics.drawable.RippleDrawable(ColorStateList.valueOf(t.accSoft), null, rounded(android.graphics.Color.WHITE, t.r, v.resources.displayMetrics.density))
            // Нажатие чуть вжимает кнопку, как в макете (scale .96): без этого
            // экран отвечает только рябью, и кажется, что ничего не произошло.
            if (v.stateListAnimator == null) v.stateListAnimator = pressAnimator(v)
        }
        // Знак красится сам: у него не тон, а светотень, и тегом её не передать.
        if (v is MarviaLogoView) {
            v.setTheme(t)
            return
        }
        if (v is TextView) {
            font(v)
            legible(v, t)
        }
        val tags = v.tag as? String ?: return
        val dp = v.resources.displayMetrics.density

        // Тегов может быть несколько через пробел: «card gap» — карточка с
        // зазором от соседа по плотности темы.
        for (tag in tags.split(' ')) paintTag(v, t, tag, dp)
    }

    /**
     * legible — строка прямо на своём фото, а не в карточке, получает тень.
     *
     * Пелена над фото выравнивает общий тон, но пёстрый снимок всё равно
     * съедал приглушённые подписи — «Всё применяется сразу» и подсказки под
     * вкладками на живом телефоне было не прочесть. Тень цветом фона темы
     * отделяет буквы от любого снимка и не видна на ровном фоне; в
     * карточках её нет — там свой непрозрачный фон.
     */
    private fun legible(v: TextView, t: Theme) {
        val ours = v.getTag(R.id.photo_shadow) == true
        val want = style.photo && !insideCard(v)
        if (want) {
            val dp = v.resources.displayMetrics.density
            v.setShadowLayer(5 * dp, 0f, 1 * dp, ColorUtils.setAlphaComponent(t.bg, 235))
            v.setTag(R.id.photo_shadow, true)
        } else if (ours) {
            v.setShadowLayer(0f, 0f, 0f, 0)
            v.setTag(R.id.photo_shadow, false)
        }
    }

    private fun insideCard(v: View): Boolean {
        var p = v.parent
        while (p is View) {
            val tag = p.tag as? String
            if (tag != null && tag.split(' ').any { it == CARD || it == CARD_PAD || it == NAVBAR || it == CHIP || it == BTN }) return true
            p = p.parent
        }
        return false
    }

    private fun paintTag(v: View, t: Theme, tag: String, dp: Float) {
        when {
            tag == BG -> v.background = Backdrop(t)
            tag == SURF -> v.setBackgroundColor(t.surf)
            // Нижняя панель — средний тон фона, чуть прозрачный, как в макете.
            tag == NAVBAR -> v.background = rounded(t.surf, 24, dp).apply {
                setStroke(dp.toInt().coerceAtLeast(1), ColorUtils.setAlphaComponent(t.acc, 38))
            }
            tag == LINE -> v.setBackgroundColor(t.line)
            // Карточка — и отступ внутри по плотности темы: «плотно», «обычно»,
            // «просторно» должны быть видны, а не только числом в таблице.
            tag == CARD -> {
                skin(v, t, dp)
                if (v is ViewGroup && v !is android.widget.FrameLayout) {
                    val side = (t.pad * dp).toInt()
                    val tall = ((t.pad - 2) * dp).toInt()
                    v.setPadding(side, tall, side, tall)
                }
            }
            tag == CARD_PAD -> {
                skin(v, t, dp)
                val pad = (t.pad * dp).toInt()
                v.setPadding(pad, pad, pad, pad)
            }
            tag == "feature" -> v.background = card(t, dp).apply {
                colors = intArrayOf(ColorUtils.blendARGB(t.surf, t.acc, if (t.dark) .18f else .10f), t.surf)
                orientation = GradientDrawable.Orientation.TL_BR
                setStroke(dp.toInt().coerceAtLeast(1), ColorUtils.setAlphaComponent(t.acc, 64))
            }
            tag == "node" -> {
                v.background = rounded(ColorUtils.blendARGB(t.surf, t.acc, .09f), 999, dp)
                text(v, t.fg, t.dim)
            }
            tag == FG -> text(v, t.fg, t.dim)
            tag == DIM -> text(v, t.dim, t.dim)
            tag == ACC -> text(v, t.acc, t.dim)
            tag == FAIL -> text(v, t.fail, t.dim)
            tag == BTN -> {
                v.background = rounded(t.acc, t.r, dp)
                text(v, t.accFg, t.accFg)
            }
            // Квадрат под значком строки: мягкий акцент, как в макете.
            tag == ICON_BOX -> v.background = rounded(t.accSoft, minOf(t.r, 12), dp).apply {
                setStroke(dp.toInt().coerceAtLeast(1), ColorUtils.setAlphaComponent(t.acc, 36))
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
        return rounded(t.surf, t.r, dp).apply {
            if (t.card != "flat") {
                orientation = GradientDrawable.Orientation.TL_BR
                colors = intArrayOf(ColorUtils.blendARGB(t.surf, t.acc, if (t.dark) .065f else .025f), t.surf)
            }
            setStroke(dp.toInt().coerceAtLeast(1), if (line == t.line) ColorUtils.blendARGB(t.surf, t.line, .65f) else line)
        }
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

    /** pressAnimator — вжатие на 4 % при нажатии и возврат; та же кривая, что у CSS-переходов макета. */
    private fun pressAnimator(v: View): android.animation.StateListAnimator {
        val pressed = android.animation.AnimatorSet().apply {
            playTogether(
                android.animation.ObjectAnimator.ofFloat(v, View.SCALE_X, 0.96f),
                android.animation.ObjectAnimator.ofFloat(v, View.SCALE_Y, 0.96f),
            )
            duration = 120
        }
        val idle = android.animation.AnimatorSet().apply {
            playTogether(
                android.animation.ObjectAnimator.ofFloat(v, View.SCALE_X, 1f),
                android.animation.ObjectAnimator.ofFloat(v, View.SCALE_Y, 1f),
            )
            duration = 260
            interpolator = android.view.animation.DecelerateInterpolator(2f)
        }
        return android.animation.StateListAnimator().apply {
            addState(intArrayOf(android.R.attr.state_pressed), pressed)
            addState(intArrayOf(), idle)
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
