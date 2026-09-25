package io.marvia.android

import org.json.JSONArray

/**
 * Nodes — список стран так, как его отдаёт ядро.
 *
 * Ядро присылает JSON строкой: через границу языков gomobile умеет переносить
 * только строки и числа, и список объектов пришлось бы читать по одному
 * элементу за вызов. Разбираем здесь, один раз на нажатие.
 */
data class NodeRow(
    val id: Long,
    val name: String,
    val country: String,
    /** Сколько нода отвечала при замере с этого телефона. Ноль — не мерили. */
    val ms: Long,
    val alive: Boolean,
    /** Через неё идёт трафик прямо сейчас. */
    val current: Boolean,
    /** Её выбрал человек руками. */
    val chosen: Boolean,
    val setupMs: Long = 0,
    /** Learned from the exit IP through this node; never sent back to the core. */
    val exitCountry: String = "",
    /** Only used to bind cached country to this exact node endpoint. */
    val endpoint: String = "",
) {

    /**
     * Страна — то, по чему ноды собираются в группы.
     *
     * Продавец пишет страну вместе с городом: «ОАЭ · Дубай». Для группы нужна
     * только первая половина, иначе каждый город станет своей страной.
     */
    val group: String
        get() = country.substringBefore('·').trim().ifEmpty {
            CountryGuess.fromName(name).ifEmpty { exitCountry }
        }

    val countryFromExit: Boolean
        get() = country.isBlank() && CountryGuess.fromName(name).isEmpty() && exitCountry.isNotBlank()

    /** Город, а если продавец его не написал — имя ноды. */
    val place: String
        get() = country.substringAfter('·', "").trim().ifEmpty { name }

    /** Как ноду называет ядро в nodeName(): по нему сверяется текущая. */
    val title: String
        get() = when {
            country.isEmpty() -> name
            name.isEmpty() -> country
            else -> country + " · " + name
        }

    companion object {

        /**
         * parse разбирает ответ nodes()/measure().
         *
         * Битый JSON здесь не беда, а пустой экран: ядро могло ещё не поднять
         * туннель. Падать из-за этого посреди выбора страны незачем.
         */
        fun parse(json: String, learnedCountry: (Long, String, String) -> String = { _, _, _ -> "" }): List<NodeRow> = try {
            val array = JSONArray(json)
            (0 until array.length()).mapNotNull { i ->
                val o = array.optJSONObject(i) ?: return@mapNotNull null
                NodeRow(
                    id = o.optLong("id"),
                    name = o.optString("name"),
                    country = o.optString("country"),
                    ms = o.optLong("ms"),
                    alive = o.optBoolean("alive"),
                    current = o.optBoolean("current"),
                    chosen = o.optBoolean("chosen"),
                    setupMs = o.optLong("setup_ms"),
                    exitCountry = learnedCountry(o.optLong("id"), o.optString("name"), o.optString("endpoint")),
                    endpoint = o.optString("endpoint"),
                )
            }
        } catch (_: Throwable) {
            emptyList()
        }
    }
}

/**
 * Flags — флаг по названию страны.
 *
 * Названия пишет продавец руками, и словарь не может знать их все. Незнакомое
 * остаётся без флага: пририсовать не тот флаг хуже, чем не рисовать никакого —
 * человек выберет страну по картинке и уедет не туда, куда собирался.
 */
object Flags {

    /** Пустая строка означает «страну не узнали». */
    fun of(country: String): String {
        val code = codeOf(country) ?: return ""

        // Флаг в юникоде — это две буквы кода страны особыми знаками.
        val base = 0x1F1E6
        val first = base + (code[0].code - 'a'.code)
        val second = base + (code[1].code - 'a'.code)
        return String(Character.toChars(first)) + String(Character.toChars(second))
    }

    /** CountryGuess uses the same aliases as flag rendering. */
    internal fun codeOf(name: String): String? {
        val key = name.lowercase().replace('ё', 'е').trim()
        return CODES[key] ?: key.takeIf { it.length == 2 && it in CODES.values }
    }

