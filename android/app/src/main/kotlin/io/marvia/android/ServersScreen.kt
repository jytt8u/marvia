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
import io.marvia.android.databinding.ItemProviderBinding
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
        ChoiceSheet.show(host, theme(), host.getString(R.string.servers_add), items.toList()) { which ->
            if (which == 0) fromClipboard() else byHand()
        }
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

        val t = theme()
        for (field in listOf(name, link)) {
            field.setTextColor(t.fg); field.setHintTextColor(t.dim)
            field.backgroundTintList = ColorStateList.valueOf(t.acc)
            field.minHeight = (54 * dp).toInt()
        }
        link.inputType = android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_URI or android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
        ChoiceSheet.form(host, t, host.getString(R.string.servers_add_manual), box, host.getString(R.string.servers_add)) {
            val clean = link.text.toString().trim()
            val valid = runCatching { Mobile.checkAccountLink(clean) }.isSuccess
            if (!valid) link.error = host.getString(R.string.servers_sub_bad)
            else add(name.text.toString(), clean)
            valid
        }
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
        ThemedDialogs.builder(host, theme())
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

    /** renderSubscriptions — перерисовать карточки по тому, что уже показано. */
    fun renderSubscriptions() {
        if (shown.isEmpty()) renderEmpty() else render(shown)
    }
    /**
     * open зовётся при каждом показе экрана: ядро могло смениться.
     *
     * Первый заход за подключение меряем сами. Экран, на котором вместо времён
     * стоят прочерки, выбрать не помогает — а нажать «Обновить» догадается не
     * каждый, кто сюда зашёл.
     */
    fun open() {
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
    }

    private fun renderEmpty() {
        shown = emptyList()
        ui.autoRow.isVisible = false
        // Мерить нечего — и кнопки перезамера тоже быть не должно.
        ui.measureButton.isVisible = false
        ui.serversEmpty.setText(R.string.servers_empty)
        ui.serversEmpty.isVisible = true
        renderProviders(emptyList())
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

        // Главный экран узнаёт о переезде отсюда же: иначе он до следующего
        // круга опроса показывал бы страну, через которую трафик уже не идёт.
        publish(rows)

        renderAuto(rows)
        renderProviders(rows)
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
        ui.autoIcon.background = Paint.rounded(t.accSoft, 12, dp)
        ui.autoMark.background = Paint.ring(t, dp, chosen = !manual)
        ImageViewCompat.setImageTintList(ui.autoMarkCheck, ColorStateList.valueOf(t.accFg))
        ui.autoMarkCheck.isVisible = !manual

        ui.autoNote.text = if (fastest == null) {
            host.getString(R.string.servers_auto_unknown)
        } else {
            host.getString(R.string.servers_auto_note, label(fastest.first))
        }
    }

    /**
     * renderProviders — по карточке на подписку, как в макете.
     *
     * Рабочая раскрыта: её серверы — это и есть список нод из ядра, а строка
     * остатка — то, что панель прислала при подключении. Остальные свёрнуты
     * до имени: их серверов и остатка телефон не знает, пока не переключится,
     * а рисовать вместо них прочерки — обещать то, чего нет.
     */
    private fun renderProviders(rows: List<NodeRow>) {
        val t = theme()
        val inflater = LayoutInflater.from(host)
        val subs = store.subscriptions
        val on = MarviaState.state.value as? TunnelState.On
        val current = rows.firstOrNull { it.current }

        // Кнопки в шапке красятся здесь: у картинки один тег, и он занят значком.
        ui.addSubscription.backgroundTintList = ColorStateList.valueOf(t.acc)
        ui.measureButton.backgroundTintList = ColorStateList.valueOf(t.surf)
        ui.serversSubtitle.text = host.resources.getQuantityString(R.plurals.servers_subs_count, subs.size, subs.size) +
            if (rows.isEmpty()) "" else " · " + host.resources.getQuantityString(R.plurals.servers_nodes_count, rows.size, rows.size)

        ui.providerList.removeAllViews()
        for (sub in subs) {
            val active = sub.link == store.accountLink
            val card = ItemProviderBinding.inflate(inflater, ui.providerList, false)
            card.providerName.text = sub.name
            card.providerMeta.text = when {
                !active -> host.getString(R.string.servers_sub_collapsed)
                rows.isEmpty() -> host.getString(R.string.servers_sub_active)
                else -> host.resources.getQuantityString(R.plurals.servers_nodes_count, rows.size, rows.size)
            }
            card.providerChevron.rotation = if (active) 0f else -90f
            card.providerHead.setOnClickListener { if (!active) use(sub) }
            card.providerMenu.setOnClickListener { actions(sub, active) }

            val quota = on?.subscription?.takeIf { active && it.known }
            card.providerQuota.isVisible = quota != null
            if (quota != null) {
                val days = if (quota.until.isEmpty()) null else Format.daysLeft(quota.until)
                card.providerLeft.text = if (days == null) "" else host.getString(R.string.servers_sub_days_left, days)
                card.providerLeft.isVisible = days != null
                val limited = quota.limitBytes > 0
                card.providerTrack.isVisible = limited
                card.providerUsage.text = if (limited) {
                    host.getString(R.string.traffic_of, Format.size(host, quota.leftBytes), Format.size(host, quota.limitBytes))
                } else "∞"
                if (limited) {
                    val left = (quota.leftBytes.toFloat() / quota.limitBytes).coerceIn(0f, 1f)
                    (card.providerFill.layoutParams as LinearLayout.LayoutParams).weight = left
                    (card.providerRest.layoutParams as LinearLayout.LayoutParams).weight = 1f - left
                }
                card.providerTrack.clipToOutline = true
            }

            if (active && rows.isNotEmpty()) {
                card.providerRule.isVisible = true
                card.providerNodes.isVisible = true
                for ((i, row) in rows.withIndex()) {
                    if (i > 0) card.providerNodes.addView(rule(t))
                    card.providerNodes.addView(nodeView(inflater, card.providerNodes, row, current))
                }
            }

            card.providerMenu.backgroundTintList = ColorStateList.valueOf(t.surf2)
            Paint.apply(card.root, t)
            ui.providerList.addView(card.root)
        }
    }

    /** rule — линия между серверами внутри карточки. */
    private fun rule(t: Theme): View = View(host).apply {
        setBackgroundColor(t.line)
        layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, (1 * dp).toInt())
    }

    /** actions — что можно сделать с подпиской: сделать рабочей, убрать. */
    private fun actions(sub: Store.Subscription, active: Boolean) {
        val items = mutableListOf(host.getString(R.string.servers_sub_remove))
        if (!active) items.add(0, host.getString(R.string.servers_sub_use))
        ChoiceSheet.show(host, theme(), sub.name, items) { which ->
            if (!active && which == 0) use(sub) else forget(sub)
        }
    }

    private fun nodeView(inflater: LayoutInflater, parent: android.view.ViewGroup, row: NodeRow, current: NodeRow?): View {
        val item = ItemNodeBinding.inflate(inflater, parent, false)

        // Флаг только для узнанной страны: чужой флаг увёл бы человека не
        // туда, куда он собирался, и он бы этого не заметил.
        val flag = Flags.of(row.group)
        item.nodeFlag.text = flag
        item.nodeFlag.isVisible = flag.isNotEmpty()

        // Страна крупно, город с именем ноды и состоянием — под ней.
        item.nodeTitle.text = row.group.ifEmpty { row.name }
        val setup = row.setupMs.takeIf { it > 0 } ?: MarviaState.ping(row.id)?.setupMs ?: 0
        item.nodeNote.text = listOf(
            row.place.takeIf { it != row.name && it.isNotEmpty() }.orEmpty(),
            row.name,
            noteFor(row, current),
            if (setup > 0) host.getString(R.string.node_setup, setup) else "",
        ).filter { it.isNotEmpty() }.joinToString(" · ")

        val t = theme()
        val seen = known(row)
        val ms = if (seen?.alive == true) seen.ms else 0L
        item.nodeBars.show(ms, seen?.alive == true, t)
        showPing(item.nodePing, ms)

        // Выбранная руками — залита мягким акцентом; та, через которую идёт
        // трафик, — чуть светлее остальных.
        item.root.setBackgroundColor(
            when {
                row.chosen -> t.accSoft
                row.current -> Look.withAlpha(t.fg, 0.04)
                else -> 0
            },
        )
        item.nodeTitle.alpha = if (seen?.alive == false) 0.55f else 1f
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
        row.setupMs > 0 || row.ms > 0 -> MarviaState.Ping(row.ms, row.alive, row.setupMs)
        row.current -> MarviaState.Ping(row.ms, row.alive, row.setupMs)
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
