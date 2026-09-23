package io.marvia.android

import android.Manifest
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.graphics.drawable.GradientDrawable
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.view.View
import android.view.inputmethod.InputMethodManager
import android.widget.ImageView
import android.widget.TextView
import android.widget.Toast
import androidx.activity.OnBackPressedCallback
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.content.ContextCompat
import androidx.core.graphics.ColorUtils
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.doOnLayout
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.core.widget.TextViewCompat
import androidx.core.view.updatePadding
import androidx.core.view.updateLayoutParams
import androidx.core.widget.ImageViewCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import io.marvia.android.databinding.ActivityMainBinding
import io.marvia.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * MainActivity — все экраны приложения.
 *
 * Они лежат друг на друге в одной активности: подключение, выбор страны,
 * тема, «ещё» (соединение, приложения, логи), ключ доступа и выбор языка.
 * Отдельных активностей нет намеренно —
 * состояние туннеля живёт в процессе, и переключение вкладок не должно
 * пересобирать экран и терять то, что человек уже видел.
 *
 * Ключ — не вкладка: пока его нет, показывать нечего, и нижняя панель вместе
 * с остальными экранами просто не появляется.
 */
class MainActivity : AppCompatActivity() {

    private enum class Screen { LANGUAGE, PERMS, KEY, CONNECT, SERVERS, STATS, THEME, MORE }

    private lateinit var ui: ActivityMainBinding
    private lateinit var store: Store
    private lateinit var servers: ServersScreen
    private lateinit var stats: StatsScreen
    private lateinit var more: MoreScreen
    private lateinit var themeScreen: ThemeScreen
    private lateinit var language: LanguageScreen
    private lateinit var perms: PermsScreen

    /** Откуда открыт выбор языка: с первого запуска возврат ведёт дальше, из настроек — назад. */
    private var languageFromSettings = false

    /** Тема на сейчас. Пересчитывается по выбору и красит всё дерево вьюх. */
    private var theme: Theme = Look.theme(Look.Choice())

    private var screen = Screen.CONNECT

    /** Был ли туннель поднят на прошлой отрисовке: по смене обновляем список стран. */
    private var wasRunning = false