    internal fun countryOf(code: String): String? = when (code) {
        "ae" -> "ОАЭ"
        "us" -> "США"
        "za" -> "ЮАР"
        else -> CODES.entries
            .firstOrNull { it.value == code && it.key.any { letter -> letter in 'а'..'я' } }
            ?.key?.replaceFirstChar { it.titlecase() }
    }

    // Long names first: «Южная Корея» must win over «Корея».
    private val namesInText by lazy {
        CODES.entries.filter { it.key.length > 2 }.sortedByDescending { it.key.length }
    }

    internal fun codeInName(name: String): String? {
        val padded = " $name "
        return namesInText.firstOrNull { padded.contains(" ${it.key} ") }?.value
    }

    private val CODES: Map<String, String> = mapOf(
        // Русские названия: продавцы пишут по-русски чаще всего.
        "оаэ" to "ae",
        "объединенные арабские эмираты" to "ae",
        "эмираты" to "ae",
        "нидерланды" to "nl",
        "голландия" to "nl",
        "финляндия" to "fi",
        "германия" to "de",
        "франция" to "fr",
        "великобритания" to "gb",
        "англия" to "gb",
        "сша" to "us",
        "соединенные штаты" to "us",
        "канада" to "ca",
        "швеция" to "se",
        "швейцария" to "ch",
        "норвегия" to "no",
        "дания" to "dk",
        "исландия" to "is",
        "ирландия" to "ie",
        "испания" to "es",
        "италия" to "it",
        "португалия" to "pt",
        "греция" to "gr",
        "австрия" to "at",
        "бельгия" to "be",
        "люксембург" to "lu",
        "польша" to "pl",
        "чехия" to "cz",
        "словакия" to "sk",
        "словения" to "si",
        "венгрия" to "hu",
        "румыния" to "ro",
        "болгария" to "bg",
        "сербия" to "rs",
        "хорватия" to "hr",
        "молдова" to "md",
        "молдавия" to "md",
        "украина" to "ua",
        "латвия" to "lv",
        "литва" to "lt",
        "эстония" to "ee",
        "кипр" to "cy",
        "турция" to "tr",
        "израиль" to "il",
        "грузия" to "ge",
        "армения" to "am",
        "азербайджан" to "az",
        "казахстан" to "kz",
        "узбекистан" to "uz",
        "киргизия" to "kg",
        "кыргызстан" to "kg",
        "белоруссия" to "by",
        "беларусь" to "by",
        "россия" to "ru",
        "индия" to "in",
        "китай" to "cn",
        "тайвань" to "tw",
        "гонконг" to "hk",
        "япония" to "jp",
        "южная корея" to "kr",
        "корея" to "kr",
        "сингапур" to "sg",
        "малайзия" to "my",
        "индонезия" to "id",
        "таиланд" to "th",
        "вьетнам" to "vn",
        "австралия" to "au",
        "новая зеландия" to "nz",
        "бразилия" to "br",
        "аргентина" to "ar",
        "чили" to "cl",
        "мексика" to "mx",
        "юар" to "za",
        "египет" to "eg",
        "катар" to "qa",
        "бахрейн" to "bh",
        "кувейт" to "kw",
        "саудовская аравия" to "sa",
        "иран" to "ir",
        "пакистан" to "pk",
        "сербия и черногория" to "rs",

        // Английские: панель у части продавцов ведётся на английском.
        "uae" to "ae",
        "united arab emirates" to "ae",
        "netherlands" to "nl",
        "finland" to "fi",
        "germany" to "de",
        "france" to "fr",
        "united kingdom" to "gb",
        "great britain" to "gb",
        "uk" to "gb",
        "usa" to "us",
        "united states" to "us",
        "canada" to "ca",
        "sweden" to "se",
        "switzerland" to "ch",
        "norway" to "no",
        "denmark" to "dk",
        "iceland" to "is",
        "ireland" to "ie",
        "spain" to "es",
        "italy" to "it",
        "portugal" to "pt",
        "greece" to "gr",
        "austria" to "at",
        "belgium" to "be",
        "luxembourg" to "lu",
        "poland" to "pl",
        "czechia" to "cz",
        "czech republic" to "cz",
        "slovakia" to "sk",
        "slovenia" to "si",
        "hungary" to "hu",
        "romania" to "ro",
        "bulgaria" to "bg",
        "serbia" to "rs",
        "croatia" to "hr",
        "moldova" to "md",
        "ukraine" to "ua",
        "latvia" to "lv",
        "lithuania" to "lt",
        "estonia" to "ee",
        "cyprus" to "cy",
        "turkey" to "tr",
        "israel" to "il",
        "georgia" to "ge",
        "armenia" to "am",
        "azerbaijan" to "az",
        "kazakhstan" to "kz",
        "uzbekistan" to "uz",
        "kyrgyzstan" to "kg",
        "belarus" to "by",
        "russia" to "ru",
        "india" to "in",
        "china" to "cn",
        "taiwan" to "tw",
        "hong kong" to "hk",
        "japan" to "jp",
        "south korea" to "kr",
        "korea" to "kr",
        "singapore" to "sg",
        "malaysia" to "my",
        "indonesia" to "id",
        "thailand" to "th",
        "vietnam" to "vn",
        "australia" to "au",
        "new zealand" to "nz",
        "brazil" to "br",
        "argentina" to "ar",
        "chile" to "cl",
        "mexico" to "mx",
        "south africa" to "za",
        "egypt" to "eg",
        "qatar" to "qa",
        "bahrain" to "bh",
        "kuwait" to "kw",
        "saudi arabia" to "sa",
        "iran" to "ir",
        "pakistan" to "pk",
    )
}

