package io.marvia.android

import android.content.ClipboardManager
import android.view.LayoutInflater
import android.view.View
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import android.content.res.ColorStateList
import androidx.core.widget.ImageViewCompat
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import io.marvia.android.databinding.ItemCountryBinding
import io.marvia.android.databinding.ItemNodeBinding
import io.marvia.android.databinding.ScreenServersBinding
import io.marvia.mobile.Mobile
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * ServersScreen — выбор страны.
 *
 * Главное, что этот экран обязан не соврать: выбранная нода и та, через
 * которую идёт трафик, — разные вещи. Человек выбрал ОАЭ, нода замолчала,
 * ядро увезло его в Финляндию и продолжает работать. Написать в этот момент
 * одно «выбрана ОАЭ» значит показать картинку, которой нет, — а проверить её
 * человеку нечем.
 *
 * Список берётся у ядра, а ядро живёт внутри поднятого туннеля. Пока туннеля
 * нет, спрашивать не у кого, и экран говорит об этом прямо, вместо того чтобы
 * показывать пустоту или прошлогодние числа.
 */
class ServersScreen(
    private val host: AppCompatActivity,
    private val ui: ScreenServersBinding,
    /** Тема на сейчас: цвета выбора, пинга и карточек берутся из неё. */
    private val theme: () -> Theme,
    /** Хранилище подписок: их список и та, что сейчас в работе. */
    private val store: Store,
    /** Подписка сменилась: ключ другой, туннель надо поднимать заново. */
    private val onSubscriptionChanged: () -> Unit,
) {

    /** Идёт замер или переключение: второе нажатие в это время только мешает. */
    private var busy = false

    /** Последний показанный список: перекрашивается при смене темы без похода в ядро. */
    private var shown: List<NodeRow> = emptyList()

    private val dp = host.resources.displayMetrics.density

    init {
        ui.measureButton.setOnClickListener { load(measure = true) }
        ui.autoRow.setOnClickListener { select(AUTO) }
        ui.addSubscription.setOnClickListener { askWhereFrom() }
    }

    // --------------------------------------------------------- подписки

    /**
     * askWhereFrom — откуда взять ссылку: из буфера или набрать руками.
     *
     * Два пути, потому что ссылку присылают в чате: чаще её копируют, и
     * тогда одно нажатие лучше поля ввода. Руками — когда буфер занят
     * другим или человек хочет назвать подписку по-своему.
     */
    private fun askWhereFrom() {
        val items = arrayOf(
            host.getString(R.string.servers_add_clipboard),
            host.getString(R.string.servers_add_manual),
        )
        AlertDialog.Builder(host)
            .setTitle(R.string.servers_add)
            .setItems(items) { _, which -> if (which == 0) fromClipboard() else byHand() }
            .show()
    }

    private fun fromClipboard() {
        val clip = host.getSystemService(ClipboardManager::class.java)
            ?.primaryClip?.takeIf { it.itemCount > 0 }
            ?.getItemAt(0)?.text?.toString().orEmpty()
        if (clip.isBlank()) {
            Toast.makeText(host, R.string.key_clipboard_empty, Toast.LENGTH_SHORT).show()
            return
        }
        add("", clip)
    }

    /** byHand — имя и ссылка. Имя необязательно: без него возьмём домен. */
    private fun byHand() {
        val pad = (18 * dp).toInt()
        val box = LinearLayout(host).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, (8 * dp).toInt(), pad, 0)
        }
        val name = EditText(host).apply {
            setHint(R.string.servers_sub_name)
            setSingleLine()
        }
        val link = EditText(host).apply {
            setHint(R.string.servers_sub_link)
            setSingleLine()
        }
        box.addView(name)
        box.addView(link)

        AlertDialog.Builder(host)
            .setTitle(R.string.servers_add_manual)
            .setView(box)
            .setPositiveButton(R.string.servers_add) { _, _ ->
                add(name.text.toString(), link.text.toString())
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /**
     * add принимает ссылку, проверив её ядром.
     *
     * Проверяем до сохранения: положить мусор и узнать об этом при следующем
     * подключении значит объяснять человеку поломку через час после того,
     * как он её устроил.
     */
    private fun add(name: String, link: String) {
        val clean = link.trim()
        val good = try {
            Mobile.checkAccountLink(clean)
            true
        } catch (e: Exception) {
            false
        }
        if (!good) {
            Toast.makeText(host, R.string.servers_sub_bad, Toast.LENGTH_LONG).show()
            return
        }

        val was = store.accountLink
        store.addSubscription(name, clean)
        Toast.makeText(host, R.string.servers_sub_added, Toast.LENGTH_SHORT).show()
        renderSubscriptions()
        if (store.accountLink != was) onSubscriptionChanged()
    }

    /** use делает подписку рабочей: ключ другой, туннель поднимается заново. */
    private fun use(sub: Store.Subscription) {
        if (sub.link == store.accountLink) return
        store.accountLink = sub.link
        renderSubscriptions()
        onSubscriptionChanged()
    }

    private fun forget(sub: Store.Subscription) {
        AlertDialog.Builder(host)
            .setMessage(host.getString(R.string.servers_sub_remove_ask, sub.name))
            .setPositiveButton(R.string.servers_sub_remove) { _, _ ->
                val was = store.accountLink
                store.removeSubscription(sub.link)
                renderSubscriptions()
                if (store.accountLink != was) onSubscriptionChanged()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    /**
     * renderSubscriptions рисует подписки — и только когда их больше одной.
     *
     * У большинства продавец один, и заголовок со списком из одной строки
     * над ним — шум над очевидным.
     */
    fun renderSubscriptions() {
        val list = store.subscriptions
        val show = list.size > 1
        ui.subsLabel.isVisible = show
        ui.subList.isVisible = show
        ui.subList.removeAllViews()
        if (!show) return

        val t = theme()
        for (sub in list) {
            val active = sub.link == store.accountLink
            val row = TextView(host).apply {
                text = if (active) {
                    sub.name + " · " + host.getString(R.string.servers_sub_active)
                } else {
                    sub.name
                }
                textSize = 14f
                setTextColor(if (active) t.acc else t.fg)
                val side = (14 * dp).toInt()
                val tall = (12 * dp).toInt()
                setPadding(side, tall, side, tall)
                background = Paint.rounded(t.surf, t.r, dp)
                isClickable = true
                setOnClickListener { use(sub) }
                setOnLongClickListener { forget(sub); true }
            }
            val lp = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT,
            )
            lp.topMargin = (6 * dp).toInt()
            ui.subList.addView(row, lp)
        }
    }

    /**
     * open зовётся при каждом показе экрана: ядро могло смениться.
     *
     * Первый заход за подключение меряем сами. Экран, на котором вместо времён
     * стоят прочерки, выбрать не помогает — а нажать «Обновить» догадается не
     * каждый, кто сюда зашёл.
     */
    fun open() {
        renderSubscriptions()
        load(measure = MarviaState.core != null && !MarviaState.anyMeasured())
    }

    /**
     * load забирает список у ядра.
     *
     * measure означает настоящий перезамер: ядро ходит до каждой ноды по
     * очереди, и это секунды. Держать в них главный поток нельзя — поэтому
     * работа на Dispatchers.IO, а на экране крутится колесо.
     */
    private fun load(measure: Boolean) {
        val core = MarviaState.core
        if (core == null) {
            renderEmpty()
            return
        }

        if (busy) {
            return
        }
        busy(true)

        host.lifecycleScope.launch {
            val json = withContext(Dispatchers.IO) {
                try {
                    if (measure) core.measure() else core.nodes()
                } catch (_: Throwable) {
                    // Туннель мог упасть прямо во время замера. Пустой ответ
                    // честнее исключения: экран просто скажет, что списка нет.
                    "[]"
                }
            }

            val rows = NodeRow.parse(json)
            if (measure) {
                MarviaState.remember(rows)
            }

            busy(false)
            render(rows)
        }
    }

    /**
     * select переводит туннель на ноду. Ноль — обратно к автовыбору.
     *
     * Ядро при этом заново договаривается с нодой: это поход в сеть, и он
     * может не получиться. Молча вернуть человека к прежней стране нельзя —
     * он решит, что нажатие не сработало.
     */
    private fun select(id: Long) {
        val core = MarviaState.core ?: return
        if (busy) {
            return
        }
        busy(true)

        host.lifecycleScope.launch {
            val failure = withContext(Dispatchers.IO) {
                try {
                    core.selectNode(id)
                    ""
                } catch (t: Throwable) {
                    MarviaVpnService.reasonOf(t)
                }
            }

            busy(false)
            if (failure.isNotEmpty()) {
                Toast.makeText(
                    host,
                    host.getString(R.string.servers_failed, failure),
                    Toast.LENGTH_LONG,
                ).show()
            }

            load(measure = false)
        }
    }

    private fun busy(now: Boolean) {
        busy = now
        ui.measureSpinner.isVisible = now
        ui.measureButton.isEnabled = !now
        ui.measureButton.alpha = if (now) 0.4f else 1f
    }

    /** paint перекрашивает экран в новую тему по тому, что уже показано. */
    fun paint() {
        renderSubscriptions()
        if (shown.isEmpty()) renderEmpty() else render(shown)
    }

    private fun renderEmpty() {
        shown = emptyList()
        ui.autoRow.isVisible = false
        ui.manualLabel.isVisible = false
        ui.nodeList.removeAllViews()
        // Мерить нечего — и кнопки «Обновить» тоже быть не должно.
        ui.measureButton.isVisible = false
        ui.serversEmpty.setText(R.string.servers_empty)
        ui.serversEmpty.isVisible = true
    }

    private fun render(rows: List<NodeRow>) {
        if (rows.isEmpty()) {
            renderEmpty()
            return
        }
        shown = rows

        ui.measureButton.isVisible = true
        ui.serversEmpty.isVisible = false
        ui.autoRow.isVisible = true
        ui.manualLabel.isVisible = true

        // Главный экран узнаёт о переезде отсюда же: иначе он до следующего
        // круга опроса показывал бы страну, через которую трафик уже не идёт.
        publish(rows)

        renderAuto(rows)
        renderList(rows)
    }

    /** publish переносит выбор и текущую ноду в общее состояние. */
    private fun publish(rows: List<NodeRow>) {
        val on = MarviaState.state.value as? TunnelState.On ?: return
        val current = rows.firstOrNull { it.current }
        val chosen = rows.firstOrNull { it.chosen }

        MarviaState.set(
            on.copy(
                node = current?.title ?: on.node,
                ms = current?.let { known(it)?.ms ?: 0 } ?: 0,
                chosen = chosen?.title.orEmpty(),
            ),
        )
    }

    private fun renderAuto(rows: List<NodeRow>) {
        val manual = rows.any { it.chosen }
        val fastest = rows
            .mapNotNull { row -> known(row)?.let { row to it } }
            .filter { it.second.alive && it.second.ms > 0 }
            .minByOrNull { it.second.ms }

        val t = theme()
        ui.autoRow.background = Paint.card(t, dp, stroke = if (manual) t.line else t.acc)
        ui.autoMark.background = Paint.ring(t, dp, chosen = !manual)
        ImageViewCompat.setImageTintList(ui.autoMarkCheck, ColorStateList.valueOf(t.accFg))
        ui.autoMarkCheck.isVisible = !manual

        ui.autoNote.text = if (fastest == null) {
            host.getString(R.string.servers_auto_unknown)
        } else {
            host.getString(R.string.servers_auto_note, label(fastest.first))
        }

        // Пока не мерили, самой быстрой нет — и числа тоже нет, только прочерк.
        showPing(ui.autoPing, fastest?.second?.ms ?: 0)
    }

    private fun renderList(rows: List<NodeRow>) {
        val inflater = LayoutInflater.from(host)
        ui.nodeList.removeAllViews()

        val current = rows.firstOrNull { it.current }

        for ((country, group) in rows.groupBy { it.group }) {
            val header = ItemCountryBinding.inflate(inflater, ui.nodeList, false)
            header.root.text = country.ifEmpty { host.getString(R.string.servers_group_other) }
            Paint.apply(header.root, theme())
            ui.nodeList.addView(header.root)

            for (row in group) {
                ui.nodeList.addView(nodeView(inflater, row, current))
            }
        }
    }

    private fun nodeView(inflater: LayoutInflater, row: NodeRow, current: NodeRow?): View {
        val item = ItemNodeBinding.inflate(inflater, ui.nodeList, false)

        // Флаг только для узнанной страны: чужой флаг увёл бы человека не
        // туда, куда он собирался, и он бы этого не заметил.
        val flag = Flags.of(row.group)
        item.nodeFlag.text = flag
        item.nodeFlag.isVisible = flag.isNotEmpty()

        item.nodeTitle.text = row.place
        item.nodeNote.text = noteFor(row, current)

        val seen = known(row)
        showPing(item.nodePing, if (seen?.alive == true) seen.ms else 0)

        val t = theme()
        item.root.background = Paint.card(t, dp, stroke = if (row.chosen) t.acc else t.line)
        item.nodeMark.background = Paint.ring(t, dp, chosen = row.chosen)
        ImageViewCompat.setImageTintList(item.nodeMarkCheck, ColorStateList.valueOf(t.accFg))
        item.nodeMarkCheck.isVisible = row.chosen
        // Строка собрана из разметки после общей покраски — красим её здесь.
        Paint.apply(item.root, t)
        item.root.setOnClickListener { select(row.id) }

        return item.root
    }

    /**
     * noteFor — строка под названием.
     *
     * Здесь и разводятся «выбрана» и «через неё идёт трафик». Совпали — так и
     * пишем одной строкой. Разошлись — называем ту, через которую трафик идёт
     * на самом деле.
     */
    private fun noteFor(row: NodeRow, current: NodeRow?): String = when {
        row.chosen && row.current -> host.getString(R.string.node_chosen_current)
        row.chosen -> host.getString(
            R.string.node_chosen_silent,
            current?.let { label(it) }.orEmpty(),
        )
        row.current -> host.getString(R.string.node_current)
        // «Не отвечает» говорим только про ноду, до которой правда ходили:
        // в ответе nodes() живость не проставлена вовсе, и написать по нему
        // «недоступна» значило бы похоронить рабочую страну.
        else -> when (known(row)?.alive) {
            null -> host.getString(R.string.node_unmeasured)
            true -> host.getString(R.string.node_ok)
            false -> host.getString(R.string.node_down)
        }
    }

    /**
     * known — что известно про ноду: из этого ответа или из прошлого замера.
     *
     * Ответ nodes() времён не содержит вовсе, поэтому без памяти о замере
     * список сразу после переключения страны стал бы сплошными прочерками.
     */
    private fun known(row: NodeRow): MarviaState.Ping? = when {
        row.ms > 0 -> MarviaState.Ping(row.ms, row.alive)
        row.current -> MarviaState.ping(row.id) ?: MarviaState.Ping(0, true)
        else -> MarviaState.ping(row.id)
    }

    private fun showPing(view: android.widget.TextView, ms: Long) {
        val t = theme()
        if (ms <= 0) {
            view.setText(R.string.node_ping_none)
            view.setTextColor(t.dim)
            return
        }

        view.text = host.getString(R.string.node_ping, ms)
        view.setTextColor(pingColor(t, ms))
    }

    /** Как ноду называть человеку: страна, а не «ae-1». */
    private fun label(row: NodeRow): String = row.country.ifEmpty { row.name }

    private companion object {
        /** Ноль в selectNode означает «выбирай сам». */
        const val AUTO = 0L
    }
}

/**
 * pingColor — быстро акцентом, терпимо янтарным, плохо красным.
 *
 * Пороги одни на оба экрана: список стран и главный экран не должны спорить,
 * хорошие ли это сорок миллисекунд.
 */
fun pingColor(t: Theme, ms: Long): Int = when {
    ms < 70 -> t.acc
    ms < 250 -> t.warn
    else -> t.fail
}
