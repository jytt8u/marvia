package io.marvia.android

// Файл собран генератором из internal/look/look.js — руками не править.
// Обновить: go generate ./internal/look. Тест в том же пакете следит,
// что этот файл не отстал от таблицы.

/** Таблица тем, общая с панелью и окном на компьютере. */
object LookTable {
    /** Пресеты в порядке экрана выбора: ключ, фон, текст, акцент. */
    val presets: List<Preset> = listOf(
        Preset("emerald", 0xFF06100C, 0xFFE8F6EF, 0xFF1FD18D),
        Preset("jade", 0xFF071310, 0xFFE3F5F0, 0xFF33D6C0),
        Preset("teal", 0xFF04100F, 0xFFDFF5F3, 0xFF00D4C8),
        Preset("ice", 0xFF060C14, 0xFFE2F0FB, 0xFF54C8FF),
        Preset("cobalt", 0xFF070A18, 0xFFE6EAFF, 0xFF5C7CFF),
        Preset("ultra", 0xFF05061A, 0xFFE4E7FF, 0xFF3D5BFF),
        Preset("plum", 0xFF0B0714, 0xFFEFE7FB, 0xFFA86DFF),
        Preset("orchid", 0xFF100716, 0xFFF7E8FB, 0xFFE46CE0),
        Preset("fuchsia", 0xFF120616, 0xFFFBE6F6, 0xFFFF5ECD),
        Preset("rose", 0xFF120709, 0xFFFBE8EC, 0xFFFF5F7E),
        Preset("crimson", 0xFF130508, 0xFFFBE3E6, 0xFFFF3B52),
        Preset("ember", 0xFF120902, 0xFFFBEADC, 0xFFFF7A3D),
        Preset("amber", 0xFF100C04, 0xFFF8F0DD, 0xFFFFC247),
        Preset("gold", 0xFF0F0D05, 0xFFF7F1DC, 0xFFE8C25A),
        Preset("citrus", 0xFF0C0F05, 0xFFF0F5DD, 0xFFC9E64B),
        Preset("lime", 0xFF080F06, 0xFFECF7E2, 0xFF8FF04F),
        Preset("olive", 0xFF0A0D07, 0xFFEAF1DE, 0xFF96BF5C),
        Preset("sand", 0xFF100D09, 0xFFF5EDE1, 0xFFD9A97A),
        Preset("copper", 0xFF110A06, 0xFFF8E9DD, 0xFFE08B4C),
        Preset("steel", 0xFF0C0E11, 0xFFF1F4F7, 0xFFC3CCD6),
        Preset("slate", 0xFF0E1013, 0xFFEEF1F4, 0xFF8FA3B8),
        Preset("mono", 0xFF0A0A0A, 0xFFFAFAFA, 0xFFFAFAFA),
        Preset("oled", 0xFF000000, 0xFFFFFFFF, 0xFF3DFFC0),
        Preset("midnight", 0xFF03050A, 0xFFDFE8F5, 0xFF4D8CFF),
        Preset("daylight", 0xFFF3F5F7, 0xFF0D1013, 0xFF046B4A),
        Preset("paper", 0xFFF7F4EE, 0xFF14120E, 0xFF8A4B1F),
        Preset("linen", 0xFFF4F1EA, 0xFF171512, 0xFF3F6B4A),
    )

    /** Плотности: ключ, отступ карточки, зазор — в dp. */
    val densities: List<Density> = listOf(
        Density("compact", 12, 8),
        Density("normal", 16, 12),
        Density("roomy", 20, 16),
    )

    /** Скругления: ключ и радиус в dp. */
    val radii: List<Radius> = listOf(
        Radius("sharp", 6),
        Radius("soft", 14),
        Radius("round", 22),
        Radius("pill", 30),
    )

    /** Свечение: ключ и сила от 0 до 1. */
    val glows: List<Glow> = listOf(
        Glow("none", 0.0),
        Glow("soft", 0.2),
        Glow("mid", 0.42),
        Glow("neon", 0.72),
    )

    /** Откуда светит: ключ и угол CSS-градиента; «c» — из центра. */
    val dirs: List<Dir> = listOf(
        Dir("n", 0, false),
        Dir("ne", 45, false),
        Dir("e", 90, false),
        Dir("se", 135, false),
        Dir("s", 180, false),
        Dir("sw", 225, false),
        Dir("w", 270, false),
        Dir("nw", 315, false),
        Dir("c", 315, true),
    )

    val kinds: List<String> = listOf("flat", "linear", "radial", "aurora")
    val buttons: List<String> = listOf("solid", "ring", "glass", "bare")
    val cards: List<String> = listOf("flat", "outline", "shadow")

