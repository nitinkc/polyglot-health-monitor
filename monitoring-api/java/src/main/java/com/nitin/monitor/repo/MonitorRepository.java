package com.nitin.monitor.repo;

import com.nitin.monitor.dto.Monitor;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

/**
 * Raw JDBC against the shared schema.sql — intentionally no ORM, so the
 * JDBC idiom (PreparedStatement, try-with-resources, ResultSet mapping)
 * stays visible rather than hidden behind Hibernate.
 */
public class MonitorRepository {
    private final Connection conn;

    public MonitorRepository(Connection conn) {
        this.conn = conn;
    }

    public void insert(Monitor m) {
        String sql = """
            INSERT INTO monitors
                (id, name, url, interval_seconds, timeout_ms, failure_threshold,
                 status, consecutive_failures, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """;
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, m.id);
            ps.setString(2, m.name);
            ps.setString(3, m.url);
            ps.setInt(4, m.intervalSeconds);
            ps.setInt(5, m.timeoutMs);
            ps.setInt(6, m.failureThreshold);
            ps.setString(7, m.status);
            ps.setInt(8, m.consecutiveFailures);
            ps.setString(9, m.createdAt.toString());
            ps.setString(10, m.updatedAt.toString());
            ps.executeUpdate();
        } catch (SQLException e) {
            throw new RuntimeException("failed to insert monitor " + m.id, e);
        }
    }

    public Optional<Monitor> findById(String id) {
        String sql = "SELECT * FROM monitors WHERE id = ?";
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, id);
            try (ResultSet rs = ps.executeQuery()) {
                return rs.next() ? Optional.of(mapRow(rs)) : Optional.empty();
            }
        } catch (SQLException e) {
            throw new RuntimeException("failed to find monitor " + id, e);
        }
    }

    public List<Monitor> findAll() {
        String sql = "SELECT * FROM monitors ORDER BY created_at DESC";
        List<Monitor> out = new ArrayList<>();
        try (PreparedStatement ps = conn.prepareStatement(sql);
             ResultSet rs = ps.executeQuery()) {
            while (rs.next()) {
                out.add(mapRow(rs));
            }
        } catch (SQLException e) {
            throw new RuntimeException("failed to list monitors", e);
        }
        return out;
    }

    /** Updates status + consecutive_failures + updated_at after a check (Section 4 rules). */
    public void updateStatus(String id, String status, int consecutiveFailures) {
        String sql = """
            UPDATE monitors
            SET status = ?, consecutive_failures = ?, updated_at = ?
            WHERE id = ?
        """;
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, status);
            ps.setInt(2, consecutiveFailures);
            ps.setString(3, Instant.now().toString());
            ps.setString(4, id);
            ps.executeUpdate();
        } catch (SQLException e) {
            throw new RuntimeException("failed to update monitor status " + id, e);
        }
    }

    public void delete(String id) {
        String sql = "DELETE FROM monitors WHERE id = ?"; // cascades to check_results
        try (PreparedStatement ps = conn.prepareStatement(sql)) {
            ps.setString(1, id);
            ps.executeUpdate();
        } catch (SQLException e) {
            throw new RuntimeException("failed to delete monitor " + id, e);
        }
    }

    private Monitor mapRow(ResultSet rs) throws SQLException {
        Monitor m = new Monitor();
        m.id = rs.getString("id");
        m.name = rs.getString("name");
        m.url = rs.getString("url");
        m.intervalSeconds = rs.getInt("interval_seconds");
        m.timeoutMs = rs.getInt("timeout_ms");
        m.failureThreshold = rs.getInt("failure_threshold");
        m.status = rs.getString("status");
        m.consecutiveFailures = rs.getInt("consecutive_failures");
        m.createdAt = Instant.parse(rs.getString("created_at"));
        m.updatedAt = Instant.parse(rs.getString("updated_at"));
        return m;
    }
}
