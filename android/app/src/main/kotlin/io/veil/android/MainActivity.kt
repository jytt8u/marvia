package io.veil.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.view.inputmethod.InputMethodManager
import android.widget.Toast
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import io.veil.android.databinding.ActivityMainBinding
import io.veil.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale

/**
 * MainActivity — единственный экран.
 *
 * Покупатель у продавца ничего не настраивает: он вставляет ссылку, которую
 * прислал бот, и жмёт одну кнопку. Всё, что можно решить за него — какая нода
 * быстрее, какой транспорт у неё, каким отпечатком представляться — решает
 * ядро. Выбор, который человек не может сделать осознанно, не должен стоять
 * у него на экране.
 */
class MainActivity : AppCompatActivity() {

    private lateinit var ui: ActivityMainBinding
    private lateinit var store: Store
    private lateinit var neutralColor: ColorStateList

    /** Системное окно «разрешить приложению создавать VPN». */
    private val consent = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            launchService()
        } else {
            // Вид пустой: фразу мы уже написали сами, переводить нечего.
            VeilState.set(TunnelState.Failed("", getString(R.string.consent_denied)))
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
        applyInsets()

        store = Store(this)
        neutralColor = ui.statusText.textColors
        ui.keyInput.setText(store.accountLink)

        ui.saveButton.setOnClickListener { saveKey() }
        ui.settingsButton.setOnClickListener {
            Settings.show(this, lifecycleScope, store) {
                showBypassSummary()
                // Исключения читаются при поднятии туннеля: менять маршруты
                // у работающего VPN нельзя, его надо пересобрать.
                if (VeilState.state.value is TunnelState.On) {
                    Toast.makeText(this, R.string.bypass_restart, Toast.LENGTH_LONG).show()
                }
            }
        }
        ui.connectButton.setOnClickListener { toggle() }

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                VeilState.state.collect { render(it) }
            }
        }

        showBypassSummary()
        refreshRoutes()
        askForNotifications()
        acceptLinkFrom(intent)
    }

    /**
     * askRussianBypass предлагает увести российские сайты мимо туннеля.
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
                showBypassSummary()
                refreshRoutes()
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

    /** showBypassSummary пишет под кнопкой, что уже настроено. */
    private fun showBypassSummary() {
        val chosen = store.bypassed
        val apps = when (chosen.size) {
            0 -> null
            1 -> Bypass.label(this, chosen.first())
            else -> getString(R.string.bypass_some, chosen.size)
        }
        val ru = if (store.bypassRussian) getString(R.string.bypass_ru_on) else null
        val auto = if (store.autoStart) getString(R.string.settings_autostart_short) else null

        ui.settingsSummary.text = listOfNotNull(ru, apps, auto).joinToString(" · ")
            .ifEmpty { getString(R.string.bypass_none) }
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
            ui.keyInput.setText(link)
            saveKey()
            return
        }

        AlertDialog.Builder(this)
            .setTitle(R.string.key_replace_title)
            .setMessage(R.string.key_replace_body)
            .setPositiveButton(R.string.key_replace_yes) { _, _ ->
                ui.keyInput.setText(link)
                saveKey()
            }
            .setNegativeButton(R.string.key_replace_no, null)
            .show()
    }

    /**
     * applyInsets отодвигает содержимое от системных панелей.
     *
     * С Android 15 приложение рисуется под строкой состояния и панелью
     * навигации, хочет оно того или нет. Без этого заголовок уезжает под часы,
     * а кнопка «Подключиться» — под навигацию.
     */
    private fun applyInsets() {
        val pad = resources.getDimensionPixelSize(R.dimen.edge_pad)
        ViewCompat.setOnApplyWindowInsetsListener(ui.root) { view, insets ->
            val bars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.ime(),
            )
            view.setPadding(view.paddingLeft, bars.top + pad, view.paddingRight, bars.bottom + pad)
            insets
        }
    }

    private fun render(state: TunnelState) {
        val hasKey = store.accountLink.isNotBlank()
        ui.techText.isVisible = false
        ui.subText.isVisible = false

        when (state) {
            TunnelState.Off -> {
                ui.statusText.setText(R.string.status_off)
                ui.statusText.setTextColor(neutralColor)
                ui.detailText.text =
                    getString(if (hasKey) R.string.detail_off else R.string.detail_no_key)
                ui.connectButton.setText(R.string.action_connect)
                ui.connectButton.isEnabled = true
            }

            TunnelState.Connecting -> {
                ui.statusText.setText(R.string.status_connecting)
                ui.statusText.setTextColor(neutralColor)
                ui.detailText.setText(R.string.detail_connecting)
                ui.connectButton.setText(R.string.action_connect)
                ui.connectButton.isEnabled = false
            }

            is TunnelState.On -> {
                ui.statusText.setText(R.string.status_on)
                ui.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_live))
                ui.detailText.text = if (state.warning.isBlank()) {
                    getString(R.string.detail_node, state.node)
                } else {
                    getString(R.string.detail_node_warning, state.node, state.warning)
                }
                ui.connectButton.setText(R.string.action_disconnect)
                ui.connectButton.isEnabled = true
                offerRussianBypass()

                val sub = subscriptionText(state.subscription)
                ui.subText.text = sub
                ui.subText.isVisible = sub.isNotEmpty()
            }

            is TunnelState.Failed -> {
                ui.statusText.setText(R.string.status_failed)
                ui.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_fail))
                ui.connectButton.setText(R.string.action_connect)
                ui.connectButton.isEnabled = true

                val human = humanReasonFor(state.kind)
                if (human == null) {
                    // Вида нет — значит фраза уже человеческая, показываем её.
                    ui.detailText.text = state.detail
                } else {
                    ui.detailText.setText(human)
                    ui.techText.text = state.detail
                    ui.techText.isVisible = state.detail.isNotBlank()
                }
            }
        }
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

    /**
     * subscriptionText — строка под состоянием: до какого числа и сколько
     * осталось.
     *
     * Пустая строка означает, что показывать нечего: продавец не поставил ни
     * срока, ни квоты. Врать «безлимит» в этом случае нельзя — он мог просто
     * не заполнить поля.
     */
    private fun subscriptionText(sub: TunnelState.Subscription): String {
        if (!sub.known) {
            return ""
        }

        val left = if (sub.limitBytes > 0) sizeText(sub.leftBytes) else ""
        return when {
            sub.until.isNotEmpty() && left.isNotEmpty() ->
                getString(R.string.sub_until_left, dateText(sub.until), left)
            sub.until.isNotEmpty() -> getString(R.string.sub_until, dateText(sub.until))
            left.isNotEmpty() -> getString(R.string.sub_left, left)
            else -> ""
        }
    }

    /** dateText превращает 2026-09-27 в 27.09.2026 — так читают дату здесь. */
    private fun dateText(iso: String): String {
        val parts = iso.split("-")
        if (parts.size != 3) {
            return iso
        }
        return parts[2] + "." + parts[1] + "." + parts[0]
    }

    private fun sizeText(bytes: Long): String {
        val gb = 1024.0 * 1024 * 1024
        if (bytes >= gb) {
            return getString(R.string.size_gb, String.format(Locale.getDefault(), "%.1f", bytes / gb))
        }
        return getString(R.string.size_mb, bytes / (1024 * 1024))
    }

    private fun toggle() {
        if (VeilState.state.value is TunnelState.On) {
            startService(
                Intent(this, VeilVpnService::class.java).setAction(VeilVpnService.ACTION_STOP),
            )
            return
        }

        if (store.accountLink.isBlank()) {
            ui.keyLayout.error = getString(R.string.detail_no_key)
            ui.keyInput.requestFocus()
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
        ContextCompat.startForegroundService(this, Intent(this, VeilVpnService::class.java))
    }

    private fun saveKey() {
        val link = ui.keyInput.text?.toString()?.trim().orEmpty()
        if (link.isEmpty()) {
            ui.keyLayout.error = getString(R.string.key_empty)
            return
        }

        // Разбираем ссылку ядром, а не своим кодом на Kotlin. Правило, что
        // считать правильной ссылкой, живёт в одном месте — иначе приложение
        // и сервер однажды разойдутся во мнениях, и разбираться в этом будет
        // человек, который просто хотел включить интернет.
        try {
            Mobile.checkAccountLink(link)
        } catch (t: Throwable) {
            ui.keyLayout.error = VeilVpnService.reasonOf(t)
            return
        }

        ui.keyLayout.error = null
        store.accountLink = link
        hideKeyboard()
        Toast.makeText(this, R.string.key_saved, Toast.LENGTH_SHORT).show()
        render(VeilState.state.value)
    }

    private fun hideKeyboard() {
        val manager = getSystemService(InputMethodManager::class.java)
        manager?.hideSoftInputFromWindow(ui.keyInput.windowToken, 0)
        ui.keyInput.clearFocus()
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
