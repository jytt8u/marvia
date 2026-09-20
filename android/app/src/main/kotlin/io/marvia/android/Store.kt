package io.marvia.android

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import io.marvia.mobile.Mobile
import java.io.File

/**
 * Store — то немногое, что приложение помнит между запусками.
 *
 * Ключ доступа лежит в обычных настройках приложения. Их защищает песочница
 * Android: другое приложение файл не прочитает, а на заблокированном телефоне
 * он зашифрован ключом владельца. От root-доступа это не спасает, но и
 * androidx.security ничего к этому не добавил бы — там тот же аппаратный
 * ключ, только через прослойку, которую сама Google перестала развивать.
 *
 * Рядом с настройками ядро держит кэш списка нод. Сам файл пишет и читает Go,
 * приложение знает про него ровно одно: когда ключ меняется, старый кэш надо
 * убрать. Ядро от чужого кэша защищено и само — оно сверяет отпечаток адреса
 * подписки, — но оставлять на диске адреса нод прошлого продавца незачем.
 */
class Store(context: Context) {

    private val app = context.applicationContext

    private val prefs = app.getSharedPreferences("veil", Context.MODE_PRIVATE)

    /** Ссылка вида marvia://… — её выдаёт бот продавца при оплате. */
    var accountLink: String
        get() = prefs.getString(KEY_ACCOUNT_LINK, "").orEmpty()
        set(value) {
            val link = value.trim()
            if (link != accountLink) {
                chosenNode = 0
                // Запоминаем день, а не саму ссылку: в настройках человек
                // видит, когда ключ появился, и понимает, тот ли он, что
                // прислал продавец на прошлой неделе.
                prefs.edit().putLong(KEY_ACCOUNT_SAVED, System.currentTimeMillis()).apply()
            }
            prefs.edit().putString(KEY_ACCOUNT_LINK, link).apply()
            // Ключ кладут и с онбординга, и нажатием marvia:// из чата.
            // Подхватываем его здесь, иначе добавленная позже вторая
            // подписка окажется в списке одна, а первая — нигде.
            if (link.isNotEmpty() && subscriptions.none { it.link == link }) {
                subscriptions = subscriptions + Subscription(defaultSubscriptionName(link), link)
            }
        }

    /**
     * chosenNode — нода, которую человек выбрал руками; ноль — автовыбор.
     *
     * Живёт здесь, а не в ядре: ядро поднимается с каждым туннелем заново и
     * своего выбора не помнит. Номер ноды принадлежит подписке, поэтому при
     * смене рабочей подписки выбор сбрасывается — чужой номер указал бы
     * не туда.
     */
    var chosenNode: Long
        get() = prefs.getLong(KEY_CHOSEN_NODE, 0)
        set(value) { prefs.edit().putLong(KEY_CHOSEN_NODE, value).apply() }

    /** Когда ключ положили сюда. Ноль означает, что он появился до этой записи. */
    val accountSavedAt: Long
        get() = prefs.getLong(KEY_ACCOUNT_SAVED, 0)

    /**
     * Подписка: имя и ссылка от одного продавца.
     *
     * Их бывает несколько — у человека может быть доступ у двоих, — но
     * работает всегда одна. Так устроен и Hiddify, откуда взят образец, и так
     * это ничего не стоит: ядру по-прежнему отдаётся одна ссылка, а ключ
     * покупателя остаётся один на весь список нод.
     *
     * Несколько подписок одновременно означали бы свой ключ у каждой ноды и
     * переделку выбора ноды в ядре. Выгоды от этого нет: человек всё равно
     * выходит в интернет через одну страну за раз.
     */
    data class Subscription(val name: String, val link: String)

    /**
     * subscriptions — что человек добавил. Хранится строками «имя\nссылка», по
     * одной паре на запись: JSON ради двух полей — лишняя зависимость, а
     * перевод строки в имени и так не наберёшь.
     */
    var subscriptions: List<Subscription>
        get() = prefs.getStringSet(KEY_SUBSCRIPTIONS, emptySet()).orEmpty()
            .mapNotNull(::subscriptionOf)
            .sortedBy { it.name.lowercase() }
        set(value) {
            prefs.edit().putStringSet(KEY_SUBSCRIPTIONS, value.map(::rowOf).toSet()).apply()
        }

