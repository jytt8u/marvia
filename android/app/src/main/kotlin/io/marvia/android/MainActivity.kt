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
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.core.view.updatePadding
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

    private enum class Screen { LANGUAGE, KEY, CONNECT, SERVERS, STATS, THEME, MORE }

    private lateinit var ui: ActivityMainBinding

    /** Своя версия — под надписью в шапке, когда её раскрыли. */
    private val ownVersion: String by lazy {
        try {
            packageManager.getPackageInfo(packageName, 0).versionName.orEmpty()
        } catch (_: android.content.pm.PackageManager.NameNotFoundException) {
            ""
        }
    }
    private lateinit var store: Store
    private lateinit var servers: ServersScreen
    private lateinit var stats: StatsScreen
    private lateinit var more: MoreScreen
    private lateinit var themeScreen: ThemeScreen
    private lateinit var language: LanguageScreen

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

    /** Разрешение на уведомления. Отказ ничего не ломает: туннель работает. */
    private val notifications = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { }

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
        stats = StatsScreen(host = this, ui = ui.statsScreen, theme = { theme }, traffic = Traffic(this))
        themeScreen = ThemeScreen(this, ui.themeScreen, store, { pickBackdrop.launch("image/*") }) { repaint() }
        language = LanguageScreen(this, ui.languageScreen, store, { theme }) { afterLanguage() }
        more = MoreScreen(
            host = this,
            ui = ui.moreScreen,
            theme = { theme },
            store = store,
            onKey = { show(Screen.KEY) },
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
        )

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                launch { MarviaState.state.collect { render(it) } }
                launch { MarviaState.traffic.collect { ui.connectScreen.trafficPanel.snapshot = it } }
            }
        }

        onBackPressedDispatcher.addCallback(this, back)

        repaint()
        show(firstScreen())
        refreshRoutes()
        askForNotifications()
        acceptLinkFrom(intent)
    }

    /**
     * После выбора языка показываем главное пространство. Ключ добавляется
     * явной кнопкой: оформление можно выбрать ещё до подключения.
     */
    private fun firstScreen(): Screen = when {
        store.language.isEmpty() -> Screen.LANGUAGE
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
            if (screen == Screen.CONNECT || screen == Screen.LANGUAGE) {
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
     * или обратно, экран пересоздаётся системой ради режима ночи — тогда
     * красить сейчас незачем, onCreate сделает это заново.
     */
    private fun repaint() {
        theme = Look.theme(store.look)

        val night = nightModeFor(theme)
        if (AppCompatDelegate.getDefaultNightMode() != night) {
            AppCompatDelegate.setDefaultNightMode(night)
            return
        }

        Paint.apply(ui.root, theme)
        applyBackdrop()
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
        ui.root.background = BackdropImage(bmp, store.backdropFit, theme.bg, veil)
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
        screen = next

        ui.keyScreen.root.isVisible = next == Screen.KEY
        ui.connectScreen.root.isVisible = next == Screen.CONNECT
        ui.serversScreen.root.isVisible = next == Screen.SERVERS
        ui.statsScreen.root.isVisible = next == Screen.STATS
        ui.moreScreen.root.isVisible = next == Screen.MORE
        ui.themeScreen.root.isVisible = next == Screen.THEME
        ui.languageScreen.root.isVisible = next == Screen.LANGUAGE

        // Пока ключа нет, ходить некуда: панель появится вместе с ним. На
        // выборе языка её тоже нет — это экран одного действия.
        ui.nav.root.isVisible = store.accountLink.isNotBlank() && next != Screen.LANGUAGE
        paintNav()

        when (next) {
            Screen.SERVERS -> servers.open()
            Screen.STATS -> stats.open()
            Screen.MORE -> more.open()
            Screen.THEME -> themeScreen.paint(theme)
            Screen.LANGUAGE -> language.open()
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
        paintTab(ui.nav.navConnectIcon, ui.nav.navConnectLabel, screen == Screen.CONNECT)
        paintTab(ui.nav.navServersIcon, ui.nav.navServersLabel, screen == Screen.SERVERS)
        paintTab(ui.nav.navStatsIcon, ui.nav.navStatsLabel, screen == Screen.STATS)
        paintTab(ui.nav.navThemeIcon, ui.nav.navThemeLabel, screen == Screen.THEME)
        paintTab(ui.nav.navMoreIcon, ui.nav.navMoreLabel, screen == Screen.MORE)
    }

    /**
     * paintTab — как в макете: значок активной вкладки на пилюле мягкого
     * акцента, подпись — цветом текста и жирнее; остальные приглушены.
     */
    private fun paintTab(icon: ImageView, label: TextView, active: Boolean) {
        val color = if (active) theme.fg else theme.dim
        ImageViewCompat.setImageTintList(icon, ColorStateList.valueOf(color))
        icon.backgroundTintList = ColorStateList.valueOf(if (active) theme.accSoft else 0)
        label.setTextColor(color)
        label.typeface = if (active) Fonts.textBold(this) else Fonts.text(this)
    }

    // --------------------------------------------------------- подключение

    private fun wireConnect() {
        if (MarviaState.state.value !is TunnelState.On) MarviaState.traffic.value = TrafficHistory(this).saved()
        val c = ui.connectScreen
        c.powerAction.setOnClickListener { toggle() }
        // Знак без надписи, как в макете; нажатие показывает её с версией.
        c.heroMark.setOnClickListener { c.heroName.isVisible = !c.heroName.isVisible }
        c.heroVersion.text = getString(R.string.hero_version, ownVersion).uppercase()
        c.techText.setOnClickListener { more.openLogs(); show(Screen.MORE) }
        c.nodeLine.setOnClickListener { show(Screen.SERVERS) }
        // Полосу остатка скругляем по фону: иначе заливка вылезает углами.
        c.trafficTrack.clipToOutline = true
    }

    private fun render(state: TunnelState) {
        val c = ui.connectScreen
        val hasKey = store.accountLink.isNotBlank()

        c.techText.isVisible = false
        c.nodeLine.isVisible = false
        c.powerAction.phase = when (state) {
            is TunnelState.On -> PowerButton.Phase.ON
            TunnelState.Connecting -> PowerButton.Phase.CONNECTING
            else -> PowerButton.Phase.OFF
        }

        when (state) {
            TunnelState.Off -> {
                status(if (hasKey) R.string.status_off else R.string.connect_welcome, theme.dim)
                paintPower(theme.acc)
                c.nodeNote.text = ""
            }

            TunnelState.Connecting -> {
                status(R.string.status_connecting, theme.dim)
                paintPower(theme.acc)
                c.nodeNote.text = ""
            }

            is TunnelState.On -> {
                status(R.string.status_on, theme.fg)
                paintPower(theme.acc)

                // Имя ноды от ядра — «Финляндия · Хельсинки»; отклик — третьим.
                c.nodeLine.isVisible = true
                c.nodeLine.text = listOf(state.node, if (state.ms > 0) getString(R.string.node_ping, state.ms) else "")
                    .filter { it.isNotEmpty() }
                    .joinToString(" · ")
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
        c.powerAction.isEnabled = state !is TunnelState.Connecting
        c.powerAction.contentDescription = c.statusText.text

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

    /** status — заголовок состояния под кнопкой: слово и его цвет. */
    private fun status(text: Int, color: Int) {
        val v = ui.connectScreen.statusText
        v.setText(text)
        v.setTextColor(color)
    }
    /** Цвет ленты — личный выбор; состояние передаём текстом и кнопкой. */
    private fun paintPower(color: Int) {
        val c = ui.connectScreen
        val dp = resources.displayMetrics.density
        c.halo.theme = theme
        c.trafficPanel.theme = theme
        c.powerAction.theme = theme.copy(acc = color)

    }

    /**
     * renderSubscription — срок и остаток трафика.
     *
     * Пустая карточка означает, что показывать нечего: продавец не поставил ни
     * срока, ни квоты. Врать «безлимит» в этом случае нельзя — он мог просто
     * не заполнить поля.
     */
    private fun renderSubscription(state: TunnelState) {
        val c = ui.connectScreen
        val sub = (state as? TunnelState.On)?.subscription
        more.showUpdate(sub)

        if (sub == null || !sub.known) {
            c.subCard.isVisible = false
            c.headerUntil.isVisible = false
            return
        }

        c.subCard.isVisible = true

        val day = if (sub.until.isEmpty()) "" else Format.day(this, sub.until)
        c.headerUntil.isVisible = day.isNotEmpty()
        if (day.isNotEmpty()) {
            c.headerUntil.text = getString(R.string.header_until, day)
        }

        val quota = sub.limitBytes > 0
        c.trafficLine.isVisible = quota
        c.trafficTrack.isVisible = quota
        if (quota) {
            c.trafficValue.text = getString(
                R.string.traffic_of,
                Format.size(this, sub.leftBytes),
                Format.size(this, sub.limitBytes),
            )
            val left = (sub.leftBytes.toFloat() / sub.limitBytes).coerceIn(0f, 1f)
            weigh(c.trafficFill, left)
            weigh(c.trafficRest, 1f - left)
        }

        c.untilLine.isVisible = day.isNotEmpty()
        if (day.isNotEmpty()) {
            val days = Format.daysLeft(sub.until)
            c.untilLine.text = if (days == null) {
                getString(R.string.sub_until_only, day)
            } else {
                getString(
                    R.string.sub_until_days,
                    day,
                    resources.getQuantityString(R.plurals.days_left, days, days),
                )
            }
        }
    }

    /** Полоса остатка рисуется весами: своего вида полосы под это в Android нет. */
    private fun weigh(view: View, weight: Float) {
        val params = view.layoutParams as android.widget.LinearLayout.LayoutParams
        params.weight = weight
        view.layoutParams = params
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
    private fun refreshRoutes() {
        val subscription = RuRoutes.subscriptionURL(store.accountLink) ?: return
        lifecycleScope.launch {
            withContext(Dispatchers.IO) { RuRoutes.refresh(applicationContext, subscription) }
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

            ui.nav.navBar.updatePadding(bottom = pad + bars.bottom)
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

    private fun askForNotifications() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            return
        }
        val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
        if (granted != PackageManager.PERMISSION_GRANTED) {
            notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
}
