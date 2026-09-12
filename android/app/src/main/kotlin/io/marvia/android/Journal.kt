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

    /**
     * Важность строки. Нужна экрану логов: продавец, которому прислали
     * пятьсот строк, первым делом хочет видеть только беды, а без метки
     * отличить «подключились» от «не поднялись» можно только чтением.
     */
    enum class Level { INFO, WARN, ERROR }

    /** Одна строка: когда, насколько важно, что. */
    data class Entry(val time: String, val level: Level, val text: String)

    /**
     * Сколько строк помним. Пятьсот — это сутки переподключений на плохой
     * сети; в переписку уходит хвост покороче, см. report.
     */
    private const val LIMIT = 500

    /** Сколько строк уезжает продавцу. Больше сотни в чате никто не читает. */
    private const val REPORT_TAIL = 120

    private val entries = ArrayDeque<Entry>(LIMIT)
    private val clock = SimpleDateFormat("HH:mm:ss", Locale.US)

    @Synchronized
    fun add(line: String, level: Level = Level.INFO) {
        if (entries.size >= LIMIT) {
            entries.removeFirst()
        }
        entries.addLast(Entry(clock.format(Date()), level, line))
    }

    @Synchronized
    fun entries(): List<Entry> = entries.toList()

    /** lines — строки в том виде, в каком они уходят в чат. */
    @Synchronized
    fun lines(): List<String> = entries.map { it.time + "  " + it.text }

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

        for (line in lines().takeLast(REPORT_TAIL)) {
            append(line).append('\n')
        }
    }
}