    /** addSubscription кладёт подписку и делает её рабочей. Повтор той же ссылки не двоится. */
    fun addSubscription(name: String, link: String) {
        val clean = link.trim()
        val title = name.trim().ifEmpty { defaultSubscriptionName(clean) }
        subscriptions = subscriptions.filter { it.link != clean } + Subscription(title, clean)
        accountLink = clean
    }

    fun removeSubscription(link: String) {
        subscriptions = subscriptions.filter { it.link != link }
        dropSubscriptionCache(link)
        // Убрали рабочую — остаёмся без ключа, а не с чужим втихую.
        if (accountLink == link) accountLink = ""
    }

    /**
     * defaultSubscriptionName — имя, когда человек его не ввёл: домен подписки.
     *
     * Он и отличает продавцов друг от друга, а «Подписка 1» и «Подписка 2»
     * не отличают ничего.
     */
    private fun defaultSubscriptionName(link: String): String =
        nameFromLink(link, app.getString(R.string.servers_sub_untitled))

    /**
     * bypassed — приложения, которые ходят мимо туннеля.
     *
     * Госуслуги, банки и всё государственное не отвечают на запросы с
     * зарубежных адресов, а нода стоит за границей — отсюда и берётся «включил
     * VPN, и госуслуги не открываются». Исключённому приложению система
     * оставляет обычную сеть, и на сервер приходит нормальный российский адрес.
     *
     * Значок VPN в шторке при этом остаётся: его рисует Android, пока поднят
     * туннель, и убрать его нельзя ничем. Но ломает госуслуги не значок, а
     * адрес, и вот его исключение чинит.
     */
    var bypassed: Set<String>
        // Копию отдаём намеренно: SharedPreferences возвращает своё множество,
        // и правка его на месте молча не сохранилась бы.
        get() = prefs.getStringSet(KEY_BYPASSED, emptySet())?.toSet().orEmpty()
        set(value) {
            prefs.edit().putStringSet(KEY_BYPASSED, value.toSet()).apply()
        }

    /**
     * bypassMode — что делать со списком приложений.
     *
     * «Мимо туннеля»: отмеченные ходят напрямую, остальные — через туннель.
     * «Только эти»: наоборот, в туннель идут одни отмеченные — так живут те,
     * кому туннель нужен ради двух приложений, а банк и такси должны видеть
     * настоящий адрес. «Выключено»: список остаётся, но не применяется —
     * чтобы отметки не пропали, когда человек на день выключает исключения.
     */
    var bypassMode: String
        get() = prefs.getString(KEY_BYPASS_MODE, BYPASS_EXCLUDE).let {
            if (it == BYPASS_INCLUDE || it == BYPASS_OFF) it else BYPASS_EXCLUDE
        }
        set(value) {
            prefs.edit().putString(KEY_BYPASS_MODE, value).apply()
        }

    /**
     * dns — кто отвечает на запросы имён. Только адрес, без порта.
     *
     * Запрос в любом случае уходит внутрь туннеля и по TCP — это решает
     * ядро, а не человек. Выбор здесь только в том, чей резолвер стоит на
     * другом конце: у кого-то из них есть фильтр рекламы, у кого-то — нет.
     */
    var dns: String
        get() = prefs.getString(KEY_DNS, null)?.takeIf { it in DNS_CHOICES } ?: DNS_CHOICES.first()
        set(value) {
            prefs.edit().putString(KEY_DNS, value).apply()
        }

