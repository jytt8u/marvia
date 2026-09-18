package io.marvia.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.util.AttributeSet
import androidx.appcompat.widget.AppCompatButton

/** Свет и тонкий незамкнутый обод повторяют исходный макет. */
class PowerButton @JvmOverloads constructor(context: Context, attrs: AttributeSet? = null) : AppCompatButton(context, attrs) {
    private val brush = Paint(Paint.ANTI_ALIAS_FLAG)
    var theme: Theme = Look.theme(Look.Choice())
        set(value) { field=value; invalidate() }
    init { background=null; setAllCaps(false); isHapticFeedbackEnabled=true }
    override fun onDraw(canvas: Canvas) {
        val dp=resources.displayMetrics.density
        val cx=width/2f; val cy=height/2f; val radius=minOf(width,height)/2f-8*dp
        val scale=if(isPressed) .965f else 1f
        canvas.save();canvas.scale(scale,scale,cx,cy)
        brush.style=Paint.Style.FILL
        val solid=theme.btn=="solid"
        if (theme.btn!="bare") {
            brush.shader=RadialGradient(cx-radius*.5f,cy-radius*.65f,radius*2, if(solid) intArrayOf(theme.acc,theme.acc) else intArrayOf(theme.surf2,theme.surf),null,Shader.TileMode.CLAMP)
            if(theme.btn=="glass") { brush.shader=null;brush.color=theme.accSoft }
            canvas.drawCircle(cx,cy,radius,brush);brush.shader=null
        }
        brush.style=Paint.Style.STROKE;brush.strokeWidth=2*dp;brush.strokeCap=Paint.Cap.ROUND;brush.color=theme.acc
        canvas.drawArc(RectF(cx-radius,cy-radius,cx+radius,cy+radius),45f,285f,false,brush)
        brush.color=if(solid)theme.accFg else theme.acc
        brush.strokeWidth=3*dp
        val r=23*dp; val y=cy-8*dp
        canvas.drawArc(RectF(cx-r,y-r,cx+r,y+r),-50f,280f,false,brush)
        canvas.drawLine(cx,y-r-4*dp,cx,y-5*dp,brush)
        brush.style=Paint.Style.FILL;brush.textSize=11*resources.displayMetrics.scaledDensity;brush.textAlign=Paint.Align.CENTER
        canvas.drawText(text.toString(),cx,cy+49*dp,brush)
        canvas.restore()
    }
    override fun drawableStateChanged() { super.drawableStateChanged(); invalidate() }
    override fun performClick(): Boolean { performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK);return super.performClick() }
}
