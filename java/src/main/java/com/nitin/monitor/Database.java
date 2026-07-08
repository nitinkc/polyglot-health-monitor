package com.nitin.monitor;

import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.SQLException;
import java.sql.Statement;

/**
 * Opens the shared SQLite file and applies schema.sql on startup if the
 * tables don't already exist. Point DB_PATH at the same file used by the
 * other three language implementations to test cross-language interop.
 */
public final class Database {

    private Database() {}

    public static Connection connect(String dbPath) {
        try {
            Connection conn = DriverManager.getConnection("jdbc:sqlite:" + dbPath);
            try (Statement stmt = conn.createStatement()) {
                stmt.execute("PRAGMA foreign_keys = ON;");
            }
            applySchema(conn);
            return conn;
        } catch (SQLException e) {
            throw new RuntimeException("failed to connect to sqlite at " + dbPath, e);
        }
    }

    private static void applySchema(Connection conn) throws SQLException {
        String schema = loadSchemaSql();
        try (Statement stmt = conn.createStatement()) {
            // schema.sql contains multiple ';'-terminated statements — split naively,
            // fine for this fixed, known-safe DDL file (not for arbitrary user SQL).
            for (String sql : schema.split(";")) {
                String trimmed = sql.trim();
                if (!trimmed.isEmpty()) {
                    stmt.execute(trimmed);
                }
            }
        }
    }

    private static String loadSchemaSql() {
        try (InputStream in = Database.class.getResourceAsStream("/schema.sql")) {
            if (in == null) {
                throw new IllegalStateException(
                        "schema.sql not found on classpath — copy it into src/main/resources/");
            }
            return new String(in.readAllBytes());
        } catch (IOException e) {
            throw new RuntimeException("failed to read schema.sql", e);
        }
    }
}
