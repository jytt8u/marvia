package io.marvia.android

import android.content.Context
import com.google.android.material.dialog.MaterialAlertDialogBuilder

object ThemedDialogs {
    fun builder(context: Context, theme: Theme): MaterialAlertDialogBuilder =
        object : MaterialAlertDialogBuilder(context) {
            override fun show(): androidx.appcompat.app.AlertDialog {
                val dialog=super.show()
                fun tint(view: android.view.View) {
                    if(view is android.widget.TextView) view.setTextColor(theme.fg)
                    if(view is android.view.ViewGroup) for(i in 0 until view.childCount) tint(view.getChildAt(i))
                }
                dialog.window?.decorView?.let { tint(it) }
                for(which in listOf(-1,-2,-3)) dialog.getButton(which)?.setTextColor(theme.acc)
                return dialog
            }
        }.setBackground(Paint.rounded(theme.surf, 24, context.resources.displayMetrics.density))
}
