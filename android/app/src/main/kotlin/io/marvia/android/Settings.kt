package io.marvia.android

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings as AndroidSettings
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatDelegate
import kotlinx.coroutines.CoroutineScope

/**
 * Settings — то немногое, что человеку и правда стоит решать самому.
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
object Settings {

    fun show(context: Context, scope: CoroutineScope, store: Store, onChanged: () -> Unit) {
        val items = arrayOf(
            context.getString(R.string.settings_apps),
            context.getString(
                if (store.autoStart) R.string.settings_autostart_on else R.string.settings_autostart_off,
            ),
            context.getString(R.string.settings_always_on),
            context.getString(R.string.settings_theme),
            context.getString(R.string.settings_journal),
        )

        AlertDialog.Builder(context)
            .setTitle(R.string.settings_title)
            .setItems(items) { _, which ->
                when (which) {
                    0 -> Bypass.show(context, scope, store) { onChanged() }
                    1 -> {
                        store.autoStart = !store.autoStart
                        onChanged()
                        Toast.makeText(
                            context,
                            if (store.autoStart) R.string.settings_autostart_saved else R.string.settings_autostart_off_saved,
                            Toast.LENGTH_SHORT,
                        ).show()
                    }
                    2 -> openAlwaysOn(context)
                    3 -> theme(context, store, onChanged)
                    4 -> journal(context)
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /**
     * openAlwaysOn отправляет в системный «постоянный VPN».
     *
     * Он надёжнее нашего автозапуска: система поднимает туннель раньше нас,
     * переживает наше падение и умеет не пускать трафик мимо, пока туннель не
     * встал. Прятать это от человека ради того, чтобы наша галочка выглядела
     * главной, — нечестно.
     */
    private fun openAlwaysOn(context: Context) {
        try {
            context.startActivity(
                Intent(AndroidSettings.ACTION_VPN_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(context, R.string.settings_always_on_missing, Toast.LENGTH_LONG).show()
        }
    }

    private fun theme(context: Context, store: Store, onChanged: () -> Unit) {
        val modes = intArrayOf(
            AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM,
            AppCompatDelegate.MODE_NIGHT_NO,
            AppCompatDelegate.MODE_NIGHT_YES,
        )
        val labels = arrayOf(
            context.getString(R.string.theme_system),
            context.getString(R.string.theme_light),
            context.getString(R.string.theme_dark),
        )

        AlertDialog.Builder(context)
            .setTitle(R.string.settings_theme)
            .setSingleChoiceItems(labels, modes.indexOf(store.theme).coerceAtLeast(0)) { dialog, which ->
                store.theme = modes[which]
                AppCompatDelegate.setDefaultNightMode(modes[which])
                dialog.dismiss()
                onChanged()
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
    private fun journal(context: Context) {
        val text = Journal.report(context)

        AlertDialog.Builder(context)
            .setTitle(R.string.settings_journal)
            .setMessage(text)
            .setPositiveButton(R.string.journal_send) { _, _ ->
                val send = Intent(Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(Intent.EXTRA_TEXT, text)
                }
                context.startActivity(
                    Intent.createChooser(send, context.getString(R.string.journal_send))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /** Нужен, чтобы Uri не выпал из импортов при правках. */
    @Suppress("unused")
    private fun keepUri(): Uri? = null
}
