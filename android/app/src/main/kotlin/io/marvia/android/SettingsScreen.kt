package io.marvia.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.provider.Settings as AndroidSettings
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.appcompat.app.AppCompatDelegate
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import io.marvia.android.databinding.ScreenSettingsBinding

/**
 * SettingsScreen — то немногое, что человеку и правда стоит решать самому.
 *
 * Здесь нет ручек протокола: ни размера фрагментов, ни числа соединений, ни
 * выбора мультиплексора. Такие настройки бывают у оболочек над чужим движком,
 * которому надо объяснить, к какому серверу он подключается. У нас клиент и
 * нода — одна система, и все эти решения приняты внутри; вынести их на экран
 * значило бы дать человеку сломать себе связь, не понимая чем.
 *
 * Остаётся то, что зависит от него, а не от сети: когда включаться, как
 * выглядеть, что показать продавцу, когда что-то пошло не так.
 */
class SettingsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenSettingsBinding,
    private val store: Store,
    /** Открыть экран ключа: он общий с первым запуском. */
    private val onKey: () -> Unit,
    /** Что-то из этого применяется только на следующем подключении. */
    private val onRoutesChanged: () -> Unit,
) {

    init {
        ui.rowAutostart.setOnClickListener {
            store.autoStart = !store.autoStart
            open()
            Toast.makeText(
                host,
                if (store.autoStart) {
                    R.string.settings_autostart_saved
                } else {
                    R.string.settings_autostart_off_saved
                },
                Toast.LENGTH_SHORT,
            ).show()
        }

        ui.rowRussian.setOnClickListener {
            store.bypassRussian = !store.bypassRussian
            open()
            onRoutesChanged()
        }

        ui.rowApps.setOnClickListener {
            Bypass.show(host, host.lifecycleScope, store) {
                open()
                onRoutesChanged()
            }
        }

        ui.rowAlwaysOn.setOnClickListener { openAlwaysOn() }
        ui.rowTheme.setOnClickListener { theme() }
        ui.rowKey.setOnClickListener { onKey() }
        ui.rowJournal.setOnClickListener { journal() }
        ui.rowAbout.setOnClickListener { about() }

        // Российские подсети умеет исключать только Android 13 и новее. На
        // старых строки нет вовсе: переключатель обещал бы то, чего система
        // не сделает, а человек считал бы, что госуслуги уже починены.
        val local = RuRoutes.supported()
        ui.rowRussian.isVisible = local
        ui.dividerRu.isVisible = local
    }

    /** open зовётся при каждом показе: настройки меняются и из других мест. */
    fun open() {
        ui.switchAutostart.isChecked = store.autoStart
        ui.switchRussian.isChecked = store.bypassRussian
        ui.appsSummary.text = appsSummary()
        ui.themeValue.text = host.getString(themeLabel())

        val saved = store.accountSavedAt
        ui.keySummary.isVisible = saved > 0
        if (saved > 0) {
            ui.keySummary.text = host.getString(
                R.string.settings_key_added,
                Format.day(host, saved),
            )
        }
    }

    private fun appsSummary(): String {
        val chosen = store.bypassed
        return when (chosen.size) {
            0 -> host.getString(R.string.bypass_none)
            1 -> Bypass.label(host, chosen.first())
            else -> host.getString(R.string.bypass_some, chosen.size)
        }
    }

    private fun themeLabel(): Int = when (store.theme) {
        AppCompatDelegate.MODE_NIGHT_NO -> R.string.theme_light
        AppCompatDelegate.MODE_NIGHT_YES -> R.string.theme_dark
        else -> R.string.theme_system
    }

    /**
     * openAlwaysOn отправляет в системный «постоянный VPN».
     *
     * Он надёжнее нашего автозапуска: система поднимает туннель раньше нас,
     * переживает наше падение и умеет не пускать трафик мимо, пока туннель не
     * встал. Прятать это от человека ради того, чтобы наша галочка выглядела
     * главной, — нечестно.
     */
    private fun openAlwaysOn() {
        try {
            host.startActivity(Intent(AndroidSettings.ACTION_VPN_SETTINGS))
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(host, R.string.settings_always_on_missing, Toast.LENGTH_LONG).show()
        }
    }

    private fun theme() {
        val modes = intArrayOf(
            AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM,
            AppCompatDelegate.MODE_NIGHT_NO,
            AppCompatDelegate.MODE_NIGHT_YES,
        )
        val labels = arrayOf(
            host.getString(R.string.theme_system),
            host.getString(R.string.theme_light),
            host.getString(R.string.theme_dark),
        )

        AlertDialog.Builder(host)
            .setTitle(R.string.settings_theme)
            .setSingleChoiceItems(labels, modes.indexOf(store.theme).coerceAtLeast(0)) { dialog, which ->
                store.theme = modes[which]
                AppCompatDelegate.setDefaultNightMode(modes[which])
                dialog.dismiss()
                open()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /**
     * journal показывает, что делало приложение, и отдаёт это продавцу.
     *
     * Одна кнопка «отправить» заменяет переписку «а что у тебя написано на
     * экране» — самую бесполезную часть любой поддержки.
     */
    private fun journal() {
        val text = Journal.report(host)

        AlertDialog.Builder(host)
            .setTitle(R.string.settings_journal)
            .setMessage(text)
            .setPositiveButton(R.string.journal_send) { _, _ ->
                val send = Intent(Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(Intent.EXTRA_TEXT, text)
                }
                host.startActivity(
                    Intent.createChooser(send, host.getString(R.string.journal_send)),
                )
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /** about отвечает на «какая у тебя версия» — первый вопрос продавца. */
    private fun about() {
        val version = try {
            host.packageManager.getPackageInfo(host.packageName, 0).versionName.orEmpty()
        } catch (_: PackageManager.NameNotFoundException) {
            ""
        }

        AlertDialog.Builder(host)
            .setTitle(R.string.settings_about)
            .setMessage(host.getString(R.string.about_body, host.getString(R.string.app_name), version))
            .setPositiveButton(android.R.string.ok, null)
            .show()
    }
}