/**
 * A foreign subscription has no country field, but its node label often does.
 * Infer only from an explicit flag, country, country code, or unambiguous city.
 * No geolocation service is called: CDN entry addresses are frequently in a
 * different country from the exit, and sending them to a third party would
 * disclose private subscription endpoints.
 */
internal object CountryGuess {
    private val separators = Regex("[^\\p{L}\\p{N}]+")
    private val lowerCodePrefix = Regex("^\\s*([a-z]{2})[-_]\\d+(?:\\D|$)")
    private val bracketedCode = Regex("\\[([A-Za-z]{2})]")

    private val cities = mapOf(
        "амстердам" to "nl", "amsterdam" to "nl",
        "хельсинки" to "fi", "helsinki" to "fi",
        "дубай" to "ae", "dubai" to "ae",
        "франкфурт" to "de", "frankfurt" to "de",
        "варшава" to "pl", "warsaw" to "pl",
        "стокгольм" to "se", "stockholm" to "se",
        "цюрих" to "ch", "zurich" to "ch",
        "лондон" to "gb", "london" to "gb",
        "париж" to "fr", "paris" to "fr",
        "мадрид" to "es", "madrid" to "es",
        "токио" to "jp", "tokyo" to "jp",
        "сингапур" to "sg", "singapore" to "sg",
    )

    fun fromName(name: String): String {
        val code = flagCode(name)
            ?: explicitCode(name)
            ?: run {
                val normalized = name.lowercase().replace('ё', 'е')
                    .replace(separators, " ").trim()
                Flags.codeInName(normalized)
                    ?: cities.entries.firstOrNull { " $normalized ".contains(" ${it.key} ") }?.value
            }
        return code?.let(Flags::countryOf).orEmpty()
    }

    private fun flagCode(name: String): String? {
        var offset = 0
        while (offset < name.length) {
            val first = name.codePointAt(offset)
            val next = offset + Character.charCount(first)
            if (first in 0x1F1E6..0x1F1FF && next < name.length) {
                val second = name.codePointAt(next)
                if (second in 0x1F1E6..0x1F1FF) {
                    val code = "${'a' + (first - 0x1F1E6)}${'a' + (second - 0x1F1E6)}"
                    return Flags.codeOf(code)
                }
            }
            offset = next
        }
        return null
    }

    private fun explicitCode(name: String): String? {
        bracketedCode.find(name)?.groupValues?.get(1)?.let { Flags.codeOf(it)?.let { code -> return code } }
        val first = name.trimStart().takeWhile { it.isLetter() }
        if (first.length == 2 && first.all { it.isUpperCase() }) {
            Flags.codeOf(first)?.let { return it }
        }
        return lowerCodePrefix.find(name)?.groupValues?.get(1)?.let(Flags::codeOf)
    }
}