    /** Готовые виды в порядке экрана выбора. */
    val looks: List<Look> = listOf(
        Look("steel", "linear", "nw", 0.55, "soft", "normal", "ring", "soft", "outline"),
        Look("emerald", "linear", "nw", 0.62, "soft", "normal", "ring", "mid", "outline"),
        Look("jade", "radial", "n", 0.58, "round", "normal", "ring", "mid", "outline"),
        Look("teal", "aurora", "ne", 0.66, "round", "normal", "solid", "mid", "flat"),
        Look("ice", "aurora", "ne", 0.8, "round", "normal", "solid", "neon", "flat"),
        Look("cobalt", "linear", "nw", 0.7, "soft", "normal", "ring", "mid", "outline"),
        Look("ultra", "radial", "c", 0.82, "round", "roomy", "solid", "neon", "shadow"),
        Look("plum", "radial", "se", 0.8, "round", "roomy", "glass", "mid", "shadow"),
        Look("orchid", "aurora", "w", 0.72, "pill", "normal", "glass", "mid", "flat"),
        Look("fuchsia", "aurora", "sw", 0.7, "round", "normal", "solid", "neon", "flat"),
        Look("rose", "linear", "s", 0.6, "pill", "roomy", "solid", "mid", "outline"),
        Look("crimson", "radial", "se", 0.85, "sharp", "compact", "ring", "mid", "flat"),
        Look("ember", "aurora", "s", 0.75, "soft", "normal", "solid", "mid", "shadow"),
        Look("amber", "linear", "s", 0.45, "pill", "roomy", "solid", "soft", "flat"),
        Look("gold", "radial", "n", 0.55, "round", "roomy", "glass", "soft", "outline"),
        Look("citrus", "linear", "ne", 0.5, "soft", "normal", "solid", "mid", "flat"),
        Look("lime", "aurora", "nw", 0.5, "round", "normal", "solid", "mid", "flat"),
        Look("olive", "linear", "w", 0.52, "soft", "normal", "ring", "soft", "outline"),
        Look("sand", "linear", "nw", 0.42, "pill", "roomy", "glass", "soft", "outline"),
        Look("copper", "linear", "e", 0.55, "pill", "roomy", "glass", "soft", "outline"),
        Look("steel", "flat", "n", 0.25, "sharp", "compact", "bare", "none", "outline"),
        Look("slate", "linear", "n", 0.4, "soft", "compact", "ring", "none", "outline"),
        Look("mono", "flat", "n", 0.3, "sharp", "compact", "bare", "none", "flat"),
        Look("oled", "flat", "n", 0.95, "soft", "compact", "ring", "soft", "outline"),
        Look("midnight", "radial", "c", 0.85, "soft", "normal", "ring", "mid", "shadow"),
        Look("daylight", "linear", "nw", 0.4, "soft", "normal", "ring", "soft", "outline"),
        Look("paper", "linear", "nw", 0.4, "soft", "roomy", "ring", "none", "outline"),
        Look("linen", "flat", "n", 0.3, "pill", "roomy", "glass", "none", "flat"),
    )

    /** Все акценты из пресетов без повторов, в порядке первого появления. */
    val accents: List<Int> = listOf(
        0xFF1FD18D.toInt(),
        0xFF33D6C0.toInt(),
        0xFF00D4C8.toInt(),
        0xFF54C8FF.toInt(),
        0xFF5C7CFF.toInt(),
        0xFF3D5BFF.toInt(),
        0xFFA86DFF.toInt(),
        0xFFE46CE0.toInt(),
        0xFFFF5ECD.toInt(),
        0xFFFF5F7E.toInt(),
        0xFFFF3B52.toInt(),
        0xFFFF7A3D.toInt(),
        0xFFFFC247.toInt(),
        0xFFE8C25A.toInt(),
        0xFFC9E64B.toInt(),
        0xFF8FF04F.toInt(),
        0xFF96BF5C.toInt(),
        0xFFD9A97A.toInt(),
        0xFFE08B4C.toInt(),
        0xFFC3CCD6.toInt(),
        0xFF8FA3B8.toInt(),
        0xFFFAFAFA.toInt(),
        0xFF3DFFC0.toInt(),
        0xFF4D8CFF.toInt(),
        0xFF046B4A.toInt(),
        0xFF8A4B1F.toInt(),
        0xFF3F6B4A.toInt(),
    )

    data class Preset(val key: String, val bg: Long, val fg: Long, val acc: Long)
    data class Density(val key: String, val pad: Int, val gap: Int)
    data class Radius(val key: String, val r: Int)
    data class Glow(val key: String, val a: Double)
    data class Dir(val key: String, val deg: Int, val centered: Boolean)
    data class Look(
        val preset: String, val kind: String, val dir: String, val depth: Double,
        val radius: String, val density: String, val btn: String, val glow: String, val card: String,
    )
}
