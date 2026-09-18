package io.marvia.android

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Экраны лежат друг на друге, и виден тот, которому show() поставил
 * видимость. Новый экран, забытый в этом списке, не падает и не ломает
 * сборку — он просто рисуется поверх соседа. Ровно это и случилось с экраном
 * расхода: он был виден одновременно с экраном ключа.
 */
class ScreensTest {

    @Test
    fun everyScreenInTheStackIsSwitchedByShow() {
        val layout = read("src/main/res/layout/activity_main.xml")
        val code = read("src/main/kotlin/io/marvia/android/MainActivity.kt")

        val ids = Regex("""android:id="@\+id/(\w+Screen)"""").findAll(layout)
            .map { it.groupValues[1] }
            .toList()

        assertTrue("в разметке не нашлось ни одного экрана", ids.size >= 6)
        for (id in ids) {
            assertTrue(
                "экран $id лежит в стопке, но show() не прячет его",
                code.contains("ui.$id.root.isVisible"),
            )
        }
    }

    /** Тест запускается из каталога модуля, но лучше не верить в это молча. */
    private fun read(path: String): String {
        var dir: File? = File(System.getProperty("user.dir").orEmpty()).absoluteFile
        while (dir != null) {
            val file = File(dir, path)
            if (file.isFile) return file.readText()
            dir = dir.parentFile
        }
        throw AssertionError("не нашёл $path от " + System.getProperty("user.dir"))
    }
}
