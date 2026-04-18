package dev.uatu.sample

import android.app.Activity
import android.graphics.Color
import android.os.Bundle
import android.view.Gravity
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView

class MainActivity : Activity() {
    companion object {
        @Volatile var clickCount: Int = 0
    }

    private lateinit var label: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setBackgroundColor(Color.parseColor("#FAFAFA"))
            setPadding(64, 96, 64, 64)
        }

        label = TextView(this).apply {
            text = "Clicks: 0"
            textSize = 28f
            setTextColor(Color.BLACK)
            gravity = Gravity.CENTER
        }
        layout.addView(label)

        val button = Button(this).apply {
            text = "Click me"
            textSize = 18f
            setOnClickListener {
                clickCount++
                label.text = "Clicks: $clickCount"
            }
        }
        layout.addView(button)

        setContentView(layout)
    }
}