    /** Системное окно «разрешить приложению создавать VPN». */
    private val consent = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            launchService()
        } else {
            // Вид пустой: фразу мы уже написали сами, переводить нечего.
            MarviaState.set(TunnelState.Failed("", getString(R.string.consent_denied)))
        }
    }

    /**
     * Выбор фото под фон. GetContent, а не разрешение на «все файлы»: система
     * сама показывает выбор и отдаёт нам один файл, доступа к галерее целиком
     * не нужно. Сохраняем его к себе и перекрашиваем всё приложение.
     */
    private val pickBackdrop = registerForActivityResult(
        ActivityResultContracts.GetContent(),
    ) { uri ->
        if (uri != null && store.saveBackdrop(uri)) {
            repaint()
            // Сразу — выбрать кадр: фото редко ложится как надо само, и искать
            // потом, где это настраивается, никто не станет.
            themeScreen.editFrame()
        } else if (uri != null) {
            Toast.makeText(this, R.string.theme_backdrop_bad, Toast.LENGTH_LONG).show()
        }
    }

    /** Своя картинка под значок в шапке — тем же путём, что фон. */
    private val pickLogo = registerForActivityResult(
        ActivityResultContracts.GetContent(),
    ) { uri ->
        if (uri != null && store.saveLogo(uri)) {
            store.logo = Store.LOGO_CUSTOM
            repaint()
        } else if (uri != null) {
            Toast.makeText(this, R.string.theme_backdrop_bad, Toast.LENGTH_LONG).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        // Режим ночи — по темноте пресета. Он нужен не нам, а Material:
        // диалоги и системные виджеты берут цвета оттуда, и светлый диалог
        // над «Полночью» выглядел бы дырой в экране.
        store = Store(this)
        theme = Look.theme(store.look)
        AppCompatDelegate.setDefaultNightMode(nightModeFor(theme))
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)

        ui = ActivityMainBinding.inflate(layoutInflater)
        setContentView(ui.root)

        applyInsets()
        wireNav()
        wireConnect()
        wireKey()

        servers = ServersScreen(
            host = this,
            ui = ui.serversScreen,
            theme = { theme },
            store = store,
            onSubscriptionChanged = { restartTunnel() },
        )
        stats = StatsScreen(host = this, ui = ui.statsScreen, theme = { theme }, traffic = Traffic(this), store = store)
        themeScreen = ThemeScreen(this, ui.themeScreen, store, { pickBackdrop.launch("image/*") }, { pickLogo.launch("image/*") }) { repaint() }
        language = LanguageScreen(this, ui.languageScreen, store, { theme }) { afterLanguage() }
        perms = PermsScreen(this, ui.permsScreen, { theme }) {
            store.onboarded = true
            show(Screen.CONNECT)
        }
        more = MoreScreen(
            host = this,
            ui = ui.moreScreen,
            theme = { theme },
            store = store,
            onLanguage = {
                languageFromSettings = true
                show(Screen.LANGUAGE)
            },
            onRoutesChanged = {
                refreshRoutes()
                // Исключения читаются при поднятии туннеля: менять маршруты у
                // работающего VPN нельзя, его надо пересобрать.
                if (MarviaState.state.value is TunnelState.On) {
                    Toast.makeText(this, R.string.bypass_restart, Toast.LENGTH_LONG).show()
                }
            },
            // IPv6 и дробление тоже читаются при подключении, но к маршрутам
            // отношения не имеют: звать ради них российский список у панели —
            // лишний запрос на каждое касание переключателя.
            onNextConnect = {
                if (MarviaState.state.value is TunnelState.On) {
                    Toast.makeText(this, R.string.bypass_restart, Toast.LENGTH_LONG).show()
                }
            },
            onReset = { resetAll() },
        )

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                launch { MarviaState.state.collect { render(it) } }
            }
        }

        onBackPressedDispatcher.addCallback(this, back)

        repaint()
        show(firstScreen())
        refreshRoutes()
        acceptLinkFrom(intent)
    }

    override fun onResume() {
        super.onResume()
        // Вернулись из системного окна разрешений — карточки могли поменяться.
        if (screen == Screen.PERMS) perms.open()
        // Включён «мимо туннеля», а списка нет — пробуем скачать, не дожидаясь,
        // пока человек переключит тумблер ещё раз: раньше единственная попытка
        // была в момент включения, и неудачная оставалась навсегда.
        if (store.bypassRussian && !RuRoutes.ready(this)) refreshRoutes(force = false)
    }

    /**
     * После выбора языка показываем главное пространство. Ключ добавляется
     * явной кнопкой: оформление можно выбрать ещё до подключения.
     */
    private fun firstScreen(): Screen = when {
        store.language.isEmpty() -> Screen.LANGUAGE
        !store.onboarded -> Screen.PERMS
        else -> Screen.CONNECT
    }

    /** afterLanguage — язык выбран: назад в настройки или дальше по первому запуску. */
    private fun afterLanguage() {
        if (languageFromSettings) {
            languageFromSettings = false
            show(Screen.MORE)
        } else {
            show(firstScreen())
        }
    }

    /** Возврат с любого экрана ведёт на подключение, а с него — из приложения. */
    private val back = object : OnBackPressedCallback(true) {
        override fun handleOnBackPressed() {
            // Из выбора языка, открытого из настроек, — назад в настройки.
            if (screen == Screen.LANGUAGE && languageFromSettings) {
                languageFromSettings = false
                show(Screen.MORE)
                return
            }
            // С подэкрана настроек (приложения, журнал) — назад в настройки.
            if (screen == Screen.MORE && more.back()) return
            if (screen == Screen.CONNECT || screen == Screen.LANGUAGE || screen == Screen.PERMS) {
                isEnabled = false
                onBackPressedDispatcher.onBackPressed()
                isEnabled = true
                return
            }
            show(Screen.CONNECT)
        }
    }

    // ---------------------------------------------------------------- тема

    /**
     * repaint пересчитывает тему по выбору и красит всё приложение.
     *
     * Дерево вьюх красится по тегам, состояние туннеля — своей отрисовкой,
     * экраны с динамикой — своими paint. Если пресет сменил тёмное на светлое
     * или обратно, режим ночи переключаем тут же: uiMode объявлен в манифесте,
     * и система экран не пересоздаёт, а значит, красить всё равно нам —
     * ранний выход здесь оставлял светлую тему на тёмных карточках до
     * следующего запуска.
     */
    private fun repaint() {
        theme = Look.theme(store.look)

        val night = nightModeFor(theme)
        if (AppCompatDelegate.getDefaultNightMode() != night) {
            AppCompatDelegate.setDefaultNightMode(night)
        }

        Paint.style = Paint.Style(pattern = store.pattern, font = store.font, photo = store.hasBackdrop())
        Paint.apply(ui.root, theme)
        applyBackdrop()
        paintLogo()
        paintNav()
        servers.paint()
        stats.paint(theme)
        themeScreen.paint(theme)
        more.paint()
        render(MarviaState.state.value)

        // Часы и кнопки системы над нашим фоном: тёмные на светлой теме,
        // светлые на тёмной. Иначе на «Бумаге» строка состояния пропадает.
        val bars = WindowCompat.getInsetsController(window, ui.root)
        bars.isAppearanceLightStatusBars = !theme.dark
        bars.isAppearanceLightNavigationBars = !theme.dark
    }

    /** paintLogo — что в шапке главной: знак, своя картинка или ничего. */
    private fun paintLogo() {
        val c = ui.connectScreen
        val mode = store.logo
        val custom = if (mode == Store.LOGO_CUSTOM) store.logoBitmap() else null
        c.heroMarkButton.isVisible = mode != Store.LOGO_NONE
        c.heroMark.isVisible = mode == Store.LOGO_MARVIA || (mode == Store.LOGO_CUSTOM && custom == null)
        c.heroCustom.isVisible = custom != null
        if (custom != null) {
            c.heroCustom.setImageBitmap(custom)
            c.heroCustom.clipToOutline = true
            c.heroCustom.outlineProvider = object : android.view.ViewOutlineProvider() {
                override fun getOutline(view: View, outline: android.graphics.Outline) {
                    outline.setRoundRect(0, 0, view.width, view.height, 9 * resources.displayMetrics.density)
                }
            }
        }
    }

    private fun nightModeFor(t: Theme): Int =
        if (t.dark) AppCompatDelegate.MODE_NIGHT_YES else AppCompatDelegate.MODE_NIGHT_NO

    /**
     * applyBackdrop кладёт свой фон поверх фона темы. Paint уже поставил корню
     * Backdrop(t); если у человека выбрано фото — заменяем им. Пелена цветом
     * фона темы держит читаемость, как и в панели.
     */
    private fun applyBackdrop() {
        val stamp = store.backdropStamp()
        if (stamp == 0L) {
            backdropBitmap?.recycle()
            backdropBitmap = null
            backdropStamp = 0L
            return
        }
        // Снимок декодируем один раз на файл, а не на каждую перерисовку:
        // ползунок затемнения зовёт repaint на каждом шаге, и читать JPEG в
        // две тысячи точек на каждый — это рывки вместо плавной пелены.
        if (backdropBitmap == null || backdropStamp != stamp) {
            backdropBitmap?.recycle()
            backdropBitmap = store.backdropBitmap()
            backdropStamp = stamp
        }
        val bmp = backdropBitmap ?: return
        val veil = colorWithAlpha(theme.bg, store.backdropDim)
        ui.root.background = BackdropImage(bmp, store.backdropFit, theme.bg, veil, store.backdropFrame)
    }

    private var backdropBitmap: android.graphics.Bitmap? = null
    private var backdropStamp = 0L

    /** colorWithAlpha — цвет фона темы с долей непрозрачности из процента затемнения. */
    private fun colorWithAlpha(color: Int, dimPercent: Int): Int {
        val a = (dimPercent.coerceIn(0, 95) * 255 / 100)
        return (a shl 24) or (color and 0x00FFFFFF)
    }

    // -------------------------------------------------------------- экраны

    private fun show(next: Screen) {
        val was = screen
        screen = next

        ui.keyScreen.root.isVisible = next == Screen.KEY
        ui.connectScreen.root.isVisible = next == Screen.CONNECT
        ui.serversScreen.root.isVisible = next == Screen.SERVERS
        ui.statsScreen.root.isVisible = next == Screen.STATS
        ui.moreScreen.root.isVisible = next == Screen.MORE
        ui.themeScreen.root.isVisible = next == Screen.THEME
        ui.languageScreen.root.isVisible = next == Screen.LANGUAGE
        ui.permsScreen.root.isVisible = next == Screen.PERMS
        // Экран поднимается снизу и проявляется, как .rise в макете: смена
        // вкладки без движения выглядит как сбой отрисовки, а не как переход.
        if (was != next) {
            val root = when (next) {
                Screen.CONNECT -> ui.connectScreen.root
                Screen.SERVERS -> ui.serversScreen.root
                Screen.STATS -> ui.statsScreen.root
                Screen.THEME -> ui.themeScreen.root
                Screen.MORE -> ui.moreScreen.root
                Screen.KEY -> ui.keyScreen.root
                Screen.LANGUAGE -> ui.languageScreen.root
                Screen.PERMS -> ui.permsScreen.root
            }
            root.alpha = 0f
            root.translationY = 12 * resources.displayMetrics.density
            root.animate().alpha(1f).translationY(0f).setDuration(420).setInterpolator(android.view.animation.DecelerateInterpolator(2f)).start()
            // На главной карточки внизу догоняют с шагом: экран собирается
            // сверху вниз, а не падает целиком.
            if (next == Screen.CONNECT) {
                val c = ui.connectScreen
                listOf(c.nodeLine, c.todayCard, c.tilesRow).forEachIndexed { i, v ->
                    v.alpha = 0f
                    v.translationY = 18 * resources.displayMetrics.density
                    v.animate().alpha(1f).translationY(0f).setStartDelay(60L + 70L * i).setDuration(460)
                        .setInterpolator(android.view.animation.DecelerateInterpolator(2.2f)).start()
                }
            }
        }

        // Панель есть и без ключа: подписка добавляется на «Серверах», а тему
        // можно выбрать до подключения. На выборе языка её нет — это экран
        // одного действия.
        ui.nav.root.isVisible = next != Screen.LANGUAGE && next != Screen.PERMS
        paintNav()

        when (next) {
            Screen.SERVERS -> servers.open()
            Screen.STATS -> stats.open()
            Screen.MORE -> more.open()
            Screen.THEME -> themeScreen.paint(theme)
            Screen.LANGUAGE -> language.open()
            Screen.PERMS -> perms.open()
            Screen.KEY -> openKey()
            Screen.CONNECT -> Unit
        }

        // Панель то появляется, то нет — а отступ под системной навигацией
        // должен остаться в обоих случаях.
        ViewCompat.requestApplyInsets(ui.root)
    }

    private fun wireNav() {
        ui.nav.navConnect.setOnClickListener { show(Screen.CONNECT) }
        ui.nav.navServers.setOnClickListener { show(Screen.SERVERS) }
        ui.nav.navStats.setOnClickListener { show(Screen.STATS) }
        ui.nav.navTheme.setOnClickListener { show(Screen.THEME) }
        ui.nav.navMore.setOnClickListener { show(Screen.MORE) }
    }

    private fun paintNav() {
        val n = ui.nav
        paintTab(n.navConnectPill, n.navConnectIcon, n.navConnectLabel, screen == Screen.CONNECT)
        paintTab(n.navServersPill, n.navServersIcon, n.navServersLabel, screen == Screen.SERVERS)
        paintTab(n.navStatsPill, n.navStatsIcon, n.navStatsLabel, screen == Screen.STATS)
        paintTab(n.navThemePill, n.navThemeIcon, n.navThemeLabel, screen == Screen.THEME)
        paintTab(n.navMorePill, n.navMoreIcon, n.navMoreLabel, screen == Screen.MORE)
    }

    /**
     * Активная вкладка отмечена заливкой акцента и контрастным значком.
     * Таблетка, которая только что стала активной, чуть подпрыгивает: так
     * видно, что нажатие принято, ещё до того, как сменился экран.
     */
    private fun paintTab(pill: View, icon: ImageView, label: TextView, active: Boolean) {
        val dp = resources.displayMetrics.density
        val color = if (active) theme.acc else theme.dim
        val wasActive = pill.background != null
        pill.background = if (active) Paint.rounded(theme.acc, 999, dp) else null
        if (active && !wasActive && pill.isAttachedToWindow) {
            pill.scaleX = .6f; pill.scaleY = .6f; pill.alpha = .3f
            pill.animate().scaleX(1f).scaleY(1f).alpha(1f).setDuration(340)
                .setInterpolator(android.view.animation.OvershootInterpolator(1.8f)).start()
        }
        ImageViewCompat.setImageTintList(icon, ColorStateList.valueOf(if (active) theme.accFg else color))
        label.setTextColor(color)
        label.typeface = if (active) android.graphics.Typeface.DEFAULT_BOLD else android.graphics.Typeface.DEFAULT
    }

    // --------------------------------------------------------- подключение

    private fun wireConnect() {
        if (MarviaState.state.value !is TunnelState.On) MarviaState.traffic.value = TrafficHistory(this).saved()
        val c = ui.connectScreen
        c.powerAction.setOnClickListener { toggle() }
        c.techText.setOnClickListener { more.openLogs(); show(Screen.MORE) }
        c.nodeLine.setOnClickListener { show(Screen.SERVERS) }

        // Нажатие на знак позволяет свернуть или вернуть подпись бренда.
        c.heroTagline.text = getString(R.string.hero_subtitle)
        c.heroWord.setTag(R.id.keep_font, true)
        // Имя — металлом, как знак: сверху светлое, книзу в приглушённый.
        c.heroWord.doOnLayout {
            c.heroWord.paint.shader = android.graphics.LinearGradient(
                0f, 0f, 0f, c.heroWord.height.toFloat(),
                intArrayOf(theme.fg, theme.fg, theme.dim), floatArrayOf(0f, 0.42f, 1f), android.graphics.Shader.TileMode.CLAMP,
            )
            c.heroWord.invalidate()
        }
        c.heroMarkButton.setOnClickListener {
            val show = !c.heroName.isVisible
            if (show) {
                c.heroName.alpha = 0f
                c.heroName.translationX = -10 * resources.displayMetrics.density
                c.heroName.isVisible = true
                c.heroName.animate().alpha(1f).translationX(0f).setDuration(450).setInterpolator(android.view.animation.DecelerateInterpolator(2f)).start()
            } else {
                c.heroName.animate().alpha(0f).setDuration(200).withEndAction { c.heroName.isVisible = false }.start()
            }
        }
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                MarviaState.traffic.collect { t ->
                    c.todayTotal.text = Format.size(this@MainActivity, t.today)
                    c.todayBars.hours = t.hours
                    c.sessionValue.text = String.format(java.util.Locale.US, "%02d:%02d:%02d", t.seconds / 3600, t.seconds / 60 % 60, t.seconds % 60)
                    c.speedValue.text = getString(R.string.stats_mbps, String.format(java.util.Locale.getDefault(), "%.1f", t.bytesPerSecond * 8 / 1_000_000))
                }
            }
        }
    }

    private fun render(state: TunnelState) {
        val c = ui.connectScreen
        val hasKey = store.accountLink.isNotBlank()

        c.techText.isVisible = false
        c.powerAction.state = when (state) {
            TunnelState.Off -> PowerButton.State.OFF
            TunnelState.Connecting -> PowerButton.State.CONNECTING
            is TunnelState.On -> PowerButton.State.ON
            is TunnelState.Failed -> PowerButton.State.FAILED
        }
        // Строка под состоянием: нода, а пока её нет — что делаем. Нажатие
        // ведёт к выбору сервера, поэтому строка есть и без туннеля.
        c.nodeLine.text = when (state) {
            TunnelState.Connecting -> getString(R.string.connect_choosing)
            is TunnelState.On -> listOf(state.node, if (state.ms > 0) getString(R.string.node_ms, state.ms) else "")
                .filter { it.isNotEmpty() }.joinToString(" · ")
            else -> lastNode
        }
        if (state is TunnelState.On) lastNode = state.node
        c.nodeLine.isVisible = c.nodeLine.text.isNotEmpty()
        c.powerHint.setText(when (state) {
            TunnelState.Off -> if (hasKey) R.string.power_hint_start else R.string.connect_add_key
            TunnelState.Connecting -> R.string.power_hint_cancel
            is TunnelState.On -> R.string.power_hint_stop
            is TunnelState.Failed -> R.string.connect_retry
        })

        when (state) {
            TunnelState.Off -> {
                status(if (hasKey) R.string.status_off else R.string.connect_welcome, theme.fg)
                c.powerAction.contentDescription = getString(if (hasKey) R.string.action_connect else R.string.connect_add_key)
                paintPower(theme.acc)
                c.nodeNote.text = ""
            }

            TunnelState.Connecting -> {
                status(R.string.status_connecting, theme.dim)
                c.powerAction.contentDescription = getString(R.string.status_connecting)
                paintPower(theme.acc)
                c.nodeNote.text = ""
            }

            is TunnelState.On -> {
                status(R.string.status_on, if (theme.dark) theme.fg else theme.acc)
                c.powerAction.contentDescription = getString(R.string.connect_disconnect)
                paintPower(theme.acc)
                c.nodeNote.text = choiceText(state)

                // Ошибка отдельного соединения туннель не роняет, но молчать о
                // ней нельзя: иначе человек видит «подключено» при наполовину
                // живой ноде.
                if (state.warning.isNotBlank()) {
                    c.techText.text = if (state.warning == getString(R.string.trouble_no_node)) state.warning else getString(R.string.connection_warning_details)
                    c.techText.setTextColor(theme.warn)
                    c.techText.isVisible = true
                }

                offerRussianBypass()
            }

            is TunnelState.Failed -> {
                status(R.string.status_failed, theme.fail)
                c.powerAction.contentDescription = getString(R.string.connect_retry)
                paintPower(theme.fail)

                val human = humanReasonFor(state.kind)
                if (human == null) {
                    // Вида нет — значит фраза уже человеческая, показываем её.
                    c.nodeNote.setText(R.string.connect_error_hint)
                    c.techText.setText(R.string.connection_warning_details)
                    c.techText.isVisible = state.detail.isNotBlank()
                } else {
                    c.nodeNote.setText(human)
                    c.techText.setText(R.string.connection_warning_details)
                    c.techText.setTextColor(theme.dim)
                    c.techText.isVisible = state.detail.isNotBlank()
                }
            }
        }
        // Пустая строка — это не строка: место под неё занимать незачем.
        c.nodeNote.isVisible = c.nodeNote.text.isNotEmpty()

        renderSubscription(state)

        // Список стран живёт внутри туннеля: он появляется и пропадает вместе
        // с ним, и открытый экран стран должен это заметить.
        val running = state is TunnelState.On
        if (running != wasRunning) {
            wasRunning = running
            if (screen == Screen.SERVERS) {
                servers.open()
            }
        }
    }

    /**
     * choiceText — кто выбрал ноду, через которую идёт трафик.
     *
     * Выбор человека и текущая нода расходятся, когда выбранная замолчала и
     * ядро уехало на живую. Написать в этот момент «выбрана ОАЭ» над словом
     * «Финляндия» значило бы соврать в единственном месте, где человек
     * проверить нас не может.
     */
    private fun choiceText(state: TunnelState.On): String = when {
        // Автовыбор — это молчание: он и так по умолчанию, и сообщать о нём
        // нечего. Строка появляется, только когда есть что сказать: человек
        // выбрал страну сам, или выбрал одну, а ядро уехало на другую.
        state.chosen.isEmpty() -> ""
        state.chosen == state.node -> getString(R.string.connect_manual)
        else -> getString(R.string.connect_manual_moved, state.chosen)
    }

    /** Последняя нода, через которую шёл трафик: её показываем и выключенными — и после перезапуска. */
    private var lastNode: String
        get() = store.lastNode
        set(value) { store.lastNode = value }

    /** Состояние — одной крупной строкой под кнопкой, цветом состояния. */
    /** Состояние крупно; новое слово проявляется, а не подменяет прежнее рывком. */
    private fun status(text: Int, color: Int) {
        val v = ui.connectScreen.statusText
        val changed = v.text.toString() != getString(text)
        v.setText(text)
        v.setTextColor(color)
        if (changed && v.isAttachedToWindow && v.isShown) {
            v.alpha = 0f
            v.translationY = 6 * resources.displayMetrics.density
            v.animate().alpha(1f).translationY(0f).setDuration(360).setInterpolator(android.view.animation.DecelerateInterpolator(2f)).start()
        }
    }

    /** Цвет ленты — личный выбор; состояние передаём дугой и строкой. */
    private fun paintPower(color: Int) {
        val c = ui.connectScreen
        c.todayBars.theme = theme
        c.halo.theme = theme
        c.halo.lit = MarviaState.state.value is TunnelState.On
        c.powerAction.theme = theme.copy(acc = color)
    }

    /**
     * renderSubscription — срок и остаток живут на «Серверах», у своего
     * провайдера. Здесь от подписки остаётся одно: есть ли новая версия.
     */
    private fun renderSubscription(state: TunnelState) {
        more.showUpdate((state as? TunnelState.On)?.subscription)
    }

    // --------------------------------------------------------------- ключ

    private fun wireKey() {
        val k = ui.keyScreen
        k.pasteButton.setOnClickListener { paste() }
        k.keySaveButton.setOnClickListener { onKeyButton() }
    }

    private fun openKey() {
        val k = ui.keyScreen
        k.keyInput.setText(store.accountLink)
        k.keyError.isVisible = false
        // Туннель уже работает — кнопка сохраняет ключ, а не подключает: два
        // разных действия под одной надписью человек нажимает наугад.
        k.keySaveButton.setText(
            if (MarviaState.state.value is TunnelState.On) R.string.action_save else R.string.action_connect,
        )
    }

    /**
     * paste берёт ссылку из буфера обмена.
     *
     * Ключ приходит в чате, и до приложения он почти всегда доезжает именно
     * так: человек скопировал и не знает, куда вставить.
     */
    private fun paste() {
        val clipboard = getSystemService(ClipboardManager::class.java)
        val text = clipboard?.primaryClip
            ?.takeIf { it.itemCount > 0 }
            ?.getItemAt(0)
            ?.coerceToText(this)
            ?.toString()
            ?.trim()
            .orEmpty()

        if (text.isEmpty()) {
            Toast.makeText(this, R.string.key_clipboard_empty, Toast.LENGTH_SHORT).show()
            return
        }
        ui.keyScreen.keyInput.setText(text)
    }

    private fun onKeyButton() {
        if (!saveKey()) {
            return
        }

        // Работающий туннель на новый ключ не переводим сами: это обрыв связи
        // посреди чужого дела, и решать про него человеку.
        val running = MarviaState.state.value is TunnelState.On
        // Через firstScreen, а не сразу на подключение: ключ мог приехать
        // ссылкой при самом первом запуске, и язык ещё не выбран.
        show(firstScreen())
        if (!running) {
            toggle()
        }
    }

    private fun saveKey(): Boolean {
        val k = ui.keyScreen
        val link = k.keyInput.text?.toString()?.trim().orEmpty()
        if (link.isEmpty()) {
            showKeyError(getString(R.string.key_empty))
            return false
        }

        // Разбираем ссылку ядром, а не своим кодом на Kotlin. Правило, что
        // считать правильной ссылкой, живёт в одном месте — иначе приложение
        // и сервер однажды разойдутся во мнениях, и разбираться в этом будет
        // человек, который просто хотел включить интернет.
        try {
            Mobile.checkAccountLink(link)
        } catch (t: Throwable) {
            showKeyError(MarviaVpnService.reasonOf(t))
            return false
        }

        k.keyError.isVisible = false
        store.accountLink = link
        hideKeyboard()
        Toast.makeText(this, R.string.key_saved, Toast.LENGTH_SHORT).show()
        refreshRoutes()
        return true
    }

    private fun showKeyError(text: String) {
        ui.keyScreen.keyError.text = text
        ui.keyScreen.keyError.isVisible = true
    }

    /**
     * acceptLinkFrom подставляет ключ из нажатой ссылки marvia://.
     *
     * Отправить её может любое приложение — телеграм, браузер, что угодно.
     * Поэтому замена уже стоящего ключа спрашивается: подменённая ссылка увела
     * бы весь трафик покупателя на серверы того, кто её подсунул, а заметить
     * это ему нечем. Когда ключа ещё нет, спрашивать не о чем — это обычная
     * первая настройка, ради которой ссылка и придумана.
     */
    private fun acceptLinkFrom(intent: Intent) {
        val link = intent.data?.toString()?.trim().orEmpty()
        if (link.isEmpty()) {
            return
        }

        // Ссылку из намерения убираем сразу: иначе возврат из системного окна
        // разрешения или смена темы подставит её заново.
        intent.data = null

        if (store.accountLink.isBlank() || store.accountLink == link) {
            takeLink(link)
            return
        }

        ThemedDialogs.builder(this, theme)
            .setTitle(R.string.key_replace_title)
            .setMessage(R.string.key_replace_body)
            .setPositiveButton(R.string.key_replace_yes) { _, _ -> takeLink(link) }
            .setNegativeButton(R.string.key_replace_no, null)
            .show()
    }

    private fun takeLink(link: String) {
        show(Screen.KEY)
        ui.keyScreen.keyInput.setText(link)
        if (saveKey()) {
            show(firstScreen())
        }
    }

    /**
     * onNewIntent ловит ссылку, когда приложение уже открыто.
     *
     * У активности launchMode=singleTask, и второй раз onCreate не позовут: без
     * этого нажатие на ссылку при открытом приложении не делало бы ничего, и
     * человек решил бы, что ссылка нерабочая.
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        acceptLinkFrom(intent)
    }

    // ------------------------------------------------------------- прочее

    /**
     * offerRussianBypass предлагает увести российские сайты мимо туннеля.
     *
     * Спрашиваем один раз при включении, а не прячем в настройки: человек
     * узнаёт про эту возможность ровно тогда, когда у него не открылись
     * госуслуги, — то есть уже разозлившись.
     */
    private fun offerRussianBypass() {
        if (!RuRoutes.supported() || store.bypassRussian || store.bypassAsked) {
            return
        }
        store.bypassAsked = true

        ThemedDialogs.builder(this, theme)
            .setTitle(R.string.bypass_ru_title)
            .setMessage(R.string.bypass_ru_body)
            .setPositiveButton(R.string.bypass_ru_yes) { _, _ ->
                store.bypassRussian = true
                refreshRoutes()
                Toast.makeText(this, R.string.bypass_restart, Toast.LENGTH_LONG).show()
            }
            .setNegativeButton(R.string.bypass_ru_no, null)
            .show()
    }

    /** refreshRoutes подтягивает список подсетей с панели продавца. */
    private fun refreshRoutes(force: Boolean = true) {
        val link = store.accountLink.takeIf { it.isNotBlank() } ?: return
        lifecycleScope.launch {
            withContext(Dispatchers.IO) { RuRoutes.refresh(applicationContext, link, force) }
            // Строка под переключателем говорит, скачан ли список, — обновить её.
            more.paint()
        }
    }

    /**
     * applyInsets отодвигает содержимое от системных панелей.
     *
     * С Android 15 приложение рисуется под строкой состояния и панелью
     * навигации, хочет оно того или нет. Без этого заголовок уезжает под часы,
     * а нижняя панель — под системную навигацию.
     */
    private fun applyInsets() {
        val pad = ui.nav.navBar.paddingBottom
        ViewCompat.setOnApplyWindowInsetsListener(ui.root) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            val ime = insets.getInsets(WindowInsetsCompat.Type.ime())

            ui.nav.navBar.updatePadding(bottom = pad)
            ui.nav.root.updateLayoutParams<android.view.ViewGroup.MarginLayoutParams> {
                bottomMargin = bars.bottom + (8 * resources.displayMetrics.density).toInt()
            }
            // Когда панели нет, её отступ забирает содержимое — иначе экран
            // ключа упирается в системную навигацию.
            val below = if (ui.nav.root.isVisible) 0 else bars.bottom
            ui.content.updatePadding(top = bars.top, bottom = maxOf(below, ime.bottom))
            insets
        }
    }

    private fun toggle() {
        if (MarviaState.state.value is TunnelState.On) {
            startService(
                Intent(this, MarviaVpnService::class.java).setAction(MarviaVpnService.ACTION_STOP),
            )
            return
        }

        if (store.accountLink.isBlank()) {
            show(Screen.KEY)
            return
        }

        // Система спрашивает разрешение один раз на установку — но спросить
        // всё равно надо каждый раз: его могли отозвать, включив другой VPN.
        val intent = VpnService.prepare(this)
        if (intent == null) {
            launchService()
        } else {
            consent.launch(intent)
        }
    }

    /**
     * restartTunnel — подписка сменилась, а с ней и ключ покупателя.
     *
     * Ядро держит ключ на всё время работы, поэтому поднятый туннель надо
     * ронять: иначе человек сменил продавца, а трафик продолжает идти через
     * прежнего — и нигде это не написано.
     */
    /**
     * resetAll — «сбросить всё»: туннель вниз, настройки в ноль, экран заново.
     * Пересоздаём активность, а не чистим экраны по одному: первый запуск и
     * так умеет начинать с пустого места, и второго пути быть не должно.
     */
    private fun resetAll() {
        if (MarviaState.state.value is TunnelState.On || MarviaState.state.value is TunnelState.Connecting) {
            startService(Intent(this, MarviaVpnService::class.java).setAction(MarviaVpnService.ACTION_STOP))
        }
        store.resetAll()
        recreate()
    }

    private fun restartTunnel() {
        if (MarviaState.state.value !is TunnelState.On) return
        startService(
            Intent(this, MarviaVpnService::class.java).setAction(MarviaVpnService.ACTION_STOP),
        )
    }

    private fun launchService() {
        ContextCompat.startForegroundService(this, Intent(this, MarviaVpnService::class.java))
    }

    /**
     * humanReasonFor подбирает фразу под вид неудачи, названный ядром.
     *
     * Ядро говорит точно: «dial tcp: lookup panel.example: no such host». Для
     * разбора это незаменимо, для покупателя — пустой звук. Поэтому наверху
     * стоит фраза с указанием, что делать, а точный текст остаётся ниже
     * мелким: его покупатель и снимет вместе с экраном для продавца.
     *
     * null означает, что вида нет и подбирать нечего.
     */
    private fun humanReasonFor(kind: String): Int? = when (kind) {
        Mobile.FailAccount -> R.string.fail_account
        Mobile.FailPanel -> R.string.fail_panel
        Mobile.FailNodes -> R.string.fail_nodes
        Mobile.FailSystem -> R.string.fail_system
        Mobile.FailExpired -> R.string.fail_expired
        Mobile.FailQuota -> R.string.fail_quota
        else -> null
    }

    private fun hideKeyboard() {
        val manager = getSystemService(InputMethodManager::class.java)
        manager?.hideSoftInputFromWindow(ui.keyScreen.keyInput.windowToken, 0)
        ui.keyScreen.keyInput.clearFocus()
    }
}
