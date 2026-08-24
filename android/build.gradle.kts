plugins {
    // Отдельный плагин Kotlin больше не нужен: с девятой версии сборщик
    // Android умеет Kotlin сам, а попытка подключить старый плагин обрывает
    // сборку.
    id("com.android.application") version "9.3.2" apply false
}