    /**
     * lanOutside — оставлять ли домашнюю сеть мимо туннеля.
     *
     * Принтер, телевизор, роутер: их адреса частные и за границу не
     * маршрутизируются, внутри туннеля до них не дойти никак. Выключено по
     * умолчанию не из вредности: в чужом Wi-Fi «домашняя сеть» — это чужая
     * сеть, и пускать туда трафик мимо туннеля человек должен сам.
     */
    var lanOutside: Boolean
        get() = prefs.getBoolean(KEY_LAN_OUTSIDE, false)
        set(value) {
            prefs.edit().putBoolean(KEY_LAN_OUTSIDE, value).apply()
        }

    /**
     * bypassRussian — вести ли российские сайты мимо туннеля.
     *
     * Отдельно от списка приложений: приложения человек выбирает сам, а это
     * один переключатель на всю страну. Работает с Android 13, где система
     * научилась исключать маршруты.
     */
    var bypassRussian: Boolean
        get() = prefs.getBoolean(KEY_BYPASS_RU, false)
        set(value) {
            prefs.edit().putBoolean(KEY_BYPASS_RU, value).apply()
        }

    /** Спрашивали ли уже про российские сайты. Второй раз не навязываемся. */
    var bypassAsked: Boolean
        get() = prefs.getBoolean(KEY_BYPASS_ASKED, false)
        set(value) {
            prefs.edit().putBoolean(KEY_BYPASS_ASKED, value).apply()
        }

    /**
     * autoStart — поднимать ли туннель после перезагрузки телефона.
     *
     * Телефон перезагружается сам: от обновления, от разряда, от зависания.
     * Человек замечает это не сразу, а когда открывает нужный сайт, — и
     * винит не телефон, а нас.
     */
    var autoStart: Boolean
        get() = prefs.getBoolean(KEY_AUTOSTART, false)
        set(value) {
            prefs.edit().putBoolean(KEY_AUTOSTART, value).apply()
        }

    /**
     * look — выбранная тема: пресет, акцент, плотность.
     *
     * До этого тем было две — светлая и тёмная, — и выбор лежал под ключом
     * theme числом режима AppCompatDelegate. Тот, кто выбирал светлую, получает
     * «Дневную»: это ближайший пресет, и человек не должен обнаружить, что его
     * выбор молча стёрли обновлением.
     */
    var look: Look.Choice
        get() {
            val d = Look.DEFAULT
            val preset = prefs.getString(KEY_PRESET, null)
                ?: if (prefs.getInt(KEY_THEME, -1) == LEGACY_LIGHT) "daylight" else d.preset
            // Незнакомое или битое значение поле за полем заменит normalize:
            // одна битая плотность не должна сбрасывать ещё и цвет.
            return Look.normalize(
                Look.Choice(
                    preset = preset,
                    accent = prefs.getInt(KEY_ACCENT, 0),
                    kind = prefs.getString(KEY_KIND, null) ?: d.kind,
                    dir = prefs.getString(KEY_DIR, null) ?: d.dir,
                    depth = prefs.getFloat(KEY_DEPTH, d.depth.toFloat()).toDouble(),
                    tint = prefs.getInt(KEY_TINT, 0),
                    radius = prefs.getString(KEY_RADIUS, null) ?: d.radius,
                    density = prefs.getString(KEY_DENSITY, null) ?: d.density,
                    btn = prefs.getString(KEY_BTN, null) ?: d.btn,
                    glow = prefs.getString(KEY_GLOW, null) ?: d.glow,
                    card = prefs.getString(KEY_CARD, null) ?: d.card,
                ),
            )
        }
        set(value) {
            val v = Look.normalize(value)
            prefs.edit()
                .putString(KEY_PRESET, v.preset)
                .putInt(KEY_ACCENT, v.accent)
                .putString(KEY_KIND, v.kind)
                .putString(KEY_DIR, v.dir)
                .putFloat(KEY_DEPTH, v.depth.toFloat())
                .putInt(KEY_TINT, v.tint)
                .putString(KEY_RADIUS, v.radius)
                .putString(KEY_DENSITY, v.density)
                .putString(KEY_BTN, v.btn)
                .putString(KEY_GLOW, v.glow)
                .putString(KEY_CARD, v.card)
                .apply()
        }

