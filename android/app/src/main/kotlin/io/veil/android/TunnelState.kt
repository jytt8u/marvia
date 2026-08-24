package io.veil.android

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Что сейчас с туннелем. */
sealed interface TunnelState {

    /** Туннеля нет, трафик идёт напрямую. */
    data object Off : TunnelState

    /** Идёт замер нод и подключение. */
    data object Connecting : TunnelState

    /**
     * Туннель поднят.
     *
     * warning — последняя ошибка отдельного соединения. Туннель от неё не
     * падает: одна недоступная цель это норма. Но показать её стоит, иначе
     * человек видит только «подключено» при наполовину живой ноде.
     */
    data class On(val node: String, val warning: String = "") : TunnelState

    /** Подняться не удалось. reason — то, что сказало ядро. */
    data class Failed(val reason: String) : TunnelState
}

/**
 * VeilState — единственное место, где живёт состояние туннеля.
 *
 * Служба и экран работают в одном процессе, поэтому широковещательные
 * сообщения между ними были бы лишним слоем: один пишет, другой читает.
 * Экран может быть закрыт и открыт заново — состояние переживает это, потому
 * что принадлежит процессу, а не экрану.
 */
object VeilState {
    private val current = MutableStateFlow<TunnelState>(TunnelState.Off)

    val state: StateFlow<TunnelState> = current.asStateFlow()

    fun set(next: TunnelState) {
        current.value = next
    }
}
