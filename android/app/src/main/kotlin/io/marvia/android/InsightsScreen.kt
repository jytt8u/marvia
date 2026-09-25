package io.marvia.android

import android.view.View
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import io.marvia.android.databinding.ScreenInsightsBinding
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.text.DateFormat
import java.util.Date
import java.util.Locale

/** In-app speed and session pages; history is read in small pages off the UI thread. */
class InsightsScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenInsightsBinding,
    private val theme: () -> Theme,
    private val onBack: () -> Unit,
) {
    enum class Mode { SPEED, SESSIONS }
    private var mode = Mode.SPEED
    private var recentOnly = true
    private var loaded = 0
    private var total = 0
    private var loading: Job? = null
    private var chartHistory: List<SessionDetails.Session> = emptyList()
    private val dp = host.resources.displayMetrics.density
    private val date = DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT)

    init {
        ui.insightsBack.setOnClickListener { onBack() }
        ui.sessionsRecent.setOnClickListener { selectRecent(true) }
        ui.sessionsAll.setOnClickListener { selectRecent(false) }
        ui.sessionsMore.setOnClickListener { loadNext() }
    }

    fun open(next: Mode) {
        mode = next
        ui.insightsTitle.setText(if (next == Mode.SPEED) R.string.stats_speed else R.string.stats_session)
        ui.insightsSubtitle.setText(R.string.insights_subtitle)
        ui.speedSection.isVisible = next == Mode.SPEED
        ui.sessionsSection.isVisible = next == Mode.SESSIONS
        ui.insightsScroll.scrollTo(0, 0)
        refresh()
        if (next == Mode.SESSIONS) {
            recentOnly = true
            total = 0
            chartHistory = emptyList()
            ui.sessionChartCard.isVisible = SessionDetails.snapshot().session != null
            loadNext(reset = true)
        }
    }

    fun refresh() {
        if (!ui.root.isVisible) return
        val t = theme()
        val snapshot = SessionDetails.snapshot()
        val session = snapshot.session
        if (mode == Mode.SPEED) {
            val last = snapshot.samples.lastOrNull()
            val down = last?.down ?: 0.0
            val up = last?.up ?: 0.0
            ui.speedTotal.text = rate(down + up)
            ui.speedDown.text = rate(down)
            ui.speedUp.text = rate(up)
            ui.speedAverage.text = host.getString(R.string.insights_average, rate(
                if (session == null) 0.0 else (session.received + session.sent).toDouble() / session.seconds.coerceAtLeast(1),
            ))
            ui.speedPeak.text = host.getString(R.string.insights_peak, rate(snapshot.peak))
            ui.speedEmpty.isVisible = session == null
            ui.speedChart.show(snapshot.samples, t)
        } else {
            ui.sessionDuration.text = if (session == null) "—" else duration(session.seconds)
            ui.sessionCurrentTraffic.text = if (session == null) host.getString(R.string.insights_not_connected)
                else host.getString(R.string.insights_current_traffic, Format.size(host, session.received), Format.size(host, session.sent))
            ui.sessionSplit.isVisible = session != null
            ui.sessionSplit.show(session?.received ?: 0L, session?.sent ?: 0L, t)
            val rows = chartHistory.take(if (session == null) 7 else 6).asReversed() + listOfNotNull(session)
            ui.sessionChartCard.isVisible = rows.isNotEmpty()
            ui.sessionChart.show(rows, session != null, t)
        }
    }

    fun paint() {
        paintTabs()
        if (ui.root.isVisible) refresh()
    }

    private fun selectRecent(recent: Boolean) {
        if (recentOnly == recent) return
        recentOnly = recent
        loadNext(reset = true)
    }

    private fun loadNext(reset: Boolean = false) {
        if (mode != Mode.SESSIONS) return
        loading?.cancel()
        if (reset) {
            loaded = 0
            ui.sessionsList.removeAllViews()
            ui.sessionsMore.isVisible = false
        }
        val offset = loaded
        val limit = if (recentOnly) 7 else 40
        loading = host.lifecycleScope.launch {
            val result = withContext(Dispatchers.IO) {
                val count = SessionDetails.count(host)
                val rows = SessionDetails.history(host, limit, offset)
                val chart = if (offset == 0) rows.take(7) else emptyList()
                Triple(count, rows, chart)
            }
            total = result.first
            loaded += result.second.size
            if (offset == 0) chartHistory = result.third
            ui.sessionsCount.text = host.getString(R.string.insights_history_count, total)
            result.second.forEach { addRow(it) }
            if (total == 0 && SessionDetails.snapshot().session == null) {
                val empty = TextView(host).apply {
                    setText(R.string.stats_sessions_empty)
                    setPadding((16 * dp).toInt(), (20 * dp).toInt(), (16 * dp).toInt(), (20 * dp).toInt())
                    tag = "dim"
                }
                ui.sessionsList.addView(empty)
                Paint.apply(empty, theme())
            }
            ui.sessionsMore.isVisible = !recentOnly && loaded < total
            paintTabs()
            refresh()
        }
    }

    private fun addRow(s: SessionDetails.Session) {
        val margin = (8 * dp).toInt()
        val padding = (16 * dp).toInt()
        val row = LinearLayout(host).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(padding, padding, padding, padding)
            tag = "card"
            layoutParams = LinearLayout.LayoutParams(-1, -2).apply { topMargin = margin }
            background = Paint.rounded(theme().surf, theme().r, dp)
        }
        row.addView(TextView(host).apply {
            text = host.getString(R.string.insights_session_item, date.format(Date(s.began)), duration(s.seconds))
            textSize = 14f
            setTypeface(typeface, android.graphics.Typeface.BOLD)
            tag = "fg"
        })
        row.addView(TextView(host).apply {
            text = host.getString(R.string.insights_session_bytes, Format.size(host, s.received), Format.size(host, s.sent))
            textSize = 12f
            tag = "dim"
            layoutParams = LinearLayout.LayoutParams(-1, -2).apply { topMargin = (7 * dp).toInt() }
        })
        ui.sessionsList.addView(row)
        Paint.apply(row, theme())
    }

    private fun paintTabs() {
        val t = theme()
        fun tab(v: TextView, selected: Boolean) {
            v.background = Paint.rounded(if (selected) t.acc else t.surf, 100, dp)
            v.setTextColor(if (selected) t.accFg else t.dim)
        }
        tab(ui.sessionsRecent, recentOnly)
        tab(ui.sessionsAll, !recentOnly)
    }

    private fun rate(bytes: Double) = MarviaVpnService.Live.rate(host, bytes)

    private fun duration(seconds: Long): String = String.format(
        Locale.ROOT, "%02d:%02d:%02d", seconds / 3600, seconds / 60 % 60, seconds % 60,
    )
}