    /**
     * Ручки темы, которых нет в общем коде: узор фона, шрифт, значок в шапке.
     *
     * В код темы они не входят намеренно: код общий с панелью и окном, и
     * узор с фотографией-значком там не значат ничего. Живут на телефоне и
     * профили их не запоминают.
     */
    var pattern: String
        get() = prefs.getString(KEY_PATTERN, PATTERN_DOTS)?.takeIf { it in PATTERNS } ?: PATTERN_DOTS
        set(value) { prefs.edit().putString(KEY_PATTERN, value.takeIf { it in PATTERNS } ?: PATTERN_DOTS).apply() }

    var font: String
        get() = prefs.getString(KEY_FONT, FONT_ONEST)?.takeIf { it in FONTS } ?: FONT_ONEST
        set(value) { prefs.edit().putString(KEY_FONT, value.takeIf { it in FONTS } ?: FONT_ONEST).apply() }

    /** logo — что в шапке главной: знак Marvia, своя картинка или ничего. */
    var logo: String
        get() = prefs.getString(KEY_LOGO, LOGO_MARVIA)?.takeIf { it in LOGOS } ?: LOGO_MARVIA
        set(value) { prefs.edit().putString(KEY_LOGO, value.takeIf { it in LOGOS } ?: LOGO_MARVIA).apply() }

    private fun logoFile(): File = File(app.filesDir, LOGO_FILE)

    fun hasLogo(): Boolean = logoFile().exists()

