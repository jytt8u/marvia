package io.marvia.android

import android.content.Context
import android.graphics.Typeface
import androidx.core.content.res.ResourcesCompat

/**
 * Fonts — два начертания макета для рисования кодом.
 *
 * Текст в разметке получает Onest из темы приложения сам; сюда ходят только
 * холсты и вьюхи, которые пишут цифры руками. Моноширинный — для чисел:
 * время сессии, скорость и проценты в нём не прыгают при смене цифр.
 *
 * Шрифты вшиты в пакет, хотя раньше решили этого не делать: макет держится
 * на них, а без них выглядит как чужое приложение. Два переменных файла —
 * меньше 400 КБ.
 */
object Fonts {
    private var monoCached: Typeface? = null
    private var boldCached: Typeface? = null

    fun mono(context: Context): Typeface = monoCached ?: (
        ResourcesCompat.getFont(context, R.font.jetbrains_mono) ?: Typeface.MONOSPACE
        ).also { monoCached = it }

    fun monoBold(context: Context): Typeface = boldCached ?: Typeface.create(mono(context), Typeface.BOLD).also { boldCached = it }

    fun text(context: Context): Typeface = ResourcesCompat.getFont(context, R.font.onest) ?: Typeface.DEFAULT

    fun textBold(context: Context): Typeface = Typeface.create(text(context), Typeface.BOLD)
}
