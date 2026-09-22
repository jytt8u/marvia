package io.marvia.android

import android.content.Context
import java.util.TimeZone

/**
 * Traffic — расход этого телефона по часам, дням и странам.
 *
 * Считает приложение, а не панель. У панели есть только накопительный итог по
 * каждому человеку, и по нему нельзя ответить ни «сколько вчера», ни «в какие
 * часы»; заводить у неё историю на каждого покупателя — значит знать о людях
 * больше, чем нужно для выдачи доступа. Здесь же цифры никуда не уходят: они
 * лежат в настройках приложения и умирают вместе с ним.
 *
 * Отсюда и честность подписи на экране: это расход одного телефона через
 * туннель, а не всей подписки. Другое устройство с тем же ключом считает своё.
 */
class Traffic(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("veil", Context.MODE_PRIVATE)

    /**
     * Книга одна на процесс и живёт в памяти. Раньше каждый опрос ядра — раз
     * в две секунды — разбирал тридцать дней истории из строки, собирал её
     * заново и переписывал файл настроек на диск: полсотни записей в минуту
     * на флеш телефона ради чисел, которые меняются на килобайты. Теперь
     * запись — раз в минуту, при смене часа и при остановке туннеля; экран
     * «Расход» читает ту же книгу, что пишет служба, и отставания не видит.
     */
    private val book: Ledger
        get() = synchronized(Companion) {
            ledger ?: Ledger(
                decodeDays(prefs.getString(KEY_DAYS, "").orEmpty()).toMutableMap(),
                decodePlaces(prefs.getString(KEY_PLACES, "").orEmpty()).toMutableMap(),
                prefs.getLong(KEY_SEEN, 0),
            ).also { ledger = it }
        }

    /** flush пишет книгу на диск, если в ней есть незаписанное. */
    fun flush(force: Boolean = true, now: Long = android.os.SystemClock.elapsedRealtime()) {
        synchronized(Companion) {
            val b = book
            if (!b.due(now, force)) return
            prefs.edit()
                .putLong(KEY_SEEN, b.seen)
                .putString(KEY_DAYS, encodeDays(b.days))
                .putString(KEY_PLACES, encodePlaces(b.places))
                .apply()
            b.written(now)
        }
    }

    /**
     * note принимает накопительный счётчик ядра и кладёт разницу в текущий час.
     *
     * Именно накопительный, а не приращение: приложение опрашивает ядро раз в
     * несколько секунд и может пропустить опрос — тогда разница всё равно
     * догонит. Счётчик меньше прошлого означает, что туннель подняли заново.
     */
    fun note(total: Long, place: String, at: Long = System.currentTimeMillis()) {
        synchronized(Companion) { book.note(total, place, dayOf(at), hourOf(at)) }
        flush(force = false)
    }

    private fun days(): Map<Long, LongArray> = synchronized(Companion) {
        book.days.mapValues { it.value.copyOf() }
    }

    /** today — сегодняшние сутки по часам; без записей — нули. */
    fun today(at: Long = System.currentTimeMillis()): List<Long> =
        days()[dayOf(at)]?.toList() ?: List(24) { 0L }

    /**
     * lastDays — итоги по суткам, от старых к новым, ровно [count] чисел.
     *
     * Дни без трафика тоже здесь, нулями: без них тридцать столбиков врут о
     * том, когда человек туннелем пользовался.
     */
    fun lastDays(count: Int, at: Long = System.currentTimeMillis()): List<DayTotal> {
        val have = days()
        val today = dayOf(at)
        return (count - 1 downTo 0).map { back ->
            val day = today - back
            DayTotal(day, have[day]?.sum() ?: 0)
        }
    }

    /**
     * week — семь строк по 24 часа, от понедельника к воскресенью.
     *
     * Берём последние семь суток, а не «эту неделю с понедельника»: в среду
     * недельная картина из трёх дней — это не картина.
     */
    fun week(at: Long = System.currentTimeMillis()): List<LongArray> {
        val have = days()
        val today = dayOf(at)
        val rows = Array(7) { LongArray(24) }
        for (back in 0..6) {
            val day = today - back
            val hours = have[day] ?: continue
            val row = rows[weekdayOf(day)]
            for (h in 0 until 24) row[h] += hours[h]
        }
        return rows.toList()
    }

    /** places — сколько прошло через каждую страну, от большего к меньшему. */
    fun places(): List<PlaceShare> =
        synchronized(Companion) { book.places.toMap() }
            .map { (name, bytes) -> PlaceShare(name, bytes) }
            .sortedByDescending { it.bytes }

    /**
     * Ledger — сама книга, без Android: сколько по часам и странам и когда
     * пора на диск. Отдельно, чтобы правило записи проверялось тестом.
     */
    class Ledger(
        val days: MutableMap<Long, LongArray>,
        val places: MutableMap<String, Long>,
        var seen: Long,
    ) {
        private var dirty = false
        private var ever = false
        private var lastWrite = 0L
        private var hourKey = -1L

        fun note(total: Long, place: String, day: Long, hour: Int) {
            val delta = if (total < seen) total else total - seen
            if (total != seen) dirty = true
            seen = total
            // Смена часа — повод записать сразу: иначе при выгрузке процесса
            // пропал бы не хвост минуты, а весь прошлый час.
            val key = day * 24 + hour
            if (hourKey != -1L && key != hourKey) boundary = true
            hourKey = key
            if (delta <= 0) return
            days.getOrPut(day) { LongArray(24) }[hour] += delta
            val title = place.trim()
            if (title.isNotEmpty()) places[title] = (places[title] ?: 0) + delta
        }

        private var boundary = false

        /** due — пора ли писать: есть что и (просят, прошла минута или сменился час). */
        fun due(now: Long, force: Boolean): Boolean =
            dirty && (force || boundary || !ever || now - lastWrite >= WRITE_EVERY_MS)

        fun written(now: Long) {
            dirty = false
            boundary = false
            ever = true
            lastWrite = now
        }
    }

    data class DayTotal(val day: Long, val bytes: Long)

    data class PlaceShare(val name: String, val bytes: Long)

    companion object {
        /** Книга процесса; null — ещё не читали с диска. */
        private var ledger: Ledger? = null

        /** Как часто писать на диск без повода. Потерять при выгрузке — минуту, не больше. */
        const val WRITE_EVERY_MS = 60_000L

        private const val KEY_DAYS = "traffic_days"
        private const val KEY_PLACES = "traffic_places"
        private const val KEY_SEEN = "traffic_seen"

        /** Сколько суток помним. Тридцать — столько же, сколько показываем. */
        const val KEEP_DAYS = 30

        /** Сколько стран помним: у продавца их единицы, а список не должен расти вечно. */
        private const val KEEP_PLACES = 24

        /**
         * dayOf и hourOf считают по местному времени телефона.
         *
         * Не по UTC, хотя панель считает свои сутки именно так: там выбор между
         * поясами продавца и покупателя, а здесь пояс один — тот, в котором
         * человек смотрит на экран. «Вчера» должно совпадать с его вчера.
         */
        fun dayOf(at: Long, tz: TimeZone = TimeZone.getDefault()): Long =
            Math.floorDiv(at + tz.getOffset(at), 86_400_000L)

        fun hourOf(at: Long, tz: TimeZone = TimeZone.getDefault()): Int =
            Math.floorMod(Math.floorDiv(at + tz.getOffset(at), 3_600_000L), 24L).toInt()

        /** weekdayOf: 0 — понедельник. 1 января 1970 было четвергом, отсюда сдвиг. */
        fun weekdayOf(day: Long): Int = Math.floorMod(day + 3, 7L).toInt()

        /**
         * Сутки одной строкой «номер:24 числа через запятую».
         *
         * Не JSON: строк тридцать, разбор нужен на каждом открытии экрана, а
         * зависимость ради этого — лишняя. Битую строку пропускаем молча: она
         * стоит одного дня в графике, а не всего графика.
         */
        fun decodeDays(text: String): Map<Long, LongArray> {
            val out = HashMap<Long, LongArray>()
            for (line in text.split('\n')) {
                val at = line.indexOf(':')
                if (at <= 0) continue
                val day = line.take(at).toLongOrNull() ?: continue
                val parts = line.substring(at + 1).split(',')
                if (parts.size != 24) continue
                val hours = LongArray(24)
                var ok = true
                for (i in 0 until 24) {
                    val v = parts[i].toLongOrNull()
                    if (v == null || v < 0) { ok = false; break }
                    hours[i] = v
                }
                if (ok) out[day] = hours
            }
            return out
        }

        fun encodeDays(days: Map<Long, LongArray>): String =
            days.entries
                .sortedByDescending { it.key }
                .take(KEEP_DAYS)
                .sortedBy { it.key }
                .joinToString("\n") { (day, hours) -> day.toString() + ":" + hours.joinToString(",") }

        /**
         * Страны строками «байты, табуляция, имя».
         *
         * Имя идёт последним: в нём бывает что угодно, кроме перевода строки и
         * табуляции. Управляющий символ вместо табуляции взять нельзя — эти
         * строки уезжают в XML настроек, а он таких символов не допускает.
         */
        fun decodePlaces(text: String): Map<String, Long> {
            val out = LinkedHashMap<String, Long>()
            for (line in text.split('\n')) {
                val at = line.indexOf('\t')
                if (at <= 0) continue
                val bytes = line.take(at).toLongOrNull() ?: continue
                val name = line.substring(at + 1)
                if (name.isNotEmpty() && bytes >= 0) out[name] = bytes
            }
            return out
        }

        fun encodePlaces(places: Map<String, Long>): String =
            places.entries
                .sortedByDescending { it.value }
                .take(KEEP_PLACES)
                .joinToString("\n") { (name, bytes) -> bytes.toString() + "\t" + name }
    }
}
