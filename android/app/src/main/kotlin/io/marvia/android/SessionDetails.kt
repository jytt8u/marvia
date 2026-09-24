package io.marvia.android

import android.content.Context
import androidx.appcompat.app.AppCompatActivity
import java.text.DateFormat
import java.util.Date
import java.util.Locale

/** Только локальные счётчики; адреса сайтов и ключи здесь не хранятся. */
object SessionDetails {
    data class Sample(val at: Long, val down: Double, val up: Double)
    data class Session(val began: Long, val seconds: Long, val received: Long, val sent: Long)
    private val samples = ArrayDeque<Sample>()
    private var current: Session? = null
    private var lastAt = 0L
    private var startedAt = 0L
    private var peak = 0.0

    @Synchronized fun begin(now: Long = android.os.SystemClock.elapsedRealtime()) {
        samples.clear()
        current = Session(System.currentTimeMillis(), 0, 0, 0)
        lastAt = now
        startedAt = now
        peak = 0.0
    }

    @Synchronized fun sample(received: Long, sent: Long, now: Long = android.os.SystemClock.elapsedRealtime()) {
        val old = current ?: return
        if (now <= lastAt) return
        val dt = (now - lastAt) / 1000.0
        val s = Sample(now, (received - old.received).coerceAtLeast(0) / dt, (sent - old.sent).coerceAtLeast(0) / dt)
        samples.addLast(s)
        while (samples.size > 120) samples.removeFirst()
        peak = maxOf(peak, s.down + s.up)
        current = old.copy(seconds = (now - startedAt) / 1000, received = received, sent = sent)
        lastAt = now
    }

    @Synchronized fun finish(context: Context) {
        val s = current ?: return
        val prefs = context.getSharedPreferences("veil", Context.MODE_PRIVATE)
        val previous = prefs.getString("session_history", "").orEmpty().lineSequence().filter { it.isNotBlank() }.take(19)
        prefs.edit().putString("session_history", (sequenceOf("${s.began},${s.seconds},${s.received},${s.sent}") + previous).joinToString("\n")).apply()
        current = null
        samples.clear()
    }

    fun showSpeed(host: AppCompatActivity, theme: Theme) {
        val (session, points, maximum) = synchronized(this) { Triple(current, samples.toList(), peak) }
        val last = points.lastOrNull()
        val average = session?.let { (it.received + it.sent).toDouble() / it.seconds.coerceAtLeast(1) } ?: 0.0
        val message = if (session == null) host.getString(R.string.stats_speed_connect) else listOf(
            host.getString(R.string.stats_speed_down, MarviaVpnService.Live.rate(host, last?.down ?: 0.0)),
            host.getString(R.string.stats_speed_up, MarviaVpnService.Live.rate(host, last?.up ?: 0.0)),
            host.getString(R.string.stats_speed_average, MarviaVpnService.Live.rate(host, average)),
            host.getString(R.string.stats_speed_peak, MarviaVpnService.Live.rate(host, maximum)),
            host.getString(R.string.stats_speed_sample_note),
        ).joinToString("\n\n")
        ThemedDialogs.builder(host, theme).setTitle(R.string.stats_speed).setMessage(message).setPositiveButton(android.R.string.ok, null).show()
    }

    fun showSession(host: AppCompatActivity, theme: Theme) {
        val active = synchronized(this) { current }
        val date = DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT)
        fun line(s: Session): String = "${date.format(Date(s.began))} · ${duration(s.seconds)}\n↓ ${Format.size(host, s.received)}   ↑ ${Format.size(host, s.sent)}"
        val saved = host.getSharedPreferences("veil", Context.MODE_PRIVATE).getString("session_history", "").orEmpty()
            .lineSequence().mapNotNull { row ->
                val v = row.split(',').mapNotNull { it.toLongOrNull()?.takeIf { n -> n >= 0 } }
                if (v.size == 4) Session(v[0], v[1], v[2], v[3]) else null
            }.take(20).toList()
        val text = buildList {
            if (active != null) add(host.getString(R.string.stats_session_current) + "\n" + line(active))
            if (saved.isNotEmpty()) add(host.getString(R.string.stats_sessions_previous) + "\n\n" + saved.joinToString("\n\n", transform = ::line))
        }.joinToString("\n\n").ifEmpty { host.getString(R.string.stats_sessions_empty) }
        ThemedDialogs.builder(host, theme).setTitle(R.string.stats_session).setMessage(text).setPositiveButton(android.R.string.ok, null).show()
    }

    private fun duration(s: Long): String = String.format(Locale.ROOT, "%02d:%02d:%02d", s / 3600, s / 60 % 60, s % 60)
}
