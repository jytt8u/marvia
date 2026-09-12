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

    /**
     * Что предлагаем одной кнопкой.
     *
     * Список российский и намеренно короткий: госуслуги, крупные банки, почта.
     * Это не «все банки страны», а то, обо что спотыкаются в первый же день.
     * Чего нет в телефоне, того в списке и не появится.
     */
    val PRESET = listOf(
        "ru.gosuslugi.pgu",
        "ru.rt.eq", // Госключ
        "ru.sberbankmobile",
        "ru.sberbank.sbbol",
        "com.idamob.tinkoff.android",
        "ru.vtb24.mobilebanking.android",
        "ru.alfabank.mobile.android",
        "ru.raiffeisennews",
        "ru.gazprombank.android.mobilebank.app",
        "com.openbank",
        "ru.mts.paysdk",
        "ru.pochta.tracker",
        "ru.russianpost.android",
        "ru.mos.polis", // ЕМИАС
        "ru.nalog.lk",
        "ru.fss.lk",
    )

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
