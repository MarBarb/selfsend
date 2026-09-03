package com.marbarb.selfsend

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.viewmodel.compose.viewModel

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            val model: SelfSendViewModel = viewModel()
            LaunchedEffect(Unit) {
                if (intent?.action == Intent.ACTION_SEND && intent.type == "text/plain") {
                    intent.getStringExtra(Intent.EXTRA_TEXT)?.takeIf { "#pair=" in it }?.let(model::connect)
                }
            }
            MaterialTheme(
                colorScheme = lightColorScheme(
                    primary = Color(0xFF09C261),
                    onPrimary = Color.White,
                    primaryContainer = Color(0xFFD9FFE6),
                    surface = Color(0xFFF7F9F7),
                    surfaceContainer = Color(0xFFFFFFFF),
                ),
            ) { SelfSendApp(model) }
        }
    }
}
