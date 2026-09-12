package io.marvia.android

import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.os.LocaleListCompat
import io.marvia.android.databinding.ScreenLanguageBinding

/**
 * LanguageScreen — какой язык у приложения.
 *
 * Первый экран при первом запуске. Раньше язык брался у системы молча, и
 * человек с турецким телефоном, купивший доступ у русскоязычного продавца,
 * получал английское приложение и русские ошибки ядра, не имея ни одной ручки,
 * чтобы это поменять. Теперь ручка есть — и на первом экране, и в настройках.
 *
 * Надписи на экране двуязычные: пока язык не выбран, экран обязан читаться
 * на обоих. Кнопка «Продолжить» — единственное, что переключается вместе с
 * выбором: так человек видит, что нажатие уже подействовало.
 */
class LanguageScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenLanguageBinding,
    private val store: Store,
    private val theme: () -> Theme,
    /** Язык выбран и применён; экран можно закрывать. */
    private val onDone: () -> Unit,
) {

    private var picked = Store.LANG_RU
    private val dp = host.resources.displayMetrics.density

    init {
        ui.langRu.setOnClickListener { pick(Store.LANG_RU) }
        ui.langEn.setOnClickListener { pick(Store.LANG_EN) }
        ui.langContinue.setOnClickListener { apply() }
    }

    /**
     * open показывает экран. Отмечен уже выбранный язык, а до первого выбора —
     * тот, что подсказывает система: русский для русскоязычных настроек и
     * соседних языков, где русский читают, иначе английский.
     */
    fun open() {
        picked = store.language.ifEmpty { guess() }
        paint()
    }

    private fun pick(lang: String) {
        picked = lang
        paint()
    }

    /** paint красит карточки по выбору и теме. */
    fun paint() {
        val t = theme()
        Paint.apply(ui.root, t)

        val ru = picked == Store.LANG_RU
        ui.langRu.background = Paint.card(t, dp, stroke = if (ru) t.acc else t.line)
        ui.langEn.background = Paint.card(t, dp, stroke = if (ru) t.line else t.acc)
        ui.langRuMark.background = Paint.ring(t, dp, chosen = ru)
        ui.langEnMark.background = Paint.ring(t, dp, chosen = !ru)
        ui.langContinue.setText(if (ru) R.string.language_continue_ru else R.string.language_continue_en)
    }

    /**
     * apply запоминает выбор и переключает язык приложения.
     *
     * Через AppCompat, а не подменой конфигурации руками: на Android 13 и новее
     * это системная настройка «язык приложения», и она видна в настройках
     * телефона, а на старых AppCompat хранит её сам. Если язык и правда
     * сменился, экраны пересоздаются системой и onCreate откроет что нужно;
     * если совпал с прежним — пересоздания не будет. Дальше идём в обоих
     * случаях: лишний показ экрана перед пересозданием безобиден, а угадывать,
     * пересоздаст система или нет, — нет.
     */
    private fun apply() {
        store.language = picked
        AppCompatDelegate.setApplicationLocales(LocaleListCompat.forLanguageTags(picked))
        onDone()
    }

    private fun guess(): String {
        val system = host.resources.configuration.locales[0].language
        return if (system in RUSSIAN_READERS) Store.LANG_RU else Store.LANG_EN
    }

    private companion object {
        /** Те же языки, с которыми окно на компьютере подсказывает русский. */
        val RUSSIAN_READERS = setOf("ru", "be", "uk", "kk", "ky", "uz", "tg", "hy", "az")
    }
}
