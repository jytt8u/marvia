package io.marvia.android

import android.content.Context
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.TimeUnit

/**
 * Format — числа и даты так, как их читает человек.
 *
 * Отдельно от экранов: одну и ту же дату показывают и главный экран, и
 * настройки, и расходиться они не должны.
 */
object Format {

    /** Язык интерфейса, а не язык системы: человек мог переключить его сам. */
    private fun locale(context: Context): Locale = context.resources.configuration.locales[0]

    /**
     * size — остаток трафика.
     *
     * До гигабайта переходим на мегабайты: «0.1 ГБ» человек читает дольше, чем
     * «104 МБ», а решение принимает по нему же.
     */
    fun size(context: Context, bytes: Long): String {
        val gb = 1024.0 * 1024 * 1024
        if (bytes >= gb) {
            return context.getString(
                R.string.size_gb,
                String.format(locale(context), "%.1f", bytes / gb),
            )
        }
        return context.getString(R.string.size_mb, bytes / (1024 * 1024))
    }

    /**
     * day превращает 2026-09-27 в «27 сентября».
     *
     * Число с точками человек сверяет с календарём, а название месяца узнаёт
     * сразу. Год не пишем: подписку продают на недели и месяцы.
     *
     * Неразобранную строку отдаём как есть: выдумывать дату нельзя, а показать
     * то, что прислала панель, честно.
     */
    fun day(context: Context, iso: String): String {
        val date = parse(iso) ?: return iso
        return SimpleDateFormat("d MMMM", locale(context)).format(date)
    }

    /** То же, но для отметки времени телефона: когда положили ключ. */
    fun day(context: Context, millis: Long): String =
        SimpleDateFormat("d MMMM", locale(context)).format(Date(millis))

    /**
     * daysLeft — сколько дней осталось до конца подписки.
     *
     * null означает, что дату разобрать не вышло. Отрицательных не бывает:
     * кончившуюся подписку считает ядро, и оно не даст туннелю подняться.
     */
    fun daysLeft(iso: String): Int? {
        val date = parse(iso) ?: return null
        val left = date.time - System.currentTimeMillis()
        if (left <= 0) {
            return 0
        }
        return TimeUnit.MILLISECONDS.toDays(left).toInt()
    }

    private fun parse(iso: String): Date? = try {
        // Локаль US намеренно: разбираем машинный формат от ядра, а не то,
        // что человек читает на экране.
        SimpleDateFormat("yyyy-MM-dd", Locale.US).parse(iso)
    } catch (_: Throwable) {
        null
    }
}
