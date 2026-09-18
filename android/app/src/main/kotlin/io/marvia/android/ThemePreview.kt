package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.util.AttributeSet
import android.view.View

/** Образец использует те же отрисовщики, что и главная, поэтому не расходится с темой. */
class ThemePreview @JvmOverloads constructor(context: Context, attrs: AttributeSet?=null): View(context,attrs) {
    private val power=PowerButton(context)
    private val traffic=TrafficPanel(context)
    private val brush=Paint(Paint.ANTI_ALIAS_FLAG)
    var theme=Look.theme(Look.Choice())
        set(value){field=value;power.theme=value;traffic.theme=value;invalidate()}
    override fun onDraw(canvas: Canvas) {
        val dp=resources.displayMetrics.density
        val scale=minOf(width/(360*dp),height/(650*dp))
        canvas.save();canvas.translate((width-360*dp*scale)/2,0f);canvas.scale(scale,scale)
        Backdrop(theme).apply { setBounds(0,0,(360*dp).toInt(),(650*dp).toInt());draw(canvas) }
        brush.color=theme.fg;brush.textSize=16*dp;canvas.drawText("MARVIA",24*dp,38*dp,brush)
        val size=(184*dp).toInt();power.layout(0,0,size,size)
        canvas.save();canvas.translate(88*dp,74*dp);power.draw(canvas);canvas.restore()
        brush.textSize=23*dp;brush.textAlign=Paint.Align.CENTER
        canvas.drawText(context.getString(R.string.status_off),180*dp,300*dp,brush)
        traffic.layout(0,0,(312*dp).toInt(),(192*dp).toInt())
        canvas.save();canvas.translate(24*dp,378*dp);traffic.draw(canvas);canvas.restore()
        brush.textAlign=Paint.Align.LEFT
        canvas.restore()
    }
}
