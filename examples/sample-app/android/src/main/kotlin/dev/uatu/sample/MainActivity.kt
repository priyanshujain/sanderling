package dev.uatu.sample

import android.app.Activity
import android.graphics.Color
import android.os.Bundle
import android.text.Editable
import android.text.TextWatcher
import android.view.Gravity
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView

class MainActivity : Activity() {
    companion object {
        @Volatile var clickCount: Int = 0
        @Volatile var username: String = ""
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

        val usernameLabel = TextView(this).apply {
            text = "Username: "
            textSize = 20f
            setTextColor(Color.BLACK)
            gravity = Gravity.CENTER
        }
        layout.addView(usernameLabel)

        val usernameField = EditText(this).apply {
            hint = "username"
            contentDescription = "username_field"
            textSize = 18f
            addTextChangedListener(object : TextWatcher {
                override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
                override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
                override fun afterTextChanged(s: Editable?) {
                    username = s?.toString() ?: ""
                    usernameLabel.text = "Username: $username"
                }
            })
        }
        layout.addView(usernameField)

        setContentView(layout)
    }
}
