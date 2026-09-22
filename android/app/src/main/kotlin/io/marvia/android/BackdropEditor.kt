package io.marvia.android

import android.annotation.SuppressLint
import android.app.Dialog
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Matrix
import android.graphics.Paint
import android.view.Gravity
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.updatePadding

/**
 * BackdropEditor — где на экране лежит своё фото.
 *
 * Во весь экран, поверх приложения: редактор занимает ровно ту площадь, на
 * которой фон потом и будет, и что видно здесь, то и окажется под
 * интерфейсом, — матрица у них общая (BackdropImage.place). Пелена темы
 * тоже та же, иначе выбирали бы кадр по одной картинке, а жили с другой.
 *
 * Жесты обычные для фото: двумя пальцами — ближе и дальше, одним — сдвиг.
 * Повернуть — кнопкой, по четверти: снимки с телефона бывают боком, а
 * крутить двумя пальцами на произвольный угол фону не нужно — такой фон
 * никто не выбирает нарочно.
 */
class BackdropEditor(
    private val context: Context,
    private val bitmap: Bitmap,
    private val fit: String,
    private val theme: Theme,
    private val veil: Int,
    start: BackdropFrame,
    private val onDone: (BackdropFrame) -> Unit,
) {
    private var frame = start

    fun show() {
        val dialog = Dialog(context, android.R.style.Theme_Black_NoTitleBar_Fullscreen)
        val dp = context.resources.displayMetrics.density
        val stage = Stage(context)
        val root = FrameLayout(context)
        root.addView(stage, FrameLayout.LayoutParams(MATCH, MATCH))

        val hint = TextView(context).apply {
            setText(R.string.backdrop_edit_hint)
            setTextColor(0xFFFFFFFF.toInt())
            textSize = 14f
            gravity = Gravity.CENTER
            setShadowLayer(6 * dp, 0f, 1 * dp, 0xCC000000.toInt())
            setPadding((24 * dp).toInt(), (16 * dp).toInt(), (24 * dp).toInt(), 0)
        }
        root.addView(hint, FrameLayout.LayoutParams(MATCH, WRAP, Gravity.TOP))

        val bar = LinearLayout(context).apply {
            orientation = LinearLayout.HORIZONTAL
            setPadding((16 * dp).toInt(), 0, (16 * dp).toInt(), (20 * dp).toInt())
        }
        fun button(text: Int, main: Boolean, act: () -> Unit) = TextView(context).apply {
            setText(text)
            gravity = Gravity.CENTER
            textSize = 15f
            typeface = android.graphics.Typeface.create(typeface, android.graphics.Typeface.BOLD)
            setTextColor(if (main) theme.accFg else theme.fg)
            background = io.marvia.android.Paint.rounded(if (main) theme.acc else Look.withAlpha(theme.surf, 0.92), minOf(theme.r, 14), dp)
            minHeight = (52 * dp).toInt()
            setOnClickListener { act() }
        }
        val lp = { LinearLayout.LayoutParams(0, WRAP, 1f).apply { marginStart = (4 * dp).toInt(); marginEnd = (4 * dp).toInt() } }
        bar.addView(button(R.string.backdrop_rotate, false) {
            frame = frame.copy(rot = (frame.rot + 1) % 4, fx = 0.5f, fy = 0.5f)
            stage.invalidate()
        }, lp())
        bar.addView(button(R.string.backdrop_reset, false) {
            frame = BackdropFrame(rot = frame.rot)
            stage.invalidate()
        }, lp())
        bar.addView(button(R.string.backdrop_done, true) {
            onDone(frame)
            dialog.dismiss()
        }, lp())
        root.addView(bar, FrameLayout.LayoutParams(MATCH, WRAP, Gravity.BOTTOM))

        // Кнопки — над системной навигацией, подсказка — под часами.
        ViewCompat.setOnApplyWindowInsetsListener(root) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            hint.updatePadding(top = bars.top + (16 * dp).toInt())
            bar.updatePadding(bottom = bars.bottom + (20 * dp).toInt())
            insets
        }
        dialog.setContentView(root, ViewGroup.LayoutParams(MATCH, MATCH))
        dialog.setOnCancelListener { /* «назад» — без сохранения, как отмена */ }
        dialog.show()
    }

    /** Stage — сам снимок под пальцами. */
    @SuppressLint("ViewConstructor")
    private inner class Stage(context: Context) : View(context) {
        private val m = Matrix()
        private val brush = Paint(Paint.FILTER_BITMAP_FLAG)
        private var lastX = 0f
        private var lastY = 0f
        private var pointer = -1

        private val scaler = ScaleGestureDetector(context, object : ScaleGestureDetector.SimpleOnScaleGestureListener() {
            override fun onScale(d: ScaleGestureDetector): Boolean {
                frame = frame.copy(zoom = (frame.zoom * d.scaleFactor).coerceIn(1f, BackdropFrame.MAX_ZOOM))
                // Ограничение сдвига зависит от приближения: пересчитываем
                // точку, чтобы при отдалении снимок не оказался за краем.
                frame = BackdropImage.focus(bitmap, width.toFloat(), height.toFloat(), fit, frame, 0f, 0f)
                invalidate()
                return true
            }
        })

        override fun onDraw(canvas: Canvas) {
            canvas.drawColor(theme.bg)
            BackdropImage.place(bitmap, width.toFloat(), height.toFloat(), fit, frame, m)
            canvas.drawBitmap(bitmap, m, brush)
            canvas.drawColor(veil)
        }

        @SuppressLint("ClickableViewAccessibility")
        override fun onTouchEvent(e: MotionEvent): Boolean {
            scaler.onTouchEvent(e)
            when (e.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    pointer = e.getPointerId(0)
                    lastX = e.x
                    lastY = e.y
                }
                MotionEvent.ACTION_POINTER_DOWN, MotionEvent.ACTION_POINTER_UP -> {
                    // Сменился набор пальцев: берём опорой первый оставшийся,
                    // иначе снимок прыгнул бы на расстояние между пальцами.
                    val keep = if (e.actionMasked == MotionEvent.ACTION_POINTER_UP && e.actionIndex == 0) 1 else 0
                    if (keep < e.pointerCount) {
                        pointer = e.getPointerId(keep)
                        lastX = e.getX(keep)
                        lastY = e.getY(keep)
                    }
                }
                MotionEvent.ACTION_MOVE -> {
                    val i = e.findPointerIndex(pointer)
                    if (i >= 0 && !scaler.isInProgress) {
                        frame = BackdropImage.focus(bitmap, width.toFloat(), height.toFloat(), fit, frame, e.getX(i) - lastX, e.getY(i) - lastY)
                        invalidate()
                    }
                    if (i >= 0) {
                        lastX = e.getX(i)
                        lastY = e.getY(i)
                    }
                }
            }
            return true
        }
    }

    private companion object {
        const val MATCH = ViewGroup.LayoutParams.MATCH_PARENT
        const val WRAP = ViewGroup.LayoutParams.WRAP_CONTENT
    }
}
