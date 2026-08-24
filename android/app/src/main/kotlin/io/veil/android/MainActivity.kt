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
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import io.veil.android.databinding.ActivityMainBinding
import io.veil.mobile.Mobile
import kotlinx.coroutines.launch

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
            VeilState.set(TunnelState.Failed(getString(R.string.consent_denied)))
        }
    }

    /** Разрешение на уведомления. Отказ ничего не ломает: туннель работает. */
    private val notifications = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)

        ui = ActivityMainBinding.inflate(layoutInflater)
        setContentView(ui.root)
        applyInsets()

        store = Store(this)
        neutralColor = ui.statusText.textColors
        ui.keyInput.setText(store.accountLink)

        ui.saveButton.setOnClickListener { saveKey() }
        ui.connectButton.setOnClickListener { toggle() }

        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                VeilState.state.collect { render(it) }
            }
        }

        askForNotifications()
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
            }

            is TunnelState.Failed -> {
                ui.statusText.setText(R.string.status_failed)
                ui.statusText.setTextColor(ContextCompat.getColor(this, R.color.veil_fail))
                ui.detailText.text = state.reason
                ui.connectButton.setText(R.string.action_connect)
                ui.connectButton.isEnabled = true
            }
        }
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
