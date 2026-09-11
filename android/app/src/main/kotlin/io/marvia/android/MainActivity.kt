package io.marvia.android

import android.Manifest
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
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
 * Их четыре, и они лежат друг на друге в одной активности: подключение, выбор
 * страны, настройки и ключ доступа. Отдельных активностей нет намеренно —
 * состояние туннеля живёт в процессе, и переключение вкладок не должно
 * пересобирать экран и терять то, что человек уже видел.
 *
 * Ключ — не вкладка: пока его нет, показывать нечего, и нижняя панель вместе
 * с остальными экранами просто не появляется.
 */
class MainActivity : AppCompatActivity() {

    private enum class Screen { KEY, CONNECT, SERVERS, SETTINGS }

    private lateinit var ui: ActivityMainBinding
    private lateinit var store: Store
    private lateinit var servers: ServersScreen
    private lateinit var settings: SettingsScreen

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

    override fun onCreate(savedInstanceState: Bundle?) {
        AppCompatDelegate.setDefaultNightMode(Store(this).theme)
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)

        ui = ActivityMainBinding.inflate(layoutInflater)
        setContentView(ui.root)
        store = Store(this)

        applyInsets()
        wireNav()
        wireConnect()
        wireKey()

        servers = ServersScreen(this, ui.serversScreen)
        settings = SettingsScreen(
            host = this,
            ui = ui.settingsScreen,
            store = store,
            onKey = { show(Screen.KEY) },
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
                MarviaState.state.collect { render(it) }
            }
        }

        onBackPressedDispatcher.addCallback(this, back)

        show(if (store.accountLink.isBlank()) Screen.KEY else Screen.CONNECT)
        refreshRoutes()
        askForNotifications()
        acceptLinkFrom(intent)
    }

    /** Возврат с любого экрана ведёт на подключение, а с него — из приложения. */
    private val back = object : OnBackPressedCallback(true) {
        override fun handleOnBackPressed() {
            if (screen == Screen.CONNECT || store.accountLink.isBlank()) {
                isEnabled = false
                onBackPressedDispatcher.onBackPressed()
                isEnabled = true
                return
            }
            show(Screen.CONNECT)
        }
    }

    // -------------------------------------------------------------- экраны

    private fun show(next: Screen) {
        screen = next

        ui.keyScreen.root.isVisible = next == Screen.KEY
        ui.connectScreen.root.isVisible = next == Screen.CONNECT
        ui.serversScreen.root.isVisible = next == Screen.SERVERS
        ui.settingsScreen.root.isVisible = next == Screen.SETTINGS

        // Пока ключа нет, ходить некуда: панель появится вместе с ним.
        ui.nav.root.isVisible = store.accountLink.isNotBlank()
        paintNav()

        when (next) {
            Screen.SERVERS -> servers.open()
            Screen.SETTINGS -> settings.open()
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
        ui.nav.navSettings.setOnClickListener { show(Screen.SETTINGS) }
    }

    private fun paintNav() {
        paintTab(ui.nav.navConnectIcon, ui.nav.navConnectLabel, screen == Screen.CONNECT)
        paintTab(ui.nav.navServersIcon, ui.nav.navServersLabel, screen == Screen.SERVERS)
        paintTab(ui.nav.navSettingsIcon, ui.nav.navSettingsLabel, screen == Screen.SETTINGS)
    }

    private fun paintTab(icon: ImageView, label: TextView, active: Boolean) {
        val color = ContextCompat.getColor(this, if (active) R.color.veil_text else R.color.veil_muted)
        ImageViewCompat.setImageTintList(icon, ColorStateList.valueOf(color))
        label.setTextColor(color)
        label.typeface = if (active) android.graphics.Typeface.DEFAULT_BOLD else android.graphics.Typeface.DEFAULT
    }

    // --------------------------------------------------------- подключение

    private fun wireConnect() {
        val c = ui.connectScreen
        c.powerOuter.setOnClickListener { toggle() }
        c.nodeLine.setOnClickListener { show(Screen.SERVERS) }
        // Полосу остатка скругляем по фону: иначе заливка вылезает углами.
        c.trafficTrack.clipToOutline = true
    }

    private fun render(state: TunnelState) {
        val c = ui.connectScreen
        val hasKey = store.accountLink.isNotBlank()

        c.techText.isVisible = false
        c.nodeLine.isVisible = false
        c.nodePing.isVisible = false

        when (state) {
            TunnelState.Off -> {
                c.statusText.setText(R.string.status_off)
                paintPower(R.color.veil_muted)
                c.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_text))
                c.nodeNote.text = if (hasKey) "" else getString(R.string.connect_no_key)
                c.powerHint.text = if (hasKey) getString(R.string.connect_tap_on) else ""
            }

            TunnelState.Connecting -> {
                c.statusText.setText(R.string.status_connecting)
                paintPower(R.color.veil_accent)
                c.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_text))
                c.nodeNote.setText(R.string.detail_connecting)
                c.powerHint.text = ""
            }

            is TunnelState.On -> {
                c.statusText.setText(R.string.status_on)
                paintPower(R.color.veil_live)
                c.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_live))
                c.powerHint.setText(R.string.connect_tap_off)

                c.nodeLine.isVisible = true
                c.nodeCountry.text = state.node
                showPing(c.nodePing, state.ms)
                c.nodeNote.text = choiceText(state)

                // Ошибка отдельного соединения туннель не роняет, но молчать о
                // ней нельзя: иначе человек видит «подключено» при наполовину
                // живой ноде.
                if (state.warning.isNotBlank()) {
                    c.techText.text = state.warning
                    c.techText.setTextColor(ContextCompat.getColor(this, R.color.veil_warn))
                    c.techText.isVisible = true
                }

                offerRussianBypass()
            }

            is TunnelState.Failed -> {
                c.statusText.setText(R.string.status_failed)
                paintPower(R.color.veil_fail)
                c.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_fail))
                c.powerHint.text = if (hasKey) getString(R.string.connect_tap_on) else ""

                val human = humanReasonFor(state.kind)
                if (human == null) {
                    // Вида нет — значит фраза уже человеческая, показываем её.
                    c.nodeNote.text = state.detail
                } else {
                    c.nodeNote.setText(human)
                    c.techText.text = state.detail
                    c.techText.setTextColor(ContextCompat.getColor(this, R.color.veil_muted))
                    c.techText.isVisible = state.detail.isNotBlank()
                }
            }
        }

        // Пустая строка — это не строка: место под неё занимать незачем.
        c.nodeNote.isVisible = c.nodeNote.text.isNotEmpty()
        c.powerHint.isVisible = c.powerHint.text.isNotEmpty()

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
        state.chosen.isEmpty() -> getString(R.string.connect_auto)
        state.chosen == state.node -> getString(R.string.connect_manual)
        else -> getString(R.string.connect_manual_moved, state.chosen)
    }

    /** paintPower красит три круга: внешние — тем же цветом, но почти прозрачным. */
    private fun paintPower(colorRes: Int) {
        val c = ui.connectScreen
        val color = ContextCompat.getColor(this, colorRes)
        c.powerOuter.backgroundTintList =
            ColorStateList.valueOf(ColorUtils.setAlphaComponent(color, 18))
        c.powerMiddle.backgroundTintList =
            ColorStateList.valueOf(ColorUtils.setAlphaComponent(color, 26))
        c.powerInner.backgroundTintList = ColorStateList.valueOf(color)
    }

    private fun showPing(view: TextView, ms: Long) {
        if (ms <= 0) {
            view.isVisible = false
            return
        }

        val color = when {
            ms < 70 -> R.color.veil_live
            ms < 250 -> R.color.veil_warn
            else -> R.color.veil_fail
        }
        val value = ContextCompat.getColor(this, color)
        view.text = getString(R.string.node_ping, ms)
        view.setTextColor(value)
        view.backgroundTintList =
            ColorStateList.valueOf(ColorUtils.setAlphaComponent(value, 31))
        view.isVisible = true
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
        show(Screen.CONNECT)
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

        AlertDialog.Builder(this)
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
            show(Screen.CONNECT)
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

        AlertDialog.Builder(this)
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