    /** saveLogo кладёт свою картинку под значок, уменьшив: в шапке она в 34 dp. */
    fun saveLogo(uri: android.net.Uri): Boolean {
        val bmp = try {
            app.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it) }
        } catch (_: Exception) {
            null
        } ?: return false
        val scale = LOGO_MAX_SIDE.toFloat() / maxOf(bmp.width, bmp.height, 1)
        val small = if (scale < 1f) Bitmap.createScaledBitmap(bmp, (bmp.width * scale).toInt().coerceAtLeast(1), (bmp.height * scale).toInt().coerceAtLeast(1), true) else bmp
        return try {
            val tmp = File(app.filesDir, "$LOGO_FILE.tmp")
            tmp.outputStream().use { small.compress(Bitmap.CompressFormat.PNG, 100, it) }
            tmp.renameTo(logoFile())
        } catch (_: Exception) {
            false
        } finally {
            if (small !== bmp) small.recycle()
            bmp.recycle()
        }
    }

    fun logoBitmap(): Bitmap? = try { BitmapFactory.decodeFile(logoFile().absolutePath) } catch (_: Exception) { null }

    /** profiles — три сохранённых вида кодами; пустой слот — null. */
    var profiles: List<String?>
        get() = (0 until 3).map { i ->
            prefs.getString(KEY_PROFILE + i, null)?.takeIf { Look.decode(it) != null }
        }
        set(value) {
            val e = prefs.edit()
            for (i in 0 until 3) {
                val code = value.getOrNull(i)
                if (code == null) e.remove(KEY_PROFILE + i) else e.putString(KEY_PROFILE + i, code)
            }
            e.apply()
        }

    /**
     * language — язык приложения: ru, en или пусто, пока не выбран.
     *
     * Пустота означает первый запуск: тогда первым открывается экран выбора.
     * Сам язык применяет AppCompat и хранит его отдельно; здесь запоминается
     * только факт выбора и то, что показать отмеченным в настройках.
     */
    var language: String
        get() = prefs.getString(KEY_LANGUAGE, "").orEmpty()
        set(value) {
            prefs.edit().putString(KEY_LANGUAGE, value).apply()
        }

    /**
     * Свой фон: фото за интерфейсом. Файл лежит в приватном каталоге
     * приложения и никуда не уходит — как ключ и настройки. Видео нет: его
     * пришлось бы декодировать своим плеером, а это ExoPlayer и почти
     * удвоение пакета ради фона. В код темы фон не входит: код передают, а
     * файл вместе с ним не унесёшь.
     *
     * fit — как вписать: cover (заполнить) или contain (целиком). dim —
     * затемнение в процентах: пелена цветом фона темы поверх снимка, чтобы
     * светлая тема на тёмном фото осталась читаемой.
     */
    var backdropFit: String
        get() = prefs.getString(KEY_BG_FIT, "cover").let { if (it == "contain") it else "cover" }
        set(value) {
            prefs.edit().putString(KEY_BG_FIT, if (value == "contain") "contain" else "cover").apply()
        }

    var backdropDim: Int
        get() = prefs.getInt(KEY_BG_DIM, 55).coerceIn(0, 95)
        set(value) {
            prefs.edit().putInt(KEY_BG_DIM, value.coerceIn(0, 95)).apply()
        }

    fun hasBackdrop(): Boolean = backdropFile().exists()

    /** backdropStamp — «версия» файла фона: ноль, если его нет. По ней кэшируют декодированный снимок. */
    fun backdropStamp(): Long {
        val f = backdropFile()
        if (!f.exists()) return 0L
        return f.lastModified().let { if (it == 0L) f.length() else it }
    }

    private fun backdropFile(): File = File(app.filesDir, BACKDROP_FILE)

    /**
     * saveBackdrop берёт выбранный файл и кладёт его к себе, уменьшив до
     * разумного размера. Полноразмерный снимок с камеры — это десятки
     * мегапикселей, которые незачем держать и рисовать под интерфейсом: на
     * фоне телефона больше пары тысяч точек по длинной стороне не видно, а
     * память они съедают всерьёз.
     *
     * Возвращает false, если файл не картинка или не читается: тогда фон
     * просто не меняется, а не роняет приложение.
     */
    fun saveBackdrop(uri: android.net.Uri): Boolean {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        try {
            app.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
        } catch (_: Exception) {
            return false
        }
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return false

        val opts = BitmapFactory.Options().apply {
            inSampleSize = sampleSize(bounds.outWidth, bounds.outHeight, BACKDROP_MAX_SIDE)
        }
        val bmp = try {
            app.contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, opts) }
        } catch (_: Exception) {
            null
        } ?: return false

        return try {
            // Во временный файл, потом переименование: прерванная запись не
            // оставит наполовину сохранённый фон, который потом не прочитается.
            val tmp = File(app.filesDir, "$BACKDROP_FILE.tmp")
            tmp.outputStream().use { bmp.compress(Bitmap.CompressFormat.JPEG, 88, it) }
            tmp.renameTo(backdropFile())
        } catch (_: Exception) {
            false
        } finally {
            bmp.recycle()
        }
    }

    /** backdropBitmap — фон для отрисовки или null, если его нет либо файл битый. */
    fun backdropBitmap(): Bitmap? {
        val f = backdropFile()
        if (!f.exists()) return null
        return try {
            BitmapFactory.decodeFile(f.absolutePath)
        } catch (_: Exception) {
            null
        }
    }

    fun clearBackdrop() {
        backdropFile().delete()
    }

    private fun sampleSize(w: Int, h: Int, max: Int): Int {
        var s = 1
        while (w / (s * 2) >= max || h / (s * 2) >= max) s *= 2
        return s
    }

    /** Каталог, который приложение отдаёт ядру под кэш подписки. */
    fun cacheDir(): String = app.filesDir.absolutePath

    private fun dropSubscriptionCache(link: String) {
        // Кэш у каждой подписки свой — стираем его; старый общий файл, если
        // остался от прежних версий, — заодно. Не удалилось — не беда: ядро
        // сверит отпечаток и чужой кэш использовать не станет.
        runCatching { Mobile.forgetSubscription(link, cacheDir()) }
        File(app.filesDir, Mobile.CacheName).delete()
    }

    companion object {
        const val LANG_RU = "ru"
        const val LANG_EN = "en"

        const val BYPASS_EXCLUDE = "exclude"
        const val BYPASS_INCLUDE = "include"
        const val BYPASS_OFF = "off"

        /**
         * Резолверы, из которых выбирают. Первый — по умолчанию, он же
         * Mobile.DefaultDNS без порта. Список короткий и публичный: свой
         * адрес вписать нельзя, потому что опечатка в нём — это «интернет
         * не работает» без единой подсказки, почему.
         */
        val DNS_CHOICES = listOf("1.1.1.1", "8.8.8.8", "9.9.9.9", "94.140.14.14")

        private const val KEY_ACCOUNT_LINK = "account_link"
        private const val KEY_ACCOUNT_SAVED = "account_saved_at"
        private const val KEY_CHOSEN_NODE = "chosen_node"
        private const val KEY_SUBSCRIPTIONS = "subscriptions"

        /**
         * rowOf и subscriptionOf — подписка одной строкой настроек.
         *
         * Пара полей, а не JSON: зависимость ради двух строк не нужна, а
         * перевод строки в имени всё равно не наберёшь.
         */
        fun rowOf(sub: Subscription): String = sub.name + "\n" + sub.link

        fun subscriptionOf(row: String): Subscription? {
            val at = row.indexOf('\n')
            return if (at <= 0) null else Subscription(row.take(at), row.substring(at + 1))
        }

        /** nameFromLink — домен панели из ссылки; порт в имя не тащим. */
        fun nameFromLink(link: String, fallback: String): String =
            Regex("@([^/?#]+)").find(link)?.groupValues?.get(1)?.substringBefore(':')
                ?: fallback
        private const val KEY_BYPASSED = "bypassed_apps"
        private const val KEY_BYPASS_MODE = "bypass_mode"
        private const val KEY_DNS = "dns"
        private const val KEY_LAN_OUTSIDE = "lan_outside"
        private const val KEY_BYPASS_RU = "bypass_russian"
        private const val KEY_BYPASS_ASKED = "bypass_asked"
        private const val KEY_AUTOSTART = "autostart"
        private const val KEY_THEME = "theme"
        private const val KEY_LANGUAGE = "language"
        private const val KEY_PRESET = "look_preset"
        private const val KEY_ACCENT = "look_accent"
        private const val KEY_DENSITY = "look_density"
        private const val KEY_KIND = "look_kind"
        private const val KEY_DIR = "look_dir"
        private const val KEY_DEPTH = "look_depth"
        private const val KEY_TINT = "look_tint"
        private const val KEY_RADIUS = "look_radius"
        private const val KEY_BTN = "look_btn"
        private const val KEY_GLOW = "look_glow"
        private const val KEY_CARD = "look_card"
        private const val KEY_PROFILE = "look_profile_"
        private const val KEY_BG_FIT = "backdrop_fit"
        private const val KEY_BG_DIM = "backdrop_dim"

        /** Имя файла фона в приватном каталоге и потолок его длинной стороны. */
        private const val BACKDROP_FILE = "backdrop.jpg"
        private const val BACKDROP_MAX_SIDE = 2048

        private const val KEY_PATTERN = "look_pattern"
        private const val KEY_FONT = "look_font"
        private const val KEY_LOGO = "look_logo"
        private const val LOGO_FILE = "logo.png"
        private const val LOGO_MAX_SIDE = 256

        const val PATTERN_NONE = "none"
        const val PATTERN_DOTS = "dots"
        val PATTERNS = listOf(PATTERN_NONE, PATTERN_DOTS, "grid", "rings", "lines")

        const val FONT_ONEST = "onest"
        val FONTS = listOf(FONT_ONEST, "manrope", "geologica")

        const val LOGO_MARVIA = "marvia"
        const val LOGO_CUSTOM = "custom"
        const val LOGO_NONE = "none"
        val LOGOS = listOf(LOGO_MARVIA, LOGO_CUSTOM, LOGO_NONE)

        /** AppCompatDelegate.MODE_NIGHT_NO — так хранилась светлая тема. */
        private const val LEGACY_LIGHT = 1
    }
}
