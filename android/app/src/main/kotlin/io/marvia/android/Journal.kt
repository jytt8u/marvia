package io.marvia.android

import android.content.Context
import android.os.Build
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * Journal — что приложение делало, своими словами.
 *
 * Нужен не человеку, а продавцу. Покупатель пишет «не работает», и дальше
 * начинается переписка: что именно, когда, а что было написано на экране.
 * Одна кнопка «отправить журнал» заменяет её целиком.
 *
 * Поэтому здесь нет ни адресов нод, ни ключей: журнал уходит в чужой чат, и
 * попасть в него должно только то, что человек и так видел на экране.
 */
object Journal {

    /** Сколько строк помним. Больше сотни в переписку никто не читает. */
    private const val LIMIT = 120

    private val lines = ArrayDeque<String>(LIMIT)
    private val clock = SimpleDateFormat("HH:mm:ss", Locale.US)

    @Synchronized
    fun add(line: String) {
        if (lines.size >= LIMIT) {
            lines.removeFirst()
        }
        lines.addLast(clock.format(Date()) + "  " + line)
    }

    @Synchronized
    fun lines(): List<String> = lines.toList()

    /**
     * report собирает то, что отправляется продавцу.
     *
     * Модель телефона и версия Android — не любопытство: половина бед бывает
     * только на конкретной прошивке, и без этих двух строк продавец будет
     * спрашивать их сам.
     */
    @Synchronized
    fun report(context: Context): String = buildString {
        append(context.getString(R.string.log_header)).append('\n')
        append(Build.MANUFACTURER).append(' ').append(Build.MODEL)
        append(", Android ").append(Build.VERSION.RELEASE).append('\n')

        val store = Store(context)
        val local = context.getString(if (store.bypassRussian) R.string.log_yes else R.string.log_no)
        append(context.getString(R.string.log_bypassed, store.bypassed.size))
        append(context.getString(R.string.log_bypass_local, local))
        append('\n').append('\n')

        for (line in lines) {
            append(line).append('\n')
        }
    }
}
