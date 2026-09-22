package io.marvia.android

import android.content.Context
import android.net.IpPrefix
import android.net.InetAddresses
import android.os.Build
import androidx.annotation.RequiresApi
import io.marvia.mobile.Mobile
import java.io.File
import java.net.InetAddress

/**
 * RuRoutes — российские подсети, которые идут мимо туннеля.
 *
 * Зачем. Нода стоит за границей, и госуслуги, банки и всё государственное
 * видят иностранца — они просто не отвечают. Исключение этих подсетей из
 * туннеля возвращает им настоящий адрес, и всё открывается.
 *
 * Список даёт панель продавца, а не мы внутри приложения: он меняется раз в
 * месяц, а обновление приложения у покупателя упирается в магазин, который в
 * нужный момент как раз и не работает. Скачанное лежит в файле рядом с кэшем
 * нод — без него включение туннеля не должно ждать сети.
 */
object RuRoutes {

    private const val FILE = "ru-routes.txt"

    /** Как часто спрашиваем панель. Список меняется раз в месяц, чаще незачем. */
    private const val MAX_AGE_MS = 7L * 24 * 60 * 60 * 1000

    /**
     * load читает подсети из файла и превращает в то, что понимает система.
     *
     * Ничего не скачивает: включение туннеля не должно упираться в сеть, а
     * панель в этот момент может быть недоступна — ровно тогда, когда VPN и
     * нужен. Обновлением занимается [refresh].
     */
    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    fun load(context: Context): List<IpPrefix> {
        val file = File(context.filesDir, FILE)
        if (!file.exists()) {
            return emptyList()
        }

        return file.readLines().mapNotNull { line ->
            val prefix = line.trim()
            if (prefix.isEmpty()) return@mapNotNull null

            val (address, bits) = prefix.split("/").let {
                if (it.size != 2) return@mapNotNull null
                it[0] to (it[1].toIntOrNull() ?: return@mapNotNull null)
            }

            try {
                // Разбираем без обращения к резолверу: это строка с адресом, а
                // не имя, и запрос имени здесь означал бы поход в сеть на
                // каждую из восьми тысяч строк.
                IpPrefix(InetAddresses.parseNumericAddress(address), bits)
            } catch (_: IllegalArgumentException) {
                null
            }
        }
    }

    /** Есть ли скачанный список. */
    fun ready(context: Context): Boolean = File(context.filesDir, FILE).exists()

    /** Сколько подсетей в скачанном списке; 0 — списка нет. */
    fun count(context: Context): Int {
        val file = File(context.filesDir, FILE)
        return if (file.exists()) file.readLines().count { it.isNotBlank() } else 0
    }

    /**
     * refresh скачивает список с панели, если он устарел.
     *
     * Вызывается с выключенным туннелем: качать его через свой же туннель
     * незачем, а до включения панель обычно доступна напрямую.
     */
    fun refresh(context: Context, accountLink: String, force: Boolean = false): Result<Int> {
        val file = File(context.filesDir, FILE)
        if (!force && file.exists() && System.currentTimeMillis() - file.lastModified() < MAX_AGE_MS) {
            return Result.success(file.readLines().size)
        }

        return try {
            // Качает ядро, а не мы: у него адреса панели из ссылки и тот же
            // путь, что у подписки. Своим HTTP мы ходили по имени панели, и
            // там, где оно не разрешалось, список не приходил никогда —
            // российские сайты шли через ноду за границей и не открывались.
            val text = Mobile.bypassRoutes(accountLink)
            val count = text.lineSequence().count { it.isNotBlank() }

            // Пишем целиком и разом: оборванная запись оставила бы половину
            // списка, и часть российских сайтов молча пошла бы через туннель.
            val tmp = File(context.filesDir, "$FILE.tmp")
            tmp.writeText(text + "\n")
            tmp.renameTo(file)
            lastError = ""
            Result.success(count)
        } catch (t: Throwable) {
            lastError = MarviaVpnService.failureOf(t).detail
            Journal.add(context.getString(R.string.log_bypass_failed, lastError), Journal.Level.WARN)
            Result.failure(t)
        }
    }

    /** Почему список не скачался в последний раз; пусто — скачался или не пробовали. */
    @Volatile
    var lastError: String = ""
        private set

    /** refreshInBackground — то же, не задерживая того, кто зовёт. */
    fun refreshInBackground(context: Context, accountLink: String) {
        Thread { refresh(context, accountLink) }.apply { isDaemon = true }.start()
    }

    /**
     * subscriptionURL — адрес подписки из ссылки доступа.
     *
     * Порт обязателен, и на нём это уже один раз сломалось. Панель продавца
     * часто стоит не на 443: её ставят рядом с чужим сайтом или за CDN, где
     * 443 занят. Без порта запрос уходил на 443, получал чужой ответ, список
     * не скачивался — и всё молча, потому что качается он в фоне.
     */
    fun subscriptionURL(accountLink: String): String? = try {
        val uri = android.net.Uri.parse(accountLink)
        val host = uri.host
        val path = uri.path
        if (host.isNullOrBlank() || path.isNullOrBlank()) {
            null
        } else {
            val port = if (uri.port > 0) ":${uri.port}" else ""
            "https://$host$port$path"
        }
    } catch (_: Throwable) {
        null
    }

    /** Заглушка для старых Android: там исключать маршруты нечем. */
    fun supported(): Boolean = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU

    @Suppress("unused")
    private fun unusedKeepImport(): InetAddress? = null
}
