package io.marvia.android

import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager

/**
 * Приложения, которые ходят мимо туннеля — или, наоборот, только через него.
 *
 * Зачем это вообще. Нода стоит за границей, и для любого сервера человек
 * находится там же. Госуслуги, банки и всё государственное на запросы из-за
 * границы просто не отвечают — отсюда «включил VPN, и госуслуги не
 * открываются». Исключённому приложению система оставляет обычную сеть, и на
 * сервер приходит его настоящий адрес.
 *
 * Чего это не чинит: значок VPN в шторке. Его рисует Android, пока поднят
 * туннель, и убрать его нельзя. Редкие приложения смотрят не на адрес, а на
 * наличие интерфейса в системе — от них исключение не спасает, и обещать
 * обратное нечестно.
 */
object Bypass {

    /** Приложение, каким его видит человек. */
    data class Entry(val pkg: String, val label: String)

    /**
     * installed перечисляет приложения, которые человек запускает сам.
     *
     * Системные без экрана запуска отсеиваем: они ничего не откроют и в списке
     * из трёхсот строк только мешают искать своё. Читать в фоне: у человека их
     * бывает под три сотни, и на медленном телефоне это заметная пауза.
     */
    fun installed(context: Context): List<Entry> {
        val pm = context.packageManager
        val mine = context.packageName

        return pm.getInstalledApplications(0)
            .asSequence()
            .filter { it.packageName != mine }
            .filter { pm.getLaunchIntentForPackage(it.packageName) != null || it.isUpdatedSystem() }
            .map { Entry(it.packageName, pm.getApplicationLabel(it).toString()) }
            .toList()
    }

    /** Обновлённое системное приложение — обычно как раз банк или госуслуги от производителя. */
    private fun ApplicationInfo.isUpdatedSystem(): Boolean =
        flags and ApplicationInfo.FLAG_UPDATED_SYSTEM_APP != 0

    /** Название приложения по имени пакета — для сводки на главном экране. */
    fun label(context: Context, pkg: String): String = try {
        val pm = context.packageManager
        pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0)).toString()
    } catch (_: PackageManager.NameNotFoundException) {
        pkg
    }
}
