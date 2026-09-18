package io.marvia.android

import android.content.Context
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.drawable.RippleDrawable
import android.view.Gravity
import android.view.View
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import com.google.android.material.bottomsheet.BottomSheetDialog

/** Общая панель выбора сохраняет цвета приложения и доступные зоны нажатия. */
object ChoiceSheet {
    fun form(context: Context, theme: Theme, title: String, content: View, action: String, submit: () -> Boolean) {
        val dp=context.resources.displayMetrics.density
        val dialog=BottomSheetDialog(context)
        val box=LinearLayout(context).apply {
            orientation=LinearLayout.VERTICAL
            setPadding((24*dp).toInt(),(24*dp).toInt(),(24*dp).toInt(),(28*dp).toInt())
            background=Paint.rounded(theme.surf,24,dp)
        }
        box.addView(TextView(context).apply { text=title;textSize=22f;setTextColor(theme.fg);setPadding(0,0,0,(16*dp).toInt()) })
        box.addView(content)
        box.addView(android.widget.Button(context).apply {
            text=action;isAllCaps=false;setTextColor(theme.accFg);minHeight=(52*dp).toInt()
            backgroundTintList=null
            background=RippleDrawable(ColorStateList.valueOf(theme.accSoft),Paint.rounded(theme.acc,14,dp),null)
            setOnClickListener { if(submit()) dialog.dismiss() }
        },LinearLayout.LayoutParams(-1,-2).apply { topMargin=(20*dp).toInt() })
        dialog.setContentView(ScrollView(context).apply { addView(box) })
        dialog.setOnShowListener { dialog.findViewById<View>(com.google.android.material.R.id.design_bottom_sheet)?.setBackgroundColor(Color.TRANSPARENT) }
        dialog.show()
    }
    fun show(context: Context, theme: Theme, title: String, labels: List<String>, selected: Int = -1, pick: (Int) -> Unit) {
        val dp = context.resources.displayMetrics.density
        val dialog = BottomSheetDialog(context)
        val box = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding((24*dp).toInt(), (18*dp).toInt(), (24*dp).toInt(), (28*dp).toInt())
            background = Paint.rounded(theme.surf, 24, dp)
        }
        box.addView(TextView(context).apply {
            text = title; textSize = 22f; setTextColor(theme.fg)
            setPadding(0, 0, 0, (20*dp).toInt())
        })
        labels.forEachIndexed { index, label ->
            box.addView(TextView(context).apply {
                text = if (index == selected) "$label   ✓" else label
                textSize = 16f; setTextColor(if(index==selected) theme.acc else theme.fg)
                minHeight = (56*dp).toInt(); gravity = Gravity.CENTER_VERTICAL
                setPadding((14*dp).toInt(), (12*dp).toInt(), (14*dp).toInt(), (12*dp).toInt())
                background = RippleDrawable(ColorStateList.valueOf(theme.accSoft), Paint.rounded(if(index==selected) theme.surf2 else Color.TRANSPARENT, 14, dp), null)
                isFocusable = true
                setOnClickListener { performHapticFeedback(android.view.HapticFeedbackConstants.CLOCK_TICK); dialog.dismiss(); pick(index) }
            }, LinearLayout.LayoutParams(-1,-2))
        }
        dialog.setContentView(ScrollView(context).apply { addView(box) })
        dialog.setOnShowListener {
            dialog.findViewById<View>(com.google.android.material.R.id.design_bottom_sheet)?.setBackgroundColor(Color.TRANSPARENT)
        }
        dialog.show()
    }
}
