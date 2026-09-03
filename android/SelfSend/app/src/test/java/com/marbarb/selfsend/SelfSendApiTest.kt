package com.marbarb.selfsend

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class SelfSendApiTest {
    @Test
    fun normalizesServerAddresses() {
        assertEquals("http://192.168.1.20:8080", SelfSendApi.normalizeServerUrl("192.168.1.20:8080/"))
        assertEquals("https://send.example.com", SelfSendApi.normalizeServerUrl(" https://send.example.com "))
    }

    @Test
    fun rejectsServerPaths() {
        assertThrows(IllegalArgumentException::class.java) {
            SelfSendApi.normalizeServerUrl("https://send.example.com/not-root")
        }
    }

    @Test
    fun requiresHttpsForPublicServers() {
        assertThrows(IllegalArgumentException::class.java) {
            SelfSendApi.normalizeServerUrl("http://send.example.com")
        }
    }
}
