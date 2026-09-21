package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Typeface
import android.util.AttributeSet
import android.view.View
import java.util.Locale

data class TrafficSnapshot(val today: Long = 0, val seconds: Long = 0, val bytesPerSecond: Double = 0.0, val hours: List<Long> = List(24){0L})

/**
 * TrafficHistory — снимок для главного экрана: сегодня по часам, длительность
 * сессии, скорость сейчас.
 *
 * Сам ничего не хранит: расход по дням ведёт [Traffic], и сегодняшние 24
 * часа — одна его строка. Второе хранилище тех же чисел разошлось бы с
 * первым при первой же смене суток. Здесь остаётся только то, что живёт
 * ровно сессию: с какого момента считать длительность и прошлый отсчёт
 * для скорости.
 */
class TrafficHistory(context: Context) {
    private val traffic = Traffic(context)
    private var lastBytes = 0L
    private var lastTime = 0L
    private var start = 0L

    /** saved — что показать до подключения: сегодняшний расход без сессии. */
    fun saved(): TrafficSnapshot {
        val hours = traffic.today()
        return TrafficSnapshot(today = hours.sum(), hours = hours)
    }

    /**
     * sample принимает накопительный счётчик ядра.
     *
     * Скорость — по разнице с прошлым снятием, на монотонных часах: время
     * телефона может прыгнуть, и скорость стала бы отрицательной или
     * бесконечной.
     */
    fun sample(bytes: Long, place: String, now: Long = android.os.SystemClock.elapsedRealtime()): TrafficSnapshot {
        traffic.note(bytes, place)
        if (start == 0L) start = now
        val delta = (bytes - lastBytes).coerceAtLeast(0)
        val rate = if (lastTime == 0L || now <= lastTime) 0.0 else delta * 1000.0 / (now - lastTime)
        lastBytes = bytes
        lastTime = now
        val hours = traffic.today()
        return TrafficSnapshot(hours.sum(), (now - start) / 1000, rate, hours)
    }
}
/** Три блока исходного макета: день, длительность сессии, текущая скорость. */
class TrafficPanel @JvmOverloads constructor(context: Context, attrs: AttributeSet?=null): View(context,attrs) {
    var theme: Theme=Look.theme(Look.Choice())
        set(value){field=value;invalidate()}
    var snapshot=TrafficSnapshot()
        set(value){field=value;contentDescription="${context.getString(R.string.stats_today)}: ${Format.size(context,value.today)}, ${value.seconds} s";invalidate()}
    private val brush=Paint(Paint.ANTI_ALIAS_FLAG)
    override fun onDraw(canvas: Canvas){
        val dp=resources.displayMetrics.density;val w=width.toFloat();val top=112*dp;val gap=10*dp;val half=(w-gap)/2
        brush.color=theme.surf
        canvas.drawRoundRect(0f,0f,w,top,theme.r*dp,theme.r*dp,brush)
        canvas.drawRoundRect(0f,top+gap,half,height.toFloat(),theme.r*dp,theme.r*dp,brush)
        canvas.drawRoundRect(half+gap,top+gap,w,height.toFloat(),theme.r*dp,theme.r*dp,brush)
        fun label(s:String,x:Float,y:Float,size:Float,color:Int,mono:Boolean=false){brush.color=color;brush.textSize=size*resources.displayMetrics.scaledDensity;brush.typeface=if(mono)Typeface.MONOSPACE else Typeface.DEFAULT;canvas.drawText(s,x,y,brush)}
        label(context.getString(R.string.stats_today),14*dp,25*dp,12f,theme.dim)
        val total=Format.size(context,snapshot.today);brush.textSize=14*resources.displayMetrics.scaledDensity;brush.typeface=Typeface.MONOSPACE
        label(total,w-14*dp-brush.measureText(total),25*dp,14f,theme.fg,true)
        val max=(snapshot.hours.maxOrNull()?:0L).coerceAtLeast(1);val bw=(w-28*dp)/24
        snapshot.hours.forEachIndexed { i,v ->brush.color=if(v==0L)theme.line else theme.acc;val h=if(v==0L)2*dp else (v.toDouble()/max*49*dp).toFloat().coerceAtLeast(3*dp);canvas.drawRoundRect(14*dp+i*bw,91*dp-h,14*dp+(i+1)*bw-3*dp,91*dp,2*dp,2*dp,brush)}
        label("00",14*dp,105*dp,9f,theme.dim,true);label("23",w-30*dp,105*dp,9f,theme.dim,true)
        label(context.getString(R.string.stats_session),14*dp,top+33*dp,11f,theme.dim)
        val s=snapshot.seconds;label(String.format(Locale.US,"%02d:%02d:%02d",s/3600,s/60%60,s%60),14*dp,top+60*dp,16f,theme.fg,true)
        label(context.getString(R.string.stats_speed),half+gap+14*dp,top+33*dp,11f,theme.dim)
        label(context.getString(R.string.stats_mbps,String.format(Locale.getDefault(),"%.1f",snapshot.bytesPerSecond*8/1000000)),half+gap+14*dp,top+60*dp,14f,theme.fg,true)
    }
}
