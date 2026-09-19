package io.marvia.android

import android.animation.ValueAnimator
import android.view.View
import android.view.ViewGroup
import android.view.animation.DecelerateInterpolator
import android.widget.ScrollView

/**
 * Motion — движение при смене экрана: содержимое всплывает снизу и
 * проявляется, карточки — по очереди.
 *
 * Как в макете, где у карточек класс `rise` с задержками: экран не
 * возникает целиком, а собирается сверху вниз за треть секунды. Уважает
 * системное «без анимаций» — тогда всё появляется сразу.
 */
object Motion {

    /** rise запускает всплытие на экране: сам экран и детей его списка. */
    fun rise(screen: View) {
        if (!ValueAnimator.areAnimatorsEnabled()) return
        val dp = screen.resources.displayMetrics.density

        screen.alpha = 0f
        screen.translationY = 10 * dp
        screen.animate().cancel()
        screen.animate().alpha(1f).translationY(0f)
            .setDuration(220).setInterpolator(DecelerateInterpolator(1.5f)).start()

        // Дети первого списка — по очереди, с шагом в 40 мс.
        val list = firstScroll(screen)?.getChildAt(0) as? ViewGroup ?: return
        var n = 0
        for (i in 0 until list.childCount) {
            val child = list.getChildAt(i)
            if (child.visibility != View.VISIBLE) continue
            child.alpha = 0f
            child.translationY = 14 * dp
            child.animate().cancel()
            child.animate().alpha(1f).translationY(0f)
                .setStartDelay(40L * n++).setDuration(320)
                .setInterpolator(DecelerateInterpolator(1.8f)).start()
            if (n > 8) break
        }
    }

    private fun firstScroll(v: View): ViewGroup? {
        if (v is ScrollView) return v
        if (v is ViewGroup) for (i in 0 until v.childCount) firstScroll(v.getChildAt(i))?.let { return it }
        return null
    }
}
