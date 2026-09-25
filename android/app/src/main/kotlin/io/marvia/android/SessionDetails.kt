package io.marvia.android

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import android.os.SystemClock

/** Local tunnel counters only. No destination addresses or access keys are retained. */
object SessionDetails {
    data class Sample(val at: Long, val down: Double, val up: Double)
    data class Session(val began: Long, val seconds: Long, val received: Long, val sent: Long)
    data class SpeedSnapshot(val session: Session?, val samples: List<Sample>, val peak: Double)

    private val samples = ArrayDeque<Sample>()
    private var current: Session? = null
    private var lastAt = 0L
    private var startedAt = 0L
    private var peak = 0.0

    @Synchronized fun begin(now: Long = SystemClock.elapsedRealtime()) {
        samples.clear()
        current = Session(System.currentTimeMillis(), 0, 0, 0)
        lastAt = now
        startedAt = now
        peak = 0.0
    }

    @Synchronized fun sample(received: Long, sent: Long, now: Long = SystemClock.elapsedRealtime()) {
        val old = current ?: return
        if (now <= lastAt) return
        val dt = (now - lastAt) / 1000.0
        val s = Sample(now, (received - old.received).coerceAtLeast(0) / dt, (sent - old.sent).coerceAtLeast(0) / dt)
        samples.addLast(s)
        while (samples.size > 120) samples.removeFirst()
        peak = maxOf(peak, s.down + s.up)
        current = old.copy(seconds = (now - startedAt) / 1000, received = received, sent = sent)
        lastAt = now
    }

    @Synchronized fun snapshot(): SpeedSnapshot = SpeedSnapshot(current, samples.toList(), peak)

    @Synchronized fun finish(context: Context) {
        val s = current ?: return
        // Do not let a damaged history database bring down the VPN service.
        try {
            History(context).use { history ->
                history.migrateLegacy(context)
                history.insert(s)
            }
        } catch (e: Exception) {
            android.util.Log.w("MarviaSessions", "Could not save session history", e)
        }
        current = null
        samples.clear()
    }

    /** Read a page, newest first. History is no longer capped at 20 sessions. */
    fun history(context: Context, limit: Int, offset: Int = 0): List<Session> = try {
        History(context).use { db ->
            db.migrateLegacy(context)
            db.page(limit.coerceIn(1, 100), offset.coerceAtLeast(0))
        }
    } catch (e: Exception) {
        android.util.Log.w("MarviaSessions", "Could not load session history", e)
        emptyList()
    }

    fun count(context: Context): Int = try {
        History(context).use { db ->
            db.migrateLegacy(context)
            db.count()
        }
    } catch (_: Exception) { 0 }

    private class History(context: Context) : SQLiteOpenHelper(context, "session_history.db", null, 1) {
        override fun onCreate(db: SQLiteDatabase) {
            db.execSQL("CREATE TABLE sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, began INTEGER NOT NULL, seconds INTEGER NOT NULL, received INTEGER NOT NULL, sent INTEGER NOT NULL)")
            db.execSQL("CREATE INDEX sessions_recent ON sessions(began DESC, id DESC)")
            db.execSQL("CREATE TABLE migration (name TEXT PRIMARY KEY)")
        }

        override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

        fun insert(s: Session) {
            writableDatabase.insertOrThrow("sessions", null, ContentValues().apply {
                put("began", s.began)
                put("seconds", s.seconds)
                put("received", s.received)
                put("sent", s.sent)
            })
        }

        fun page(limit: Int, offset: Int): List<Session> {
            val result = ArrayList<Session>(limit)
            readableDatabase.rawQuery(
                "SELECT began, seconds, received, sent FROM sessions ORDER BY began DESC, id DESC LIMIT ? OFFSET ?",
                arrayOf(limit.toString(), offset.toString()),
            ).use { cursor ->
                while (cursor.moveToNext()) result += Session(cursor.getLong(0), cursor.getLong(1), cursor.getLong(2), cursor.getLong(3))
            }
            return result
        }

        fun count(): Int = readableDatabase.rawQuery("SELECT COUNT(*) FROM sessions", null).use {
            if (it.moveToFirst()) it.getInt(0) else 0
        }

        fun migrateLegacy(context: Context) {
            val prefs = context.getSharedPreferences("veil", Context.MODE_PRIVATE)
            val saved = prefs.getString("session_history", "").orEmpty()
            val db = writableDatabase
            db.beginTransaction()
            try {
                val alreadyDone = db.rawQuery("SELECT 1 FROM migration WHERE name = 'legacy'", null).use { it.moveToFirst() }
                if (alreadyDone) {
                    db.setTransactionSuccessful()
                    return
                }
                saved.lineSequence().forEach { row ->
                    val values = row.split(',').mapNotNull { it.toLongOrNull()?.takeIf { n -> n >= 0 } }
                    if (values.size == 4) insert(Session(values[0], values[1], values[2], values[3]))
                }
                db.execSQL("INSERT INTO migration(name) VALUES ('legacy')")
                db.setTransactionSuccessful()
            } finally { db.endTransaction() }
            // The marker and imported rows commit together. Keep the old string as a recovery copy.
        }
    }
}
