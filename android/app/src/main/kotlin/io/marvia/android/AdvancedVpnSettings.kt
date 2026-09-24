package io.marvia.android

import androidx.appcompat.app.AppCompatActivity

/** Дополнительные настройки с прямым действием на интерфейс и опрос туннеля. */
object AdvancedVpnSettings {
    fun show(host: AppCompatActivity, store: Store, theme: () -> Theme, onNextConnect: () -> Unit) {
        val items = arrayOf(
            host.getString(R.string.advanced_mtu_value, store.vpnMtu),
            host.getString(R.string.advanced_live_value, store.liveRefreshSeconds),
            host.getString(R.string.advanced_idle_value, store.idleRefreshSeconds),
            host.getString(R.string.advanced_ping_value,
                if (store.pingIntervalSeconds == 0) host.getString(R.string.advanced_off)
                else host.getString(R.string.advanced_seconds, store.pingIntervalSeconds)),
        )
        ThemedDialogs.builder(host, theme())
            .setTitle(R.string.advanced_title)
            .setItems(items) { _, which ->
                when (which) {
                    0 -> choose(host, theme, R.string.advanced_mtu, Store.MTU_CHOICES, store.vpnMtu,
                        { it.toString() }) { store.vpnMtu = it; onNextConnect() }
                    1 -> choose(host, theme, R.string.advanced_live, Store.LIVE_REFRESH_CHOICES, store.liveRefreshSeconds,
                        { host.getString(R.string.advanced_seconds, it) }) { store.liveRefreshSeconds = it }
                    2 -> choose(host, theme, R.string.advanced_idle, Store.IDLE_REFRESH_CHOICES, store.idleRefreshSeconds,
                        { host.getString(R.string.advanced_seconds, it) }) { store.idleRefreshSeconds = it }
                    3 -> choose(host, theme, R.string.advanced_ping, Store.PING_INTERVAL_CHOICES, store.pingIntervalSeconds,
                        { if (it == 0) host.getString(R.string.advanced_off) else host.getString(R.string.advanced_seconds, it) }) {
                        store.pingIntervalSeconds = it
                    }
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun choose(
        host: AppCompatActivity,
        theme: () -> Theme,
        title: Int,
        choices: List<Int>,
        current: Int,
        label: (Int) -> String,
        save: (Int) -> Unit,
    ) {
        val labels = choices.map(label).toTypedArray()
        ThemedDialogs.builder(host, theme())
            .setTitle(title)
            .setSingleChoiceItems(labels, choices.indexOf(current)) { dialog, which ->
                save(choices[which])
                dialog.dismiss()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }
}
